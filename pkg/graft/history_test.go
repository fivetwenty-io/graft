package graft_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	. "github.com/fivetwenty-io/graft/pkg/graft"
)

// TestHistory_DefaultOff asserts a plain engine (no WithHistoryTracking/
// WithMemoryConfig, no TrackHistory()) produces a Document whose History()
// is present (never nil) but carries no entries - tracking defaults off,
// matching the "costs ~nothing when off" constraint.
func TestHistory_DefaultOff(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	base := parseYAML(t, engine, "host: localhost\n")
	overlay := parseYAML(t, engine, "host: production.com\n")

	result, err := engine.Merge(context.Background(), base, overlay).Execute()
	if err != nil {
		t.Fatalf("failed to merge: %v", err)
	}

	history := result.History()
	if history == nil {
		t.Fatal("Document.History() must never return a nil interface")
	}
	if got := history.Timeline(); len(got) != 0 {
		t.Errorf("expected empty Timeline() with tracking off, got %d entries", len(got))
	}
	if got := history.AllPaths(); len(got) != 0 {
		t.Errorf("expected empty AllPaths() with tracking off, got %v", got)
	}
	if got := history.ChangedPaths(); len(got) != 0 {
		t.Errorf("expected empty ChangedPaths() with tracking off, got %v", got)
	}
	if got := history.ForPath("host"); len(got) != 0 {
		t.Errorf("expected empty ForPath() with tracking off, got %v", got)
	}
}

// TestHistory_TrackHistory_LazilyActivatesTracking asserts calling
// TrackHistory() on a merge chain built against an engine with no
// engine-level history configuration still records and attaches history,
// via mergeBuilderImpl.ensureHistoryTracking's lazy EnableMemoryTracking
// call.
func TestHistory_TrackHistory_LazilyActivatesTracking(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	base := parseYAML(t, engine, "host: localhost\n")
	overlay := parseYAML(t, engine, "host: production.com\n")

	result, err := engine.Merge(context.Background(), base, overlay).
		TrackHistory().
		Execute()
	if err != nil {
		t.Fatalf("failed to merge: %v", err)
	}

	entries := result.History().ForPath("host")
	if len(entries) != 1 {
		t.Fatalf("expected 1 recorded entry for \"host\", got %d: %+v", len(entries), entries)
	}
	// ForPath's OldValue is reconstructed from a PRIOR RECORDED version;
	// "host" has none (its base-document value was never separately
	// recorded), so ForPath reports nil here even though the real prior
	// value was "localhost" - see HistoryEntry.OldValue's doc comment.
	if entries[0].OldValue != nil {
		t.Errorf("expected ForPath's OldValue to be nil on a path's first recorded entry, got %v", entries[0].OldValue)
	}
	if entries[0].NewValue != "production.com" {
		t.Errorf("expected NewValue \"production.com\", got %v", entries[0].NewValue)
	}
	if entries[0].Phase != PhaseMerge {
		t.Errorf("expected Phase PhaseMerge, got %v", entries[0].Phase)
	}

	// Timeline()/Query() read ChangeEvent directly, which does carry the
	// real caller-supplied prior value even on a path's first entry.
	timeline := result.History().Timeline()
	if len(timeline) != 1 {
		t.Fatalf("expected 1 timeline entry, got %d", len(timeline))
	}
	if timeline[0].OldValue != "localhost" {
		t.Errorf("expected Timeline's OldValue \"localhost\", got %v", timeline[0].OldValue)
	}
}

// TestHistory_ConcurrentExecute_TrackHistory reproduces the phase2-review
// F1 data race: EnableMemoryTracking (engine.go) reads and writes
// e.documentMemory with no synchronization, called from
// ensureHistoryTracking on Execute()'s hot path, so two goroutines sharing
// one engine - at least one calling TrackHistory() - raced under `-race`
// on the very first run. Beyond the detector warning, the interleaving was
// lossy: two goroutines could each construct a DocumentMemory and one was
// silently dropped, leaving that goroutine's own Document.History() empty
// even though its merge succeeded with no error. This test asserts both:
// no data race (enforced by `go test -race`, not by an assertion here) and
// no lossy drop: history is engine-scoped, so every TrackHistory()
// goroutine must observe a non-empty history — under the pre-fix race a
// goroutine's freshly built DocumentMemory could be silently replaced,
// leaving its History() empty. The assertion cannot distinguish which
// goroutine recorded an entry, only that none observes the lossy drop.
func TestHistory_ConcurrentExecute_TrackHistory(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	const goroutines = 16
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	lostHistory := make(chan int, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			base, err := engine.ParseYAML([]byte(fmt.Sprintf("host: base-%d\n", id)))
			if err != nil {
				errs <- fmt.Errorf("goroutine %d: parse base: %w", id, err)
				return
			}
			overlay, err := engine.ParseYAML([]byte(fmt.Sprintf("host: overlay-%d\n", id)))
			if err != nil {
				errs <- fmt.Errorf("goroutine %d: parse overlay: %w", id, err)
				return
			}

			builder := engine.Merge(context.Background(), base, overlay)
			tracked := id%2 == 0
			if tracked {
				builder = builder.TrackHistory()
			}

			result, err := builder.Execute()
			if err != nil {
				errs <- fmt.Errorf("goroutine %d: execute: %w", id, err)
				return
			}

			if tracked && len(result.History().ForPath("host")) == 0 {
				lostHistory <- id
			}
		}(i)
	}

	wg.Wait()
	close(errs)
	close(lostHistory)

	for err := range errs {
		t.Error(err)
	}

	var lost []int
	for id := range lostHistory {
		lost = append(lost, id)
	}
	if len(lost) != 0 {
		t.Errorf("expected every TrackHistory() goroutine to retain its own recorded \"host\" entry; %d goroutines lost theirs to a dropped DocumentMemory: %v", len(lost), lost)
	}
}

// TestHistory_EngineLevelTracking_AppliesWithoutTrackHistory asserts
// engine-level WithHistoryTracking(true) records and attaches history
// even when the individual merge chain never calls TrackHistory() -
// consistent with DocumentMemory recording already being gated only on
// tracker.IsEnabled() at every recording site in merge_builder_impl.go.
func TestHistory_EngineLevelTracking_AppliesWithoutTrackHistory(t *testing.T) {
	engine, err := NewEngine(WithHistoryTracking(true))
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	base := parseYAML(t, engine, "host: localhost\n")
	overlay := parseYAML(t, engine, "host: production.com\n")

	result, err := engine.Merge(context.Background(), base, overlay).Execute()
	if err != nil {
		t.Fatalf("failed to merge: %v", err)
	}

	entries := result.History().ForPath("host")
	if len(entries) != 1 {
		t.Fatalf("expected 1 recorded entry for \"host\" from engine-level tracking, got %d", len(entries))
	}
}

// TestHistory_WithHistoryConfig_MaxEntriesPerPath asserts
// HistoryConfig.MaxEntriesPerPath reaches MemoryConfig.MaxVersionsPerNode
// and is enforced by DocumentMemory's existing per-path pruning.
func TestHistory_WithHistoryConfig_MaxEntriesPerPath(t *testing.T) {
	engine, err := NewEngine(WithHistoryConfig(HistoryConfig{MaxEntriesPerPath: 1}))
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	base := parseYAML(t, engine, "host: one\n")
	mid := parseYAML(t, engine, "host: two\n")
	top := parseYAML(t, engine, "host: three\n")

	result, err := engine.Merge(context.Background(), base, mid, top).Execute()
	if err != nil {
		t.Fatalf("failed to merge: %v", err)
	}

	entries := result.History().ForPath("host")
	if len(entries) != 1 {
		t.Fatalf("expected MaxEntriesPerPath=1 to cap \"host\" at 1 recorded version, got %d: %+v", len(entries), entries)
	}
	if entries[0].NewValue != "three" {
		t.Errorf("expected the single retained version to be the most recent (\"three\"), got %v", entries[0].NewValue)
	}
}

// TestHistory_WithHistoryConfig_MaxEntriesPerPathDoesNotBoundTimeline
// pins phase2-review finding F3.1: HistoryConfig.MaxEntriesPerPath caps
// only NodeHistory.Versions (History.ForPath's storage); the timeline
// backing History.Timeline/Query/AllPaths/ChangedPaths is never trimmed.
// With MaxEntriesPerPath: 1 and three merges on the same path,
// ForPath("host") stays capped at 1 while ChangedPaths still reports
// "host" as touched more than once - the documented incoherence.
func TestHistory_WithHistoryConfig_MaxEntriesPerPathDoesNotBoundTimeline(t *testing.T) {
	engine, err := NewEngine(WithHistoryConfig(HistoryConfig{MaxEntriesPerPath: 1}))
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	base := parseYAML(t, engine, "host: one\n")
	mid := parseYAML(t, engine, "host: two\n")
	top := parseYAML(t, engine, "host: three\n")

	result, err := engine.Merge(context.Background(), base, mid, top).Execute()
	if err != nil {
		t.Fatalf("failed to merge: %v", err)
	}

	history := result.History()

	if entries := history.ForPath("host"); len(entries) != 1 {
		t.Fatalf("expected MaxEntriesPerPath=1 to cap ForPath(\"host\") at 1, got %d", len(entries))
	}

	timelineCount := 0
	for _, e := range history.Timeline() {
		if e.Path == "host" {
			timelineCount++
		}
	}
	if timelineCount < 2 {
		t.Errorf("expected Timeline() to keep every \"host\" change uncapped by MaxEntriesPerPath (>= 2), got %d", timelineCount)
	}

	changed := history.ChangedPaths()
	found := false
	for _, p := range changed {
		if p == "host" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected ChangedPaths() to still report \"host\" as touched more than once despite MaxEntriesPerPath=1, got %v", changed)
	}
}

// TestHistory_WithHistoryConfig_CompressValuesNoObservableEffect pins
// phase2-review finding F3.2: HistoryConfig.CompressValues, like
// RetentionPeriod, has no observable effect through WithHistoryConfig -
// compression only runs from performCleanup, which WithHistoryConfig
// never triggers (it sets neither MaxTotalVersions/MaxMemoryMB nor
// CleanupInterval). No version is ever compressed, regardless of
// CompressValues or RetentionPeriod.
func TestHistory_WithHistoryConfig_CompressValuesNoObservableEffect(t *testing.T) {
	engine, err := NewEngine(WithHistoryConfig(HistoryConfig{
		CompressValues:  true,
		RetentionPeriod: time.Nanosecond,
	}))
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	base := parseYAML(t, engine, "host: one\n")
	overlay := parseYAML(t, engine, "host: two\n")

	result, err := engine.Merge(context.Background(), base, overlay).Execute()
	if err != nil {
		t.Fatalf("failed to merge: %v", err)
	}

	entries := result.History().ForPath("host")
	if len(entries) == 0 {
		t.Fatal("expected recorded entries for \"host\"; an empty history would make this test pass vacuously")
	}
	for _, entry := range entries {
		if entry.NewValue == nil {
			t.Errorf("expected value to remain uncompressed (non-nil NewValue), entry: %+v", entry)
		}
		if entry.Metadata["compressed"] != nil {
			t.Errorf("expected no \"compressed\" metadata (CompressValues has no effect through WithHistoryConfig), got %+v", entry.Metadata)
		}
	}
}

// TestHistory_Query_Limit exercises the new HistoryFilter.Limit field,
// both through History.Query and directly against DocumentMemory.Query.
func TestHistory_Query_Limit(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	base := parseYAML(t, engine, "a: 1\nb: 1\nc: 1\n")
	overlay := parseYAML(t, engine, "a: 2\nb: 2\nc: 2\n")

	result, err := engine.Merge(context.Background(), base, overlay).
		TrackHistory().
		Execute()
	if err != nil {
		t.Fatalf("failed to merge: %v", err)
	}

	full := result.History().Query(HistoryFilter{})
	if len(full) < 3 {
		t.Fatalf("expected at least 3 entries with no filter, got %d", len(full))
	}

	limited := result.History().Query(HistoryFilter{Limit: 2})
	if len(limited) != 2 {
		t.Fatalf("expected Limit: 2 to cap results at 2, got %d", len(limited))
	}
	for i, e := range limited {
		if e.Path != full[i].Path || e.Version != full[i].Version || e.Index != full[i].Index {
			t.Errorf("expected Limit to keep the first N matches in timeline order; entry %d differs: %+v vs %+v", i, e, full[i])
		}
	}

	if got := result.History().Query(HistoryFilter{Limit: 0}); len(got) != len(full) {
		t.Errorf("expected Limit: 0 to mean unlimited (matching the unfiltered count %d), got %d", len(full), len(got))
	}
}

// TestDocumentMemory_QueryLimit is the same Limit behavior tested
// directly against DocumentMemory.Query, independent of the History
// veneer.
func TestDocumentMemory_QueryLimit(t *testing.T) {
	memory := NewDocumentMemory(MemoryConfig{Enabled: true})
	defer memory.StopBackgroundCleanup()

	for i, path := range []string{"a", "b", "c", "d"} {
		if err := memory.RecordChange(path, nil, i, PhaseMerge, OpMerge, "test"); err != nil {
			t.Fatalf("failed to record change: %v", err)
		}
	}

	results := memory.Query(HistoryFilter{Limit: 2})
	if len(results) != 2 {
		t.Fatalf("expected 2 results with Limit: 2, got %d", len(results))
	}
	if results[0].Path != "a" || results[1].Path != "b" {
		t.Errorf("expected the first 2 recorded paths (a, b) in order, got %q, %q", results[0].Path, results[1].Path)
	}
}

// TestHistory_ListElementGapIsPinned asserts list-element mutations are
// NOT recorded - the documented gap in History's doc comment (history.go)
// - so a future accidental fix does not silently change the documented
// contract without a test catching the API-shape change it implies.
func TestHistory_ListElementGapIsPinned(t *testing.T) {
	engine, err := NewEngine(WithHistoryTracking(true))
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	doc := parseYAML(t, engine, `
list:
  - (( append ))
  - c
meta:
  k: v
`)

	result, err := engine.Merge(context.Background(), doc).Execute()
	if err != nil {
		t.Fatalf("failed to merge: %v", err)
	}

	for _, path := range result.History().AllPaths() {
		if strings.Contains(path, "list.0") || strings.Contains(path, "list.1") {
			t.Errorf("expected no list-element path to be recorded (documented gap); got %q among AllPaths()", path)
		}
	}

	if entries := result.History().ForPath("list.0"); len(entries) != 0 {
		t.Errorf("expected ForPath(\"list.0\") to be empty (list-element gap), got %+v", entries)
	}
}

// TestHistory_NestedSubtreeGapIsPinned asserts a brand-new nested subtree
// is recorded only at its top-level key, not at every descendant path -
// the documented gap in History's doc comment (history.go) distinct from
// the list-element gap above. A newly added map key has no corresponding
// entry in the base document, so performSimpleMergeAtPath's recursion
// into mergeValuesAtPath never reaches the nested levels; the whole
// subtree is recorded once, as a single add, at the point it first
// appears.
func TestHistory_NestedSubtreeGapIsPinned(t *testing.T) {
	engine, err := NewEngine(WithHistoryTracking(true))
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	base := parseYAML(t, engine, "meta:\n  k: v\n")
	overlay := parseYAML(t, engine, "added:\n  nested:\n    leaf: hello\n")

	result, err := engine.Merge(context.Background(), base, overlay).Execute()
	if err != nil {
		t.Fatalf("failed to merge: %v", err)
	}

	history := result.History()

	if entries := history.ForPath("added.nested.leaf"); len(entries) != 0 {
		t.Errorf("expected ForPath(\"added.nested.leaf\") to be empty (documented nested-subtree gap), got %+v", entries)
	}
	if entries := history.ForPath("added.nested"); len(entries) != 0 {
		t.Errorf("expected ForPath(\"added.nested\") to be empty (documented nested-subtree gap), got %+v", entries)
	}

	topEntries := history.ForPath("added")
	if len(topEntries) != 1 {
		t.Fatalf("expected exactly 1 recorded entry at the top-level \"added\" key, got %d: %+v", len(topEntries), topEntries)
	}
	newValue, ok := topEntries[0].NewValue.(map[string]interface{})
	if !ok {
		t.Fatalf("expected \"added\"'s recorded NewValue to be the whole subtree map, got %T", topEntries[0].NewValue)
	}
	if _, ok := newValue["nested"]; !ok {
		t.Errorf("expected the recorded subtree to carry \"nested\", got %+v", newValue)
	}
}

// TestHistory_GoPatchDocument_IsEmpty asserts a go-patch document's
// promoted History() method (via *document embedding - see
// gopatch_document.go) returns a valid, empty History rather than
// panicking or returning nil.
func TestHistory_GoPatchDocument_IsEmpty(t *testing.T) {
	doc := NewGoPatchDocument(nil)

	history := doc.History()
	if history == nil {
		t.Fatal("expected a non-nil History even for a go-patch document")
	}
	if got := history.Timeline(); len(got) != 0 {
		t.Errorf("expected an empty Timeline() for a go-patch document, got %v", got)
	}
	if got := history.AllPaths(); len(got) != 0 {
		t.Errorf("expected empty AllPaths() for a go-patch document, got %v", got)
	}
	jsonBytes, err := history.ToJSON()
	if err != nil {
		t.Fatalf("expected ToJSON to succeed on an empty History, got error: %v", err)
	}
	if !strings.Contains(string(jsonBytes), `"total_entries":0`) {
		t.Errorf("expected ToJSON output to report zero entries, got %s", jsonBytes)
	}
}

// TestHistory_Interaction_PostProcessors asserts history survives
// WithPostProcessors' document rebuild (runPostProcessors reconstructs a
// fresh *document via NewDocumentFromInterface whenever any processor is
// registered - see merge_builder_impl.go's applyPostProcessing, which
// attaches history only after that rebuild).
func TestHistory_Interaction_PostProcessors(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	base := parseYAML(t, engine, "host: localhost\npassword: secret\n")
	overlay := parseYAML(t, engine, "host: production.com\n")

	result, err := engine.Merge(context.Background(), base, overlay).
		TrackHistory().
		WithPostProcessors(NewSecurityRedactor([]string{"password"}, "")).
		Execute()
	if err != nil {
		t.Fatalf("failed to merge: %v", err)
	}

	// The redactor actually ran (proves post-processing still applied).
	if got, _ := result.GetString("password"); got != "***REDACTED***" {
		t.Errorf("expected password to be redacted, got %q", got)
	}

	// History about the merge (recorded before the redactor ran) is still
	// attached to the post-processed Document.
	entries := result.History().ForPath("host")
	if len(entries) != 1 {
		t.Fatalf("expected history to survive post-processing, got %d entries for \"host\"", len(entries))
	}
}

// TestHistory_Interaction_BaseOverlay asserts TrackHistory() records
// correctly through the C5 Base/Overlay/OverlayFile builder surface, not
// only through Engine.Merge's variadic document list.
func TestHistory_Interaction_BaseOverlay(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	defaults := parseYAML(t, engine, "name: myapp\ndatabase:\n  host: localhost\n")
	production := parseYAML(t, engine, "database:\n  host: prod.example.com\n")

	result, err := engine.Merge(context.Background()).
		Base(defaults).
		Overlay(production).
		TrackHistory().
		Execute()
	if err != nil {
		t.Fatalf("failed to merge: %v", err)
	}

	entries := result.History().ForPath("database.host")
	if len(entries) != 1 {
		t.Fatalf("expected 1 recorded entry for \"database.host\", got %d", len(entries))
	}
	if entries[0].NewValue != "prod.example.com" {
		t.Errorf("expected NewValue \"prod.example.com\", got %v", entries[0].NewValue)
	}
}

// TestHistory_ToJSON_ToYAML checks both serialization methods produce a
// valid, decodable document with the expected summary shape.
func TestHistory_ToJSON_ToYAML(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	base := parseYAML(t, engine, "host: localhost\n")
	overlay := parseYAML(t, engine, "host: production.com\n")

	result, err := engine.Merge(context.Background(), base, overlay).
		TrackHistory().
		Execute()
	if err != nil {
		t.Fatalf("failed to merge: %v", err)
	}

	jsonBytes, err := result.History().ToJSON()
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}

	var decoded struct {
		Entries []map[string]interface{} `json:"entries"`
		Summary struct {
			TotalEntries int            `json:"total_entries"`
			ChangedPaths int            `json:"changed_paths"`
			ByPhase      map[string]int `json:"by_phase"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(jsonBytes, &decoded); err != nil {
		t.Fatalf("failed to decode ToJSON output: %v", err)
	}
	if decoded.Summary.TotalEntries != 1 {
		t.Errorf("expected total_entries 1, got %d", decoded.Summary.TotalEntries)
	}
	if decoded.Summary.ByPhase["merge"] != 1 {
		t.Errorf("expected by_phase.merge 1, got %+v", decoded.Summary.ByPhase)
	}
	if len(decoded.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(decoded.Entries))
	}
	if decoded.Entries[0]["path"] != "host" {
		t.Errorf("expected entry path \"host\", got %v", decoded.Entries[0]["path"])
	}

	yamlBytes, err := result.History().ToYAML()
	if err != nil {
		t.Fatalf("ToYAML failed: %v", err)
	}
	if len(yamlBytes) == 0 {
		t.Error("expected non-empty ToYAML output")
	}
	if !strings.Contains(string(yamlBytes), "total_entries: 1") {
		t.Errorf("expected ToYAML output to contain the summary block, got %s", yamlBytes)
	}
}

// TestHistory_TimelineAfterBefore checks the two new time-bounded
// Timeline queries against two merges run in sequence on the same
// tracking-enabled engine. This also exercises History's documented
// engine-wide scope: the second merge's Document.History() sees the
// first merge's recorded change too, since both share one DocumentMemory.
func TestHistory_TimelineAfterBefore(t *testing.T) {
	engine, err := NewEngine(WithHistoryTracking(true))
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	baseA := parseYAML(t, engine, "a: 1\n")
	overlayA := parseYAML(t, engine, "a: 2\n")
	if _, err := engine.Merge(context.Background(), baseA, overlayA).Execute(); err != nil {
		t.Fatalf("failed first merge: %v", err)
	}

	mid := time.Now()
	time.Sleep(2 * time.Millisecond)

	baseB := parseYAML(t, engine, "b: 1\n")
	overlayB := parseYAML(t, engine, "b: 2\n")
	result, err := engine.Merge(context.Background(), baseB, overlayB).Execute()
	if err != nil {
		t.Fatalf("failed second merge: %v", err)
	}

	history := result.History()

	after := history.TimelineAfter(mid)
	if len(after) != 1 || after[0].Path != "b" {
		t.Errorf("expected TimelineAfter(mid) to return only \"b\", got %+v", after)
	}

	before := history.TimelineBefore(mid)
	if len(before) != 1 || before[0].Path != "a" {
		t.Errorf("expected TimelineBefore(mid) to return only \"a\", got %+v", before)
	}
}

// TestHistory_AllPaths_ChangedPaths checks the derived path-enumeration
// methods against a mix of once-touched and repeatedly-touched paths.
func TestHistory_AllPaths_ChangedPaths(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	base := parseYAML(t, engine, "host: localhost\nport: 5432\n")
	overlay := parseYAML(t, engine, "host: production.com\n")

	result, err := engine.Merge(context.Background(), base, overlay).
		TrackHistory().
		Execute()
	if err != nil {
		t.Fatalf("failed to merge: %v", err)
	}

	all := result.History().AllPaths()
	if len(all) != 1 || all[0] != "host" {
		t.Errorf("expected AllPaths() == [\"host\"] (only paths with a recorded change), got %v", all)
	}

	changed := result.History().ChangedPaths()
	if len(changed) != 0 {
		t.Errorf("expected ChangedPaths() empty (\"host\" was set once, not overwritten twice), got %v", changed)
	}
}

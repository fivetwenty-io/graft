package graft

import (
	"context"
	"errors"
	"testing"
	"time"
)

// recordingProcessor records the RawData() it observed and the
// ProcessMetadata it was given, so tests can assert both when in the
// pipeline it ran and what it saw. It always implements
// PriorityPostProcessor; when priority is nil, Priority() mirrors the
// adapter's own default-by-phase fallback (see minimalProcessor below for
// a type that omits Priority() entirely, exercising that fallback inside
// the adapter itself rather than duplicating it here).
type recordingProcessor struct {
	name     string
	phase    PostProcessPhase
	priority *int

	observedData map[string]interface{}
	observedMeta *ProcessMetadata
	returnErr    error
	mutate       func(Document) Document

	// observedDuration is a snapshot of meta.Duration taken at the moment
	// this processor's Process ran, independent of observedMeta: every
	// processor in one pipeline shares the same *ProcessMetadata pointer
	// (see runPostProcessors), so observedMeta.Duration alone cannot
	// distinguish "processor 1 saw X" from "processor 2 later refreshed
	// the shared struct to Y" once the pipeline has finished running.
	observedDuration time.Duration

	// sleep, when positive, is paused inside Process AFTER this
	// processor has recorded its own observedMeta but BEFORE returning -
	// so it does not inflate this processor's own observed Duration, but
	// does inflate whatever runs after it, since postProcessorAdapter
	// refreshes meta.Duration from time.Since(StartTime) immediately
	// before each processor's own Process call (see
	// TestWithPostProcessors_DurationRefreshedPerProcessor).
	sleep time.Duration
}

func (r *recordingProcessor) Name() string            { return r.name }
func (r *recordingProcessor) Phase() PostProcessPhase { return r.phase }

func (r *recordingProcessor) Priority() int {
	if r.priority != nil {
		return *r.priority
	}
	switch r.phase {
	case PhaseEarly:
		return 0
	case PhaseLate:
		return 100
	default:
		return 50
	}
}

func (r *recordingProcessor) Process(_ context.Context, doc Document, meta *ProcessMetadata) (Document, error) {
	if data, ok := doc.RawData().(map[string]interface{}); ok {
		r.observedData = data
	}
	r.observedMeta = meta
	r.observedDuration = meta.Duration
	if r.sleep > 0 {
		time.Sleep(r.sleep)
	}
	if r.returnErr != nil {
		return nil, r.returnErr
	}
	if r.mutate != nil {
		return r.mutate(doc), nil
	}
	return doc, nil
}

// minimalProcessor implements only PostProcessor (no Priority method), to
// exercise postProcessorAdapter's default-priority-by-phase fallback for a
// processor that genuinely does not implement PriorityPostProcessor.
type minimalProcessor struct {
	name  string
	phase PostProcessPhase
	ran   *bool
}

func (m *minimalProcessor) Name() string            { return m.name }
func (m *minimalProcessor) Phase() PostProcessPhase { return m.phase }

func (m *minimalProcessor) Process(_ context.Context, doc Document, _ *ProcessMetadata) (Document, error) {
	*m.ran = true
	return doc, nil
}

func TestWithPostProcessors_RunsAfterEvaluationPruneCherryPick(t *testing.T) {
	rec := &recordingProcessor{name: "recorder", phase: PhaseNormal}

	engine := NewDefaultEngine()
	base := NewDocument(map[string]interface{}{
		"keep": "value",
		// "prune" carries a "(( prune ))" operator, which the EVALUATOR
		// removes during evaluation regardless of where applyPruning
		// runs in the pipeline - it does not, on its own, distinguish
		// "post-processors ran after evaluation" from "post-processors
		// ran after pruning". "plain" carries no operator at all and is
		// only ever removed by applyPruning itself (via WithPrune
		// below), so its absence is what actually pins pruning running
		// before the post-processor - see phase2-review finding F7.
		"prune":  "(( prune ))",
		"plain":  "b",
		"grabme": "(( grab keep ))",
	})

	result, err := engine.Merge(context.Background(), base).
		WithPrune("prune", "plain").
		WithPostProcessors(rec).
		Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if rec.observedData == nil {
		t.Fatal("post-processor never ran")
	}

	// prune must already be gone by the time the post-processor observes
	// the document.
	if _, present := rec.observedData["prune"]; present {
		t.Errorf("post-processor observed unpruned key %q", "prune")
	}

	// "plain" carries no (( prune )) operator, so only applyPruning
	// itself removes it - its absence here is the load-bearing
	// assertion that pruning actually ran before the post-processor.
	if _, present := rec.observedData["plain"]; present {
		t.Errorf("post-processor observed un-pruned plain key %q (no (( prune )) operator - only applyPruning removes it)", "plain")
	}

	// grab must already be resolved (evaluation ran before this
	// post-processor).
	if rec.observedData["grabme"] != "value" {
		t.Errorf("post-processor observed unevaluated grab: %#v", rec.observedData["grabme"])
	}

	finalData := result.RawData().(map[string]interface{})
	if _, present := finalData["prune"]; present {
		t.Errorf("final result still has pruned key %q", "prune")
	}
	if _, present := finalData["plain"]; present {
		t.Errorf("final result still has pruned key %q", "plain")
	}
}

func TestWithPostProcessors_CherryPickRunsBeforePostProcessor(t *testing.T) {
	rec := &recordingProcessor{name: "recorder", phase: PhaseNormal}

	engine := NewDefaultEngine()
	base := NewDocument(map[string]interface{}{
		"keep": map[string]interface{}{"a": 1},
		"drop": map[string]interface{}{"b": 2},
	})

	_, err := engine.Merge(context.Background(), base).
		WithCherryPick("keep").
		WithPostProcessors(rec).
		Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if _, present := rec.observedData["drop"]; present {
		t.Errorf("post-processor observed key removed by cherry-pick: %#v", rec.observedData)
	}
	if _, present := rec.observedData["keep"]; !present {
		t.Errorf("post-processor missing cherry-picked key: %#v", rec.observedData)
	}
}

func TestWithPostProcessors_PhaseThenPriorityOrdering(t *testing.T) {
	var order []string

	record := func(name string) *recordingProcessor {
		return &recordingProcessor{
			name: name,
			mutate: func(doc Document) Document {
				order = append(order, name)
				return doc
			},
		}
	}

	late := record("late")
	late.phase = PhaseLate

	early := record("early")
	early.phase = PhaseEarly

	normalHigh := record("normal-high-priority-number-runs-second")
	normalHigh.phase = PhaseNormal
	hp := 10
	normalHigh.priority = &hp

	normalLow := record("normal-low-priority-number-runs-first")
	normalLow.phase = PhaseNormal
	lp := 1
	normalLow.priority = &lp

	engine := NewDefaultEngine()
	base := NewDocument(map[string]interface{}{"value": "x"})

	_, err := engine.Merge(context.Background(), base).
		WithPostProcessors(late, early, normalHigh, normalLow).
		Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	want := []string{"early", "normal-low-priority-number-runs-first", "normal-high-priority-number-runs-second", "late"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

func TestWithPostProcessors_DefaultPriorityFallback(t *testing.T) {
	var ranA, ranB bool
	a := &minimalProcessor{name: "a", phase: PhaseNormal, ran: &ranA}
	b := &minimalProcessor{name: "b", phase: PhaseNormal, ran: &ranB}

	engine := NewDefaultEngine()
	base := NewDocument(map[string]interface{}{"value": "x"})

	_, err := engine.Merge(context.Background(), base).
		WithPostProcessors(a, b).
		Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !ranA || !ranB {
		t.Errorf("expected both default-priority processors to run: ranA=%v ranB=%v", ranA, ranB)
	}
}

func TestWithPostProcessors_ErrorPropagation(t *testing.T) {
	boom := errors.New("boom")
	failing := &recordingProcessor{name: "failing-proc", phase: PhaseNormal, returnErr: boom}
	neverRuns := &recordingProcessor{name: "never-runs", phase: PhaseLate}

	engine := NewDefaultEngine()
	base := NewDocument(map[string]interface{}{"value": "x"})

	_, err := engine.Merge(context.Background(), base).
		WithPostProcessors(failing, neverRuns).
		Execute()

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want wrapping %v", err, boom)
	}
	wantMsg := `post-processor "failing-proc" failed: boom`
	if err.Error() != wantMsg {
		t.Errorf("error message = %q, want %q", err.Error(), wantMsg)
	}
	if neverRuns.observedData != nil {
		t.Errorf("later post-processor ran after an earlier one failed")
	}
}

func TestWithPostProcessors_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	neverRuns := &recordingProcessor{name: "never-runs", phase: PhaseNormal}

	engine := NewDefaultEngine()
	base := NewDocument(map[string]interface{}{"value": "x"})

	_, err := engine.Merge(ctx, base).
		WithPostProcessors(neverRuns).
		Execute()

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestWithPostProcessors_EngineLevelAndBuilderLevelCombine(t *testing.T) {
	engineProc := &recordingProcessor{name: "engine-proc", phase: PhaseEarly}
	builderProc := &recordingProcessor{name: "builder-proc", phase: PhaseLate}

	engine, err := NewEngine(WithPostProcessors(engineProc))
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	base := NewDocument(map[string]interface{}{"value": "x"})
	_, err = engine.Merge(context.Background(), base).
		WithPostProcessors(builderProc).
		Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if engineProc.observedData == nil {
		t.Error("engine-level post-processor never ran")
	}
	if builderProc.observedData == nil {
		t.Error("builder-level post-processor never ran")
	}
}

func TestWithPostProcessors_MetadataFields(t *testing.T) {
	rec := &recordingProcessor{name: "recorder", phase: PhaseNormal}

	engine := NewDefaultEngine()
	base := NewDocument(map[string]interface{}{"value": "x"})
	overlay := NewDocument(map[string]interface{}{"other": "y"})

	_, err := engine.Merge(context.Background(), base, overlay).
		WithPostProcessors(rec).
		Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if rec.observedMeta == nil {
		t.Fatal("post-processor received nil metadata")
	}
	if rec.observedMeta.MergeCount != 2 {
		t.Errorf("MergeCount = %d, want 2", rec.observedMeta.MergeCount)
	}
	if rec.observedMeta.StartTime.IsZero() {
		t.Error("StartTime is zero")
	}
	if rec.observedMeta.Duration <= 0 {
		t.Error("Duration is not positive")
	}
}

// TestWithPostProcessors_DurationRefreshedPerProcessor pins phase2-review
// finding F6: ProcessMetadata.Duration's doc comment promises it is
// refreshed before each processor in the pipeline runs, so two processors
// in the same Execute() call observe different (growing) values rather
// than a single pipeline-wide snapshot. rec1 sleeps after recording its
// own observedDuration, guaranteeing measurable elapsed time before rec2
// runs and the adapter refreshes meta.Duration again.
func TestWithPostProcessors_DurationRefreshedPerProcessor(t *testing.T) {
	rec1 := &recordingProcessor{name: "first", phase: PhaseEarly, sleep: 5 * time.Millisecond}
	rec2 := &recordingProcessor{name: "second", phase: PhaseNormal}

	engine := NewDefaultEngine()
	base := NewDocument(map[string]interface{}{"value": "x"})

	_, err := engine.Merge(context.Background(), base).
		WithPostProcessors(rec1, rec2).
		Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if rec1.observedMeta == nil || rec2.observedMeta == nil {
		t.Fatal("expected both processors to run")
	}
	if rec2.observedDuration <= rec1.observedDuration {
		t.Errorf("expected the second processor to observe a strictly greater Duration than the first (refreshed per processor, not a single snapshot); first=%v second=%v", rec1.observedDuration, rec2.observedDuration)
	}
}

// TestWithPostProcessors_EvalDurationReflectsSkipEvaluation pins the other
// half of phase2-review finding F6: ProcessMetadata.EvalDuration must be
// exactly zero when SkipEvaluation() is set (evaluation did not run) and
// positive otherwise.
func TestWithPostProcessors_EvalDurationReflectsSkipEvaluation(t *testing.T) {
	engine := NewDefaultEngine()

	withEval := &recordingProcessor{name: "with-eval", phase: PhaseNormal}
	base := NewDocument(map[string]interface{}{"a": "(( grab b ))", "b": "value"})
	if _, err := engine.Merge(context.Background(), base).
		WithPostProcessors(withEval).
		Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if withEval.observedMeta == nil {
		t.Fatal("with-eval processor never ran")
	}
	if withEval.observedMeta.EvalDuration <= 0 {
		t.Errorf("expected positive EvalDuration when evaluation runs, got %v", withEval.observedMeta.EvalDuration)
	}

	skipEval := &recordingProcessor{name: "skip-eval", phase: PhaseNormal}
	skipBase := NewDocument(map[string]interface{}{"a": "(( grab b ))", "b": "value"})
	if _, err := engine.Merge(context.Background(), skipBase).
		SkipEvaluation().
		WithPostProcessors(skipEval).
		Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if skipEval.observedMeta == nil {
		t.Fatal("skip-eval processor never ran")
	}
	if skipEval.observedMeta.EvalDuration != 0 {
		t.Errorf("expected EvalDuration == 0 under SkipEvaluation, got %v", skipEval.observedMeta.EvalDuration)
	}
}

func TestWithPostProcessors_ZeroProcessorsNoOverhead(t *testing.T) {
	// Documents an explicit contract check (not a strict benchmark): a
	// merge builder with no post-processors registered must not error and
	// must not construct a pipeline at all. Covered functionally here;
	// BenchmarkExecute_NoPostProcessors below is the timing evidence.
	engine := NewDefaultEngine()
	base := NewDocument(map[string]interface{}{"value": "x"})

	result, err := engine.Merge(context.Background(), base).Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.String("value") != "x" {
		t.Errorf("value = %q, want %q", result.String("value"), "x")
	}
}

// TestWithPostProcessors_RunOnEmptyMerge pins phase2-review finding F2:
// registered post-processors must run on a zero-document merge
// (engine.Merge(ctx).Execute(), no Base/Overlay/OverlayFile calls), not be
// silently skipped. WithPostProcessors' doc comment, EngineOptions.
// PostProcessors' doc comment, and options.md all promise processors run
// "on every merge this engine executes", with no documented carve-out for
// an empty document list.
func TestWithPostProcessors_RunOnEmptyMerge(t *testing.T) {
	rec := &recordingProcessor{name: "recorder", phase: PhaseNormal}

	engine := NewDefaultEngine()

	result, err := engine.Merge(context.Background()).
		WithPostProcessors(rec).
		Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if rec.observedData == nil {
		t.Fatal("post-processor never ran on a zero-document merge")
	}
	if len(rec.observedData) != 0 {
		t.Errorf("expected the post-processor to observe an empty document, got %#v", rec.observedData)
	}
	if result == nil {
		t.Fatal("Execute() returned a nil Document")
	}
	if data, ok := result.RawData().(map[string]interface{}); !ok || len(data) != 0 {
		t.Errorf("expected an empty result document, got %#v", result.RawData())
	}
}

func TestSecurityRedactor(t *testing.T) {
	engine := NewDefaultEngine()
	base := NewDocument(map[string]interface{}{
		"database": map[string]interface{}{
			"host":     "db.example.com",
			"password": "hunter2",
		},
		"api_key": "abc123",
		"safe":    "visible",
	})

	result, err := engine.Merge(context.Background(), base).
		WithPostProcessors(NewSecurityRedactor([]string{"password", "api_key"}, "***REDACTED***")).
		Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if result.String("database.password") != "***REDACTED***" {
		t.Errorf("database.password = %q, want redacted", result.String("database.password"))
	}
	if result.String("api_key") != "***REDACTED***" {
		t.Errorf("api_key = %q, want redacted", result.String("api_key"))
	}
	if result.String("database.host") != "db.example.com" {
		t.Errorf("database.host = %q, want unchanged", result.String("database.host"))
	}
	if result.String("safe") != "visible" {
		t.Errorf("safe = %q, want unchanged", result.String("safe"))
	}
}

func TestSecurityRedactor_DefaultMask(t *testing.T) {
	engine := NewDefaultEngine()
	base := NewDocument(map[string]interface{}{"secret": "shh"})

	result, err := engine.Merge(context.Background(), base).
		WithPostProcessors(NewSecurityRedactor([]string{"secret"}, "")).
		Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.String("secret") != "***REDACTED***" {
		t.Errorf("secret = %q, want default mask", result.String("secret"))
	}
}

func TestSecurityRedactor_InvalidRegexFallsBackToLiteralMatch(t *testing.T) {
	engine := NewDefaultEngine()
	// "key[unclosed" is not a valid regular expression (unbalanced
	// bracket expression); NewSecurityRedactor must not panic
	// constructing it, and must still match the literal key via
	// regexp.QuoteMeta. RawData() (not the path-based accessors) is used
	// to sidestep tree.ParseCursor's own bracket-syntax handling, which
	// is unrelated to what is being tested here.
	base := NewDocument(map[string]interface{}{
		"key[unclosed": "sensitive",
		"other":        "visible",
	})

	result, err := engine.Merge(context.Background(), base).
		WithPostProcessors(NewSecurityRedactor([]string{"key[unclosed"}, "***REDACTED***")).
		Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	data, ok := result.RawData().(map[string]interface{})
	if !ok {
		t.Fatalf("RawData() is not a map: %#v", result.RawData())
	}
	if data["key[unclosed"] != "***REDACTED***" {
		t.Errorf(`data["key[unclosed"] = %#v, want redacted via literal fallback`, data["key[unclosed"])
	}
	if data["other"] != "visible" {
		t.Errorf(`data["other"] = %#v, want unchanged`, data["other"])
	}
}

func TestNewPruner(t *testing.T) {
	engine := NewDefaultEngine()
	base := NewDocument(map[string]interface{}{
		"keep":     "a",
		"internal": "b",
	})

	result, err := engine.Merge(context.Background(), base).
		WithPostProcessors(NewPruner("internal")).
		Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Has("internal") {
		t.Error("internal key still present after NewPruner")
	}
	if !result.Has("keep") {
		t.Error("keep key missing after NewPruner")
	}
}

func TestNewCherryPicker(t *testing.T) {
	engine := NewDefaultEngine()
	base := NewDocument(map[string]interface{}{
		"keep": "a",
		"drop": "b",
	})

	result, err := engine.Merge(context.Background(), base).
		WithPostProcessors(NewCherryPicker("keep")).
		Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Has("drop") {
		t.Error("drop key still present after NewCherryPicker")
	}
	if !result.Has("keep") {
		t.Error("keep key missing after NewCherryPicker")
	}
}

func TestNewKeySorter(t *testing.T) {
	engine := NewDefaultEngine()
	base := NewDocument(map[string]interface{}{
		"zebra": 1,
		"alpha": 2,
	})

	result, err := engine.Merge(context.Background(), base).
		WithPostProcessors(NewKeySorter(true)).
		Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	// SortKeys' contract is documented on the *document implementation;
	// here we only need the document to still resolve both keys - the
	// sort must not lose data.
	if !result.Has("zebra") || !result.Has("alpha") {
		t.Errorf("keys lost after NewKeySorter: %#v", result.RawData())
	}
}

func TestNewKeySorter_Disabled(t *testing.T) {
	engine := NewDefaultEngine()
	base := NewDocument(map[string]interface{}{"a": 1})

	result, err := engine.Merge(context.Background(), base).
		WithPostProcessors(NewKeySorter(false)).
		Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Int("a") != 1 {
		t.Errorf("a = %d, want 1", result.Int("a"))
	}
}

func TestWithPostProcessors_NilProcessorIgnored(t *testing.T) {
	engine := NewDefaultEngine()
	base := NewDocument(map[string]interface{}{"value": "x"})

	result, err := engine.Merge(context.Background(), base).
		WithPostProcessors(nil).
		Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.String("value") != "x" {
		t.Errorf("value = %q, want %q", result.String("value"), "x")
	}
}

func BenchmarkExecute_NoPostProcessors(b *testing.B) {
	engine := NewDefaultEngine()
	base := NewDocument(map[string]interface{}{"value": "x", "nested": map[string]interface{}{"a": 1, "b": 2}})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := engine.Merge(context.Background(), base.Clone()).Execute(); err != nil {
			b.Fatalf("Execute() error = %v", err)
		}
	}
}

func BenchmarkExecute_WithPostProcessors(b *testing.B) {
	engine := NewDefaultEngine()
	base := NewDocument(map[string]interface{}{"value": "x", "nested": map[string]interface{}{"a": 1, "b": 2}})
	proc := NewKeySorter(true)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := engine.Merge(context.Background(), base.Clone()).WithPostProcessors(proc).Execute(); err != nil {
			b.Fatalf("Execute() error = %v", err)
		}
	}
}

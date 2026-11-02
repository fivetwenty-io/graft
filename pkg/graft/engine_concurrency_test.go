package graft_test

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
	_ "github.com/fivetwenty-io/graft/pkg/graft/operators"
	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// These tests share one Engine across goroutines and require every
// result to equal the result the same call produces on a fresh engine.
// Each test computes sharedGoroutines*sharedCallsPerGoroutine serial
// results first, then makes the same calls concurrently on one engine.

const (
	sharedGoroutines        = 16
	sharedCallsPerGoroutine = 10
)

// sharedMergeBase exercises static_ips, (( prune )), (( sort )) on a
// list the overlay re-declares, grab, concat, and join.
const sharedMergeBase = `
meta:
  secret: shh
  ports: [3, 1, 2]
name: (( concat "svc-" params.instance_id ))
params:
  instance_id: placeholder
networks:
- name: net1
  type: manual
  subnets:
  - range: 10.0.0.0/24
    gateway: 10.0.0.1
    az: z1
    static: [10.0.0.10 - 10.0.0.20]
instance_groups:
- name: g1
  instances: 2
  azs: [z1]
  vm_type: (( grab meta.vm ))
  networks:
  - name: net1
    static_ips: (( static_ips 0 1 ))
credentials:
  host: (( grab params.instance_id ))
  list: (( join "," meta.items ))
`

// sharedVaultBase adds vault calls to sharedMergeBase. creds.derived
// builds its path from creds.password, so under skip-vault redaction it
// reads the vault placeholder creds.password recorded.
const sharedVaultBase = sharedMergeBase + `
creds:
  password: (( vault "secret/" params.instance_id ":password" ))
  derived: (( vault creds.password "/sub:key" ))
`

func sharedMergeOverlay(id string) string {
	return fmt.Sprintf(`
meta:
  vm: vm-%[1]s
  items: [a, b, %[1]s]
  secret: (( prune ))
  ports: (( sort ))
params:
  instance_id: %[1]s
`, id)
}

// sharedEvalDoc is the document each Evaluate call parses for itself.
const sharedEvalDoc = `
meta:
  vm: vm-%[1]s
  items: [a, b, %[1]s]
  gone: (( prune ))
name: (( concat "svc-" meta.vm ))
list: (( join "," meta.items ))
vm: (( grab meta.vm ))
networks:
- name: net1
  subnets:
  - range: 10.0.0.0/24
    gateway: 10.0.0.1
    az: z1
    static: [10.0.0.10 - 10.0.0.20]
instance_groups:
- name: g1
  instances: 2
  azs: [z1]
  networks:
  - name: net1
    static_ips: (( static_ips 0 1 ))
`

func sharedIDs() []string {
	ids := make([]string, 0, sharedGoroutines*sharedCallsPerGoroutine)
	for g := 0; g < sharedGoroutines; g++ {
		for i := 0; i < sharedCallsPerGoroutine; i++ {
			ids = append(ids, fmt.Sprintf("g%di%d", g, i))
		}
	}
	return ids
}

func newSharedTestEngine(t *testing.T, opts ...graft.EngineOption) graft.Engine {
	t.Helper()
	e, err := graft.NewEngine(append([]graft.EngineOption{graft.WithCacheDisabled()}, opts...)...)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return e
}

// sharedMergeText merges baseText and id's overlay on e and renders the
// result, or the error text when the merge fails.
func sharedMergeText(e graft.Engine, baseText, id string) string {
	base, err := e.ParseYAML([]byte(baseText))
	if err != nil {
		return "parse base: " + err.Error()
	}
	overlay, err := e.ParseYAML([]byte(sharedMergeOverlay(id)))
	if err != nil {
		return "parse overlay: " + err.Error()
	}
	res, err := e.Merge(context.Background(), base, overlay).Execute()
	if err != nil {
		return "merge: " + err.Error()
	}
	out, err := res.ToYAML()
	if err != nil {
		return "render: " + err.Error()
	}
	return string(out)
}

func sharedEvalText(e graft.Engine, id string) string {
	doc, err := e.ParseYAML([]byte(fmt.Sprintf(sharedEvalDoc, id)))
	if err != nil {
		return "parse: " + err.Error()
	}
	res, err := e.Evaluate(context.Background(), doc)
	if err != nil {
		return "evaluate: " + err.Error()
	}
	out, err := res.ToYAML()
	if err != nil {
		return "render: " + err.Error()
	}
	return string(out)
}

// checkSharedMerge guards against a serial result that is itself wrong,
// which would make the serial-versus-shared comparison meaningless.
func checkSharedMerge(id, out string) error {
	wants := []string{
		"name: svc-" + id,
		"host: " + id,
		"vm_type: vm-" + id,
		"list: a,b," + id,
		"- 10.0.0.10",
		"- 10.0.0.11",
		"ports:\n  - 1\n  - 2\n  - 3",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			return fmt.Errorf("missing %q", want)
		}
	}
	if strings.Contains(out, "secret:") {
		return fmt.Errorf("meta.secret was not pruned")
	}
	return nil
}

func checkSharedEval(id, out string) error {
	for _, want := range []string{"name: svc-vm-" + id, "list: a,b," + id, "vm: vm-" + id, "- 10.0.0.10", "- 10.0.0.11"} {
		if !strings.Contains(out, want) {
			return fmt.Errorf("missing %q", want)
		}
	}
	if strings.Contains(out, "gone:") {
		return fmt.Errorf("meta.gone was not pruned")
	}
	return nil
}

// serialResults runs call once per id, each on a fresh engine from
// newEngine, and checks each result with check when check is non-nil.
func serialResults(t *testing.T, newEngine func() graft.Engine, call func(e graft.Engine, id string) string, check func(id, out string) error) map[string]string {
	t.Helper()
	want := make(map[string]string)
	for _, id := range sharedIDs() {
		out := call(newEngine(), id)
		if check != nil {
			if err := check(id, out); err != nil {
				t.Fatalf("serial result for %s is wrong: %v\n%s", id, err, out)
			}
		}
		want[id] = out
	}
	return want
}

// runOnSharedEngine makes every call from sharedGoroutines goroutines on
// e and returns each id's result.
func runOnSharedEngine(e graft.Engine, call func(e graft.Engine, id string) string) map[string]string {
	got := make(map[string]string)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for g := 0; g < sharedGoroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < sharedCallsPerGoroutine; i++ {
				id := fmt.Sprintf("g%di%d", g, i)
				out := call(e, id)
				mu.Lock()
				got[id] = out
				mu.Unlock()
			}
		}(g)
	}
	wg.Wait()
	return got
}

// requireSameResults fails the test with the number of ids whose shared
// result differs from the serial one, and shows the first difference.
func requireSameResults(t *testing.T, name string, want, got map[string]string) {
	t.Helper()
	var bad []string
	for _, id := range sharedIDs() {
		if got[id] != want[id] {
			bad = append(bad, id)
		}
	}
	if len(bad) == 0 {
		return
	}
	first := bad[0]
	t.Errorf("%s: %d of %d shared-engine results differ from serial; first is %s\n--- serial\n%s\n--- shared\n%s",
		name, len(bad), len(want), first, want[first], got[first])
}

func TestSharedEngineMergeMatchesSerial(t *testing.T) {
	cases := []struct {
		name string
		base string
		opts []graft.EngineOption
	}{
		{name: "default", base: sharedMergeBase},
		{
			name: "skip-vault-redact",
			base: sharedVaultBase,
			opts: []graft.EngineOption{graft.WithSkipVault(true), graft.WithRedact(true)},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			call := func(e graft.Engine, id string) string { return sharedMergeText(e, tc.base, id) }
			newEngine := func() graft.Engine { return newSharedTestEngine(t, tc.opts...) }
			want := serialResults(t, newEngine, call, checkSharedMerge)
			got := runOnSharedEngine(newEngine(), call)
			requireSameResults(t, "merge "+tc.name, want, got)
		})
	}
}

func TestSharedEngineEvaluateMatchesSerial(t *testing.T) {
	newEngine := func() graft.Engine { return newSharedTestEngine(t) }
	want := serialResults(t, newEngine, sharedEvalText, checkSharedEval)
	got := runOnSharedEngine(newEngine(), sharedEvalText)
	requireSameResults(t, "evaluate", want, got)
}

// checkSharedDiff requires a serial diff to be a real one: JSON that
// reports the values that differ between the "x" document and id's.
func checkSharedDiff(id, out string) error {
	if !strings.HasPrefix(strings.TrimSpace(out), "{") && !strings.HasPrefix(strings.TrimSpace(out), "[") {
		return fmt.Errorf("not a JSON diff")
	}
	for _, want := range []string{"vm-x", "vm-" + id} {
		if !strings.Contains(out, want) {
			return fmt.Errorf("missing %q", want)
		}
	}
	return nil
}

func TestSharedEngineDiffMatchesSerial(t *testing.T) {
	var mu sync.Mutex
	var mutated []string
	call := func(e graft.Engine, id string) string {
		a, err := e.ParseYAML([]byte(fmt.Sprintf(sharedEvalDoc, "x")))
		if err != nil {
			return "parse a: " + err.Error()
		}
		b, err := e.ParseYAML([]byte(fmt.Sprintf(sharedEvalDoc, id)))
		if err != nil {
			return "parse b: " + err.Error()
		}
		aBefore, errA := a.ToYAML()
		bBefore, errB := b.ToYAML()
		if errA != nil || errB != nil {
			return fmt.Sprintf("render before: %v, %v", errA, errB)
		}
		out, err := e.Diff(a, b).ToJSON()
		if err != nil {
			return "diff: " + err.Error()
		}
		aAfter, errA := a.ToYAML()
		bAfter, errB := b.ToYAML()
		if errA != nil || errB != nil {
			return fmt.Sprintf("render after: %v, %v", errA, errB)
		}
		if !bytes.Equal(aBefore, aAfter) || !bytes.Equal(bBefore, bAfter) {
			mu.Lock()
			mutated = append(mutated, id)
			mu.Unlock()
		}
		return string(out)
	}
	newEngine := func() graft.Engine { return newSharedTestEngine(t) }
	want := serialResults(t, newEngine, call, checkSharedDiff)
	got := runOnSharedEngine(newEngine(), call)
	requireSameResults(t, "diff", want, got)
	if len(mutated) > 0 {
		t.Errorf("Diff changed the documents it compared for %d calls, first %s", len(mutated), mutated[0])
	}
}

func TestSharedEngineRedact(t *testing.T) {
	t.Setenv("REDACT", "1")

	call := func(e graft.Engine, id string) string { return sharedMergeText(e, sharedVaultBase, id) }
	newEngine := func() graft.Engine { return newSharedTestEngine(t) }
	want := serialResults(t, newEngine, call, checkSharedMerge)
	for id, out := range want {
		if !strings.Contains(out, "password: REDACTED") {
			t.Fatalf("serial result for %s was not redacted:\n%s", id, out)
		}
	}

	shared := newEngine()
	got := runOnSharedEngine(shared, call)
	requireSameResults(t, "redact", want, got)

	state := shared.GetOperatorState()
	if state.IsVaultSkipped() || state.IsAWSSkipped() || state.IsNATSSkipped() || state.IsRedactMode() {
		t.Errorf("REDACT=1 changed the engine's own settings: skipVault=%v skipAws=%v skipNats=%v redact=%v",
			state.IsVaultSkipped(), state.IsAWSSkipped(), state.IsNATSSkipped(), state.IsRedactMode())
	}
}

// sharedPublishBase places each id's vault calls under a key named for
// the id, so every run records its placeholders at its own tree paths.
const sharedPublishBase = `
meta:
  secret: shh
params:
  instance_id: placeholder
`

func sharedPublishOverlay(id string) string {
	return fmt.Sprintf(`
meta:
  secret: (( prune ))
params:
  instance_id: %[1]s
creds_%[1]s:
  password: (( vault "secret/" params.instance_id ":password" ))
  derived: (( vault creds_%[1]s.password "/sub:key" ))
`, id)
}

type publishedOutputs struct {
	refs         map[string][]string
	deferred     []string
	placeholders map[string]string
	pruned       []string
}

func sharedPublishPaths(id string) []string {
	return []string{"creds_" + id + ".password", "creds_" + id + ".derived"}
}

func runPublishMerge(t *testing.T, e graft.Engine, id string) {
	t.Helper()
	base, err := e.ParseYAML([]byte(sharedPublishBase))
	if err != nil {
		t.Errorf("parse base: %v", err)
		return
	}
	overlay, err := e.ParseYAML([]byte(sharedPublishOverlay(id)))
	if err != nil {
		t.Errorf("parse overlay: %v", err)
		return
	}
	if _, err := e.Merge(context.Background(), base, overlay).Execute(); err != nil {
		t.Errorf("merge %s: %v", id, err)
	}
}

func collectPublished(e graft.Engine, ids []string) publishedOutputs {
	state := e.GetOperatorState()
	out := publishedOutputs{
		refs:         state.GetVaultRefs(),
		placeholders: make(map[string]string),
		pruned:       state.GetLastEvaluatedPrunedPaths(),
	}
	for k := range out.refs {
		sort.Strings(out.refs[k])
	}
	for _, d := range state.GetDeferredPaths() {
		out.deferred = append(out.deferred, d.Path+" "+d.Reason)
	}
	for _, id := range ids {
		for _, p := range sharedPublishPaths(id) {
			if key, ok := state.VaultPlaceholderFor(p); ok {
				out.placeholders[p] = key
			}
		}
	}
	return out
}

func TestSharedEngineOutputsPublished(t *testing.T) {
	cases := []struct {
		name string
		opts []graft.EngineOption
	}{
		// Redaction records vault references and placeholders.
		{name: "skip-vault-redact", opts: []graft.EngineOption{graft.WithSkipVault(true), graft.WithRedact(true)}},
		// Skipping without redaction defers the calls instead.
		{name: "skip-vault-defer", opts: []graft.EngineOption{graft.WithSkipVault(true)}},
	}
	ids := sharedIDs()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			union := publishedOutputs{refs: map[string][]string{}, placeholders: map[string]string{}}
			for _, id := range ids {
				e := newSharedTestEngine(t, tc.opts...)
				runPublishMerge(t, e, id)
				one := collectPublished(e, []string{id})
				for k, v := range one.refs {
					union.refs[k] = append(union.refs[k], v...)
				}
				union.deferred = append(union.deferred, one.deferred...)
				for k, v := range one.placeholders {
					union.placeholders[k] = v
				}
				if !slices.Equal(one.pruned, []string{"meta.secret"}) {
					t.Fatalf("serial run %s pruned %v, want [meta.secret]", id, one.pruned)
				}
			}
			for k := range union.refs {
				sort.Strings(union.refs[k])
			}
			sort.Strings(union.deferred)
			switch tc.name {
			case "skip-vault-redact":
				if len(union.refs) == 0 || len(union.placeholders) != 2*len(ids) {
					t.Fatalf("serial runs recorded %d vault references and %d placeholders, want both for every run",
						len(union.refs), len(union.placeholders))
				}
			case "skip-vault-defer":
				if len(union.deferred) != 2*len(ids) {
					t.Fatalf("serial runs deferred %d paths, want %d", len(union.deferred), 2*len(ids))
				}
			}

			shared := newSharedTestEngine(t, tc.opts...)
			runOnSharedEngine(shared, func(e graft.Engine, id string) string {
				runPublishMerge(t, e, id)
				return ""
			})
			got := collectPublished(shared, ids)
			sort.Strings(got.deferred)

			if len(got.refs) != len(union.refs) {
				t.Errorf("engine holds %d vault reference paths, serial runs recorded %d", len(got.refs), len(union.refs))
			}
			for k, v := range union.refs {
				if !slices.Equal(got.refs[k], v) {
					t.Errorf("vault reference %q: engine holds %v, serial runs recorded %v", k, got.refs[k], v)
				}
			}
			if !slices.Equal(got.deferred, union.deferred) {
				t.Errorf("engine holds %d deferred paths, serial runs recorded %d", len(got.deferred), len(union.deferred))
			}
			if len(got.placeholders) != len(union.placeholders) {
				t.Errorf("engine holds %d vault placeholders, serial runs recorded %d", len(got.placeholders), len(union.placeholders))
			}
			for k, v := range union.placeholders {
				if got.placeholders[k] != v {
					t.Errorf("vault placeholder %q: engine holds %q, serial runs recorded %q", k, got.placeholders[k], v)
				}
			}
			if !slices.Equal(got.pruned, []string{"meta.secret"}) {
				t.Errorf("engine reports last pruned paths %v, want [meta.secret]", got.pruned)
			}
		})
	}
}

// TestSharedEngineVaultPlaceholdersPerRun keeps the vault calls at the
// same tree paths, creds.password and creds.derived, in every run and
// varies only the vault path. creds.derived builds its path from the
// placeholder creds.password recorded, so a run that read another run's
// placeholder would record a vault reference no serial run records.
func TestSharedEngineVaultPlaceholdersPerRun(t *testing.T) {
	opts := []graft.EngineOption{graft.WithSkipVault(true), graft.WithRedact(true)}
	call := func(e graft.Engine, id string) string { return sharedMergeText(e, sharedVaultBase, id) }

	union := map[string][]string{}
	for _, id := range sharedIDs() {
		e := newSharedTestEngine(t, opts...)
		if err := checkSharedMerge(id, call(e, id)); err != nil {
			t.Fatalf("serial merge %s is wrong: %v", id, err)
		}
		for path, keys := range e.GetOperatorState().GetVaultRefs() {
			union[path] = append(union[path], keys...)
		}
	}
	derived := 0
	for path := range union {
		sort.Strings(union[path])
		if strings.Contains(path, "/sub:key") {
			derived++
		}
	}
	if derived != len(sharedIDs()) {
		t.Fatalf("serial runs recorded %d derived vault paths, want one per run (%d)", derived, len(sharedIDs()))
	}

	shared := newSharedTestEngine(t, opts...)
	runOnSharedEngine(shared, call)
	got := shared.GetOperatorState().GetVaultRefs()

	var extra []string
	for path := range got {
		if _, ok := union[path]; !ok {
			extra = append(extra, path)
		}
	}
	sort.Strings(extra)
	if len(extra) > 0 {
		t.Errorf("shared engine recorded %d vault paths no serial run recorded, first %q", len(extra), extra[0])
	}
	for path, keys := range union {
		sort.Strings(got[path])
		if !slices.Equal(got[path], keys) {
			t.Errorf("vault reference %q: shared engine holds %v, serial runs recorded %v", path, got[path], keys)
		}
	}
}

// evaluatingPostProcessor calls Evaluate on its engine with the context
// the merge hands it, once per document, all at once.
type evaluatingPostProcessor struct {
	engine graft.Engine
	docs   []string
	mu     *sync.Mutex
	got    map[string]string
}

func (p *evaluatingPostProcessor) Name() string                  { return "evaluating" }
func (p *evaluatingPostProcessor) Phase() graft.PostProcessPhase { return graft.PhaseNormal }

func (p *evaluatingPostProcessor) Process(ctx context.Context, doc graft.Document, _ *graft.ProcessMetadata) (graft.Document, error) {
	var wg sync.WaitGroup
	for _, id := range p.docs {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			out := "parse: "
			nested, err := p.engine.ParseYAML([]byte(fmt.Sprintf(sharedEvalDoc, id)))
			if err == nil {
				out = "evaluate: "
				var res graft.Document
				if res, err = p.engine.Evaluate(ctx, nested); err == nil {
					out = "render: "
					var text []byte
					if text, err = res.ToYAML(); err == nil {
						out = string(text)
					}
				}
			}
			if err != nil {
				out += err.Error()
			}
			p.mu.Lock()
			p.got[id] = out
			p.mu.Unlock()
		}(id)
	}
	wg.Wait()
	return doc, nil
}

// TestSharedEnginePostProcessorEvaluate covers a post-processor that
// calls Evaluate with the context it was given. That Evaluate is a run
// of its own: it must not continue the merge's finished run, publish
// that run's outputs a second time, or share its state with the other
// Evaluate calls the post-processors make at the same time.
func TestSharedEnginePostProcessorEvaluate(t *testing.T) {
	t.Run("deferred paths published once", func(t *testing.T) {
		e := newSharedTestEngine(t, graft.WithSkipVault(true))
		base, err := e.ParseYAML([]byte("creds:\n  password: (( vault \"secret/a:password\" ))\n"))
		if err != nil {
			t.Fatalf("parse base: %v", err)
		}
		pp := &evaluatingPostProcessor{engine: e, docs: []string{"nested"}, mu: &sync.Mutex{}, got: map[string]string{}}
		if _, err := e.Merge(context.Background(), base).WithPostProcessors(pp).Execute(); err != nil {
			t.Fatalf("merge: %v", err)
		}
		if err := checkSharedEval("nested", pp.got["nested"]); err != nil {
			t.Errorf("nested Evaluate is wrong: %v\n%s", err, pp.got["nested"])
		}
		var paths []string
		for _, d := range e.GetOperatorState().GetDeferredPaths() {
			paths = append(paths, d.Path)
		}
		if !slices.Equal(paths, []string{"creds.password"}) {
			t.Errorf("engine holds deferred paths %v, want [creds.password] once", paths)
		}
	})

	t.Run("concurrent nested runs", func(t *testing.T) {
		const merges, perMerge = 8, 4
		e := newSharedTestEngine(t)
		var mu sync.Mutex
		got := map[string]string{}
		var wg sync.WaitGroup
		for m := 0; m < merges; m++ {
			docs := make([]string, perMerge)
			for i := range docs {
				docs[i] = fmt.Sprintf("m%dn%d", m, i)
			}
			wg.Add(1)
			go func(m int) {
				defer wg.Done()
				base, err := e.ParseYAML([]byte(sharedMergeBase))
				if err != nil {
					t.Errorf("parse base: %v", err)
					return
				}
				id := fmt.Sprintf("m%d", m)
				overlay, err := e.ParseYAML([]byte(sharedMergeOverlay(id)))
				if err != nil {
					t.Errorf("parse overlay: %v", err)
					return
				}
				pp := &evaluatingPostProcessor{engine: e, docs: docs, mu: &mu, got: got}
				res, err := e.Merge(context.Background(), base, overlay).WithPostProcessors(pp).Execute()
				if err != nil {
					t.Errorf("merge %s: %v", id, err)
					return
				}
				out, err := res.ToYAML()
				if err != nil {
					t.Errorf("render %s: %v", id, err)
					return
				}
				if err := checkSharedMerge(id, string(out)); err != nil {
					t.Errorf("merge %s is wrong: %v\n%s", id, err, out)
				}
			}(m)
		}
		wg.Wait()
		if len(got) != merges*perMerge {
			t.Fatalf("post-processors made %d nested Evaluate calls, want %d", len(got), merges*perMerge)
		}
		bad := 0
		for id, out := range got {
			if err := checkSharedEval(id, out); err != nil {
				if bad == 0 {
					t.Errorf("nested Evaluate %s is wrong: %v\n%s", id, err, out)
				}
				bad++
			}
		}
		if bad > 0 {
			t.Errorf("%d of %d nested Evaluate calls were wrong", bad, len(got))
		}
	})
}

// stateAssertingPrune queues its own path for pruning through an
// assertion of GetEngine(ev) to graft.OperatorState, the way a
// third-party operator written against the Engine it is handed might.
type stateAssertingPrune struct{}

func (stateAssertingPrune) Setup() error               { return nil }
func (stateAssertingPrune) Phase() graft.OperatorPhase { return graft.EvalPhase }
func (stateAssertingPrune) Dependencies(_ *graft.Evaluator, _ []*graft.Expr, _, auto []*tree.Cursor) []*tree.Cursor {
	return auto
}

func (stateAssertingPrune) Run(ev *graft.Evaluator, _ []*graft.Expr) (*graft.Response, error) {
	state, ok := graft.GetEngine(ev).(graft.OperatorState)
	if !ok {
		return nil, fmt.Errorf("engine %T is not an OperatorState", graft.GetEngine(ev))
	}
	state.AddKeyToPrune(ev.Here.Quoted())
	return &graft.Response{Type: graft.Replace, Value: "unpruned"}, nil
}

// TestSharedEngineOperatorStateAssertion requires an operator that
// asserts its engine to graft.OperatorState to reach the run's state:
// the key it queues is pruned from that run's result and leaves no
// marker on the engine.
func TestSharedEngineOperatorStateAssertion(t *testing.T) {
	e := newSharedTestEngine(t)
	if err := e.RegisterOperator("assert_prune", stateAssertingPrune{}); err != nil {
		t.Fatalf("RegisterOperator: %v", err)
	}
	doc, err := e.ParseYAML([]byte("gone: (( assert_prune ))\nkept: 1\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	res, err := e.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	out, err := res.ToYAML()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(string(out), "gone:") || !strings.Contains(string(out), "kept: 1") {
		t.Errorf("the key the operator queued was not pruned from the run's result:\n%s", out)
	}
	if keys := e.GetOperatorState().GetKeysToPrune(); len(keys) > 0 {
		t.Errorf("the operator's prune marker reached the engine's own state: %v", keys)
	}
}

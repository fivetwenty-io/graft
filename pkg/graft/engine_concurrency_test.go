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
		aBefore, _ := a.ToYAML()
		bBefore, _ := b.ToYAML()
		out, err := e.Diff(a, b).ToJSON()
		if err != nil {
			return "diff: " + err.Error()
		}
		aAfter, _ := a.ToYAML()
		bAfter, _ := b.ToYAML()
		if !bytes.Equal(aBefore, aAfter) || !bytes.Equal(bBefore, bAfter) {
			mu.Lock()
			mutated = append(mutated, id)
			mu.Unlock()
		}
		return string(out)
	}
	newEngine := func() graft.Engine { return newSharedTestEngine(t) }
	want := serialResults(t, newEngine, call, nil)
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

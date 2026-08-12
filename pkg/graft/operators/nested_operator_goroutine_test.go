package operators

import (
	"runtime"
	"testing"

	graft "github.com/fivetwenty-io/graft/pkg/graft"
)

// TestNestedOperatorResolution_NilEngineDoesNotLeakGoroutines is the F4
// regression guard: evaluateNestedOperator's operator lookup must resolve a
// nil-engine Evaluator's operators via graft.EngineOf (returns nil, no
// allocation), not graft.GetEngine (constructs a full CreateDefaultEngine()
// — with an unstoppable cache-cleanup goroutine — on every call when
// ev.engine is nil). This path runs once per nested operator call
// (ResolveOperatorArgument's OperatorCall case), so using GetEngine there
// leaked one goroutine per call; the review measured exactly 200 leaked
// goroutines over 200 iterations before this fix, 0 after.
//
// This calls ResolveOperatorArgument directly, in a tight loop, reusing one
// Evaluator and one already-parsed nested Expr across every iteration —
// deliberately not going through Evaluator.Run or DataFlow, both of which
// have their own, pre-existing (not part of this fix's scope, per the
// review's F4 root-cause note) GetEngine call sites that would otherwise
// dominate the measurement and make it impossible to isolate whether *this*
// site leaks.
//
// Tolerance: goroutine counts are not perfectly quiet even at rest (GC,
// finalizers, the test binary's own background work can transiently add or
// remove a handful), so this asserts growth stays small and bounded, not
// exactly zero. The leak this guards against is linear in iteration count
// (one goroutine per lookup) and N=300 makes that shape unmistakable next
// to any constant-size scheduling noise: a real regression would show up
// as growth on the order of N, not a handful.
func TestNestedOperatorResolution_NilEngineDoesNotLeakGoroutines(t *testing.T) {
	const iterations = 300
	const tolerance = 20 // generous headroom for scheduler/GC noise, still far below a per-iteration leak

	ev := &graft.Evaluator{Tree: map[string]interface{}{"x": "y"}}
	opcall, err := graft.ParseOpcallWithParser(graft.EvalPhase, `(( grab x ))`)
	if err != nil {
		t.Fatalf("ParseOpcallWithParser: %v", err)
	}
	nested := &graft.Expr{Type: graft.OperatorCall, Operator: "grab", Call: opcall}

	// Warm-up iteration so any one-time package-level lazy init inside the
	// resolution path isn't mistaken for a per-call leak, then let any
	// steady-state background goroutines (GC workers, etc.) settle before
	// the baseline is captured.
	if _, err := ResolveOperatorArgument(ev, nested); err != nil {
		t.Fatalf("ResolveOperatorArgument warm-up: %v", err)
	}
	runtime.GC()
	runtime.Gosched()
	before := runtime.NumGoroutine()

	for i := 0; i < iterations; i++ {
		val, err := ResolveOperatorArgument(ev, nested)
		if err != nil {
			t.Fatalf("ResolveOperatorArgument: %v", err)
		}
		if val != "y" {
			t.Fatalf("ResolveOperatorArgument = %#v, want %q", val, "y")
		}
	}

	runtime.GC()
	runtime.Gosched()
	after := runtime.NumGoroutine()

	delta := after - before
	if delta > tolerance {
		t.Fatalf("runtime.NumGoroutine() grew by %d over %d nested-operator resolutions (before=%d, after=%d), want growth <= %d — this is the shape of a per-lookup goroutine leak (e.g. graft.GetEngine materializing a default engine's cache-cleanup loop on every nil-engine lookup)", delta, iterations, before, after, tolerance)
	}
}

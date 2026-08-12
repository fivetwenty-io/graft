package operators

import (
	"runtime"
	"testing"
	"time"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// TestResolveCustomBackend_EngineOfDiffersFromGetEngine pins the actual
// behavioral difference between graft.EngineOf and graft.GetEngine that
// resolveCustomBackend's choice of EngineOf depends on (see EngineOf's own
// doc comment, and c7-notes.md's "GetEngine hazard" section, corrected by
// phase3-review.md M8). With ev.engine == nil, graft.GetEngine constructs
// (and discards) a full default *DefaultEngine - complete with a
// background cache cleanup goroutine (internal/cache's cleanupLoop,
// started unconditionally by cache.NewCache's default 1-minute
// CleanupInterval, since CreateDefaultEngine's default configuration has
// caching enabled) - on every call, while graft.EngineOf never allocates
// anything. Calling resolveCustomBackend many times against a nil-engine
// Evaluator must not grow the goroutine count; the mutation this test
// targets (swap resolveCustomBackend's graft.EngineOf(ev) to
// graft.GetEngine(ev)) does, one leaked goroutine per call.
func TestResolveCustomBackend_EngineOfDiffersFromGetEngine(t *testing.T) {
	ev := &graft.Evaluator{Tree: map[string]interface{}{}}
	if graft.EngineOf(ev) != nil {
		t.Fatalf("test precondition failed: ev.engine must be nil")
	}

	// Let goroutines already in flight from earlier tests/package init
	// settle before sampling a baseline.
	runtime.GC()
	time.Sleep(10 * time.Millisecond)
	before := runtime.NumGoroutine()

	const calls = 50
	for i := 0; i < calls; i++ {
		if _, ok := resolveCustomBackend(ev, "vault"); ok {
			t.Fatalf("call %d: resolveCustomBackend unexpectedly found a backend against a nil-engine Evaluator", i)
		}
	}

	runtime.GC()
	time.Sleep(10 * time.Millisecond)
	after := runtime.NumGoroutine()

	// Allow slack for unrelated background goroutines (test framework, GC)
	// started or stopped incidentally during the loop; the mutation this
	// test targets leaks one goroutine per call (50 here), which
	// comfortably clears any such slack.
	const slack = 5
	if after > before+slack {
		t.Fatalf("goroutine count grew from %d to %d over %d resolveCustomBackend calls (want growth <= %d) - resolveCustomBackend is materializing a throwaway engine per call instead of using graft.EngineOf", before, after, calls, slack)
	}
}

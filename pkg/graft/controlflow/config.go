package controlflow

import (
	"os"
	"strconv"
	"sync"
)

// DefaultMaxLoopIterations is the (( while )) iteration cap used when
// neither --max-loop-iterations nor GRAFT_MAX_LOOP_ITERATIONS override it,
// matching docs/user-guide/operators/control-flow.md's documented default.
const DefaultMaxLoopIterations = 1000

// maxLoopIterationsEnvVar is the environment variable that overrides
// DefaultMaxLoopIterations. The CLI --max-loop-iterations flag overrides
// both by calling SetMaxLoopIterations directly.
const maxLoopIterationsEnvVar = "GRAFT_MAX_LOOP_ITERATIONS"

var (
	maxLoopIterationsMu  sync.RWMutex
	maxLoopIterationsSet bool
	maxLoopIterationsVal int
)

// SetMaxLoopIterations overrides the (( while )) iteration cap for every
// subsequent Expand call in this process. Intended for the CLI's
// --max-loop-iterations flag. A non-positive value is ignored (the
// configured or default cap remains in effect) since 0 or negative
// iteration limits have no sensible interpretation.
func SetMaxLoopIterations(n int) {
	if n <= 0 {
		return
	}
	maxLoopIterationsMu.Lock()
	defer maxLoopIterationsMu.Unlock()
	maxLoopIterationsSet = true
	maxLoopIterationsVal = n
}

// MaxLoopIterations returns the currently effective (( while )) iteration
// cap: the value set via SetMaxLoopIterations if one was set, otherwise the
// value of GRAFT_MAX_LOOP_ITERATIONS if it parses as a positive integer,
// otherwise DefaultMaxLoopIterations.
func MaxLoopIterations() int {
	maxLoopIterationsMu.RLock()
	if maxLoopIterationsSet {
		defer maxLoopIterationsMu.RUnlock()
		return maxLoopIterationsVal
	}
	maxLoopIterationsMu.RUnlock()

	if raw := os.Getenv(maxLoopIterationsEnvVar); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	return DefaultMaxLoopIterations
}

// resetMaxLoopIterationsForTest clears any override set via
// SetMaxLoopIterations. Test-only helper (unexported, used from _test.go
// files in this package).
func resetMaxLoopIterationsForTest() {
	maxLoopIterationsMu.Lock()
	defer maxLoopIterationsMu.Unlock()
	maxLoopIterationsSet = false
	maxLoopIterationsVal = 0
}

package graft

import (
	"sync"
	"testing"
)

// TestSilenceWarningsIsGoroutineSafe hammers SilenceWarnings against
// Warn from concurrent goroutines. dontPrintWarning is package-global
// state read by every Warn call; with parallel evaluation dispatching
// operator work across goroutines, an unsynchronized bool is a data
// race the -race detector flags even when the observable output happens
// to be right. This test is the red/green witness for making that state
// atomic - it only fails under -race, which is how CI runs the suite.
func TestSilenceWarningsIsGoroutineSafe(t *testing.T) {
	// Stay silenced for the duration so the writer/reader race is
	// exercised without spamming stderr.
	SilenceWarnings(true)

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				SilenceWarnings(true)
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				NewWarningError(eContextAll, "race probe").Warn()
			}
		}()
	}
	wg.Wait()
}

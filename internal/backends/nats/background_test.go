package natsbackend

import (
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func backgroundRunning() bool {
	bgMu.Lock()
	defer bgMu.Unlock()
	return bgStop != nil
}

// cleanupGoroutines counts goroutines running a cleanup loop.
func cleanupGoroutines() int {
	buf := make([]byte, 1<<20)
	buf = buf[:runtime.Stack(buf, true)]
	n := 0
	for _, g := range strings.Split(string(buf), "\n\n") {
		if strings.Contains(g, "backends/nats.") && strings.Contains(g, "leanupLoop") {
			n++
		}
	}
	return n
}

func TestBackgroundStartsOnPoolUseAndStopsOnShutdown(t *testing.T) {
	Shutdown()
	Shutdown() // safe when nothing is running
	if backgroundRunning() {
		t.Fatal("cleanup loops running after shutdown")
	}

	_, _ = DefaultPool.GetConnection("no-such-target")
	if !backgroundRunning() {
		t.Fatal("pool use did not start the cleanup loops")
	}
	ensureBackground() // second call must not start a second pair
	Shutdown()
	if backgroundRunning() {
		t.Fatal("shutdown left the cleanup loops running")
	}
}

// TestBackgroundStartAndShutdownRace starts and stops the cleanup loops
// from many goroutines at once. Run under -race, it checks that the two
// share their state safely, and afterwards that a final Shutdown leaves
// no loop running.
func TestBackgroundStartAndShutdownRace(t *testing.T) {
	Shutdown()
	const workers = 16
	const rounds = 50
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				if (w+i)%2 == 0 {
					ensureBackground()
				} else {
					Shutdown()
				}
			}
		}(w)
	}
	wg.Wait()

	Shutdown()
	if backgroundRunning() {
		t.Fatal("shutdown left the cleanup loops marked as running")
	}
	waitForCleanupGoroutines(t, 0)

	ensureBackground()
	waitForCleanupGoroutines(t, 2)
	Shutdown()
	waitForCleanupGoroutines(t, 0)
}

func waitForCleanupGoroutines(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for cleanupGoroutines() != want {
		if time.Now().After(deadline) {
			t.Fatalf("want %d cleanup goroutines, have %d", want, cleanupGoroutines())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

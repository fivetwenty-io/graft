package operators

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	natsbackend "github.com/fivetwenty-io/graft/internal/backends/nats"
)

const natsLifecycleEnv = "GRAFT_NATS_LIFECYCLE_CHILD"

// natsCleanupGoroutines counts goroutines running a nats cleanup loop.
func natsCleanupGoroutines() int {
	buf := make([]byte, 1<<20)
	buf = buf[:runtime.Stack(buf, true)]
	n := 0
	for _, g := range strings.Split(string(buf), "\n\n") {
		if strings.Contains(g, "backends/nats.") && strings.Contains(g, "CleanupLoop") {
			n++
		}
	}
	return n
}

func waitForNatsGoroutines(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if natsCleanupGoroutines() == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("want %d nats cleanup goroutines, have %d", want, natsCleanupGoroutines())
}

// TestNatsCleanupLifecycleParent re-runs the lifecycle in a fresh process,
// because other tests in this binary may already have used the nats
// operator, and a fresh process is the only way to see the state right
// after import.
func TestNatsCleanupLifecycleParent(t *testing.T) {
	if os.Getenv(natsLifecycleEnv) != "" {
		t.Skip("running as the child")
	}
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestNatsCleanupLifecycleChild$", "-test.v") //nolint:gosec // re-runs this test binary
	cmd.Env = append(os.Environ(), natsLifecycleEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "--- PASS: TestNatsCleanupLifecycleChild") {
		t.Fatalf("child failed: %v\n%s", err, out)
	}
}

func TestNatsCleanupLifecycleChild(t *testing.T) {
	if os.Getenv(natsLifecycleEnv) == "" {
		t.Skip("runs only as a child of TestNatsCleanupLifecycleParent")
	}

	// Safe to call before anything started, and twice.
	if got := natsCleanupGoroutines(); got != 0 {
		t.Fatalf("after import: want 0 nats cleanup goroutines, have %d", got)
	}
	ShutdownNatsOperator()
	ShutdownNatsOperator()
	if got := natsCleanupGoroutines(); got != 0 {
		t.Fatalf("after idle shutdown: want 0, have %d", got)
	}

	// A cache use starts both loops, exactly once.
	for i := 0; i < 3; i++ {
		_, err := natsbackend.FetchFromKVCachedWith("t", "p", time.Minute, false, false,
			func() (interface{}, error) { return "v", nil })
		if err != nil {
			t.Fatal(err)
		}
	}
	waitForNatsGoroutines(t, 2)

	// Shutdown stops them, and a second shutdown does not panic.
	ShutdownNatsOperator()
	waitForNatsGoroutines(t, 0)
	ShutdownNatsOperator()

	// A later use after shutdown starts them again.
	_, err := natsbackend.FetchFromKVCachedWith("t", "q", time.Minute, false, false,
		func() (interface{}, error) { return "v", nil })
	if err != nil {
		t.Fatal(err)
	}
	waitForNatsGoroutines(t, 2)
	ShutdownNatsOperator()
	waitForNatsGoroutines(t, 0)
}

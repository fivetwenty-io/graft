package natsbackend

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestClientPool_GetConnection_ConcurrentSameTarget hammers GetConnection
// for the same target from many goroutines. Before the fix, the cache-hit
// fast path mutated the shared *PooledConnection's RefCount/LastUsed
// fields while holding only mu.RLock() (a read lock, which permits
// multiple concurrent holders) instead of mu.Lock() - a data race on
// those fields reliably caught by -race under enough concurrent hits.
func TestClientPool_GetConnection_ConcurrentSameTarget(t *testing.T) {
	pc := &PooledConnection{
		Conn:     nil,
		LastUsed: time.Now(),
		RefCount: 0,
	}
	pool := &ClientPool{
		connections: map[string]*PooledConnection{"shared": pc},
		configs:     map[string]*Target{"shared": {URL: "nats://example.invalid:4222"}},
	}

	const callers = 200
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, err := pool.GetConnection("shared")
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if conn != pc {
				t.Errorf("expected the pre-seeded pooled connection, got a different instance")
			}
		}()
	}
	wg.Wait()

	if pc.RefCount != callers {
		t.Errorf("expected RefCount=%d after %d concurrent hits, got %d", callers, callers, pc.RefCount)
	}
}

// TestClientPool_ConcurrentGetConnection_MultiTarget exercises the
// configs/connections map access itself (both already had an RWMutex, but
// verify the fast/slow-path interplay stays race-free with real
// concurrent, distinct-target traffic that forces cache-fill).
func TestClientPool_ConcurrentGetConnection_MultiTarget(t *testing.T) {
	const targets = 5
	const callersPerTarget = 20

	pool := &ClientPool{
		connections: make(map[string]*PooledConnection),
		configs:     make(map[string]*Target),
	}

	for i := 0; i < targets; i++ {
		name := fmt.Sprintf("target%d", i)
		t.Setenv("NATS_"+envSafeUpper(name)+"_URL", "nats://"+name+".example.invalid:4222")
	}

	var wg sync.WaitGroup
	errs := make(chan error, targets*callersPerTarget)
	for i := 0; i < targets; i++ {
		name := fmt.Sprintf("target%d", i)
		for c := 0; c < callersPerTarget; c++ {
			wg.Add(1)
			go func(target string) {
				defer wg.Done()
				if _, err := pool.GetTargetConfig(target); err != nil {
					errs <- fmt.Errorf("target %s: %w", target, err)
				}
			}(name)
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func envSafeUpper(s string) string {
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		out[i] = c
	}
	return string(out)
}

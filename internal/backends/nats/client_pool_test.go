package natsbackend

import (
	"fmt"
	"sync"
	"sync/atomic"
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

// TestStoreOrDiscard_FirstCallerStores verifies the simple case: an empty
// pool stores the caller's candidate and does not invoke closeLoser.
func TestStoreOrDiscard_FirstCallerStores(t *testing.T) {
	pool := &ClientPool{
		connections: make(map[string]*PooledConnection),
		configs:     make(map[string]*Target),
	}
	candidate := &PooledConnection{RefCount: 1, LastUsed: time.Now()}
	cfg := &Target{URL: "nats://target.example.invalid:4222"}

	var closed atomic.Bool
	winner := pool.storeOrDiscard("target", candidate, cfg, func() { closed.Store(true) })

	if winner != candidate {
		t.Errorf("expected the sole candidate to win, got a different instance")
	}
	if closed.Load() {
		t.Error("closeLoser must not be called when there is no existing connection to converge onto")
	}
	if pool.connections["target"] != candidate {
		t.Error("expected the candidate to be stored in the pool")
	}
	if pool.configs["target"] != cfg {
		t.Error("expected the config to be stored alongside the connection")
	}
}

// TestStoreOrDiscard_LoserConvergesAndCloses is a core regression test:
// when a connection for targetName already exists (another
// goroutine won the race while this caller was still connecting),
// storeOrDiscard must return the existing winner - not overwrite it with
// the caller's candidate - and must invoke closeLoser exactly once so the
// caller's now-redundant connection doesn't leak (unreachable from the
// pool, never closed by CloseAll). Before the fix, GetConnection
// unconditionally overwrote ncp.connections[targetName], so the loser's
// real TCP connection + JetStream context were silently dropped, not
// closed.
func TestStoreOrDiscard_LoserConvergesAndCloses(t *testing.T) {
	winnerConn := &PooledConnection{RefCount: 1, LastUsed: time.Now()}
	pool := &ClientPool{
		connections: map[string]*PooledConnection{"target": winnerConn},
		configs:     map[string]*Target{"target": {URL: "nats://winner.example.invalid:4222"}},
	}

	loserCandidate := &PooledConnection{RefCount: 1, LastUsed: time.Now()}
	var closeCalls atomic.Int32
	got := pool.storeOrDiscard("target", loserCandidate, &Target{URL: "nats://loser.example.invalid:4222"}, func() {
		closeCalls.Add(1)
	})

	if got != winnerConn {
		t.Errorf("expected storeOrDiscard to return the existing winner, got a different instance")
	}
	if pool.connections["target"] != winnerConn {
		t.Errorf("expected the winner to remain stored, got the loser overwrote it")
	}
	if got := closeCalls.Load(); got != 1 {
		t.Errorf("expected closeLoser to be called exactly once for the discarded candidate, got %d calls", got)
	}
	if winnerConn.RefCount != 2 {
		t.Errorf("expected the winner's RefCount to be incremented for the converging caller (1 -> 2), got %d", winnerConn.RefCount)
	}
}

// TestStoreOrDiscard_ConcurrentColdTarget simulates a real thundering
// herd: many goroutines racing storeOrDiscard for the
// same never-before-seen target, each with its own already-constructed
// candidate (standing in for a goroutine that already paid the cost of
// CreateConnectionFromConfig + jetstream.New before reaching the store
// step). Exactly one candidate must end up in the pool; every other
// candidate's closeLoser must fire exactly once, so none of them leak.
func TestStoreOrDiscard_ConcurrentColdTarget(t *testing.T) {
	pool := &ClientPool{
		connections: make(map[string]*PooledConnection),
		configs:     make(map[string]*Target),
	}

	const callers = 100
	var closedCount atomic.Int32
	winners := make([]*PooledConnection, callers)
	candidates := make([]*PooledConnection, callers)
	for i := range candidates {
		candidates[i] = &PooledConnection{RefCount: 1, LastUsed: time.Now()}
	}

	var startGate sync.WaitGroup
	startGate.Add(1)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			startGate.Wait()
			winners[idx] = pool.storeOrDiscard("cold-target", candidates[idx], &Target{URL: "nats://cold.example.invalid:4222"}, func() {
				closedCount.Add(1)
			})
		}(i)
	}
	startGate.Done()
	wg.Wait()

	stored := pool.connections["cold-target"]
	if stored == nil {
		t.Fatal("expected exactly one connection to end up stored for the target")
	}

	survivorCount := 0
	for _, w := range winners {
		if w != stored {
			t.Errorf("caller returned a connection that isn't the pool's stored winner - convergence broken")
			continue
		}
		survivorCount++
	}
	if survivorCount != callers {
		t.Errorf("expected all %d callers to converge on the same winner, got %d", callers, survivorCount)
	}

	// Exactly (callers - 1) candidates lost the race and must each have
	// had closeLoser invoked once; the winning candidate's own connection
	// is never passed to closeLoser.
	if got := closedCount.Load(); got != callers-1 {
		t.Errorf("expected %d losing candidates to be closed, got %d (a leak or a double-close)", callers-1, got)
	}
	if stored.RefCount != callers {
		t.Errorf("expected the winner's RefCount to equal the number of callers (%d), got %d", callers, stored.RefCount)
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

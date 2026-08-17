package vault

import (
	"fmt"
	"sync"
	"testing"
)

// TestClientPool_ConcurrentGetClient_MultiTarget hammers ClientPool.GetClient
// from many goroutines across several distinct targets, some sharing a
// target so cache-hit and cache-fill paths both run concurrently. Run with
// -race: the maps backing ClientPool were previously read and written with
// no synchronization at all, so this reliably triggers a data race (or a
// runtime "concurrent map writes" fatal error) before the fix.
func TestClientPool_ConcurrentGetClient_MultiTarget(t *testing.T) {
	const targets = 6
	const callersPerTarget = 30
	const rounds = 5

	for round := 0; round < rounds; round++ {
		pool := &ClientPool{
			clients: make(map[string]Reader),
			configs: make(map[string]*Target),
		}

		for i := 0; i < targets; i++ {
			name := fmt.Sprintf("target%d", i)
			t.Setenv("VAULT_"+envSafeUpper(name)+"_ADDR", "https://vault-"+name+".example.com")
			t.Setenv("VAULT_"+envSafeUpper(name)+"_TOKEN", "token-"+name)
		}

		var startGate sync.WaitGroup
		startGate.Add(1)
		var wg sync.WaitGroup
		errs := make(chan error, targets*callersPerTarget)

		for i := 0; i < targets; i++ {
			name := fmt.Sprintf("target%d", i)
			for c := 0; c < callersPerTarget; c++ {
				wg.Add(1)
				go func(targetName string) {
					defer wg.Done()
					startGate.Wait() // maximize simultaneous map access
					client, err := pool.GetClient(targetName, nil)
					if err != nil {
						errs <- fmt.Errorf("target %s: %w", targetName, err)
						return
					}
					if client == nil {
						errs <- fmt.Errorf("target %s: nil client", targetName)
					}
				}(name)
			}
		}

		startGate.Done()
		wg.Wait()
		close(errs)

		for err := range errs {
			t.Error(err)
		}

		// Every target should have exactly one pooled client (proves the
		// double-checked-locking cache-fill converged instead of racing to
		// distinct entries).
		if got := len(pool.clients); got != targets {
			t.Errorf("round %d: expected %d pooled clients, got %d", round, targets, got)
		}
	}
}

// TestClientPool_ConcurrentGetClient_SameTargetReusesClient verifies that
// many concurrent GetClient calls for the SAME target converge on one
// created client instance rather than each racing to create and store its
// own (which would be wasteful but, more importantly, would indicate the
// map access itself is not properly serialized).
func TestClientPool_ConcurrentGetClient_SameTargetReusesClient(t *testing.T) {
	pool := &ClientPool{
		clients: make(map[string]Reader),
		configs: make(map[string]*Target),
	}

	t.Setenv("VAULT_SHARED_ADDR", "https://vault-shared.example.com")
	t.Setenv("VAULT_SHARED_TOKEN", "shared-token")

	const callers = 50
	var wg sync.WaitGroup
	results := make([]Reader, callers)
	errs := make([]error, callers)

	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			client, err := pool.GetClient("shared", nil)
			results[idx] = client
			errs[idx] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("caller %d: unexpected error: %v", i, err)
		}
	}
	first := results[0]
	for i, r := range results {
		if r != first {
			t.Errorf("caller %d: got a different client instance than caller 0 (pool did not converge)", i)
		}
	}

	if got := len(pool.clients); got != 1 {
		t.Errorf("expected exactly 1 pooled client for the shared target, got %d", got)
	}
}

// envSafeUpper mirrors the uppercasing ClientPool.getTargetConfig applies to
// target names when building environment variable prefixes.
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

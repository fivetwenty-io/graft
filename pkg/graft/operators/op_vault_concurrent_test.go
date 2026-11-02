package operators

import (
	"context"
	"sync"
	"testing"

	vaultbackend "github.com/fivetwenty-io/graft/internal/backends/vault"
	"github.com/fivetwenty-io/graft/pkg/graft"
)

// TestVaultFirstUseConcurrent evaluates (( vault )) on several engines at
// once, before the default reader exists, so the lazy initialization is
// exercised under -race. The address is unreachable, so every evaluation is
// expected to fail; the test only cares that they do not race.
func TestVaultFirstUseConcurrent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VAULT_ADDR", "http://127.0.0.1:1")
	t.Setenv("VAULT_TOKEN", "fake-token")

	for _, name := range []string{"VAULT_NAMESPACE", "VAULT_SKIP_VERIFY", "VAULT_CACERT", "VAULT_CAPATH", "VAULT_CLIENT_CERT", "VAULT_CLIENT_KEY", "VAULT_PROXY_ADDR", "VAULT_HTTP_PROXY"} {
		t.Setenv(name, "")
	}

	previous := vaultbackend.GlobalReader
	vaultbackend.GlobalReader = nil

	t.Cleanup(func() { vaultbackend.GlobalReader = previous })

	const workers = 8

	engines := make([]graft.Engine, workers)

	for i := range engines {
		engine, err := graft.NewEngine()
		if err != nil {
			t.Fatalf("NewEngine: %v", err)
		}

		engines[i] = engine
	}

	var wg sync.WaitGroup

	start := make(chan struct{})

	for _, engine := range engines {
		wg.Add(1)

		go func() {
			defer wg.Done()

			doc, err := engine.ParseYAML([]byte(`secret: (( vault "secret/x:y" ))` + "\n"))
			if err != nil {
				t.Errorf("parse: %v", err)

				return
			}

			<-start

			_, evalErr := engine.Evaluate(context.Background(), doc)
			if evalErr == nil {
				t.Errorf("expected an error from the unreachable vault")
			}
		}()
	}

	close(start)
	wg.Wait()
}

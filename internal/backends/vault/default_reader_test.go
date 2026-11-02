package vault

import (
	"sync"
	"testing"
)

// isolateVaultEnv keeps DefaultReader from reading machine state. It points
// HOME at an empty directory, so neither ~/.vault-token nor ~/.svtoken is
// found, clears every VAULT_* variable the client consults, and resets
// GlobalReader both before and after the test.
func isolateVaultEnv(t *testing.T) {
	t.Helper()

	t.Setenv("HOME", t.TempDir())

	for _, name := range []string{
		"VAULT_ADDR", "VAULT_TOKEN", "VAULT_NAMESPACE", "VAULT_SKIP_VERIFY",
		"VAULT_CACERT", "VAULT_CAPATH", "VAULT_CLIENT_CERT", "VAULT_CLIENT_KEY",
		"VAULT_PROXY_ADDR", "VAULT_HTTP_PROXY",
	} {
		t.Setenv(name, "")
	}

	GlobalReader = nil

	t.Cleanup(func() { GlobalReader = nil })
}

func TestDefaultReaderConcurrentInit(t *testing.T) {
	isolateVaultEnv(t)
	t.Setenv("VAULT_ADDR", "http://127.0.0.1:1")
	t.Setenv("VAULT_TOKEN", "fake-token")

	const workers = 8

	readers := make([]Reader, workers)
	errs := make([]error, workers)

	var wg sync.WaitGroup

	start := make(chan struct{})

	for i := range workers {
		wg.Add(1)

		go func() {
			defer wg.Done()

			<-start

			readers[i], errs[i] = DefaultReader()
		}()
	}

	close(start)
	wg.Wait()

	for i := range workers {
		if errs[i] != nil {
			t.Fatalf("worker %d: DefaultReader returned error: %v", i, errs[i])
		}

		if readers[i] == nil {
			t.Fatalf("worker %d: DefaultReader returned a nil reader", i)
		}

		if readers[i] != readers[0] {
			t.Fatalf("worker %d got a different reader than worker 0", i)
		}
	}
}

func TestDefaultReaderRetriesAfterFailure(t *testing.T) {
	isolateVaultEnv(t)
	t.Setenv("VAULT_ADDR", "http://127.0.0.1:1")

	reader, err := DefaultReader()
	if err == nil {
		t.Fatal("expected an error without a token")
	}

	if reader != nil {
		t.Fatalf("expected a nil reader after failure, got %v", reader)
	}

	t.Setenv("VAULT_TOKEN", "fake-token")

	reader, err = DefaultReader()
	if err != nil {
		t.Fatalf("expected the retry to succeed: %v", err)
	}

	if reader == nil {
		t.Fatal("expected a reader after the retry")
	}
}

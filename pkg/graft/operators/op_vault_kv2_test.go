package operators

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	vaultbackend "github.com/fivetwenty-io/graft/internal/backends/vault"
	"github.com/fivetwenty-io/graft/internal/vaultkv/vaultkvtest"
	"github.com/fivetwenty-io/graft/pkg/graft"
)

// envKVSecrets holds one secret on a KV v1 mount and one on a KV v2
// mount, with values that say which mount served them. prefix keeps
// each test's paths apart in the process-wide secret cache.
func envKVSecrets(prefix string) map[string]map[string]interface{} {
	return map[string]map[string]interface{}{
		"kv1/" + prefix + "/db": {"password": "v1-password"},
		"kv2/" + prefix + "/db": {"password": "v2-password"},
	}
}

var envKVMounts = map[string]int{"kv1/": 1, "kv2/": 2}

// useEnvVault points the environment-configured default reader at addr
// for one test, with no token or CA file from the machine.
func useEnvVault(t *testing.T, addr string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	for _, name := range []string{"VAULT_NAMESPACE", "VAULT_SKIP_VERIFY", "VAULT_CACERT", "VAULT_CAPATH"} {
		t.Setenv(name, "")
	}
	t.Setenv("VAULT_ADDR", addr)
	t.Setenv("VAULT_TOKEN", "t")

	previous := vaultbackend.GlobalReader
	vaultbackend.GlobalReader = nil
	vaultbackend.SecretCache.Reset()
	t.Cleanup(func() {
		vaultbackend.GlobalReader = previous
		vaultbackend.SecretCache.Reset()
	})
}

// envEvaluate evaluates doc on an engine with no vault configured, so
// every vault call goes through the environment-configured readers.
func envEvaluate(t *testing.T, doc string) (graft.Document, error) {
	t.Helper()
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	parsed, err := engine.ParseYAML([]byte(doc))
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}
	return engine.Evaluate(context.Background(), parsed)
}

func envWant(t *testing.T, out graft.Document, want map[string]string) {
	t.Helper()
	for key, value := range want {
		got, err := out.Get(key)
		if err != nil {
			t.Fatalf("Get(%s): %v", key, err)
		}
		if got != value {
			t.Errorf("%s = %v, want %q", key, got, value)
		}
	}
}

// TestVaultEnvDefaultReaderReadsKV1AndKV2 reads whole secrets through
// DefaultReader and path:key values through the vault operator, from a
// KV v1 mount and a KV v2 mount configured by VAULT_ADDR.
func TestVaultEnvDefaultReaderReadsKV1AndKV2(t *testing.T) {
	fake := vaultkvtest.New(t, envKVMounts, envKVSecrets("envdefault"))
	useEnvVault(t, fake.URL)

	reader, err := vaultbackend.DefaultReader()
	if err != nil {
		t.Fatalf("DefaultReader: %v", err)
	}
	for path, want := range map[string]string{
		"kv1/envdefault/db":      "v1-password",
		"kv2/envdefault/db":      "v2-password",
		"kv2/data/envdefault/db": "v2-password",
	} {
		secret, err := reader.ReadSecret(context.Background(), path)
		if err != nil {
			t.Fatalf("ReadSecret(%s): %v", path, err)
		}
		if len(secret) != 1 || secret["password"] != want {
			t.Errorf("ReadSecret(%s) = %v, want only password=%s", path, secret, want)
		}
	}

	out, err := envEvaluate(t, `v1: (( vault "kv1/envdefault/db:password" ))
v2: (( vault "kv2/envdefault/db:password" ))
`)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	envWant(t, out, map[string]string{"v1": "v1-password", "v2": "v2-password"})
	if n := fake.SysMountsHits(); n != 0 {
		t.Errorf("sys/mounts was requested %d times", n)
	}
}

// TestVaultEnvTargetPoolReadsKV1AndKV2 reads through the pool a
// VAULT_<TARGET>_* target builds, both whole secrets and path:key.
func TestVaultEnvTargetPoolReadsKV1AndKV2(t *testing.T) {
	fake := vaultkvtest.New(t, envKVMounts, envKVSecrets("envpool"))
	useEnvVault(t, "http://127.0.0.1:1")
	const target = "kvtwopooltarget"
	t.Setenv("VAULT_KVTWOPOOLTARGET_ADDR", fake.URL)
	t.Setenv("VAULT_KVTWOPOOLTARGET_TOKEN", "t")

	reader, err := vaultbackend.DefaultPool.GetClient(target, nil)
	if err != nil {
		t.Fatalf("GetClient: %v", err)
	}
	for path, want := range map[string]string{
		"kv1/envpool/db": "v1-password",
		"kv2/envpool/db": "v2-password",
	} {
		secret, err := reader.ReadSecret(context.Background(), path)
		if err != nil {
			t.Fatalf("ReadSecret(%s): %v", path, err)
		}
		if secret["password"] != want {
			t.Errorf("ReadSecret(%s) = %v, want password=%s", path, secret, want)
		}
	}

	out, err := envEvaluate(t, `v1: (( vault@`+target+` "kv1/envpool/db:password" ))
v2: (( vault@`+target+` "kv2/envpool/db:password" ))
`)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	envWant(t, out, map[string]string{"v1": "v1-password", "v2": "v2-password"})
}

// TestVaultEnvMountLookupDeniedExplainsFallback denies the per-path
// mounts lookup. The KV v1 secret still reads, and the KV v2 secret,
// read as KV v1, fails with the contract's "secret ... not found" shape
// plus a clause that says why.
func TestVaultEnvMountLookupDeniedExplainsFallback(t *testing.T) {
	fake := vaultkvtest.New(t, envKVMounts, envKVSecrets("envdenied"))
	fake.DenyMountLookups()
	useEnvVault(t, fake.URL)

	out, err := envEvaluate(t, `v1: (( vault "kv1/envdenied/db:password" ))`+"\n")
	if err != nil {
		t.Fatalf("KV v1 read after a denied lookup: %v", err)
	}
	envWant(t, out, map[string]string{"v1": "v1-password"})

	reader, err := vaultbackend.DefaultReader()
	if err != nil {
		t.Fatal(err)
	}
	_, err = reader.ReadSecret(context.Background(), "kv2/envdenied/db")
	var nf *vaultbackend.ErrNotFound
	if !errors.As(err, &nf) || nf.Path != "kv2/envdenied/db" {
		t.Fatalf("ReadSecret error = %v, want an *ErrNotFound for the logical path", err)
	}
	if !strings.Contains(graft.DetailedError(err), `mount "kv2/": KV version undetected, read as KV v1: `) {
		t.Errorf("ReadSecret error does not explain the fallback: %s", graft.DetailedError(err))
	}

	_, err = envEvaluate(t, `v2: (( vault "kv2/envdenied/db:password" ))`+"\n")
	if err == nil {
		t.Fatal("a KV v2 secret read as KV v1 must not be found")
	}
	detail := graft.DetailedError(err)
	const want = `secret kv2/envdenied/db:password (mount "kv2/": KV version undetected, read as KV v1) not found`
	if !strings.Contains(detail, want) {
		t.Errorf("DetailedError = %s, want it to hold %s", detail, want)
	}
	if strings.Contains(detail, "v2-password") {
		t.Errorf("the error quotes a secret value: %s", detail)
	}
}

// TestVaultEnvSysMountsDeniedStillReadsKV2 pins that mount detection
// uses only sys/internal/ui/mounts/<path>: the fake refuses sys/mounts
// with a 403, and the KV v2 read must still go through data/.
func TestVaultEnvSysMountsDeniedStillReadsKV2(t *testing.T) {
	fake := vaultkvtest.New(t, envKVMounts, envKVSecrets("envsysmounts"))
	useEnvVault(t, fake.URL)

	out, err := envEvaluate(t, `v2: (( vault "kv2/envsysmounts/db:password" ))`+"\n")
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	envWant(t, out, map[string]string{"v2": "v2-password"})
	if n := fake.SysMountsHits(); n != 0 {
		t.Errorf("sys/mounts was requested %d times", n)
	}
	reads := fake.Reads()
	if len(reads) != 1 || reads[0] != "kv2/data/envsysmounts/db" {
		t.Errorf("reads = %v, want one read of kv2/data/envsysmounts/db", reads)
	}
}

// TestVaultEnvConcurrentKV1AndKV2Reads reads both mounts through the
// default reader from many goroutines at once, for -race.
func TestVaultEnvConcurrentKV1AndKV2Reads(t *testing.T) {
	fake := vaultkvtest.New(t, envKVMounts, envKVSecrets("envrace"))
	useEnvVault(t, fake.URL)

	reader, err := vaultbackend.DefaultReader()
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 32)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			path, want := "kv1/envrace/db", "v1-password"
			if i%2 == 1 {
				path, want = "kv2/envrace/db", "v2-password"
			}
			secret, err := reader.ReadSecret(context.Background(), path)
			switch {
			case err != nil:
				errs[i] = err
			case secret["password"] != want:
				errs[i] = errors.New("wrong value from " + path)
			}
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("read %d: %v", i, err)
		}
	}
}

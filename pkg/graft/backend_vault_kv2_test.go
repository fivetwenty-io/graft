package graft_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/fivetwenty-io/graft/internal/vaultkv/vaultkvtest"
	"github.com/fivetwenty-io/graft/pkg/graft"
)

// kvMounts is one KV v1 mount and one KV v2 mount.
var kvMounts = map[string]int{"kv1/": 1, "kv2/": 2}

// kvSecrets holds the same secret on each of kvMounts, with values
// that say which mount served them.
func kvSecrets() map[string]map[string]interface{} {
	return map[string]map[string]interface{}{
		"kv1/app/db": {"password": "v1-password", "user": "v1-user"},
		"kv2/app/db": {"password": "v2-password", "user": "v2-user"},
	}
}

// kvEvaluate merges doc on eng and returns the evaluated document.
func kvEvaluate(t *testing.T, eng graft.Engine, doc string) (graft.Document, error) {
	t.Helper()
	parsed, err := eng.ParseYAML([]byte(doc))
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}
	return eng.Merge(context.Background(), parsed).Execute()
}

// kvWant checks that each key of out holds its value in want.
func kvWant(t *testing.T, out graft.Document, want map[string]string) {
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

// TestWithVault_ReadsKV1AndKV2 reads path:key values through the
// WithVault default target from a KV v1 mount and a KV v2 mount, the
// v2 one both by its logical path and with data/ spelled out.
func TestWithVault_ReadsKV1AndKV2(t *testing.T) {
	fake := vaultkvtest.New(t, kvMounts, kvSecrets())
	eng, err := graft.NewEngine(
		graft.WithBackendRegistry(true),
		graft.WithVault(graft.VaultConfig{Address: fake.URL, Token: "t"}),
	)
	if err != nil {
		t.Fatal(err)
	}

	out, err := kvEvaluate(t, eng, `v1: (( vault "kv1/app/db:password" ))
v2: (( vault "kv2/app/db:password" ))
v2user: (( vault "/kv2/app/db:user" ))
v2data: (( vault "kv2/data/app/db:password" ))
`)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	kvWant(t, out, map[string]string{
		"v1":     "v1-password",
		"v2":     "v2-password",
		"v2user": "v2-user",
		"v2data": "v2-password",
	})
	if n := fake.SysMountsHits(); n != 0 {
		t.Errorf("sys/mounts was requested %d times", n)
	}
	for _, read := range fake.Reads() {
		if strings.HasPrefix(read, "kv2/data/data/") {
			t.Errorf("data/ was doubled in %q", read)
		}
	}
}

// TestWithVaultTarget_ReadsKV1AndKV2 reads path:key values through a
// named WithVaultTarget with the vault@name syntax.
func TestWithVaultTarget_ReadsKV1AndKV2(t *testing.T) {
	fake := vaultkvtest.New(t, kvMounts, kvSecrets())
	eng, err := graft.NewEngine(
		graft.WithBackendRegistry(true),
		graft.WithVaultTarget("prod", graft.VaultConfig{Address: fake.URL, Token: "t"}),
	)
	if err != nil {
		t.Fatal(err)
	}

	out, err := kvEvaluate(t, eng, `v1: (( vault@prod "kv1/app/db:user" ))
v2: (( vault@prod "kv2/app/db:user" ))
v2data: (( vault@prod "kv2/data/app/db:user" ))
`)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	kvWant(t, out, map[string]string{"v1": "v1-user", "v2": "v2-user", "v2data": "v2-user"})
}

// TestWithVault_MountLookupDeniedFallsBackToKV1 denies the per-path
// mounts lookup with a 403. The KV v1 mount still reads, and a KV v2
// secret, read as KV v1, fails with an error that says why.
func TestWithVault_MountLookupDeniedFallsBackToKV1(t *testing.T) {
	fake := vaultkvtest.New(t, kvMounts, kvSecrets())
	fake.DenyMountLookups()
	eng, err := graft.NewEngine(
		graft.WithBackendRegistry(true),
		graft.WithVault(graft.VaultConfig{Address: fake.URL, Token: "t"}),
	)
	if err != nil {
		t.Fatal(err)
	}

	out, err := kvEvaluate(t, eng, `v1: (( vault "kv1/app/db:password" ))`+"\n")
	if err != nil {
		t.Fatalf("KV v1 read after a denied lookup: %v", err)
	}
	kvWant(t, out, map[string]string{"v1": "v1-password"})

	_, err = kvEvaluate(t, eng, `v2: (( vault "kv2/app/db:password" ))`+"\n")
	if err == nil {
		t.Fatal("a KV v2 secret read as KV v1 must not be found")
	}
	detail := graft.DetailedError(err)
	const clause = `secret kv2/app/db:password (mount "kv2/": KV version undetected, read as KV v1) not found`
	if !strings.Contains(detail, clause) {
		t.Errorf("DetailedError does not explain the fallback:\n%s", detail)
	}
	if !strings.Contains(err.Error(), "KV version undetected, read as KV v1") {
		t.Errorf("Error() does not explain the fallback:\n%s", err.Error())
	}
	if strings.Contains(detail, "v2-password") || strings.Contains(err.Error(), "v2-password") {
		t.Errorf("the error quotes a secret value:\n%s", detail)
	}
}

// TestWithVault_DetectedMountNotFoundHasNoFallbackClause checks that a
// missing secret on a mount whose version was detected keeps the plain
// "secret <key> not found" text.
func TestWithVault_DetectedMountNotFoundHasNoFallbackClause(t *testing.T) {
	fake := vaultkvtest.New(t, kvMounts, kvSecrets())
	eng, err := graft.NewEngine(
		graft.WithBackendRegistry(true),
		graft.WithVault(graft.VaultConfig{Address: fake.URL, Token: "t"}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = kvEvaluate(t, eng, `v2: (( vault "kv2/app/missing:password" ))`+"\n")
	if err == nil {
		t.Fatal("expected a not-found error")
	}
	detail := graft.DetailedError(err)
	if !strings.Contains(detail, "secret kv2/app/missing:password not found") {
		t.Errorf("DetailedError lost the contract shape:\n%s", detail)
	}
	if strings.Contains(detail, "undetected") {
		t.Errorf("a detected mount must not get the fallback clause:\n%s", detail)
	}
}

// TestWithVault_TargetsKeepTheirOwnMountTables configures the default
// target on a vault whose secret/ mount is KV v1 and a named target on
// one whose secret/ mount is KV v2, so one target's mount table must
// never answer for the other.
func TestWithVault_TargetsKeepTheirOwnMountTables(t *testing.T) {
	v1 := vaultkvtest.New(t, map[string]int{"secret/": 1}, map[string]map[string]interface{}{
		"secret/app": {"password": "from-v1"},
	})
	v2 := vaultkvtest.New(t, map[string]int{"secret/": 2}, map[string]map[string]interface{}{
		"secret/app": {"password": "from-v2"},
	})
	eng, err := graft.NewEngine(
		graft.WithBackendRegistry(true),
		graft.WithVault(graft.VaultConfig{Address: v1.URL, Token: "t"}),
		graft.WithVaultTarget("other", graft.VaultConfig{Address: v2.URL, Token: "t"}),
	)
	if err != nil {
		t.Fatal(err)
	}

	// Two merges, so each order of first lookup is exercised.
	for _, doc := range []string{
		"a: (( vault \"secret/app:password\" ))\nb: (( vault@other \"secret/app:password\" ))\n",
		"b: (( vault@other \"secret/app:password\" ))\na: (( vault \"secret/app:password\" ))\n",
	} {
		out, err := kvEvaluate(t, eng, doc)
		if err != nil {
			t.Fatalf("merge: %v", err)
		}
		kvWant(t, out, map[string]string{"a": "from-v1", "b": "from-v2"})
	}
}

// TestWithVault_ConcurrentKV1AndKV2Reads evaluates many vault calls on
// both mounts and both targets in one parallel merge, for -race.
func TestWithVault_ConcurrentKV1AndKV2Reads(t *testing.T) {
	fake := vaultkvtest.New(t, kvMounts, kvSecrets())
	eng, err := graft.NewEngine(
		graft.WithBackendRegistry(true),
		graft.WithVault(graft.VaultConfig{Address: fake.URL, Token: "t"}),
		graft.WithVaultTarget("prod", graft.VaultConfig{Address: fake.URL, Token: "t"}),
	)
	if err != nil {
		t.Fatal(err)
	}

	var doc strings.Builder
	want := map[string]string{}
	for i := 0; i < 32; i++ {
		mount := []string{"kv1", "kv2"}[i%2]
		op := []string{"vault", "vault@prod"}[(i/2)%2]
		fmt.Fprintf(&doc, "k%d: (( %s \"%s/app/db:password\" ))\n", i, op, mount)
		want[fmt.Sprintf("k%d", i)] = strings.Replace(mount, "kv", "v", 1) + "-password"
	}

	var wg sync.WaitGroup
	errs := make([]error, 4)
	outs := make([]graft.Document, 4)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			parsed, err := eng.ParseYAML([]byte(doc.String()))
			if err != nil {
				errs[i] = err
				return
			}
			outs[i], errs[i] = eng.Merge(context.Background(), parsed).Execute()
		}()
	}
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("merge %d: %v", i, errs[i])
		}
		kvWant(t, outs[i], want)
	}
}

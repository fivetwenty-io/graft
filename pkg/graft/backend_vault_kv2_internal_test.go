package graft

import (
	"context"
	"errors"
	"testing"

	"github.com/fivetwenty-io/graft/internal/vaultkv"
	"github.com/fivetwenty-io/graft/internal/vaultkv/vaultkvtest"
)

// TestVaultOptionBackend_WholeSecretReadsKV1AndKV2 reads whole secrets
// through the reader each WithVault target builds, the default target
// and a named one, on a KV v1 mount and a KV v2 mount.
func TestVaultOptionBackend_WholeSecretReadsKV1AndKV2(t *testing.T) {
	fake := vaultkvtest.New(t, map[string]int{"kv1/": 1, "kv2/": 2}, map[string]map[string]interface{}{
		"kv1/app/db": {"password": "v1-password", "user": "v1-user"},
		"kv2/app/db": {"password": "v2-password", "user": "v2-user"},
	})
	b := newVaultOptionBackend()
	b.setConfig("", VaultConfig{Address: fake.URL, Token: "t"})
	b.setConfig("prod", VaultConfig{Address: fake.URL, Token: "t"})

	for _, target := range []string{"", "prod"} {
		reader, err := b.readerFor(target)
		if err != nil {
			t.Fatalf("readerFor(%q): %v", target, err)
		}
		for path, want := range map[string]string{"kv1/app/db": "v1", "kv2/app/db": "v2"} {
			res, err := reader.Read(context.Background(), path)
			if err != nil {
				t.Fatalf("target %q: Read(%s): %v", target, path, err)
			}
			if len(res.Data) != 2 || res.Data["password"] != want+"-password" || res.Data["user"] != want+"-user" {
				t.Errorf("target %q: Read(%s) = %v", target, path, res.Data)
			}
		}
	}

	if a, _ := b.readerFor(""); a == mustReader(t, b, "prod") {
		t.Error("two targets share one reader, and so one mount cache")
	}
}

// TestVaultOptionBackend_UndetectedNotFoundKeepsSentinel checks that a
// missing secret, read as KV v1 after a denied mounts lookup, still
// reports ErrBackendNotFound and names the fallback.
func TestVaultOptionBackend_UndetectedNotFoundKeepsSentinel(t *testing.T) {
	fake := vaultkvtest.New(t, map[string]int{"kv2/": 2}, map[string]map[string]interface{}{
		"kv2/app/db": {"password": "v2-password"},
	})
	fake.DenyMountLookups()
	b := newVaultOptionBackend()
	b.setConfig("", VaultConfig{Address: fake.URL, Token: "t"})

	_, err := b.Get(context.Background(), "kv2/app/db:password")
	if !errors.Is(err, ErrBackendNotFound) {
		t.Fatalf("Get error = %v, want ErrBackendNotFound", err)
	}
	var undetected *vaultkv.UndetectedError
	if !errors.As(err, &undetected) || undetected.Mount != "kv2/" {
		t.Fatalf("Get error = %v, want an *UndetectedError for kv2/", err)
	}
	const want = `mount "kv2/": KV version undetected, read as KV v1: backend: path not found: kv2/app/db:password`
	if got := DetailedError(err); got != want {
		t.Errorf("DetailedError = %q, want %q", got, want)
	}
	if got := err.Error(); got != "KV version undetected, read as KV v1: backend: path not found" {
		t.Errorf("Error() = %q", got)
	}
}

func mustReader(t *testing.T, b *vaultOptionBackend, target string) *vaultkv.Reader {
	t.Helper()
	r, err := b.readerFor(target)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

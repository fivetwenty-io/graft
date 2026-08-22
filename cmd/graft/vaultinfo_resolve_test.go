package main

import (
	"context"
	"testing"

	vaultbackend "github.com/fivetwenty-io/graft/internal/backends/vault"
)

// selfReferencingPathReader stubs a single Vault mount, "secret/paths",
// whose "root" key names another key ("child") in the same secret.
// Mirrors pkg/graft/operators/op_vault_selfreference_test.go's reader of
// the same name, and assets/vault/self-reference.yml.
type selfReferencingPathReader struct{}

func (selfReferencingPathReader) ReadSecret(_ context.Context, path string) (map[string]interface{}, error) {
	if path != "secret/paths" {
		return nil, &vaultbackend.ErrNotFound{Path: path}
	}
	return map[string]interface{}{
		"root":  "child",
		"child": "s3kr1t",
	}, nil
}

// withGlobalVaultReader swaps vaultbackend.GlobalReader for
// selfReferencingPathReader (the one stub every caller uses) for the
// duration of fn, restoring the previous value afterward so other tests
// aren't affected.
func withGlobalVaultReader(fn func()) {
	previous := vaultbackend.GlobalReader
	vaultbackend.GlobalReader = selfReferencingPathReader{}
	defer func() { vaultbackend.GlobalReader = previous }()
	fn()
}

// runVaultinfo invokes main() with the given vaultinfo args via
// runGraftCommand (testsupport_test.go).
func runVaultinfo(t *testing.T, args []string) (stdout, stderr string, rc int) {
	t.Helper()
	return runGraftCommand(t, args)
}

// TestVaultInfoResolveReportsConcretePaths pins `vaultinfo --resolve`:
// against a reachable (stub) Vault, it performs real lookups instead of
// skipping them, so a path composed from another vault lookup resolves
// to its real, concrete form - "secret/paths:child" - rather than the
// symbolic "<secret/paths:root>" reference vaultinfo's default
// (skip-vault) mode reports for the same fixture (see the "vaultinfo
// renders a path composed from another vault lookup symbolically..."
// case in main_test.go).
func TestVaultInfoResolveReportsConcretePaths(t *testing.T) {
	vaultbackend.SecretCache.Reset()
	defer vaultbackend.SecretCache.Reset()

	withGlobalVaultReader(func() {
		stdout, stderr, rc := runVaultinfo(t, []string{"vaultinfo", "--resolve", "../../assets/vault/self-reference.yml"})

		if rc != 0 {
			t.Fatalf("rc = %d, stderr: %s", rc, stderr)
		}
		if stderr != "" {
			t.Fatalf("stderr = %q, want empty", stderr)
		}
		want := `secrets:
- key: secret/paths:child
  references:
  - value
- key: secret/paths:root
  references:
  - meta.path

`
		if stdout != want {
			t.Fatalf("stdout = %q, want %q", stdout, want)
		}
	})
}

// TestVaultInfoWithoutResolveStaysSkippedEvenWithAReachableVault confirms
// --resolve is opt-in: with the very same reachable stub Vault wired in,
// plain `vaultinfo` (no --resolve) must still skip Vault entirely and
// report the symbolic placeholder, not the concrete resolved path - the
// stub is never consulted.
func TestVaultInfoWithoutResolveStaysSkippedEvenWithAReachableVault(t *testing.T) {
	vaultbackend.SecretCache.Reset()
	defer vaultbackend.SecretCache.Reset()

	withGlobalVaultReader(func() {
		stdout, stderr, rc := runVaultinfo(t, []string{"vaultinfo", "../../assets/vault/self-reference.yml"})

		if rc != 0 {
			t.Fatalf("rc = %d, stderr: %s", rc, stderr)
		}
		if stderr != "" {
			t.Fatalf("stderr = %q, want empty", stderr)
		}
		want := `secrets:
- key: secret/paths:<secret/paths:root>
  references:
  - value
- key: secret/paths:root
  references:
  - meta.path

`
		if stdout != want {
			t.Fatalf("stdout = %q, want %q (--resolve not given: Vault must stay skipped)", stdout, want)
		}
	})
}

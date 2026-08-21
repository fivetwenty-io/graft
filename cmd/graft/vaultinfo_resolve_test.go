package main

import (
	"context"
	"fmt"
	"os"
	"testing"

	vaultbackend "github.com/fivetwenty-io/graft/internal/backends/vault"
	"github.com/fivetwenty-io/graft/log"
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

// withGlobalVaultReader swaps vaultbackend.GlobalReader for the duration
// of fn, restoring the previous value afterward so other tests aren't
// affected.
func withGlobalVaultReader(reader vaultbackend.Reader, fn func()) {
	previous := vaultbackend.GlobalReader
	vaultbackend.GlobalReader = reader
	defer func() { vaultbackend.GlobalReader = previous }()
	fn()
}

// runVaultinfo invokes main() with the given vaultinfo args and returns
// the captured stdout/stderr/exit code, restoring the previous test
// hooks and os.Args afterward.
func runVaultinfo(t *testing.T, args []string) (stdout, stderr string, rc int) {
	t.Helper()

	prevPrintStdOutf := printStdOutf
	prevPrintStdErrf := log.PrintStdErrf
	prevExit := exit
	prevUsage := usage
	prevArgs := os.Args
	defer func() {
		printStdOutf = prevPrintStdOutf
		log.PrintStdErrf = prevPrintStdErrf
		exit = prevExit
		usage = prevUsage
		os.Args = prevArgs
	}()

	printStdOutf = func(format string, fmtArgs ...interface{}) {
		stdout += fmt.Sprintf(format, fmtArgs...)
	}
	log.PrintStdErrf = func(format string, fmtArgs ...interface{}) {
		stderr += fmt.Sprintf(format, fmtArgs...)
	}
	rc = 256 // sentinel: unset if exit is never called
	exit = func(code int) { rc = code }
	usage = func() { exit(1) }

	os.Args = append([]string{"graft"}, args...)
	main()
	return stdout, stderr, rc
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

	withGlobalVaultReader(selfReferencingPathReader{}, func() {
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

	withGlobalVaultReader(selfReferencingPathReader{}, func() {
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

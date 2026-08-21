package operators

import (
	"context"
	"os"
	"testing"

	vaultbackend "github.com/fivetwenty-io/graft/internal/backends/vault"
	"github.com/fivetwenty-io/graft/pkg/graft"
)

// selfReferenceFixturePath locates assets/vault/self-reference.yml relative
// to this package's directory (pkg/graft/operators), following the same
// "../../assets/..." convention cmd/graft's tests use for their own
// package depth (see cmd/graft/main_test.go's openFiles calls).
const selfReferenceFixturePath = "../../../assets/vault/self-reference.yml"

// selfReferencingPathReader stubs a single Vault mount, "secret/paths",
// whose "root" key names another key ("child") in the same secret. It
// mirrors assets/vault/self-reference.yml.
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

// TestVaultOperatorPathComposedFromAnotherVaultLookup pins the
// vault-from-vault path composition pattern documented in
// docs/user-guide/secrets/vault.md ("Path Segment From Another Vault
// Lookup"): a second (( vault ... )) call's path argument may itself come
// from a prior (( vault ... )) lookup elsewhere in the tree. It loads
// assets/vault/self-reference.yml directly rather than inlining the YAML,
// so the fixture stays load-bearing instead of drifting out of sync with
// the test.
//
// VaultOperator.Dependencies (op_vault.go:357-359) returns the auto-detected
// reference to meta.path as a real dependency edge, so the evaluator's Kahn
// topological sort orders meta.path's lookup before the second lookup that
// consumes it, regardless of document order.
func TestVaultOperatorPathComposedFromAnotherVaultLookup(t *testing.T) {
	vaultbackend.SecretCache.Reset()
	defer vaultbackend.SecretCache.Reset()

	withGlobalVaultReader(selfReferencingPathReader{}, func() {
		engine, err := graft.NewEngine()
		if err != nil {
			t.Fatalf("NewEngine: %v", err)
		}

		yamlDoc, err := os.ReadFile(selfReferenceFixturePath)
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", selfReferenceFixturePath, err)
		}
		doc, err := evaluateYAML(t, engine, string(yamlDoc))
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}

		gotPath, err := doc.GetString("meta.path")
		if err != nil {
			t.Fatalf("GetString(\"meta.path\"): %v", err)
		}
		if gotPath != "child" {
			t.Fatalf("meta.path = %q, want %q", gotPath, "child")
		}

		gotValue, err := doc.GetString("value")
		if err != nil {
			t.Fatalf("GetString(\"value\"): %v", err)
		}
		if gotValue != "s3kr1t" {
			t.Fatalf("value = %q, want %q", gotValue, "s3kr1t")
		}
	})
}

// TestVaultOperatorPathComposedFromAnotherVaultLookupReverseOrder pins that
// the composition in TestVaultOperatorPathComposedFromAnotherVaultLookup
// does not depend on meta.path being declared before value in the document -
// the dependency edge, not document order, drives evaluation order.
func TestVaultOperatorPathComposedFromAnotherVaultLookupReverseOrder(t *testing.T) {
	vaultbackend.SecretCache.Reset()
	defer vaultbackend.SecretCache.Reset()

	withGlobalVaultReader(selfReferencingPathReader{}, func() {
		engine, err := graft.NewEngine()
		if err != nil {
			t.Fatalf("NewEngine: %v", err)
		}

		yamlDoc := "value: (( vault \"secret/paths:\" meta.path ))\nmeta:\n  path: (( vault \"secret/paths:root\" ))\n"
		doc, err := evaluateYAML(t, engine, yamlDoc)
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}

		gotValue, err := doc.GetString("value")
		if err != nil {
			t.Fatalf("GetString(\"value\"): %v", err)
		}
		if gotValue != "s3kr1t" {
			t.Fatalf("value = %q, want %q", gotValue, "s3kr1t")
		}
	})
}

package operators

import (
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// TestVaultOperatorSkipVaultPathComposition pins the fix for a vault path
// composed from another, skipped vault lookup (assets/vault/self-
// reference.yml; also exercised live, with a real stub Vault, by
// TestVaultOperatorPathComposedFromAnotherVaultLookup in
// op_vault_selfreference_test.go). Under graft.WithSkipVault(true) +
// graft.WithRedact(true) (redact mode - vaultinfo's own internal skip,
// and REDACT=1), a vault lookup's *document value* is still the flat
// literal "REDACTED" - byte-identical, unaffected by this fix - but a
// second (( vault ... )) call that builds its own path from a direct
// reference to that first lookup's tree node must render a symbolic
// "<path/to/secret:key>" instead of concatenating the literal word
// "REDACTED" into a corrupted path. Redact mode is what makes this
// scenario meaningful in the first place: without graft.WithRedact(true)
// (the --skip-vault CLI flag's own default), a skipped vault lookup
// defers itself instead (see op_skip_defer_test.go) and never produces a
// flat "REDACTED" value or a composed key to test here.
func TestVaultOperatorSkipVaultPathComposition(t *testing.T) {
	engine, err := graft.NewEngine(graft.WithSkipVault(true), graft.WithRedact(true))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	yamlDoc := "value: (( vault \"secret/paths:\" meta.path ))\nmeta:\n  path: (( vault \"secret/paths:root\" ))\n"
	doc, err := evaluateYAML(t, engine, yamlDoc)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	assertRedactedLeaf(t, doc, "meta.path")
	assertRedactedLeaf(t, doc, "value")
	assertComposedVaultKeyRecorded(t, engine, "secret/paths:<secret/paths:root>", "secret/paths:REDACTED")
}

// TestVaultOperatorSkipVaultPathCompositionReverseOrder pins that the
// composition above does not depend on meta.path being declared before
// value in the document - the dependency edge, not document order,
// drives evaluation order (mirroring
// TestVaultOperatorPathComposedFromAnotherVaultLookupReverseOrder's live
// counterpart).
func TestVaultOperatorSkipVaultPathCompositionReverseOrder(t *testing.T) {
	engine, err := graft.NewEngine(graft.WithSkipVault(true), graft.WithRedact(true))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	yamlDoc := "meta:\n  path: (( vault \"secret/paths:root\" ))\nvalue: (( vault \"secret/paths:\" meta.path ))\n"
	doc, err := evaluateYAML(t, engine, yamlDoc)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	assertRedactedLeaf(t, doc, "meta.path")
	assertRedactedLeaf(t, doc, "value")
	assertComposedVaultKeyRecorded(t, engine, "secret/paths:<secret/paths:root>", "secret/paths:REDACTED")
}

// TestVaultTryOperatorSkipVaultPathComposition covers vault-try's use of
// the same shared performVaultLookup (op_vault.go:534, called from both
// VaultOperator.tryVaultPaths and VaultTryOperator.Run at op_vault.go:726):
// a (( vault-try ... )) lookup that is itself skipped must be tracked the
// same way a (( vault ... )) lookup is, so a later (( vault ... )) call
// composing a path from vault-try's result also renders the symbolic
// form instead of the flat "REDACTED" text.
func TestVaultTryOperatorSkipVaultPathComposition(t *testing.T) {
	engine, err := graft.NewEngine(graft.WithSkipVault(true), graft.WithRedact(true))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	yamlDoc := "meta:\n  path: (( vault-try \"secret/paths:root\" \"fallback\" ))\nvalue: (( vault \"secret/paths:\" meta.path ))\n"
	doc, err := evaluateYAML(t, engine, yamlDoc)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	assertRedactedLeaf(t, doc, "meta.path")
	assertRedactedLeaf(t, doc, "value")
	assertComposedVaultKeyRecorded(t, engine, "secret/paths:<secret/paths:root>", "secret/paths:REDACTED")
}

// TestVaultOperatorSkipVaultGrabAliasComposition pins the same symbolic
// composition when the skipped lookup's tree node is routed through an
// intermediate (( grab )) alias (assets/vault/grab-alias.yml's
// tree_value shape): grab copies the flat sentinel, so it must also
// forward the source path's placeholder to its own tree path for the
// later vault-path-building reference to find.
func TestVaultOperatorSkipVaultGrabAliasComposition(t *testing.T) {
	engine, err := graft.NewEngine(graft.WithSkipVault(true), graft.WithRedact(true))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	yamlDoc := "meta:\n  path: (( vault \"secret/paths:root\" ))\nalias: (( grab meta.path ))\nvalue: (( vault \"secret/paths:\" alias ))\n"
	doc, err := evaluateYAML(t, engine, yamlDoc)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	assertRedactedLeaf(t, doc, "meta.path")
	assertRedactedLeaf(t, doc, "alias")
	assertRedactedLeaf(t, doc, "value")
	assertComposedVaultKeyRecorded(t, engine, "secret/paths:<secret/paths:root>", "secret/paths:REDACTED")
}

// TestVaultOperatorSkipVaultGrabAliasWholePath pins the alias-as-whole-
// path shape: (( vault alias )) with no literal segments at all must
// report the bare symbolic key, not the bare "REDACTED" text.
func TestVaultOperatorSkipVaultGrabAliasWholePath(t *testing.T) {
	engine, err := graft.NewEngine(graft.WithSkipVault(true), graft.WithRedact(true))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	yamlDoc := "meta:\n  path: (( vault \"secret/paths:root\" ))\nalias: (( grab meta.path ))\nvalue: (( vault alias ))\n"
	doc, err := evaluateYAML(t, engine, yamlDoc)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	assertRedactedLeaf(t, doc, "alias")
	assertRedactedLeaf(t, doc, "value")
	assertComposedVaultKeyRecorded(t, engine, "<secret/paths:root>", "REDACTED")
}

// TestVaultOperatorSkipVaultChainedGrabComposition pins that the
// placeholder forwarding is transitive: each grab in an alias chain
// records its own tree path, so the vault call at the end of the chain
// still composes symbolically. Ordering holds because each grab depends
// on its source (GrabOperator.Dependencies) and so runs after it.
func TestVaultOperatorSkipVaultChainedGrabComposition(t *testing.T) {
	engine, err := graft.NewEngine(graft.WithSkipVault(true), graft.WithRedact(true))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	yamlDoc := "meta:\n  path: (( vault \"secret/paths:root\" ))\na: (( grab meta.path ))\nb: (( grab a ))\nvalue: (( vault \"secret/paths:\" b ))\n"
	doc, err := evaluateYAML(t, engine, yamlDoc)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	assertRedactedLeaf(t, doc, "a")
	assertRedactedLeaf(t, doc, "b")
	assertRedactedLeaf(t, doc, "value")
	assertComposedVaultKeyRecorded(t, engine, "secret/paths:<secret/paths:root>", "secret/paths:REDACTED")
}

// TestVaultOperatorSkipVaultNestedGrabComposition pins the grab call
// nested directly inside the vault arguments:
// (( vault "secret/paths:" (grab meta.path) )). The symbolic probe must
// look through the OperatorCall to its reference argument. It also pins
// that the placeholder finally recorded for the vault op's own tree path
// is the composed key (performVaultLookup writes it last), not a stale
// entry a nested-grab evaluation might have left behind for that path.
func TestVaultOperatorSkipVaultNestedGrabComposition(t *testing.T) {
	engine, err := graft.NewEngine(graft.WithSkipVault(true), graft.WithRedact(true))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	yamlDoc := "meta:\n  path: (( vault \"secret/paths:root\" ))\nvalue: (( vault \"secret/paths:\" (grab meta.path) ))\n"
	doc, err := evaluateYAML(t, engine, yamlDoc)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	assertRedactedLeaf(t, doc, "meta.path")
	assertRedactedLeaf(t, doc, "value")
	assertComposedVaultKeyRecorded(t, engine, "secret/paths:<secret/paths:root>", "secret/paths:REDACTED")

	key, ok := engine.GetOperatorState().VaultPlaceholderFor("value")
	if !ok || key != "secret/paths:<secret/paths:root>" {
		t.Fatalf("VaultPlaceholderFor(\"value\") = %q, %v; want the composed key %q recorded last", key, ok, "secret/paths:<secret/paths:root>")
	}
}

// assertRedactedLeaf fails t unless doc's string value at path is the
// flat literal "REDACTED" - confirming a skip-vault sentinel's document
// value is unaffected by the symbolic-composition fix.
func assertRedactedLeaf(t *testing.T, doc graft.Document, path string) {
	t.Helper()
	got, err := doc.GetString(path)
	if err != nil {
		t.Fatalf("GetString(%q): %v", path, err)
	}
	if got != redactedValue {
		t.Fatalf("%s = %q, want %q (flat sentinel; document value must stay unaffected by the composition fix)", path, got, redactedValue)
	}
}

// assertComposedVaultKeyRecorded fails t unless engine's tracked vault
// refs (vaultinfo's data source) contain wantKey and do not contain
// wantAbsentKey - the pre-fix, corrupted composed key.
func assertComposedVaultKeyRecorded(t *testing.T, engine graft.Engine, wantKey, wantAbsentKey string) {
	t.Helper()
	refs := engine.GetOperatorState().GetVaultRefs()
	if _, ok := refs[wantKey]; !ok {
		t.Fatalf("GetVaultRefs() = %v, want a %q entry (symbolic composed path)", refs, wantKey)
	}
	if _, ok := refs[wantAbsentKey]; ok {
		t.Fatalf("GetVaultRefs() contains the corrupted flat key %q; the composition bug regressed", wantAbsentKey)
	}
}

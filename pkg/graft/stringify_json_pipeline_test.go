package graft_test

// Disposition for the "graft json leaves stringify markers unevaluated"
// report: graft json (pkg/graft/json.go's JSONifyFiles) is a pure
// YAML<->JSON format converter — it parses the given YAML directly
// (ParseYAML11CompatAware) and never runs the graft operator evaluator at
// all. This is BY DESIGN and matches the reference spruce binary
// byte-for-byte (`spruce json` on raw, unevaluated source also never
// resolves `(( ... ))` markers — confirmed against spruce v1.35.16), and
// matches graft's own documented usage (docs/user-guide/cli/json.md's
// "With Merge" section: `graft merge base.yml overlay.yml | graft json`).
// Running `graft json` directly on unevaluated source was never expected
// to evaluate operators — the original report compared that against
// `graft merge`'s own YAML output, which does evaluate, an apples-to-
// oranges comparison rather than a json-pipeline-specific gap.
//
// The actual, reproducible symptom (stringify-heavy keys showing
// unresolved marker text even through the documented merge-then-json
// pipeline) shares its root cause with the stringify subtree-dependency
// bug (see stringify_dependency_test.go): once StringifyOperator.Dependencies
// correctly declares the opcalls under its target subtree, `graft merge`
// itself produces fully-resolved output, and json — being a faithful
// converter — carries that resolved content straight through with no
// separate fix needed here.
//
// This test pins that path end to end: merge the exact repro through the
// library API (exercising the now-fixed stringify Dependencies), marshal
// the merged document the same way the CLI does before handing it to
// `graft json`, and confirm the JSON conversion carries the fully
// resolved content through with no `((` marker surviving.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
	_ "github.com/fivetwenty-io/graft/pkg/graft/operators"
)

func TestStringifyThroughJSONPipeline(t *testing.T) {
	src := "services:\n  a: 1\n  b: 2\nenv:\n  config:\n    services: (( grab services ))\n    other: hello\n  final: (( stringify env.config ))\n"

	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	doc, err := engine.ParseYAML([]byte(src))
	if err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}
	merged, err := engine.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatalf("unexpected merge error: %v", err)
	}

	yamlBytes, err := graft.MarshalYAML(merged.RawData())
	if err != nil {
		t.Fatalf("failed to marshal merged document to YAML: %v", err)
	}

	// Mirrors the documented pipeline: `graft merge ... | graft json`
	// hands json.go's JSONifyFiles the already-evaluated YAML, either via
	// a file or stdin. Using a temp file here exercises the exact same
	// JSONifyFiles/jsonifyData code path the CLI's `graft json <file>`
	// invocation does.
	tmpFile := filepath.Join(t.TempDir(), "merged.yml")
	if err := os.WriteFile(tmpFile, yamlBytes, 0o600); err != nil {
		t.Fatalf("failed to write temp merged YAML: %v", err)
	}

	lines, err := graft.JSONifyFiles([]string{tmpFile}, false)
	if err != nil {
		t.Fatalf("unexpected JSONifyFiles error: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("expected exactly one JSON line, got %d: %v", len(lines), lines)
	}

	if strings.Contains(lines[0], "((") {
		t.Fatalf("JSON output still contains an unresolved marker: %s", lines[0])
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &decoded); err != nil {
		t.Fatalf("failed to decode JSON output: %v", err)
	}
	env, ok := decoded["env"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected env to be an object, got %T", decoded["env"])
	}
	final, ok := env["final"].(string)
	if !ok {
		t.Fatalf("expected env.final to be a string, got %T", env["final"])
	}
	for _, want := range []string{"other: hello", "a: 1", "b: 2"} {
		if !strings.Contains(final, want) {
			t.Fatalf("env.final missing expected resolved content %q: %q", want, final)
		}
	}
}

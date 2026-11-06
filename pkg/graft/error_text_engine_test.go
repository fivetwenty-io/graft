package graft_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// TestCycleErrorTextHoldsNoExpression closes a cycle through an operator
// whose expression holds the probe. The CLI prints each operator's
// expression on the cycle, and Error() names only the paths.
func TestCycleErrorTextHoldsNoExpression(t *testing.T) {
	withColorOn(t)
	for _, mode := range leakModes {
		t.Run(mode.name, func(t *testing.T) {
			eng, _ := leakEngine(t, mode.parallel)
			doc, err := eng.ParseYAML([]byte("a: (( concat \"PROBEzq81x\" b ))\nb: (( grab a ))\n"))
			if err != nil {
				t.Fatal(err)
			}
			_, err = eng.Merge(context.Background(), doc).Execute()
			assertErrorText(t, err, errProbe, true)
			if !errors.Is(err, graft.ErrDependencyCycle) {
				t.Fatalf("the cycle sentinel is gone: %v", err)
			}
		})
	}
}

// probeVault stands in for a Vault server. A path holding the probe
// answers 500, secret/num holds a number, and every other path is
// missing.
func probeVault(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, errProbe):
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"errors": ["internal failure"]}`))
		case r.URL.Path == "/v1/secret/num":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data": {"PROBEzq81x": 4079}}`))
		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors": []}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestVaultBackendErrorTextHoldsNoPath fails each lookup a WithVault
// backend can fail, with the ansi toggle on.
func TestVaultBackendErrorTextHoldsNoPath(t *testing.T) {
	withColorOn(t)
	srv := probeVault(t)
	cases := []errorTextCase{
		{name: "server failure", out: `(( vault "secret/PROBEzq81x:k" ))`, detailed: true},
		{name: "invalid path", out: `(( vault "PROBEzq81x" ))`, detailed: true},
		{name: "missing secret", out: `(( vault "secret/missing:PROBEzq81x" ))`, detailed: true},
		{name: "value not a string", out: `(( vault "secret/num:PROBEzq81x" ))`, detailed: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eng, err := graft.NewEngine(
				graft.WithBackendRegistry(true),
				graft.WithVault(graft.VaultConfig{Address: srv.URL, Token: "t"}),
			)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := eng.ParseYAML([]byte(tc.document()))
			if err != nil {
				t.Fatal(err)
			}
			_, err = eng.Merge(context.Background(), doc).Execute()
			assertErrorText(t, err, tc.value(), tc.detailed)
		})
	}
}

// TestVaultBackendGetErrorTextHoldsNoPath calls the WithVault backend
// directly, the way a library caller does, and checks that a missing
// secret still reports ErrBackendNotFound.
func TestVaultBackendGetErrorTextHoldsNoPath(t *testing.T) {
	srv := probeVault(t)
	eng, err := graft.NewEngine(
		graft.WithBackendRegistry(true),
		graft.WithVault(graft.VaultConfig{Address: srv.URL, Token: "t"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	backend, ok := eng.GetBackend("vault")
	if !ok {
		t.Fatal("no vault backend")
	}
	details := map[string]string{
		"secret/missing:PROBEzq81x": "backend: path not found: secret/missing:PROBEzq81x",
		"secret/num:PROBEzq81x":     `vault: value at "secret/num:PROBEzq81x" is not a string (got json.Number)`,
		"PROBEzq81x":                `vault: invalid path "PROBEzq81x": must be in the form path/to/secret:key`,
		"secret/PROBEzq81x:k":       `vault: reading "secret/PROBEzq81x": Error making API request.`,
	}
	for path, want := range details {
		_, err := backend.Get(context.Background(), path)
		assertErrorText(t, err, errProbe, true)
		if got := graft.DetailedError(err); !strings.HasPrefix(got, want) {
			t.Errorf("DetailedError = %q, want the text the CLI has always printed, %q", got, want)
		}
		if path == "secret/missing:PROBEzq81x" && !errors.Is(err, graft.ErrBackendNotFound) {
			t.Fatalf("a missing secret no longer reports ErrBackendNotFound: %v", err)
		}
	}
}

// TestPartialEvaluationErrorHoldsNoSiblingValue fails a vault lookup and
// a grab whose siblings hold the probe. The aggregated error names the
// failing paths, and the path the grab asked for, and quotes nothing
// else: no sibling value, no source snippet, and no context line.
func TestPartialEvaluationErrorHoldsNoSiblingValue(t *testing.T) {
	withColorOn(t)
	doc := `meta:
  s: PROBEzq81x
creds:
  user: PROBEzq81x
  pass: (( vault "secret/creds:pass" ))
  note: PROBEzq81x
refs:
  before: PROBEzq81x
  missing: (( grab meta.nope ))
  after: PROBEzq81x
`
	for _, mode := range leakModes {
		t.Run(mode.name, func(t *testing.T) {
			eng, _ := leakEngine(t, mode.parallel)
			parsed, err := eng.ParseYAML([]byte(doc))
			if err != nil {
				t.Fatal(err)
			}
			_, err = eng.Merge(context.Background(), parsed).Execute()
			var partial *graft.PartialEvaluationError
			if !errors.As(err, &partial) {
				t.Fatalf("want a PartialEvaluationError, got %T: %v", err, err)
			}
			assertErrorText(t, err, errProbe, false)
			plain := err.Error()
			for _, want := range []string{"$.creds.pass", "$.refs.missing"} {
				if !strings.Contains(plain, want) {
					t.Errorf("Error() does not name %s:\n%s", want, plain)
				}
			}
			for _, snippet := range []string{"secret/creds", "^", "(("} {
				if strings.Contains(plain, snippet) {
					t.Errorf("Error() quotes source text %q:\n%s", snippet, plain)
				}
			}
		})
	}
}

// parseErrorCases are documents the parser refuses. Where the CLI has
// always printed the source lines around the failure, the probe is in
// them, and Error() must leave them out.
var parseErrorCases = []errorTextCase{
	{name: "unclosed flow sequence", doc: "a: [PROBEzq81x\n", detailed: true},
	{name: "unclosed quote", doc: "a: \"PROBEzq81x\n", detailed: true},
	{name: "tab indent", doc: "a: b\n\tc: PROBEzq81x\n", detailed: true},
	{name: "mapping in a scalar", doc: "a: PROBEzq81x\n  b: c\n", detailed: true},
	{name: "unknown alias", doc: "a: *PROBEzq81x\n", detailed: true},
	// graft's own merge-key check fails this one, as spruce does, and
	// its error carries a line but no source lines.
	{name: "merge of a scalar", doc: "a: &x PROBEzq81x\nb: {<<: *x}\n"},
	{name: "duplicate key", doc: "a: 1\na: PROBEzq81x\n", detailed: true},
	{name: "line before the failure", doc: "a: |\n PROBEzq81x\n  x\n- y\n", detailed: true},
	{name: "tag mismatch", doc: "x: 1\na: !!int PROBEzq81x\n"},
	{name: "list root", doc: "- PROBEzq81x\n"},
	{name: "two documents after an end marker", doc: "a: PROBEzq81x\n...\nb: [\n"},
}

// TestParseErrorTextHoldsNoSource fails ParseYAML and ParseGoPatch with
// the ansi toggle on, and checks the error's two texts.
func TestParseErrorTextHoldsNoSource(t *testing.T) {
	withColorOn(t)
	eng, err := graft.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range parseErrorCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := eng.ParseYAML([]byte(tc.document()))
			assertErrorText(t, err, tc.value(), tc.detailed)
		})
	}

	goPatch := []errorTextCase{
		{name: "go-patch syntax", doc: "- type: replace\n  path: /a\n  value: [PROBEzq81x\n", detailed: true},
		{name: "go-patch unknown op", doc: "- type: PROBEzq81x\n  path: /a\n", detailed: true},
		{name: "go-patch bad value", doc: "- type: replace\n  path: /PROBEzq81x\n", detailed: true},
	}
	for _, tc := range goPatch {
		t.Run(tc.name, func(t *testing.T) {
			_, err := graft.ParseGoPatch([]byte(tc.document()))
			assertErrorText(t, err, tc.value(), tc.detailed)
		})
	}
}

// TestEvaluatorErrorTextHoldsNoValueOrColor fails the evaluator's own
// checks with the ansi toggle on.
func TestEvaluatorErrorTextHoldsNoValueOrColor(t *testing.T) {
	withColorOn(t)
	eng, err := graft.NewEngine()
	if err != nil {
		t.Fatal(err)
	}

	t.Run("duplicate entry name", func(t *testing.T) {
		doc, err := eng.ParseYAML([]byte("meta: {v: x}\njobs:\n  - name: PROBEzq81x\n    cmd: (( grab meta.v ))\n  - name: PROBEzq81x\n    cmd: (( grab meta.v ))\n"))
		if err != nil {
			t.Fatal(err)
		}
		_, err = eng.Merge(context.Background(), doc).Execute()
		assertErrorText(t, err, errProbe, true)
	})
}

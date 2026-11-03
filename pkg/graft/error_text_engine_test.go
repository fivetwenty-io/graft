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
// TestParseErrorTextHoldsNoSource fails ParseYAML and ParseGoPatch with
// the ansi toggle on, and checks the error's two texts.
// TestEvaluatorErrorTextHoldsNoValueOrColor fails the evaluator's own
// checks with the ansi toggle on.

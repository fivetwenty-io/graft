package operators

import (
	"context"
	"fmt"
	"strings"
	"testing"

	vaultbackend "github.com/fivetwenty-io/graft/internal/backends/vault"
	"github.com/fivetwenty-io/graft/internal/utils/ansi"
	"github.com/fivetwenty-io/graft/pkg/graft"
)

// failingReader answers every read the way the Vault API client does when
// the server fails: with an error that quotes the request URL.
type failingReader struct{}

func (failingReader) ReadSecret(_ context.Context, path string) (map[string]interface{}, error) {
	return nil, fmt.Errorf("Error making API request.\n\nURL: GET http://vault.example/v1/%s\nCode: 500. Errors:\n\n* internal failure", path)
}

// numberReader holds every secret with a number in each subkey.
type numberReader struct{}

func (numberReader) ReadSecret(_ context.Context, _ string) (map[string]interface{}, error) {
	return map[string]interface{}{"PROBEzq81x": 4079}, nil
}

// TestBuiltInVaultErrorTextHoldsNoPath fails the built-in vault lookup
// each way it can fail, with the ansi toggle on. Error() holds neither
// the secret path nor color, and the detailed text keeps both.
func TestBuiltInVaultErrorTextHoldsNoPath(t *testing.T) {
	was := ansi.IsColorEnabled()
	ansi.Color(true)
	defer ansi.Color(was)

	cases := []struct {
		name   string
		reader vaultbackend.Reader
		expr   string
	}{
		{"server failure", failingReader{}, `(( vault "secret/PROBEzq81x:k" ))`},
		{"missing subkey", missingSubkeyReader{}, `(( vault "secret/x:PROBEzq81x" ))`},
		{"value not a string", numberReader{}, `(( vault "secret/x:PROBEzq81x" ))`},
		{"missing secret", notFoundReader{}, `(( vault "secret/PROBEzq81x:k" ))`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vaultbackend.SecretCache.Reset()
			withGlobalVaultReader(tc.reader, func() {
				engine, err := graft.NewEngine()
				if err != nil {
					t.Fatal(err)
				}
				doc, err := engine.ParseYAML([]byte("out: " + tc.expr + "\n"))
				if err != nil {
					t.Fatal(err)
				}
				_, err = engine.Evaluate(context.Background(), doc)
				if err == nil {
					t.Fatal("the lookup must fail")
				}
				plain := err.Error()
				if strings.Contains(plain, "PROBEzq81x") || strings.Contains(plain, "\x1b[") {
					t.Fatalf("Error() quotes the path or holds color:\n%q", plain)
				}
				if !strings.Contains(graft.DetailedError(err), "PROBEzq81x") {
					t.Fatalf("DetailedError dropped the path:\n%s", graft.DetailedError(err))
				}
			})
		})
	}
}

// TestVaultTryStillFallsBackOnAMissingSubkey keeps the not-found check
// working now that the missing-subkey error's own text is plain.
func TestVaultTryStillFallsBackOnAMissingSubkey(t *testing.T) {
	vaultbackend.SecretCache.Reset()
	withGlobalVaultReader(missingSubkeyReader{}, func() {
		engine, err := graft.NewEngine()
		if err != nil {
			t.Fatal(err)
		}
		doc, err := engine.ParseYAML([]byte(`out: (( vault-try "secret/x:k" "fallback" ))` + "\n"))
		if err != nil {
			t.Fatal(err)
		}
		out, err := engine.Evaluate(context.Background(), doc)
		if err != nil {
			t.Fatalf("vault-try failed: %v", err)
		}
		if got, _ := out.GetString("out"); got != "fallback" {
			t.Fatalf("out = %q, want the fallback", got)
		}
	})
}

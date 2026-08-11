package operators

import (
	"context"
	"sync"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	vaultbackend "github.com/fivetwenty-io/graft/internal/backends/vault"
	"github.com/fivetwenty-io/graft/internal/utils/ansi"
	"github.com/fivetwenty-io/graft/pkg/graft"
)

// countingVaultReader is a vaultbackend.VaultReader stub that records how many
// times ReadSecret was invoked and by which path, so tests can assert on
// backend-call counts (caching behavior, REDACT short-circuiting) without a
// real Vault server.
type countingVaultReader struct {
	mu      sync.Mutex
	calls   int
	paths   []string
	secrets map[string]map[string]interface{}
}

func (r *countingVaultReader) ReadSecret(_ context.Context, path string) (map[string]interface{}, error) {
	r.mu.Lock()
	r.calls++
	r.paths = append(r.paths, path)
	r.mu.Unlock()

	if secret, ok := r.secrets[path]; ok {
		return secret, nil
	}
	return nil, &vaultbackend.ErrNotFound{Path: path}
}

func (r *countingVaultReader) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// TestVaultOperatorKeyParseErrorText locks in that graft's malformed vault
// argument error (path missing a ":key" suffix) matches spruce's op_vault.go
// wording byte-for-byte: "invalid argument %s; must be in the form
// path/to/secret:key". The path-parse failure must also happen before any
// backend call is attempted.
func TestVaultOperatorKeyParseErrorText(t *testing.T) {
	previousColor := ansi.IsColorEnabled()
	ansi.Color(false)
	defer ansi.Color(previousColor)

	Convey("(( vault ... )) argument without a ':key' suffix", t, func() {
		reader := &countingVaultReader{secrets: map[string]map[string]interface{}{}}

		withGlobalVaultReader(reader, func() {
			engine, err := graft.NewEngine()
			So(err, ShouldBeNil)

			doc, err := engine.ParseYAML([]byte(`secret: (( vault "noColonHere" ))` + "\n"))
			So(err, ShouldBeNil)

			_, evalErr := engine.Evaluate(context.Background(), doc)
			So(evalErr, ShouldNotBeNil)

			So(extractOperatorErrorMessage(evalErr), ShouldEqual,
				"invalid argument noColonHere; must be in the form path/to/secret:key")

			So(reader.callCount(), ShouldEqual, 0)
		})
	})

	Convey("(( vault ... )) argument that is only a ':key' suffix", t, func() {
		reader := &countingVaultReader{secrets: map[string]map[string]interface{}{}}

		withGlobalVaultReader(reader, func() {
			engine, err := graft.NewEngine()
			So(err, ShouldBeNil)

			doc, err := engine.ParseYAML([]byte(`secret: (( vault ":key" ))` + "\n"))
			So(err, ShouldBeNil)

			_, evalErr := engine.Evaluate(context.Background(), doc)
			So(evalErr, ShouldNotBeNil)

			So(extractOperatorErrorMessage(evalErr), ShouldEqual,
				"invalid argument :key; must be in the form path/to/secret:key")

			So(reader.callCount(), ShouldEqual, 0)
		})
	})
}

// TestVaultOperatorCachesPerSecretPath locks in that graft's vault operator
// performs exactly one backend call per distinct secret path per run,
// matching spruce's vaultSecretCache behavior: fetching two different
// subkeys from the same secret path must hit the backend only once.
func TestVaultOperatorCachesPerSecretPath(t *testing.T) {
	const path = "secret/parity-cache-test/shared"

	vaultbackend.SecretCache.Reset()
	defer vaultbackend.SecretCache.Reset()

	reader := &countingVaultReader{
		secrets: map[string]map[string]interface{}{
			path: {
				"one": "value-one",
				"two": "value-two",
			},
		},
	}

	Convey("two references to the same secret path with different subkeys", t, func() {
		withGlobalVaultReader(reader, func() {
			engine, err := graft.NewEngine()
			So(err, ShouldBeNil)

			yamlInput := []byte(`
first: (( vault "` + path + `:one" ))
second: (( vault "` + path + `:two" ))
`)
			doc, err := engine.ParseYAML(yamlInput)
			So(err, ShouldBeNil)

			result, err := engine.Evaluate(context.Background(), doc)
			So(err, ShouldBeNil)

			first, getErr := result.Get("first")
			So(getErr, ShouldBeNil)
			So(first, ShouldEqual, "value-one")

			second, getErr := result.Get("second")
			So(getErr, ShouldBeNil)
			So(second, ShouldEqual, "value-two")

			So(reader.callCount(), ShouldEqual, 1)
			So(reader.paths, ShouldResemble, []string{path})
		})
	})
}

// TestVaultOperatorSkipVaultNoBackendCall locks in that once vault lookups
// are skipped (the REDACT environment variable, wired through
// DefaultEngine.evaluate -> OperatorState.SetSkipVault), the vault operator
// returns the literal "REDACTED" without making any backend call at all —
// not even a cache lookup that falls through to the network.
func TestVaultOperatorSkipVaultNoBackendCall(t *testing.T) {
	vaultbackend.SecretCache.Reset()
	defer vaultbackend.SecretCache.Reset()

	Convey("REDACT is active", t, func() {
		t.Setenv("REDACT", "1")

		reader := &countingVaultReader{
			secrets: map[string]map[string]interface{}{
				"secret/parity-redact-test": {"password": "never-fetched"},
			},
		}

		withGlobalVaultReader(reader, func() {
			engine, err := graft.NewEngine()
			So(err, ShouldBeNil)

			doc, err := engine.ParseYAML([]byte(`secret: (( vault "secret/parity-redact-test:password" ))` + "\n"))
			So(err, ShouldBeNil)

			result, err := engine.Evaluate(context.Background(), doc)
			So(err, ShouldBeNil)

			val, getErr := result.Get("secret")
			So(getErr, ShouldBeNil)
			So(val, ShouldEqual, "REDACTED")

			So(reader.callCount(), ShouldEqual, 0)
		})
	})
}

package operators

import (
	"context"
	"errors"
	"regexp"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	vaultbackend "github.com/fivetwenty-io/graft/internal/backends/vault"
	"github.com/fivetwenty-io/graft/internal/utils/ansi"
	"github.com/fivetwenty-io/graft/pkg/graft"
)

// genesisSecretNotFoundRe mirrors genesis's ManifestProvider.pm regex used to
// detect a vault-secret-not-found error inside a stderr error line:
//
//	/^secret (.*) not found/
//
// Genesis short-circuits its retry-with-prune loop on this exact substring,
// so graft's vault error text must match it byte-for-byte.
var genesisSecretNotFoundRe = regexp.MustCompile(`^secret (.*) not found$`)

// notFoundReader is a minimal vaultbackend.VaultReader stub that reports every
// path as missing, mirroring a real Vault 404 response.
type notFoundReader struct{}

func (notFoundReader) ReadSecret(_ context.Context, path string) (map[string]interface{}, error) {
	return nil, &vaultbackend.ErrNotFound{Path: path}
}

// missingSubkeyReader is a minimal vaultbackend.VaultReader stub whose secret
// exists but never contains the subkey the test asks for.
type missingSubkeyReader struct{}

func (missingSubkeyReader) ReadSecret(_ context.Context, _ string) (map[string]interface{}, error) {
	return map[string]interface{}{"unrelated-key": "value"}, nil
}

// withGlobalVaultReader swaps vaultbackend.GlobalReader for the duration of fn,
// restoring the previous value afterward so other tests aren't affected.
func withGlobalVaultReader(reader vaultbackend.VaultReader, fn func()) {
	previous := vaultbackend.GlobalReader
	vaultbackend.GlobalReader = reader
	defer func() { vaultbackend.GlobalReader = previous }()
	fn()
}

func TestVaultOperatorNotFoundErrorText(t *testing.T) {
	// main.go's default ("auto") color handling disables ANSI color whenever
	// stderr isn't a tty (isatty.IsTerminal(os.Stderr.Fd())) before any
	// operator runs. Genesis always captures graft's stderr to a tempfile,
	// never a tty, so this is the color state genesis actually observes;
	// reproduce it here rather than relying on package-level Convey ordering.
	previousColor := ansi.IsColorEnabled()
	ansi.Color(false)
	defer ansi.Color(previousColor)

	Convey("(( vault ... )) not-found error text matches genesis's expected format", t, func() {
		Convey("secret path does not exist in Vault", func() {
			withGlobalVaultReader(notFoundReader{}, func() {
				engine, err := graft.NewEngine()
				So(err, ShouldBeNil)

				doc, err := engine.ParseYAML([]byte(`secret: (( vault "secret/e:noent" ))` + "\n"))
				So(err, ShouldBeNil)

				_, evalErr := engine.Evaluate(context.Background(), doc)
				So(evalErr, ShouldNotBeNil)

				match := genesisSecretNotFoundRe.FindStringSubmatch(extractOperatorErrorMessage(evalErr))
				So(match, ShouldNotBeNil)
				So(match[1], ShouldEqual, "secret/e:noent")
			})
		})

		Convey("secret path exists but the requested subkey does not", func() {
			withGlobalVaultReader(missingSubkeyReader{}, func() {
				engine, err := graft.NewEngine()
				So(err, ShouldBeNil)

				doc, err := engine.ParseYAML([]byte(`secret: (( vault "secret/hand:shake" ))` + "\n"))
				So(err, ShouldBeNil)

				_, evalErr := engine.Evaluate(context.Background(), doc)
				So(evalErr, ShouldNotBeNil)

				match := genesisSecretNotFoundRe.FindStringSubmatch(extractOperatorErrorMessage(evalErr))
				So(match, ShouldNotBeNil)
				So(match[1], ShouldEqual, "secret/hand:shake")
			})
		})
	})
}

// extractOperatorErrorMessage pulls the innermost error message out of
// whatever wrapper graft.Evaluate returns (MultiError, path-annotated line,
// or a bare error), leaving just the operator's own message text so tests
// can assert on it without depending on the "- $.path: " line-framing that
// wraps it for stderr output.
func extractOperatorErrorMessage(err error) string {
	var multi graft.MultiError
	if ok := asMultiError(err, &multi); ok && len(multi.Errors) > 0 {
		err = multi.Errors[0]
	}

	msg := err.Error()

	// Strip a leading "$.path: " or " - $.path: " annotation if present, so
	// the assertion only covers the operator's own message text.
	if loc := regexp.MustCompile(`^\s*-?\s*\$\.[^:]*:\s*`).FindStringIndex(msg); loc != nil {
		msg = msg[loc[1]:]
	}

	return msg
}

func asMultiError(err error, target *graft.MultiError) bool {
	var me graft.MultiError
	if errors.As(err, &me) {
		*target = me
		return true
	}
	var mep *graft.MultiError
	if errors.As(err, &mep) {
		*target = *mep
		return true
	}
	return false
}

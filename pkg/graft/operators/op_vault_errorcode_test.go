package operators

import (
	"context"
	"errors"
	"testing"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
	"github.com/fivetwenty-io/graft/pkg/graft"
)

// TestVaultSecretNotFoundClassifiesAsCodeSecretNotFound exercises the same
// end-to-end path as TestVaultOperatorNotFoundErrorText
// (op_vault_notfound_test.go): a real (( vault ... )) evaluation against a
// reader that reports every path missing. It additionally asserts the
// resulting error carries graft.CodeSecretNotFound, and that the default
// (GRAFT_ERROR_CODES unset) error text is byte-identical to the plain
// "secret <key> not found" wording genesis depends on — tagging the error
// with WithCode must not change what Error() returns.
func TestVaultSecretNotFoundClassifiesAsCodeSecretNotFound(t *testing.T) {
	previousColor := ansi.IsColorEnabled()
	ansi.Color(false)
	defer ansi.Color(previousColor)

	withGlobalVaultReader(notFoundReader{}, func() {
		engine, err := graft.NewEngine()
		if err != nil {
			t.Fatalf("failed to create engine: %v", err)
		}

		doc, err := engine.ParseYAML([]byte(`secret: (( vault "secret/e:noent" ))` + "\n"))
		if err != nil {
			t.Fatalf("failed to parse YAML: %v", err)
		}

		_, evalErr := engine.Evaluate(context.Background(), doc)
		if evalErr == nil {
			t.Fatalf("expected an evaluation error, got none")
		}

		msg := extractOperatorErrorMessage(evalErr)
		if want := "secret secret/e:noent not found"; msg != want {
			t.Fatalf("operator error message = %q, want %q (tagging must not alter Error())", msg, want)
		}

		var multi graft.MultiError
		if !asMultiError(evalErr, &multi) || len(multi.Errors) == 0 {
			t.Fatalf("expected a graft.MultiError with at least one error, got %T: %v", evalErr, evalErr)
		}

		var pe *graft.PathError
		if !errors.As(multi.Errors[0], &pe) {
			t.Fatalf("expected a *graft.PathError, got %T: %v", multi.Errors[0], multi.Errors[0])
		}
		if pe.Code() != graft.CodeSecretNotFound {
			t.Fatalf("Code() = %q, want %q", pe.Code(), graft.CodeSecretNotFound)
		}
	})
}

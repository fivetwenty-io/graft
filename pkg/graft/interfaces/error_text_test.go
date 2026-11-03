package interfaces

import (
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/errtext"
)

// TestSyntaxErrorTextLeavesOutTheToken checks that an unexpected-token
// error names what was expected without quoting the token it found.
func TestSyntaxErrorTextLeavesOutTheToken(t *testing.T) {
	err := NewExpectedTokenError([]string{"IDENT", "STRING"}, `"PROBEzq81x"`, Position{Line: 1, Column: 4}, `(( "PROBEzq81x" ))`)
	if got, want := err.Error(), "syntax error at line 1, column 4: expected IDENT or STRING"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	want := `syntax error at line 1, column 4: expected IDENT or STRING, got "PROBEzq81x"`
	if got := errtext.Detail(err); got != want {
		t.Fatalf("detail = %q, want %q", got, want)
	}
}

// TestScannerNumberErrorsLeaveOutTheNumber checks that a number the
// scanner cannot parse stays out of the error it records.
func TestScannerNumberErrorsLeaveOutTheNumber(t *testing.T) {
	for _, src := range []string{"1e99999", "0x7fffffffffffffffff"} {
		s := NewScanner(src)
		for tok := s.Scan(); tok.Type != TokenEOF; tok = s.Scan() {
			if strings.Contains(tok.Literal, "fffff") || (tok.Type == TokenInvalid && strings.Contains(tok.Literal, "99999")) {
				t.Errorf("%s: token literal quotes the number: %q", src, tok.Literal)
			}
		}
		errs := s.Errors()
		if len(errs) == 0 {
			t.Fatalf("%s: the scanner must record an error", src)
		}
		for _, err := range errs {
			number := strings.TrimPrefix(src, "0x")
			if strings.Contains(err.Error(), number) {
				t.Errorf("%s: Error() quotes the number: %q", src, err.Error())
			}
			if !strings.Contains(errtext.Detail(err), number) {
				t.Errorf("%s: detail lacks the number: %q", src, errtext.Detail(err))
			}
		}
	}
}

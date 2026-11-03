package tree

import (
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/errtext"
	"github.com/fivetwenty-io/graft/internal/utils/ansi"
)

const probe = "PROBE-tree-7"

func withColor(t *testing.T, on bool) {
	t.Helper()
	was := ansi.IsColorEnabled()
	ansi.Color(on)
	t.Cleanup(func() { ansi.Color(was) })
}

// TestErrorTextIsPlainAndValueFree checks each tree error with the ansi
// toggle on. Error() holds no escape code and no value, and the detailed
// text is the colored text the CLI has always printed.
func TestErrorTextIsPlainAndValueFree(t *testing.T) {
	withColor(t, true)
	cases := []struct {
		name   string
		err    error
		plain  string
		detail string
	}{
		{
			name:   "type mismatch with a value",
			err:    TypeMismatchError{Path: []string{"meta", "k"}, Wanted: wantedContainer, Got: gotScalar, Value: probe},
			plain:  "$.meta.k is a scalar (not a map or a list)",
			detail: ansi.Sprintf("@c{$.%s} @R{[=%v] is %s (not} @m{%s}@R{)}", "meta.k", probe, gotScalar, wantedContainer),
		},
		{
			name:   "type mismatch without a value",
			err:    TypeMismatchError{Path: []string{"meta", "k"}, Wanted: wantedContainer, Got: gotScalar},
			plain:  "$.meta.k is a scalar (not a map or a list)",
			detail: ansi.Sprintf("@C{$.%s} @R{is %s (not} @m{%s}@R{)}", "meta.k", gotScalar, wantedContainer),
		},
		{
			name:   "type mismatch without a found type",
			err:    TypeMismatchError{Path: []string{"meta", "k"}, Wanted: "a map"},
			plain:  "meta.k is not a map",
			detail: ansi.Sprintf("@c{%s} @R{is not} @m{%s}", "meta.k", "a map"),
		},
		{
			name:   "not found",
			err:    NotFoundError{Path: []string{"meta", "k"}},
			plain:  "`$.meta.k` could not be found in the datastructure",
			detail: ansi.Sprintf("@R{`}@c{$.%s}@R{` could not be found in the datastructure}", "meta.k"),
		},
		{
			name:   "number that is not an integer",
			err:    func() error { _, err := Number(2.5).Int64(); return err }(),
			plain:  "the number does not represent an integer, cannot auto-convert",
			detail: "2.500000 does not represent an integer, cannot auto-convert",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.err.Error()
			if got != tc.plain {
				t.Errorf("Error() = %q, want %q", got, tc.plain)
			}
			if strings.Contains(got, "\x1b[") || strings.Contains(got, probe) {
				t.Errorf("Error() leaks color or a value: %q", got)
			}
			if d := errtext.Detail(tc.err); d != tc.detail {
				t.Errorf("detail = %q, want %q", d, tc.detail)
			}
		})
	}
}

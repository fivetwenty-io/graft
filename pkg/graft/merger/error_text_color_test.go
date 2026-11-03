package merger

import (
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/errtext"
	"github.com/fivetwenty-io/graft/internal/utils/ansi"
)

// TestMergeErrorTextHoldsNoColor merges a (( merge )) and a (( delete ))
// outside a list with the ansi toggle on. Error(), and the text of every
// error on its Unwrap chain, must be free of escape codes, and the
// detailed text keeps its color.
func TestMergeErrorTextHoldsNoColor(t *testing.T) {
	was := ansi.IsColorEnabled()
	ansi.Color(true)
	t.Cleanup(func() { ansi.Color(was) })

	tests := []struct {
		name string
		val  string
		want string
	}{
		{name: "merge", val: "(( merge || nil ))", want: "inappropriate use of (( merge )) operator outside of a list"},
		{name: "delete", val: `(( delete "a" ))`, want: "inappropriate use of (( delete )) operator outside of a list"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := &Merger{}
			m.MergeObj(map[string]interface{}{}, map[string]interface{}{"key": tc.val}, "node")
			err := m.Error()
			if err == nil {
				t.Fatal("the merge must fail")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Error() is not the expected message:\n%q", err.Error())
			}
			var walk func(error)
			walk = func(e error) {
				if e == nil {
					return
				}
				if strings.Contains(e.Error(), "\x1b[") {
					t.Errorf("a %T on the chain holds an escape code:\n%q", e, e.Error())
				}
				switch u := e.(type) { //nolint:errorlint // the chain is walked one level at a time
				case interface{ Unwrap() error }:
					walk(u.Unwrap())
				case interface{ Unwrap() []error }:
					for _, child := range u.Unwrap() {
						walk(child)
					}
				}
			}
			walk(err)
			if !strings.Contains(errtext.Detail(err), "\x1b[") {
				t.Errorf("the detailed text lost its color:\n%q", errtext.Detail(err))
			}
		})
	}
}

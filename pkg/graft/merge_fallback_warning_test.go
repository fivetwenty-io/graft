package graft

import (
	"context"
	"fmt"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
	"github.com/fivetwenty-io/graft/log"
)

// TestDefaultArrayMergeFallbackWarning pins graft's stderr warning output for
// spruce's default array-merge fallback (key-merge -> inline) to match
// spruce's merge.go behavior, verified against the spruce binary:
//
//	warning: $.<path>: <object> object's key '<key>' cannot have a value
//	  which is a hash or sequence - cannot merge by key
//	warning: Falling back to inline merge strategy
//
// spruce emits one such warning pair per document-merge step in which the
// fallback triggers (including the very first document, when it is merged
// into the initial empty root) - not just for overlay documents merged
// after the first. The warning line must never match the CLI error-line
// regex (^ - \$\.), which genesis' stderr scraping depends on.
func TestDefaultArrayMergeFallbackWarning(t *testing.T) {
	Convey("Default array-merge fallback emits an equivalent stderr warning", t, func() {
		ansi.Color(false)
		defer ansi.Color(true)

		var captured []string
		original := log.PrintStdErrf
		log.PrintStdErrf = func(format string, args ...interface{}) {
			captured = append(captured, fmt.Sprintf(format, args...))
		}
		defer func() { log.PrintStdErrf = original }()

		engine, err := NewEngine()
		So(err, ShouldBeNil)

		// list.1's "name" key holds a map in the first document and a map in
		// the second, so canKeyMergeArray fails for both "original" and "new"
		// across the two merge steps (doc0 alone against an empty base, then
		// doc1 against the accumulated result), forcing an inline fallback
		// at each step - matching spruce exactly.
		first, err := engine.ParseYAML([]byte(`
list:
- name: foo
  org: org1
- name:
    beep: boop
  org: org2
`))
		So(err, ShouldBeNil)
		second, err := engine.ParseYAML([]byte(`
list:
- name: foo
  org: org3
- name: bar
  org: org4
`))
		So(err, ShouldBeNil)

		result, err := engine.Merge(context.Background(), first, second).Execute()
		So(err, ShouldBeNil)

		list, err := result.GetSlice("list")
		So(err, ShouldBeNil)
		So(list, ShouldHaveLength, 2)

		joined := strings.Join(captured, "\n")
		So(joined, ShouldContainSubstring, "warning: $.list.1: new object's key 'name' cannot have a value which is a hash or sequence - cannot merge by key")
		So(joined, ShouldContainSubstring, "warning: $.list.1: original object's key 'name' cannot have a value which is a hash or sequence - cannot merge by key")
		So(strings.Count(joined, "warning: Falling back to inline merge strategy"), ShouldEqual, 2)

		// None of the captured warning lines may match the CLI's error-line
		// scraping regex (^ - \$\.) - warnings must stay distinguishable from
		// hard errors on stderr.
		for _, line := range captured {
			So(strings.HasPrefix(strings.TrimSpace(line), "- $."), ShouldBeFalse)
		}
	})
}

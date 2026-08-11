package operators

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
	"github.com/fivetwenty-io/graft/pkg/graft"
)

// TestSortMarkerParity locks the sort operator's marker-handling behavior
// against spruce's op_sort.go and merge.go (mergeObjSortRx / addToSortListIfNecessary):
// an orphaned (( sort by X )) marker (no prior list at that path) must fail
// evaluation, and a quoted (( sort by "key" )) marker must be taken
// literally (matching only a map key spelled with the quote characters
// included), erroring when no map entry has that literal key. Uses the
// same Merge().Execute() pipeline the CLI drives, since that's where both
// divergences lived (marker registration happens at merge time, keyed off
// whether a prior document already had a list at that path).
func TestSortMarkerParity(t *testing.T) {
	// Disable ANSI color so the scraped message portion is plain text,
	// matching how genesis reads non-tty stderr output.
	ansi.Color(false)
	defer ansi.Color(true)

	Convey("sort: unquoted `by <key>` against a homogeneous prior list sorts and stays green", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		base, err := engine.ParseYAML([]byte(`
list:
- name: charlie
- name: alpha
- name: bravo
`))
		So(err, ShouldBeNil)

		override, err := engine.ParseYAML([]byte(`
list: (( sort by name ))
`))
		So(err, ShouldBeNil)

		out, err := engine.Merge(context.TODO(), base, override).Execute()
		So(err, ShouldBeNil)

		name, err := out.GetString("list.0.name")
		So(err, ShouldBeNil)
		So(name, ShouldEqual, "alpha")
	})

	Convey("sort: a marker with no prior list at that path is orphaned and errors with spruce's exact wording", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
result: (( sort by "name" ))
`))
		So(err, ShouldBeNil)

		_, err = engine.Merge(context.TODO(), doc).Execute()
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "orphaned (( sort )) operator at $.result, no list exists at that path")
	})

	Convey("sort: a quoted `by \"key\"` is taken literally and errors when no map entry has that literal key", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		base, err := engine.ParseYAML([]byte(`
list:
- name: charlie
- name: alpha
- name: bravo
`))
		So(err, ShouldBeNil)

		override, err := engine.ParseYAML([]byte(`
list: (( sort by "name" ))
`))
		So(err, ShouldBeNil)

		_, err = engine.Merge(context.TODO(), base, override).Execute()
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, `is a list with map entries, where some do not contain "name" (not a list with map entries each containing "name")`)
	})
}

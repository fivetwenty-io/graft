package merger

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
)

func TestGetArrayModificationsNamedInsertOnSimpleList(t *testing.T) {
	Convey("(( insert ... \"<name>\" )) against a simple list", t, func() {
		Convey("leaves the key empty so the anchor matches the entry value", func() {
			results := getArrayModifications([]interface{}{`(( insert after "bravo" ))`, "delta"}, true)
			So(results, ShouldHaveLength, 2)
			So(results[1].listOp, ShouldEqual, listOpInsert)
			So(results[1].relative, ShouldEqual, "after")
			So(results[1].key, ShouldBeEmpty)
			So(results[1].name, ShouldEqual, "bravo")
			So(results[1].list, ShouldResemble, []interface{}{"delta"})
		})

		Convey("keeps an explicit key so the merge can reject it", func() {
			results := getArrayModifications([]interface{}{`(( insert after name "bravo" ))`, "delta"}, true)
			So(results, ShouldHaveLength, 2)
			So(results[1].listOp, ShouldEqual, listOpInsert)
			So(results[1].key, ShouldEqual, "name")
			So(results[1].name, ShouldEqual, "bravo")
		})

		Convey("still defaults the key against a list of maps", func() {
			results := getArrayModifications([]interface{}{`(( insert after "bravo" ))`}, false)
			So(results, ShouldHaveLength, 2)
			So(results[1].key, ShouldEqual, "name")
			So(results[1].name, ShouldEqual, "bravo")
		})
	})
}

func TestMergeArrayNamedInsertOnSimpleList(t *testing.T) {
	// Disable ANSI colors for testing
	ansi.Color(false)

	Convey("named (( insert )) on a list of scalars", t, func() {
		orig := []interface{}{"alpha", "bravo", "charlie"}

		Convey("after the matching value puts the new entry behind it", func() {
			array := []interface{}{`(( insert after "bravo" ))`, "delta"}
			expect := []interface{}{"alpha", "bravo", "delta", "charlie"}

			m := &Merger{}
			a := m.mergeArray(orig, array, "node-path")
			err := m.Error()
			So(a, ShouldResemble, expect)
			So(err, ShouldBeNil)
		})

		Convey("before the matching value puts the new entry in front of it", func() {
			array := []interface{}{`(( insert before "bravo" ))`, "delta"}
			expect := []interface{}{"alpha", "delta", "bravo", "charlie"}

			m := &Merger{}
			a := m.mergeArray(orig, array, "node-path")
			err := m.Error()
			So(a, ShouldResemble, expect)
			So(err, ShouldBeNil)
		})

		Convey("after the last value appends", func() {
			array := []interface{}{`(( insert after "charlie" ))`, "delta"}
			expect := []interface{}{"alpha", "bravo", "charlie", "delta"}

			m := &Merger{}
			a := m.mergeArray(orig, array, "node-path")
			err := m.Error()
			So(a, ShouldResemble, expect)
			So(err, ShouldBeNil)
		})

		Convey("before the first value prepends", func() {
			array := []interface{}{`(( insert before "alpha" ))`, "delta"}
			expect := []interface{}{"delta", "alpha", "bravo", "charlie"}

			m := &Merger{}
			a := m.mergeArray(orig, array, "node-path")
			err := m.Error()
			So(a, ShouldResemble, expect)
			So(err, ShouldBeNil)
		})

		Convey("first match wins when the anchor value appears multiple times", func() {
			orig := []interface{}{"alpha", "bravo", "alpha"}
			array := []interface{}{`(( insert after "alpha" ))`, "delta"}
			expect := []interface{}{"alpha", "delta", "bravo", "alpha"}

			m := &Merger{}
			a := m.mergeArray(orig, array, "node-path")
			err := m.Error()
			So(a, ShouldResemble, expect)
			So(err, ShouldBeNil)
		})

		Convey("throw an error when the anchor value cannot be found", func() {
			array := []interface{}{`(( insert after "missing" ))`, "delta"}

			m := &Merger{}
			a := m.mergeArray(orig, array, "node-path")
			err := m.Error()
			So(a, ShouldBeNil)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "unable to find specified modification point with 'missing'")
		})

		Convey("throw the same targeted error on an empty original list", func() {
			array := []interface{}{`(( insert after "missing" ))`, "delta"}

			m := &Merger{}
			a := m.mergeArray([]interface{}{}, array, "node-path")
			err := m.Error()
			So(a, ShouldBeNil)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "unable to find specified modification point with 'missing'")
		})

		Convey("throw an error when the insertion point is keyed", func() {
			array := []interface{}{`(( insert after name "bravo" ))`, "delta"}

			m := &Merger{}
			a := m.mergeArray(orig, array, "node-path")
			err := m.Error()
			So(a, ShouldBeNil)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "cannot target entries in a list of scalars")
		})

		Convey("a non-string scalar anchor is reported as not found", func() {
			// getIndexOfSimpleEntry compares strings only, mirroring the
			// named delete on simple lists
			orig := []interface{}{1, 2, 3}
			array := []interface{}{`(( insert after "2" ))`, "delta"}

			m := &Merger{}
			a := m.mergeArray(orig, array, "node-path")
			err := m.Error()
			So(a, ShouldBeNil)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "unable to find specified modification point with '2'")
		})

		Convey("index-form insert on a simple list is unchanged", func() {
			array := []interface{}{"(( insert after 0 ))", "delta"}
			expect := []interface{}{"alpha", "delta", "bravo", "charlie"}

			m := &Merger{}
			a := m.mergeArray(orig, array, "node-path")
			err := m.Error()
			So(a, ShouldResemble, expect)
			So(err, ShouldBeNil)
		})
	})
}

func TestMergeArrayNamedInsertOnMapListRegressions(t *testing.T) {
	// Disable ANSI colors for testing
	ansi.Color(false)

	Convey("named (( insert )) on a list of maps", t, func() {
		orig := []interface{}{
			map[string]interface{}{"name": "alpha"},
			map[string]interface{}{"name": "bravo"},
			map[string]interface{}{"name": "charlie"},
		}

		Convey("still anchors on the default identifier key", func() {
			array := []interface{}{
				`(( insert after "bravo" ))`,
				map[string]interface{}{"name": "delta"},
			}

			expect := []interface{}{
				map[string]interface{}{"name": "alpha"},
				map[string]interface{}{"name": "bravo"},
				map[string]interface{}{"name": "delta"},
				map[string]interface{}{"name": "charlie"},
			}

			m := &Merger{}
			a := m.mergeArray(orig, array, "node-path")
			err := m.Error()
			So(a, ShouldResemble, expect)
			So(err, ShouldBeNil)
		})

		Convey("throw an error when the new entry duplicates the first list entry", func() {
			array := []interface{}{
				`(( insert after name "bravo" ))`,
				map[string]interface{}{"name": "alpha"},
			}

			m := &Merger{}
			a := m.mergeArray(orig, array, "node-path")
			err := m.Error()
			So(a, ShouldBeNil)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "unable to insert, because new list entry 'name: alpha' is detected multiple times")
		})
	})
}

func TestMergeObjQuickReferenceInsertExample(t *testing.T) {
	// Disable ANSI colors for testing
	ansi.Color(false)

	Convey("the operator quick-reference named-insert example produces its documented output", t, func() {
		orig := map[string]interface{}{
			"items": []interface{}{"first", "target", "last"},
		}
		n := map[string]interface{}{
			"items": []interface{}{`(( insert after "target" ))`, "inserted_after"},
		}
		expect := map[string]interface{}{
			"items": []interface{}{"first", "target", "inserted_after", "last"},
		}

		m := &Merger{}
		o := m.MergeObj(orig, n, "$")
		err := m.Error()
		So(o, ShouldResemble, expect)
		So(err, ShouldBeNil)
	})
}

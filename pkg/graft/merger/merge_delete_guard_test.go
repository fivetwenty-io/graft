package merger

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	yamlv3 "github.com/goccy/go-yaml"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
)

// TestDeleteOutsideListGuard covers mergeMap's guard against an
// argument-bearing (( delete ... )) in map-value position: the marker only
// has meaning as a list entry, and anywhere else it would otherwise survive
// the merge as literal data (spruce errors on the same input, from its eval
// phase). The bare, argument-less (( delete )) form is deliberately NOT
// guarded — spruce passes it through as literal text, and graft pins that
// parity below.
func TestDeleteOutsideListGuard(t *testing.T) {
	ansi.Color(false)

	YAML := func(s string) map[string]interface{} {
		data := make(map[string]interface{})
		err := yamlv3.Unmarshal(quoteInjectKeys([]byte(s)), &data)
		So(err, ShouldBeNil)
		return normalizeYAMLMap(data)
	}

	const guardMsg = `$.scalarkey: inappropriate use of (( delete )) operator outside of a list`

	forms := []struct {
		name    string
		overlay string
	}{
		{"string argument", "scalarkey: (( delete \"hello\" ))\n"},
		{"keyed argument", "scalarkey: (( delete name \"hello\" ))\n"},
		{"integer argument", "scalarkey: (( delete 0 ))\n"},
		{"reference argument", "scalarkey: (( delete meta.key ))\n"},
	}

	bases := []struct {
		name string
		base string
	}{
		{"scalar base", "scalarkey: hello\n"},
		{"map base", "scalarkey:\n  nested: 1\n"},
		{"list base", "scalarkey:\n- hello\n"},
		{"absent base", "other: 1\n"},
	}

	Convey("Merge() rejects an argument-bearing (( delete ... )) outside a list", t, func() {
		for _, form := range forms {
			for _, base := range bases {
				Convey(form.name+" onto "+base.name, func() {
					_, err := Merge(YAML(base.base), YAML(form.overlay))
					So(err, ShouldNotBeNil)
					So(err.Error(), ShouldContainSubstring, guardMsg)
				})
			}
		}

		Convey("a nested map key with the marker reports its full path", func() {
			_, err := Merge(YAML("outer:\n  inner: 1\n"), YAML("outer:\n  inner: (( delete \"x\" ))\n"))
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, `$.outer.inner: inappropriate use of (( delete )) operator outside of a list`)
		})
	})

	Convey("bare (( delete )) in map-value position passes through untouched (spruce parity)", t, func() {
		merged, err := Merge(YAML("scalarkey: hello\n"), YAML("scalarkey: (( delete ))\n"))
		So(err, ShouldBeNil)
		So(merged["scalarkey"], ShouldEqual, "(( delete ))")
	})

	Convey("argument-bearing delete markers in list position still delete", t, func() {
		Convey("delete by value", func() {
			merged, err := Merge(YAML("list:\n- a\n- b\n"), YAML("list:\n- (( delete \"a\" ))\n"))
			So(err, ShouldBeNil)
			So(merged["list"], ShouldResemble, []interface{}{"b"})
		})

		Convey("delete by index", func() {
			merged, err := Merge(YAML("list:\n- a\n- b\n"), YAML("list:\n- (( delete 0 ))\n"))
			So(err, ShouldBeNil)
			So(merged["list"], ShouldResemble, []interface{}{"b"})
		})

		Convey("delete-if-present of a missing entry is a no-op", func() {
			merged, err := Merge(YAML("list:\n- a\n- b\n"), YAML("list:\n- (( delete \"not-there\" ))\n"))
			So(err, ShouldBeNil)
			So(merged["list"], ShouldResemble, []interface{}{"a", "b"})
		})
	})
}

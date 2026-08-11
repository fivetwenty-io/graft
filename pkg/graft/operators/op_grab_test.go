package operators

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// TestGrabMultiArgFlattensLists locks grab's multi-arg behavior to
// spruce's: with more than one argument, list-valued results are
// flattened into the combined output list while scalar/map results are
// appended as single elements.
func TestGrabMultiArgFlattensLists(t *testing.T) {
	Convey("grab with multiple list-valued references flattens into one list", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
a: [1, 2]
b: [3, 4]
combined: (( grab a b ))
`))
		So(err, ShouldBeNil)

		result, err := engine.Evaluate(context.Background(), doc)
		So(err, ShouldBeNil)

		val, getErr := result.Get("combined")
		So(getErr, ShouldBeNil)
		So(val, ShouldResemble, []interface{}{1, 2, 3, 4})
	})

	Convey("grab with a mix of list and scalar references appends scalars as single elements", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
a: [1, 2]
b: 3
combined: (( grab a b ))
`))
		So(err, ShouldBeNil)

		result, err := engine.Evaluate(context.Background(), doc)
		So(err, ShouldBeNil)

		val, getErr := result.Get("combined")
		So(getErr, ShouldBeNil)
		So(val, ShouldResemble, []interface{}{1, 2, 3})
	})

	Convey("grab with a single argument returns the value as-is, without flattening", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
a: [1, 2]
combined: (( grab a ))
`))
		So(err, ShouldBeNil)

		result, err := engine.Evaluate(context.Background(), doc)
		So(err, ShouldBeNil)

		val, getErr := result.Get("combined")
		So(getErr, ShouldBeNil)
		So(val, ShouldResemble, []interface{}{1, 2})
	})
}

// TestGrabDynamicBracketKeyResolution locks bracket-notation dynamic key
// resolution parity with spruce's op_grab bracket support: "key[lookup]"
// resolves "lookup" as its own path against the document, and uses the
// resulting scalar as the actual key to grab, rather than treating
// "lookup" as a literal field name (which dot notation would).
func TestGrabDynamicBracketKeyResolution(t *testing.T) {
	Convey("grab resolves a value using a dynamic bracket reference", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
key:
  subkey: found it
  other: value 2
lookup: subkey
combined: (( grab key[lookup] ))
`))
		So(err, ShouldBeNil)

		result, err := engine.Evaluate(context.Background(), doc)
		So(err, ShouldBeNil)

		val, getErr := result.Get("combined")
		So(getErr, ShouldBeNil)
		So(val, ShouldEqual, "found it")
	})

	Convey("grab resolves a value using a nested path inside a bracket reference", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
key:
  subkey: found it
meta:
  which: subkey
combined: (( grab key[meta.which] ))
`))
		So(err, ShouldBeNil)

		result, err := engine.Evaluate(context.Background(), doc)
		So(err, ShouldBeNil)

		val, getErr := result.Get("combined")
		So(getErr, ShouldBeNil)
		So(val, ShouldEqual, "found it")
	})

	Convey("grab returns an error when the bracket key reference resolves to a non-scalar", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
key:
  subkey: found it
lookup:
  nested: subkey
combined: (( grab key[lookup] ))
`))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.Background(), doc)
		So(err, ShouldNotBeNil)
	})

	Convey("grab resolves a value using a numeric-valued bracket reference", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
versions:
  "1": first
  "2": second
pick: 2
combined: (( grab versions[pick] ))
`))
		So(err, ShouldBeNil)

		result, err := engine.Evaluate(context.Background(), doc)
		So(err, ShouldBeNil)

		val, getErr := result.Get("combined")
		So(getErr, ShouldBeNil)
		So(val, ShouldEqual, "second")
	})

	Convey("grab treats a numeric bracket literal as an array index, not a dynamic key", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
items: [first, second, third]
combined: (( grab items[1] ))
`))
		So(err, ShouldBeNil)

		result, err := engine.Evaluate(context.Background(), doc)
		So(err, ShouldBeNil)

		val, getErr := result.Get("combined")
		So(getErr, ShouldBeNil)
		So(val, ShouldEqual, "second")
	})

	Convey("grab still resolves an environment-variable bracket key by name", t, func() {
		t.Setenv("GRAFT_GRAB_BRACKET_KEY", "subkey")

		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
key:
  subkey: found it
  other: value 2
combined: (( grab key[$GRAFT_GRAB_BRACKET_KEY] ))
`))
		So(err, ShouldBeNil)

		result, err := engine.Evaluate(context.Background(), doc)
		So(err, ShouldBeNil)

		val, getErr := result.Get("combined")
		So(getErr, ShouldBeNil)
		So(val, ShouldEqual, "found it")
	})
}

// TestGrabUnresolvablePathErrorText locks grab's error wording for an
// unresolvable path to spruce's: "unable to resolve `path`: `$.path`
// could not be found in the datastructure", since genesis scrapes the
// message portion of `- $.path: msg` stderr lines.
func TestGrabUnresolvablePathErrorText(t *testing.T) {
	Convey("grab referencing a missing path", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
combined: (( grab meta.missing ))
`))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.Background(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "unable to resolve `meta.missing`:")
		So(err.Error(), ShouldContainSubstring, "could not be found in the datastructure")
	})
}

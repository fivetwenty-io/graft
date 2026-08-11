package operators

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// TestCalcRawSubstringMatchesQuotedForm pins the rule that the
// parser's raw-substring capture for "(( calc * 2 ))" must produce the same
// result as writing the equivalent "(( calc "* 2" ))" by hand. Since
// nothing overwrote the value at this path (single document, no merge), the
// documented default of 0 is prepended before the operator — "0 * 2" is 0,
// "0 + 5" is 5 — unaffected by A5's PriorValues gap 2, which is covered
// separately by op_calc_prior_test.go.
func TestCalcRawSubstringMatchesQuotedForm(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want int64
	}{
		{"raw *", "x: (( calc * 2 ))\n", 0},
		{"quoted *", `x: (( calc "* 2" ))` + "\n", 0},
		{"raw +", "x: (( calc + 5 ))\n", 5},
		{"quoted +", `x: (( calc "+ 5" ))` + "\n", 5},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			Convey(tc.name, t, func() {
				engine, err := graft.NewEngine()
				So(err, ShouldBeNil)

				doc, err := engine.ParseYAML([]byte(tc.yaml))
				So(err, ShouldBeNil)

				result, err := engine.Evaluate(context.Background(), doc)
				So(err, ShouldBeNil)

				val, getErr := result.Get("x")
				So(getErr, ShouldBeNil)
				So(val, ShouldEqual, tc.want)
			})
		})
	}
}

// TestCalcLiteralOnlyUnaffected pins B-8: a literal-only calc expression
// (no leading operator, no named variables) takes the exact same path as
// before A5.
func TestCalcLiteralOnlyUnaffected(t *testing.T) {
	Convey("calc \"2 * 3\" evaluates unchanged", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`x: (( calc "2 * 3" ))` + "\n"))
		So(err, ShouldBeNil)

		result, err := engine.Evaluate(context.Background(), doc)
		So(err, ShouldBeNil)

		val, getErr := result.Get("x")
		So(getErr, ShouldBeNil)
		So(val, ShouldEqual, int64(6))
	})
}

// TestCalcNamedVariables pins the rule that bare named variables
// resolve relative to the calc call's own parent first (siblings), then
// absolutely from the document root.
func TestCalcNamedVariables(t *testing.T) {
	Convey("sibling variables resolve, matching arithmetic.md's documented example", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte("a: 10\nb: 5\nx: (( calc \"a + b\" ))\n"))
		So(err, ShouldBeNil)

		result, err := engine.Evaluate(context.Background(), doc)
		So(err, ShouldBeNil)

		val, getErr := result.Get("x")
		So(getErr, ShouldBeNil)
		So(val, ShouldEqual, int64(15))
	})

	Convey("relative-before-absolute: a nested sibling wins over a root name of the same word", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
a: 1
nested:
  a: 100
  x: (( calc "a + 1" ))
`))
		So(err, ShouldBeNil)

		result, err := engine.Evaluate(context.Background(), doc)
		So(err, ShouldBeNil)

		val, getErr := result.Get("nested.x")
		So(getErr, ShouldBeNil)
		So(val, ShouldEqual, int64(101))
	})

	Convey("falls back to an absolute root reference when no sibling exists", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
root_value: 100
nested:
  x: (( calc "root_value + 1" ))
`))
		So(err, ShouldBeNil)

		result, err := engine.Evaluate(context.Background(), doc)
		So(err, ShouldBeNil)

		val, getErr := result.Get("nested.x")
		So(getErr, ShouldBeNil)
		So(val, ShouldEqual, int64(101))
	})

	Convey("an unresolvable name is reported by name", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`x: (( calc "q + 1" ))` + "\n"))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.Background(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "calc operator does not support named variables in expression: q")
	})

	Convey("only the still-unresolved names are listed, not the resolved ones", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte("a: 1\nx: (( calc \"a + q\" ))\n"))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.Background(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "calc operator does not support named variables in expression: q")
		So(err.Error(), ShouldNotContainSubstring, "expression: a")
	})

	Convey("a resolved but non-numeric name errors immediately with its type", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte("a: hello\nx: (( calc \"a + 1\" ))\n"))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.Background(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "path a is of type string, which cannot be used in calculations")
	})

	Convey("a name resolving to an explicit null errors rather than panicking", t, func() {
		// reflect.TypeOf(nil) is nil, so reporting the type of an explicitly
		// null value the way a non-numeric value is reported would panic.
		// replaceCalcReferences already has a dedicated nil message for the
		// dotted-path case; bare names use the same one.
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte("a: ~\nx: (( calc \"a + 1\" ))\n"))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.Background(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "path a references a nil value, which cannot be used in calculations")
	})

	Convey("named variables compose with the documented function table", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte("value: 150\nx: (( calc \"max(0, min(100, value))\" ))\n"))
		So(err, ShouldBeNil)

		result, err := engine.Evaluate(context.Background(), doc)
		So(err, ShouldBeNil)

		val, getErr := result.Get("x")
		So(getErr, ShouldBeNil)
		So(val, ShouldEqual, int64(100))
	})
}

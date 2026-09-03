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

// TestCalcExpressionSemantics is a characterization test pinning the
// observable behavior of the (( calc )) expression grammar: division,
// modulo, exponent operators, the ternary safe-division idiom documented in
// arithmetic.md, and the undefined-function error. These rows pass under
// the Knetic/govaluate dependency in place when this test was written, and
// must keep passing unchanged after any future swap of the underlying
// expression library, since nothing here depends on library-specific error
// wording.
func TestCalcExpressionSemantics(t *testing.T) {
	Convey("10 / 3 stays a float, not truncated", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`x: (( calc "10 / 3" ))` + "\n"))
		So(err, ShouldBeNil)

		result, err := engine.Evaluate(context.Background(), doc)
		So(err, ShouldBeNil)

		val, getErr := result.Get("x")
		So(getErr, ShouldBeNil)
		So(val, ShouldEqual, 3.3333333333333335)
	})

	Convey("100 / 4 coerces to an int when evenly divisible", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`x: (( calc "100 / 4" ))` + "\n"))
		So(err, ShouldBeNil)

		result, err := engine.Evaluate(context.Background(), doc)
		So(err, ShouldBeNil)

		val, getErr := result.Get("x")
		So(getErr, ShouldBeNil)
		So(val, ShouldEqual, int64(25))
	})

	Convey("10 % 3 is an int modulo", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`x: (( calc "10 % 3" ))` + "\n"))
		So(err, ShouldBeNil)

		result, err := engine.Evaluate(context.Background(), doc)
		So(err, ShouldBeNil)

		val, getErr := result.Get("x")
		So(getErr, ShouldBeNil)
		So(val, ShouldEqual, int64(1))
	})

	Convey("10.5 % 3 is a float modulo (math.Mod)", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`x: (( calc "10.5 % 3" ))` + "\n"))
		So(err, ShouldBeNil)

		result, err := engine.Evaluate(context.Background(), doc)
		So(err, ShouldBeNil)

		val, getErr := result.Get("x")
		So(getErr, ShouldBeNil)
		So(val, ShouldEqual, 1.5)
	})

	Convey("2 ** 3 is exponentiation", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`x: (( calc "2 ** 3" ))` + "\n"))
		So(err, ShouldBeNil)

		result, err := engine.Evaluate(context.Background(), doc)
		So(err, ShouldBeNil)

		val, getErr := result.Get("x")
		So(getErr, ShouldBeNil)
		So(val, ShouldEqual, int64(8))
	})

	Convey("2 ^ 3 is bitwise xor, not exponentiation", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`x: (( calc "2 ^ 3" ))` + "\n"))
		So(err, ShouldBeNil)

		result, err := engine.Evaluate(context.Background(), doc)
		So(err, ShouldBeNil)

		val, getErr := result.Get("x")
		So(getErr, ShouldBeNil)
		So(val, ShouldEqual, int64(1))
	})

	Convey("2 ** 0.5 is a fractional exponent", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`x: (( calc "2 ** 0.5" ))` + "\n"))
		So(err, ShouldBeNil)

		result, err := engine.Evaluate(context.Background(), doc)
		So(err, ShouldBeNil)

		val, getErr := result.Get("x")
		So(getErr, ShouldBeNil)
		So(val, ShouldEqual, 1.4142135623730951)
	})

	Convey("the ternary safe-division idiom returns 0 when the divisor is 0", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		yaml := "dividend: 100\ndivisor: 0\nx: '(( calc \"divisor != 0 ? dividend / divisor : 0\" ))'\n"
		doc, err := engine.ParseYAML([]byte(yaml))
		So(err, ShouldBeNil)

		result, err := engine.Evaluate(context.Background(), doc)
		So(err, ShouldBeNil)

		val, getErr := result.Get("x")
		So(getErr, ShouldBeNil)
		So(val, ShouldEqual, int64(0))
	})

	Convey("the ternary safe-division idiom divides when the divisor is nonzero", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		yaml := "dividend: 100\ndivisor: 5\nx: '(( calc \"divisor != 0 ? dividend / divisor : 0\" ))'\n"
		doc, err := engine.ParseYAML([]byte(yaml))
		So(err, ShouldBeNil)

		result, err := engine.Evaluate(context.Background(), doc)
		So(err, ShouldBeNil)

		val, getErr := result.Get("x")
		So(getErr, ShouldBeNil)
		So(val, ShouldEqual, int64(20))
	})

	Convey("calling an undefined function reports its name", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`x: (( calc "abs(1)" ))` + "\n"))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.Background(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "Undefined function abs")
	})
}

// TestCalcParseErrorsUseForkWording pins the parse-error wording and hex
// literal support of the casbin/govaluate fork (github.com/casbin/govaluate
// v1.10.0). The upstream Knetic/govaluate library capitalizes these
// messages and rejects hex literals; the fork lowercases the messages and
// accepts hex. Both libraries share the same grammar otherwise.
func TestCalcParseErrorsUseForkWording(t *testing.T) {
	Convey("an unbalanced parenthesis reports the fork's lowercase wording", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`x: (( calc "(1 +" ))` + "\n"))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.Background(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "unbalanced parenthesis")
	})

	Convey("an unexpected end of expression reports the fork's lowercase wording", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`x: (( calc "1 +" ))` + "\n"))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.Background(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "unexpected end of expression")
	})

	Convey("an unclosed string literal reports the fork's lowercase wording", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`x: (( calc "\"a" ))` + "\n"))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.Background(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "unclosed string literal")
	})

	Convey("an unclosed parameter bracket reports the fork's lowercase wording", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`x: (( calc "1 + [foo" ))` + "\n"))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.Background(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "unclosed parameter bracket")
	})

	Convey("hex literals parse under the fork", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`x: (( calc "0x10 + 1" ))` + "\n"))
		So(err, ShouldBeNil)

		result, err := engine.Evaluate(context.Background(), doc)
		So(err, ShouldBeNil)

		val, getErr := result.Get("x")
		So(getErr, ShouldBeNil)
		So(val, ShouldEqual, int64(17))
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

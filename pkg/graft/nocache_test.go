package graft

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

// These tests pin the expression-modifier surface: the Expr modifier
// methods, the parser's `name[:modifier][@target]` grammar, and the
// resulting Opcall flag. Only `nocache` is a valid modifier; an unknown
// modifier is a loud parse error rather than a silent pass-through. The
// modifier must be written without whitespace (grab:nocache) - a spaced
// colon is not the modifier form and keeps its old parse-error behavior.
//
// spruce has no modifier syntax at all: it leaves any (( op:anything ... ))
// string as unresolved literal text. graft has always hard-errored on those
// inputs instead (a pre-existing, documented divergence), so giving
// grab:nocache a meaning changes only inputs that previously failed to
// parse - no working spruce or graft document changes behavior.
func TestExprModifiers(t *testing.T) {
	Convey("Expr modifier methods", t, func() {
		Convey("zero value has no modifiers", func() {
			e := &Expr{Type: OperatorCall, Operator: "vault"}
			So(e.IsNoCache(), ShouldBeFalse)
			So(e.HasModifier("nocache"), ShouldBeFalse)
			So(e.GetModifiers(), ShouldBeEmpty)
		})

		Convey("SetModifier marks the expression", func() {
			e := &Expr{Type: OperatorCall, Operator: "vault"}
			e.SetModifier("nocache")
			So(e.IsNoCache(), ShouldBeTrue)
			So(e.HasModifier("nocache"), ShouldBeTrue)
			So(e.GetModifiers(), ShouldResemble, []string{"nocache"})
		})

		Convey("SetModifier is idempotent", func() {
			e := &Expr{}
			e.SetModifier("nocache")
			e.SetModifier("nocache")
			So(e.GetModifiers(), ShouldResemble, []string{"nocache"})
		})

		Convey("GetModifiers returns a sorted list", func() {
			e := &Expr{}
			e.SetModifier("zeta")
			e.SetModifier("alpha")
			So(e.GetModifiers(), ShouldResemble, []string{"alpha", "zeta"})
		})
	})
}

func TestNoCacheModifierParsing(t *testing.T) {
	parse := func(input string) (*Opcall, error) {
		return NewParser(input, EvalPhase).ParseOpcall()
	}

	Convey("Parsing the :nocache modifier", t, func() {
		Convey("op:nocache parses and sets the flag", func() {
			opcall, err := parse(`(( vault:nocache "secret/path" ))`)
			So(err, ShouldBeNil)
			So(opcall, ShouldNotBeNil)
			So(opcall.NoCache(), ShouldBeTrue)
			So(opcall.Args(), ShouldHaveLength, 1)
		})

		Convey("without the modifier the flag stays false", func() {
			opcall, err := parse(`(( vault "secret/path" ))`)
			So(err, ShouldBeNil)
			So(opcall.NoCache(), ShouldBeFalse)
		})

		Convey("an unknown modifier is a parse error", func() {
			_, err := parse(`(( grab:bogus meta.data ))`)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "unknown operator modifier")
			So(err.Error(), ShouldContainSubstring, "bogus")
		})

		Convey("modifier composes with @target, in that order", func() {
			opcall, err := parse(`(( vault:nocache@prod "secret/path" ))`)
			So(err, ShouldBeNil)
			So(opcall.NoCache(), ShouldBeTrue)
			So(opcall.Target(), ShouldEqual, "prod")
		})

		Convey("target-then-modifier is not accepted", func() {
			_, err := parse(`(( vault@prod:nocache "secret/path" ))`)
			So(err, ShouldNotBeNil)
		})

		Convey("a spaced colon is not the modifier form", func() {
			_, err := parse(`(( grab : nocache meta.data ))`)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldNotContainSubstring, "unknown operator modifier")
		})

		Convey("modifier works on the right side of ||", func() {
			opcall, err := parse(`(( grab meta.a || vault:nocache "secret/path" ))`)
			So(err, ShouldBeNil)
			So(opcall, ShouldNotBeNil)
		})

		Convey("ternary ? : is unaffected", func() {
			opcall, err := parse(`(( true ? 1 : 2 ))`)
			So(err, ShouldBeNil)
			So(opcall, ShouldNotBeNil)
		})
	})
}

// TestNoCacheModifierEndToEnd proves the modifier is accepted through a
// real merge and is semantically inert for a non-backend operator (the
// backend cache bypass is wired separately).
func TestNoCacheModifierEndToEnd(t *testing.T) {
	Convey("A grab:nocache expression evaluates like plain grab", t, func() {
		engine, err := NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte("meta:\n  data: hello\nv: (( grab:nocache meta.data ))\n"))
		So(err, ShouldBeNil)

		result, err := engine.Merge(context.Background(), doc).Execute()
		So(err, ShouldBeNil)

		v, err := result.Get("v")
		So(err, ShouldBeNil)
		So(v, ShouldEqual, "hello")
	})
}

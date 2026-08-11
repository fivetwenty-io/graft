package operators

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// TestFlattenOperator_DeepNesting pins §4.2: flatten is fully recursive, not
// one level, using the doc's own worked example.
func TestFlattenOperator_DeepNesting(t *testing.T) {
	Convey("flatten recursively flattens arbitrarily nested arrays", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
v:
- [1, 2]
- [3, 4]
- - [5, 6]
  - 7
t: (( flatten v ))
`))
		So(err, ShouldBeNil)

		result, err := engine.Evaluate(context.Background(), doc)
		So(err, ShouldBeNil)

		val, getErr := result.Get("t")
		So(getErr, ShouldBeNil)
		So(val, ShouldResemble, []interface{}{1, 2, 3, 4, 5, 6, 7})
	})

	Convey("flatten of an empty list is an empty list", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte("v: []\nt: (( flatten v ))\n"))
		So(err, ShouldBeNil)

		result, err := engine.Evaluate(context.Background(), doc)
		So(err, ShouldBeNil)

		val, getErr := result.Get("t")
		So(getErr, ShouldBeNil)
		So(val, ShouldResemble, []interface{}{})
	})

	Convey("flatten preserves nested nil elements rather than dropping them", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte("v:\n- 1\n- ~\n- [2, ~]\nt: (( flatten v ))\n"))
		So(err, ShouldBeNil)

		result, err := engine.Evaluate(context.Background(), doc)
		So(err, ShouldBeNil)

		val, getErr := result.Get("t")
		So(getErr, ShouldBeNil)
		So(val, ShouldResemble, []interface{}{1, nil, 2, nil})
	})
}

// TestFlattenOperator_Arity pins exactly-one-argument arity.
func TestFlattenOperator_Arity(t *testing.T) {
	Convey("flatten with zero arguments errors", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte("t: (( flatten ))\n"))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.Background(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "flatten operator requires exactly one argument, got 0")
	})

	Convey("flatten with two arguments errors", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte("a: [1]\nb: [2]\nt: (( flatten a b ))\n"))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.Background(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "flatten operator requires exactly one argument, got 2")
	})
}

// TestFlattenOperator_NonListArgument pins the §4.2 type-mismatch message.
func TestFlattenOperator_NonListArgument(t *testing.T) {
	Convey("flatten of a non-list argument errors with its type", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte("v: hello\nt: (( flatten v ))\n"))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.Background(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "flatten operator requires a list argument, got string")
	})
}

package operators

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// TestUniqOperator_StableFirstOccurrence pins §4.3: uniq dedupes while
// preserving input order, keeping the first occurrence — it never sorts.
func TestUniqOperator_StableFirstOccurrence(t *testing.T) {
	Convey("uniq preserves input order, dropping later duplicates", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte("v: [3, 1, 2, 1, 3, 2]\nt: (( uniq v ))\n"))
		So(err, ShouldBeNil)

		result, err := engine.Evaluate(context.Background(), doc)
		So(err, ShouldBeNil)

		val, getErr := result.Get("t")
		So(getErr, ShouldBeNil)
		So(val, ShouldResemble, []interface{}{3, 1, 2})
	})

	Convey("uniq of an already-unique list is unchanged", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte("v: [a, b, c]\nt: (( uniq v ))\n"))
		So(err, ShouldBeNil)

		result, err := engine.Evaluate(context.Background(), doc)
		So(err, ShouldBeNil)

		val, getErr := result.Get("t")
		So(getErr, ShouldBeNil)
		So(val, ShouldResemble, []interface{}{"a", "b", "c"})
	})

	Convey("uniq dedupes structurally equal maps", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
v:
- {a: 1}
- {a: 2}
- {a: 1}
t: (( uniq v ))
`))
		So(err, ShouldBeNil)

		result, err := engine.Evaluate(context.Background(), doc)
		So(err, ShouldBeNil)

		val, getErr := result.Get("t")
		So(getErr, ShouldBeNil)
		So(val, ShouldResemble, []interface{}{
			map[string]interface{}{"a": 1},
			map[string]interface{}{"a": 2},
		})
	})
}

// TestUniqOperator_Arity pins exactly-one-argument arity.
func TestUniqOperator_Arity(t *testing.T) {
	Convey("uniq with zero arguments errors", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte("t: (( uniq ))\n"))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.Background(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "uniq operator requires exactly one argument, got 0")
	})

	Convey("uniq with two arguments errors", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte("a: [1]\nb: [2]\nt: (( uniq a b ))\n"))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.Background(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "uniq operator requires exactly one argument, got 2")
	})
}

// TestUniqOperator_NonListArgument pins the §4.3 type-mismatch message.
func TestUniqOperator_NonListArgument(t *testing.T) {
	Convey("uniq of a non-list argument errors with its type", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte("v: 5\nt: (( uniq v ))\n"))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.Background(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "uniq operator requires a list argument, got int")
	})
}

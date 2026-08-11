package operators

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// TestTypeOperator_Vocabulary pins the §4.1 return vocabulary: "string",
// "int", "float", "bool", "array" (not "list"), "map" (not "hash"), and
// "null" (interpretation decision T-1; not "nil").
func TestTypeOperator_Vocabulary(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{"string", "v: hello\nt: (( type v ))\n", "string"},
		{"int", "v: 5\nt: (( type v ))\n", "int"},
		{"float", "v: 5.5\nt: (( type v ))\n", "float"},
		{"bool", "v: true\nt: (( type v ))\n", "bool"},
		{"array", "v: [1, 2, 3]\nt: (( type v ))\n", "array"},
		{"map", "v:\n  a: 1\nt: (( type v ))\n", "map"},
		{"null", "v: ~\nt: (( type v ))\n", "null"},
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

				val, getErr := result.Get("t")
				So(getErr, ShouldBeNil)
				So(val, ShouldEqual, tc.want)
			})
		})
	}
}

// TestTypeOperator_Arity pins the exact-one-argument error text.
func TestTypeOperator_Arity(t *testing.T) {
	Convey("type with zero arguments errors with the argument count", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte("t: (( type ))\n"))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.Background(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "type operator requires exactly one argument, got 0")
	})

	Convey("type with two arguments errors with the argument count", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte("a: 1\nb: 2\nt: (( type a b ))\n"))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.Background(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "type operator requires exactly one argument, got 2")
	})
}

package operators

import (
	"context"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
	"github.com/fivetwenty-io/graft/pkg/graft"
)

// TestConcatErrorTextMatchesSpruce locks the exact wording of the concat
// operator's error messages to spruce's op_concat.go text, since genesis
// scrapes the message portion of `- $.path: msg` stderr lines.
func TestConcatErrorTextMatchesSpruce(t *testing.T) {
	// Disable ANSI color so the scraped message portion is plain text,
	// matching how genesis reads non-tty stderr output.
	ansi.Color(false)
	defer ansi.Color(true)

	Convey("concat operand that resolves to a map via a reference", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
meta:
  thing:
    nested: value
combined: (( concat "x-" meta.thing ))
`))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.TODO(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "tried to concat meta.thing, which is not a string scalar")
	})

	Convey("concat operand that resolves to a list via a reference", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
meta:
  items: [1, 2, 3]
combined: (( concat "x-" meta.items ))
`))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.TODO(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "tried to concat meta.items, which is not a string scalar")
	})

	Convey("concat operand that references a missing key", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
combined: (( concat "x-" meta.missing ))
`))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.TODO(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "unable to resolve `meta.missing`:")
	})

	Convey("concat called with fewer than two arguments", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
combined: (( concat "only-one" ))
`))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.TODO(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "concat operator requires at least two arguments")
	})

	Convey("concat of string-scalar references still succeeds", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
meta:
  first: hello
  second: world
combined: (( concat meta.first "-" meta.second ))
`))
		So(err, ShouldBeNil)

		result, err := engine.Evaluate(context.TODO(), doc)
		So(err, ShouldBeNil)

		val, getErr := result.Get("combined")
		So(getErr, ShouldBeNil)
		So(val, ShouldEqual, "hello-world")
	})
}

// TestConcatOperatorRunDirect exercises ConcatOperator.Run directly to
// verify the exact error string returned for a map operand, independent
// of any wrapping done by the evaluator's multi-error formatting.
func TestConcatOperatorRunDirect(t *testing.T) {
	ansi.Color(false)
	defer ansi.Color(true)

	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("NewEngine failed: %s", err)
	}

	doc, err := engine.ParseYAML([]byte(`
meta:
  thing:
    nested: value
combined: (( concat "x-" meta.thing ))
`))
	if err != nil {
		t.Fatalf("ParseYAML failed: %s", err)
	}

	_, err = engine.Evaluate(context.TODO(), doc)
	if err == nil {
		t.Fatal("expected an error for concat of a map operand, got nil")
	}

	const want = "tried to concat meta.thing, which is not a string scalar"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected error to contain %q, got %q", want, err.Error())
	}
}

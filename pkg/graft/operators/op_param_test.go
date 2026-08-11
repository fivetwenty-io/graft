package operators

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
	"github.com/fivetwenty-io/graft/pkg/graft"
)

// TestParamErrorMessageIsExactUserText locks the param operator's error
// message to spruce's behavior: the message is exactly the literal
// user-supplied argument text, with no prefix or suffix added by the
// operator (spruce: `(( param "why u no set this?" ))` errors with the
// literal message "why u no set this?"). Genesis scrapes the message
// portion of `- $.path: msg` stderr lines, so the exact text matters.
func TestParamErrorMessageIsExactUserText(t *testing.T) {
	ansi.Color(false)
	defer ansi.Color(true)

	Convey("param with a literal string argument", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
foo: (( param "why u no set this?" ))
`))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.TODO(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, " - $.foo: why u no set this?\n")
	})

	Convey("param message text is not wrapped in quotes or other decoration", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
bar: (( param "set bar explicitly" ))
`))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.TODO(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldNotContainSubstring, `"set bar explicitly"`)
		So(err.Error(), ShouldContainSubstring, "set bar explicitly")
	})

	Convey("param overridden by a later document produces no error", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		base, err := engine.ParseYAML([]byte(`
foo: (( param "why u no set this?" ))
`))
		So(err, ShouldBeNil)

		override, err := engine.ParseYAML([]byte("foo: set\n"))
		So(err, ShouldBeNil)

		result, err := engine.Merge(context.TODO(), base, override).Execute()
		So(err, ShouldBeNil)

		val, getErr := result.Get("foo")
		So(getErr, ShouldBeNil)
		So(val, ShouldEqual, "set")
	})
}

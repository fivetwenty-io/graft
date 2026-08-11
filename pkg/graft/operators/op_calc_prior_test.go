package operators

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// TestCalcPriorValue_GenuineTwoFileMerge pins spec cluster A5 §5.3 gap 2:
// a "(( calc <leading-op> ... ))" value-modification expression in an
// overlay document must multiply the *base* document's value at that path,
// not the unevaluated operator node itself. The single-file "base:"/
// "overlay:" doc example the docs use does not exercise this at all —
// nothing is actually overwritten in that shape — so this test uses a
// genuine two-document merge, matching the spec's own diagnosis.
func TestCalcPriorValue_GenuineTwoFileMerge(t *testing.T) {
	Convey("calc \"* 2\" doubles the base document's value", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		base, err := engine.ParseYAML([]byte("timeout: 30\n"))
		So(err, ShouldBeNil)

		overlay, err := engine.ParseYAML([]byte(`timeout: (( calc "* 2" ))` + "\n"))
		So(err, ShouldBeNil)

		result, err := engine.Merge(context.Background(), base, overlay).Execute()
		So(err, ShouldBeNil)

		val, getErr := result.Get("timeout")
		So(getErr, ShouldBeNil)
		So(val, ShouldEqual, int64(60))
	})

	Convey("calc * 2 (raw leading-operator form) doubles the base document's value", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		base, err := engine.ParseYAML([]byte("timeout: 30\n"))
		So(err, ShouldBeNil)

		overlay, err := engine.ParseYAML([]byte("timeout: (( calc * 2 ))\n"))
		So(err, ShouldBeNil)

		result, err := engine.Merge(context.Background(), base, overlay).Execute()
		So(err, ShouldBeNil)

		val, getErr := result.Get("timeout")
		So(getErr, ShouldBeNil)
		So(val, ShouldEqual, int64(60))
	})

	Convey("calc \"+ 5\" adds to the base document's value", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		base, err := engine.ParseYAML([]byte("count: 10\n"))
		So(err, ShouldBeNil)

		overlay, err := engine.ParseYAML([]byte(`count: (( calc "+ 5" ))` + "\n"))
		So(err, ShouldBeNil)

		result, err := engine.Merge(context.Background(), base, overlay).Execute()
		So(err, ShouldBeNil)

		val, getErr := result.Get("count")
		So(getErr, ShouldBeNil)
		So(val, ShouldEqual, int64(15))
	})

	Convey("absent path still defaults to 0, per the documented default", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		base, err := engine.ParseYAML([]byte("other: 1\n"))
		So(err, ShouldBeNil)

		overlay, err := engine.ParseYAML([]byte(`timeout: (( calc "* 2" ))` + "\n"))
		So(err, ShouldBeNil)

		result, err := engine.Merge(context.Background(), base, overlay).Execute()
		So(err, ShouldBeNil)

		val, getErr := result.Get("timeout")
		So(getErr, ShouldBeNil)
		So(val, ShouldEqual, int64(0))
	})

	Convey("nested path resolves its prior value correctly", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		base, err := engine.ParseYAML([]byte("service:\n  timeout: 30\n"))
		So(err, ShouldBeNil)

		overlay, err := engine.ParseYAML([]byte("service:\n  timeout: (( calc \"* 2\" ))\n"))
		So(err, ShouldBeNil)

		result, err := engine.Merge(context.Background(), base, overlay).Execute()
		So(err, ShouldBeNil)

		val, getErr := result.Get("service.timeout")
		So(getErr, ShouldBeNil)
		So(val, ShouldEqual, int64(60))
	})

	Convey("B-8: an ordinary calc expression is unaffected by PriorValues machinery", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		base, err := engine.ParseYAML([]byte("x: 30\n"))
		So(err, ShouldBeNil)

		overlay, err := engine.ParseYAML([]byte(`x: (( calc "2 * 3" ))` + "\n"))
		So(err, ShouldBeNil)

		result, err := engine.Merge(context.Background(), base, overlay).Execute()
		So(err, ShouldBeNil)

		val, getErr := result.Get("x")
		So(getErr, ShouldBeNil)
		So(val, ShouldEqual, int64(6))
	})

	Convey("a document routed through the legacy merger still records its prior value", t, func() {
		// Any array-merge marker, array-of-maps, prune, or sort marker in the
		// overlay routes the whole merge through pkg/graft/merger instead of
		// the builder's own simple-merge walk. That is the common shape for
		// real manifests, so the prior value has to be recorded for it too —
		// otherwise the leading-operator form silently yields 0 (a timeout of
		// 0, not the untouched 30) exactly where it matters most.
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		base, err := engine.ParseYAML([]byte("timeout: 30\njobs:\n- name: web\n  count: 1\n"))
		So(err, ShouldBeNil)

		overlay, err := engine.ParseYAML([]byte("timeout: (( calc \"* 2\" ))\njobs:\n- name: web\n  count: 4\n"))
		So(err, ShouldBeNil)

		result, err := engine.Merge(context.Background(), base, overlay).Execute()
		So(err, ShouldBeNil)

		val, getErr := result.Get("timeout")
		So(getErr, ShouldBeNil)
		So(val, ShouldEqual, int64(60))
	})

	Convey("a nested path under the legacy merger records its prior value too", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		base, err := engine.ParseYAML([]byte("service:\n  timeout: 30\njobs:\n- name: web\n"))
		So(err, ShouldBeNil)

		overlay, err := engine.ParseYAML([]byte("service:\n  timeout: (( calc \"* 3\" ))\njobs:\n- name: web\n"))
		So(err, ShouldBeNil)

		result, err := engine.Merge(context.Background(), base, overlay).Execute()
		So(err, ShouldBeNil)

		val, getErr := result.Get("service.timeout")
		So(getErr, ShouldBeNil)
		So(val, ShouldEqual, int64(90))
	})

	Convey("the single-file base:/overlay: doc shape still yields 0 — nothing is actually overwritten", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte("base:\n  timeout: 30\noverlay:\n  timeout: (( calc \"* 2\" ))\n"))
		So(err, ShouldBeNil)

		result, err := engine.Evaluate(context.Background(), doc)
		So(err, ShouldBeNil)

		val, getErr := result.Get("overlay.timeout")
		So(getErr, ShouldBeNil)
		So(val, ShouldEqual, int64(0))
	})
}

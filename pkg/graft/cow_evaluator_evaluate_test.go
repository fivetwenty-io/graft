package graft

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestCOWEvaluator_Evaluate_NoOperators(t *testing.T) {
	Convey("Evaluate with plain data preserves all values", t, func() {
		data := map[string]interface{}{
			"name":    "test-app",
			"version": 42,
			"nested": map[string]interface{}{
				"key1": "value1",
				"key2": "value2",
			},
		}

		ce := NewCOWEvaluator(data)
		err := ce.Evaluate(context.Background())
		So(err, ShouldBeNil)

		name, err := ce.GetValue("name")
		So(err, ShouldBeNil)
		So(name, ShouldEqual, "test-app")

		version, err := ce.GetValue("version")
		So(err, ShouldBeNil)
		So(version, ShouldEqual, 42)

		k1, err := ce.GetValue("nested", "key1")
		So(err, ShouldBeNil)
		So(k1, ShouldEqual, "value1")

		k2, err := ce.GetValue("nested", "key2")
		So(err, ShouldBeNil)
		So(k2, ShouldEqual, "value2")
	})
}

func TestCOWEvaluator_Evaluate_WithGrab(t *testing.T) {
	Convey("Evaluate resolves (( grab )) operators", t, func() {
		data := map[string]interface{}{
			"source": "hello-world",
			"target": "(( grab source ))",
		}

		ce := NewCOWEvaluator(data)
		err := ce.Evaluate(context.Background())
		So(err, ShouldBeNil)

		val, err := ce.GetValue("target")
		So(err, ShouldBeNil)
		So(val, ShouldEqual, "hello-world")
	})
}

func TestCOWEvaluator_Evaluate_WithConcat(t *testing.T) {
	Convey("Evaluate resolves (( concat )) operators", t, func() {
		data := map[string]interface{}{
			"first":  "hello",
			"second": "world",
			"result": "(( concat first \" \" second ))",
		}

		ce := NewCOWEvaluator(data)
		err := ce.Evaluate(context.Background())
		So(err, ShouldBeNil)

		val, err := ce.GetValue("result")
		So(err, ShouldBeNil)
		So(val, ShouldEqual, "hello world")
	})
}

func TestCOWEvaluator_Evaluate_PreservesTreeOnError(t *testing.T) {
	Convey("Evaluate preserves non-operator data when errors occur", t, func() {
		data := map[string]interface{}{
			"good_key": "good_value",
			"bad_ref":  "(( grab nonexistent.path ))",
		}

		ce := NewCOWEvaluator(data)
		err := ce.Evaluate(context.Background())

		// The evaluation should return an error for the unresolvable reference
		So(err, ShouldNotBeNil)

		// The original good_key should still be accessible since the tree
		// was not replaced on error
		val, getErr := ce.GetValue("good_key")
		So(getErr, ShouldBeNil)
		So(val, ShouldEqual, "good_value")
	})
}

func TestCOWEvaluator_Evaluate_EmptyTree(t *testing.T) {
	Convey("Evaluate on empty tree returns no error", t, func() {
		data := map[string]interface{}{}

		ce := NewCOWEvaluator(data)
		err := ce.Evaluate(context.Background())
		So(err, ShouldBeNil)
	})
}

func TestCOWEvaluator_Evaluate_ContextCancellation(t *testing.T) {
	Convey("Evaluate with cancelled context does not panic", t, func() {
		data := map[string]interface{}{
			"key": "value",
		}

		ce := NewCOWEvaluator(data)
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // cancel immediately

		// Should not panic; may return error or nil depending on timing
		So(func() { ce.Evaluate(ctx) }, ShouldNotPanic)
	})
}

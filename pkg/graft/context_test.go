package graft

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestCherryPickContextPropagation(t *testing.T) {
	Convey("Cherry-pick paths context propagation", t, func() {
		Convey("WithCherryPickPaths() should add paths to context", func() {
			ctx := context.Background()
			paths := []string{"params", "meta.environment"}

			newCtx := WithCherryPickPaths(ctx, paths)

			So(newCtx, ShouldNotBeNil)
			So(newCtx, ShouldNotEqual, ctx)
		})

		Convey("GetCherryPickPaths() should retrieve paths from context", func() {
			ctx := context.Background()
			expectedPaths := []string{"params", "meta.environment", "instances"}

			ctx = WithCherryPickPaths(ctx, expectedPaths)
			retrievedPaths := GetCherryPickPaths(ctx)

			So(retrievedPaths, ShouldNotBeNil)
			So(len(retrievedPaths), ShouldEqual, len(expectedPaths))
			So(retrievedPaths, ShouldResemble, expectedPaths)
		})

		Convey("GetCherryPickPaths() should return nil for context without paths", func() {
			ctx := context.Background()

			retrievedPaths := GetCherryPickPaths(ctx)

			So(retrievedPaths, ShouldBeNil)
		})

		Convey("Context propagation through MergeBuilder", func() {
			// Create test documents
			doc1 := NewDocument(map[string]interface{}{
				"params": map[string]interface{}{
					"username": "admin",
					"port":     8080,
				},
				"meta": map[string]interface{}{
					"environment": "production",
					"region":      "us-east-1",
				},
				"other": map[string]interface{}{
					"data": "should not be in output",
				},
			})

			// Create engine
			engine, err := NewEngine()
			So(err, ShouldBeNil)

			// Test that cherry-pick paths are propagated through MergeBuilder
			builder := engine.Merge(context.Background(), doc1)
			mergeBuilder := builder.WithCherryPick("params", "meta.environment")

			// Check that cherry-pick paths are stored in the builder
			impl, ok := mergeBuilder.(*mergeBuilderImpl)
			So(ok, ShouldBeTrue)
			So(impl.cherryPickKeys, ShouldNotBeNil)
			So(len(impl.cherryPickKeys), ShouldEqual, 2)
			So(impl.cherryPickKeys, ShouldContain, "params")
			So(impl.cherryPickKeys, ShouldContain, "meta.environment")
		})

		Convey("Context propagation to Evaluator", func() {
			// Create test document with operators
			doc := NewDocument(map[string]interface{}{
				"params": map[string]interface{}{
					"username": "(( grab meta.user ))",
					"port":     8080,
				},
				"meta": map[string]interface{}{
					"user":        "testuser",
					"environment": "(( grab config.env ))",
				},
				"config": map[string]interface{}{
					"env": "production",
				},
			})

			// Create engine
			engine, err := NewEngine()
			So(err, ShouldBeNil)
			_ = engine // engine not used in this test

			// Create evaluator with cherry-pick paths via context
			ctx := WithCherryPickPaths(context.Background(), []string{"params"})

			// The evaluator should receive the cherry-pick paths
			rawData, ok := doc.RawData().(map[string]interface{})
			So(ok, ShouldBeTrue)
			evaluator := &Evaluator{
				Tree: rawData,
			}

			// Extract paths from context (simulating what Engine.Evaluate does)
			if paths := GetCherryPickPaths(ctx); paths != nil {
				evaluator.CherryPickPaths = paths
			}

			So(evaluator.CherryPickPaths, ShouldNotBeNil)
			So(len(evaluator.CherryPickPaths), ShouldEqual, 1)
			So(evaluator.CherryPickPaths[0], ShouldEqual, "params")
		})

		Convey("Multiple WithCherryPickPaths calls should replace paths", func() {
			ctx := context.Background()

			// First set of paths
			ctx = WithCherryPickPaths(ctx, []string{"params", "meta"})
			paths1 := GetCherryPickPaths(ctx)
			So(len(paths1), ShouldEqual, 2)

			// Second set of paths should replace the first
			ctx = WithCherryPickPaths(ctx, []string{"config", "instances", "networks"})
			paths2 := GetCherryPickPaths(ctx)
			So(len(paths2), ShouldEqual, 3)
			So(paths2, ShouldContain, "config")
			So(paths2, ShouldContain, "instances")
			So(paths2, ShouldContain, "networks")
			So(paths2, ShouldNotContain, "params")
			So(paths2, ShouldNotContain, "meta")
		})

		Convey("Empty paths should be handled correctly", func() {
			ctx := context.Background()

			// Empty slice
			ctx = WithCherryPickPaths(ctx, []string{})
			paths := GetCherryPickPaths(ctx)
			So(paths, ShouldNotBeNil)
			So(len(paths), ShouldEqual, 0)

			// nil slice
			ctx = WithCherryPickPaths(ctx, nil)
			paths = GetCherryPickPaths(ctx)
			So(paths, ShouldBeNil)
		})
	})
}

func TestEngineEvaluateWithCherryPick(t *testing.T) {
	Convey("Engine.Evaluate() with cherry-pick context", t, func() {
		Convey("Should pass cherry-pick paths to evaluator", func() {
			doc := NewDocument(map[string]interface{}{
				"params": map[string]interface{}{
					"value": "(( grab meta.data ))",
				},
				"meta": map[string]interface{}{
					"data": "test-value",
				},
				"other": map[string]interface{}{
					"skip": "(( grab missing.value ))", // This would cause error if evaluated
				},
			})

			engine, err := NewEngine()
			So(err, ShouldBeNil)
			ctx := WithCherryPickPaths(context.Background(), []string{"params", "meta"})

			result, err := engine.Evaluate(ctx, doc)

			So(err, ShouldBeNil)
			So(result, ShouldNotBeNil)

			// Verify the operator was evaluated
			data, ok := result.RawData().(map[string]interface{})
			So(ok, ShouldBeTrue)
			params, ok := data["params"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(params["value"], ShouldEqual, "test-value")

			// The "other" section should still be present but unevaluated
			// since we didn't prune, just selectively evaluated
			other, ok := data["other"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(other["skip"], ShouldEqual, "(( grab missing.value ))")
		})
	})
}

func TestMergeBuilderCherryPickIntegration(t *testing.T) {
	Convey("MergeBuilder cherry-pick integration", t, func() {
		Convey("Should propagate cherry-pick paths through merge and evaluate", func() {
			doc1 := NewDocument(map[string]interface{}{
				"base": map[string]interface{}{
					"url": "http://example.com",
				},
				"params": map[string]interface{}{
					"endpoint": "(( concat base.url \"/api\" ))",
				},
			})

			doc2 := NewDocument(map[string]interface{}{
				"params": map[string]interface{}{
					"timeout": 30,
				},
				"extra": map[string]interface{}{
					"bad": "(( grab nonexistent ))", // Would error if evaluated
				},
			})

			engine, err := NewEngine()
			So(err, ShouldBeNil)

			// Use MergeBuilder with cherry-pick
			result, err := engine.Merge(context.Background(), doc1, doc2).
				WithCherryPick("params", "base").
				Execute()

			So(err, ShouldBeNil)
			So(result, ShouldNotBeNil)

			// Check that cherry-picked paths are evaluated
			data, ok := result.RawData().(map[string]interface{})
			So(ok, ShouldBeTrue)
			params, ok := data["params"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(params["endpoint"], ShouldEqual, "http://example.com/api")
			So(params["timeout"], ShouldEqual, 30)

			// Check that only cherry-picked paths remain
			So(data["extra"], ShouldBeNil)
		})

		Convey("Should handle nested cherry-pick paths", func() {
			doc := NewDocument(map[string]interface{}{
				"meta": map[string]interface{}{
					"environment": map[string]interface{}{
						"name":   "prod",
						"region": "(( grab cloud.region ))",
					},
					"version": "1.0",
				},
				"cloud": map[string]interface{}{
					"region": "us-west-2",
				},
				"skip": map[string]interface{}{
					"error": "(( grab undefined ))",
				},
			})

			engine, err := NewEngine()
			So(err, ShouldBeNil)

			result, err := engine.Merge(context.Background(), doc).
				WithCherryPick("meta.environment", "cloud").
				Execute()

			So(err, ShouldBeNil)
			So(result, ShouldNotBeNil)

			// Verify structure
			data, ok := result.RawData().(map[string]interface{})
			So(ok, ShouldBeTrue)
			meta, ok := data["meta"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			env, ok := meta["environment"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(env["name"], ShouldEqual, "prod")
			So(env["region"], ShouldEqual, "us-west-2")

			// meta.version should not be present
			So(meta["version"], ShouldBeNil)

			// skip section should not be present
			So(data["skip"], ShouldBeNil)
		})
	})
}

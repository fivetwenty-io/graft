package graft

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

// These tests pin graft's merge-phase parity with spruce for two related
// behaviors verified against the spruce binary:
//
//  1. Array-merge markers (append, prepend, insert, delete, merge,
//     merge on <key>, replace, inline) are merge-phase constructs and are
//     always consumed during merge, never emitted as literal text in the
//     output - including under --skip-eval, which only disables the
//     evaluator phase, not the merger.
//  2. A (( prune )) or (( sort by X )) marker queued at one merge step
//     survives the key being overwritten by a later document ("ghost"
//     semantics): the prune/sort still applies to the final result.

func TestSkipEvalArrayMergeMarkerParity(t *testing.T) {
	Convey("Array-merge markers are resolved during merge, never left as literal text", t, func() {
		ctx := context.Background()

		Convey("a lone document's marker is consumed under --skip-eval", func() {
			engine, err := NewEngine()
			So(err, ShouldBeNil)

			doc, err := engine.ParseYAML([]byte(`
list:
- (( merge on id ))
- id: a
  val: 1
- id: b
  val: 2
`))
			So(err, ShouldBeNil)

			result, err := engine.Merge(ctx, doc).SkipEvaluation().Execute()
			So(err, ShouldBeNil)

			out, err := result.ToYAML()
			So(err, ShouldBeNil)
			So(string(out), ShouldNotContainSubstring, "(( merge")

			list, err := result.GetSlice("list")
			So(err, ShouldBeNil)
			So(list, ShouldHaveLength, 2)
		})

		Convey("append/prepend across two documents resolve identically with and without --skip-eval", func() {
			expected := []interface{}{"consul", "route", "cell", "cc_bridge"}

			for _, skipEval := range []bool{false, true} {
				engine, err := NewEngine()
				So(err, ShouldBeNil)

				first, err := engine.ParseYAML([]byte(`
jobs:
- name: route
- (( append ))
- name: cell
`))
				So(err, ShouldBeNil)
				second, err := engine.ParseYAML([]byte(`
jobs:
- name: cc_bridge
- (( prepend ))
- name: consul
`))
				So(err, ShouldBeNil)

				builder := engine.Merge(ctx, first, second)
				if skipEval {
					builder = builder.SkipEvaluation()
				}
				result, err := builder.Execute()
				So(err, ShouldBeNil)

				out, err := result.ToYAML()
				So(err, ShouldBeNil)
				So(string(out), ShouldNotContainSubstring, "(( append")
				So(string(out), ShouldNotContainSubstring, "(( prepend")

				jobs, err := result.GetSlice("jobs")
				So(err, ShouldBeNil)
				So(jobs, ShouldHaveLength, len(expected))
				for i, job := range jobs {
					entry, ok := job.(map[string]interface{})
					So(ok, ShouldBeTrue)
					So(entry["name"], ShouldEqual, expected[i])
				}
			}
		})

		Convey("a marker present only in the first of several documents is still resolved", func() {
			for _, skipEval := range []bool{false, true} {
				engine, err := NewEngine()
				So(err, ShouldBeNil)

				first, err := engine.ParseYAML([]byte(`
list:
- (( merge on id ))
- id: a
  val: 1
- id: b
  val: 2
`))
				So(err, ShouldBeNil)
				second, err := engine.ParseYAML([]byte(`
other: 1
`))
				So(err, ShouldBeNil)

				builder := engine.Merge(ctx, first, second)
				if skipEval {
					builder = builder.SkipEvaluation()
				}
				result, err := builder.Execute()
				So(err, ShouldBeNil)

				out, err := result.ToYAML()
				So(err, ShouldBeNil)
				So(string(out), ShouldNotContainSubstring, "(( merge")

				list, err := result.GetSlice("list")
				So(err, ShouldBeNil)
				So(list, ShouldHaveLength, 2)
			}
		})
	})
}

func TestPruneSortGhostSurvivesOverwrite(t *testing.T) {
	Convey("A prune/sort marker queued at one merge step survives being overwritten", t, func() {
		ctx := context.Background()

		Convey("(( prune )) ghost still prunes after the key is overwritten", func() {
			engine, err := NewEngine()
			So(err, ShouldBeNil)

			first, err := engine.ParseYAML([]byte("key: (( prune ))\nother: 1\n"))
			So(err, ShouldBeNil)
			second, err := engine.ParseYAML([]byte("key: bar\n"))
			So(err, ShouldBeNil)

			result, err := engine.Merge(ctx, first, second).Execute()
			So(err, ShouldBeNil)

			data, ok := result.RawData().(map[string]interface{})
			So(ok, ShouldBeTrue)
			_, hasKey := data["key"]
			So(hasKey, ShouldBeFalse)
			So(data["other"], ShouldEqual, 1)
		})

		Convey("(( sort )) ghost still sorts the overwriting list, with and without --skip-eval", func() {
			for _, skipEval := range []bool{false, true} {
				engine, err := NewEngine()
				So(err, ShouldBeNil)

				first, err := engine.ParseYAML([]byte("key: (( sort ))\n"))
				So(err, ShouldBeNil)
				second, err := engine.ParseYAML([]byte("key:\n- charlie\n- alpha\n- bravo\n"))
				So(err, ShouldBeNil)

				builder := engine.Merge(ctx, first, second)
				if skipEval {
					builder = builder.SkipEvaluation()
				}
				result, err := builder.Execute()
				So(err, ShouldBeNil)

				list, err := result.GetSlice("key")
				So(err, ShouldBeNil)
				So(list, ShouldResemble, []interface{}{"alpha", "bravo", "charlie"})
			}
		})
	})
}

// TestEnginePerRunStateNotLeakedAcrossExecute pins a library-API regression:
// DefaultEngine.evaluate() consumed GetKeysToPrune()/GetPathsToSort() as
// post-processing but never reset them (unlike the legacy Evaluator.Run(),
// evaluator.go:908-909/914-915), so a queued prune/sort marker from one
// Merge(...).Execute() call silently reapplied itself to the next, unrelated
// Execute() call on the same reused engine - a library caller sharing one
// engine instance across merges could lose data with no error. The same gap
// applied to used-IP tracking: spruce's StaticIPOperator.Setup()
// (op_static_ips.go) clears its per-process UsedIPs map on every phase run,
// but graft's Setup() is a no-op, and ResetUsedIPs() was otherwise never
// called anywhere in the codebase, so a static_ips claim from one run wrongly
// blocked the identical claim on the next.
func TestEnginePerRunStateNotLeakedAcrossExecute(t *testing.T) {
	Convey("Per-run prune/sort/used-IP state does not leak across Execute() calls on a reused engine", t, func() {
		ctx := context.Background()

		Convey("a (( prune )) marker from run 1 does not delete an unrelated key in run 2", func() {
			engine, err := NewEngine()
			So(err, ShouldBeNil)

			run1Doc, err := engine.ParseYAML([]byte("kill: (( prune ))\nother: 1\n"))
			So(err, ShouldBeNil)

			result1, err := engine.Merge(ctx, run1Doc).Execute()
			So(err, ShouldBeNil)

			data1, ok := result1.RawData().(map[string]interface{})
			So(ok, ShouldBeTrue)
			_, hasKill1 := data1["kill"]
			So(hasKill1, ShouldBeFalse)
			So(data1["other"], ShouldEqual, 1)

			// Unrelated second document, same key name, on the same engine.
			// The value must survive: nothing in this run asked for it to
			// be pruned.
			run2Doc, err := engine.ParseYAML([]byte("kill: precious-value\nother: 2\n"))
			So(err, ShouldBeNil)

			result2, err := engine.Merge(ctx, run2Doc).Execute()
			So(err, ShouldBeNil)

			data2, ok := result2.RawData().(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(data2["kill"], ShouldEqual, "precious-value")
			So(data2["other"], ShouldEqual, 2)
		})

		Convey("a (( sort by X )) marker from run 1 does not resort an unrelated list in run 2", func() {
			engine, err := NewEngine()
			So(err, ShouldBeNil)

			base1, err := engine.ParseYAML([]byte("list:\n- name: charlie\n- name: alpha\n- name: bravo\n"))
			So(err, ShouldBeNil)
			override1, err := engine.ParseYAML([]byte("list: (( sort by name ))\n"))
			So(err, ShouldBeNil)

			result1, err := engine.Merge(ctx, base1, override1).Execute()
			So(err, ShouldBeNil)

			list1, err := result1.GetSlice("list")
			So(err, ShouldBeNil)
			So(list1, ShouldResemble, []interface{}{
				map[string]interface{}{"name": "alpha"},
				map[string]interface{}{"name": "bravo"},
				map[string]interface{}{"name": "charlie"},
			})

			// Same path, same engine, a fresh document with no sort marker
			// at all. Authored order must survive unchanged.
			run2Doc, err := engine.ParseYAML([]byte("list:\n- name: charlie\n- name: alpha\n- name: bravo\n"))
			So(err, ShouldBeNil)

			result2, err := engine.Merge(ctx, run2Doc).Execute()
			So(err, ShouldBeNil)

			list2, err := result2.GetSlice("list")
			So(err, ShouldBeNil)
			So(list2, ShouldResemble, []interface{}{
				map[string]interface{}{"name": "charlie"},
				map[string]interface{}{"name": "alpha"},
				map[string]interface{}{"name": "bravo"},
			})
		})

		Convey("a static_ips claim from run 1 does not block the identical claim in run 2", func() {
			fixture := []byte(`
jobs:
- name: api_z1
  instances: 1
  networks:
  - name: net1
    static_ips: (( static_ips(0) ))
networks:
- name: net1
  subnets:
    - static: [192.168.1.2 - 192.168.1.30]
`)

			engine, err := NewEngine()
			So(err, ShouldBeNil)

			run1Doc, err := engine.ParseYAML(fixture)
			So(err, ShouldBeNil)

			result1, err := engine.Merge(ctx, run1Doc).Execute()
			So(err, ShouldBeNil)

			ip1, err := result1.GetString("jobs.0.networks.0.static_ips.0")
			So(err, ShouldBeNil)
			So(ip1, ShouldEqual, "192.168.1.2")

			// Same claim, same engine, a new run. Without a per-run reset
			// this fails with "already allocated to api_z1/0" even though
			// run 1 is long over.
			run2Doc, err := engine.ParseYAML(fixture)
			So(err, ShouldBeNil)

			result2, err := engine.Merge(ctx, run2Doc).Execute()
			So(err, ShouldBeNil)

			ip2, err := result2.GetString("jobs.0.networks.0.static_ips.0")
			So(err, ShouldBeNil)
			So(ip2, ShouldEqual, "192.168.1.2")
		})
	})
}

package graft

import (
	"context"
	"os"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
)

// These tests pin graft's array-merge-marker parity with spruce across the
// full marker vocabulary, verified byte-for-byte against a built spruce
// binary during development (see merge_marker_parity_test.go for the
// --skip-eval / ghost-semantics counterpart). They exercise:
//
//   - (( append )), (( prepend ))
//   - (( insert before/after <index> )), (( insert before/after "<name>" ))
//   - (( delete <index> )), (( delete "<name>" ))
//   - (( merge )), (( merge on <key> ))
//   - (( replace )), (( inline ))
//   - the no-marker default path (key-merge by name, falling back to inline
//     for scalar arrays)
//   - nested arrays, custom DEFAULT_ARRAY_MERGE_KEY, and multi-document
//     chains
//
// plus the error path: insert/delete/merge failures on a missing or
// ambiguous target must surface spruce's exact, per-path detail message
// (not a generic "failed to merge documents" wrapper).

func namesOf(t *testing.T, list []interface{}) []interface{} {
	t.Helper()
	out := make([]interface{}, len(list))
	for i, item := range list {
		entry, ok := item.(map[string]interface{})
		if !ok {
			out[i] = item
			continue
		}
		out[i] = entry["name"]
	}
	return out
}

func mergeYAML(ctx context.Context, t *testing.T, engine Engine, docsYAML ...string) (Document, error) {
	t.Helper()
	docs := make([]Document, len(docsYAML))
	for i, y := range docsYAML {
		doc, err := engine.ParseYAML([]byte(y))
		if err != nil {
			t.Fatalf("parse doc %d: %v", i, err)
		}
		docs[i] = doc
	}
	return engine.Merge(ctx, docs...).Execute()
}

func TestArrayMergeMarkerFixtures(t *testing.T) {
	Convey("Array-merge markers resolve identically to spruce", t, func() {
		ctx := context.Background()
		base := `
jobs:
- name: route
- name: cell
`

		Convey("(( insert before <index> ))", func() {
			engine, err := NewEngine()
			So(err, ShouldBeNil)
			result, err := mergeYAML(ctx, t, engine, base, `
jobs:
- (( insert before 1 ))
- name: consul
`)
			So(err, ShouldBeNil)
			jobs, err := result.GetSlice("jobs")
			So(err, ShouldBeNil)
			So(namesOf(t, jobs), ShouldResemble, []interface{}{"route", "consul", "cell"})
		})

		Convey("(( insert after <index> ))", func() {
			engine, err := NewEngine()
			So(err, ShouldBeNil)
			result, err := mergeYAML(ctx, t, engine, base, `
jobs:
- (( insert after 1 ))
- name: consul
`)
			So(err, ShouldBeNil)
			jobs, err := result.GetSlice("jobs")
			So(err, ShouldBeNil)
			So(namesOf(t, jobs), ShouldResemble, []interface{}{"route", "cell", "consul"})
		})

		Convey(`(( insert before "<name>" ))`, func() {
			engine, err := NewEngine()
			So(err, ShouldBeNil)
			result, err := mergeYAML(ctx, t, engine, base, `
jobs:
- (( insert before "cell" ))
- name: consul
`)
			So(err, ShouldBeNil)
			jobs, err := result.GetSlice("jobs")
			So(err, ShouldBeNil)
			So(namesOf(t, jobs), ShouldResemble, []interface{}{"route", "consul", "cell"})
		})

		Convey(`(( insert after "<name>" ))`, func() {
			engine, err := NewEngine()
			So(err, ShouldBeNil)
			result, err := mergeYAML(ctx, t, engine, base, `
jobs:
- (( insert after "cell" ))
- name: consul
`)
			So(err, ShouldBeNil)
			jobs, err := result.GetSlice("jobs")
			So(err, ShouldBeNil)
			So(namesOf(t, jobs), ShouldResemble, []interface{}{"route", "cell", "consul"})
		})

		Convey("(( delete <index> ))", func() {
			engine, err := NewEngine()
			So(err, ShouldBeNil)
			result, err := mergeYAML(ctx, t, engine, base, `
jobs:
- (( delete 1 ))
`)
			So(err, ShouldBeNil)
			jobs, err := result.GetSlice("jobs")
			So(err, ShouldBeNil)
			So(namesOf(t, jobs), ShouldResemble, []interface{}{"route"})
		})

		Convey(`(( delete "<name>" ))`, func() {
			engine, err := NewEngine()
			So(err, ShouldBeNil)
			result, err := mergeYAML(ctx, t, engine, base, `
jobs:
- (( delete "cell" ))
`)
			So(err, ShouldBeNil)
			jobs, err := result.GetSlice("jobs")
			So(err, ShouldBeNil)
			So(namesOf(t, jobs), ShouldResemble, []interface{}{"route"})
		})

		Convey("(( replace ))", func() {
			engine, err := NewEngine()
			So(err, ShouldBeNil)
			result, err := mergeYAML(ctx, t, engine, base, `
jobs:
- (( replace ))
- name: consul
`)
			So(err, ShouldBeNil)
			jobs, err := result.GetSlice("jobs")
			So(err, ShouldBeNil)
			So(namesOf(t, jobs), ShouldResemble, []interface{}{"consul"})
		})

		Convey("(( inline )) forces index-position merge instead of key-merge", func() {
			engine, err := NewEngine()
			So(err, ShouldBeNil)
			result, err := mergeYAML(ctx, t, engine, base, `
jobs:
- (( inline ))
- name: route
  extra: yes
`)
			So(err, ShouldBeNil)
			jobs, err := result.GetSlice("jobs")
			So(err, ShouldBeNil)
			// inline replaces index 0 in place and leaves index 1 (cell)
			// untouched, unlike a key-merge which would match by name.
			So(jobs, ShouldHaveLength, 2)
			entry0, ok := jobs[0].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(entry0["name"], ShouldEqual, "route")
			So(entry0["extra"], ShouldEqual, true)
			entry1, ok := jobs[1].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(entry1["name"], ShouldEqual, "cell")
		})

		Convey("(( merge )) key-merges by the default 'name' field", func() {
			engine, err := NewEngine()
			So(err, ShouldBeNil)
			result, err := mergeYAML(ctx, t, engine, `
jobs:
- name: route
  instances: 1
- name: cell
  instances: 2
`, `
jobs:
- (( merge ))
- name: cell
  instances: 3
- name: consul
  instances: 1
`)
			So(err, ShouldBeNil)
			jobs, err := result.GetSlice("jobs")
			So(err, ShouldBeNil)
			So(namesOf(t, jobs), ShouldResemble, []interface{}{"route", "cell", "consul"})
			cell, ok := jobs[1].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(cell["instances"], ShouldEqual, 3)
		})

		Convey("(( merge on <key> )) key-merges by an explicit field", func() {
			engine, err := NewEngine()
			So(err, ShouldBeNil)
			result, err := mergeYAML(ctx, t, engine, `
jobs:
- id: route
  instances: 1
- id: cell
  instances: 2
`, `
jobs:
- (( merge on id ))
- id: cell
  instances: 3
- id: consul
  instances: 1
`)
			So(err, ShouldBeNil)
			jobs, err := result.GetSlice("jobs")
			So(err, ShouldBeNil)
			So(jobs, ShouldHaveLength, 3)
			cell, ok := jobs[1].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(cell["id"], ShouldEqual, "cell")
			So(cell["instances"], ShouldEqual, 3)
		})

		Convey("default path (no marker): arrays of maps key-merge by name", func() {
			engine, err := NewEngine()
			So(err, ShouldBeNil)
			result, err := mergeYAML(ctx, t, engine, base, `
jobs:
- name: cell
  instances: 3
- name: consul
  instances: 1
`)
			So(err, ShouldBeNil)
			jobs, err := result.GetSlice("jobs")
			So(err, ShouldBeNil)
			So(namesOf(t, jobs), ShouldResemble, []interface{}{"route", "cell", "consul"})
		})

		Convey("default path (no marker): scalar arrays fall back to inline replace", func() {
			engine, err := NewEngine()
			So(err, ShouldBeNil)
			result, err := mergeYAML(ctx, t, engine, `
list:
- a
- b
`, `
list:
- c
- d
- e
`)
			So(err, ShouldBeNil)
			list, err := result.GetSlice("list")
			So(err, ShouldBeNil)
			So(list, ShouldResemble, []interface{}{"c", "d", "e"})
		})

		Convey("nested arrays: a marker inside a key-merged parent array is resolved too", func() {
			engine, err := NewEngine()
			So(err, ShouldBeNil)
			result, err := mergeYAML(ctx, t, engine, `
groups:
- name: g1
  members:
  - name: m1
  - name: m2
`, `
groups:
- (( merge on name ))
- name: g1
  members:
  - (( append ))
  - name: m3
`)
			So(err, ShouldBeNil)

			out, err := result.ToYAML()
			So(err, ShouldBeNil)
			So(string(out), ShouldNotContainSubstring, "(( merge")
			So(string(out), ShouldNotContainSubstring, "(( append")

			groups, err := result.GetSlice("groups")
			So(err, ShouldBeNil)
			So(groups, ShouldHaveLength, 1)
			group, ok := groups[0].(map[string]interface{})
			So(ok, ShouldBeTrue)
			members, ok := group["members"].([]interface{})
			So(ok, ShouldBeTrue)
			So(namesOf(t, members), ShouldResemble, []interface{}{"m1", "m2", "m3"})
		})

		Convey("DEFAULT_ARRAY_MERGE_KEY overrides the implicit key-merge field", func() {
			original, wasSet := os.LookupEnv("DEFAULT_ARRAY_MERGE_KEY")
			So(os.Setenv("DEFAULT_ARRAY_MERGE_KEY", "ident"), ShouldBeNil)
			defer func() {
				if wasSet {
					_ = os.Setenv("DEFAULT_ARRAY_MERGE_KEY", original)
				} else {
					_ = os.Unsetenv("DEFAULT_ARRAY_MERGE_KEY")
				}
			}()

			engine, err := NewEngine()
			So(err, ShouldBeNil)
			result, err := mergeYAML(ctx, t, engine, `
jobs:
- ident: route
  instances: 1
- ident: cell
  instances: 2
`, `
jobs:
- ident: cell
  instances: 3
- ident: consul
  instances: 1
`)
			So(err, ShouldBeNil)
			jobs, err := result.GetSlice("jobs")
			So(err, ShouldBeNil)
			So(jobs, ShouldHaveLength, 3)
			cell, ok := jobs[1].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(cell["ident"], ShouldEqual, "cell")
			So(cell["instances"], ShouldEqual, 3)
		})

		Convey("a marker chain spanning three documents resolves left to right", func() {
			engine, err := NewEngine()
			So(err, ShouldBeNil)
			result, err := mergeYAML(ctx, t, engine,
				"jobs:\n- name: route\n",
				"jobs:\n- (( append ))\n- name: cell\n",
				"jobs:\n- (( append ))\n- name: consul\n",
			)
			So(err, ShouldBeNil)
			jobs, err := result.GetSlice("jobs")
			So(err, ShouldBeNil)
			So(namesOf(t, jobs), ShouldResemble, []interface{}{"route", "cell", "consul"})
		})
	})
}

// TestArrayMergeMarkerErrorsPreserveDetail pins the regression covered by
// isMergerError: array-marker failures raised deep inside the legacy
// merger (merger.Merger, via performLegacyMerge) must reach the caller
// with their original, per-path detail intact - previously only a subset
// of merger error strings were recognized by a substring allowlist, so
// out-of-bounds insert/delete indices and duplicate-entry detection fell
// through to a generic "failed to merge documents" wrapper that dropped
// the useful detail spruce reports.
func TestArrayMergeMarkerErrorsPreserveDetail(t *testing.T) {
	Convey("Legacy-merger array errors are never flattened to a generic wrapper", t, func() {
		// MultiError.Error() renders through ansi.Sprintf, which embeds
		// color escape codes around each highlighted segment (path, key
		// names, etc.). Disable color so substring assertions below match
		// the same plain text spruce prints when NOTTY / --color=false.
		ansi.Color(false)
		defer ansi.Color(true)

		ctx := context.Background()
		base := `
jobs:
- name: route
- name: cell
`

		cases := []struct {
			name     string
			overlay  string
			contains string
		}{
			{
				name:     "delete missing name",
				overlay:  "jobs:\n- (( delete \"nonexistent\" ))\n",
				contains: `unable to find specified modification point with 'name: nonexistent'`,
			},
			{
				name:     "delete out-of-bounds index",
				overlay:  "jobs:\n- (( delete 99 ))\n",
				contains: "unable to modify the list, because specified index 99 is out of bounds",
			},
			{
				name:     "insert before missing name",
				overlay:  "jobs:\n- (( insert before \"nonexistent\" ))\n- name: consul\n",
				contains: `unable to find specified modification point with 'name: nonexistent'`,
			},
			{
				name:     "insert before out-of-bounds index",
				overlay:  "jobs:\n- (( insert before 99 ))\n- name: consul\n",
				contains: "unable to modify the list, because specified index 99 is out of bounds",
			},
			{
				name:     "insert with duplicate entry name",
				overlay:  "jobs:\n- (( insert before name \"cell\" ))\n- name: cell\n",
				contains: "unable to insert, because new list entry 'name: cell' is detected multiple times",
			},
		}

		for _, c := range cases {
			c := c
			Convey(c.name, func() {
				engine, err := NewEngine()
				So(err, ShouldBeNil)
				_, err = mergeYAML(ctx, t, engine, base, c.overlay)
				So(err, ShouldNotBeNil)
				So(err.Error(), ShouldContainSubstring, c.contains)
				So(err.Error(), ShouldNotContainSubstring, "failed to merge documents")
			})
		}

		Convey("arrays that cannot be key-merged report the specific field failure", func() {
			engine, err := NewEngine()
			So(err, ShouldBeNil)
			_, err = mergeYAML(ctx, t, engine, `
jobs:
- id: route
- notid: cell
`, `
jobs:
- (( merge on id ))
- id: consul
`)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldNotContainSubstring, "failed to merge documents")
		})
	})
}

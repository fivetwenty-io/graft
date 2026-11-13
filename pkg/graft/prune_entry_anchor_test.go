package graft_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// A list entry is a prune entry only when the whole string is the
// (( prune )) call. An entry that starts with the call and goes on, such
// as "(( prune )) x", is a plain string, and every list that adds it
// keeps it, as spruce does. Where an inline merge lines a new entry up
// with an original one, the original is pruned as before, since spruce
// does the same there.
func TestPruneEntryMatchIsAnchored(t *testing.T) {
	const tail = "(( prune )) x"
	const or = `(( prune )) || "x"`
	cases := []struct {
		name     string
		srcs     []string
		fallback bool
		want     map[string]interface{}
	}{
		{
			name: "one document",
			srcs: []string{"l: [\"(( prune )) x\", a]\n" +
				"m: ['(( prune )) || \"x\"', b]\n"},
			want: map[string]interface{}{
				"l": []interface{}{tail, "a"},
				"m": []interface{}{or, "b"},
			},
		},
		{
			name: "one document beside a real prune",
			srcs: []string{"l: [\"(( prune )) x\", a, (( prune ))]\n" +
				"k: (( prune ))\n"},
			want: map[string]interface{}{
				"l": []interface{}{tail, "a"},
			},
		},
		{
			name: "one document with a list of maps",
			srcs: []string{"l: [\"(( prune )) x\", {name: a}]\n"},
			want: map[string]interface{}{
				"l": []interface{}{tail,
					map[string]interface{}{"name": "a"}},
			},
		},
		{
			name: "append by default",
			srcs: []string{"l: [z]\n",
				"l: [\"(( prune )) x\", a, (( prune ))]\n"},
			fallback: true,
			want:     map[string]interface{}{"l": []interface{}{"z", tail, "a"}},
		},
		{
			name: "prepend",
			srcs: []string{"l: [z]\n",
				"l: [(( prepend )), \"(( prune )) x\", a]\n"},
			want: map[string]interface{}{"l": []interface{}{tail, "a", "z"}},
		},
		{
			name: "append",
			srcs: []string{"l: [z]\n",
				"l: [(( append )), \"(( prune )) x\", a]\n"},
			want: map[string]interface{}{"l": []interface{}{"z", tail, "a"}},
		},
		{
			name: "insert",
			srcs: []string{"l: [z]\n",
				"l: [(( insert after 0 )), \"(( prune )) x\", a]\n"},
			want: map[string]interface{}{"l": []interface{}{"z", tail, "a"}},
		},
		{
			name: "replace",
			srcs: []string{"l: [z]\n",
				"l: [(( replace )), \"(( prune )) x\", a]\n"},
			want: map[string]interface{}{"l": []interface{}{tail, "a"}},
		},
		{
			name: "inline past the original's end",
			srcs: []string{"l: [z]\n",
				"l: [(( inline )), q, \"(( prune )) x\", (( prune )), r]\n"},
			want: map[string]interface{}{"l": []interface{}{"q", tail, "r"}},
		},
		{
			name: "inline over an original entry",
			srcs: []string{"l: [z, y]\n",
				"l: [\"(( prune )) x\", a]\n"},
			want: map[string]interface{}{"l": []interface{}{"a"}},
		},
	}
	ctx := context.Background()
	for _, tc := range cases {
		ways := map[string]func(graft.Engine, []graft.Document) (graft.Document, error){
			"Merge": func(e graft.Engine, docs []graft.Document) (graft.Document, error) {
				b := e.Merge(ctx, docs...)
				if tc.fallback {
					b = b.FallbackAppend()
				}
				return b.Execute()
			},
			"SkipEvaluation then Evaluate": func(e graft.Engine, docs []graft.Document) (graft.Document, error) {
				b := e.Merge(ctx, docs...).SkipEvaluation()
				if tc.fallback {
					b = b.FallbackAppend()
				}
				merged, err := b.Execute()
				if err != nil {
					return nil, err
				}
				return e.Evaluate(ctx, merged)
			},
		}
		if len(tc.srcs) == 1 {
			ways["Evaluate"] = func(e graft.Engine, docs []graft.Document) (graft.Document, error) {
				return e.Evaluate(ctx, docs[0])
			}
		}
		for _, parallel := range []bool{true, false} {
			for way, run := range ways {
				t.Run(fmt.Sprintf("%s/%s/parallel=%v", tc.name, way, parallel), func(t *testing.T) {
					e := markerEngine(t, parallel)
					docs := make([]graft.Document, 0, len(tc.srcs))
					for _, src := range tc.srcs {
						docs = append(docs, markerParse(t, e, src))
					}
					doc, err := run(e, docs)
					if err != nil {
						t.Fatalf("err = %v", err)
					}
					got := doc.RawData().(map[string]interface{})
					if !reflect.DeepEqual(got, tc.want) {
						t.Fatalf("got %#v, want %#v", got, tc.want)
					}
				})
			}
		}
	}
}

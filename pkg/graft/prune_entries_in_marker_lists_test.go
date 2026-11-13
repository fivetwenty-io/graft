package graft_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// Each (( prune )) entry of a list that a prepend, an append, an
// insert, a replace, or an inline merge adds is dropped, and every
// other entry stays, however many prune entries the list holds. A grab
// of the list reads it without them. Merge, Evaluate after a merge with
// SkipEvaluation, and Evaluate on a lone document give the same result.
func TestPruneEntriesInAnAddedListAllGo(t *testing.T) {
	cases := []struct {
		name     string
		srcs     []string
		fallback bool
		want     map[string]interface{}
	}{
		{
			name: "prepend",
			srcs: []string{"l: [z]\n",
				"l: [(( prepend )), (( prune )), a, (( prune )), b]\n"},
			want: map[string]interface{}{"l": []interface{}{"a", "b", "z"}},
		},
		{
			name: "append",
			srcs: []string{"l: [z]\n",
				"l: [(( append )), (( prune )), a, (( prune )), b]\n"},
			want: map[string]interface{}{"l": []interface{}{"z", "a", "b"}},
		},
		{
			name: "append by default",
			srcs: []string{"l: [z]\n",
				"l: [(( prune )), a, (( prune )), b]\n"},
			fallback: true,
			want:     map[string]interface{}{"l": []interface{}{"z", "a", "b"}},
		},
		{
			name: "insert after an index",
			srcs: []string{"l: [z]\n",
				"l: [(( insert after 0 )), (( prune )), a, (( prune )), b]\n"},
			want: map[string]interface{}{"l": []interface{}{"z", "a", "b"}},
		},
		{
			name: "insert before a name",
			srcs: []string{"l: [z, y]\n",
				"l: [(( insert before \"y\" )), (( prune )), a, (( prune )), b]\n"},
			want: map[string]interface{}{"l": []interface{}{"z", "a", "b", "y"}},
		},
		{
			name: "insert after a keyed entry",
			srcs: []string{"l: [{name: z}]\n",
				"l: [(( insert after \"z\" )), (( prune )), {name: a}, (( prune )), {name: b}]\n"},
			want: map[string]interface{}{"l": []interface{}{
				map[string]interface{}{"name": "z"},
				map[string]interface{}{"name": "a"},
				map[string]interface{}{"name": "b"},
			}},
		},
		{
			name: "replace",
			srcs: []string{"l: [z]\n",
				"l: [(( replace )), (( prune )), a, (( prune )), b]\n"},
			want: map[string]interface{}{"l": []interface{}{"a", "b"}},
		},
		{
			name: "replace on one document",
			srcs: []string{"l: [(( replace )), (( prune )), a, (( prune )), b]\n" +
				"g: (( grab l.1 ))\n"},
			want: map[string]interface{}{
				"l": []interface{}{"a", "b"},
				"g": "b",
			},
		},
		{
			name: "inline",
			srcs: []string{"l: [z]\n",
				"l: [(( inline )), q, (( prune )), r, (( prune )), s]\n"},
			want: map[string]interface{}{"l": []interface{}{"q", "r", "s"}},
		},
		{
			name: "grab of a prepended list",
			srcs: []string{"l: [z]\n",
				"l: [(( prepend )), (( prune )), a]\ng: (( grab l.0 ))\n"},
			want: map[string]interface{}{
				"l": []interface{}{"a", "z"},
				"g": "a",
			},
		},
		{
			name: "one document",
			srcs: []string{"l: [(( append )), (( prune )), a, (( prune )), b]\n" +
				"g: (( grab l.1 ))\n"},
			want: map[string]interface{}{
				"l": []interface{}{"a", "b"},
				"g": "b",
			},
		},
	}
	ctx := context.Background()
	for _, tc := range cases {
		for _, parallel := range []bool{true, false} {
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

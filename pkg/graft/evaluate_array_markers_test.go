package graft_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// markerEngine returns an engine in the parallel or the sequential mode.
func markerEngine(t *testing.T, parallel bool) graft.Engine {
	t.Helper()
	engine, err := graft.NewEngine(graft.WithParallel(parallel))
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

// markerParse parses src with engine.
func markerParse(t *testing.T, engine graft.Engine, src string) graft.Document {
	t.Helper()
	doc, err := engine.ParseYAML([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// markerWays returns what each library path makes of src, by name.
// Every path parses src afresh, so none sees another's changes.
func markerWays(t *testing.T, parallel bool, src string) map[string]func() (graft.Document, error) {
	t.Helper()
	ctx := context.Background()
	return map[string]func() (graft.Document, error){
		"Merge": func() (graft.Document, error) {
			e := markerEngine(t, parallel)
			return e.Merge(ctx, markerParse(t, e, src)).Execute()
		},
		"Evaluate": func() (graft.Document, error) {
			e := markerEngine(t, parallel)
			return e.Evaluate(ctx, markerParse(t, e, src))
		},
		"Evaluate twice": func() (graft.Document, error) {
			e := markerEngine(t, parallel)
			doc := markerParse(t, e, src)
			if _, err := e.Evaluate(ctx, doc); err != nil {
				return nil, err
			}
			return e.Evaluate(ctx, doc)
		},
		"SkipEvaluation then Evaluate": func() (graft.Document, error) {
			e := markerEngine(t, parallel)
			doc := markerParse(t, e, src)
			merged, err := e.Merge(ctx, doc).SkipEvaluation().Execute()
			if err != nil {
				return nil, err
			}
			return e.Evaluate(ctx, merged)
		},
	}
}

// Evaluate on a document that holds an array marker gives what a merge
// of the document gives, because it runs the merger's step for a lone
// document first. Evaluating the result again, or evaluating what a
// merge with SkipEvaluation returns, gives the same.
func TestEvaluateResolvesArrayMarkers(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want map[string]interface{}
	}{
		{
			name: "append",
			src:  "l: [(( append )), a]\ng: (( grab l ))\n",
			want: map[string]interface{}{
				"l": []interface{}{"a"},
				"g": []interface{}{"a"},
			},
		},
		{
			name: "replace",
			src:  "l: [(( replace )), a]\ng: (( grab l.0 ))\n",
			want: map[string]interface{}{
				"l": []interface{}{"a"},
				"g": "a",
			},
		},
		{
			name: "prepend and inline under a map",
			src: "m:\n  l: [(( prepend )), a, b]\n  k: [(( inline )), c]\n" +
				"g: (( grab m.l.1 ))\nj: (( join \",\" m.k ))\n",
			want: map[string]interface{}{
				"m": map[string]interface{}{
					"l": []interface{}{"a", "b"},
					"k": []interface{}{"c"},
				},
				"g": "b",
				"j": "c",
			},
		},
		{
			name: "append inside a list of maps",
			src: "jobs:\n- name: web\n  l: [(( append )), x]\n" +
				"g: (( grab jobs.web.l ))\n",
			want: map[string]interface{}{
				"jobs": []interface{}{
					map[string]interface{}{
						"name": "web",
						"l":    []interface{}{"x"},
					},
				},
				"g": []interface{}{"x"},
			},
		},
	}
	for _, tc := range cases {
		for _, parallel := range []bool{true, false} {
			for way, run := range markerWays(t, parallel, tc.src) {
				t.Run(fmt.Sprintf("%s/%s/parallel=%v", tc.name, way, parallel), func(t *testing.T) {
					doc, err := run()
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

// Evaluate refuses an array marker the merger refuses, as a merge of
// the document does.
func TestEvaluateRefusesWhatTheMergerRefuses(t *testing.T) {
	src := "l: [(( insert after 0 )), x]\n"
	for _, parallel := range []bool{true, false} {
		for way, run := range markerWays(t, parallel, src) {
			t.Run(fmt.Sprintf("%s/parallel=%v", way, parallel), func(t *testing.T) {
				if _, err := run(); err == nil {
					t.Fatal("err = nil, want the merger's error")
				}
			})
		}
	}
}

// A document a merge returns holds no array marker, so Evaluate gives
// it back as it is.
func TestEvaluateLeavesAMergedDocumentAlone(t *testing.T) {
	ctx := context.Background()
	for _, parallel := range []bool{true, false} {
		t.Run(fmt.Sprintf("parallel=%v", parallel), func(t *testing.T) {
			e := markerEngine(t, parallel)
			base := markerParse(t, e, "l: [z]\njobs:\n- name: web\n  k: 1\n")
			over := markerParse(t, e, "l: [(( append )), a]\n"+
				"jobs:\n- (( append ))\n- name: db\n  k: 2\n")
			merged, err := e.Merge(ctx, base, over).Execute()
			if err != nil {
				t.Fatal(err)
			}
			want := merged.Clone().RawData()
			got, err := e.Evaluate(ctx, merged)
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if !reflect.DeepEqual(got.RawData(), want) {
				t.Fatalf("got %#v, want %#v", got.RawData(), want)
			}
		})
	}
}

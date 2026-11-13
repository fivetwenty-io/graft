package graft_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// A Go caller can hand Evaluate two lists cut from one slice, which
// start at the same entry but end at different ones. Each loses its own
// (( prune )) entries and keeps its own length.
func TestEvaluateDropsPruneEntriesFromEachReslice(t *testing.T) {
	for _, parallel := range []bool{true, false} {
		t.Run(fmt.Sprintf("parallel=%v", parallel), func(t *testing.T) {
			s := []interface{}{"a", "(( prune ))", "b"}
			doc := graft.NewDocument(map[string]interface{}{
				"x": s[:3],
				"y": s[:2],
			})
			out, err := markerEngine(t, parallel).Evaluate(context.Background(), doc)
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			got := out.RawData().(map[string]interface{})
			want := map[string]interface{}{
				"x": []interface{}{"a", "b"},
				"y": []interface{}{"a"},
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %#v, want %#v", got, want)
			}
		})
	}
}

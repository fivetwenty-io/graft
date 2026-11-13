package graft_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// Evaluate refuses a document that holds itself with the cycle error a
// merge of it gives, in both modes, and never hands back an array
// marker as text.
func TestEvaluateRefusesASelfHoldingDocument(t *testing.T) {
	build := map[string]func() map[string]interface{}{
		"map holds itself": func() map[string]interface{} {
			m := map[string]interface{}{
				"l": []interface{}{"(( append ))", "a"},
			}
			m["self"] = m
			return m
		},
		"list holds itself": func() map[string]interface{} {
			l := []interface{}{"(( append ))", "a", nil}
			l[2] = l
			return map[string]interface{}{"l": l}
		},
		"no list work": func() map[string]interface{} {
			m := map[string]interface{}{"k": "v"}
			m["self"] = m
			return m
		},
	}
	ctx := context.Background()
	for name, mk := range build {
		for _, parallel := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/parallel=%v", name, parallel), func(t *testing.T) {
				e := markerEngine(t, parallel)
				_, mergeErr := e.Merge(ctx, graft.NewDocument(mk())).Execute()
				if mergeErr == nil {
					t.Fatal("Merge err = nil, want a cycle error")
				}
				_, err := e.Evaluate(ctx, graft.NewDocument(mk()))
				if err == nil {
					t.Fatal("Evaluate err = nil, want a cycle error")
				}
				if !strings.Contains(mergeErr.Error(), err.Error()) {
					t.Fatalf("Evaluate err = %q, want the cycle error of %q",
						err, mergeErr)
				}
			})
		}
	}
}

package graft

import (
	"testing"

	"github.com/fivetwenty-io/graft/internal/literal"
)

// TestLiteralCycleChecks makes sure both cycle checks look inside a
// literal. A wrapped value that refers to itself is still a cycle, and
// Strip on it would otherwise walk to its depth bound.
func TestLiteralCycleChecks(t *testing.T) {
	cyclic := map[string]interface{}{"a": 1}
	cyclic["self"] = cyclic
	cyclicList := []interface{}{nil}
	cyclicList[0] = cyclicList

	tests := []struct {
		name    string
		tree    map[string]interface{}
		wantErr bool
	}{
		{name: "plain literal", tree: map[string]interface{}{"p": literal.Wrap(map[string]interface{}{"a": []interface{}{1}})}},
		{name: "nested literal", tree: map[string]interface{}{"p": literal.Wrap(map[string]interface{}{"q": literal.Wrap("x")})}},
		{name: "cyclic map in a literal", tree: map[string]interface{}{"p": literal.Wrap(cyclic)}, wantErr: true},
		{name: "cyclic list in a literal", tree: map[string]interface{}{"p": literal.Wrap(cyclicList)}, wantErr: true},
		{name: "cyclic map deep in a literal", tree: map[string]interface{}{"p": literal.Wrap(map[string]interface{}{"q": literal.Wrap(cyclic)})}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := CheckForCycles(tt.tree, 4096); (err != nil) != tt.wantErr {
				t.Errorf("CheckForCycles error = %v, want error %v", err, tt.wantErr)
			}
			ev := &Evaluator{Tree: tt.tree}
			if err := ev.CheckForCycles(4096); (err != nil) != tt.wantErr {
				t.Errorf("Evaluator.CheckForCycles error = %v, want error %v", err, tt.wantErr)
			}
		})
	}
}

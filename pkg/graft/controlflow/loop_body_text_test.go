package controlflow

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// runLoopMergeYAML merges src on an engine built with opts and returns
// the merged document as YAML text, so a test can both compare a value
// and check that no text in the output names the loop binding tree.
func runLoopMergeYAML(t *testing.T, opts []graft.EngineOption, src string) (map[string]interface{}, string) {
	t.Helper()
	engine, err := graft.NewEngine(opts...)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	doc, err := engine.ParseYAML([]byte(src))
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}
	result, err := engine.Merge(context.Background(), doc).Execute()
	if err != nil {
		assertNoLoopNames(t, err)
		t.Fatalf("Execute: %v", err)
	}
	text, err := result.ToYAML()
	if err != nil {
		t.Fatalf("ToYAML: %v", err)
	}
	if strings.Contains(string(text), "__graft_loop") {
		t.Errorf("the output names the loop binding tree:\n%s", text)
	}
	data, ok := result.RawData().(map[string]interface{})
	if !ok {
		t.Fatalf("result is not a map: %#v", result.RawData())
	}
	return data, string(text)
}

// TestLoopVariableNamedLikeAnOperator names a loop variable after an
// operator and uses it both as the operator and as its argument. The
// operator name stays an operator call, and the argument reads the loop
// variable, as the same expression does outside a loop with a document
// key of that name. A name the parser reads as an operand, as in
// "type == ..." and "type || ...", still reads the loop variable.
func TestLoopVariableNamedLikeAnOperator(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"grab as the argument", `names: [a, b]
out:
(( for join in names ))
  - (( grab join ))
(( done ))
`, "[a b]"},
		{"concat with the argument", `names: [a, b]
out:
(( for join in names ))
  - (( concat "n=" join ))
(( done ))
`, "[n=a n=b]"},
		{"join of join", `names: [[a, b], [c, d]]
out:
(( for join in names ))
  - (( join ", " join ))
(( done ))
`, "[a, b c, d]"},
		{"join nested in concat", `names: [[a, b]]
out:
(( for join in names ))
  - (( concat "<" (join "," join) ">" ))
(( done ))
`, "[<a,b>]"},
		{"grab of grab", `names: [a, b]
out:
(( for grab in names ))
  - (( grab grab ))
(( done ))
`, "[a b]"},
		{"concat of concat", `names: [a, b]
out:
(( for concat in names ))
  - (( concat "x" concat ))
  - (( grab concat ))
(( done ))
`, "[xa a xb b]"},
		{"concat of keys twice", `names: [a, b]
out:
(( for keys in names ))
  - (( concat keys keys ))
(( done ))
`, "[aa bb]"},
		{"keys of keys", `names: [{k: 1}]
out:
(( for keys in names ))
  - (( keys keys ))
(( done ))
`, "[[k]]"},
		{"type in every position", `names: [a]
out:
(( for type in names ))
  - '(( type == "a" ? "yes" : "no" ))'
  - (( grab type ))
  - (( concat type "-x" ))
  - (( "z" || type ))
  - (( type || "z" ))
  - (( type "x" ))
  - (( "a" == type ))
(( done ))
`, "[yes a a-x z a string true]"},
	}
	for _, mode := range rootRefModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				data, _ := runLoopMergeYAML(t, mode.opts, tc.src)
				if got := fmt.Sprint(data["out"]); got != tc.want {
					t.Errorf("out = %s, want %s", got, tc.want)
				}
			})
		}
	}
}

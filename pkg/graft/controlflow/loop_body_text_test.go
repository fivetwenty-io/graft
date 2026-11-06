package controlflow

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// runLoopMergeYAML merges src on an engine built with opts and returns
// the merged document. It fails the test when the document, written out
// as YAML, names the loop binding tree anywhere.
func runLoopMergeYAML(t *testing.T, opts []graft.EngineOption, src string) map[string]interface{} {
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
	return data
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
				data := runLoopMergeYAML(t, mode.opts, tc.src)
				if got := fmt.Sprint(data["out"]); got != tc.want {
					t.Errorf("out = %s, want %s", got, tc.want)
				}
			})
		}
	}
}

// TestLoopBodyCalcReadsTheLoopVariable checks that a loop variable
// inside the quoted expression of a calc in a loop body reads the loop
// binding, and never a document key of the same name.
func TestLoopBodyCalcReadsTheLoopVariable(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"bare name", `out:
(( for i in range 1 3 ))
  - (( calc "i + 100" ))
(( done ))
`, "[101 102 103]"},
		{"bare name beside a document key", `i: 7
out:
(( for i in range 1 3 ))
  - (( calc "i + 100" ))
(( done ))
`, "[101 102 103]"},
		{"function argument", `n: 2
out:
(( for i in range 1 3 ))
  - (( calc "pow(2, i)" ))
(( done ))
`, "[2 4 8]"},
		{"subtraction with no spaces", `out:
(( for i in range 1 3 ))
  - (( calc "i-1" ))
(( done ))
`, "[0 1 2]"},
		{"single quotes", `out:
(( for i in range 1 3 ))
  - (( calc 'i * 2' ))
(( done ))
`, "[2 4 6]"},
		{"dotted path beside a document key", `svc: {n: 100}
items: [{n: 3}, {n: 4}]
out:
(( for svc in items ))
  - (( calc "svc.n * 2" ))
(( done ))
`, "[6 8]"},
		{"nested in a list", `out:
(( for i in range 1 2 ))
  - (( concat "v" (calc "i * 10") ))
(( done ))
`, "[v10 v20]"},
		{"two loop variables", `items: [{n: 3}]
out:
(( for k, svc in items ))
  - (( calc "svc.n + k" ))
(( done ))
`, "[3]"},
		{"outer and inner loop variables", `out:
(( for i in range 1 2 ))
(( for j in range 1 2 ))
  - (( calc "i * 10 + j" ))
(( done ))
(( done ))
`, "[11 12 21 22]"},
	}
	for _, mode := range rootRefModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				data := runLoopMergeYAML(t, mode.opts, tc.src)
				if got := fmt.Sprint(data["out"]); got != tc.want {
					t.Errorf("out = %s, want %s", got, tc.want)
				}
			})
		}
	}
}

// TestLoopHeaderCalcReadsTheLoopVariable pins that a loop variable in
// the quoted expression of a calc in a loop header, whether an if
// condition or a nested loop's subject, reads the loop binding and not
// a document key of the same name.
func TestLoopHeaderCalcReadsTheLoopVariable(t *testing.T) {
	cases := []struct {
		name string
		src  string
		key  string
		want string
	}{
		{"if condition beside a document key", `i: 7
hdr:
(( for i in range 1 3 ))
(( if calc "i > 1" ))
  - (( grab i ))
(( fi ))
(( done ))
`, "hdr", "[2 3]"},
		{"nested loop subject", `out:
(( for i in range 1 2 ))
(( for j in range 1 (calc "i + 1") ))
  - (( concat i "." j ))
(( done ))
(( done ))
`, "out", "[1.1 1.2 2.1 2.2 2.3]"},
	}
	for _, mode := range rootRefModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				data := runLoopMergeYAML(t, mode.opts, tc.src)
				if got := fmt.Sprint(data[tc.key]); got != tc.want {
					t.Errorf("%s = %s, want %s", tc.key, got, tc.want)
				}
			})
		}
	}
}

// TestLoopBodyDeferWritesTheWrittenText checks that defer in a loop
// body writes the expression as the user wrote it in the body, which is
// what the same defer writes outside a loop.
func TestLoopBodyDeferWritesTheWrittenText(t *testing.T) {
	src := `items: [{name: A, ip: 10.0.0.1}]
out:
(( for svc in items ))
  - (( defer svc.name ))
  - (( defer grab svc.ip ))
  - (( defer concat svc.name "-" (grab svc.ip) ))
  - x(( defer grab svc.ip ))
  - (( concat svc.name (defer grab svc.ip) ))
(( done ))
`
	want := "[(( svc.name )) (( grab svc.ip )) (( concat svc.name \"-\" grab svc.ip )) x(( defer grab svc.ip )) A(( grab svc.ip ))]"
	for _, mode := range rootRefModes {
		t.Run(mode.name, func(t *testing.T) {
			data := runLoopMergeYAML(t, mode.opts, src)
			if got := fmt.Sprint(data["out"]); got != want {
				t.Errorf("out = %s, want %s", got, want)
			}
		})
	}
}

// TestLoopBodyPassThroughTextReadsAsWritten writes loop body text that
// evaluation passes through as a string, such as a lone name in
// (( )), an operator embedded in other text, and an expression the
// parser cannot read. The output holds the text the user wrote, in
// both evaluator modes and inside nested loops.
func TestLoopBodyPassThroughTextReadsAsWritten(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"a lone loop variable", `list: [a, b]
out:
(( for svc in list ))
  - (( svc ))
(( done ))
`, "[(( svc )) (( svc ))]"},
		{"a lone operator-named variable", `list: [a]
out:
(( for join in list ))
  - (( join ))
(( done ))
`, "[(( join ))]"},
		{"an operator embedded in text", `list: [1, 2]
out:
(( for i in list ))
  - big-(( grab i ))
(( done ))
`, "[big-(( grab i )) big-(( grab i ))]"},
		{"an expression the parser cannot read", `list: [a]
out:
(( for svc in list ))
  - (( svc.n ? svc.n ))
(( done ))
`, "[(( svc.n ? svc.n ))]"},
		{"a map key", `list: [a]
out:
(( for svc in list ))
  - (( svc )): 1
(( done ))
`, "[map[(( svc )):1]]"},
		{"nested loops", `outer: [a]
inner: [1, 2]
out:
(( for svc in outer ))
(( for i in inner ))
  - (( svc ))
  - big-(( grab i ))
  - (( svc.n ? i ))
(( done ))
(( done ))
`, "[(( svc )) big-(( grab i )) (( svc.n ? i )) (( svc )) big-(( grab i )) (( svc.n ? i ))]"},
	}
	for _, mode := range rootRefModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				data := runLoopMergeYAML(t, mode.opts, tc.src)
				if got := fmt.Sprint(data["out"]); got != tc.want {
					t.Errorf("out = %s, want %s", got, tc.want)
				}
			})
		}
	}
}

package graft_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// exponentArgDoc holds keys spelled like numbers in exponent form, so an
// argument read as a number gives a value that differs from every key.
const exponentArgDoc = `
"1e5": k1e5
"1E5": k1E5
"1e+5": k1e+5
"1e-5": k1e-5
"-1e5": km1e5
"1":
  "5e3": k1.5e3
"-1":
  "5e3": km1.5e3
`

// TestExponentArgumentIsAReference pins that a number in exponent form
// that stands as a whole operator argument is a reference, the way spruce
// v1.35.17 reads it, wherever an argument or a side of an argument's ||
// can hold one. A fraction with an exponent splits at its dot, so 1.5e3
// is the key 5e3 inside 1.
func TestExponentArgumentIsAReference(t *testing.T) {
	paths := []struct {
		path string
		want string
	}{
		{"1e5", "k1e5"},
		{"1E5", "k1E5"},
		{"1e+5", "k1e+5"},
		{"1e-5", "k1e-5"},
		{"-1e5", "km1e5"},
		{"1.5e3", "k1.5e3"},
		{"-1.5e3", "km1.5e3"},
	}
	shapes := []struct {
		name   string
		expr   func(path string) string
		result func(want string) string
	}{
		{"grab", func(p string) string { return "(( grab " + p + " ))" }, func(w string) string { return w }},
		{"concat", func(p string) string { return `(( concat "v=" ` + p + ` "!" ))` }, func(w string) string { return "v=" + w + "!" }},
		{"concat last", func(p string) string { return `(( concat "v=" ` + p + " ))" }, func(w string) string { return "v=" + w }},
		{"left of ||", func(p string) string { return "(( grab " + p + ` || "fallback" ))` }, func(w string) string { return w }},
		{"right of ||", func(p string) string { return "(( grab missing.key || " + p + " ))" }, func(w string) string { return w }},
		{"concat right of ||", func(p string) string { return `(( concat "v=" missing.key || ` + p + ` "!" ))` }, func(w string) string { return "v=" + w + "!" }},
		{"inside a nested call", func(p string) string { return `(( concat "v=" (grab ` + p + `) ))` }, func(w string) string { return "v=" + w }},
	}
	for _, mode := range slashModes {
		for _, sh := range shapes {
			for _, p := range paths {
				// A '(' opens a call only when the operator name is not
				// followed by an infix operator, so "(grab -1e5)" is a
				// subtraction, as "(grab -5)" always has been.
				if sh.name == "inside a nested call" && strings.HasPrefix(p.path, "-") {
					continue
				}
				t.Run(mode.name+"/"+sh.name+"/"+p.path, func(t *testing.T) {
					d, err := evalSlashDoc(t, mode.opts, exponentArgDoc+"result: "+sh.expr(p.path)+"\n")
					if err != nil {
						t.Fatalf("unexpected error: %v", err)
					}
					got, err := d.Get("result")
					if err != nil {
						t.Fatalf("failed to read result: %v", err)
					}
					if fmt.Sprint(got) != sh.result(p.want) {
						t.Fatalf("result = %v (%T), want %q", got, got, sh.result(p.want))
					}
				})
			}
		}
	}
}

// TestExponentInExpressionStaysNumeric pins the shapes where a number in
// exponent form is an operand rather than a whole argument, so it stays
// a number. That covers the top level of an expression, a parenthesized
// group, a function-style argument list, and an argument that an infix
// operator follows, as in "grab 1e5 + 1", which is (grab 1e5) + 1.
func TestExponentInExpressionStaysNumeric(t *testing.T) {
	cases := []struct {
		expr string
		want string
	}{
		{"(( 1e5 * 2 ))", "200000"},
		{"(( 1e5 + 1 ))", "100001"},
		{`(( 1e5 || "d" ))`, "100000"},
		{"(( -1e5 * 2 ))", "-200000"},
		{"(( grab 1e5 + 1 ))", "100001"},
		{"(( grab 1e5 * 2 ))", "200000"},
		{"(( grab 1e+5 / 2 ))", "50000"},
		{`(( concat "a" (1e5 * 2) ))`, "a200000"},
		{`(( concat "a" (1e5) ))`, "a100000"},
		{"(( grab(1e5) ))", "100000"},
		{"(( grab missing.key || 1e5 * 2 ))", "200000"},
	}
	for _, mode := range slashModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.expr, func(t *testing.T) {
				d, err := evalSlashDoc(t, mode.opts, exponentArgDoc+"result: "+tc.expr+"\n")
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				got, err := d.Get("result")
				if err != nil {
					t.Fatalf("failed to read result: %v", err)
				}
				if fmt.Sprint(got) != tc.want {
					t.Fatalf("result = %v (%T), want %q", got, got, tc.want)
				}
			})
		}
	}
}

// TestExponentArgumentParse pins the parsed argument list, so the split
// between a reference argument and a negative-literal argument is seen
// without evaluating anything. A '-' before a number starts the next
// argument, spaced or not, as it does after any other argument, so the
// exponent before it is a whole argument too.
func TestExponentArgumentParse(t *testing.T) {
	cases := []struct {
		src  string
		want []string
	}{
		{`(( concat "a" 1e5 "b" ))`, []string{"a", "ref:1e5", "b"}},
		{`(( concat "a" -1e5 ))`, []string{"a", "ref:-1e5"}},
		{`(( concat "a" 1.5e3 ))`, []string{"a", "ref:1.5e3"}},
		{`(( concat "a" 1e5 -1 ))`, []string{"a", "ref:1e5", "-1"}},
		{`(( concat "a" 1e5, "b" ))`, []string{"a", "ref:1e5", "b"}},
		{`(( concat "a" 1e5 -1e5 ))`, []string{"a", "ref:1e5", "ref:-1e5"}},
		{`(( concat "a" 1e5 - 1 ))`, []string{"a", "ref:1e5", "-1"}},
		{`(( concat "a" 2e5-1 ))`, []string{"a", "ref:2e5", "-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.src, func(t *testing.T) {
			opcall, err := graft.ParseOpcallWithParser(graft.EvalPhase, tc.src)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			if opcall == nil {
				t.Fatalf("parsed as literal text, want an operator call")
			}
			var got []string
			for _, a := range opcall.Args() {
				if a.Type == graft.Reference {
					got = append(got, "ref:"+a.String())
				} else {
					got = append(got, a.String())
				}
			}
			if fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Fatalf("arguments = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestExponentArgumentAfterWideText pins that the reference is built from
// the tokens, so text wider than one byte before the argument leaves the
// key intact.
func TestExponentArgumentAfterWideText(t *testing.T) {
	for _, mode := range slashModes {
		t.Run(mode.name, func(t *testing.T) {
			d, err := evalSlashDoc(t, mode.opts, exponentArgDoc+`result: (( concat "é→" -1e5 ))`+"\n")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got, err := d.Get("result")
			if err != nil {
				t.Fatalf("failed to read result: %v", err)
			}
			if fmt.Sprint(got) != "é→km1e5" {
				t.Fatalf("result = %v, want %q", got, "é→km1e5")
			}
		})
	}
}

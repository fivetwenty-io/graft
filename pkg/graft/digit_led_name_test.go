package graft_test

import (
	"fmt"
	"testing"
)

// digitNameDoc holds keys that start with a digit and are not purely a
// number, beside the keys their pieces would name if graft split them,
// so a split read gives a different value or fails.
const digitNameDoc = `
"3em": k3em
"1e": k1e
"3abc": k3abc
"2e5x": k2e5x
"1e5a": k1e5a
"3e-x": k3e-x
"0x1f": k0x1f
"3":
  "5abc": k3.5abc
"1e5":
  x: k1e5.x
abc: plain-abc
x: plain-x
`

// TestDigitLedNameIsOneKey pins that a run starting with a digit that is
// not purely a number reads as one key, the way spruce v1.35.17 reads
// it, wherever an argument or a side of || can hold a reference.
func TestDigitLedNameIsOneKey(t *testing.T) {
	paths := []struct {
		path string
		want string
	}{
		{"3em", "k3em"},
		{"1e", "k1e"},
		{"3abc", "k3abc"},
		{"2e5x", "k2e5x"},
		{"1e5a", "k1e5a"},
		{"3e-x", "k3e-x"},
		{"0x1f", "k0x1f"},
		{"3.5abc", "k3.5abc"},
		{"1e5.x", "k1e5.x"},
	}
	shapes := []struct {
		name   string
		expr   func(path string) string
		result func(want string) string
	}{
		{"grab", func(p string) string { return "(( grab " + p + " ))" }, func(w string) string { return w }},
		{"concat", func(p string) string { return `(( concat "v=" ` + p + ` "!" ))` }, func(w string) string { return "v=" + w + "!" }},
		{"left of ||", func(p string) string { return "(( grab " + p + ` || "fallback" ))` }, func(w string) string { return w }},
		{"right of ||", func(p string) string { return "(( grab missing.key || " + p + " ))" }, func(w string) string { return w }},
	}
	for _, mode := range slashModes {
		for _, sh := range shapes {
			for _, p := range paths {
				t.Run(mode.name+"/"+sh.name+"/"+p.path, func(t *testing.T) {
					d, err := evalSlashDoc(t, mode.opts, digitNameDoc+"result: "+sh.expr(p.path)+"\n")
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

// TestDigitLedArithmeticStaysNumeric pins the shapes next to a digit-led
// name that are still arithmetic, because the run is a number followed
// by an operator and not a name.
func TestDigitLedArithmeticStaysNumeric(t *testing.T) {
	cases := []struct {
		expr string
		want string
	}{
		{"(( 3-1 ))", "2"},
		{"(( 2e5-1 ))", "199999"},
		{"(( 2e+3+1 ))", "2001"},
		{"(( 1.5 * 2 ))", "3"},
		{"(( 8 - 3 ))", "5"},
	}
	for _, mode := range slashModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.expr, func(t *testing.T) {
				d, err := evalSlashDoc(t, mode.opts, digitNameDoc+"result: "+tc.expr+"\n")
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

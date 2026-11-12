package yamlnode

import (
	"fmt"
	"reflect"
	"testing"
)

// TestDashScalarOnItsOwnLine checks that a plain scalar that starts with
// "-" and sits on a line of its own reads as it reads on the line of its
// key or its dash, as goccy and libyaml read it. Only a later line of the
// scalar that starts with "-" makes it a continuation that folds into a
// string.
func TestDashScalarOnItsOwnLine(t *testing.T) {
	cases := []struct{ own, inline string }{
		{"a:\n  -1\n", "a: -1\n"},
		{"a:\n  -1.5\n", "a: -1.5\n"},
		{"a:\n  -.inf\n", "a: -.inf\n"},
		{"a:\n  -0x1\n", "a: -0x1\n"},
		{"a:\n\n\n  -1 #c\n", "a: -1 #c\n"},
		{"a:\r\n  -1\r\n", "a: -1\r\n"},
		{"x:\n  -0.0: q\n", "x: {-0.0: q}\n"},
		{"x:\n  -1: q\n  -1.5: r\n", "x: {-1: q, -1.5: r}\n"},
		{"-\n  -1\n", "- -1\n"},
		{"a: [\n  -1]\n", "a: [-1]\n"},
		{"a: {\n  -1: \n  -2}\n", "a: {-1: -2}\n"},
		{"? \n  -1\n: v\n", "-1: v\n"},
		{"a: &x\n  -1\nb: *x\n", "a: &x -1\nb: *x\n"},
	}
	for _, c := range cases {
		t.Run(c.own, func(t *testing.T) {
			got, want := parsedScalars(t, c.own), parsedScalars(t, c.inline)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("ParseBytes scalars = %q, want %q", got, want)
			}
		})
	}
}

// TestDashContinuationAfterOwnLine checks that a scalar on a line of its
// own still folds into a string when a later line of it starts with "-".
func TestDashContinuationAfterOwnLine(t *testing.T) {
	cases := []struct{ doc, want string }{
		{"a:\n  -1\n  -2\n", "-1 -2"},
		{"a:\n  -x\n  - y #c\n", "-x - y"},
		{"a:\n  v\n  - x #c\n", "v - x"},
		{"a:\n\n  1\n  - 2\t\n", "1 - 2"},
	}
	for _, c := range cases {
		t.Run(c.doc, func(t *testing.T) {
			got := parsedScalars(t, c.doc)
			if want := fmt.Sprintf("str %q", c.want); len(got) != 2 || got[1] != want {
				t.Errorf("ParseBytes scalars = %q, want the value %s", got, want)
			}
		})
	}
}

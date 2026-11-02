package graft

import (
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
)

// TestDiffStringKeepsUserMarkupLiteral checks that String prints a key or a
// value holding color markup exactly as written, with color on or off, while
// graft's own markup still colors.
func TestDiffStringKeepsUserMarkupLiteral(t *testing.T) {
	for _, on := range []bool{true, false} {
		name := "color off"
		if on {
			name = "color on"
		}
		t.Run(name, func(t *testing.T) {
			prev := ansi.IsColorEnabled()
			ansi.Color(on)
			defer ansi.Color(prev)

			d, err := Diff(
				map[string]interface{}{"@G{k}": "before @R{x} text", "n": "a"},
				map[string]interface{}{"@G{k}": "after @C{y} 100%s", "n": "b"},
			)
			if err != nil {
				t.Fatal(err)
			}
			got := d.String("@B{root}")
			for _, want := range []string{"@B{root}.@G{k}", "before @R{x} text", "after @C{y} 100%s"} {
				if !strings.Contains(got, want) {
					t.Errorf("String output lost literal %q:\n%q", want, got)
				}
			}
			if hasEsc := strings.Contains(got, "\x1b["); hasEsc != on {
				t.Errorf("escapes present = %v, want %v:\n%q", hasEsc, on, got)
			}

			s := DiffScalar{Old: "o @R{x}", New: "n @G{y}"}.String("@C{key}")
			for _, want := range []string{"@C{key}", "o @R{x}", "n @G{y}"} {
				if !strings.Contains(s, want) {
					t.Errorf("scalar String lost literal %q:\n%q", want, s)
				}
			}
		})
	}
}

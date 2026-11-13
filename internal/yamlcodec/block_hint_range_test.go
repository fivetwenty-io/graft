package yamlcodec

import (
	"testing"

	"github.com/fivetwenty-io/graft/internal/yamlnode"
)

// A block scalar's indent hint is one digit, so a value that needs a
// hint is written double-quoted when the indent is outside 1 to 9, and
// always reads back as itself.
func TestBlockScalarHintStaysOneDigit(t *testing.T) {
	const s = "  x\n y\n"
	for _, best := range []int{-1, 10, 12} {
		got := renderValueScalarAs(s, literalScalarStyle, 0, 2, best, true)
		if got[0] != '"' {
			t.Errorf("indent %d: got %q, want double quotes", best, got)
			continue
		}
		docs, err := yamlnode.Parse([]byte("k: " + got + "\n"))
		if err != nil || docs[0].Content[0].Content[1].Value != s {
			t.Errorf("indent %d: %q does not read back as %q: %v", best, got, s, err)
		}
	}
	for _, best := range []int{0, 1, 2, 9} {
		got := renderValueScalarAs(s, literalScalarStyle, 0, 2, best, true)
		if got[0] != '|' {
			t.Errorf("indent %d: got %q, want a literal block", best, got)
		}
	}
}

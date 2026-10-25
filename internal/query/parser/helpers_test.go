package parser

import (
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/query/ast"
)

func mustParse(t *testing.T, src string, g Grammar) ast.Node {
	t.Helper()
	n, err := Parse(src, g)
	if err != nil {
		t.Fatalf("Parse(%q) error: %v", src, err)
	}
	return n
}

func wantEqual[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func wantErrContains(t *testing.T, err error, sub string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want one containing %q", sub)
	}
	if !strings.Contains(err.Error(), sub) {
		t.Fatalf("error = %q, want it to contain %q", err.Error(), sub)
	}
}

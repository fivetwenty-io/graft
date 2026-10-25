package lexer

import (
	"strings"
	"testing"
)

func mustLex(t *testing.T, src string, set TokenSet) []Token {
	t.Helper()
	toks, err := Tokenize(src, set)
	if err != nil {
		t.Fatalf("Tokenize(%q) error: %v", src, err)
	}
	return toks
}

func wantKinds(t *testing.T, toks []Token, want ...TokenKind) {
	t.Helper()
	got := make([]TokenKind, len(toks))
	for i, tk := range toks {
		got[i] = tk.Kind
	}
	if len(got) != len(want) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("kinds = %v, want %v", got, want)
		}
	}
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

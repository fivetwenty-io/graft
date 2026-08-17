package graft

import (
	"bytes"
	"testing"
)

func TestQuoteInjectKeysQuotesInjectForms(t *testing.T) {
	cases := []struct{ in, want string }{
		{"<<<: (( grab meta ))\n", "\"<<<\": (( grab meta ))\n"},
		{"  <<<: value\n", "  \"<<<\": value\n"},
		{"- <<<: value\n", "- \"<<<\": value\n"},
		{"host.web1.<<<: value\n", "\"host.web1.<<<\": value\n"},
		{"  - foo.<<<: value\n", "  - \"foo.<<<\": value\n"},
	}
	for _, c := range cases {
		got := QuoteInjectKeys([]byte(c.in))
		if string(got) != c.want {
			t.Errorf("QuoteInjectKeys(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestQuoteInjectKeysNoInjectKeyIsZeroCopy(t *testing.T) {
	in := []byte("name: thing\nmeta:\n  key: value\nlist:\n- a\n- b\n")
	out := QuoteInjectKeys(in)
	if !bytes.Equal(in, out) {
		t.Fatalf("QuoteInjectKeys altered inject-free input: %q", out)
	}
	if &out[0] != &in[0] {
		t.Errorf("QuoteInjectKeys reallocated inject-free input; want the original slice back")
	}
}

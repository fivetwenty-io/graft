package graft

import (
	"bytes"
	"testing"
)

func TestSanitizeBareSequenceTerminatorsZeroCopy(t *testing.T) {
	// Documents without a bare dash line - and documents whose bare
	// dashes need no rewrite - must come back as the original slice,
	// not a reallocated copy.
	unchanged := [][]byte{
		[]byte("name: thing\nlist:\n- a\n- b\nmeta:\n  key: value\n"),
		[]byte("jobs:\n- name: web\n  instances: 2\n"),
		// bare dash followed by a sibling sequence item: not the goccy
		// misparse shape, so no rewrite happens
		[]byte("list:\n- \n- a\n"),
	}
	for _, in := range unchanged {
		out := sanitizeBareSequenceTerminators(in)
		if !bytes.Equal(in, out) {
			t.Fatalf("sanitize altered %q -> %q", in, out)
		}
		if len(out) > 0 && &out[0] != &in[0] {
			t.Errorf("sanitize reallocated unchanged input %q; want the original slice back", in)
		}
	}
}

func TestSanitizeBareSequenceTerminatorsStillRewrites(t *testing.T) {
	in := []byte("list:\n- a\n-\nnext: value\n")
	want := "list:\n- a\n- ~\nnext: value\n"
	if got := string(sanitizeBareSequenceTerminators(in)); got != want {
		t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
	}
}

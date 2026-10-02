package yamlprep

import (
	"bytes"
	"reflect"
	"testing"
)

func TestBareDashRewritesZeroCopy(t *testing.T) {
	unchanged := [][]byte{
		[]byte("name: thing\nlist:\n- a\n- b\nmeta:\n  key: value\n"),
		[]byte("jobs:\n- name: web\n  instances: 2\n"),
		[]byte("list:\n- \n- a\n"),
	}
	for _, in := range unchanged {
		out, lines := BareDashRewrites(in)
		if !bytes.Equal(in, out) || lines != nil {
			t.Fatalf("BareDashRewrites altered %q -> %q (lines %v)", in, out, lines)
		}
		if len(out) > 0 && &out[0] != &in[0] {
			t.Errorf("BareDashRewrites reallocated unchanged input %q; want the original slice back", in)
		}
	}
}

func TestBareDashRewritesStillRewrites(t *testing.T) {
	in := []byte("list:\n- a\n-\nnext: value\n")
	want := "list:\n- a\n- ~\nnext: value\n"
	if got, _ := BareDashRewrites(in); string(got) != want {
		t.Errorf("BareDashRewrites(%q) = %q, want %q", in, got, want)
	}
}

func TestBareDashRewritesReportsLines(t *testing.T) {
	in := []byte("list:\n- a\n-\nnext: value\nother:\n  - x\n  -\n  key: v\n")
	out, lines := BareDashRewrites(in)
	want := "list:\n- a\n- ~\nnext: value\nother:\n  - x\n  - ~\n  key: v\n"
	if string(out) != want {
		t.Fatalf("BareDashRewrites(%q) = %q, want %q", in, out, want)
	}
	if !reflect.DeepEqual(lines, []int{3, 7}) {
		t.Fatalf("BareDashRewrites lines = %v, want [3 7]", lines)
	}
}

func TestBareDashRewritesUnchangedReportsNoLines(t *testing.T) {
	in := []byte("list:\n- a\n- b\n")
	out, lines := BareDashRewrites(in)
	if &out[0] != &in[0] || lines != nil {
		t.Fatalf("BareDashRewrites(%q) = (%q, %v), want the original slice and nil lines", in, out, lines)
	}
}

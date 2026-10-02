package yamlprep

import (
	"reflect"
	"testing"
)

func TestPrepareAppliesRewritesInParseYAMLOrder(t *testing.T) {
	in := "<<<: (( inject a ))\nb: {{x}}\nl:\n- a\n-\nc: 1\n"
	out, lines := Prepare([]byte(in))
	want := "\"<<<\": (( inject a ))\nb: '{{x}}'\nl:\n- a\n- ~\nc: 1\n"
	if string(out) != want {
		t.Fatalf("Prepare(%q) = %q, want %q", in, out, want)
	}
	if !reflect.DeepEqual(lines, []int{5}) {
		t.Fatalf("Prepare lines = %v, want [5]", lines)
	}
}

func TestPrepareIsZeroCopyForPlainInput(t *testing.T) {
	in := []byte("a: 1\n")
	out, lines := Prepare(in)
	if &out[0] != &in[0] || lines != nil {
		t.Fatal("plain input must come back as the original slice with nil lines")
	}
}

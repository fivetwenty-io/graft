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

// TestPrepareSharesInputWhenNothingIsRewritten feeds Prepare input that
// reaches each rewrite's slow path without anything to rewrite: a bare
// dash that a sibling item follows, a "<<<" that is no key, and a
// "{{x}}" placeholder used as a key. Prepare must hand back the input
// slice itself, so Parse makes no copy.
func TestPrepareSharesInputWhenNothingIsRewritten(t *testing.T) {
	for _, in := range []string{
		"l:\n- a\n-\n- b\n",
		"a: x<<<y\n",
		"{{x}}: 1\n",
		"l:\n- a\n-\n- b\na: x<<<y\n{{x}}: 1\n",
	} {
		data := []byte(in)
		out, lines := Prepare(data)
		if len(out) != len(data) || &out[0] != &data[0] || lines != nil {
			t.Errorf("Prepare(%q) must come back as the original slice with nil lines", in)
		}
	}
}

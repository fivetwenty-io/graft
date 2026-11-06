package yamlcodec_test

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/yamlcodec"
)

// TestDecodeEnvValueFollowsSpruce pins the value spruce 1.35.17 gives
// for each text, checked against its own YAML library. A text that
// decodes to a string, or that does not decode, comes back unchanged.
func TestDecodeEnvValueFollowsSpruce(t *testing.T) {
	cases := []struct {
		text string
		want any
	}{
		{"yes", true},
		{"Off", false},
		{"TRUE", true},
		{"yes ", true},
		{"  yes", true},
		{"~", nil},
		{"Null", nil},
		{"# c", nil},
		{"   ", nil},
		{"8080", 8080},
		{"01234", 668},
		{"0x10", 16},
		{"0b11", 3},
		{"1_000", 1000},
		{"-1", -1},
		{"3.14", 3.14},
		{".5", 0.5},
		{".inf", math.Inf(1)},
		{"a: b", map[string]any{"a": "b"}},
		{"x: yes", map[string]any{"x": true}},
		{"[yes, no]", []any{true, false}},
		{"- a", []any{"a"}},
		{"- ", []any{nil}},
		{"? a", map[string]any{"a": nil}},
		{"!!int 5", 5},
		{"!!bool yes", true},
		{"!!map {a: 1}", map[string]any{"a": 1}},
		{"a: &x [1, 2]\nb: *x", map[string]any{"a": []any{1, 2}, "b": []any{1, 2}}},
		{"{a: [1, {b: yes}]}", map[string]any{"a": []any{1, map[string]any{"b": true}}}},
		{"--- 5", 5},
		{"%YAML 1.1\n--- 3", 3},

		// Only the first document counts, as in spruce, even when a
		// later one does not parse.
		{"1\n---\n2", 1},
		{"1\n---\n[", 1},
		{"a: 1\n---\nb: 2", map[string]any{"a": 1}},
		{"1\n...\n", 1},

		// These decode to strings, so the text stays as it was.
		{"hello", "hello"},
		{"hello world", "hello world"},
		{`"q"`, `"q"`},
		{"'yes'", "'yes'"},
		{"!!str yes", "!!str yes"},
		{"a #c", "a #c"},
		{"Yes!", "Yes!"},
		{"2001-01-01", "2001-01-01"},
		{"y", "y"},
		{"N", "N"},

		// These do not decode in spruce, so the text stays as it was.
		{"[", "["},
		{"[a", "[a"},
		{"{", "{"},
		{`"unterminated`, `"unterminated`},
		{"key: [", "key: ["},
		{"!x !y", "!x !y"},
		{"*a", "*a"},
		{"1\n...\nfoo", "1\n...\nfoo"},
		{"\t", "\t"},
		{"\tyes", "\tyes"},
		{" \tyes", " \tyes"},
		{"\n\tyes", "\n\tyes"},
		{"\t- a", "\t- a"},
	}
	for _, tc := range cases {
		got := yamlcodec.DecodeEnvValue(tc.text)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("DecodeEnvValue(%q) = %#v (%T), want %#v (%T)", tc.text, got, got, tc.want, tc.want)
		}
	}
}

// TestDecodeEnvValueNaN checks .nan apart, since NaN never equals
// itself.
func TestDecodeEnvValueNaN(t *testing.T) {
	for _, text := range []string{".nan", ".NaN"} {
		got, ok := yamlcodec.DecodeEnvValue(text).(float64)
		if !ok || !math.IsNaN(got) {
			t.Errorf("DecodeEnvValue(%q) = %#v, want NaN", text, got)
		}
	}
}

// billionLaughs is a value whose aliases expand to a billion nodes.
func billionLaughs() string {
	var b strings.Builder
	b.WriteString("a: &a [x, x, x, x, x, x, x, x, x, x]\n")
	prev := "a"
	for _, name := range []string{"b", "c", "d", "e", "f", "g", "h", "i"} {
		b.WriteString(name + ": &" + name + " [")
		for j := 0; j < 10; j++ {
			if j > 0 {
				b.WriteString(", ")
			}
			b.WriteString("*" + prev)
		}
		b.WriteString("]\n")
		prev = name
	}
	return b.String()
}

// TestDecodeEnvValueRefusesAliasBlowup checks that a value whose
// aliases would expand past the merge's node cap stays the text it was,
// rather than expanding.
func TestDecodeEnvValueRefusesAliasBlowup(t *testing.T) {
	text := billionLaughs()
	if got := yamlcodec.DecodeEnvValue(text); got != text {
		t.Fatalf("DecodeEnvValue expanded an alias-heavy value to %T", got)
	}
}

// TestDecodeExternalErrorsQuoteNoValue checks that a value which fails
// to decode gives an error that names no part of it, since the value
// may be a secret.
func TestDecodeExternalErrorsQuoteNoValue(t *testing.T) {
	const secret = "hunter2-s3cr3t"
	for _, text := range []string{
		"[" + secret,
		"{a: " + secret,
		secret + ":\n  - [",
		"\"" + secret,
		"x: " + secret + "\nx: 2",
		"!x !y " + secret,
		"*" + secret,
		"\t" + secret,
		strings.Repeat("[", 20000) + secret,
		billionLaughs() + "z: " + secret,
	} {
		v, err := yamlcodec.DecodeExternal([]byte(text))
		if err == nil {
			t.Errorf("DecodeExternal accepted a bad value and gave %T", v)
			continue
		}
		if !errors.Is(err, yamlcodec.ErrExternalValue) {
			t.Errorf("DecodeExternal error is not ErrExternalValue: %T", err)
		}
		if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "hunter") {
			t.Errorf("DecodeExternal error quotes the value: %q", err.Error())
		}
		if errors.Unwrap(err) != nil {
			t.Errorf("DecodeExternal error wraps a cause that may quote the value")
		}
	}
}

// TestDecodeExternalKeepsStrings checks that DecodeExternal itself
// returns a decoded string, which DecodeEnvValue then discards.
func TestDecodeExternalKeepsStrings(t *testing.T) {
	v, err := yamlcodec.DecodeExternal([]byte(`"yes"`))
	if err != nil {
		t.Fatalf("DecodeExternal: %v", err)
	}
	if v != "yes" {
		t.Fatalf("DecodeExternal(\"yes\") = %#v, want the string yes", v)
	}
}

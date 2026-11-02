package graft

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

// scalarClass names the kind of Go value a merge decoded, so a table can
// say what spruce printed without depending on which Go integer type
// carries a number.
func scalarClass(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return "str:" + x
	case bool:
		return fmt.Sprintf("bool:%t", x)
	case int, int64, uint64:
		return fmt.Sprintf("int:%d", x)
	case float64:
		switch {
		case math.IsNaN(x):
			return "float:.nan"
		case math.IsInf(x, 1):
			return "float:.inf"
		case math.IsInf(x, -1):
			return "float:-.inf"
		}
		return fmt.Sprintf("float:%v", x)
	}
	return fmt.Sprintf("%T:%v", v, v)
}

// TestMergeExplicitTags pins what graft merge reads for a scalar with
// an explicit core tag. Every expectation is the value real spruce
// v1.35.17 printed for the same document.
func TestMergeExplicitTags(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// !!str keeps the text as written.
		{"a: !!str yes", "str:yes"},
		{"a: !!str No", "str:No"},
		{"a: !!str ON", "str:ON"},
		{"a: !!str off", "str:off"},
		{"a: !!str ~", "str:~"},
		{"a: !!str null", "str:null"},
		{"a: !!str 1.50", "str:1.50"},
		{"a: !!str 012", "str:012"},
		{"a: !!str 0x1F", "str:0x1F"},
		{"a: !!str \"\"", "str:"},
		{"a: !!str |-\n  yes", "str:yes"},
		{"a: !<tag:yaml.org,2002:str> yes", "str:yes"},
		// !!bool takes the YAML 1.1 words, quoted or plain.
		{"a: !!bool ON", "bool:true"},
		{"a: !!bool off", "bool:false"},
		{"a: !!bool y", "bool:true"},
		{"a: !!bool n", "bool:false"},
		{"a: !!bool Y", "bool:true"},
		{"a: !!bool N", "bool:false"},
		{"a: !!bool \"yes\"", "bool:true"},
		{"a: !!bool 'off'", "bool:false"},
		{"a: !!bool \"Yes\"", "bool:true"},
		{"a: !!bool true", "bool:true"},
		// !!int takes yaml.v2's integer forms.
		{"a: !!int 12", "int:12"},
		{"a: !!int 0x1f", "int:31"},
		{"a: !!int \"0x1f\"", "int:31"},
		{"a: !!int '12'", "int:12"},
		{"a: !!int 1_000", "int:1000"},
		{"a: !!int 0o17", "int:15"},
		{"a: !!int 0b101", "int:5"},
		{"a: !!int 0B101", "int:5"},
		{"a: !!int -0b101", "int:-5"},
		{"a: !!int +0b101", "int:5"},
		{"a: !!int 012", "int:10"},
		{"a: !!int -5", "int:-5"},
		{"a: !!int +5", "int:5"},
		{"a: !!int 0x_1F", "int:31"},
		{"a: !!int -0x1f", "int:-31"},
		{"a: !!int 1__0", "int:10"},
		{"a: !!int 9223372036854775807", "int:9223372036854775807"},
		{"a: !!int 18446744073709551615", "int:18446744073709551615"},
		{"a: !!int |-\n  12", "int:12"},
		// !!float takes yaml.v2's float forms.
		{"a: !!float 1.5", "float:1.5"},
		{"a: !!float 1.50", "float:1.5"},
		{"a: !!float 1e3", "float:1000"},
		{"a: !!float \"2.5\"", "float:2.5"},
		{"a: !!float .inf", "float:.inf"},
		{"a: !!float -.inf", "float:-.inf"},
		{"a: !!float .nan", "float:.nan"},
		{"a: !!float +inf", "float:.inf"},
		{"a: !!float 1_000.5", "float:1000.5"},
		{"a: !!float 09", "float:9"},
		{"a: !!float .5", "float:0.5"},
		{"a: !!float -1.5e-3", "float:-0.0015"},
		{"a: !!float 0x1p3", "float:8"},
		// !!null takes the null words and empty text.
		{"a: !!null ~", "null"},
		{"a: !!null null", "null"},
		{"a: !!null NULL", "null"},
		{"a: !!null \"\"", "null"},
		{"a: !!null \"~\"", "null"},
		// !!binary decodes base64 into the bytes' text.
		{"a: !!binary aGk=", "str:hi"},
		{"a: !!binary \"aGk=\"", "str:hi"},
		{"a: !!binary |\n  aGVsbG8g\n  d29ybGQ=", "str:hello world"},
		{"a: !!binary \"\"", "str:"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			doc, err := NewDefaultEngine().ParseYAML([]byte(tc.in))
			if err != nil {
				t.Fatalf("ParseYAML: %v", err)
			}
			if got := scalarClass(mustGet(t, doc, "a")); got != tc.want {
				t.Errorf("a = %s, want %s", got, tc.want)
			}
		})
	}
}

// TestMergeExplicitTagsNested covers tags inside flow and block
// collections and under anchors, which spruce resolves the same way.
func TestMergeExplicitTagsNested(t *testing.T) {
	cases := []struct {
		in   string
		path string
		want string
	}{
		{"a: {b: !!str yes}", "a.b", "str:yes"},
		{"a: [!!str yes, !!bool y]", "a.[0]", "str:yes"},
		{"a: [!!str yes, !!bool y]", "a.[1]", "bool:true"},
		{"a:\n- !!str yes\n- !!int 0x10", "a.[0]", "str:yes"},
		{"a:\n- !!str yes\n- !!int 0x10", "a.[1]", "int:16"},
		{"a: !!str &x yes\nb: *x", "a", "str:yes"},
		{"a: !!str &x yes\nb: *x", "b", "str:yes"},
		{"a: &x !!str yes\nb: *x", "a", "str:yes"},
		{"a: &x !!str yes\nb: *x", "b", "str:yes"},
	}
	for _, tc := range cases {
		t.Run(tc.in+"/"+tc.path, func(t *testing.T) {
			doc, err := NewDefaultEngine().ParseYAML([]byte(tc.in))
			if err != nil {
				t.Fatalf("ParseYAML: %v", err)
			}
			if got := scalarClass(mustGet(t, doc, tc.path)); got != tc.want {
				t.Errorf("%s = %s, want %s", tc.path, got, tc.want)
			}
		})
	}
}

// TestMergeExplicitTagKeys covers tagged mapping keys. spruce prints the
// resolved key, so a !!bool key reads true and a !!int key reads 1.
func TestMergeExplicitTagKeys(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"!!bool yes: a", "true"},
		{"!!int \"1\": a", "1"},
		{"!!str 1: a", "1"},
		{"!!null ~: a", "null"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			doc, err := NewDefaultEngine().ParseYAML([]byte(tc.in))
			if err != nil {
				t.Fatalf("ParseYAML: %v", err)
			}
			if got := scalarClass(mustGet(t, doc, tc.want)); got != "str:a" {
				t.Errorf("%s = %s, want str:a", tc.want, got)
			}
		})
	}
}

// TestMergeExplicitTagErrors pins the documents spruce refuses because a
// tag cannot hold its text. graft refuses them too, and its message names
// the tag and the position but never the text.
func TestMergeExplicitTagErrors(t *testing.T) {
	cases := []struct {
		in, value, tag string
	}{
		{"a: !!bool tRue", "tRue", "!!bool"},
		{"a: !!bool YeS", "YeS", "!!bool"},
		{"a: !!bool 1", "1", "!!bool"},
		{"a: !!bool 0", "0", "!!bool"},
		{"a: !!bool ~", "~", "!!bool"},
		{"a: !!bool \"\"", "", "!!bool"},
		{"a: !!bool word", "word", "!!bool"},
		{"a: !!int yes", "yes", "!!int"},
		{"a: !!int true", "true", "!!int"},
		{"a: !!int word", "word", "!!int"},
		{"a: !!int 1.5", "1.5", "!!int"},
		{"a: !!int \"\"", "", "!!int"},
		{"a: !!int ~", "~", "!!int"},
		{"a: !!int 09", "09", "!!int"},
		{"a: !!int 99999999999999999999", "99999999999999999999", "!!int"},
		{"a: !!int _1", "_1", "!!int"},
		{"a: !!int \" 12\"", " 12", "!!int"},
		{"a: !!int 0x", "0x", "!!int"},
		{"a: !!int >-\n  word", "word", "!!int"},
		{"a: !<tag:yaml.org,2002:int> word", "word", "!!int"},
		{"a: !!float word", "word", "!!float"},
		{"a: !!float yes", "yes", "!!float"},
		{"a: !!float 1", "", "!!float"},
		{"a: !!float ~", "~", "!!float"},
		{"a: !!float .", ".", "!!float"},
		{"a: !!null yes", "yes", "!!null"},
		{"a: !!null x", "x", "!!null"},
		{"a: !!null 0", "", "!!null"},
		{"a: !!binary aGk", "aGk", "!!binary"},
		{"!!int word: a", "word", "!!int"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			_, err := NewDefaultEngine().ParseYAML([]byte(tc.in))
			if err == nil {
				t.Fatal("ParseYAML succeeded, want an error")
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.tag) {
				t.Errorf("error %q does not name %s", msg, tc.tag)
			}
			if !strings.Contains(msg, "line 1, column ") {
				t.Errorf("error %q does not name the position", msg)
			}
			if len(tc.value) > 1 && strings.Contains(msg, tc.value) {
				t.Errorf("error %q quotes the value", msg)
			}
		})
	}
}

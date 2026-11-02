package graft

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

// classify reduces a DetectArrayRoot result to the one distinction its
// callers consume (IsArrayError), so the fast-path probe can be checked
// against the full-parse classification.
func classify(err error) string {
	if IsArrayError(err) {
		return "array"
	}
	return "not-array"
}

func TestRootCannotBeArray(t *testing.T) {
	// Inputs where the byte probe must prove "not an array" without a
	// full parse.
	definite := []string{
		"foo: bar\n",
		"foo:\n  - a\n  - b\n",
		"# comment\nfoo: bar\n",
		"\n\n# comment\n\nfoo: bar\n",
		"---\nfoo: bar\n",
		"---   \nfoo: bar\n",
		"# leading\n---\nfoo: bar\n",
		"\"quoted\": value\n",
		"'single': value\n",
		"{a: 1, b: 2}\n",
		"plain scalar\n",
		"123: numeric key\n",
		"_private: x\n",
		"   indented: map\n",
		"--- {a: 1}\n",
	}
	for _, in := range definite {
		if !rootCannotBeArray([]byte(in)) {
			t.Errorf("rootCannotBeArray(%q) = false, want true", in)
		}
	}

	// Inputs the probe must NOT rule out (arrays, or shapes only a real
	// parse can classify).
	ambiguous := []string{
		"- a\n- b\n",
		"  - indented\n",
		"[1, 2, 3]\n",
		"---\n- a\n",
		"--- [1, 2]\n",
		"-5\n",
		"-foo: dash key\n",
		"&anchor\n- a\n",
		"!!seq [a]\n",
		"%YAML 1.1\n---\nfoo: bar\n",
		"*alias\n",
		"? complex\n: key\n",
		"|\n  block\n",
		"...\n",
		"",
		"   \n",
	}
	for _, in := range ambiguous {
		if rootCannotBeArray([]byte(in)) {
			t.Errorf("rootCannotBeArray(%q) = true, want false", in)
		}
	}
}

func TestDetectArrayRootFastPathEquivalence(t *testing.T) {
	// Every probe input must classify identically to the full parse the
	// fast path short-circuits.
	corpus := []string{
		"foo: bar\n",
		"foo:\n  - a\n  - b\n",
		"# comment\nfoo: bar\n",
		"---\nfoo: bar\n",
		"\"quoted\": value\n",
		"'single': value\n",
		"{a: 1, b: 2}\n",
		"plain scalar\n",
		"   indented: map\n",
		"--- {a: 1}\n",
		"- a\n- b\n",
		"  - indented\n",
		"[1, 2, 3]\n",
		"---\n- a\n",
		"--- [1, 2]\n",
		"-5\n",
		"-foo: dash key\n",
		"!!seq [a]\n",
		"",
		"   \n",
		"null\n",
		"--- |\n  doc\n",
	}
	for _, in := range corpus {
		fast := classify(DetectArrayRoot([]byte(in)))
		slow := classify(detectArrayRootFull([]byte(in)))
		if fast != slow {
			t.Errorf("DetectArrayRoot(%q) classified %q, full parse says %q", in, fast, slow)
		}
	}
}

// TestGoPatchResolvesExplicitTags parses a go-patch ops file whose
// values carry explicit tags. The values read the way a merge input
// reads them, and every expectation is what real spruce v1.35.17 merged.
func TestGoPatchResolvesExplicitTags(t *testing.T) {
	ops := "- type: replace\n  path: /a\n  value: !!int 0x1f\n" +
		"- type: replace\n  path: /b?\n  value: !!binary aGk=\n" +
		"- type: replace\n  path: /c?\n  value: !!timestamp word\n" +
		"- type: replace\n  path: /d?\n  value: !foo on\n" +
		"- type: replace\n  path: /e?\n  value: !!str 1.50\n" +
		"- type: replace\n  path: /f?\n  value: !!bool on\n"
	if !IsArrayError(DetectArrayRoot([]byte(ops))) {
		t.Fatalf("DetectArrayRoot = %v, want the array root a go-patch file has", DetectArrayRoot([]byte(ops)))
	}
	parsed, err := ParseGoPatch([]byte(ops))
	if err != nil {
		t.Fatalf("ParseGoPatch: %v", err)
	}
	got, err := parsed.Apply(map[any]any{"a": 1})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := map[any]any{"a": 31, "b": "hi", "c": "word", "d": "on", "e": "1.50", "f": true}
	m, ok := got.(map[any]any)
	if !ok {
		t.Fatalf("Apply gave %T", got)
	}
	// The integer's Go type is goccy's choice, so only its value counts.
	if a, ok := m["a"].(uint64); ok && a <= math.MaxInt32 {
		m["a"] = int(a)
	}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("patched = %#v, want %#v", m, want)
	}
}

// TestGoPatchRefusesTagsThatCannotHoldTheirText parses an ops file that
// spruce refuses. The error names the tag but never quotes the text.
func TestGoPatchRefusesTagsThatCannotHoldTheirText(t *testing.T) {
	_, err := ParseGoPatch([]byte("- type: replace\n  path: /a\n  value: !!int S3KRIT\n"))
	if err == nil {
		t.Fatal("ParseGoPatch succeeded, want an error")
	}
	if msg := err.Error(); !strings.Contains(msg, "!!int") || strings.Contains(msg, "S3KRIT") {
		t.Errorf("ParseGoPatch error %q, want one naming !!int without the text", msg)
	}
}

// TestGoPatchReadsBoolWordsAsMergeDoes parses go-patch values that hold
// untagged YAML 1.1 boolean words. They read the way a merge input reads
// them, so a plain word is a boolean while a quoted word, a !!str word,
// and a block scalar stay strings. Every expectation is what real
// spruce v1.35.17 merged, apart from y, which spruce reads as true and
// graft merge keeps as a word.
func TestGoPatchReadsBoolWordsAsMergeDoes(t *testing.T) {
	ops := "- type: replace\n  path: /a\n  value: yes\n" +
		"- type: replace\n  path: /b?\n  value: \"off\"\n" +
		"- type: replace\n  path: /c?\n  value: [On, y, \"no\"]\n" +
		"- type: replace\n  path: /d?\n  value: {e: NO, f: !!str yes}\n" +
		"- type: replace\n  path: /g?\n  value: |-\n    on\n" +
		"- type: replace\n  path: /h?\n  value: Off\n"
	parsed, err := ParseGoPatch([]byte(ops))
	if err != nil {
		t.Fatalf("ParseGoPatch: %v", err)
	}
	got, err := parsed.Apply(map[any]any{"a": 1})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := map[any]any{
		"a": true, "b": "off", "c": []any{true, "y", "no"},
		"d": map[any]any{"e": false, "f": "yes"}, "g": "on", "h": false,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("patched = %#v, want %#v", got, want)
	}
}

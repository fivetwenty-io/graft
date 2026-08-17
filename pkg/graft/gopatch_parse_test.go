package graft

import (
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

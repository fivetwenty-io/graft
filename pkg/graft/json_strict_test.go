package graft

import (
	"strings"
	"testing"
)

const strictKeyError = "non-string keys found during strict JSON conversion"

// TestJSONStrictRefusesNonStringKeys pins which keys spruce's
// `json --strict` refuses. Each rejected case is one spruce exits 2 on
// with the strict-conversion message, and each accepted case is one it
// converts, with the same JSON it prints without --strict.
func TestJSONStrictRefusesNonStringKeys(t *testing.T) {
	rejected := map[string]string{
		"integer":             "1: a\n",
		"negative integer":    "-5: a\n",
		"hex integer":         "0x10: a\n",
		"float":               "1.5: a\n",
		"exponent":            "1e3: a\n",
		"infinity":            ".inf: a\n",
		"true":                "true: b\n",
		"capitalized True":    "True: b\n",
		"false":               "false: b\n",
		"yes":                 "yes: b\n",
		"no":                  "no: b\n",
		"on":                  "on: b\n",
		"off":                 "off: b\n",
		"tilde null":          "~: a\n",
		"null":                "null: a\n",
		"explicit int tag":    "!!int \"1\": a\n",
		"nested map":          "a:\n  2: x\n",
		"in a list of maps":   "a:\n- b: 1\n- 2: x\n",
		"flow map":            "a: [{1: x}]\n",
		"flow map at root":    "{1: a}\n",
		"alias to an integer": "x: &k 1\ny: {*k : b}\n",
		"merged from a map":   "base: &b {1: x}\nm:\n  <<: *b\n  z: 1\n",
	}
	for name, in := range rejected {
		_, err := JSONifyIO(strings.NewReader(in), true)
		if err == nil || !strings.Contains(err.Error(), strictKeyError) {
			t.Errorf("%s: JSONifyIO(%q, strict) = %v, want %q", name, in, err, strictKeyError)
		}
		if _, err := JSONifyIO(strings.NewReader(in), false); err != nil {
			t.Errorf("%s: JSONifyIO(%q) without strict failed: %v", name, in, err)
		}
	}

	accepted := map[string]string{
		"plain strings":        "a: 1\nb:\n  c: [1, 2]\n",
		"quoted integer":       "\"1\": a\n'2': b\n",
		"quoted bool word":     "\"yes\": a\n'true': b\n",
		"explicit str tag":     "!!str 1: a\n",
		"a date":               "2001-01-01: a\n",
		"merged string keys":   "base: &b {k: x}\nm:\n  <<: *b\n  z: 1\n",
		"non-string value":     "a: 1\nb: true\nc: ~\nd: 1.5\n",
		"alias to a string":    "x: &k s\ny: {*k : b}\n",
		"integer-like strings": "a1: x\n1a: y\n",
	}
	for name, in := range accepted {
		strict, err := JSONifyIO(strings.NewReader(in), true)
		if err != nil {
			t.Errorf("%s: JSONifyIO(%q, strict) failed: %v", name, in, err)
			continue
		}
		loose, err := JSONifyIO(strings.NewReader(in), false)
		if err != nil || strict != loose {
			t.Errorf("%s: strict = %q, loose = %q (%v), want the same JSON", name, strict, loose, err)
		}
	}
}

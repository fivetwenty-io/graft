package main

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// parseTreeForTest parses one YAML document exactly as the merge path
// does and returns its underlying tree.
func parseTreeForTest(t *testing.T, doc string) map[string]interface{} {
	t.Helper()
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	d, err := engine.ParseYAML([]byte(doc))
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}
	if d == nil {
		return map[string]interface{}{}
	}
	m, ok := d.RawData().(map[string]interface{})
	if !ok {
		t.Fatalf("RawData is %T, not map[string]interface{}", d.RawData())
	}
	return m
}

// TestParseCacheTreeRoundTripFidelity is the integrity rule for Layer 2:
// a tree that survives encode/decode must be indistinguishable from the
// original - same shapes, same scalar types (int vs uint64 vs float64),
// and the same marshaled YAML bytes.
func TestParseCacheTreeRoundTripFidelity(t *testing.T) {
	doc := "int: 42\n" +
		"neg: -7\n" +
		"big: 9223372036854775807\n" +
		"huge: 18446744073709551615\n" +
		"float: 3.14\n" +
		"exp: 1e3\n" +
		"bool_true: true\n" +
		"quoted_bool: \"yes\"\n" +
		"str: hello\n" +
		"empty_str: \"\"\n" +
		"nul: ~\n" +
		"op: (( grab meta.x ))\n" +
		"date: 2026-08-17\n" +
		"nested:\n" +
		"  list:\n" +
		"  - 1\n" +
		"  - two\n" +
		"  - inner:\n" +
		"    - a\n" +
		"    - b\n" +
		"  map:\n" +
		"    deep:\n" +
		"      key: value\n" +
		"multiline: |\n" +
		"  line1\n" +
		"  line2\n"

	orig := parseTreeForTest(t, doc)

	encoded, err := encodeCachedTree(orig)
	if err != nil {
		t.Fatalf("encodeCachedTree: %v", err)
	}
	decoded, ok := decodeCachedTree(encoded)
	if !ok {
		t.Fatal("decodeCachedTree failed on freshly encoded data")
	}

	if !reflect.DeepEqual(orig, decoded) {
		t.Fatalf("round trip changed the tree:\norig:    %#v\ndecoded: %#v", orig, decoded)
	}

	origYAML, err := graft.MarshalYAML(orig)
	if err != nil {
		t.Fatalf("MarshalYAML(orig): %v", err)
	}
	decodedYAML, err := graft.MarshalYAML(decoded)
	if err != nil {
		t.Fatalf("MarshalYAML(decoded): %v", err)
	}
	if !bytes.Equal(origYAML, decodedYAML) {
		t.Fatalf("round trip changed marshaled YAML:\norig:\n%s\ndecoded:\n%s", origYAML, decodedYAML)
	}
}

// TestParseCacheTreeRoundTripEmpty: a blank document normalizes to an
// empty map and must round-trip to a usable (non-nil) map.
func TestParseCacheTreeRoundTripEmpty(t *testing.T) {
	encoded, err := encodeCachedTree(map[string]interface{}{})
	if err != nil {
		t.Fatalf("encodeCachedTree: %v", err)
	}
	decoded, ok := decodeCachedTree(encoded)
	if !ok {
		t.Fatal("decodeCachedTree failed")
	}
	if decoded == nil {
		t.Fatal("decoded tree is nil; must be an empty map")
	}
	if len(decoded) != 0 {
		t.Fatalf("decoded tree has %d entries", len(decoded))
	}
}

// TestParseCacheDecodeCorrupt: garbage bytes must report a miss, never
// panic or return a partial tree.
func TestParseCacheDecodeCorrupt(t *testing.T) {
	if _, ok := decodeCachedTree([]byte("not gob at all")); ok {
		t.Fatal("corrupt data decoded successfully")
	}
}

func TestParseCacheKeyStabilityAndSensitivity(t *testing.T) {
	a := parseCacheKey([]byte("a: 1\n"))
	if a != parseCacheKey([]byte("a: 1\n")) {
		t.Fatal("identical bytes must produce identical keys")
	}
	if a == parseCacheKey([]byte("a: 2\n")) {
		t.Fatal("different bytes must produce different keys")
	}

	origVersion := Version
	defer func() { Version = origVersion }()
	Version = origVersion + "-test"
	if a == parseCacheKey([]byte("a: 1\n")) {
		t.Fatal("graft version must salt the parse cache key")
	}
}

package graft

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// TestDocumentCheckedGetters covers String/Int/Int64/Float64/Bool/Has: hit,
// missing path, and wrong type, asserting the documented zero value (and
// no error return at all, since these are the checked-getter forms).
func TestDocumentCheckedGetters(t *testing.T) {
	doc := NewDocument(map[string]interface{}{
		"string": "hello",
		"int":    42,
		"int64":  int64(9223372036854775807),
		"float":  3.14,
		"bool":   true,
		"nested": map[string]interface{}{
			"deep": "value",
		},
	})

	t.Run("String", func(t *testing.T) {
		cases := map[string]string{
			"string":          "hello", // hit
			"does.not.exist":  "",      // missing path
			"int":             "",      // wrong type
			"nested.does.not": "",      // missing nested path
		}
		for path, want := range cases {
			if got := doc.String(path); got != want {
				t.Errorf("String(%q) = %q, want %q", path, got, want)
			}
		}
	})

	t.Run("Int", func(t *testing.T) {
		cases := map[string]int{
			"int":            42, // hit
			"does.not.exist": 0,  // missing path
			"string":         0,  // wrong type
			"float":          0,  // wrong type (non-whole handled by GetFloat64 fixture below; 3.14 is not whole)
		}
		for path, want := range cases {
			if got := doc.Int(path); got != want {
				t.Errorf("Int(%q) = %d, want %d", path, got, want)
			}
		}
	})

	t.Run("Int64", func(t *testing.T) {
		cases := map[string]int64{
			"int64":          9223372036854775807, // hit
			"int":            42,                  // hit via widening conversion
			"does.not.exist": 0,                   // missing path
			"string":         0,                   // wrong type
		}
		for path, want := range cases {
			if got := doc.Int64(path); got != want {
				t.Errorf("Int64(%q) = %d, want %d", path, got, want)
			}
		}
	})

	t.Run("Float64", func(t *testing.T) {
		cases := map[string]float64{
			"float":          3.14, // hit
			"int":            42.0, // hit via widening conversion
			"does.not.exist": 0,    // missing path
			"string":         0,    // wrong type
		}
		for path, want := range cases {
			if got := doc.Float64(path); got != want {
				t.Errorf("Float64(%q) = %f, want %f", path, got, want)
			}
		}
	})

	t.Run("Bool", func(t *testing.T) {
		cases := map[string]bool{
			"bool":           true,  // hit
			"does.not.exist": false, // missing path
			"string":         false, // wrong type
		}
		for path, want := range cases {
			if got := doc.Bool(path); got != want {
				t.Errorf("Bool(%q) = %v, want %v", path, got, want)
			}
		}
	})

	t.Run("Has", func(t *testing.T) {
		cases := map[string]bool{
			"string":          true,  // top-level hit
			"nested.deep":     true,  // nested hit
			"does.not.exist":  false, // missing
			"nested.does.not": false, // missing nested
			"$":               true,  // root always resolves
		}
		for path, want := range cases {
			if got := doc.Has(path); got != want {
				t.Errorf("Has(%q) = %v, want %v", path, got, want)
			}
		}
	})
}

// TestDocumentPaths covers Paths() against a fixture with nested maps,
// lists, and lists-of-maps, asserting sorted, stable output and that every
// returned path round-trips through Get.
func TestDocumentPaths(t *testing.T) {
	doc := NewDocument(map[string]interface{}{
		"name": "app",
		"meta": map[string]interface{}{
			"owner": "team-a",
			"tags":  []interface{}{"a", "b"},
		},
		"instance_groups": []interface{}{
			map[string]interface{}{
				"name": "web",
				"jobs": []interface{}{
					map[string]interface{}{"name": "nginx"},
				},
			},
			map[string]interface{}{
				"name": "worker",
			},
		},
		"empty_map":  map[string]interface{}{},
		"empty_list": []interface{}{},
		"null_value": nil,
	})

	want := []string{
		"instance_groups.0.jobs.0.name",
		"instance_groups.0.name",
		"instance_groups.1.name",
		"meta.owner",
		"meta.tags.0",
		"meta.tags.1",
		"name",
		"null_value",
	}

	got := doc.Paths()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Paths() = %#v, want %#v", got, want)
	}

	if !sort.StringsAreSorted(got) {
		t.Error("Paths() output is not sorted")
	}

	// Stable across repeated calls.
	again := doc.Paths()
	if !reflect.DeepEqual(got, again) {
		t.Fatalf("Paths() is not stable across calls: %#v then %#v", got, again)
	}

	// Every returned path round-trips through Get, including the
	// explicit-nil leaf.
	for _, p := range got {
		if _, err := doc.Get(p); err != nil {
			t.Errorf("Get(%q) after Paths() returned error: %v", p, err)
		}
	}

	// Empty containers contribute no path of their own.
	for _, p := range got {
		if p == "empty_map" || p == "empty_list" {
			t.Errorf("Paths() unexpectedly included empty container path %q", p)
		}
	}
}

// TestDocumentPathsEmptyDocument covers the degenerate empty-document case.
func TestDocumentPathsEmptyDocument(t *testing.T) {
	doc := NewDocument(nil)
	if got := doc.Paths(); len(got) != 0 {
		t.Errorf("Paths() on an empty document = %#v, want empty", got)
	}
}

// TestDocumentSortKeys verifies SortKeys returns a distinct Document with
// the same data, and that the resulting document's YAML/JSON
// serialization has keys in ascending alphabetical order at every nesting
// level (the guarantee the method's doc comment describes).
func TestDocumentSortKeys(t *testing.T) {
	original := map[string]interface{}{
		"zeta": 1,
		"alpha": map[string]interface{}{
			"z": 1,
			"a": 2,
		},
		"list": []interface{}{
			map[string]interface{}{"z": 1, "a": 2},
		},
	}
	doc := NewDocument(original)

	sorted := doc.SortKeys()

	if sorted == Document(doc) {
		t.Fatal("SortKeys() returned the same Document value, want a distinct one")
	}

	// Data is unchanged (deep equal), regardless of storage order.
	if !reflect.DeepEqual(sorted.RawData(), original) {
		t.Fatalf("SortKeys() changed the data: got %#v, want %#v", sorted.RawData(), original)
	}

	yamlOut, err := sorted.ToYAML()
	if err != nil {
		t.Fatalf("ToYAML() after SortKeys(): %v", err)
	}
	assertIndexOrder(t, string(yamlOut), []string{"alpha:", "list:", "zeta:"})
	assertIndexOrder(t, string(yamlOut), []string{"a: 2", "z: 1"})

	jsonOut, err := sorted.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() after SortKeys(): %v", err)
	}
	assertIndexOrder(t, string(jsonOut), []string{`"alpha"`, `"list"`, `"zeta"`})
}

// assertIndexOrder fails the test unless every substring in order appears
// in s, at strictly increasing offsets.
func assertIndexOrder(t *testing.T, s string, order []string) {
	t.Helper()
	last := 0
	for _, sub := range order {
		rel := strings.Index(s[last:], sub)
		if rel < 0 {
			t.Errorf("expected %q to appear at or after offset %d in %q (order %v)", sub, last, s, order)
			return
		}
		last += rel + len(sub)
	}
}

// TestDocumentToJSONIndent covers the new Document-level convenience.
func TestDocumentToJSONIndent(t *testing.T) {
	doc := NewDocument(map[string]interface{}{
		"a": 1,
		"b": map[string]interface{}{"c": 2},
	})

	out, err := doc.ToJSONIndent("  ")
	if err != nil {
		t.Fatalf("ToJSONIndent() error: %v", err)
	}

	want := "{\n  \"a\": 1,\n  \"b\": {\n    \"c\": 2\n  }\n}"
	if string(out) != want {
		t.Errorf("ToJSONIndent(\"  \") = %q, want %q", string(out), want)
	}
}

// TestDocumentPruneVariadic covers the widening from Prune(key string) to
// Prune(keys ...string): a single-argument call (the pre-existing call
// shape, e.g. engine.go's doc.Prune(cleanPath)) still compiles and behaves
// identically, multiple keys prune independently against the same clone,
// zero keys is a no-op clone, and a key that does not resolve is skipped.
func TestDocumentPruneVariadic(t *testing.T) {
	base := func() Document {
		return NewDocument(map[string]interface{}{
			"a": 1,
			"b": 2,
			"c": map[string]interface{}{"d": 3, "e": 4},
		})
	}

	t.Run("single key call site keeps compiling and working", func(t *testing.T) {
		doc := base()
		pruned := doc.Prune("a")
		if pruned.Has("a") {
			t.Error("expected \"a\" to be pruned")
		}
		if !pruned.Has("b") {
			t.Error("expected \"b\" to remain")
		}
		if !doc.Has("a") {
			t.Error("Prune must not mutate the receiver")
		}
	})

	t.Run("multiple keys", func(t *testing.T) {
		doc := base()
		pruned := doc.Prune("a", "c.d")
		if pruned.Has("a") {
			t.Error("expected \"a\" to be pruned")
		}
		if pruned.Has("c.d") {
			t.Error("expected \"c.d\" to be pruned")
		}
		if !pruned.Has("c.e") {
			t.Error("expected \"c.e\" to remain")
		}
		if !pruned.Has("b") {
			t.Error("expected \"b\" to remain")
		}
	})

	t.Run("no keys returns an unmodified clone", func(t *testing.T) {
		doc := base()
		pruned := doc.Prune()
		if pruned == doc {
			t.Error("expected Prune() to return a distinct Document")
		}
		if !pruned.Has("a") || !pruned.Has("b") || !pruned.Has("c.d") {
			t.Error("expected Prune() with no keys to leave all data intact")
		}
	})

	t.Run("a key that does not resolve is silently skipped", func(t *testing.T) {
		doc := base()
		pruned := doc.Prune("does.not.exist", "a")
		if pruned.Has("a") {
			t.Error("expected \"a\" to still be pruned despite the earlier missing key")
		}
		if !pruned.Has("b") {
			t.Error("expected \"b\" to remain")
		}
	})
}

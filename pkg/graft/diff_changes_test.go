package graft

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/goccy/go-yaml"
)

// mustFlatten diffs a and b via the unmodified Diff() (diff.go) and runs
// the result through flattenDiff, failing the test on a Diff() error.
func mustFlatten(t *testing.T, a, b interface{}) []Change {
	t.Helper()
	d, err := Diff(a, b)
	if err != nil {
		t.Fatalf("Diff(%v, %v): unexpected error: %v", a, b, err)
	}
	return flattenDiff("", a, b, d)
}

func TestFlattenDiffTableDriven(t *testing.T) {
	tests := []struct {
		name string
		a    interface{}
		b    interface{}
		want []Change
	}{
		{
			name: "nested map field modified",
			a: map[string]interface{}{
				"meta": map[string]interface{}{"name": "foo", "version": 1},
			},
			b: map[string]interface{}{
				"meta": map[string]interface{}{"name": "bar", "version": 1},
			},
			want: []Change{
				{Type: ChangeModified, Path: "meta.name", OldValue: "foo", NewValue: "bar"},
			},
		},
		{
			name: "keyed list add, remove, and modify",
			a: map[string]interface{}{
				"servers": []interface{}{
					map[string]interface{}{"name": "web", "port": 80},
					map[string]interface{}{"name": "db", "port": 5432},
				},
			},
			b: map[string]interface{}{
				"servers": []interface{}{
					map[string]interface{}{"name": "web", "port": 8080},
					map[string]interface{}{"name": "cache", "port": 6379},
				},
			},
			want: []Change{
				{
					Type: ChangeAdded, Path: "servers[name=cache]",
					NewValue: map[string]interface{}{"name": "cache", "port": 6379},
				},
				{
					Type: ChangeRemoved, Path: "servers[name=db]",
					OldValue: map[string]interface{}{"name": "db", "port": 5432},
				},
				{Type: ChangeModified, Path: "servers[name=web].port", OldValue: 80, NewValue: 8080},
			},
		},
		{
			name: "simple list positional modify plus tail add",
			a:    map[string]interface{}{"items": []interface{}{"a", "b", "c"}},
			b:    map[string]interface{}{"items": []interface{}{"a", "x", "c", "d"}},
			want: []Change{
				{Type: ChangeAdded, Path: "items[3]", NewValue: "d"},
				{Type: ChangeModified, Path: "items[1]", OldValue: "b", NewValue: "x"},
			},
		},
		{
			name: "simple list tail removed",
			a:    map[string]interface{}{"items": []interface{}{"a", "b", "c"}},
			b:    map[string]interface{}{"items": []interface{}{"a", "b"}},
			want: []Change{
				{Type: ChangeRemoved, Path: "items[2]", OldValue: "c"},
			},
		},
		{
			name: "scalar to map type change",
			a:    map[string]interface{}{"value": "hello"},
			b:    map[string]interface{}{"value": map[string]interface{}{"nested": true}},
			want: []Change{
				{
					Type: ChangeTypeChanged, Path: "value",
					OldValue: "hello", NewValue: map[string]interface{}{"nested": true},
				},
			},
		},
		{
			name: "added-only top level key",
			a:    map[string]interface{}{"a": 1},
			b:    map[string]interface{}{"a": 1, "b": 2},
			want: []Change{
				{Type: ChangeAdded, Path: "b", NewValue: 2},
			},
		},
		{
			name: "removed-only top level key",
			a:    map[string]interface{}{"a": 1, "b": 2},
			b:    map[string]interface{}{"a": 1},
			want: []Change{
				{Type: ChangeRemoved, Path: "b", OldValue: 2},
			},
		},
		{
			name: "no change",
			a:    map[string]interface{}{"a": 1, "b": map[string]interface{}{"c": "d"}},
			b:    map[string]interface{}{"a": 1, "b": map[string]interface{}{"c": "d"}},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mustFlatten(t, tt.a, tt.b)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("flattenDiff mismatch\n got: %#v\nwant: %#v", got, tt.want)
			}
		})
	}
}

func TestChangeTypeString(t *testing.T) {
	tests := []struct {
		ct   ChangeType
		want string
	}{
		{ChangeAdded, "added"},
		{ChangeRemoved, "removed"},
		{ChangeModified, "modified"},
		{ChangeTypeChanged, "type changed"},
		{ChangeType(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.ct.String(); got != tt.want {
			t.Errorf("ChangeType(%d).String() = %q, want %q", tt.ct, got, tt.want)
		}
	}
}

func TestDiffResultAccessors(t *testing.T) {
	a := NewDocument(map[string]interface{}{
		"a": 1,
		"b": 2,
		"c": map[string]interface{}{"x": "old"},
	})
	b := NewDocument(map[string]interface{}{
		"a": 1,
		"d": 3,
		"c": map[string]interface{}{"x": "new"},
	})

	result, err := DiffDocuments(a, b, nil)
	if err != nil {
		t.Fatalf("DiffDocuments: unexpected error: %v", err)
	}

	if !result.HasChanges() {
		t.Fatal("HasChanges() = false, want true")
	}

	added := result.Added()
	if len(added) != 1 || added[0].Path != "d" {
		t.Errorf("Added() = %#v, want single change at path \"d\"", added)
	}

	removed := result.Removed()
	if len(removed) != 1 || removed[0].Path != "b" {
		t.Errorf("Removed() = %#v, want single change at path \"b\"", removed)
	}

	modified := result.Modified()
	if len(modified) != 1 || modified[0].Path != "c.x" {
		t.Errorf("Modified() = %#v, want single change at path \"c.x\"", modified)
	}

	all := result.Changes()
	if len(all) != 3 {
		t.Errorf("Changes() returned %d entries, want 3", len(all))
	}

	atPath := result.ChangesAtPath("c")
	if len(atPath) != 1 || atPath[0].Path != "c.x" {
		t.Errorf("ChangesAtPath(\"c\") = %#v, want the descendant change at \"c.x\"", atPath)
	}
	if len(result.ChangesAtPath("c.x")) != 1 {
		t.Errorf("ChangesAtPath(\"c.x\") should also match the change at its own exact path")
	}
	if len(result.ChangesAtPath("nonexistent")) != 0 {
		t.Errorf("ChangesAtPath(\"nonexistent\") should return no changes")
	}
}

func TestDiffResultChangesReturnsACopy(t *testing.T) {
	a := NewDocument(map[string]interface{}{"a": 1})
	b := NewDocument(map[string]interface{}{"a": 2})

	result, err := DiffDocuments(a, b, nil)
	if err != nil {
		t.Fatalf("DiffDocuments: unexpected error: %v", err)
	}

	got := result.Changes()
	got[0].Path = "mutated"

	again := result.Changes()
	if again[0].Path == "mutated" {
		t.Fatal("Changes() leaked internal storage: mutating the returned slice affected a later call")
	}
}

func TestHasChangesFalseForIdenticalDocuments(t *testing.T) {
	docA := func() Document {
		return NewDocument(map[string]interface{}{
			"servers": []interface{}{
				map[string]interface{}{"name": "web", "port": 80},
				map[string]interface{}{"name": "db", "port": 5432},
			},
			"meta": map[string]interface{}{"version": 1},
		})
	}

	result, err := DiffDocuments(docA(), docA(), nil)
	if err != nil {
		t.Fatalf("DiffDocuments: unexpected error: %v", err)
	}
	if result.HasChanges() {
		t.Errorf("HasChanges() = true for identical documents, want false; changes: %#v", result.Changes())
	}
}

func TestHasChangesFalseWhenIgnoreArrayOrderReorders(t *testing.T) {
	a := NewDocument(map[string]interface{}{"items": []interface{}{"a", "b", "c"}})
	b := NewDocument(map[string]interface{}{"items": []interface{}{"c", "a", "b"}})

	// Without IgnoreArrayOrder, the reordered list registers as changes.
	strict, err := DiffDocuments(a, b, DefaultDiffOptions())
	if err != nil {
		t.Fatalf("DiffDocuments (strict): unexpected error: %v", err)
	}
	if !strict.HasChanges() {
		t.Fatal("HasChanges() = false for a reordered list under strict (default) options, want true")
	}

	opts := DefaultDiffOptions()
	opts.IgnoreArrayOrder = true
	lenient, err := DiffDocuments(a, b, opts)
	if err != nil {
		t.Fatalf("DiffDocuments (IgnoreArrayOrder): unexpected error: %v", err)
	}
	if lenient.HasChanges() {
		t.Errorf("HasChanges() = true with IgnoreArrayOrder for a reordered list, want false; changes: %#v", lenient.Changes())
	}
}

// TestHasChangesFalseWhenIgnoreWhitespaceNormalizes is the F7 regression
// guard: DiffOptions.IgnoreWhitespace previously had zero test coverage
// anywhere in pkg/graft. It mirrors
// TestHasChangesFalseWhenIgnoreArrayOrderReorders's shape (strict diff
// registers changes, lenient diff does not), and exercises the
// normalization at both map and list nesting depth in one document, the
// same two shapes the review's probe covered.
func TestHasChangesFalseWhenIgnoreWhitespaceNormalizes(t *testing.T) {
	a := NewDocument(map[string]interface{}{
		"meta":  map[string]interface{}{"note": "hello   world"},
		"items": []interface{}{"foo  bar", "  baz  "},
	})
	b := NewDocument(map[string]interface{}{
		"meta":  map[string]interface{}{"note": "hello world"},
		"items": []interface{}{"foo bar", "baz"},
	})

	// Without IgnoreWhitespace, the differing internal/leading/trailing
	// whitespace registers as changes.
	strict, err := DiffDocuments(a, b, DefaultDiffOptions())
	if err != nil {
		t.Fatalf("DiffDocuments (strict): unexpected error: %v", err)
	}
	if !strict.HasChanges() {
		t.Fatal("HasChanges() = false for whitespace-only differences under strict (default) options, want true")
	}

	opts := DefaultDiffOptions()
	opts.IgnoreWhitespace = true
	lenient, err := DiffDocuments(a, b, opts)
	if err != nil {
		t.Fatalf("DiffDocuments (IgnoreWhitespace): unexpected error: %v", err)
	}
	if lenient.HasChanges() {
		t.Errorf("HasChanges() = true with IgnoreWhitespace for whitespace-only differences, want false; changes: %#v", lenient.Changes())
	}
}

func TestDiffDocumentsIgnorePathsAndOnlyPaths(t *testing.T) {
	a := NewDocument(map[string]interface{}{
		"keep":   1,
		"drop":   2,
		"nested": map[string]interface{}{"a": 1, "b": 2},
	})
	b := NewDocument(map[string]interface{}{
		"keep":   10,
		"drop":   20,
		"nested": map[string]interface{}{"a": 10, "b": 20},
	})

	opts := DefaultDiffOptions()
	opts.IgnorePaths = []string{"drop", "nested.b"}
	result, err := DiffDocuments(a, b, opts)
	if err != nil {
		t.Fatalf("DiffDocuments: unexpected error: %v", err)
	}
	for _, c := range result.Changes() {
		if c.Path == "drop" || c.Path == "nested.b" {
			t.Errorf("IgnorePaths did not exclude change at %q", c.Path)
		}
	}
	if len(result.Changes()) != 2 {
		t.Errorf("expected 2 surviving changes, got %d: %#v", len(result.Changes()), result.Changes())
	}

	opts2 := DefaultDiffOptions()
	opts2.OnlyPaths = []string{"nested.*"}
	only, err := DiffDocuments(a, b, opts2)
	if err != nil {
		t.Fatalf("DiffDocuments: unexpected error: %v", err)
	}
	for _, c := range only.Changes() {
		if c.Path != "nested.a" && c.Path != "nested.b" {
			t.Errorf("OnlyPaths let through unexpected change at %q", c.Path)
		}
	}
	if len(only.Changes()) != 2 {
		t.Errorf("expected 2 changes under nested.*, got %d: %#v", len(only.Changes()), only.Changes())
	}
}

func TestDiffDocumentsRejectsNilDocuments(t *testing.T) {
	valid := NewDocument(map[string]interface{}{"a": 1})

	if _, err := DiffDocuments(nil, valid, nil); err == nil {
		t.Error("DiffDocuments(nil, valid, nil) should return an error")
	}
	if _, err := DiffDocuments(valid, nil, nil); err == nil {
		t.Error("DiffDocuments(valid, nil, nil) should return an error")
	}
}

func TestDiffResultToJSONRoundTrip(t *testing.T) {
	a := NewDocument(map[string]interface{}{"count": 1, "name": "old"})
	b := NewDocument(map[string]interface{}{"count": 2, "name": "old"})

	result, err := DiffDocuments(a, b, nil)
	if err != nil {
		t.Fatalf("DiffDocuments: unexpected error: %v", err)
	}

	data, err := result.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON: unexpected error: %v", err)
	}

	var decoded []map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(ToJSON output): %v", err)
	}
	if len(decoded) != 1 {
		t.Fatalf("decoded %d entries, want 1", len(decoded))
	}
	entry := decoded[0]
	if entry["type"] != "modified" || entry["path"] != "count" {
		t.Errorf("unexpected decoded entry: %#v", entry)
	}
	// JSON numbers decode as float64; compare numerically rather than by type.
	if v, ok := entry["old_value"].(float64); !ok || v != 1 {
		t.Errorf("old_value = %#v, want 1", entry["old_value"])
	}
	if v, ok := entry["new_value"].(float64); !ok || v != 2 {
		t.Errorf("new_value = %#v, want 2", entry["new_value"])
	}
}

func TestDiffResultToYAMLRoundTrip(t *testing.T) {
	a := NewDocument(map[string]interface{}{"count": 1})
	b := NewDocument(map[string]interface{}{"count": 2})

	result, err := DiffDocuments(a, b, nil)
	if err != nil {
		t.Fatalf("DiffDocuments: unexpected error: %v", err)
	}

	data, err := result.ToYAML()
	if err != nil {
		t.Fatalf("ToYAML: unexpected error: %v", err)
	}

	var decoded []map[string]interface{}
	if err := yaml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("yaml.Unmarshal(ToYAML output): %v\n%s", err, data)
	}
	if len(decoded) != 1 {
		t.Fatalf("decoded %d entries, want 1", len(decoded))
	}
	if decoded[0]["type"] != "modified" || decoded[0]["path"] != "count" {
		t.Errorf("unexpected decoded entry: %#v", decoded[0])
	}
}

// -----------------------------------------------------------------------
// Property test: applying every Change in Changes() to a (under
// DiffDocuments' default, positional options) yields b. This exercises
// map field set/delete, simple-list index set/append/delete, and
// keyed-list entry set/append/delete, by walking Change.Path with
// ParsePath -- the same parser PathMatches uses -- and is therefore also a
// second, independent check that flattenDiff's paths are valid
// ParsePath/PathMatches grammar, not just well-formed strings.
//
// This property is scoped to DefaultDiffOptions(): IgnoreArrayOrder
// intentionally diffs canonicalized copies rather than the originals
// (diff_options.go), so its Change.Path values are not meant to apply
// back onto the un-canonicalized original a.
// -----------------------------------------------------------------------

func TestApplyChangesReconstructsB(t *testing.T) {
	cases := []struct {
		name string
		a    map[string]interface{}
		b    map[string]interface{}
	}{
		{
			name: "nested map field modified",
			a:    map[string]interface{}{"meta": map[string]interface{}{"name": "foo", "version": 1}},
			b:    map[string]interface{}{"meta": map[string]interface{}{"name": "bar", "version": 1}},
		},
		{
			name: "keyed list add, remove, and modify",
			a: map[string]interface{}{"servers": []interface{}{
				map[string]interface{}{"name": "web", "port": 80},
				map[string]interface{}{"name": "db", "port": 5432},
			}},
			b: map[string]interface{}{"servers": []interface{}{
				map[string]interface{}{"name": "web", "port": 8080},
				map[string]interface{}{"name": "cache", "port": 6379},
			}},
		},
		{
			name: "simple list modify and tail add",
			a:    map[string]interface{}{"items": []interface{}{"a", "b", "c"}},
			b:    map[string]interface{}{"items": []interface{}{"a", "x", "c", "d"}},
		},
		{
			name: "simple list tail removed",
			a:    map[string]interface{}{"items": []interface{}{"a", "b", "c"}},
			b:    map[string]interface{}{"items": []interface{}{"a", "b"}},
		},
		{
			name: "top level add and remove",
			a:    map[string]interface{}{"a": 1, "drop": 2},
			b:    map[string]interface{}{"a": 1, "keep": 3},
		},
		{
			name: "type change",
			a:    map[string]interface{}{"value": "hello"},
			b:    map[string]interface{}{"value": map[string]interface{}{"nested": true}},
		},
		{
			name: "no changes",
			a:    map[string]interface{}{"a": 1, "b": []interface{}{1, 2, 3}},
			b:    map[string]interface{}{"a": 1, "b": []interface{}{1, 2, 3}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := DiffDocuments(NewDocument(tc.a), NewDocument(tc.b), nil)
			if err != nil {
				t.Fatalf("DiffDocuments: unexpected error: %v", err)
			}

			got := applyChangesForTest(t, tc.a, result.Changes())
			if !reflect.DeepEqual(got, tc.b) {
				t.Errorf("applying Changes() to a did not reconstruct b\n  a: %#v\n  applied: %#v\n  b: %#v\n  changes: %#v",
					tc.a, got, tc.b, result.Changes())
			}
		})
	}
}

// applyChangesForTest applies each change to a deep copy of a in order and
// returns the result.
func applyChangesForTest(t *testing.T, a map[string]interface{}, changes []Change) map[string]interface{} {
	t.Helper()
	root := deepCopy(a)
	for _, c := range changes {
		root = applyChangeForTest(t, root, c)
	}
	result, ok := root.(map[string]interface{})
	if !ok {
		t.Fatalf("applyChangesForTest: root is %T after applying changes, want map[string]interface{}", root)
	}
	return result
}

// applyChangeForTest applies a single Change to root by parsing Change.Path
// with ParsePath and navigating/mutating the matching container.
func applyChangeForTest(t *testing.T, root interface{}, c Change) interface{} {
	t.Helper()
	segs, err := ParsePath(c.Path)
	if err != nil {
		t.Fatalf("ParsePath(%q): unexpected error: %v", c.Path, err)
	}
	if len(segs) == 0 {
		t.Fatalf("Change.Path %q parsed to zero segments", c.Path)
	}
	return applySegmentsForTest(root, segs, c)
}

func applySegmentsForTest(root interface{}, segs []PathSegment, c Change) interface{} {
	head := segs[0]
	rest := segs[1:]

	if len(rest) == 0 {
		return applyLeafForTest(root, head, c)
	}

	switch head.Type {
	case PathSegmentField:
		m, ok := root.(map[string]interface{})
		if !ok {
			return root
		}
		m[head.Key] = applySegmentsForTest(m[head.Key], rest, c)
		return m
	case PathSegmentIndex:
		l, ok := root.([]interface{})
		if !ok || head.Index < 0 || head.Index >= len(l) {
			return root
		}
		l[head.Index] = applySegmentsForTest(l[head.Index], rest, c)
		return l
	case PathSegmentKeyMatch:
		l, ok := root.([]interface{})
		if !ok {
			return root
		}
		for i, item := range l {
			if m, ok := item.(map[string]interface{}); ok && keyMatchValue(m, head.Match.Key) == head.Match.Value {
				l[i] = applySegmentsForTest(item, rest, c)
				return l
			}
		}
		return root
	default:
		return root
	}
}

func applyLeafForTest(root interface{}, seg PathSegment, c Change) interface{} {
	switch seg.Type {
	case PathSegmentField:
		m, ok := root.(map[string]interface{})
		if !ok {
			m = map[string]interface{}{}
		}
		switch c.Type {
		case ChangeAdded, ChangeModified, ChangeTypeChanged:
			m[seg.Key] = c.NewValue
		case ChangeRemoved:
			delete(m, seg.Key)
		}
		return m
	case PathSegmentIndex:
		l, _ := root.([]interface{})
		switch c.Type {
		case ChangeAdded:
			l = append(l, c.NewValue)
		case ChangeModified, ChangeTypeChanged:
			if seg.Index >= 0 && seg.Index < len(l) {
				l[seg.Index] = c.NewValue
			}
		case ChangeRemoved:
			if seg.Index >= 0 && seg.Index < len(l) {
				l = append(l[:seg.Index], l[seg.Index+1:]...)
			}
		}
		return l
	case PathSegmentKeyMatch:
		l, _ := root.([]interface{})
		switch c.Type {
		case ChangeAdded:
			l = append(l, c.NewValue)
		case ChangeModified, ChangeTypeChanged:
			for i, item := range l {
				if m, ok := item.(map[string]interface{}); ok && keyMatchValue(m, seg.Match.Key) == seg.Match.Value {
					l[i] = c.NewValue
				}
			}
		case ChangeRemoved:
			out := l[:0]
			for _, item := range l {
				if m, ok := item.(map[string]interface{}); ok && keyMatchValue(m, seg.Match.Key) == seg.Match.Value {
					continue
				}
				out = append(out, item)
			}
			l = out
		}
		return l
	default:
		return root
	}
}

// keyMatchValue stringifies m[field] the same way diffKeyedLists'
// underlying mapify (diff.go, `fmt.Sprintf("%v", kv)`) does, so
// PathSegmentKeyMatch.Match.Value (itself produced from that same
// stringification, via keyed()/mapify() inside flattenDiffList) compares
// equal to it.
func keyMatchValue(m map[string]interface{}, field string) string {
	v, ok := m[field]
	if !ok {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

package merger

import (
	"errors"
	"testing"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
)

// TestCanKeyMergeArrayMessageParity pins every failure case's rendered
// message to the historical text, in both color modes.
func TestCanKeyMergeArrayMessageParity(t *testing.T) {
	cases := []struct {
		name  string
		array []interface{}
		want  string // expected message with color disabled
	}{
		{
			name:  "nil element",
			array: []interface{}{nil},
			want:  "jobs.0: original object is nil - cannot merge by key",
		},
		{
			name:  "non-map element",
			array: []interface{}{"a-string"},
			want:  "jobs.0: original object is a string, not a map - cannot merge by key",
		},
		{
			name:  "int element",
			array: []interface{}{42},
			want:  "jobs.0: original object is a int, not a map - cannot merge by key",
		},
		{
			name:  "interface-keyed map element",
			array: []interface{}{map[interface{}]interface{}{"name": "x"}},
			want:  "jobs.0: original object is not a map[string]interface{} - cannot merge by key",
		},
		{
			name:  "missing key",
			array: []interface{}{map[string]interface{}{"id": "x"}},
			want:  "jobs.0: original object does not contain the key 'name' - cannot merge by key",
		},
		{
			name:  "second element missing key",
			array: []interface{}{map[string]interface{}{"name": "a"}, map[string]interface{}{"id": "b"}},
			want:  "jobs.1: original object does not contain the key 'name' - cannot merge by key",
		},
	}

	for _, colorOn := range []bool{false, true} {
		ansi.Color(colorOn)
		for _, tc := range cases {
			err := canKeyMergeArray("original", tc.array, "jobs", "name")
			if err == nil {
				t.Errorf("color=%v %s: expected an error", colorOn, tc.name)
				continue
			}
			if !colorOn && err.Error() != tc.want {
				t.Errorf("%s: got %q, want %q", tc.name, err.Error(), tc.want)
			}
			// Color mode: exact bytes must match what the eager
			// ansi.Errorf formatting produced.
			var refErr error
			switch tc.name {
			case "nil element":
				refErr = ansi.Errorf("@m{%s.%d}: @R{%s object is nil - cannot merge by key}", "jobs", 0, "original")
			case "non-map element":
				refErr = ansi.Errorf("@m{%s.%d}: @R{%s object is a} @c{%s}@R{, not a} @c{map} @R{- cannot merge by key}", "jobs", 0, "original", "string")
			case "int element":
				refErr = ansi.Errorf("@m{%s.%d}: @R{%s object is a} @c{%s}@R{, not a} @c{map} @R{- cannot merge by key}", "jobs", 0, "original", "int")
			case "interface-keyed map element":
				refErr = ansi.Errorf("@m{%s.%d}: @R{%s object is not a map[string]interface{} - cannot merge by key}", "jobs", 0, "original")
			case "missing key":
				refErr = ansi.Errorf("@m{%s.%d}: @R{%s object does not contain the key} @c{'%s'}@R{ - cannot merge by key}", "jobs", 0, "original", "name")
			case "second element missing key":
				refErr = ansi.Errorf("@m{%s.%d}: @R{%s object does not contain the key} @c{'%s'}@R{ - cannot merge by key}", "jobs", 1, "original", "name")
			}
			if err.Error() != refErr.Error() {
				t.Errorf("color=%v %s: got %q, want %q", colorOn, tc.name, err.Error(), refErr.Error())
			}
		}
	}
	ansi.Color(false)
}

// TestCanKeyMergeArrayWarningCase pins the hash/sequence-valued key
// case: it must stay a WarningError carrying eContextDefaultMerge, since
// mergeArrayDefault's fallback warning depends on errors.As finding it.
func TestCanKeyMergeArrayWarningCase(t *testing.T) {
	ansi.Color(false)
	array := []interface{}{map[string]interface{}{"name": map[string]interface{}{"nested": true}}}

	err := canKeyMergeArray("original", array, "jobs", "name")
	if err == nil {
		t.Fatal("expected an error for hash-valued key")
	}
	var warning WarningError
	if !errors.As(err, &warning) {
		t.Fatalf("expected a WarningError, got %T", err)
	}
	if !warning.HasContext(eContextDefaultMerge) {
		t.Error("expected eContextDefaultMerge context")
	}
	want := "jobs.0: original object's key 'name' cannot have a value which is a hash or sequence - cannot merge by key"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

// TestCanKeyMergeArraySuccess: a clean key-mergeable array yields nil.
func TestCanKeyMergeArraySuccess(t *testing.T) {
	array := []interface{}{
		map[string]interface{}{"name": "a", "v": 1},
		map[string]interface{}{"name": "b", "v": 2},
	}
	if err := canKeyMergeArray("original", array, "jobs", "name"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

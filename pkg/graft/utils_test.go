package graft

import (
	"encoding/json"
	"reflect"
	"testing"
)

// =============================================================================
// Path Parsing Tests
// =============================================================================

func TestParsePath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected []PathSegment
		wantErr  bool
	}{
		{
			name: "simple single field",
			path: "database",
			expected: []PathSegment{
				{Type: PathSegmentField, Key: "database"},
			},
		},
		{
			name: "dotted path",
			path: "database.host",
			expected: []PathSegment{
				{Type: PathSegmentField, Key: "database"},
				{Type: PathSegmentField, Key: "host"},
			},
		},
		{
			name: "deep path",
			path: "a.b.c.d.e",
			expected: []PathSegment{
				{Type: PathSegmentField, Key: "a"},
				{Type: PathSegmentField, Key: "b"},
				{Type: PathSegmentField, Key: "c"},
				{Type: PathSegmentField, Key: "d"},
				{Type: PathSegmentField, Key: "e"},
			},
		},
		{
			name: "array index",
			path: "items[0].name",
			expected: []PathSegment{
				{Type: PathSegmentField, Key: "items"},
				{Type: PathSegmentIndex, Index: 0},
				{Type: PathSegmentField, Key: "name"},
			},
		},
		{
			name: "key match",
			path: "items[name=foo].value",
			expected: []PathSegment{
				{Type: PathSegmentField, Key: "items"},
				{Type: PathSegmentKeyMatch, Match: &KeyMatch{Key: "name", Value: "foo"}},
				{Type: PathSegmentField, Key: "value"},
			},
		},
		{
			name: "index only",
			path: "[0]",
			expected: []PathSegment{
				{Type: PathSegmentIndex, Index: 0},
			},
		},
		{
			name: "index with field",
			path: "[0].name",
			expected: []PathSegment{
				{Type: PathSegmentIndex, Index: 0},
				{Type: PathSegmentField, Key: "name"},
			},
		},
		{
			name: "multiple indices",
			path: "matrix[0][1]",
			expected: []PathSegment{
				{Type: PathSegmentField, Key: "matrix"},
				{Type: PathSegmentIndex, Index: 0},
				{Type: PathSegmentIndex, Index: 1},
			},
		},
		{
			name:     "empty path",
			path:     "",
			expected: []PathSegment{},
		},
		{
			name: "quoted key match value",
			path: `items[name="foo bar"].value`,
			expected: []PathSegment{
				{Type: PathSegmentField, Key: "items"},
				{Type: PathSegmentKeyMatch, Match: &KeyMatch{Key: "name", Value: "foo bar"}},
				{Type: PathSegmentField, Key: "value"},
			},
		},
		{
			name:    "unclosed bracket",
			path:    "items[0",
			wantErr: true,
		},
		{
			name:    "invalid index",
			path:    "items[abc]",
			wantErr: true,
		},
		{
			name:    "trailing dot",
			path:    "database.",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePath(tt.path)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParsePath(%q) expected error, got nil", tt.path)
				}
				return
			}

			if err != nil {
				t.Errorf("ParsePath(%q) unexpected error: %v", tt.path, err)
				return
			}

			if len(got) != len(tt.expected) {
				t.Errorf("ParsePath(%q) got %d segments, want %d", tt.path, len(got), len(tt.expected))
				return
			}

			for i, seg := range got {
				exp := tt.expected[i]
				if seg.Type != exp.Type {
					t.Errorf("ParsePath(%q) segment %d: type = %v, want %v", tt.path, i, seg.Type, exp.Type)
				}
				if seg.Key != exp.Key {
					t.Errorf("ParsePath(%q) segment %d: key = %q, want %q", tt.path, i, seg.Key, exp.Key)
				}
				if seg.Index != exp.Index {
					t.Errorf("ParsePath(%q) segment %d: index = %d, want %d", tt.path, i, seg.Index, exp.Index)
				}
				if exp.Match != nil {
					if seg.Match == nil {
						t.Errorf("ParsePath(%q) segment %d: match is nil, want %+v", tt.path, i, exp.Match)
					} else if *seg.Match != *exp.Match {
						t.Errorf("ParsePath(%q) segment %d: match = %+v, want %+v", tt.path, i, seg.Match, exp.Match)
					}
				}
			}
		})
	}
}

func TestPathSegmentString(t *testing.T) {
	tests := []struct {
		segment  PathSegment
		expected string
	}{
		{PathSegment{Type: PathSegmentField, Key: "database"}, "database"},
		{PathSegment{Type: PathSegmentIndex, Index: 5}, "[5]"},
		{PathSegment{Type: PathSegmentKeyMatch, Match: &KeyMatch{Key: "id", Value: "abc"}}, "[id=abc]"},
		{PathSegment{Type: PathSegmentKeyMatch, Match: nil}, "[]"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.segment.String(); got != tt.expected {
				t.Errorf("PathSegment.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// =============================================================================
// Path Building Tests
// =============================================================================

func TestJoinPath(t *testing.T) {
	tests := []struct {
		name     string
		segments []string
		expected string
	}{
		{"empty", []string{}, ""},
		{"single", []string{"database"}, "database"},
		{"two fields", []string{"database", "host"}, "database.host"},
		{"three fields", []string{"a", "b", "c"}, "a.b.c"},
		{"with index", []string{"items", "[0]", "name"}, "items[0].name"},
		{"empty segments ignored", []string{"a", "", "b"}, "a.b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := JoinPath(tt.segments...); got != tt.expected {
				t.Errorf("JoinPath(%v) = %q, want %q", tt.segments, got, tt.expected)
			}
		})
	}
}

func TestAppendPath(t *testing.T) {
	tests := []struct {
		base     string
		segment  string
		expected string
	}{
		{"", "database", "database"},
		{"database", "", "database"},
		{"database", "host", "database.host"},
		{"items", "[0]", "items[0]"},
		{"items[0]", "name", "items[0].name"},
		{"", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.base+"+"+tt.segment, func(t *testing.T) {
			if got := AppendPath(tt.base, tt.segment); got != tt.expected {
				t.Errorf("AppendPath(%q, %q) = %q, want %q", tt.base, tt.segment, got, tt.expected)
			}
		})
	}
}

func TestParentPath(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"", ""},
		{"database", ""},
		{"database.host", "database"},
		{"a.b.c", "a.b"},
		{"items[0].name", "items[0]"},
		{"items[0]", "items"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := ParentPath(tt.path); got != tt.expected {
				t.Errorf("ParentPath(%q) = %q, want %q", tt.path, got, tt.expected)
			}
		})
	}
}

func TestBaseName(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"", ""},
		{"database", "database"},
		{"database.host", "host"},
		{"a.b.c", "c"},
		{"items[0].name", "name"},
		{"items[0]", "[0]"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := BaseName(tt.path); got != tt.expected {
				t.Errorf("BaseName(%q) = %q, want %q", tt.path, got, tt.expected)
			}
		})
	}
}

// =============================================================================
// Path Matching Tests
// =============================================================================

func TestPathMatches(t *testing.T) {
	tests := []struct {
		path    string
		pattern string
		matches bool
	}{
		// Exact matches
		{"database.host", "database.host", true},
		{"database.host", "database.port", false},

		// Single wildcard
		{"database.host", "database.*", true},
		{"database.host", "*.host", true},
		{"a.b.c", "a.*.c", true},
		{"a.b.c", "*.*.c", true},

		// Double wildcard
		{"a.b.c.d", "a.**", true},
		{"a.b.c.d", "**.d", true},
		{"a.b.c.d", "a.**.d", true},
		{"a.b.c.d.e", "a.**.e", true},
		{"a.b", "a.**", true},

		// Empty
		{"", "", true},
		{"database", "", false},
		{"", "database", false},

		// Complex patterns
		{"items[0].name", "items.*.name", true},
		{"items[0].name", "items.[0].name", true},
	}

	for _, tt := range tests {
		t.Run(tt.path+"~"+tt.pattern, func(t *testing.T) {
			if got := PathMatches(tt.path, tt.pattern); got != tt.matches {
				t.Errorf("PathMatches(%q, %q) = %v, want %v", tt.path, tt.pattern, got, tt.matches)
			}
		})
	}
}

func TestPathHasPrefix(t *testing.T) {
	tests := []struct {
		path   string
		prefix string
		result bool
	}{
		{"database.host", "database", true},
		{"database.host", "database.host", true},
		{"database.host", "database.host.port", false},
		{"database.host", "other", false},
		{"database.host", "", true},
		{"", "database", false},
		{"items[0].name", "items[0]", true},
		{"items[0].name", "items[1]", false},
	}

	for _, tt := range tests {
		t.Run(tt.path+"^"+tt.prefix, func(t *testing.T) {
			if got := PathHasPrefix(tt.path, tt.prefix); got != tt.result {
				t.Errorf("PathHasPrefix(%q, %q) = %v, want %v", tt.path, tt.prefix, got, tt.result)
			}
		})
	}
}

// =============================================================================
// Type Conversion Tests
// =============================================================================

func TestToString(t *testing.T) {
	tests := []struct {
		input    interface{}
		expected string
	}{
		{nil, ""},
		{"hello", "hello"},
		{42, "42"},
		{int64(42), "42"},
		{3.14, "3.14"},
		{float32(3.14), "3.14"},
		{true, "true"},
		{false, "false"},
		{[]byte("bytes"), "bytes"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := ToString(tt.input); got != tt.expected {
				t.Errorf("ToString(%v) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestToInt(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected int
		wantErr  bool
	}{
		{"int", 42, 42, false},
		{"int64", int64(42), 42, false},
		{"float64", 42.9, 42, false},
		{"string int", "42", 42, false},
		{"string float", "42.5", 42, false},
		{"bool true", true, 1, false},
		{"bool false", false, 0, false},
		{"json.Number", json.Number("42"), 42, false},
		{"nil", nil, 0, true},
		{"invalid string", "abc", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ToInt(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ToInt(%v) expected error, got nil", tt.input)
				}
				return
			}

			if err != nil {
				t.Errorf("ToInt(%v) unexpected error: %v", tt.input, err)
				return
			}

			if got != tt.expected {
				t.Errorf("ToInt(%v) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}

func TestToInt64(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected int64
		wantErr  bool
	}{
		{"int", 42, 42, false},
		{"int64", int64(9223372036854775807), 9223372036854775807, false},
		{"float64", 42.9, 42, false},
		{"string", "42", 42, false},
		{"nil", nil, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ToInt64(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ToInt64(%v) expected error, got nil", tt.input)
				}
				return
			}

			if err != nil {
				t.Errorf("ToInt64(%v) unexpected error: %v", tt.input, err)
				return
			}

			if got != tt.expected {
				t.Errorf("ToInt64(%v) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}

func TestToFloat64(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected float64
		wantErr  bool
	}{
		{"float64", 3.14, 3.14, false},
		{"float32", float32(3.14), 3.140000104904175, false},
		{"int", 42, 42.0, false},
		{"string", "3.14", 3.14, false},
		{"bool true", true, 1.0, false},
		{"nil", nil, 0, true},
		{"invalid string", "abc", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ToFloat64(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ToFloat64(%v) expected error, got nil", tt.input)
				}
				return
			}

			if err != nil {
				t.Errorf("ToFloat64(%v) unexpected error: %v", tt.input, err)
				return
			}

			if got != tt.expected {
				t.Errorf("ToFloat64(%v) = %f, want %f", tt.input, got, tt.expected)
			}
		})
	}
}

func TestToBool(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected bool
		wantErr  bool
	}{
		{"true", true, true, false},
		{"false", false, false, false},
		{"string true", "true", true, false},
		{"string false", "false", false, false},
		{"string yes", "yes", true, false},
		{"string no", "no", false, false},
		{"string 1", "1", true, false},
		{"string 0", "0", false, false},
		{"string on", "on", true, false},
		{"string off", "off", false, false},
		{"string t", "t", true, false},
		{"string f", "f", false, false},
		{"string y", "y", true, false},
		{"string n", "n", false, false},
		{"string TRUE", "TRUE", true, false},
		{"string FALSE", "FALSE", false, false},
		{"int 1", 1, true, false},
		{"int 0", 0, false, false},
		{"float 1.0", 1.0, true, false},
		{"float 0.0", 0.0, false, false},
		{"nil", nil, false, false},
		{"empty string", "", false, false},
		{"invalid string", "maybe", false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ToBool(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ToBool(%v) expected error, got nil", tt.input)
				}
				return
			}

			if err != nil {
				t.Errorf("ToBool(%v) unexpected error: %v", tt.input, err)
				return
			}

			if got != tt.expected {
				t.Errorf("ToBool(%v) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestToSlice(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected []interface{}
		wantErr  bool
	}{
		{"interface slice", []interface{}{"a", 1}, []interface{}{"a", 1}, false},
		{"string slice", []string{"a", "b"}, []interface{}{"a", "b"}, false},
		{"int slice", []int{1, 2, 3}, []interface{}{1, 2, 3}, false},
		{"nil", nil, nil, false},
		{"not a slice", "hello", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ToSlice(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ToSlice(%v) expected error, got nil", tt.input)
				}
				return
			}

			if err != nil {
				t.Errorf("ToSlice(%v) unexpected error: %v", tt.input, err)
				return
			}

			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("ToSlice(%v) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestToStringSlice(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected []string
	}{
		{"string slice", []string{"a", "b"}, []string{"a", "b"}},
		{"interface slice", []interface{}{"a", 1, true}, []string{"a", "1", "true"}},
		{"int slice", []int{1, 2, 3}, []string{"1", "2", "3"}},
		{"nil", nil, nil},
		{"single value", "hello", []string{"hello"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ToStringSlice(tt.input)
			if err != nil {
				t.Errorf("ToStringSlice(%v) unexpected error: %v", tt.input, err)
				return
			}

			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("ToStringSlice(%v) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestToMap(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected map[string]interface{}
		wantErr  bool
	}{
		{
			"string map",
			map[string]interface{}{"a": 1, "b": 2},
			map[string]interface{}{"a": 1, "b": 2},
			false,
		},
		{
			"interface map",
			map[string]interface{}{"a": 1, "b": 2},
			map[string]interface{}{"a": 1, "b": 2},
			false,
		},
		{"nil", nil, nil, false},
		{"not a map", "hello", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ToMap(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ToMap(%v) expected error, got nil", tt.input)
				}
				return
			}

			if err != nil {
				t.Errorf("ToMap(%v) unexpected error: %v", tt.input, err)
				return
			}

			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("ToMap(%v) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

// =============================================================================
// Type Checking Tests
// =============================================================================

func TestTypeOf(t *testing.T) {
	tests := []struct {
		input    interface{}
		expected string
	}{
		{nil, "null"},
		{"hello", "string"},
		{42, "int"},
		{int64(42), "int"},
		{3.14, "float"},
		{float32(3.14), "float"},
		{true, "bool"},
		{map[string]interface{}{}, "map"},
		{[]interface{}{}, "array"},
		{[]string{}, "array"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := TypeOf(tt.input); got != tt.expected {
				t.Errorf("TypeOf(%v) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestIsEmpty(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected bool
	}{
		{"nil", nil, true},
		{"empty string", "", true},
		{"non-empty string", "hello", false},
		{"zero int", 0, true},
		{"non-zero int", 42, false},
		{"zero float", 0.0, true},
		{"non-zero float", 3.14, false},
		{"false bool", false, true},
		{"true bool", true, false},
		{"empty slice", []interface{}{}, true},
		{"non-empty slice", []interface{}{1}, false},
		{"empty map", map[string]interface{}{}, true},
		{"non-empty map", map[string]interface{}{"a": 1}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsEmpty(tt.input); got != tt.expected {
				t.Errorf("IsEmpty(%v) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestIsNil(t *testing.T) {
	var nilSlice []string
	var nilMap map[string]interface{}
	var nilPtr *int

	tests := []struct {
		name     string
		input    interface{}
		expected bool
	}{
		{"nil", nil, true},
		{"nil slice", nilSlice, true},
		{"nil map", nilMap, true},
		{"nil pointer", nilPtr, true},
		{"empty slice", []string{}, false},
		{"empty map", map[string]interface{}{}, false},
		{"string", "hello", false},
		{"int", 42, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNil(tt.input); got != tt.expected {
				t.Errorf("IsNil(%v) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestDeepEqual(t *testing.T) {
	tests := []struct {
		name     string
		a        interface{}
		b        interface{}
		expected bool
	}{
		{"equal strings", "hello", "hello", true},
		{"different strings", "hello", "world", false},
		{"equal ints", 42, 42, true},
		{"different ints", 42, 43, false},
		{"equal slices", []int{1, 2, 3}, []int{1, 2, 3}, true},
		{"different slices", []int{1, 2, 3}, []int{1, 2, 4}, false},
		{
			"equal maps",
			map[string]int{"a": 1, "b": 2},
			map[string]int{"a": 1, "b": 2},
			true,
		},
		{
			"different maps",
			map[string]int{"a": 1, "b": 2},
			map[string]int{"a": 1, "b": 3},
			false,
		},
		{"nil vs nil", nil, nil, true},
		{"nil vs value", nil, "hello", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DeepEqual(tt.a, tt.b); got != tt.expected {
				t.Errorf("DeepEqual(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.expected)
			}
		})
	}
}

// =============================================================================
// Deep Access Tests
// =============================================================================

func TestDeepGet(t *testing.T) {
	data := map[string]interface{}{
		"database": map[string]interface{}{
			"host": "localhost",
			"port": 5432,
		},
		"items": []interface{}{
			map[string]interface{}{"name": "item1", "value": 10},
			map[string]interface{}{"name": "item2", "value": 20},
		},
		"matrix": []interface{}{
			[]interface{}{1, 2, 3},
			[]interface{}{4, 5, 6},
		},
	}

	// Pre-extract items for test table to avoid type assertion in the struct literal
	items, ok := data["items"].([]interface{})
	if !ok {
		t.Fatal("expected items to be []interface{}")
	}

	tests := []struct {
		name     string
		path     string
		expected interface{}
		wantErr  bool
	}{
		{"root access", "", data, false},
		{"simple field", "database", data["database"], false},
		{"nested field", "database.host", "localhost", false},
		{"array index", "items[0]", items[0], false},
		{"array field", "items[0].name", "item1", false},
		{"key match", "items[name=item2].value", 20, false},
		{"matrix access", "matrix[1][2]", 6, false},
		{"missing key", "database.missing", nil, true},
		{"invalid index", "items[10]", nil, true},
		{"no key match", "items[name=missing]", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DeepGet(data, tt.path)

			if tt.wantErr {
				if err == nil {
					t.Errorf("DeepGet(%q) expected error, got nil", tt.path)
				}
				return
			}

			if err != nil {
				t.Errorf("DeepGet(%q) unexpected error: %v", tt.path, err)
				return
			}

			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("DeepGet(%q) = %v, want %v", tt.path, got, tt.expected)
			}
		})
	}
}

func TestDeepSet(t *testing.T) {
	t.Run("set nested field", func(t *testing.T) {
		data := map[string]interface{}{
			"database": map[string]interface{}{
				"host": "localhost",
			},
		}

		err := DeepSet(data, "database.port", 5432)
		if err != nil {
			t.Fatalf("DeepSet unexpected error: %v", err)
		}

		got, _ := DeepGet(data, "database.port")
		if got != 5432 {
			t.Errorf("DeepSet did not set value correctly: got %v", got)
		}
	})

	t.Run("create intermediate maps", func(t *testing.T) {
		data := map[string]interface{}{}

		err := DeepSet(data, "a.b.c", "value")
		if err != nil {
			t.Fatalf("DeepSet unexpected error: %v", err)
		}

		got, _ := DeepGet(data, "a.b.c")
		if got != "value" {
			t.Errorf("DeepSet did not create intermediate maps: got %v", got)
		}
	})

	t.Run("empty path error", func(t *testing.T) {
		data := map[string]interface{}{}
		err := DeepSet(data, "", "value")
		if err == nil {
			t.Error("DeepSet with empty path should error")
		}
	})
}

func TestDeepDelete(t *testing.T) {
	t.Run("delete field", func(t *testing.T) {
		data := map[string]interface{}{
			"a": 1,
			"b": 2,
		}

		err := DeepDelete(data, "a")
		if err != nil {
			t.Fatalf("DeepDelete unexpected error: %v", err)
		}

		if _, ok := data["a"]; ok {
			t.Error("DeepDelete did not remove field")
		}
	})

	t.Run("delete nested field", func(t *testing.T) {
		data := map[string]interface{}{
			"outer": map[string]interface{}{
				"a": 1,
				"b": 2,
			},
		}

		err := DeepDelete(data, "outer.a")
		if err != nil {
			t.Fatalf("DeepDelete unexpected error: %v", err)
		}

		outer, ok := data["outer"].(map[string]interface{})
		if !ok {
			t.Fatal("expected outer to be map[string]interface{}")
		}
		if _, ok := outer["a"]; ok {
			t.Error("DeepDelete did not remove nested field")
		}
	})
}

func TestDeepCopy(t *testing.T) {
	t.Run("copy map", func(t *testing.T) {
		original := map[string]interface{}{
			"a": 1,
			"b": map[string]interface{}{
				"c": 2,
			},
		}

		copiedRaw := DeepCopy(original)
		copied, ok := copiedRaw.(map[string]interface{})
		if !ok {
			t.Fatal("expected DeepCopy result to be map[string]interface{}")
		}

		// Modify original
		original["a"] = 999
		origB, ok := original["b"].(map[string]interface{})
		if !ok {
			t.Fatal("expected original[b] to be map[string]interface{}")
		}
		origB["c"] = 888

		// Verify copy is unchanged
		if copied["a"] != 1 {
			t.Errorf("DeepCopy did not create independent copy: a = %v", copied["a"])
		}
		copiedB, ok := copied["b"].(map[string]interface{})
		if !ok {
			t.Fatal("expected copied[b] to be map[string]interface{}")
		}
		if copiedB["c"] != 2 {
			t.Errorf("DeepCopy did not deep copy nested map: b.c = %v", copiedB["c"])
		}
	})

	t.Run("copy slice", func(t *testing.T) {
		original := []interface{}{1, 2, []interface{}{3, 4}}

		copiedRaw := DeepCopy(original)
		copied, ok := copiedRaw.([]interface{})
		if !ok {
			t.Fatal("expected DeepCopy result to be []interface{}")
		}

		// Modify original
		original[0] = 999
		origNested, ok := original[2].([]interface{})
		if !ok {
			t.Fatal("expected original[2] to be []interface{}")
		}
		origNested[0] = 888

		// Verify copy is unchanged
		if copied[0] != 1 {
			t.Errorf("DeepCopy did not create independent slice copy: [0] = %v", copied[0])
		}
		copiedNested, ok := copied[2].([]interface{})
		if !ok {
			t.Fatal("expected copied[2] to be []interface{}")
		}
		if copiedNested[0] != 3 {
			t.Errorf("DeepCopy did not deep copy nested slice: [2][0] = %v", copiedNested[0])
		}
	})

	t.Run("copy primitives", func(t *testing.T) {
		if DeepCopy("hello") != "hello" {
			t.Error("DeepCopy should preserve string")
		}
		if DeepCopy(42) != 42 {
			t.Error("DeepCopy should preserve int")
		}
		if DeepCopy(nil) != nil {
			t.Error("DeepCopy should preserve nil")
		}
	})
}

// =============================================================================
// String Utility Tests
// =============================================================================

func TestQuote(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", `"hello"`},
		{"hello world", `"hello world"`},
		{`hello "world"`, `"hello \"world\""`},
		{"line1\nline2", `"line1\nline2"`},
		{"tab\there", `"tab\there"`},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := Quote(tt.input); got != tt.expected {
				t.Errorf("Quote(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestUnquote(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{"double quoted", `"hello"`, "hello", false},
		{"single quoted", `'hello'`, "hello", false},
		{"backtick quoted", "`hello`", "hello", false},
		{"unquoted", "hello", "hello", false},
		{"empty", "", "", false},
		{"escaped", `"hello \"world\""`, `hello "world"`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Unquote(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Unquote(%q) expected error, got nil", tt.input)
				}
				return
			}

			if err != nil {
				t.Errorf("Unquote(%q) unexpected error: %v", tt.input, err)
				return
			}

			if got != tt.expected {
				t.Errorf("Unquote(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestIndent(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		prefix   string
		expected string
	}{
		{"single line", "hello", "  ", "  hello"},
		{"multi line", "line1\nline2\nline3", "  ", "  line1\n  line2\n  line3"},
		{"empty", "", "  ", ""},
		{"empty prefix", "hello", "", "hello"},
		{"tabs", "hello\nworld", "\t", "\thello\n\tworld"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Indent(tt.input, tt.prefix); got != tt.expected {
				t.Errorf("Indent(%q, %q) = %q, want %q", tt.input, tt.prefix, got, tt.expected)
			}
		})
	}
}

func TestTrimIndent(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			"common indent",
			"    line1\n    line2\n    line3",
			"line1\nline2\nline3",
		},
		{
			"mixed indent",
			"    line1\n      line2\n    line3",
			"line1\n  line2\nline3",
		},
		{
			"no indent",
			"line1\nline2",
			"line1\nline2",
		},
		{
			"empty lines",
			"    line1\n\n    line2",
			"line1\n\nline2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TrimIndent(tt.input); got != tt.expected {
				t.Errorf("TrimIndent(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// =============================================================================
// Additional Utility Tests
// =============================================================================

func TestSegmentsToPath(t *testing.T) {
	tests := []struct {
		name     string
		segments []PathSegment
		expected string
	}{
		{
			"empty",
			[]PathSegment{},
			"",
		},
		{
			"single field",
			[]PathSegment{{Type: PathSegmentField, Key: "database"}},
			"database",
		},
		{
			"dotted path",
			[]PathSegment{
				{Type: PathSegmentField, Key: "database"},
				{Type: PathSegmentField, Key: "host"},
			},
			"database.host",
		},
		{
			"with index",
			[]PathSegment{
				{Type: PathSegmentField, Key: "items"},
				{Type: PathSegmentIndex, Index: 0},
				{Type: PathSegmentField, Key: "name"},
			},
			"items[0].name",
		},
		{
			"with key match",
			[]PathSegment{
				{Type: PathSegmentField, Key: "items"},
				{Type: PathSegmentKeyMatch, Match: &KeyMatch{Key: "id", Value: "abc"}},
			},
			"items[id=abc]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SegmentsToPath(tt.segments); got != tt.expected {
				t.Errorf("SegmentsToPath(%v) = %q, want %q", tt.segments, got, tt.expected)
			}
		})
	}
}

func TestSplitPath(t *testing.T) {
	tests := []struct {
		path     string
		expected []string
	}{
		{"", []string{}},
		{"database", []string{"database"}},
		{"database.host", []string{"database", "host"}},
		{"items[0].name", []string{"items", "[0]", "name"}},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := SplitPath(tt.path)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("SplitPath(%q) = %v, want %v", tt.path, got, tt.expected)
			}
		})
	}
}

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"database.host", "database.host"},
		{"items[0].name", "items[0].name"},
		{"a.b.c", "a.b.c"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := NormalizePath(tt.input); got != tt.expected {
				t.Errorf("NormalizePath(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestContainsOperator(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"(( grab foo ))", true},
		{"(( vault secret/path ))", true},
		{"normal text", false},
		{"(( ))", true},
		{"some (( grab x )) text", true},
		{"no operators here", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := ContainsOperator(tt.input); got != tt.expected {
				t.Errorf("ContainsOperator(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestExtractOperatorContent(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		ok       bool
	}{
		{"(( grab foo ))", "grab foo", true},
		{"(( vault secret/path ))", "vault secret/path", true},
		{"((concat a b))", "concat a b", true},
		{"normal text", "", false},
		{"( grab foo )", "", false},
		{"(grab foo))", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, ok := ExtractOperatorContent(tt.input)
			if ok != tt.ok {
				t.Errorf("ExtractOperatorContent(%q) ok = %v, want %v", tt.input, ok, tt.ok)
			}
			if got != tt.expected {
				t.Errorf("ExtractOperatorContent(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// =============================================================================
// Edge Case Tests
// =============================================================================

func TestUnicodeInPaths(t *testing.T) {
	// Test that unicode characters are handled correctly
	segments, err := ParsePath("database.名前.value")
	if err != nil {
		t.Fatalf("ParsePath with unicode failed: %v", err)
	}

	if len(segments) != 3 {
		t.Errorf("Expected 3 segments, got %d", len(segments))
	}

	if segments[1].Key != "名前" {
		t.Errorf("Unicode key not preserved: got %q", segments[1].Key)
	}
}

func TestPathSegmentTypeString(t *testing.T) {
	tests := []struct {
		segType  PathSegmentType
		expected string
	}{
		{PathSegmentField, "Field"},
		{PathSegmentIndex, "Index"},
		{PathSegmentKeyMatch, "KeyMatch"},
		{PathSegmentType(99), "Unknown"},
	}

	for _, tt := range tests {
		if got := tt.segType.String(); got != tt.expected {
			t.Errorf("PathSegmentType(%d).String() = %q, want %q", tt.segType, got, tt.expected)
		}
	}
}

func TestToIntOverflow(t *testing.T) {
	// Test uint64 overflow to int
	var bigUint uint64 = 1<<63 + 1
	_, err := ToInt(bigUint)
	if err == nil {
		t.Error("ToInt should error on uint64 overflow")
	}

	// Test int64 (this depends on platform)
	var bigInt64 int64 = 1 << 62
	_, err = ToInt(bigInt64)
	// This may or may not error depending on platform
	// Just ensure it doesn't panic
	_ = err
}

func TestDeepGetNilHandling(t *testing.T) {
	var nilMap map[string]interface{}
	_, err := DeepGet(nilMap, "key")
	if err == nil {
		t.Error("DeepGet on nil should error")
	}

	_, err = DeepGet(nil, "key")
	if err == nil {
		t.Error("DeepGet on nil should error")
	}
}

// =============================================================================
// Benchmark Tests
// =============================================================================

func BenchmarkParsePath(b *testing.B) {
	paths := []string{
		"database.host",
		"items[0].name",
		"a.b.c.d.e.f.g.h",
		"items[name=foo].nested.value",
	}

	for _, path := range paths {
		b.Run(path, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_, _ = ParsePath(path)
			}
		})
	}
}

func BenchmarkDeepGet(b *testing.B) {
	data := map[string]interface{}{
		"a": map[string]interface{}{
			"b": map[string]interface{}{
				"c": map[string]interface{}{
					"d": "value",
				},
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = DeepGet(data, "a.b.c.d")
	}
}

func BenchmarkDeepCopy(b *testing.B) {
	data := map[string]interface{}{
		"a": 1,
		"b": []interface{}{1, 2, 3},
		"c": map[string]interface{}{
			"d": "value",
			"e": []interface{}{4, 5, 6},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = DeepCopy(data)
	}
}

func BenchmarkToString(b *testing.B) {
	values := []interface{}{
		"already a string",
		42,
		3.14159,
		true,
		[]int{1, 2, 3},
	}

	for i := 0; i < b.N; i++ {
		for _, v := range values {
			_ = ToString(v)
		}
	}
}

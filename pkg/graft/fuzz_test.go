// Package graft provides fuzz tests for core components.
package graft

import (
	"testing"
)

// FuzzParseYAML tests YAML parsing with random input.
func FuzzParseYAML(f *testing.F) {
	// Add seed corpus
	seeds := []string{
		"key: value",
		"number: 123",
		"float: 1.23",
		"bool: true",
		"null_val: null",
		"list:\n  - item1\n  - item2",
		"nested:\n  key: value\n  nested2:\n    deep: value",
		"operator: (( grab foo.bar ))",
		"concat: (( concat a \" \" b ))",
		"complex:\n  value: (( grab x ))\n  list:\n    - (( grab y ))",
		"",
		"# comment only",
		"---",
		"---\nkey: value\n---\nkey2: value2",
		"multi_doc: |\n  line1\n  line2\n  line3",
		"folded: >\n  this is\n  folded text",
		"anchor: &anchor\n  key: value\nalias: *anchor",
		"tagged: !custom value",
		"explicit_string: \"123\"",
		"single_quote: 'value'",
		"special: \"line\\nbreak\"",
		"unicode: \"café ☕\"",
		"{json: style}",
		"[1, 2, 3]",
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		engine, err := NewEngine()
		if err != nil {
			t.Skip("Failed to create engine")
		}

		// ParseYAML should never panic
		_, _ = engine.ParseYAML([]byte(input))
	})
}

// FuzzParsePath tests path parsing with random input.
func FuzzParsePath(f *testing.F) {
	// Add seed corpus
	seeds := []string{
		"foo",
		"foo.bar",
		"foo.bar.baz",
		"foo[0]",
		"foo[0].bar[1]",
		"foo.bar[0].baz",
		"[0]",
		"[0][1]",
		"",
		".",
		"..",
		"...",
		"foo.",
		".foo",
		"foo..bar",
		"[",
		"]",
		"[]",
		"foo[]",
		"foo[abc]",
		"foo[-1]",
		"foo[999999999999999999999]",
		"very.long.path.segment.here.a.b.c.d.e.f.g.h.i.j.k.l.m.n.o.p",
		"key-with-dashes.and-more",
		"key_with_underscores_here",
		"MixedCase.Path",
		"123.456",
		"$env.var",
		"special!@#$%chars",
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		// Path functions should never panic
		_, _ = ParsePath(input)
		_ = SplitPath(input)
		_ = JoinPath(input, "suffix")
		_ = ParentPath(input)
		_ = BaseName(input)
	})
}

// FuzzDeepOperations tests deep get/set operations with random paths.
func FuzzDeepOperations(f *testing.F) {
	// Add seed corpus
	seeds := []string{
		"key",
		"nested.key",
		"array[0]",
		"deep.nested.array[0].value",
		"",
		"[0][1][2]",
		"special.chars-here_and.more",
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, path string) {
		data := map[string]interface{}{
			"key": "value",
			"nested": map[string]interface{}{
				"key": "nested_value",
				"array": []interface{}{
					map[string]interface{}{"value": "array_value"},
				},
			},
			"array": []interface{}{1, 2, 3},
		}

		// DeepGet should never panic
		_, _ = DeepGet(data, path)

		// DeepSet should never panic
		copied := DeepCopy(data)
		_ = DeepSet(copied, path, "new_value")

		// DeepDelete should never panic
		copied = DeepCopy(data)
		_ = DeepDelete(copied, path)
	})
}

// FuzzExtractOperator tests operator extraction with random input.
func FuzzExtractOperator(f *testing.F) {
	// Add seed corpus
	seeds := []string{
		"(( grab foo ))",
		"(( concat a b ))",
		"regular string",
		"(( unclosed",
		"unclosed ))",
		"))((",
		"",
		"((()))",
		"(( (( nested )) ))",
		"mixed (( grab x )) text",
		"\"(( not operator ))\"",
		"(( ))",
		"((x))",
		"((\tx\t))",
		"((\n))",
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		// Should never panic
		_ = ContainsOperator(input)
		_, _ = ExtractOperatorContent(input)
	})
}

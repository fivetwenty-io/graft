package interfaces

import (
	"errors"
	"testing"
)

func TestPreScan_BasicExtraction(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		expected []string // Expected raw contents
	}{
		{
			name:     "simple grab",
			source:   "(( grab foo ))",
			expected: []string{" grab foo "},
		},
		{
			name:     "simple grab no spaces",
			source:   "((grab foo))",
			expected: []string{"grab foo"},
		},
		{
			name:     "grab with path",
			source:   "(( grab meta.name ))",
			expected: []string{" grab meta.name "},
		},
		{
			name:     "concat operator",
			source:   `(( concat "prefix-" name ))`,
			expected: []string{` concat "prefix-" name `},
		},
		{
			name:     "empty content",
			source:   "(())",
			expected: []string{""},
		},
		{
			name:     "no operators",
			source:   "just plain text",
			expected: []string{},
		},
		{
			name:     "empty source",
			source:   "",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			locations, err := PreScan(tt.source)
			if err != nil {
				t.Fatalf("PreScan returned error: %v", err)
			}

			contents := ExtractOperatorContents(locations)
			if len(contents) != len(tt.expected) {
				t.Fatalf("expected %d operators, got %d", len(tt.expected), len(contents))
			}

			for i, expected := range tt.expected {
				if contents[i] != expected {
					t.Errorf("operator %d: expected %q, got %q", i, expected, contents[i])
				}
			}
		})
	}
}

func TestPreScan_MultipleExpressions(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		expected []string
	}{
		{
			name:     "two operators same line",
			source:   "(( grab a )) and (( grab b ))",
			expected: []string{" grab a ", " grab b "},
		},
		{
			name:     "three operators",
			source:   "(( a )) (( b )) (( c ))",
			expected: []string{" a ", " b ", " c "},
		},
		{
			name:     "operators in yaml structure",
			source:   "key1: (( grab a ))\nkey2: (( grab b ))",
			expected: []string{" grab a ", " grab b "},
		},
		{
			name: "operators in multiline yaml",
			source: `name: (( grab meta.name ))
version: (( grab meta.version ))
description: (( concat "App: " name ))`,
			expected: []string{" grab meta.name ", " grab meta.version ", ` concat "App: " name `},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			locations, err := PreScan(tt.source)
			if err != nil {
				t.Fatalf("PreScan returned error: %v", err)
			}

			contents := ExtractOperatorContents(locations)
			if len(contents) != len(tt.expected) {
				t.Fatalf("expected %d operators, got %d", len(tt.expected), len(contents))
			}

			for i, expected := range tt.expected {
				if contents[i] != expected {
					t.Errorf("operator %d: expected %q, got %q", i, expected, contents[i])
				}
			}
		})
	}
}

func TestPreScan_NestedParentheses(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		expected []string
	}{
		{
			name:     "array index",
			source:   "(( grab foo.bar[0] ))",
			expected: []string{" grab foo.bar[0] "},
		},
		{
			name:     "function call simple",
			source:   "(( concat(a, b) ))",
			expected: []string{" concat(a, b) "},
		},
		{
			name:     "function call with strings",
			source:   `(( concat("a", "b") ))`,
			expected: []string{` concat("a", "b") `},
		},
		{
			name:     "nested function calls",
			source:   "(( concat(grab(a), grab(b)) ))",
			expected: []string{" concat(grab(a), grab(b)) "},
		},
		{
			name:     "deeply nested parens",
			source:   "(( a(b(c(d))) ))",
			expected: []string{" a(b(c(d))) "},
		},
		{
			name:     "mixed brackets and parens",
			source:   "(( foo[bar(0)] ))",
			expected: []string{" foo[bar(0)] "},
		},
		{
			name:     "ternary expression",
			source:   "(( condition ? (a) : (b) ))",
			expected: []string{" condition ? (a) : (b) "},
		},
		{
			name:     "arithmetic in parens",
			source:   "(( (1 + 2) * (3 + 4) ))",
			expected: []string{" (1 + 2) * (3 + 4) "},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			locations, err := PreScan(tt.source)
			if err != nil {
				t.Fatalf("PreScan returned error: %v", err)
			}

			contents := ExtractOperatorContents(locations)
			if len(contents) != len(tt.expected) {
				t.Fatalf("expected %d operators, got %d", len(tt.expected), len(contents))
			}

			for i, expected := range tt.expected {
				if contents[i] != expected {
					t.Errorf("operator %d: expected %q, got %q", i, expected, contents[i])
				}
			}
		})
	}
}

func TestPreScan_StringsContainingDelimiters(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		expected []string
	}{
		{
			name:     "string with (( inside",
			source:   `(( "text with (( inside" ))`,
			expected: []string{` "text with (( inside" `},
		},
		{
			name:     "string with )) inside",
			source:   `(( "text with )) inside" ))`,
			expected: []string{` "text with )) inside" `},
		},
		{
			name:     "string with both delimiters",
			source:   `(( "text (( with )) both" ))`,
			expected: []string{` "text (( with )) both" `},
		},
		{
			name:     "multiple strings with delimiters",
			source:   `(( concat("((", "))") ))`,
			expected: []string{` concat("((", "))") `},
		},
		{
			name:     "raw string with ((",
			source:   `(( 'raw (( string' ))`,
			expected: []string{` 'raw (( string' `},
		},
		{
			name:     "raw string with ))",
			source:   `(( 'raw )) string' ))`,
			expected: []string{` 'raw )) string' `},
		},
		{
			name:     "mixed quoted strings",
			source:   `(( concat("((", '))', "test") ))`,
			expected: []string{` concat("((", '))', "test") `},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			locations, err := PreScan(tt.source)
			if err != nil {
				t.Fatalf("PreScan returned error: %v", err)
			}

			contents := ExtractOperatorContents(locations)
			if len(contents) != len(tt.expected) {
				t.Fatalf("expected %d operators, got %d", len(tt.expected), len(contents))
			}

			for i, expected := range tt.expected {
				if contents[i] != expected {
					t.Errorf("operator %d: expected %q, got %q", i, expected, contents[i])
				}
			}
		})
	}
}

func TestPreScan_EscapedQuotes(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		expected []string
	}{
		{
			name:     "escaped quote in string",
			source:   `(( "say \"hello\"" ))`,
			expected: []string{` "say \"hello\"" `},
		},
		{
			name:     "escaped backslash",
			source:   `(( "path\\to\\file" ))`,
			expected: []string{` "path\\to\\file" `},
		},
		{
			name:     "escaped quote before ))",
			source:   `(( "test\"" ))`,
			expected: []string{` "test\"" `},
		},
		{
			name:     "complex escapes",
			source:   `(( "a\"b\\c\"d" ))`,
			expected: []string{` "a\"b\\c\"d" `},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			locations, err := PreScan(tt.source)
			if err != nil {
				t.Fatalf("PreScan returned error: %v", err)
			}

			contents := ExtractOperatorContents(locations)
			if len(contents) != len(tt.expected) {
				t.Fatalf("expected %d operators, got %d", len(tt.expected), len(contents))
			}

			for i, expected := range tt.expected {
				if contents[i] != expected {
					t.Errorf("operator %d: expected %q, got %q", i, expected, contents[i])
				}
			}
		})
	}
}

func TestPreScan_MultilineExpressions(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		expected []string
	}{
		{
			name: "simple multiline",
			source: `((
  grab foo
))`,
			expected: []string{"\n  grab foo\n"},
		},
		{
			name: "multiline with multiple args",
			source: `((
  concat
    "hello"
    "world"
))`,
			expected: []string{"\n  concat\n    \"hello\"\n    \"world\"\n"},
		},
		{
			name: "multiline ternary",
			source: `((
  condition
  ? "yes"
  : "no"
))`,
			expected: []string{"\n  condition\n  ? \"yes\"\n  : \"no\"\n"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			locations, err := PreScan(tt.source)
			if err != nil {
				t.Fatalf("PreScan returned error: %v", err)
			}

			contents := ExtractOperatorContents(locations)
			if len(contents) != len(tt.expected) {
				t.Fatalf("expected %d operators, got %d", len(tt.expected), len(contents))
			}

			for i, expected := range tt.expected {
				if contents[i] != expected {
					t.Errorf("operator %d: expected %q, got %q", i, expected, contents[i])
				}
			}
		})
	}
}

func TestPreScan_ErrorCases(t *testing.T) {
	tests := []struct {
		name        string
		source      string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "unbalanced - missing close",
			source:      "(( grab foo",
			expectError: true,
			errorMsg:    "unterminated operator expression",
		},
		{
			name:        "unbalanced - single paren",
			source:      "( not an operator",
			expectError: false, // Single ( is not an operator start
		},
		{
			name:        "unclosed string in operator",
			source:      `(( "unclosed string ))`,
			expectError: true,
			errorMsg:    "unterminated string",
		},
		{
			name:        "unclosed raw string in operator",
			source:      `(( 'unclosed raw string ))`,
			expectError: true,
			errorMsg:    "unterminated raw string",
		},
		{
			name:        "nested operator start but closed properly",
			source:      `(( "contains ((" ))`,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := PreScan(tt.source)
			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errorMsg)
				}
				if tt.errorMsg != "" && !containsSubstring(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestPreScan_PositionTracking(t *testing.T) {
	source := "key: (( grab foo ))"
	locations, err := PreScan(source)
	if err != nil {
		t.Fatalf("PreScan returned error: %v", err)
	}

	if len(locations) != 1 {
		t.Fatalf("expected 1 operator, got %d", len(locations))
	}

	loc := locations[0]

	// Check start position
	if loc.Start.Line != 1 {
		t.Errorf("Start.Line: expected 1, got %d", loc.Start.Line)
	}
	if loc.Start.Column != 6 { // 0-indexed would be 5, 1-indexed is 6
		t.Errorf("Start.Column: expected 6, got %d", loc.Start.Column)
	}
	if loc.Start.Offset != 5 {
		t.Errorf("Start.Offset: expected 5, got %d", loc.Start.Offset)
	}

	// Check end position
	if loc.End.Line != 1 {
		t.Errorf("End.Line: expected 1, got %d", loc.End.Line)
	}
	if loc.End.Offset != 19 { // After the closing ))
		t.Errorf("End.Offset: expected 19, got %d", loc.End.Offset)
	}
}

func TestPreScan_MultilinePositionTracking(t *testing.T) {
	source := `line1
key: ((
  grab foo
))`

	locations, err := PreScan(source)
	if err != nil {
		t.Fatalf("PreScan returned error: %v", err)
	}

	if len(locations) != 1 {
		t.Fatalf("expected 1 operator, got %d", len(locations))
	}

	loc := locations[0]

	// Check start position (line 2)
	if loc.Start.Line != 2 {
		t.Errorf("Start.Line: expected 2, got %d", loc.Start.Line)
	}

	// Check end position (line 4)
	if loc.End.Line != 4 {
		t.Errorf("End.Line: expected 4, got %d", loc.End.Line)
	}
}

func TestPreScan_FileInformation(t *testing.T) {
	source := "(( grab foo ))"
	filename := "test.yml"

	locations, err := PreScanWithFile(source, filename)
	if err != nil {
		t.Fatalf("PreScanWithFile returned error: %v", err)
	}

	if len(locations) != 1 {
		t.Fatalf("expected 1 operator, got %d", len(locations))
	}

	loc := locations[0]
	if loc.Start.File != filename {
		t.Errorf("Start.File: expected %q, got %q", filename, loc.Start.File)
	}
	if loc.End.File != filename {
		t.Errorf("End.File: expected %q, got %q", filename, loc.End.File)
	}
}

func TestOperatorLocation_Methods(t *testing.T) {
	loc := OperatorLocation{
		Start:      NewPosition(1, 1, 0),
		End:        NewPosition(1, 17, 16),
		RawContent: " grab foo ",
	}

	// Test String()
	str := loc.String()
	if str == "" {
		t.Error("String() returned empty string")
	}

	// Test Range()
	r := loc.Range()
	if r.Start != loc.Start || r.End != loc.End {
		t.Error("Range() returned incorrect range")
	}

	// Test FullContent()
	full := loc.FullContent()
	expected := "(( grab foo ))"
	if full != expected {
		t.Errorf("FullContent(): expected %q, got %q", expected, full)
	}
}

func TestPreScanError(t *testing.T) {
	source := "(( unclosed"
	_, err := PreScan(source)
	if err == nil {
		t.Fatal("expected error for unclosed operator")
	}

	// Check it's a PreScanError
	var psErr *PreScanError
	if !errors.As(err, &psErr) {
		t.Fatalf("expected *PreScanError, got %T", err)
	}

	if psErr.Position.IsZero() {
		t.Error("error position should not be zero")
	}

	errStr := err.Error()
	if errStr == "" {
		t.Error("error string should not be empty")
	}
}

func TestPreScanHelperFunctions(t *testing.T) {
	t.Run("TrimOperatorContent", func(t *testing.T) {
		tests := []struct {
			input    string
			expected string
		}{
			{" grab foo ", "grab foo"},
			{"  concat a b  ", "concat a b"},
			{"no-spaces", "no-spaces"},
			{"  ", ""},
		}

		for _, tt := range tests {
			result := TrimOperatorContent(tt.input)
			if result != tt.expected {
				t.Errorf("TrimOperatorContent(%q): expected %q, got %q", tt.input, tt.expected, result)
			}
		}
	})

	t.Run("CountOperators", func(t *testing.T) {
		tests := []struct {
			source   string
			expected int
		}{
			{"(( a ))", 1},
			{"(( a )) (( b ))", 2},
			{"no operators", 0},
			{"(( a )) text (( b )) more (( c ))", 3},
		}

		for _, tt := range tests {
			count, err := CountOperators(tt.source)
			if err != nil {
				t.Fatalf("CountOperators(%q): unexpected error: %v", tt.source, err)
			}
			if count != tt.expected {
				t.Errorf("CountOperators(%q): expected %d, got %d", tt.source, tt.expected, count)
			}
		}
	})

	t.Run("HasOperators", func(t *testing.T) {
		tests := []struct {
			source   string
			expected bool
		}{
			{"(( grab foo ))", true},
			{"no operators here", false},
			{"", false},
			{"(( a )) and (( b ))", true},
		}

		for _, tt := range tests {
			result := HasOperators(tt.source)
			if result != tt.expected {
				t.Errorf("HasOperators(%q): expected %v, got %v", tt.source, tt.expected, result)
			}
		}
	})
}

func TestPreScan_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		expected []string
	}{
		{
			name:     "consecutive operators no space",
			source:   "(())(())",
			expected: []string{"", ""},
		},
		{
			name:     "operator at start of file",
			source:   "(( grab foo ))",
			expected: []string{" grab foo "},
		},
		{
			name:     "operator at end of file",
			source:   "prefix (( grab foo ))",
			expected: []string{" grab foo "},
		},
		{
			name:     "single paren not operator",
			source:   "( not an operator )",
			expected: []string{},
		},
		{
			name:     "mixed single and double parens",
			source:   "(single) (( double )) (single)",
			expected: []string{" double "},
		},
		{
			name:     "operator with only whitespace",
			source:   "((   ))",
			expected: []string{"   "},
		},
		{
			name:     "operator with tabs",
			source:   "((\tgrab\tfoo\t))",
			expected: []string{"\tgrab\tfoo\t"},
		},
		{
			name:     "operator with pipe",
			source:   "(( grab foo || default ))",
			expected: []string{" grab foo || default "},
		},
		{
			name:     "complex real-world example",
			source:   `networks: (( static_ips(0, 1, 2) || grab defaults.networks ))`,
			expected: []string{" static_ips(0, 1, 2) || grab defaults.networks "},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			locations, err := PreScan(tt.source)
			if err != nil {
				t.Fatalf("PreScan returned error: %v", err)
			}

			contents := ExtractOperatorContents(locations)
			if len(contents) != len(tt.expected) {
				t.Fatalf("expected %d operators, got %d", len(tt.expected), len(contents))
			}

			for i, expected := range tt.expected {
				if contents[i] != expected {
					t.Errorf("operator %d: expected %q, got %q", i, expected, contents[i])
				}
			}
		})
	}
}

func TestPreScan_RealWorldYAML(t *testing.T) {
	source := `---
meta:
  name: (( grab params.name ))
  version: (( grab params.version || "1.0.0" ))

instance_groups:
  - name: (( concat meta.name "-web" ))
    instances: (( grab params.web_instances || 2 ))
    networks:
      - name: (( grab params.network ))
        static_ips: (( static_ips(0, 1, 2) ))

properties:
  description: (( concat "Application: " meta.name " version " meta.version ))
  config: ((
    grab params.config
  ))
`

	locations, err := PreScan(source)
	if err != nil {
		t.Fatalf("PreScan returned error: %v", err)
	}

	// Should find multiple operators
	if len(locations) < 5 {
		t.Errorf("expected at least 5 operators, got %d", len(locations))
	}

	// Check that all locations have valid positions
	for i, loc := range locations {
		if loc.Start.IsZero() {
			t.Errorf("operator %d: Start position is zero", i)
		}
		if loc.End.IsZero() {
			t.Errorf("operator %d: End position is zero", i)
		}
		if loc.RawContent == "" && i > 0 { // Empty content is valid but unusual
			t.Logf("operator %d: has empty content (unusual but valid)", i)
		}
	}
}

// Helper function to check if a string contains a substring.
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || s != "" && containsSubstringHelper(s, substr))
}

func containsSubstringHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestPreScanner_Reset(t *testing.T) {
	ps := NewPreScanner("(( first ))")
	locations1, err := ps.Scan()
	if err != nil {
		t.Fatalf("first scan error: %v", err)
	}
	if len(locations1) != 1 {
		t.Fatalf("expected 1 operator in first scan, got %d", len(locations1))
	}

	// Create new prescanner with different source
	ps2 := NewPreScanner("(( second )) (( third ))")
	locations2, err := ps2.Scan()
	if err != nil {
		t.Fatalf("second scan error: %v", err)
	}
	if len(locations2) != 2 {
		t.Fatalf("expected 2 operators in second scan, got %d", len(locations2))
	}
}

func BenchmarkPreScan(b *testing.B) {
	source := `---
meta:
  name: (( grab params.name ))
  version: (( grab params.version || "1.0.0" ))
instance_groups:
  - name: (( concat meta.name "-web" ))
    instances: (( grab params.web_instances || 2 ))
`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := PreScan(source)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPreScan_LargeFile(b *testing.B) {
	// Simulate a larger file with many operators
	var builder string
	for i := 0; i < 100; i++ {
		builder += `key` + string(rune('0'+i%10)) + `: (( grab params.value` + string(rune('0'+i%10)) + ` ))
`
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := PreScan(builder)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// ============================================================================
// Control Flow Block Detection Tests
// ============================================================================

func TestControlFlowType_String(t *testing.T) {
	tests := []struct {
		cfType   ControlFlowType
		expected string
	}{
		{ControlFlowIf, "if"},
		{ControlFlowFor, "for"},
		{ControlFlowWhile, "while"},
		{ControlFlowCase, "case"},
		{ControlFlowType(99), "ControlFlowType(99)"},
	}

	for _, tt := range tests {
		result := tt.cfType.String()
		if result != tt.expected {
			t.Errorf("ControlFlowType(%d).String(): expected %q, got %q", tt.cfType, tt.expected, result)
		}
	}
}

func TestIsControlFlowStart(t *testing.T) {
	tests := []struct {
		content  string
		expected ControlFlowType
		isStart  bool
	}{
		{" if x > 0 ", ControlFlowIf, true},
		{" for i in list ", ControlFlowFor, true},
		{" while running ", ControlFlowWhile, true},
		{" case value ", ControlFlowCase, true},
		{" grab foo ", 0, false},
		{" fi ", 0, false},
		{" done ", 0, false},
		{"", 0, false},
	}

	for _, tt := range tests {
		cfType, isStart := IsControlFlowStart(tt.content)
		if isStart != tt.isStart {
			t.Errorf("IsControlFlowStart(%q): expected isStart=%v, got %v", tt.content, tt.isStart, isStart)
		}
		if isStart && cfType != tt.expected {
			t.Errorf("IsControlFlowStart(%q): expected type=%v, got %v", tt.content, tt.expected, cfType)
		}
	}
}

func TestIsControlFlowEnd(t *testing.T) {
	tests := []struct {
		content  string
		expected ControlFlowType
		isEnd    bool
	}{
		{" fi ", ControlFlowIf, true},
		{" endif ", ControlFlowIf, true},
		{" done ", ControlFlowFor, true}, // done can end both for and while
		{" endfor ", ControlFlowFor, true},
		{" endwhile ", ControlFlowFor, true}, // normalized to done
		{" esac ", ControlFlowCase, true},
		{" endcase ", ControlFlowCase, true},
		{" if x > 0 ", 0, false},
		{" grab foo ", 0, false},
		{"", 0, false},
	}

	for _, tt := range tests {
		cfType, isEnd := IsControlFlowEnd(tt.content)
		if isEnd != tt.isEnd {
			t.Errorf("IsControlFlowEnd(%q): expected isEnd=%v, got %v", tt.content, tt.isEnd, isEnd)
		}
		if isEnd && cfType != tt.expected {
			t.Errorf("IsControlFlowEnd(%q): expected type=%v, got %v", tt.content, tt.expected, cfType)
		}
	}
}

func TestIsControlFlowMiddle(t *testing.T) {
	tests := []struct {
		content    string
		normalized string
		isMiddle   bool
	}{
		{" elif x < 0 ", "elif", true},
		{" elsif x < 0 ", "elif", true}, // alias
		{" else ", "else", true},
		{" when 1 ", "when", true},
		{" default ", "default", true},
		{" if x > 0 ", "", false},
		{" fi ", "", false},
		{"", "", false},
	}

	for _, tt := range tests {
		normalized, isMiddle := IsControlFlowMiddle(tt.content)
		if isMiddle != tt.isMiddle {
			t.Errorf("IsControlFlowMiddle(%q): expected isMiddle=%v, got %v", tt.content, tt.isMiddle, isMiddle)
		}
		if isMiddle && normalized != tt.normalized {
			t.Errorf("IsControlFlowMiddle(%q): expected normalized=%q, got %q", tt.content, tt.normalized, normalized)
		}
	}
}

func TestDetectControlFlowBlocks_SimpleIf(t *testing.T) {
	source := `key: (( if condition ))
value: (( grab foo ))
(( fi ))`

	locations, err := PreScan(source)
	if err != nil {
		t.Fatalf("PreScan error: %v", err)
	}

	blocks, err := DetectControlFlowBlocks(locations)
	if err != nil {
		t.Fatalf("DetectControlFlowBlocks error: %v", err)
	}

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}

	block := blocks[0]
	if block.Type != ControlFlowIf {
		t.Errorf("expected if block, got %v", block.Type)
	}
	if !block.IsComplete() {
		t.Error("block should be complete")
	}
}

func TestDetectControlFlowBlocks_IfElse(t *testing.T) {
	source := `(( if condition ))
(( grab a ))
(( else ))
(( grab b ))
(( fi ))`

	locations, err := PreScan(source)
	if err != nil {
		t.Fatalf("PreScan error: %v", err)
	}

	blocks, err := DetectControlFlowBlocks(locations)
	if err != nil {
		t.Fatalf("DetectControlFlowBlocks error: %v", err)
	}

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}

	block := blocks[0]
	if block.ElseLocation == nil {
		t.Error("expected else location to be set")
	}
}

func TestDetectControlFlowBlocks_IfElifElse(t *testing.T) {
	source := `(( if x > 0 ))
(( grab a ))
(( elif x < 0 ))
(( grab b ))
(( elif x == 0 ))
(( grab c ))
(( else ))
(( grab d ))
(( fi ))`

	locations, err := PreScan(source)
	if err != nil {
		t.Fatalf("PreScan error: %v", err)
	}

	blocks, err := DetectControlFlowBlocks(locations)
	if err != nil {
		t.Fatalf("DetectControlFlowBlocks error: %v", err)
	}

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}

	block := blocks[0]
	if len(block.ElseIfLocations) != 2 {
		t.Errorf("expected 2 elif locations, got %d", len(block.ElseIfLocations))
	}
	if block.ElseLocation == nil {
		t.Error("expected else location to be set")
	}
}

func TestDetectControlFlowBlocks_ForLoop(t *testing.T) {
	source := `(( for item in list ))
(( grab item.name ))
(( done ))`

	locations, err := PreScan(source)
	if err != nil {
		t.Fatalf("PreScan error: %v", err)
	}

	blocks, err := DetectControlFlowBlocks(locations)
	if err != nil {
		t.Fatalf("DetectControlFlowBlocks error: %v", err)
	}

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}

	block := blocks[0]
	if block.Type != ControlFlowFor {
		t.Errorf("expected for block, got %v", block.Type)
	}
}

func TestDetectControlFlowBlocks_CaseWhen(t *testing.T) {
	source := `(( case value ))
(( when 1 ))
(( grab a ))
(( when 2 ))
(( grab b ))
(( default ))
(( grab c ))
(( esac ))`

	locations, err := PreScan(source)
	if err != nil {
		t.Fatalf("PreScan error: %v", err)
	}

	blocks, err := DetectControlFlowBlocks(locations)
	if err != nil {
		t.Fatalf("DetectControlFlowBlocks error: %v", err)
	}

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}

	block := blocks[0]
	if block.Type != ControlFlowCase {
		t.Errorf("expected case block, got %v", block.Type)
	}
	if len(block.WhenLocations) != 2 {
		t.Errorf("expected 2 when locations, got %d", len(block.WhenLocations))
	}
	if block.DefaultLocation == nil {
		t.Error("expected default location to be set")
	}
}

func TestDetectControlFlowBlocks_Nested(t *testing.T) {
	source := `(( if outer ))
(( if inner ))
(( grab x ))
(( fi ))
(( fi ))`

	locations, err := PreScan(source)
	if err != nil {
		t.Fatalf("PreScan error: %v", err)
	}

	blocks, err := DetectControlFlowBlocks(locations)
	if err != nil {
		t.Fatalf("DetectControlFlowBlocks error: %v", err)
	}

	if len(blocks) != 1 {
		t.Fatalf("expected 1 top-level block, got %d", len(blocks))
	}

	outer := blocks[0]
	if len(outer.NestedBlocks) != 1 {
		t.Fatalf("expected 1 nested block, got %d", len(outer.NestedBlocks))
	}

	inner := outer.NestedBlocks[0]
	if inner.Type != ControlFlowIf {
		t.Errorf("expected nested if block, got %v", inner.Type)
	}
}

func TestDetectControlFlowBlocks_UnclosedBlock(t *testing.T) {
	source := `(( if condition ))
(( grab foo ))`

	locations, err := PreScan(source)
	if err != nil {
		t.Fatalf("PreScan error: %v", err)
	}

	_, err = DetectControlFlowBlocks(locations)
	if err == nil {
		t.Fatal("expected error for unclosed block")
	}

	var cfErr *ControlFlowError
	if !errors.As(err, &cfErr) {
		t.Fatalf("expected *ControlFlowError, got %T", err)
	}
	if cfErr.Location == nil {
		t.Error("error should have location")
	}
}

func TestDetectControlFlowBlocks_UnexpectedEnd(t *testing.T) {
	source := `(( grab foo ))
(( fi ))`

	locations, err := PreScan(source)
	if err != nil {
		t.Fatalf("PreScan error: %v", err)
	}

	_, err = DetectControlFlowBlocks(locations)
	if err == nil {
		t.Fatal("expected error for unexpected end")
	}
}

func TestDetectControlFlowBlocks_MismatchedEnd(t *testing.T) {
	source := `(( if condition ))
(( grab foo ))
(( esac ))`

	locations, err := PreScan(source)
	if err != nil {
		t.Fatalf("PreScan error: %v", err)
	}

	_, err = DetectControlFlowBlocks(locations)
	if err == nil {
		t.Fatal("expected error for mismatched end")
	}
}

func TestControlFlowBlock_Methods(t *testing.T) {
	t.Run("BlockRange", func(t *testing.T) {
		block := &ControlFlowBlock{
			StartLocation: &OperatorLocation{
				Start: NewPosition(1, 1, 0),
				End:   NewPosition(1, 10, 9),
			},
			EndLocation: &OperatorLocation{
				Start: NewPosition(3, 1, 20),
				End:   NewPosition(3, 5, 24),
			},
		}

		r := block.BlockRange()
		if r.Start.Line != 1 || r.End.Line != 3 {
			t.Errorf("unexpected range: %v", r)
		}
	})

	t.Run("BlockRange nil", func(t *testing.T) {
		var block *ControlFlowBlock
		r := block.BlockRange()
		if !r.IsZero() {
			t.Error("expected zero range for nil block")
		}
	})

	t.Run("IsComplete", func(t *testing.T) {
		block := &ControlFlowBlock{
			StartLocation: &OperatorLocation{},
			EndLocation:   &OperatorLocation{},
		}
		if !block.IsComplete() {
			t.Error("block should be complete")
		}

		block.EndLocation = nil
		if block.IsComplete() {
			t.Error("block without end should not be complete")
		}
	})

	t.Run("String", func(t *testing.T) {
		block := &ControlFlowBlock{
			Type:          ControlFlowIf,
			StartLocation: &OperatorLocation{},
			EndLocation:   &OperatorLocation{},
		}
		str := block.String()
		if str == "" {
			t.Error("String() should not be empty")
		}

		var nilBlock *ControlFlowBlock
		nilStr := nilBlock.String()
		if nilStr == "" {
			t.Error("String() for nil should not be empty")
		}
	})
}

func TestValidateControlFlowBlocks(t *testing.T) {
	t.Run("valid blocks", func(t *testing.T) {
		blocks := []*ControlFlowBlock{
			{
				Type:          ControlFlowIf,
				StartLocation: &OperatorLocation{Start: NewPosition(1, 1, 0)},
				EndLocation:   &OperatorLocation{Start: NewPosition(5, 1, 50)},
				ElseIfLocations: []*OperatorLocation{
					{Start: NewPosition(2, 1, 10)},
				},
				ElseLocation: &OperatorLocation{Start: NewPosition(3, 1, 20)},
			},
		}

		err := ValidateControlFlowBlocks(blocks)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("else before elif", func(t *testing.T) {
		blocks := []*ControlFlowBlock{
			{
				Type:          ControlFlowIf,
				StartLocation: &OperatorLocation{Start: NewPosition(1, 1, 0)},
				EndLocation:   &OperatorLocation{Start: NewPosition(5, 1, 50)},
				ElseIfLocations: []*OperatorLocation{
					{Start: NewPosition(3, 1, 30)}, // elif after else
				},
				ElseLocation: &OperatorLocation{Start: NewPosition(2, 1, 10)}, // else before elif
			},
		}

		err := ValidateControlFlowBlocks(blocks)
		if err == nil {
			t.Error("expected error for else before elif")
		}
	})
}

func TestFindBlockContaining(t *testing.T) {
	source := `(( if outer ))
(( if inner ))
(( grab x ))
(( fi ))
(( fi ))`

	locations, err := PreScan(source)
	if err != nil {
		t.Fatalf("PreScan error: %v", err)
	}

	blocks, err := DetectControlFlowBlocks(locations)
	if err != nil {
		t.Fatalf("DetectControlFlowBlocks error: %v", err)
	}

	// Find block at line 3 (inside inner)
	found := FindBlockContaining(blocks, 3, 1)
	if found == nil {
		t.Fatal("expected to find a block")
	}
	// Should find the inner block
	if len(found.NestedBlocks) != 0 {
		t.Error("expected innermost block (no nested blocks)")
	}

	// Find block at line 1 (outer but not inner)
	found = FindBlockContaining(blocks, 1, 5)
	if found == nil {
		t.Fatal("expected to find outer block")
	}

	// Position outside all blocks
	found = FindBlockContaining(blocks, 100, 1)
	if found != nil {
		t.Error("expected nil for position outside blocks")
	}
}

func TestGetBlockDepth(t *testing.T) {
	source := `(( if level0 ))
(( if level1 ))
(( if level2 ))
(( grab x ))
(( fi ))
(( fi ))
(( fi ))`

	locations, err := PreScan(source)
	if err != nil {
		t.Fatalf("PreScan error: %v", err)
	}

	blocks, err := DetectControlFlowBlocks(locations)
	if err != nil {
		t.Fatalf("DetectControlFlowBlocks error: %v", err)
	}

	level0 := blocks[0]
	level1 := level0.NestedBlocks[0]
	level2 := level1.NestedBlocks[0]

	if depth := GetBlockDepth(blocks, level0); depth != 0 {
		t.Errorf("expected depth 0, got %d", depth)
	}
	if depth := GetBlockDepth(blocks, level1); depth != 1 {
		t.Errorf("expected depth 1, got %d", depth)
	}
	if depth := GetBlockDepth(blocks, level2); depth != 2 {
		t.Errorf("expected depth 2, got %d", depth)
	}

	// Non-existent block
	other := &ControlFlowBlock{}
	if depth := GetBlockDepth(blocks, other); depth != -1 {
		t.Errorf("expected depth -1 for non-existent block, got %d", depth)
	}
}

func TestCountNestedBlocks(t *testing.T) {
	block := &ControlFlowBlock{
		NestedBlocks: []*ControlFlowBlock{
			{
				NestedBlocks: []*ControlFlowBlock{
					{},
					{},
				},
			},
			{},
		},
	}

	count := CountNestedBlocks(block)
	if count != 4 { // 2 at level 1, 2 at level 2
		t.Errorf("expected 4 nested blocks, got %d", count)
	}

	// nil block
	if count := CountNestedBlocks(nil); count != 0 {
		t.Errorf("expected 0 for nil block, got %d", count)
	}
}

func TestGetAllBlocks(t *testing.T) {
	blocks := []*ControlFlowBlock{
		{
			Type: ControlFlowIf,
			NestedBlocks: []*ControlFlowBlock{
				{Type: ControlFlowFor},
			},
		},
		{
			Type: ControlFlowWhile,
		},
	}

	all := GetAllBlocks(blocks)
	if len(all) != 3 {
		t.Errorf("expected 3 blocks total, got %d", len(all))
	}
}

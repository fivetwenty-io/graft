package interfaces

import (
	"testing"
)

// TestGetKeywordCategory tests the GetKeywordCategory function.
func TestGetKeywordCategory(t *testing.T) {
	t.Run("control flow keywords", func(t *testing.T) {
		controlFlow := []TokenType{
			TokenIf, TokenElif, TokenElse, TokenFi,
			TokenFor, TokenWhile, TokenDone,
			TokenCase, TokenWhen, TokenDefault, TokenEsac, TokenIn,
		}

		for _, tt := range controlFlow {
			cat := GetKeywordCategory(tt)
			if cat != CategoryControlFlow {
				t.Errorf("GetKeywordCategory(%v) = %v, want CategoryControlFlow", tt, cat)
			}
		}
	})

	t.Run("literal keywords", func(t *testing.T) {
		if GetKeywordCategory(TokenBoolean) != CategoryLiteral {
			t.Error("GetKeywordCategory(TokenBoolean) should return CategoryLiteral")
		}
		if GetKeywordCategory(TokenNull) != CategoryLiteral {
			t.Error("GetKeywordCategory(TokenNull) should return CategoryLiteral")
		}
	})

	t.Run("operator names", func(t *testing.T) {
		if GetKeywordCategory(TokenOperatorName) != CategoryOperator {
			t.Error("GetKeywordCategory(TokenOperatorName) should return CategoryOperator")
		}
	})

	t.Run("builtin keywords", func(t *testing.T) {
		if GetKeywordCategory(TokenRange) != CategoryBuiltin {
			t.Error("GetKeywordCategory(TokenRange) should return CategoryBuiltin")
		}
	})

	t.Run("non-keyword returns CategoryUnknown", func(t *testing.T) {
		nonKeywords := []TokenType{
			TokenEOF, TokenInvalid, TokenInteger, TokenString,
			TokenPlus, TokenMinus, TokenIdentifier,
		}

		for _, tt := range nonKeywords {
			cat := GetKeywordCategory(tt)
			if cat != CategoryUnknown {
				t.Errorf("GetKeywordCategory(%v) = %v, want CategoryUnknown", tt, cat)
			}
		}
	})
}

// TestIsControlFlowKeyword tests the IsControlFlowKeyword function.
func TestIsControlFlowKeyword(t *testing.T) {
	tests := []struct {
		input    TokenType
		expected bool
	}{
		{TokenIf, true},
		{TokenElif, true},
		{TokenElse, true},
		{TokenFi, true},
		{TokenFor, true},
		{TokenWhile, true},
		{TokenCase, true},
		{TokenBoolean, false},
		{TokenOperatorName, false},
		{TokenIdentifier, false},
	}

	for _, tt := range tests {
		got := IsControlFlowKeyword(tt.input)
		if got != tt.expected {
			t.Errorf("IsControlFlowKeyword(%v) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

// TestIsLogicalKeywordStr tests the IsLogicalKeywordStr function.
func TestIsLogicalKeywordStr(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"and", true},
		{"or", true},
		{"not", true},
		{"AND", true},
		{"Or", true},
		{"NOT", true},
		{"true", false},
		{"false", false},
		{"if", false},
		{"grab", false},
	}

	for _, tt := range tests {
		got := IsLogicalKeywordStr(tt.input)
		if got != tt.expected {
			t.Errorf("IsLogicalKeywordStr(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

// TestIsLiteralKeyword tests the IsLiteralKeyword function.
func TestIsLiteralKeyword(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"true", true},
		{"false", true},
		{"null", true},
		{"nil", true},
		{"TRUE", true},
		{"False", true},
		{"NULL", true},
		{"NIL", true},
		{"and", false},
		{"if", false},
		{"grab", false},
	}

	for _, tt := range tests {
		got := IsLiteralKeyword(tt.input)
		if got != tt.expected {
			t.Errorf("IsLiteralKeyword(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

// TestIsOperatorKeyword tests the IsOperatorKeyword function.
func TestIsOperatorKeyword(t *testing.T) {
	tests := []struct {
		input    TokenType
		expected bool
	}{
		{TokenOperatorName, true},
		{TokenIf, false},
		{TokenBoolean, false},
		{TokenIdentifier, false},
	}

	for _, tt := range tests {
		got := IsOperatorKeyword(tt.input)
		if got != tt.expected {
			t.Errorf("IsOperatorKeyword(%v) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

// TestIsBoolLiteral tests the IsBoolLiteral function.
func TestIsBoolLiteral(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"true", true},
		{"false", true},
		{"True", true},
		{"False", true},
		{"TRUE", true},
		{"FALSE", true},
		{"TrUe", true},
		{"FaLsE", true},
		{"null", false},
		{"nil", false},
		{"1", false},
		{"0", false},
		{"yes", false},
		{"no", false},
		{"", false},
	}

	for _, tt := range tests {
		got := IsBoolLiteral(tt.input)
		if got != tt.expected {
			t.Errorf("IsBoolLiteral(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

// TestIsNullLiteral tests the IsNullLiteral function.
func TestIsNullLiteral(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"null", true},
		{"nil", true},
		{"~", true},
		{"Null", true},
		{"Nil", true},
		{"NULL", true},
		{"NIL", true},
		{"NuLl", true},
		{"true", false},
		{"false", false},
		{"none", false},
		{"undefined", false},
		{"", false},
	}

	for _, tt := range tests {
		got := IsNullLiteral(tt.input)
		if got != tt.expected {
			t.Errorf("IsNullLiteral(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

// TestParseBoolLiteral tests the ParseBoolLiteral function.
func TestParseBoolLiteral(t *testing.T) {
	tests := []struct {
		input       string
		expectedVal bool
		expectedOk  bool
	}{
		{"true", true, true},
		{"false", false, true},
		{"True", true, true},
		{"False", false, true},
		{"TRUE", true, true},
		{"FALSE", false, true},
		{"null", false, false},
		{"nil", false, false},
		{"1", false, false},
		{"0", false, false},
		{"", false, false},
	}

	for _, tt := range tests {
		val, ok := ParseBoolLiteral(tt.input)
		if ok != tt.expectedOk {
			t.Errorf("ParseBoolLiteral(%q) ok = %v, want %v", tt.input, ok, tt.expectedOk)
		}
		if ok && val != tt.expectedVal {
			t.Errorf("ParseBoolLiteral(%q) = %v, want %v", tt.input, val, tt.expectedVal)
		}
	}
}

// TestIsKeywordIdentifier tests the IsKeywordIdentifier function.
func TestIsKeywordIdentifier(t *testing.T) {
	t.Run("control flow keywords", func(t *testing.T) {
		keywords := []string{
			"if", "elif", "else", "fi",
			"for", "in", "done",
			"while",
			"case", "when", "default", "esac",
		}

		for _, kw := range keywords {
			if !IsKeywordIdentifier(kw) {
				t.Errorf("IsKeywordIdentifier(%q) = false, want true", kw)
			}
		}
	})

	t.Run("operator names", func(t *testing.T) {
		operators := []string{
			"grab", "concat", "vault", "param",
			"join", "split", "defer", "prune",
		}

		for _, op := range operators {
			if !IsKeywordIdentifier(op) {
				t.Errorf("IsKeywordIdentifier(%q) = false, want true", op)
			}
		}
	})

	t.Run("logical keywords", func(t *testing.T) {
		logicals := []string{"and", "or", "not"}

		for _, kw := range logicals {
			if !IsKeywordIdentifier(kw) {
				t.Errorf("IsKeywordIdentifier(%q) = false, want true", kw)
			}
		}
	})

	t.Run("non-keywords", func(t *testing.T) {
		nonKeywords := []string{"foo", "bar", "myvar", "custom_name", ""}

		for _, s := range nonKeywords {
			if IsKeywordIdentifier(s) {
				t.Errorf("IsKeywordIdentifier(%q) = true, want false", s)
			}
		}
	})

	t.Run("case insensitivity", func(t *testing.T) {
		testCases := []string{
			"If", "IF", "iF",
			"GRAB", "Grab",
			"AND", "And",
		}

		for _, tc := range testCases {
			if !IsKeywordIdentifier(tc) {
				t.Errorf("IsKeywordIdentifier(%q) = false, want true (case-insensitive)", tc)
			}
		}
	})
}

// TestNormalizeKeywordAlias tests the NormalizeKeywordAlias function.
func TestNormalizeKeywordAlias(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"elsif", "elif"},
		{"endif", "fi"},
		{"endfor", "done"},
		{"endwhile", "done"},
		{"endcase", "esac"},
		{"if", "if"},
		{"else", "else"},
		{"fi", "fi"},
		{"done", "done"},
		{"esac", "esac"},
		// Case insensitivity
		{"ELSIF", "elif"},
		{"ENDIF", "fi"},
		{"EndFor", "done"},
		// Non-aliases
		{"grab", "grab"},
		{"foo", "foo"},
	}

	for _, tt := range tests {
		got := NormalizeKeywordAlias(tt.input)
		if got != tt.expected {
			t.Errorf("NormalizeKeywordAlias(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

// TestLookupKeywordWithAlias tests the LookupKeywordWithAlias function.
func TestLookupKeywordWithAlias(t *testing.T) {
	tests := []struct {
		input    string
		expected TokenType
	}{
		{"if", TokenIf},
		{"elif", TokenElif},
		{"elsif", TokenElif}, // alias
		{"fi", TokenFi},
		{"endif", TokenFi}, // alias
		{"done", TokenDone},
		{"endfor", TokenDone},   // alias
		{"endwhile", TokenDone}, // alias
		{"esac", TokenEsac},
		{"endcase", TokenEsac}, // alias
		{"unknown", TokenIdentifier},
	}

	for _, tt := range tests {
		got := LookupKeywordWithAlias(tt.input)
		if got != tt.expected {
			t.Errorf("LookupKeywordWithAlias(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

// TestIsValidPathStart tests the IsValidPathStart function.
func TestIsValidPathStart(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		// Valid path starts
		{"meta.name", true},
		{"foo.bar", true},
		{"foo[0]", true},
		{"foo", true},
		{"_private", true},
		{"var_name", true},
		{".", true},
		{".meta", true},
		{".meta.name", true},
		{".[0]", true},
		{"path-with-dash", true},

		// Invalid path starts
		{"", false},
		{"123", false},
		{"123abc", false},
		{"-dash", false},
	}

	for _, tt := range tests {
		got := IsValidPathStart(tt.input)
		if got != tt.expected {
			t.Errorf("IsValidPathStart(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

// TestIsValidPathSegment tests the IsValidPathSegment function.
func TestIsValidPathSegment(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		// Valid identifiers
		{"foo", true},
		{"bar_baz", true},
		{"_private", true},
		{"name123", true},
		{"with-dash", true},

		// Valid numeric indices
		{"0", true},
		{"1", true},
		{"42", true},
		{"123456", true},

		// Valid quoted strings
		{"\"key\"", true},
		{"'key'", true},
		{"\"key with spaces\"", true},
		{"'key-with-dashes'", true},

		// Invalid segments
		{"", false},
		{"-dash", false},
		{"123abc", false},
	}

	for _, tt := range tests {
		got := IsValidPathSegment(tt.input)
		if got != tt.expected {
			t.Errorf("IsValidPathSegment(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

// TestControlFlowKeywords tests the ControlFlowKeywords function.
func TestControlFlowKeywords(t *testing.T) {
	keywords := ControlFlowKeywords()
	if len(keywords) == 0 {
		t.Error("ControlFlowKeywords() returned empty list")
	}

	expected := map[string]bool{
		"if": true, "else": true, "elif": true, "fi": true,
		"for": true, "while": true, "case": true, "when": true,
		"done": true, "default": true, "esac": true, "in": true,
	}

	for _, kw := range keywords {
		if !expected[kw] {
			t.Errorf("ControlFlowKeywords() returned unexpected keyword: %q", kw)
		}
		delete(expected, kw)
	}

	if len(expected) > 0 {
		t.Errorf("ControlFlowKeywords() missing: %v", expected)
	}
}

// TestControlFlowAliases tests the ControlFlowAliases function.
func TestControlFlowAliases(t *testing.T) {
	aliases := ControlFlowAliases()
	if len(aliases) == 0 {
		t.Error("ControlFlowAliases() returned empty list")
	}

	expected := map[string]bool{
		"elsif": true, "endif": true, "endfor": true,
		"endwhile": true, "endcase": true,
	}

	for _, alias := range aliases {
		if !expected[alias] {
			t.Errorf("ControlFlowAliases() returned unexpected alias: %q", alias)
		}
		delete(expected, alias)
	}

	if len(expected) > 0 {
		t.Errorf("ControlFlowAliases() missing: %v", expected)
	}
}

// TestLogicalKeywordsList tests the LogicalKeywordsList function.
func TestLogicalKeywordsList(t *testing.T) {
	keywords := LogicalKeywordsList()
	if len(keywords) != 3 {
		t.Errorf("LogicalKeywordsList() returned %d keywords, want 3", len(keywords))
	}

	expected := map[string]bool{"and": true, "or": true, "not": true}

	for _, kw := range keywords {
		if !expected[kw] {
			t.Errorf("LogicalKeywordsList() returned unexpected keyword: %q", kw)
		}
		delete(expected, kw)
	}

	if len(expected) > 0 {
		t.Errorf("LogicalKeywordsList() missing: %v", expected)
	}
}

// TestLiteralKeywordsList tests the LiteralKeywordsList function.
func TestLiteralKeywordsList(t *testing.T) {
	keywords := LiteralKeywordsList()
	if len(keywords) != 4 {
		t.Errorf("LiteralKeywordsList() returned %d keywords, want 4", len(keywords))
	}

	expected := map[string]bool{"true": true, "false": true, "null": true, "nil": true}

	for _, kw := range keywords {
		if !expected[kw] {
			t.Errorf("LiteralKeywordsList() returned unexpected keyword: %q", kw)
		}
		delete(expected, kw)
	}

	if len(expected) > 0 {
		t.Errorf("LiteralKeywordsList() missing: %v", expected)
	}
}

// TestAllOperatorNames tests the AllOperatorNames function.
func TestAllOperatorNames(t *testing.T) {
	names := AllOperatorNames()
	if len(names) == 0 {
		t.Error("AllOperatorNames() returned empty list")
	}

	// Check that some expected operators are present
	expectedOps := []string{"grab", "concat", "vault", "join", "split"}
	nameSet := make(map[string]bool)
	for _, n := range names {
		nameSet[n] = true
	}

	for _, op := range expectedOps {
		if !nameSet[op] {
			t.Errorf("AllOperatorNames() missing expected operator: %q", op)
		}
	}
}

// TestGetKeywordInfo tests the GetKeywordInfo function.
func TestGetKeywordInfo(t *testing.T) {
	t.Run("control flow keyword", func(t *testing.T) {
		info := GetKeywordInfo("if")
		if info == nil {
			t.Fatal("GetKeywordInfo(\"if\") returned nil")
		}
		if info.Name != "if" {
			t.Errorf("info.Name = %q, want %q", info.Name, "if")
		}
		if info.TokenType != TokenIf {
			t.Errorf("info.TokenType = %v, want TokenIf", info.TokenType)
		}
		if info.Category != CategoryControlFlow {
			t.Errorf("info.Category = %v, want CategoryControlFlow", info.Category)
		}
	})

	t.Run("keyword with aliases", func(t *testing.T) {
		info := GetKeywordInfo("elif")
		if info == nil {
			t.Fatal("GetKeywordInfo(\"elif\") returned nil")
		}
		if len(info.Aliases) == 0 {
			t.Error("GetKeywordInfo(\"elif\") should have aliases")
		}
		hasElsif := false
		for _, alias := range info.Aliases {
			if alias == "elsif" {
				hasElsif = true
				break
			}
		}
		if !hasElsif {
			t.Error("GetKeywordInfo(\"elif\") should have \"elsif\" as alias")
		}
	})

	t.Run("alias lookup returns canonical info", func(t *testing.T) {
		info := GetKeywordInfo("elsif")
		if info == nil {
			t.Fatal("GetKeywordInfo(\"elsif\") returned nil")
		}
		if info.Name != "elif" {
			t.Errorf("info.Name = %q, want %q (canonical form)", info.Name, "elif")
		}
	})

	t.Run("operator name", func(t *testing.T) {
		info := GetKeywordInfo("grab")
		if info == nil {
			t.Fatal("GetKeywordInfo(\"grab\") returned nil")
		}
		if info.TokenType != TokenOperatorName {
			t.Errorf("info.TokenType = %v, want TokenOperatorName", info.TokenType)
		}
		if info.Category != CategoryOperator {
			t.Errorf("info.Category = %v, want CategoryOperator", info.Category)
		}
	})

	t.Run("logical keyword", func(t *testing.T) {
		info := GetKeywordInfo("and")
		if info == nil {
			t.Fatal("GetKeywordInfo(\"and\") returned nil")
		}
		if info.Category != CategoryLogical {
			t.Errorf("info.Category = %v, want CategoryLogical", info.Category)
		}
	})

	t.Run("non-keyword", func(t *testing.T) {
		info := GetKeywordInfo("foo")
		if info != nil {
			t.Error("GetKeywordInfo(\"foo\") should return nil")
		}
	})

	t.Run("case insensitive", func(t *testing.T) {
		info := GetKeywordInfo("IF")
		if info == nil {
			t.Fatal("GetKeywordInfo(\"IF\") returned nil (should be case-insensitive)")
		}
		if info.Name != "if" {
			t.Errorf("info.Name = %q, want %q", info.Name, "if")
		}
	})
}

// TestRegisterOperatorName tests dynamic operator registration.
func TestRegisterOperatorName(t *testing.T) {
	testOp := "custom_test_operator"

	// Initially should not be registered
	if IsRegisteredOperator(testOp) {
		t.Errorf("IsRegisteredOperator(%q) = true before registration", testOp)
	}

	// Register
	RegisterOperatorName(testOp)
	if !IsRegisteredOperator(testOp) {
		t.Errorf("IsRegisteredOperator(%q) = false after registration", testOp)
	}

	// Case insensitivity
	if !IsRegisteredOperator("CUSTOM_TEST_OPERATOR") {
		t.Error("IsRegisteredOperator should be case-insensitive")
	}

	// Unregister
	UnregisterOperatorName(testOp)
	if IsRegisteredOperator(testOp) {
		t.Errorf("IsRegisteredOperator(%q) = true after unregistration", testOp)
	}
}

// TestKeywordCategoryString tests the String method of KeywordCategory.
func TestKeywordCategoryString(t *testing.T) {
	tests := []struct {
		cat      KeywordCategory
		expected string
	}{
		{CategoryControlFlow, "ControlFlow"},
		{CategoryLogical, "Logical"},
		{CategoryLiteral, "Literal"},
		{CategoryOperator, "Operator"},
		{CategoryBuiltin, "Builtin"},
		{CategoryUnknown, "Unknown"},
		{KeywordCategory(100), "Unknown"},
	}

	for _, tt := range tests {
		got := tt.cat.String()
		if got != tt.expected {
			t.Errorf("KeywordCategory(%d).String() = %q, want %q", tt.cat, got, tt.expected)
		}
	}
}

// TestEdgeCases tests edge cases and boundary conditions.
func TestEdgeCases(t *testing.T) {
	t.Run("empty string", func(t *testing.T) {
		if IsKeywordIdentifier("") {
			t.Error("IsKeywordIdentifier(\"\") = true, want false")
		}

		if IsBoolLiteral("") {
			t.Error("IsBoolLiteral(\"\") = true, want false")
		}

		if IsNullLiteral("") {
			t.Error("IsNullLiteral(\"\") = true, want false")
		}

		if IsValidPathStart("") {
			t.Error("IsValidPathStart(\"\") = true, want false")
		}

		if IsValidPathSegment("") {
			t.Error("IsValidPathSegment(\"\") = true, want false")
		}

		if GetKeywordInfo("") != nil {
			t.Error("GetKeywordInfo(\"\") should return nil")
		}
	})

	t.Run("whitespace handling", func(t *testing.T) {
		// Keywords should not match strings with whitespace
		if IsKeywordIdentifier(" if") {
			t.Error("IsKeywordIdentifier(\" if\") = true, want false")
		}
		if IsKeywordIdentifier("if ") {
			t.Error("IsKeywordIdentifier(\"if \") = true, want false")
		}
	})

	t.Run("special characters", func(t *testing.T) {
		// Tilde is a null literal
		if !IsNullLiteral("~") {
			t.Error("IsNullLiteral(\"~\") = false, want true")
		}

		// Other special characters should not be keywords
		specials := []string{"@", "#", "$", "%", "^", "&", "*"}
		for _, s := range specials {
			if IsKeywordIdentifier(s) {
				t.Errorf("IsKeywordIdentifier(%q) = true, want false", s)
			}
		}
	})

	t.Run("path identifier edge cases", func(t *testing.T) {
		// Test paths that break out of identifier mid-scan - "@" is not valid
		if IsValidPathStart("foo@bar") {
			t.Error("IsValidPathStart(\"foo@bar\") = true, want false (@ is invalid)")
		}

		// But paths with dots or brackets are valid
		if !IsValidPathStart("foo.bar") {
			t.Error("IsValidPathStart(\"foo.bar\") = false, want true")
		}

		// Test single char identifiers
		if !IsValidPathStart("x") {
			t.Error("IsValidPathStart(\"x\") = false, want true")
		}

		// Test mixed case
		if !IsValidPathStart("Foo") {
			t.Error("IsValidPathStart(\"Foo\") = false, want true")
		}

		// Test underscore-only identifier
		if !IsValidPathStart("_") {
			t.Error("IsValidPathStart(\"_\") = false, want true")
		}
	})

	t.Run("numeric index edge cases", func(t *testing.T) {
		// Single digit
		if !IsValidPathSegment("0") {
			t.Error("IsValidPathSegment(\"0\") = false, want true")
		}

		// Zero-prefixed number
		if !IsValidPathSegment("007") {
			t.Error("IsValidPathSegment(\"007\") = false, want true")
		}

		// Mixed digits and letters (invalid index, may be valid identifier)
		if IsValidPathSegment("12a") {
			t.Error("IsValidPathSegment(\"12a\") = true, want false")
		}
	})
}

// Benchmark tests for performance-critical functions.
func BenchmarkIsKeywordIdentifier(b *testing.B) {
	keywords := []string{"if", "grab", "vault", "concat", "true", "null", "unknown"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		IsKeywordIdentifier(keywords[i%len(keywords)])
	}
}

func BenchmarkLookupKeywordWithAlias(b *testing.B) {
	keywords := []string{"if", "elsif", "endif", "endfor", "grab", "unknown"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		LookupKeywordWithAlias(keywords[i%len(keywords)])
	}
}

func BenchmarkIsValidPathStart(b *testing.B) {
	paths := []string{"meta.name", "foo[0]", ".", ".path", "simple", "123invalid"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		IsValidPathStart(paths[i%len(paths)])
	}
}

func BenchmarkIsBoolLiteral(b *testing.B) {
	values := []string{"true", "false", "True", "FALSE", "null", "other"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		IsBoolLiteral(values[i%len(values)])
	}
}

func BenchmarkGetKeywordInfo(b *testing.B) {
	keywords := []string{"if", "grab", "and", "elsif", "unknown"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GetKeywordInfo(keywords[i%len(keywords)])
	}
}

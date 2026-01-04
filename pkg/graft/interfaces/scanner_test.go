package interfaces

import (
	"testing"
)

func TestNewScanner(t *testing.T) {
	source := "hello world"
	s := NewScanner(source)

	if s.Source() != source {
		t.Errorf("Source() = %q, want %q", s.Source(), source)
	}
	if s.EOF() {
		t.Error("EOF() = true for non-empty source")
	}
}

func TestNewScannerWithFile(t *testing.T) {
	source := "test"
	filename := "test.graft"
	s := NewScannerWithFile(source, filename)

	pos := s.Pos()
	if pos.File != filename {
		t.Errorf("Pos().File = %q, want %q", pos.File, filename)
	}
}

func TestScanner_Reset(t *testing.T) {
	s := NewScanner("first")
	s.Scan() // consume something

	s.Reset("second")
	if s.Source() != "second" {
		t.Errorf("after Reset, Source() = %q, want %q", s.Source(), "second")
	}
	if s.Pos().Offset != 0 {
		t.Error("after Reset, offset should be 0")
	}
}

func TestScanner_EmptyInput(t *testing.T) {
	s := NewScanner("")

	tok := s.Scan()
	if tok.Type != TokenEOF {
		t.Errorf("empty input: got %v, want TokenEOF", tok.Type)
	}
	if !s.EOF() {
		t.Error("EOF() should be true for empty input")
	}
}

func TestScanner_WhitespaceOnly(t *testing.T) {
	s := NewScanner("   \t\n  ")

	tok := s.Scan()
	if tok.Type != TokenEOF {
		t.Errorf("whitespace only: got %v, want TokenEOF", tok.Type)
	}
}

func TestScanner_Comments(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   TokenType
	}{
		{"comment only", "# this is a comment", TokenEOF},
		{"comment then EOF", "# comment\n", TokenEOF},
		{"token after comment", "# comment\n42", TokenInteger},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewScanner(tt.source)
			tok := s.Scan()
			if tok.Type != tt.want {
				t.Errorf("got %v, want %v", tok.Type, tt.want)
			}
		})
	}
}

func TestScanner_OperatorMarkers(t *testing.T) {
	s := NewScanner("(( foo ))")

	tok := s.Scan()
	if tok.Type != TokenOperatorStart {
		t.Errorf("expected TokenOperatorStart, got %v", tok.Type)
	}
	if tok.Literal != "((" {
		t.Errorf("expected literal '((', got %q", tok.Literal)
	}

	s.Scan() // skip foo

	tok = s.Scan()
	if tok.Type != TokenOperatorEnd {
		t.Errorf("expected TokenOperatorEnd, got %v", tok.Type)
	}
	if tok.Literal != "))" {
		t.Errorf("expected literal '))', got %q", tok.Literal)
	}
}

func TestScanner_Integers(t *testing.T) {
	tests := []struct {
		source string
		value  int64
	}{
		{"0", 0},
		{"42", 42},
		{"123", 123},
		{"-1", -1},
		{"-999", -999},
		{"0x1F", 0x1F},
		{"0xFF", 0xFF},
		{"0x00", 0x00},
	}

	for _, tt := range tests {
		t.Run(tt.source, func(t *testing.T) {
			s := NewScanner(tt.source)
			tok := s.Scan()
			if tok.Type != TokenInteger {
				t.Errorf("got type %v, want TokenInteger", tok.Type)
			}
			val, ok := tok.Value.(int64)
			if !ok {
				t.Fatal("expected tok.Value to be int64")
			}
			if val != tt.value {
				t.Errorf("got value %v, want %v", tok.Value, tt.value)
			}
		})
	}
}

func TestScanner_Floats(t *testing.T) {
	tests := []struct {
		source string
		value  float64
	}{
		{"3.14", 3.14},
		{"0.5", 0.5},
		{"-2.5", -2.5},
		{"1e10", 1e10},
		{"2.5e-3", 2.5e-3},
		{"1E+5", 1e+5},
	}

	for _, tt := range tests {
		t.Run(tt.source, func(t *testing.T) {
			s := NewScanner(tt.source)
			tok := s.Scan()
			if tok.Type != TokenFloat {
				t.Errorf("got type %v, want TokenFloat", tok.Type)
			}
			val, ok := tok.Value.(float64)
			if !ok {
				t.Fatal("expected tok.Value to be float64")
			}
			if val != tt.value {
				t.Errorf("got value %v, want %v", tok.Value, tt.value)
			}
		})
	}
}

func TestScanner_InvalidNumbers(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{"incomplete hex", "0x"},
		{"invalid hex digit", "0xGG"},
		{"incomplete exponent", "1e"},
		{"invalid exponent", "1e+"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewScanner(tt.source)
			tok := s.Scan()
			if tok.Type != TokenInvalid {
				t.Errorf("got type %v, want TokenInvalid", tok.Type)
			}
		})
	}
}

func TestScanner_Strings(t *testing.T) {
	tests := []struct {
		source  string
		value   string
		literal string
	}{
		{`"hello"`, "hello", `"hello"`},
		{`"hello world"`, "hello world", `"hello world"`},
		{`""`, "", `""`},
		{`"line\nbreak"`, "line\nbreak", `"line\nbreak"`},
		{`"tab\there"`, "tab\there", `"tab\there"`},
		{`"quote\"here"`, `quote"here`, `"quote\"here"`},
		{`"back\\slash"`, `back\slash`, `"back\\slash"`},
	}

	for _, tt := range tests {
		t.Run(tt.source, func(t *testing.T) {
			s := NewScanner(tt.source)
			tok := s.Scan()
			if tok.Type != TokenString {
				t.Errorf("got type %v, want TokenString (error: %s)", tok.Type, tok.Error)
			}
			val, ok := tok.Value.(string)
			if !ok {
				t.Fatal("expected tok.Value to be string")
			}
			if val != tt.value {
				t.Errorf("got value %q, want %q", tok.Value, tt.value)
			}
		})
	}
}

func TestScanner_InvalidStrings(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{"unterminated", `"hello`},
		{"newline in string", "\"hello\nworld\""},
		{"invalid escape", `"bad\q"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewScanner(tt.source)
			tok := s.Scan()
			if tok.Type != TokenInvalid {
				t.Errorf("got type %v, want TokenInvalid", tok.Type)
			}
		})
	}
}

func TestScanner_RawStrings(t *testing.T) {
	tests := []struct {
		source  string
		value   string
		literal string
	}{
		{`'hello'`, "hello", `'hello'`},
		{`'hello world'`, "hello world", `'hello world'`},
		{`''`, "", `''`},
		{`'no\nescape'`, `no\nescape`, `'no\nescape'`},
		{"'multi\nline'", "multi\nline", "'multi\nline'"},
	}

	for _, tt := range tests {
		t.Run(tt.source, func(t *testing.T) {
			s := NewScanner(tt.source)
			tok := s.Scan()
			if tok.Type != TokenRawString {
				t.Errorf("got type %v, want TokenRawString (error: %s)", tok.Type, tok.Error)
			}
			val, ok := tok.Value.(string)
			if !ok {
				t.Fatal("expected tok.Value to be string")
			}
			if val != tt.value {
				t.Errorf("got value %q, want %q", tok.Value, tt.value)
			}
		})
	}
}

func TestScanner_Identifiers(t *testing.T) {
	tests := []struct {
		source  string
		tokType TokenType
		literal string
	}{
		{"foo", TokenIdentifier, "foo"},
		{"bar123", TokenIdentifier, "bar123"},
		{"_private", TokenIdentifier, "_private"},
		{"camelCase", TokenIdentifier, "camelCase"},
		{"snake_case", TokenIdentifier, "snake_case"},
		{"with-hyphen", TokenIdentifier, "with-hyphen"},
		{"ABC", TokenIdentifier, "ABC"},
	}

	for _, tt := range tests {
		t.Run(tt.source, func(t *testing.T) {
			s := NewScanner(tt.source)
			tok := s.Scan()
			if tok.Type != tt.tokType {
				t.Errorf("got type %v, want %v", tok.Type, tt.tokType)
			}
			if tok.Literal != tt.literal {
				t.Errorf("got literal %q, want %q", tok.Literal, tt.literal)
			}
		})
	}
}

func TestScanner_Keywords(t *testing.T) {
	tests := []struct {
		source  string
		tokType TokenType
	}{
		{"if", TokenIf},
		{"elif", TokenElif},
		{"else", TokenElse},
		{"fi", TokenFi},
		{"for", TokenFor},
		{"while", TokenWhile},
		{"done", TokenDone},
		{"case", TokenCase},
		{"when", TokenWhen},
		{"default", TokenDefault},
		{"esac", TokenEsac},
		{"in", TokenIn},
		{"range", TokenRange},
		{"on", TokenOn},
		{"before", TokenBefore},
		{"after", TokenAfter},
	}

	for _, tt := range tests {
		t.Run(tt.source, func(t *testing.T) {
			s := NewScanner(tt.source)
			tok := s.Scan()
			if tok.Type != tt.tokType {
				t.Errorf("got type %v, want %v", tok.Type, tt.tokType)
			}
			if tok.Literal != tt.source {
				t.Errorf("got literal %q, want %q", tok.Literal, tt.source)
			}
		})
	}
}

func TestScanner_BooleanLiterals(t *testing.T) {
	tests := []struct {
		source string
		value  bool
	}{
		{"true", true},
		{"false", false},
	}

	for _, tt := range tests {
		t.Run(tt.source, func(t *testing.T) {
			s := NewScanner(tt.source)
			tok := s.Scan()
			if tok.Type != TokenBoolean {
				t.Errorf("got type %v, want TokenBoolean", tok.Type)
			}
			val, ok := tok.Value.(bool)
			if !ok {
				t.Fatal("expected tok.Value to be bool")
			}
			if val != tt.value {
				t.Errorf("got value %v, want %v", tok.Value, tt.value)
			}
		})
	}
}

func TestScanner_NullLiterals(t *testing.T) {
	tests := []string{"null", "nil"}

	for _, source := range tests {
		t.Run(source, func(t *testing.T) {
			s := NewScanner(source)
			tok := s.Scan()
			if tok.Type != TokenNull {
				t.Errorf("got type %v, want TokenNull", tok.Type)
			}
			if tok.Value != nil {
				t.Errorf("got value %v, want nil", tok.Value)
			}
		})
	}
}

func TestScanner_NamedOperators(t *testing.T) {
	// All named operators should be recognized as TokenOperatorName
	// Only include operators that are defined in OperatorNames map in token.go
	operators := []string{
		// Data manipulation
		"grab", "concat", "join", "static", "param", "prune",
		"inject", "stringify", "parse", "defer",
		// External sources
		"vault", "openbao", "awsparam", "awssecret", "awssm", "nats",
		"file", "load", "env",
		// Array operations
		"flatten", "uniq", "sort", "reverse", "shuffle",
		"first", "last", "index", "length", "filter", "map", "reduce",
		"append", "prepend", "inline", "merge", "insert", "delete",
		"cartesian", "cartesian-product",
		// IP operations
		"static_ips", "ips", "ip", "net", "cidr",
		// Math
		"min", "max", "sum", "calc",
		// String
		"substr", "trim", "upper", "lower", "split", "replace",
		"regex", "regexp", "contains", "has-prefix", "has-suffix",
		// Boolean/existence
		"exists", "defined", "any", "all", "none", "empty",
		// Type conversion
		"type", "bool", "int", "float", "string",
		"base64", "base64-decode", "base64decode",
		// Hash
		"md5", "sha1", "sha256", "sha512",
		// Keys/values
		"keys", "values", "elem",
	}

	for _, op := range operators {
		t.Run(op, func(t *testing.T) {
			s := NewScanner(op)
			tok := s.Scan()
			if tok.Type != TokenOperatorName {
				t.Errorf("got type %v, want TokenOperatorName for %q", tok.Type, op)
			}
			if tok.Literal != op {
				t.Errorf("got literal %q, want %q", tok.Literal, op)
			}
		})
	}
}

func TestScanner_Environment(t *testing.T) {
	tests := []struct {
		source  string
		name    string
		literal string
	}{
		{"$HOME", "HOME", "$HOME"},
		{"$PATH", "PATH", "$PATH"},
		{"$MY_VAR", "MY_VAR", "$MY_VAR"},
		{"$var123", "var123", "$var123"},
		{"${HOME}", "HOME", "${HOME}"},
		{"${MY_VAR}", "MY_VAR", "${MY_VAR}"},
	}

	for _, tt := range tests {
		t.Run(tt.source, func(t *testing.T) {
			s := NewScanner(tt.source)
			tok := s.Scan()
			if tok.Type != TokenEnvironment {
				t.Errorf("got type %v, want TokenEnvironment (error: %v)", tok.Type, tok.Value)
			}
			val, ok := tok.Value.(string)
			if !ok {
				t.Fatal("expected tok.Value to be string")
			}
			if val != tt.name {
				t.Errorf("got value %q, want %q", tok.Value, tt.name)
			}
			if tok.Literal != tt.literal {
				t.Errorf("got literal %q, want %q", tok.Literal, tt.literal)
			}
		})
	}
}

func TestScanner_InvalidEnvironment(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{"empty after $", "$"},
		{"invalid char", "$123"},
		{"unclosed brace", "${HOME"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewScanner(tt.source)
			tok := s.Scan()
			if tok.Type != TokenInvalid {
				t.Errorf("got type %v, want TokenInvalid", tok.Type)
			}
		})
	}
}

func TestScanner_SingleCharOperators(t *testing.T) {
	tests := []struct {
		source  string
		tokType TokenType
	}{
		{"+", TokenPlus},
		{"-", TokenMinus},
		{"*", TokenStar},
		{"/", TokenSlash},
		{"%", TokenPercent},
		{"<", TokenLess},
		{">", TokenGreater},
		{"!", TokenNot},
		{"?", TokenQuestion},
		{":", TokenColon},
		{"(", TokenLeftParen},
		{")", TokenRightParen},
		{"[", TokenLeftBracket},
		{"]", TokenRightBracket},
		{"{", TokenLeftBrace},
		{"}", TokenRightBrace},
		{",", TokenComma},
		{".", TokenDot},
		{"@", TokenAt},
		{"|", TokenPipe},
	}

	for _, tt := range tests {
		t.Run(tt.source, func(t *testing.T) {
			s := NewScanner(tt.source)
			tok := s.Scan()
			if tok.Type != tt.tokType {
				t.Errorf("got type %v, want %v", tok.Type, tt.tokType)
			}
		})
	}
}

func TestScanner_MultiCharOperators(t *testing.T) {
	tests := []struct {
		source  string
		tokType TokenType
	}{
		{"==", TokenEqual},
		{"!=", TokenNotEqual},
		{"<=", TokenLessEqual},
		{">=", TokenGreaterEqual},
		{"&&", TokenAnd},
		{"||", TokenOr},
	}

	for _, tt := range tests {
		t.Run(tt.source, func(t *testing.T) {
			s := NewScanner(tt.source)
			tok := s.Scan()
			if tok.Type != tt.tokType {
				t.Errorf("got type %v, want %v", tok.Type, tt.tokType)
			}
		})
	}
}

func TestScanner_Peek(t *testing.T) {
	s := NewScanner("a b c")

	// Peek should not advance
	tok1 := s.Peek()
	tok2 := s.Peek()
	if tok1.Literal != tok2.Literal {
		t.Error("Peek() should return same token on repeated calls")
	}

	// Scan should return peeked token
	tok3 := s.Scan()
	if tok3.Literal != tok1.Literal {
		t.Error("Scan() should return same token as Peek()")
	}

	// After Scan, Peek should show next token
	tok4 := s.Peek()
	if tok4.Literal == tok1.Literal {
		t.Error("After Scan(), Peek() should show next token")
	}
}

func TestScanner_PeekN(t *testing.T) {
	s := NewScanner("a b c")

	tok0 := s.PeekN(0)
	tok1 := s.PeekN(1)
	tok2 := s.PeekN(2)

	if tok0.Literal != "a" {
		t.Errorf("PeekN(0) = %q, want 'a'", tok0.Literal)
	}
	if tok1.Literal != "b" {
		t.Errorf("PeekN(1) = %q, want 'b'", tok1.Literal)
	}
	if tok2.Literal != "c" {
		t.Errorf("PeekN(2) = %q, want 'c'", tok2.Literal)
	}

	// Scanner should still be at start
	scanned := s.Scan()
	if scanned.Literal != "a" {
		t.Errorf("After PeekN, Scan() = %q, want 'a'", scanned.Literal)
	}
}

func TestScanner_Position(t *testing.T) {
	s := NewScanner("a\nbc\n  d")

	// Token 'a' at line 1, col 1
	tok := s.Scan()
	if tok.Pos.Line != 1 || tok.Pos.Column != 1 {
		t.Errorf("'a' position = %d:%d, want 1:1", tok.Pos.Line, tok.Pos.Column)
	}

	// Token 'bc' at line 2, col 1
	tok = s.Scan()
	if tok.Pos.Line != 2 || tok.Pos.Column != 1 {
		t.Errorf("'bc' position = %d:%d, want 2:1", tok.Pos.Line, tok.Pos.Column)
	}

	// Token 'd' at line 3, col 3
	tok = s.Scan()
	if tok.Pos.Line != 3 || tok.Pos.Column != 3 {
		t.Errorf("'d' position = %d:%d, want 3:3", tok.Pos.Line, tok.Pos.Column)
	}
}

func TestScanner_ComplexExpression(t *testing.T) {
	s := NewScanner("grab foo.bar || \"default\"")

	expected := []struct {
		tokType TokenType
		literal string
	}{
		{TokenOperatorName, "grab"},
		{TokenIdentifier, "foo"},
		{TokenDot, "."},
		{TokenIdentifier, "bar"},
		{TokenOr, "||"},
		{TokenString, `"default"`},
		{TokenEOF, ""},
	}

	for i, exp := range expected {
		tok := s.Scan()
		if tok.Type != exp.tokType {
			t.Errorf("token[%d]: got type %v, want %v", i, tok.Type, exp.tokType)
		}
		if tok.Literal != exp.literal {
			t.Errorf("token[%d]: got literal %q, want %q", i, tok.Literal, exp.literal)
		}
	}
}

func TestTokenizeAll(t *testing.T) {
	tokens := TokenizeAll("a + b")

	if len(tokens) != 4 { // a, +, b, EOF
		t.Errorf("got %d tokens, want 4", len(tokens))
	}

	if tokens[0].Type != TokenIdentifier {
		t.Errorf("tokens[0] = %v, want identifier", tokens[0].Type)
	}
	if tokens[1].Type != TokenPlus {
		t.Errorf("tokens[1] = %v, want plus", tokens[1].Type)
	}
	if tokens[2].Type != TokenIdentifier {
		t.Errorf("tokens[2] = %v, want identifier", tokens[2].Type)
	}
	if tokens[3].Type != TokenEOF {
		t.Errorf("tokens[3] = %v, want EOF", tokens[3].Type)
	}
}

func TestScanner_UnexpectedCharacter(t *testing.T) {
	s := NewScanner("~")
	tok := s.Scan()

	if tok.Type != TokenInvalid {
		t.Errorf("got type %v, want TokenInvalid", tok.Type)
	}
}

func TestIsValidIdentifier(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"foo", true},
		{"_bar", true},
		{"a123", true},
		{"with-hyphen", true},
		{"123abc", false},
		{"-start", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := IsValidIdentifier(tt.input); got != tt.valid {
				t.Errorf("IsValidIdentifier(%q) = %v, want %v", tt.input, got, tt.valid)
			}
		})
	}
}

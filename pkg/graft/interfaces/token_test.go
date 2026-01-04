package interfaces

import (
	"testing"
)

func TestTokenType_String(t *testing.T) {
	tests := []struct {
		tokType TokenType
		want    string
	}{
		{TokenEOF, "EOF"},
		{TokenInvalid, "INVALID"},
		{TokenOperatorStart, "(("},
		{TokenOperatorEnd, "))"},
		{TokenInteger, "INTEGER"},
		{TokenFloat, "FLOAT"},
		{TokenString, "STRING"},
		{TokenRawString, "RAW_STRING"},
		{TokenBoolean, "BOOLEAN"},
		{TokenNull, "NULL"},
		{TokenIdentifier, "IDENTIFIER"},
		{TokenReference, "REFERENCE"},
		{TokenEnvironment, "ENVIRONMENT"},
		{TokenPlus, "+"},
		{TokenMinus, "-"},
		{TokenStar, "*"},
		{TokenSlash, "/"},
		{TokenPercent, "%"},
		{TokenEqual, "=="},
		{TokenNotEqual, "!="},
		{TokenLess, "<"},
		{TokenGreater, ">"},
		{TokenLessEqual, "<="},
		{TokenGreaterEqual, ">="},
		{TokenAnd, "&&"},
		{TokenOr, "||"},
		{TokenNot, "!"},
		{TokenQuestion, "?"},
		{TokenColon, ":"},
		{TokenLeftParen, "("},
		{TokenRightParen, ")"},
		{TokenLeftBracket, "["},
		{TokenRightBracket, "]"},
		{TokenLeftBrace, "{"},
		{TokenRightBrace, "}"},
		{TokenComma, ","},
		{TokenDot, "."},
		{TokenAt, "@"},
		{TokenPipe, "|"},
		{TokenIf, "if"},
		{TokenElif, "elif"},
		{TokenElse, "else"},
		{TokenFi, "fi"},
		{TokenFor, "for"},
		{TokenIn, "in"},
		{TokenDone, "done"},
		{TokenWhile, "while"},
		{TokenCase, "case"},
		{TokenWhen, "when"},
		{TokenDefault, "default"},
		{TokenEsac, "esac"},
		{TokenOn, "on"},
		{TokenBefore, "before"},
		{TokenAfter, "after"},
		{TokenRange, "range"},
		{TokenOperatorName, "OPERATOR_NAME"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.tokType.String(); got != tt.want {
				t.Errorf("TokenType.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTokenType_String_Unknown(t *testing.T) {
	unknown := TokenType(9999)
	got := unknown.String()
	if got == "" {
		t.Error("Unknown token type should return non-empty string")
	}
}

func TestNewToken(t *testing.T) {
	pos := Position{Line: 1, Column: 5, Offset: 4}
	end := Position{Line: 1, Column: 7, Offset: 6}

	tok := NewToken(TokenPlus, "+", pos, end)

	if tok.Type != TokenPlus {
		t.Errorf("Type = %v, want TokenPlus", tok.Type)
	}
	if tok.Literal != "+" {
		t.Errorf("Literal = %q, want +", tok.Literal)
	}
	if tok.Pos != pos {
		t.Errorf("Pos = %v, want %v", tok.Pos, pos)
	}
	if tok.End != end {
		t.Errorf("End = %v, want %v", tok.End, end)
	}
}

func TestNewLiteralToken(t *testing.T) {
	pos := Position{Line: 1, Column: 1, Offset: 0}
	end := Position{Line: 1, Column: 3, Offset: 2}

	tok := NewLiteralToken(TokenInteger, int64(42), "42", pos, end)

	if tok.Type != TokenInteger {
		t.Errorf("Type = %v, want TokenInteger", tok.Type)
	}
	val, ok := tok.Value.(int64)
	if !ok {
		t.Fatal("expected tok.Value to be int64")
	}
	if val != 42 {
		t.Errorf("Value = %v, want 42", tok.Value)
	}
	if tok.Literal != "42" {
		t.Errorf("Literal = %q, want 42", tok.Literal)
	}
}

func TestNewIntToken(t *testing.T) {
	pos := Position{Line: 1, Column: 1, Offset: 0}
	end := Position{Line: 1, Column: 4, Offset: 3}

	tok := NewIntToken(123, "123", pos, end)

	if tok.Type != TokenInteger {
		t.Errorf("Type = %v, want TokenInteger", tok.Type)
	}
	val, ok := tok.Value.(int64)
	if !ok {
		t.Fatal("expected tok.Value to be int64")
	}
	if val != 123 {
		t.Errorf("Value = %v, want 123", tok.Value)
	}
}

func TestNewFloatToken(t *testing.T) {
	pos := Position{Line: 1, Column: 1, Offset: 0}
	end := Position{Line: 1, Column: 5, Offset: 4}

	tok := NewFloatToken(3.14, "3.14", pos, end)

	if tok.Type != TokenFloat {
		t.Errorf("Type = %v, want TokenFloat", tok.Type)
	}
	val, ok := tok.Value.(float64)
	if !ok {
		t.Fatal("expected tok.Value to be float64")
	}
	if val != 3.14 {
		t.Errorf("Value = %v, want 3.14", tok.Value)
	}
}

func TestNewStringToken(t *testing.T) {
	pos := Position{Line: 1, Column: 1, Offset: 0}
	end := Position{Line: 1, Column: 8, Offset: 7}

	tok := NewStringToken("hello", `"hello"`, pos, end)

	if tok.Type != TokenString {
		t.Errorf("Type = %v, want TokenString", tok.Type)
	}
	val, ok := tok.Value.(string)
	if !ok {
		t.Fatal("expected tok.Value to be string")
	}
	if val != "hello" {
		t.Errorf("Value = %q, want hello", tok.Value)
	}
	if tok.Literal != `"hello"` {
		t.Errorf("Literal = %q, want \"hello\"", tok.Literal)
	}
}

func TestNewBoolToken(t *testing.T) {
	pos := Position{Line: 1, Column: 1, Offset: 0}
	end := Position{Line: 1, Column: 5, Offset: 4}

	tokTrue := NewBoolToken(true, "true", pos, end)
	if tokTrue.Type != TokenBoolean {
		t.Errorf("Type = %v, want TokenBoolean", tokTrue.Type)
	}
	valTrue, ok := tokTrue.Value.(bool)
	if !ok {
		t.Fatal("expected tokTrue.Value to be bool")
	}
	if valTrue != true {
		t.Errorf("Value = %v, want true", tokTrue.Value)
	}

	tokFalse := NewBoolToken(false, "false", pos, end)
	valFalse, ok := tokFalse.Value.(bool)
	if !ok {
		t.Fatal("expected tokFalse.Value to be bool")
	}
	if valFalse != false {
		t.Errorf("Value = %v, want false", tokFalse.Value)
	}
}

func TestNewNullToken(t *testing.T) {
	pos := Position{Line: 1, Column: 1, Offset: 0}
	end := Position{Line: 1, Column: 5, Offset: 4}

	tok := NewNullToken("null", pos, end)

	if tok.Type != TokenNull {
		t.Errorf("Type = %v, want TokenNull", tok.Type)
	}
	if tok.Value != nil {
		t.Errorf("Value = %v, want nil", tok.Value)
	}
}

func TestNewIdentToken(t *testing.T) {
	pos := Position{Line: 1, Column: 1, Offset: 0}
	end := Position{Line: 1, Column: 4, Offset: 3}

	tok := NewIdentToken("foo", pos, end)

	if tok.Type != TokenIdentifier {
		t.Errorf("Type = %v, want TokenIdentifier", tok.Type)
	}
	val, ok := tok.Value.(string)
	if !ok {
		t.Fatal("expected tok.Value to be string")
	}
	if val != "foo" {
		t.Errorf("Value = %q, want foo", tok.Value)
	}
	if tok.Literal != "foo" {
		t.Errorf("Literal = %q, want foo", tok.Literal)
	}
}

func TestNewEnvVarToken(t *testing.T) {
	pos := Position{Line: 1, Column: 1, Offset: 0}
	end := Position{Line: 1, Column: 6, Offset: 5}

	tok := NewEnvVarToken("HOME", "$HOME", pos, end)

	if tok.Type != TokenEnvironment {
		t.Errorf("Type = %v, want TokenEnvironment", tok.Type)
	}
	val, ok := tok.Value.(string)
	if !ok {
		t.Fatal("expected tok.Value to be string")
	}
	if val != "HOME" {
		t.Errorf("Value = %q, want HOME", tok.Value)
	}
	if tok.Literal != "$HOME" {
		t.Errorf("Literal = %q, want $HOME", tok.Literal)
	}
}

func TestNewRefToken(t *testing.T) {
	pos := Position{Line: 1, Column: 1, Offset: 0}
	end := Position{Line: 1, Column: 8, Offset: 7}

	tok := NewRefToken("foo.bar", pos, end)

	if tok.Type != TokenReference {
		t.Errorf("Type = %v, want TokenReference", tok.Type)
	}
	val, ok := tok.Value.(string)
	if !ok {
		t.Fatal("expected tok.Value to be string")
	}
	if val != "foo.bar" {
		t.Errorf("Value = %q, want foo.bar", tok.Value)
	}
}

func TestNewEOFToken(t *testing.T) {
	pos := Position{Line: 10, Column: 1, Offset: 100}

	tok := NewEOFToken(pos)

	if tok.Type != TokenEOF {
		t.Errorf("Type = %v, want TokenEOF", tok.Type)
	}
	if tok.Pos != pos {
		t.Errorf("Pos = %v, want %v", tok.Pos, pos)
	}
	if tok.End != pos {
		t.Errorf("End = %v, want %v", tok.End, pos)
	}
}

func TestNewErrorToken(t *testing.T) {
	pos := Position{Line: 1, Column: 5, Offset: 4}
	end := Position{Line: 1, Column: 6, Offset: 5}

	tok := NewErrorToken("unexpected character", pos, end)

	if tok.Type != TokenInvalid {
		t.Errorf("Type = %v, want TokenInvalid", tok.Type)
	}
	if tok.Error != "unexpected character" {
		t.Errorf("Error = %q, want unexpected character", tok.Error)
	}
}

func TestToken_String(t *testing.T) {
	pos := Position{Line: 1, Column: 1, Offset: 0}
	end := Position{Line: 1, Column: 3, Offset: 2}

	tests := []struct {
		name string
		tok  Token
	}{
		{
			name: "with value",
			tok:  NewIntToken(42, "42", pos, end),
		},
		{
			name: "with literal only",
			tok:  NewToken(TokenPlus, "+", pos, end),
		},
		{
			name: "with error",
			tok:  NewErrorToken("bad token", pos, end),
		},
		{
			name: "EOF",
			tok:  NewEOFToken(pos),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := tt.tok.String()
			if s == "" {
				t.Error("String() should not return empty string")
			}
		})
	}
}

func TestToken_Range(t *testing.T) {
	pos := Position{Line: 1, Column: 1, Offset: 0}
	end := Position{Line: 1, Column: 5, Offset: 4}

	tok := NewToken(TokenInteger, "1234", pos, end)
	r := tok.Range()

	if r.Start != pos {
		t.Errorf("Range.Start = %v, want %v", r.Start, pos)
	}
	if r.End != end {
		t.Errorf("Range.End = %v, want %v", r.End, end)
	}
}

func TestToken_IsZero(t *testing.T) {
	var zeroTok Token
	if !zeroTok.IsZero() {
		t.Error("Zero token should return true for IsZero()")
	}

	tok := NewIntToken(1, "1", Position{Line: 1, Column: 1}, Position{Line: 1, Column: 2})
	if tok.IsZero() {
		t.Error("Non-zero token should return false for IsZero()")
	}
}

func TestToken_IsEOF(t *testing.T) {
	eofTok := NewEOFToken(Position{})
	if !eofTok.IsEOF() {
		t.Error("EOF token should return true for IsEOF()")
	}

	otherTok := NewIntToken(1, "1", Position{}, Position{})
	if otherTok.IsEOF() {
		t.Error("Non-EOF token should return false for IsEOF()")
	}
}

func TestToken_IsInvalid(t *testing.T) {
	invalidTok := NewErrorToken("error", Position{}, Position{})
	if !invalidTok.IsInvalid() {
		t.Error("Invalid token should return true for IsInvalid()")
	}

	validTok := NewIntToken(1, "1", Position{}, Position{})
	if validTok.IsInvalid() {
		t.Error("Valid token should return false for IsInvalid()")
	}
}

func TestTokenType_IsLiteral(t *testing.T) {
	literals := []TokenType{
		TokenInteger, TokenFloat, TokenString, TokenRawString, TokenBoolean, TokenNull,
	}
	for _, tt := range literals {
		if !tt.IsLiteral() {
			t.Errorf("%v should be a literal", tt)
		}
	}

	nonLiterals := []TokenType{
		TokenIdentifier, TokenPlus, TokenIf, TokenOperatorName,
	}
	for _, tt := range nonLiterals {
		if tt.IsLiteral() {
			t.Errorf("%v should not be a literal", tt)
		}
	}
}

func TestTokenType_IsArithmeticOperator(t *testing.T) {
	arithmetic := []TokenType{TokenPlus, TokenMinus, TokenStar, TokenSlash, TokenPercent}
	for _, tt := range arithmetic {
		if !tt.IsArithmeticOperator() {
			t.Errorf("%v should be an arithmetic operator", tt)
		}
	}

	nonArithmetic := []TokenType{TokenEqual, TokenAnd, TokenIf}
	for _, tt := range nonArithmetic {
		if tt.IsArithmeticOperator() {
			t.Errorf("%v should not be an arithmetic operator", tt)
		}
	}
}

func TestTokenType_IsComparisonOperator(t *testing.T) {
	comparisons := []TokenType{
		TokenEqual, TokenNotEqual, TokenLess, TokenGreater, TokenLessEqual, TokenGreaterEqual,
	}
	for _, tt := range comparisons {
		if !tt.IsComparisonOperator() {
			t.Errorf("%v should be a comparison operator", tt)
		}
	}

	nonComparisons := []TokenType{TokenPlus, TokenAnd, TokenIf}
	for _, tt := range nonComparisons {
		if tt.IsComparisonOperator() {
			t.Errorf("%v should not be a comparison operator", tt)
		}
	}
}

func TestTokenType_IsLogicalOperator(t *testing.T) {
	logical := []TokenType{TokenAnd, TokenOr, TokenNot}
	for _, tt := range logical {
		if !tt.IsLogicalOperator() {
			t.Errorf("%v should be a logical operator", tt)
		}
	}

	nonLogical := []TokenType{TokenPlus, TokenEqual, TokenIf}
	for _, tt := range nonLogical {
		if tt.IsLogicalOperator() {
			t.Errorf("%v should not be a logical operator", tt)
		}
	}
}

func TestTokenType_IsOperator(t *testing.T) {
	operators := []TokenType{
		TokenPlus, TokenMinus, TokenStar, TokenSlash, TokenPercent,
		TokenEqual, TokenNotEqual, TokenLess, TokenGreater, TokenLessEqual, TokenGreaterEqual,
		TokenAnd, TokenOr, TokenNot,
		TokenQuestion, TokenColon,
	}
	for _, tt := range operators {
		if !tt.IsOperator() {
			t.Errorf("%v should be an operator", tt)
		}
	}
}

func TestTokenType_IsBinaryOperator(t *testing.T) {
	binary := []TokenType{
		TokenPlus, TokenMinus, TokenStar, TokenSlash, TokenPercent,
		TokenEqual, TokenNotEqual, TokenLess, TokenGreater, TokenLessEqual, TokenGreaterEqual,
		TokenAnd, TokenOr,
	}
	for _, tt := range binary {
		if !tt.IsBinaryOperator() {
			t.Errorf("%v should be a binary operator", tt)
		}
	}

	notBinary := []TokenType{TokenNot, TokenIf}
	for _, tt := range notBinary {
		if tt.IsBinaryOperator() {
			t.Errorf("%v should not be a binary operator", tt)
		}
	}
}

func TestTokenType_IsUnaryOperator(t *testing.T) {
	unary := []TokenType{TokenNot, TokenMinus}
	for _, tt := range unary {
		if !tt.IsUnaryOperator() {
			t.Errorf("%v should be a unary operator", tt)
		}
	}

	notUnary := []TokenType{TokenPlus, TokenAnd}
	for _, tt := range notUnary {
		if tt.IsUnaryOperator() {
			t.Errorf("%v should not be a unary operator", tt)
		}
	}
}

func TestTokenType_IsKeyword(t *testing.T) {
	keywords := []TokenType{
		TokenIf, TokenElif, TokenElse, TokenFi,
		TokenFor, TokenIn, TokenDone, TokenWhile,
		TokenCase, TokenWhen, TokenDefault, TokenEsac,
		TokenRange, TokenOn, TokenBefore, TokenAfter,
	}
	for _, tt := range keywords {
		if !tt.IsKeyword() {
			t.Errorf("%v should be a keyword", tt)
		}
	}

	notKeywords := []TokenType{TokenIdentifier, TokenPlus, TokenOperatorName}
	for _, tt := range notKeywords {
		if tt.IsKeyword() {
			t.Errorf("%v should not be a keyword", tt)
		}
	}
}

func TestTokenType_IsDelimiter(t *testing.T) {
	delimiters := []TokenType{
		TokenLeftParen, TokenRightParen,
		TokenLeftBracket, TokenRightBracket,
		TokenLeftBrace, TokenRightBrace,
		TokenComma, TokenDot, TokenAt, TokenPipe,
		TokenColon, TokenQuestion,
	}
	for _, tt := range delimiters {
		if !tt.IsDelimiter() {
			t.Errorf("%v should be a delimiter", tt)
		}
	}
}

func TestTokenType_IsControlToken(t *testing.T) {
	control := []TokenType{TokenOperatorStart, TokenOperatorEnd, TokenEOF, TokenInvalid}
	for _, tt := range control {
		if !tt.IsControlToken() {
			t.Errorf("%v should be a control token", tt)
		}
	}
}

func TestTokenType_IsNamedOperator(t *testing.T) {
	if !TokenOperatorName.IsNamedOperator() {
		t.Error("TokenOperatorName should be a named operator")
	}
	if TokenIdentifier.IsNamedOperator() {
		t.Error("TokenIdentifier should not be a named operator")
	}
}

func TestTokenType_CanStartExpression(t *testing.T) {
	canStart := []TokenType{
		TokenInteger, TokenFloat, TokenString, TokenRawString,
		TokenBoolean, TokenNull,
		TokenIdentifier, TokenEnvironment, TokenReference,
		TokenLeftParen, TokenNot, TokenMinus,
		TokenOperatorName,
		TokenIf, TokenFor, TokenCase,
	}
	for _, tt := range canStart {
		if !tt.CanStartExpression() {
			t.Errorf("%v should be able to start an expression", tt)
		}
	}

	cannotStart := []TokenType{TokenRightParen, TokenComma, TokenEOF}
	for _, tt := range cannotStart {
		if tt.CanStartExpression() {
			t.Errorf("%v should not be able to start an expression", tt)
		}
	}
}

func TestTokenType_Precedence(t *testing.T) {
	// Verify precedence order (higher = tighter binding)
	tests := []struct {
		a, b TokenType
	}{
		{TokenStar, TokenPlus},   // * > +
		{TokenPlus, TokenLess},   // + > <
		{TokenLess, TokenEqual},  // < > ==
		{TokenEqual, TokenAnd},   // == > &&
		{TokenAnd, TokenOr},      // && > ||
		{TokenOr, TokenQuestion}, // || > ?
		{TokenNot, TokenStar},    // ! (unary) > *
	}

	for _, tt := range tests {
		if tt.a.Precedence() <= tt.b.Precedence() {
			t.Errorf("Precedence(%v) should be > Precedence(%v)", tt.a, tt.b)
		}
	}

	// Non-operators should have 0 precedence
	if TokenIdentifier.Precedence() != 0 {
		t.Error("Non-operator should have 0 precedence")
	}
}

func TestTokenCount(t *testing.T) {
	count := TokenCount()
	if count < 40 {
		t.Errorf("TokenCount() = %d, want >= 40", count)
	}
}

func TestLookupKeyword(t *testing.T) {
	tests := []struct {
		input string
		want  TokenType
	}{
		// Keywords
		{"if", TokenIf},
		{"elif", TokenElif},
		{"else", TokenElse},
		{"fi", TokenFi},
		{"for", TokenFor},
		{"in", TokenIn},
		{"done", TokenDone},
		{"while", TokenWhile},
		{"case", TokenCase},
		{"when", TokenWhen},
		{"default", TokenDefault},
		{"esac", TokenEsac},
		{"range", TokenRange},
		{"on", TokenOn},
		{"before", TokenBefore},
		{"after", TokenAfter},
		// Boolean literals
		{"true", TokenBoolean},
		{"false", TokenBoolean},
		// Null literals
		{"null", TokenNull},
		{"nil", TokenNull},
		// Operator names
		{"grab", TokenOperatorName},
		{"vault", TokenOperatorName},
		{"concat", TokenOperatorName},
		// Unknown identifier
		{"unknown_identifier", TokenIdentifier},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := LookupKeyword(tt.input); got != tt.want {
				t.Errorf("LookupKeyword(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestToken_IsLiteral(t *testing.T) {
	pos := Position{}
	end := Position{}

	literalTok := NewIntToken(1, "1", pos, end)
	if !literalTok.IsLiteral() {
		t.Error("Integer token should be a literal")
	}

	identTok := NewIdentToken("foo", pos, end)
	if identTok.IsLiteral() {
		t.Error("Identifier token should not be a literal")
	}
}

func TestToken_IsOperator(t *testing.T) {
	pos := Position{}
	end := Position{}

	opTok := NewToken(TokenPlus, "+", pos, end)
	if !opTok.IsOperator() {
		t.Error("Plus token should be an operator")
	}

	identTok := NewIdentToken("foo", pos, end)
	if identTok.IsOperator() {
		t.Error("Identifier token should not be an operator")
	}
}

func TestToken_IsKeyword(t *testing.T) {
	pos := Position{}
	end := Position{}

	kwTok := NewToken(TokenIf, "if", pos, end)
	if !kwTok.IsKeyword() {
		t.Error("If token should be a keyword")
	}

	identTok := NewIdentToken("foo", pos, end)
	if identTok.IsKeyword() {
		t.Error("Identifier token should not be a keyword")
	}
}

func TestToken_IsNamedOperator(t *testing.T) {
	pos := Position{}
	end := Position{}

	namedOpTok := NewToken(TokenOperatorName, "grab", pos, end)
	if !namedOpTok.IsNamedOperator() {
		t.Error("TokenOperatorName token should be a named operator")
	}

	identTok := NewIdentToken("foo", pos, end)
	if identTok.IsNamedOperator() {
		t.Error("Identifier token should not be a named operator")
	}
}

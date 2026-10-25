// Package lexer turns jq and yq query source into tokens.
//
// A TokenSet supplied by the dialect decides the keywords, the operator
// spellings, whether $__loc__ is a token of its own, and which comment
// rule applies. String literals are lexed with an interpolation state
// stack, so "a\(1 + 2)b" arrives as StrStart, StrLit, InterpStart,
// the tokens of the inner expression, InterpEnd, StrLit, and StrEnd.
package lexer

import "fmt"

// TokenKind identifies what a Token is.
type TokenKind int

const (
	// EOF marks the end of the source and is returned forever after.
	EOF TokenKind = iota

	// Ident is a name, optionally namespaced with "::".
	Ident
	// Keyword is an identifier that the active TokenSet lists as a keyword.
	Keyword
	// Field is ".foo". Text holds the source spelling, dot included.
	Field
	// Variable is "$name". Text holds the source spelling, dollar included.
	Variable
	// Loc is "$__loc__" when the TokenSet asks for it.
	Loc
	// Number is a numeric literal. Text holds the source spelling.
	Number
	// Format is "@name". Text holds the source spelling, at sign included.
	Format

	// StrStart is the opening quote of a string.
	StrStart
	// StrLit is a run of literal string text. Text holds it with escapes decoded.
	StrLit
	// InterpStart is the "\(" that opens an interpolation.
	InterpStart
	// InterpEnd is the ")" that closes an interpolation.
	InterpEnd
	// StrEnd is the closing quote of a string.
	StrEnd
)

// Operator and punctuation kinds, named for their spelling in the source.
const (
	Dot = StrEnd + 1 + iota
	DotDot
	DotDotDot
	Question
	AltDestructure
	Pipe
	Comma
	Alt
	Assign
	UpdateAssign
	AltAssign
	AddAssign
	SubAssign
	MulAssign
	DivAssign
	ModAssign
	Eq
	Neq
	Lt
	Le
	Gt
	Ge
	Plus
	Minus
	Star
	Slash
	Percent
	LParen
	RParen
	LBracket
	RBracket
	LBrace
	RBrace
	Colon
	Semicolon
)

var kindNames = map[TokenKind]string{
	EOF: "EOF", Ident: "Ident", Keyword: "Keyword", Field: "Field",
	Variable: "Variable", Loc: "Loc", Number: "Number", Format: "Format",
	StrStart: "StrStart", StrLit: "StrLit", InterpStart: "InterpStart",
	InterpEnd: "InterpEnd", StrEnd: "StrEnd", Dot: "Dot", DotDot: "DotDot",
	DotDotDot: "DotDotDot", Question: "Question", AltDestructure: "AltDestructure",
	Pipe: "Pipe", Comma: "Comma", Alt: "Alt", Assign: "Assign",
	UpdateAssign: "UpdateAssign", AltAssign: "AltAssign", AddAssign: "AddAssign",
	SubAssign: "SubAssign", MulAssign: "MulAssign", DivAssign: "DivAssign",
	ModAssign: "ModAssign", Eq: "Eq", Neq: "Neq", Lt: "Lt", Le: "Le",
	Gt: "Gt", Ge: "Ge", Plus: "Plus", Minus: "Minus", Star: "Star",
	Slash: "Slash", Percent: "Percent", LParen: "LParen", RParen: "RParen",
	LBracket: "LBracket", RBracket: "RBracket", LBrace: "LBrace",
	RBrace: "RBrace", Colon: "Colon", Semicolon: "Semicolon",
}

// String returns the kind's name, for test and diagnostic output.
func (k TokenKind) String() string {
	if n, ok := kindNames[k]; ok {
		return n
	}
	return fmt.Sprintf("TokenKind(%d)", int(k))
}

// Token is one lexical unit. Pos and End are byte offsets into the source,
// with End exclusive.
type Token struct {
	Kind TokenKind
	Text string
	Pos  int
	End  int
}

// Error is a lexing failure at a byte offset.
type Error struct {
	Pos int
	Msg string
}

// Error implements the error interface.
func (e *Error) Error() string {
	return fmt.Sprintf("%s at offset %d", e.Msg, e.Pos)
}

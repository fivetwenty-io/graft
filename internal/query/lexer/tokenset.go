package lexer

// TokenSet is the dialect's description of its lexical surface.
type TokenSet struct {
	// Keywords are the identifiers that lex as Keyword.
	Keywords []string
	// Operators are the operator and punctuation spellings in use.
	// They match longest first regardless of the order given.
	Operators []string
	// LocToken makes "$__loc__" a Loc token instead of a Variable.
	LocToken bool
	// CommentContinuation lets a backslash at the end of a comment line
	// continue the comment onto the next line, as jq 1.7 does.
	CommentContinuation bool
	// TripleDot makes "..." a DotDotDot token.
	TripleDot bool
	// MergeFlags are the characters that may follow "*" and "*=" as part
	// of the same token, which is how yq spells its merge options, such
	// as "*+" and "*=d".
	MergeFlags string
	// AssignFlags are the characters that may follow "=" and "|=" as part
	// of the same token, which is how yq spells its clobbering
	// assignments, such as "=c".
	AssignFlags string
	// LenientEscapes makes a backslash that starts none of yq's escapes
	// stand for itself, as it does in yq. The escapes are \\, \", \b,
	// \f, \n, \r, \t, \a, and \v, so "\u" and "\/" keep their backslash.
	LenientEscapes bool
	// HexNumbers lets a number be written as 0x followed by hex digits.
	HexNumbers bool
	// TildeNull makes "~" an identifier, which yq reads as null.
	TildeNull bool
}

// operatorKinds maps every spelling the lexer knows to its kind. A
// TokenSet selects which of them are active.
var operatorKinds = map[string]TokenKind{
	"|": Pipe, ",": Comma, "//": Alt, "=": Assign, "|=": UpdateAssign,
	"//=": AltAssign, "+=": AddAssign, "-=": SubAssign, "*=": MulAssign,
	"/=": DivAssign, "%=": ModAssign, "==": Eq, "!=": Neq, "<": Lt,
	"<=": Le, ">": Gt, ">=": Ge, "+": Plus, "-": Minus, "*": Star,
	"/": Slash, "%": Percent, "?": Question, "?//": AltDestructure,
	"(": LParen, ")": RParen, "[": LBracket, "]": RBracket, "{": LBrace,
	"}": RBrace, ":": Colon, ";": Semicolon,
}

// JQ returns the token set for the jq dialect.
func JQ() TokenSet {
	return TokenSet{
		Keywords: []string{
			"def", "if", "then", "elif", "else", "end", "as", "reduce",
			"foreach", "try", "catch", "label", "import", "include",
			"and", "or", "module", "break",
		},
		Operators: []string{
			"|", ",", "//", "=", "|=", "//=", "+=", "-=", "*=", "/=", "%=",
			"==", "!=", "<", "<=", ">", ">=", "+", "-", "*", "/", "%",
			"?", "?//", "(", ")", "[", "]", "{", "}", ":", ";",
		},
		LocToken:            true,
		CommentContinuation: true,
	}
}

// YQ returns the token set for the yq dialect. It keeps jq's keywords
// for the forms yq lacks, so they stay parse errors rather than calls,
// and adds ireduce. It has no "?//", "/=", "%=", "//=", or "$__loc__",
// comments never continue, "*" and "*=" take yq's merge flags, and "="
// and "|=" take its clobber flag.
func YQ() TokenSet {
	return TokenSet{
		Keywords: []string{
			"and", "or", "as", "if", "then", "elif", "else", "end", "ireduce",
			"def", "reduce", "foreach", "try", "catch", "label",
		},
		Operators: []string{
			"|", ",", "//", "=", "|=", "+=", "-=", "*=", "==", "!=", "<",
			"<=", ">", ">=", "+", "-", "*", "/", "%", "?", "(", ")", "[",
			"]", "{", "}", ":", ";",
		},
		TripleDot:   true,
		MergeFlags:  "+?cdn",
		AssignFlags: "c",

		LenientEscapes: true,
		HexNumbers:     true,
		TildeNull:      true,
	}
}

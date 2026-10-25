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

// YQ returns the token set for the yq dialect. Only tests consult it
// until the yq phase lands.
func YQ() TokenSet {
	return TokenSet{
		Keywords: []string{"and", "or", "as", "def", "reduce", "foreach", "if", "then", "elif", "else", "end", "try", "catch", "label"},
		Operators: []string{
			"|", ",", "//", "=", "|=", "+=", "-=", "*=", "/=", "%=",
			"==", "!=", "<", "<=", ">", ">=", "+", "-", "*", "/", "%",
			"?", "(", ")", "[", "]", "{", "}", ":", ";",
		},
		TripleDot: true,
	}
}

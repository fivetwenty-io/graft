package lexer

import "testing"

func TestLexFieldForms(t *testing.T) {
	toks := mustLex(t, `.foo ."bar" .["baz"] .. ?`, jqSet())
	wantKinds(t, toks, Field, Dot, StrStart, StrLit, StrEnd, Dot, LBracket, StrStart, StrLit, StrEnd, RBracket, DotDot, Question, EOF)
}

func TestLexInterpolationNests(t *testing.T) {
	toks := mustLex(t, `"a\(1 + "b\(2)")c"`, jqSet())
	wantKinds(t, toks, StrStart, StrLit, InterpStart, Number, Plus, StrStart, StrLit, InterpStart, Number, InterpEnd, StrEnd, InterpEnd, StrLit, StrEnd, EOF)
}

func TestLexInterpolationGroupedParens(t *testing.T) {
	toks := mustLex(t, `"\((1))x"`, jqSet())
	wantKinds(t, toks, StrStart, InterpStart, LParen, Number, RParen, InterpEnd, StrLit, StrEnd, EOF)
}

func TestLexFormatString(t *testing.T) {
	toks := mustLex(t, `@base64 "x\(.y)"`, jqSet())
	wantEqual(t, toks[0].Kind, Format)
	wantEqual(t, toks[0].Text, "@base64")
}

func TestLexCommentContinuationJQ17(t *testing.T) {
	toks := mustLex(t, "1 # comment \\\n still comment\n+ 2", jqSet())
	wantKinds(t, toks, Number, Plus, Number, EOF)
}

func TestLexEvenBackslashesEndComment(t *testing.T) {
	toks := mustLex(t, "1 # c \\\\\n+ 2", jqSet())
	wantKinds(t, toks, Number, Plus, Number, EOF)
}

func TestLexNoContinuationForYQSet(t *testing.T) {
	toks := mustLex(t, "1 # c \\\n+ 2", yqSet())
	wantKinds(t, toks, Number, Plus, Number, EOF)
}

func TestLexContinuationSwallowsNextLineJQ(t *testing.T) {
	toks := mustLex(t, "1 # c \\\n+ 2", jqSet())
	wantKinds(t, toks, Number, EOF)
}

func TestLexLongestMatchOperators(t *testing.T) {
	toks := mustLex(t, `a //= b ?// c |= d`, jqSet())
	wantKinds(t, toks, Ident, AltAssign, Ident, AltDestructure, Ident, UpdateAssign, Ident, EOF)
}

func TestLexLocToken(t *testing.T) {
	wantEqual(t, mustLex(t, `$__loc__`, jqSet())[0].Kind, Loc)
	wantEqual(t, mustLex(t, `$__loc__`, yqSet())[0].Kind, Variable)
}

func TestLexUnterminatedString(t *testing.T) {
	_, err := Tokenize(`"abc`, jqSet())
	wantErrContains(t, err, "unterminated string")
}

func TestLexTripleDotOnlyWhenEnabled(t *testing.T) {
	wantKinds(t, mustLex(t, `...`, yqSet()), DotDotDot, EOF)
	wantKinds(t, mustLex(t, `...`, jqSet()), DotDot, Dot, EOF)
}

func TestLexKeywordsAndIdentifiers(t *testing.T) {
	toks := mustLex(t, `if a::b then .and else $x end`, jqSet())
	wantKinds(t, toks, Keyword, Ident, Keyword, Field, Keyword, Variable, Keyword, EOF)
	wantEqual(t, toks[1].Text, "a::b")
	wantEqual(t, toks[3].Text, ".and")
}

func TestLexNumbers(t *testing.T) {
	toks := mustLex(t, `1 1.5 .5 1e3 2E-2 3.`, jqSet())
	wantKinds(t, toks, Number, Number, Number, Number, Number, Number, EOF)
	wantEqual(t, toks[2].Text, ".5")
	wantEqual(t, toks[4].Text, "2E-2")
}

func TestLexStringEscapes(t *testing.T) {
	toks := mustLex(t, `"a\n\t\"\\é😀"`, jqSet())
	wantKinds(t, toks, StrStart, StrLit, StrEnd, EOF)
	wantEqual(t, toks[1].Text, "a\n\t\"\\é😀")
}

func TestLexInvalidEscape(t *testing.T) {
	_, err := Tokenize(`"\q"`, jqSet())
	wantErrContains(t, err, "invalid escape")
}

func TestLexEmptyString(t *testing.T) {
	wantKinds(t, mustLex(t, `""`, jqSet()), StrStart, StrEnd, EOF)
}

func TestLexPositions(t *testing.T) {
	toks := mustLex(t, `  .foo | 12`, jqSet())
	wantEqual(t, toks[0].Pos, 2)
	wantEqual(t, toks[0].End, 6)
	wantEqual(t, toks[2].Pos, 9)
	wantEqual(t, toks[2].End, 11)
}

func TestLexUnterminatedInterpolation(t *testing.T) {
	_, err := Tokenize(`"a\(1`, jqSet())
	wantErrContains(t, err, "unterminated")
}

func TestLexUnexpectedCharacter(t *testing.T) {
	_, err := Tokenize("1 ` 2", jqSet())
	wantErrContains(t, err, "unexpected character")
}

func TestLexEOFIsSticky(t *testing.T) {
	l := New(``, jqSet())
	for range 3 {
		tk, err := l.Next()
		if err != nil || tk.Kind != EOF {
			t.Fatalf("Next() = %v, %v; want EOF", tk, err)
		}
	}
}

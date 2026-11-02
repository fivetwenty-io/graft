package yamlprep

import (
	"strings"

	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/token"
)

// Tokenize returns the tokens goccy's lexer gives src, with two of the
// scanner's misreadings of a tab put right, at the positions goccy gives
// each token when it reads the text right. A double-quoted scalar that
// holds a tab between two characters reads as libyaml reads it, and so
// does a tag or an anchor with a tab after it. Both fixes start from the
// tokens of a copy of src with each tab made a space, which goccy reads
// right and which leaves every byte where it is.
//
// A tab after a tag or an anchor's name becomes a space, which YAML reads
// the same way there, and goes back into the source text of the tokens
// afterwards. A tab inside a double-quoted scalar becomes a character
// that src does not hold, which the scanner counts once and keeps in the
// value as it keeps the tab, and goes back into the value and the source
// text of the scalar afterwards. A tab that starts a continuation line,
// or ends one, stays as it is, since goccy reads those right. When the
// marker reaches any token other than a double-quoted scalar, the quoted
// tabs stay as they are.
func Tokenize(src string) token.Tokens {
	quoted, property := mayHoldQuotedTab(src), mayHoldPropertyTab(src)
	if !quoted && !property {
		return lexer.Tokenize(src)
	}
	neutral := strings.ReplaceAll(src, "\t", " ")
	scan := lexer.Tokenize(neutral)
	idx := newByteIndex(neutral)
	var separators []propertyTab
	if property {
		separators = propertyTabs(src, scan, idx)
	}
	text := spaceAt(src, separators)
	var toks token.Tokens
	if quoted {
		if tabs := doubledQuotedTabs(text, scan, idx); len(tabs) > 0 {
			toks, _ = tokenizeQuotedTabs(text, tabs)
		}
	}
	if toks == nil {
		toks = lexer.Tokenize(text)
	}
	keepPropertyTabs(toks, scan, text, separators)
	return toks
}

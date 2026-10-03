package yamlnode

import (
	"errors"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/token"
)

// coreTagKinds maps each core tag goccy checks against its node to the
// kind of node goccy requires under it. yaml.v3 makes no such check. It
// takes the kind from the node and keeps the tag, so "!!map" over a
// sequence is a sequence tagged "!!map".
var coreTagKinds = map[string]Kind{
	string(token.MappingTag):    MappingNode,
	string(token.SetTag):        MappingNode,
	string(token.SequenceTag):   SequenceNode,
	string(token.OrderedMapTag): SequenceNode,
	string(token.StringTag):     ScalarNode,
	string(token.IntegerTag):    ScalarNode,
	string(token.FloatTag):      ScalarNode,
	string(token.NullTag):       ScalarNode,
	string(token.BooleanTag):    ScalarNode,
	string(token.BinaryTag):     ScalarNode,
	string(token.TimestampTag):  ScalarNode,
}

// kindMismatchMessages holds the exact messages goccy gives when a core
// tag sits over a node of another kind.
var kindMismatchMessages = map[string]bool{
	"could not find map":                           true,
	"value is not allowed in this context":         true,
	"unexpected scalar value type":                 true,
	"unexpected scalar value":                      true,
	"mapping value is not allowed in this context": true,
}

// scalarTokenTypes holds the token types that start a scalar node.
var scalarTokenTypes = map[token.Type]bool{
	token.SingleQuoteType:   true,
	token.DoubleQuoteType:   true,
	token.NullType:          true,
	token.InfinityType:      true,
	token.NanType:           true,
	token.IntegerType:       true,
	token.BinaryIntegerType: true,
	token.OctetIntegerType:  true,
	token.HexIntegerType:    true,
	token.FloatType:         true,
	token.StringType:        true,
	token.BoolType:          true,
	token.LiteralType:       true,
	token.FoldedType:        true,
}

// neutralizeKindTags reports whether err is goccy refusing a core tag
// over a node of another kind. When it is, it returns text with every
// such tag replaced by a local tag of the same width, which goccy
// accepts over any node, and the original tags keyed by their line and
// column. It returns false for every other error, including any input
// where one tag directly follows another, which libyaml refuses.
func neutralizeKindTags(text string, err error) (string, map[[2]int]string, bool) {
	var se *yaml.SyntaxError
	if !errors.As(err, &se) || se.Token == nil || se.Token.Position == nil || !kindMismatchMessages[se.Message] {
		return "", nil, false
	}
	mismatched, ok := mismatchedTags(text, se.Token.Position)
	if !ok {
		return "", nil, false
	}
	return blankTags(text, mismatched)
}

// mismatchedTags returns every core tag in text over a node of another
// kind. It reports false unless the node under one of them starts at
// at, the token goccy blamed, or when one tag directly follows another.
func mismatchedTags(text string, at *token.Position) ([]*token.Token, bool) {
	var mismatched []*token.Token
	explains := false
	for _, tk := range lexer.Tokenize(text) {
		if tk.Type != token.TagType {
			continue
		}
		next := nextNonComment(tk)
		if next == nil {
			continue
		}
		kind, known, tagged := valueKind(tk, next)
		if tagged {
			return nil, false
		}
		want, core := coreTagKinds[tk.Value]
		if !core || !known || kind == want {
			continue
		}
		mismatched = append(mismatched, tk)
		if next.Position.Line == at.Line && next.Position.Column == at.Column {
			explains = true
		}
	}
	return mismatched, explains
}

// blankTags replaces each tag in tags with a local tag of the same
// width, so every token keeps its line and column. It returns the new
// text and the original tags keyed by line and column, and reports false
// when a tag is not where its token says it is.
func blankTags(text string, tags []*token.Token) (string, map[[2]int]string, bool) {
	lines := strings.SplitAfter(text, "\n")
	original := make(map[[2]int]string, len(tags))
	for _, tk := range tags {
		line, col := tk.Position.Line, tk.Position.Column
		if line < 1 || line > len(lines) || col < 1 {
			return "", nil, false
		}
		runes := []rune(lines[line-1])
		width := len([]rune(tk.Value))
		if col-1+width > len(runes) || string(runes[col-1:col-1+width]) != tk.Value {
			return "", nil, false
		}
		placeholder := "!" + strings.Repeat("x", width-1)
		lines[line-1] = string(runes[:col-1]) + placeholder + string(runes[col-1+width:])
		original[[2]int{line, col}] = tk.Value
	}
	return strings.Join(lines, ""), original, true
}

// nextNonComment returns the first token after tk that is not a comment.
func nextNonComment(tk *token.Token) *token.Token {
	next := tk.Next
	for next != nil && next.Type == token.CommentType {
		next = next.Next
	}
	return next
}

// valueKind returns the kind of the node that starts at next, the first
// token after the tag tk, skipping an anchor and its name. It reports
// whether the kind is known, and whether the node's first token is
// another tag. A token on a later line that is not indented under the
// tag's entry starts a sibling, and the tag's node is empty, so its kind
// is not known. A plain or quoted scalar followed on its own line by ":"
// is the first key of a block mapping when it sits below the tag. On the
// tag's line the tag belongs to that key, as it does in libyaml.
func valueKind(tk, next *token.Token) (Kind, bool, bool) {
	if next.Type == token.AnchorType {
		if next = next.Next; next != nil {
			next = nextNonComment(next)
		}
		if next == nil {
			return 0, false, false
		}
	}
	if next.Position.Line > tk.Position.Line && !nestedUnder(tk, next) {
		return 0, false, false
	}
	switch next.Type {
	case token.TagType:
		return 0, false, true
	case token.SequenceEntryType, token.SequenceStartType:
		return SequenceNode, true, false
	case token.MappingStartType, token.MappingKeyType:
		return MappingNode, true, false
	default:
		if !scalarTokenTypes[next.Type] {
			return 0, false, false
		}
		if after := next.Next; after != nil && after.Type == token.MappingValueType && after.Position.Line == next.Position.Line && next.Position.Line > tk.Position.Line {
			return MappingNode, true, false
		}
		return ScalarNode, true, false
	}
}

// nestedUnder reports whether next, on a later line than the tag tk, is
// indented far enough to start tk's node. It must sit to the right of the
// block entry that holds tk, except that a "-" may share the column of a
// mapping key, as a block sequence under a key may in YAML.
func nestedUnder(tk, next *token.Token) bool {
	owner := tk.Prev
	for owner != nil {
		switch {
		case owner.Type == token.CommentType || owner.Type == token.TagType || owner.Type == token.AnchorType:
			owner = owner.Prev
			continue
		case owner.Type == token.StringType && owner.Prev != nil && owner.Prev.Type == token.AnchorType:
			owner = owner.Prev.Prev // an anchor's name
			continue
		}
		break
	}
	indent, column := entryIndent(owner), next.Position.Column-1
	if column > indent {
		return true
	}
	return column == indent && next.Type == token.SequenceEntryType && owner.Type == token.MappingValueType
}

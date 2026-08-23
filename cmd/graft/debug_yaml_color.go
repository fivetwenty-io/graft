package main

import (
	"bytes"

	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/token"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
)

// yamlWhitespaceCutset is the set of bytes colorizeYAML treats as
// insignificant padding around a token's meaningful content: indentation,
// the single space after a mapping colon, and the newlines the lexer's
// scanner sometimes folds into a neighboring token's span (see the
// per-token span comment below). None of these ever carry a role's
// style, so mono's underline (rolePath/roleFile) never draws under
// indentation and no theme's color ever paints a line break.
const yamlWhitespaceCutset = " \t\r\n"

// writeYAML is cmdInspect's and cmdOutput's single choke point for
// writing a rendered YAML document to the session: raw bytes, unchanged,
// when color is off (so every existing bytes.Buffer test's plain-mode
// byte assertions keep matching exactly what graft.MarshalYAML
// produced), or colorizeYAML's token-styled rendering when color is on.
// The disabled branch never calls the lexer at all, so byte-identity in
// color-off mode is structural, not merely tested.
func (s *debugSession) writeYAML(raw []byte) {
	if !s.styler.enabled {
		_, _ = s.out.Write(raw)
		return
	}
	_, _ = s.out.Write(colorizeYAML(raw, s.styler))
}

// colorizeYAML renders raw YAML with st's theme applied to each token's
// semantic role, falling back to raw unchanged whenever it cannot prove
// the styled output round-trips back to raw with every escape sequence
// stripped. That fallback covers three things at once: a document the
// lexer cannot make sense of, a lexer/scanner quirk in the Position
// offsets it reports (see the per-token span comment below - measured
// against this module's goccy/go-yaml v1.19.2 in the research that fed
// this design), and any bug in this function itself. The identity
// property is therefore structural on both the color-off path (writeYAML
// never lexes) and this path (the strip-and-compare check below), not
// merely asserted by a test.
//
// raw already containing an ESC byte skips colorizing outright: the
// strip-and-compare safety net strips ANSI sequences to check for a
// match, and an ESC byte already present in the input (not one this
// function added) would defeat that check in either direction - it
// could get stripped along with ours and appear to "match" a corrupted
// reconstruction, or make a correct reconstruction appear mismatched.
// Neither failure mode is one this function can tell apart from the
// other, so it declines to colorize such input at all.
func colorizeYAML(raw []byte, st debugStyler) []byte {
	if bytes.IndexByte(raw, '\x1b') >= 0 {
		return raw
	}

	tokens := lexer.Tokenize(string(raw))
	starts, ok := yamlTokenStarts(raw, tokens)
	if !ok {
		return raw
	}

	styled := renderYAMLTokens(raw, tokens, starts, st)
	if ansi.StripEscapes(string(styled)) != string(raw) {
		return raw
	}
	return styled
}

// yamlTokenStarts resolves every token's start offset (0-based) in raw,
// converting the lexer's 1-based Token.Position.Offset, and validates
// that the sequence is usable for reconstruction: every offset must fall
// within raw, and offsets must never decrease from one token to the
// next. A lexer/scanner quirk that violates either property (an invalid
// token, or - measured during this design's research - the position
// drift a Comment token's trailing-newline lookahead introduces into
// every token after it, which can in principle compound past a later
// token's start) is exactly what this check exists to catch, handing the
// caller a clean "give up" signal instead of a corrupt slice.
func yamlTokenStarts(raw []byte, tokens token.Tokens) ([]int, bool) {
	starts := make([]int, len(tokens))
	prev := 0
	for i, tok := range tokens {
		if tok.Position == nil {
			return nil, false
		}
		start := tok.Position.Offset - 1
		if start < 0 || start > len(raw) || start < prev {
			return nil, false
		}
		starts[i] = start
		prev = start
	}
	return starts, true
}

// renderYAMLTokens builds raw's styled rendering from tokens' validated
// start offsets. Each token's span runs from its own start to the next
// token's start (or, for the last token, to the end of raw) - not from
// Token.Origin, which does not reliably round-trip source bytes (see
// colorizeYAML's doc comment). Concatenating every span in order always
// reproduces raw exactly, by construction, regardless of where within a
// span the lexer's own bookkeeping actually placed the token's
// meaningful content; the only cost of that imprecision is a styled
// boundary landing a character or two off from the "true" token edge in
// the rare inputs that trigger it, never a lost or duplicated byte.
//
// Within each span, leading and trailing whitespace is split off before
// the role's style is applied to the remaining core, so an indented
// line's leading spaces (and a mapping colon's single trailing space)
// never end up inside a styled span - the literal need behind mono's
// underline never drawing under indentation.
func renderYAMLTokens(raw []byte, tokens token.Tokens, starts []int, st debugStyler) []byte {
	var buf bytes.Buffer
	if len(starts) > 0 {
		buf.Write(raw[:starts[0]])
	} else {
		buf.Write(raw)
	}

	for i, tok := range tokens {
		end := len(raw)
		if i+1 < len(tokens) {
			end = starts[i+1]
		}
		span := raw[starts[i]:end]

		leadLen := len(span) - len(bytes.TrimLeft(span, yamlWhitespaceCutset))
		leading, rest := span[:leadLen], span[leadLen:]
		trailLen := len(rest) - len(bytes.TrimRight(rest, yamlWhitespaceCutset))
		core, trailing := rest[:len(rest)-trailLen], rest[len(rest)-trailLen:]

		buf.Write(leading)
		if role, has := roleForYAMLToken(tok); has && len(core) > 0 {
			buf.WriteString(st.apply(role, string(core)))
		} else {
			buf.Write(core)
		}
		buf.Write(trailing)
	}
	return buf.Bytes()
}

// roleForYAMLToken maps a lexer token to the semantic role it renders
// with (Category G / the YAML Colorizer section,
// plans/debugger-colorizing.md), or reports no role for a plain string,
// quoted string, tag, or structural punctuation token, which stays
// unstyled. Order matters: a mapping key is identified by what follows
// it (any token type immediately followed by ":"), which takes priority
// over every other rule, since a key can otherwise be any scalar type.
//
// Anchor and alias sigils ("&"/"*") are their own token, separate from
// the name that follows; both the sigil (matched by this token's own
// Type) and the name (matched by looking at PreviousType()) resolve to
// roleYAMLAnchor, so the two adjacent styled spans read as one colored
// unit with nothing unstyled between them.
//
// Timestamps are documented as part of roleYAMLLiteral's meaning (the
// role table, plans/debugger-colorizing.md), but this lexer has no
// distinct token type for one - a timestamp scans as an ordinary
// String - so there is nothing here to key a timestamp rule on without
// sniffing scalar content the lexer itself declined to classify; that
// is out of scope for this token-class mapper.
func roleForYAMLToken(tok *token.Token) (debugRole, bool) {
	switch {
	case tok.NextType() == token.MappingValueType:
		return roleYAMLKey, true
	case tok.Type == token.AnchorType || tok.Type == token.AliasType:
		return roleYAMLAnchor, true
	case tok.PreviousType() == token.AnchorType || tok.PreviousType() == token.AliasType:
		return roleYAMLAnchor, true
	case tok.Type == token.CommentType || tok.Type == token.DocumentHeaderType || tok.Type == token.DocumentEndType:
		return roleYAMLComment, true
	case isYAMLLiteralType(tok.Type):
		return roleYAMLLiteral, true
	default:
		return 0, false
	}
}

// isYAMLLiteralType reports whether t is one of the scalar types
// roleYAMLLiteral covers: every integer base, float, bool, and the two
// null spellings the lexer distinguishes (an explicit "null"/"~" versus
// an implicit empty value). Infinity and Nan are float-family literals
// this lexer tokenizes distinctly and belong in the same role.
func isYAMLLiteralType(t token.Type) bool {
	switch t {
	case token.BoolType,
		token.IntegerType,
		token.BinaryIntegerType,
		token.OctetIntegerType,
		token.HexIntegerType,
		token.FloatType,
		token.NullType,
		token.ImplicitNullType,
		token.InfinityType,
		token.NanType:
		return true
	default:
		return false
	}
}

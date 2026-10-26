package yq

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/fivetwenty-io/graft/internal/query/lexer"
	"github.com/fivetwenty-io/graft/pkg/regex"
)

// namePatterns are the rules of yq's lexer that can match where a name,
// a keyword, or a format starts, in the order yq tries them. yq takes the
// first rule that matches, not the longest, so "paths" lexes as path
// followed by an "s" no rule matches, and "ascii_downcase" as "as"
// followed by "cii_downcase". The number rules are here because a name
// can run on into digits that yq lexes as a number.
var namePatterns = []string{
	`line_?comment|lineComment`,
	`head_?comment|headComment`,
	`foot_?comment|footComment`,
	`as`,
	`ref`,
	`length`,
	`line`,
	`column`,
	`eval`,
	`to_?number`,
	`map_?values`,
	`map`,
	`filter`,
	`pick`,
	`omit`,
	`flatten\([0-9]+\)`,
	`flatten`,
	`format_datetime`,
	`now`,
	`tz`,
	`from_?unix`,
	`to_?unix`,
	`with_dtf`,
	`error`,
	`shuffle`,
	`sortKeys`,
	`sort_?keys`,
	`array_?to_?map`,
	`root`,
	`to_?yaml\([0-9]+\)`,
	`to_?xml\([0-9]+\)`,
	`to_?json\([0-9]+\)`,
	`from_?yaml|@yamld|from_?json|@jsond`,
	`to_?yaml|@yaml`,
	`to_?json`,
	`@json`,
	`from_?props|@propsd`,
	`to_?props|@props`,
	`from_?xml|@xmld`,
	`to_?xml`,
	`@xml`,
	`from_?csv|@csvd`,
	`to_?csv|@csv`,
	`from_?tsv|@tsvd`,
	`to_?tsv|@tsv`,
	`@base64urld`,
	`@base64url`,
	`@base64d`,
	`@base64`,
	`@urid`,
	`@uri`,
	`@sh`,
	`load_?xml|xml_?load`,
	`load_?base64`,
	`load_?props`,
	`load_?str|str_?load`,
	`load`,
	`system`,
	`splitDoc|split_?doc`,
	`select`,
	`has`,
	`unique_?by`,
	`unique`,
	`group_?by`,
	`explode`,
	`or`,
	`and`,
	`not`,
	`ireduce`,
	`join`,
	`sub`,
	`match`,
	`capture`,
	`test`,
	`sort_?by`,
	`sort`,
	`first`,
	`reverse`,
	`any_c`,
	`any`,
	`all_c`,
	`all`,
	`contains`,
	`split`,
	`parents`,
	`parent\(-?[0-9]+\)`,
	`parent`,
	`keys`,
	`key`,
	`is_?key`,
	`file_?name|fileName`,
	`file_?index|fileIndex|fi`,
	`path`,
	`set_?path`,
	`del_?paths`,
	`to_?entries|toEntries`,
	`from_?entries|fromEntries`,
	`with_?entries|withEntries`,
	`with`,
	`collect`,
	`del`,
	styleName,
	`tag|type`,
	`kind`,
	anchorName,
	aliasName,
	`comments\s*=`,
	`comments\s*\|=`,
	`if`,
	`then`,
	`elif`,
	`else`,
	`end`,
	`documentIndex|document_?index|di`,
	`upcase|ascii_?upcase`,
	`downcase|ascii_?downcase`,
	`trim`,
	`to_?string`,
	`0[xX][0-9A-Fa-f]+`,
	`-?[1-9](\.\d+)?[Ee][-+]?\d+`,
	`-?\d+(\.\d+)`,
	`-?\d+`,
	`[Tt][Rr][Uu][Ee]`,
	`[Ff][Aa][Ll][Ss][Ee]`,
	`[Nn][Uu][Ll][Ll]|~`,
	`strenv\([^\)]+\)`,
	`env\([^\)]+\)`,
	`envsubst\((ne|nu|ff| |,)+\)`,
	`envsubst`,
	`min`,
	`max`,
	`pivot`,
}

var nameRules = func() []*regex.Pattern {
	rules := make([]*regex.Pattern, len(namePatterns))
	for i, p := range namePatterns {
		rules[i] = regex.MustCompile(`\A(?:` + p + `)`)
	}
	return rules
}()

// internalPrefix starts the names of the natives that the yq preludes
// call. yq has no such names, but they are ours to call, so the lexer's
// rules pass them.
const internalPrefix = "_yq_"

// unlexable reports the first name, keyword, or format in expr that yq's
// lexer refuses, worded as yq words it, with the byte offset it names.
// Inside an interpolated string, yq lexes each interpolation as an
// expression of its own, so the position and the text quoted count from
// where that interpolation starts. A rule that matches on past its token,
// as "env(NAME)" does, lexes the tokens it covers too.
func unlexable(expr string, toks []lexer.Token) (string, int, bool) {
	var frames [][2]int
	lexed := 0
	for i, tok := range toks {
		switch tok.Kind {
		case lexer.InterpStart:
			frames = append(frames, [2]int{tok.End, interpEnd(toks, i)})
			continue
		case lexer.InterpEnd:
			if len(frames) > 0 {
				frames = frames[:len(frames)-1]
			}
			continue
		case lexer.Ident, lexer.Keyword, lexer.Format:
		default:
			continue
		}
		if tok.Pos < lexed {
			continue
		}
		if tok.Kind == lexer.Ident && strings.HasPrefix(tok.Text, internalPrefix) {
			continue
		}
		base, limit := 0, len(expr)
		if n := len(frames); n > 0 {
			base, limit = frames[n-1][0], frames[n-1][1]
		}
		at, refused := refusedAt(expr[:limit], tok)
		if refused {
			return lexerError(expr[base:limit], at-base), at, true
		}
		lexed = at
	}
	return "", 0, false
}

// interpEnd returns the offset of the InterpEnd that closes the
// interpolation opened at toks[open].
func interpEnd(toks []lexer.Token, open int) int {
	depth := 0
	for _, tok := range toks[open:] {
		switch tok.Kind {
		case lexer.InterpStart:
			depth++
		case lexer.InterpEnd:
			depth--
			if depth == 0 {
				return tok.Pos
			}
		default:
		}
	}
	return toks[len(toks)-1].End
}

// refusedAt lexes the text of tok the way yq does, rule by rule from the
// left, and returns the offset where no rule matches. A rule may match on
// past the token, as "flatten(1)" and "comments =" do, and the token then
// lexes. When every rule matches, it returns the offset where lexing
// stopped.
func refusedAt(src string, tok lexer.Token) (int, bool) {
	at := tok.Pos
	for at < tok.End {
		n := 0
		for _, re := range nameRules {
			if m, err := re.Find(context.Background(), src[at:], 0); err == nil && m != nil {
				n = m.End
				break
			}
		}
		if n == 0 {
			return at, true
		}
		at += n
	}
	return at, false
}

// lexerError is yq's message for text it cannot lex at offset at of src:
// the line and the column in runes, both from 1, and the text left, cut
// to 16 runes.
func lexerError(src string, at int) string {
	before := src[:at]
	line := 1 + strings.Count(before, "\n")
	col := 1 + utf8.RuneCountInString(before[strings.LastIndex(before, "\n")+1:])
	rest := []rune(src[at:])
	if len(rest) > 16 {
		rest = append(rest[:16], []rune("...")...)
	}
	return fmt.Sprintf("%d:%d: lexer: invalid input text %q", line, col, string(rest))
}

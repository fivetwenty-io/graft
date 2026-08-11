package controlflow

import (
	"fmt"
	"regexp"
	"strings"
)

// maxBlockNestingDepth bounds control-flow block nesting, matching the
// parser's own expression nesting cap (pkg/graft's unexported
// maxNestingDepth, spec decision C-23). Duplicated rather than imported
// because the parser's constant is unexported and pkg/graft/controlflow
// must not import pkg/graft's internals beyond its public API.
const maxBlockNestingDepth = 64

// keywordAliases maps alternative keyword spellings to their canonical
// form, matching docs/user-guide/operators/control-flow.md's aliases and
// pkg/graft/interfaces/keywords.go's (currently unused) alias table.
var keywordAliases = map[string]string{
	"elsif":    "elif",
	"endif":    "fi",
	"endfor":   "done",
	"endwhile": "done",
	"endcase":  "esac",
}

var startKeywords = map[string]bool{"if": true, "for": true, "while": true, "case": true}
var middleKeywords = map[string]bool{"elif": true, "else": true, "when": true, "default": true}
var endKeywords = map[string]bool{"fi": true, "done": true, "esac": true}

// canonicalKeyword resolves aliases to their canonical spelling. Non-alias
// input is returned unchanged (already lowercased by the caller).
func canonicalKeyword(kw string) string {
	if c, ok := keywordAliases[kw]; ok {
		return c
	}
	return kw
}

// isControlFlowKeyword reports whether kw (already canonicalized and
// lowercased) is a recognized control-flow start, middle, or end keyword.
func isControlFlowKeyword(kw string) bool {
	return startKeywords[kw] || middleKeywords[kw] || endKeywords[kw]
}

// blockScalarStartRe matches a line that opens a YAML block scalar: a
// mapping key (optionally under a "- " sequence-item prefix) followed by
// ":", optional whitespace, and a "|" or ">" block-scalar indicator with an
// optional chomping ("+"/"-") and indentation digit, then an optional
// trailing comment. This is intentionally approximate rather than a full
// YAML grammar — good enough to keep control-flow markers embedded in a
// block scalar (an operator string, a script, another templating
// language's own "(( if ))"-shaped syntax) from being misread as graft's
// own markers (spec decision C-21).
var blockScalarStartRe = regexp.MustCompile(`:\s*[|>][+-]?\d?\s*(#.*)?$`)

// scanLine is one line of source annotated with whether it is a
// control-flow marker line and, if so, its canonical keyword and the raw
// text following the keyword (trimmed, with the closing "))" removed).
type scanLine struct {
	index     int // 0-based line number in the original source
	text      string
	isMarker  bool
	keyword   string // canonical: "if", "elif", "else", "fi", "for", "while", "done", "case", "when", "default", "esac"
	remainder string
}

// matchMarker reports whether trimmed is one balanced "(( ... ))" group
// spanning the whole string apart from an optional trailing YAML comment,
// honoring quoted strings and nested parentheses within the group. A marker
// is confined to a single line by construction here (spec decision C-24:
// multi-line markers unsupported) — matchMarker only ever sees one line's
// text, so it cannot match across a line break.
func matchMarker(trimmed string) (raw string, ok bool) {
	if !strings.HasPrefix(trimmed, "((") {
		return "", false
	}
	n := len(trimmed)
	i := 2
	depth := 0
	for i < n {
		switch trimmed[i] {
		case '"':
			j := skipQuoted(trimmed, i, '"')
			if j < 0 {
				return "", false
			}
			i = j
		case '\'':
			j := skipQuoted(trimmed, i, '\'')
			if j < 0 {
				return "", false
			}
			i = j
		case '(':
			depth++
			i++
		case ')':
			if depth == 0 {
				if i+1 < n && trimmed[i+1] == ')' {
					if isMarkerTail(trimmed[i+2:]) {
						return strings.TrimSpace(trimmed[2:i]), true
					}
					return "", false // trailing content after the closing "))"
				}
				return "", false // unbalanced close at depth 0
			}
			depth--
			i++
		default:
			i++
		}
	}
	return "", false // reached end of line without a balanced close
}

// isMarkerTail reports whether rest — everything after a marker's closing
// "))" — leaves the line still a marker: either nothing at all, or a YAML
// comment. YAML only starts a comment at a "#" preceded by whitespace, so
// "(( fi ))#x" is an ordinary scalar and not a marker, while "(( fi )) # x"
// is a marker with a comment.
func isMarkerTail(rest string) bool {
	if rest == "" {
		return true
	}
	if rest[0] != ' ' && rest[0] != '\t' {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(rest), "#")
}

// skipQuoted returns the index just past the closing quote matching the
// quote character at trimmed[start], honoring backslash escapes, or -1 if
// the quoted string is not terminated on this line.
func skipQuoted(trimmed string, start int, quote byte) int {
	n := len(trimmed)
	j := start + 1
	for j < n {
		if trimmed[j] == '\\' && j+1 < n {
			j += 2
			continue
		}
		if trimmed[j] == quote {
			return j + 1
		}
		j++
	}
	return -1
}

// splitKeyword splits raw marker content ("if grab x == \"y\"", "fi", "for
// svc in services") into its lowercased first token and the remainder.
func splitKeyword(raw string) (keyword, remainder string) {
	raw = strings.TrimSpace(raw)
	idx := strings.IndexFunc(raw, func(r rune) bool { return r == ' ' || r == '\t' })
	if idx < 0 {
		return strings.ToLower(raw), ""
	}
	return strings.ToLower(raw[:idx]), strings.TrimSpace(raw[idx:])
}

// leadingSpaces returns the number of leading ASCII space characters in s.
// YAML indentation is spaces-only (tabs are illegal as YAML indentation),
// so this deliberately does not treat tabs as indentation.
func leadingSpaces(s string) int {
	i := 0
	for i < len(s) && s[i] == ' ' {
		i++
	}
	return i
}

// classifyLines splits source into lines and annotates each with whether it
// is a control-flow marker, skipping any line inside a YAML block scalar
// (spec decision C-21). Block-scalar tracking uses the opening line's
// leading-space count as the scalar's reference indent: a line is still
// inside the block scalar while it is blank or more deeply indented than
// that reference; the first non-blank line at or below the reference indent
// ends it.
func classifyLines(source string) []scanLine {
	// A trailing "\n" would otherwise leave strings.Split with a spurious
	// final empty element, becoming a phantom trailing itemRaw with nothing
	// in it. render's own output always ends with exactly one "\n" (see
	// expander.render), so nothing downstream needs that artifact preserved.
	source = strings.TrimSuffix(source, "\n")
	rawLines := strings.Split(source, "\n")
	lines := make([]scanLine, len(rawLines))

	inBlockScalar := false
	blockScalarIndent := 0

	for i, text := range rawLines {
		lines[i] = scanLine{index: i, text: text}

		if inBlockScalar {
			isBlank := strings.TrimSpace(text) == ""
			if isBlank || leadingSpaces(text) > blockScalarIndent {
				continue
			}
			inBlockScalar = false
		}

		trimmed := strings.TrimSpace(text)
		if raw, ok := matchMarker(trimmed); ok {
			kw, rest := splitKeyword(raw)
			canon := canonicalKeyword(kw)
			if isControlFlowKeyword(canon) {
				lines[i].isMarker = true
				lines[i].keyword = canon
				lines[i].remainder = rest
				continue
			}
		}

		if blockScalarStartRe.MatchString(text) {
			inBlockScalar = true
			blockScalarIndent = leadingSpaces(text)
		}
	}

	return lines
}

// hasControlFlowMarkers reports whether any line in lines was classified as
// a control-flow marker. Used as the preprocessor's fast no-op path: a
// document with no markers is returned byte-identical to its input.
func hasControlFlowMarkers(lines []scanLine) bool {
	for _, l := range lines {
		if l.isMarker {
			return true
		}
	}
	return false
}

// itemKind discriminates the variant stored in an item.
type itemKind int

const (
	itemRaw itemKind = iota
	itemIf
	itemFor
	itemWhile
	itemCase
)

// item is one element of a block's body: either a run of verbatim body
// lines, or a nested control-flow block.
type item struct {
	kind itemKind

	// itemRaw
	rawLines []string

	// itemIf
	clauses []ifClause

	// itemFor
	loopVars    []string // 1 or 2 entries
	iterableRaw string

	// itemWhile
	condRaw string

	// itemFor / itemWhile
	body []item

	// itemCase
	subjectRaw  string
	whens       []caseClause
	defaultBody []item
	hasDefault  bool

	// Source location of the block's opening and closing marker lines
	// (0-based), used for error messages and for --pinning golden tests.
	// Meaningless (zero) for itemRaw.
	startLine int
	endLine   int
}

// ifClause is one if/elif/else branch.
type ifClause struct {
	kind    string // "if", "elif", "else"
	condRaw string // empty for "else"
	body    []item
}

// caseClause is one when-branch of a case block.
type caseClause struct {
	patterns []string // raw pattern tokens, split on unquoted "|"
	body     []item
}

// blockParser is a recursive-descent parser over classified lines that
// builds the item tree described in scanner.go's doc comment.
type blockParser struct {
	lines []scanLine
	pos   int
	depth int
}

// parseDocument parses the full line stream into a top-level item list.
// Returns an error for any orphaned middle/end marker, any block missing
// its terminator, or nesting beyond maxBlockNestingDepth.
func parseDocument(lines []scanLine) ([]item, error) {
	p := &blockParser{lines: lines}
	items, err := p.parseItems()
	if err != nil {
		return nil, err
	}
	if p.pos < len(p.lines) {
		// parseItems only returns early (before EOF) for a stop keyword,
		// and the top level has none — reaching here would be a bug, not
		// a user-facing condition.
		return nil, fmt.Errorf("internal error: control flow parser stopped before end of input at line %d", p.lines[p.pos].index+1)
	}
	return items, nil
}

// parseItems consumes lines until a stop keyword (if any) or EOF, appending
// itemRaw for contiguous non-marker runs and recursing into nested blocks
// for start keywords. It does not consume the stop-keyword line itself; the
// caller inspects and consumes it. An empty stopKeywords set means "top
// level": any middle/end keyword encountered there is an orphan and is an
// error, and EOF is a valid, successful stop.
func (p *blockParser) parseItems(stopKeywords ...string) ([]item, error) {
	var items []item
	var rawBuf []string

	flush := func() {
		if len(rawBuf) > 0 {
			items = append(items, item{kind: itemRaw, rawLines: rawBuf})
			rawBuf = nil
		}
	}

	stop := make(map[string]bool, len(stopKeywords))
	for _, k := range stopKeywords {
		stop[k] = true
	}

	for p.pos < len(p.lines) {
		ln := p.lines[p.pos]
		if !ln.isMarker {
			rawBuf = append(rawBuf, ln.text)
			p.pos++
			continue
		}

		if stop[ln.keyword] {
			flush()
			return items, nil
		}

		switch ln.keyword {
		case "if":
			flush()
			blk, err := p.parseIf()
			if err != nil {
				return nil, err
			}
			items = append(items, blk)
		case "for":
			flush()
			blk, err := p.parseFor()
			if err != nil {
				return nil, err
			}
			items = append(items, blk)
		case "while":
			flush()
			blk, err := p.parseWhile()
			if err != nil {
				return nil, err
			}
			items = append(items, blk)
		case "case":
			flush()
			blk, err := p.parseCase()
			if err != nil {
				return nil, err
			}
			items = append(items, blk)
		default:
			flush()
			return nil, fmt.Errorf("(( %s )) at line %d has no matching block start", ln.keyword, ln.index+1)
		}
	}

	flush()
	if len(stopKeywords) > 0 {
		return nil, fmt.Errorf("unclosed block: expected (( %s )), reached end of input", strings.Join(stopKeywords, " )) or (( "))
	}
	return items, nil
}

// enterBlock increments the nesting depth counter and enforces
// maxBlockNestingDepth. Every parseX block-start method must call this
// exactly once, paired with a deferred p.depth--.
func (p *blockParser) enterBlock(startLine int) error {
	p.depth++
	if p.depth > maxBlockNestingDepth {
		return fmt.Errorf("control flow block nesting too deep (max %d) at line %d", maxBlockNestingDepth, startLine+1)
	}
	return nil
}

func (p *blockParser) parseIf() (item, error) {
	start := p.lines[p.pos]
	if err := p.enterBlock(start.index); err != nil {
		return item{}, err
	}
	defer func() { p.depth-- }()

	firstRaw := start.remainder
	p.pos++

	body, err := p.parseItems("elif", "else", "fi")
	if err != nil {
		return item{}, err
	}
	clauses := []ifClause{{kind: "if", condRaw: firstRaw, body: body}}

	elseSeen := false
	for {
		if p.pos >= len(p.lines) {
			return item{}, fmt.Errorf("unclosed (( if )) block starting at line %d: missing (( fi ))", start.index+1)
		}
		cur := p.lines[p.pos]
		switch cur.keyword {
		case "elif":
			if elseSeen {
				return item{}, fmt.Errorf("(( elif )) at line %d follows (( else )), which is not allowed", cur.index+1)
			}
			p.pos++
			b, err := p.parseItems("elif", "else", "fi")
			if err != nil {
				return item{}, err
			}
			clauses = append(clauses, ifClause{kind: "elif", condRaw: cur.remainder, body: b})
		case "else":
			if elseSeen {
				return item{}, fmt.Errorf("duplicate (( else )) at line %d", cur.index+1)
			}
			elseSeen = true
			p.pos++
			// "elif" and "else" stop the else-branch body too, so a
			// misplaced clause returns here and gets the diagnostic above
			// rather than parseItems' generic orphan-marker message.
			b, err := p.parseItems("elif", "else", "fi")
			if err != nil {
				return item{}, err
			}
			clauses = append(clauses, ifClause{kind: "else", body: b})
		case "fi":
			end := cur.index
			p.pos++
			return item{kind: itemIf, clauses: clauses, startLine: start.index, endLine: end}, nil
		default:
			return item{}, fmt.Errorf("internal error: unexpected (( %s )) at line %d while parsing if block", cur.keyword, cur.index+1)
		}
	}
}

// forHeaderRe parses a for-loop header: one or two comma-separated
// identifiers, "in", and the remaining iterable text.
var forHeaderRe = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*(?:,\s*([A-Za-z_][A-Za-z0-9_]*)\s*)?in\s+(.+)$`)

func (p *blockParser) parseFor() (item, error) {
	start := p.lines[p.pos]
	if err := p.enterBlock(start.index); err != nil {
		return item{}, err
	}
	defer func() { p.depth-- }()

	m := forHeaderRe.FindStringSubmatch(start.remainder)
	if m == nil {
		return item{}, fmt.Errorf("invalid (( for )) header at line %d: expected \"for VAR [, VAR] in ITERABLE\", got %q", start.index+1, start.remainder)
	}
	vars := []string{m[1]}
	if m[2] != "" {
		vars = append(vars, m[2])
	}
	iterable := m[3]
	p.pos++

	body, err := p.parseItems("done")
	if err != nil {
		return item{}, err
	}
	if p.pos >= len(p.lines) {
		return item{}, fmt.Errorf("unclosed (( for )) block starting at line %d: missing (( done ))", start.index+1)
	}
	end := p.lines[p.pos].index
	p.pos++

	return item{kind: itemFor, loopVars: vars, iterableRaw: iterable, body: body, startLine: start.index, endLine: end}, nil
}

func (p *blockParser) parseWhile() (item, error) {
	start := p.lines[p.pos]
	if err := p.enterBlock(start.index); err != nil {
		return item{}, err
	}
	defer func() { p.depth-- }()

	cond := start.remainder
	p.pos++

	body, err := p.parseItems("done")
	if err != nil {
		return item{}, err
	}
	if p.pos >= len(p.lines) {
		return item{}, fmt.Errorf("unclosed (( while )) block starting at line %d: missing (( done ))", start.index+1)
	}
	end := p.lines[p.pos].index
	p.pos++

	return item{kind: itemWhile, condRaw: cond, body: body, startLine: start.index, endLine: end}, nil
}

func (p *blockParser) parseCase() (item, error) {
	start := p.lines[p.pos]
	if err := p.enterBlock(start.index); err != nil {
		return item{}, err
	}
	defer func() { p.depth-- }()

	subject := start.remainder
	p.pos++

	var whens []caseClause
	var defaultBody []item
	hasDefault := false

	for {
		if p.pos >= len(p.lines) {
			return item{}, fmt.Errorf("unclosed (( case )) block starting at line %d: missing (( esac ))", start.index+1)
		}
		cur := p.lines[p.pos]
		switch cur.keyword {
		case "when":
			if hasDefault {
				return item{}, fmt.Errorf("(( when )) at line %d follows (( default )), which must be last", cur.index+1)
			}
			patterns := splitPatterns(cur.remainder)
			p.pos++
			b, err := p.parseItems("when", "default", "esac")
			if err != nil {
				return item{}, err
			}
			whens = append(whens, caseClause{patterns: patterns, body: b})
		case "default":
			if hasDefault {
				return item{}, fmt.Errorf("duplicate (( default )) at line %d", cur.index+1)
			}
			hasDefault = true
			p.pos++
			// "when" and "default" stop the default-branch body too, so a
			// misplaced clause returns here and gets the C-15/C-16
			// diagnostics above rather than a generic orphan-marker message.
			b, err := p.parseItems("when", "default", "esac")
			if err != nil {
				return item{}, err
			}
			defaultBody = b
		case "esac":
			end := cur.index
			p.pos++
			return item{kind: itemCase, subjectRaw: subject, whens: whens, defaultBody: defaultBody, hasDefault: hasDefault, startLine: start.index, endLine: end}, nil
		default:
			return item{}, fmt.Errorf("internal error: unexpected (( %s )) at line %d while parsing case block", cur.keyword, cur.index+1)
		}
	}
}

// splitPatterns splits a when-clause's raw text on unquoted "|" separators
// (e.g. `"prod" | "production"`), trimming whitespace around each pattern.
func splitPatterns(raw string) []string {
	var patterns []string
	var buf strings.Builder
	n := len(raw)
	for i := 0; i < n; i++ {
		c := raw[i]
		switch c {
		case '"':
			j := skipQuoted(raw, i, '"')
			if j < 0 {
				buf.WriteString(raw[i:])
				i = n
				break
			}
			buf.WriteString(raw[i:j])
			i = j - 1
		case '\'':
			j := skipQuoted(raw, i, '\'')
			if j < 0 {
				buf.WriteString(raw[i:])
				i = n
				break
			}
			buf.WriteString(raw[i:j])
			i = j - 1
		case '|':
			patterns = append(patterns, strings.TrimSpace(buf.String()))
			buf.Reset()
		default:
			buf.WriteByte(c)
		}
	}
	if s := strings.TrimSpace(buf.String()); s != "" || len(patterns) > 0 {
		patterns = append(patterns, s)
	}
	return patterns
}

package controlflow

import (
	"strings"

	"github.com/fivetwenty-io/graft/internal/exprlex"
	"github.com/fivetwenty-io/graft/pkg/graft"
	"github.com/fivetwenty-io/graft/pkg/graft/interfaces"
	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// opcallSpan locates one "(( ... ))" occurrence within a single line: the
// outer bounds include the delimiters, the inner bounds are the content
// between them.
type opcallSpan struct {
	outerStart, outerEnd int
	innerStart, innerEnd int
}

// findOpcallSpans scans a single line left to right and returns every
// balanced "(( ... ))" span it contains, honoring quoted strings and nested
// parentheses within each span exactly like matchMarker does for whole
// lines. An unterminated "((" (no matching "))" before the line ends) stops
// the scan at that point; whatever spans were already found are returned.
func findOpcallSpans(line string) []opcallSpan {
	var spans []opcallSpan
	n := len(line)
	i := 0
	for i+1 < n {
		if line[i] != '(' || line[i+1] != '(' {
			i++
			continue
		}
		innerStart := i + 2
		j := innerStart
		depth := 0
		closed := -1
		for j < n {
			c := line[j]
			switch c {
			case '"', '\'':
				k := skipQuoted(line, j, c)
				if k < 0 {
					j = n
					continue
				}
				j = k
				continue
			case '(':
				depth++
				j++
				continue
			case ')':
				if depth == 0 {
					if j+1 < n && line[j+1] == ')' {
						closed = j
					}
				} else {
					depth--
				}
				j++
				continue
			default:
				j++
			}
			if closed >= 0 {
				break
			}
		}
		if closed < 0 {
			break
		}
		spans = append(spans, opcallSpan{outerStart: i, outerEnd: closed + 2, innerStart: innerStart, innerEnd: closed})
		i = closed + 2
	}
	return spans
}

// nameSpan marks a name in an expression that the evaluator resolves
// from the top of the document. start and end are byte offsets into the
// expression.
type nameSpan struct {
	start, end int

	// rooted marks the first segment of a "$." path, which names a
	// top-level key from the document root.
	rooted bool

	// bare marks a name the lexer read as a whole identifier token.
	bare bool

	// tokenEnd is the byte offset where the reference token that holds
	// a rooted name ends, so the name and the rest of its path are
	// expr[start:tokenEnd].
	tokenEnd int

	// calc marks a name inside the quoted expression of a calc call,
	// which calc's own expression language reads. dotted marks such a
	// name that starts a dotted path, as "svc" in "svc.n * 2".
	calc, dotted bool
}

// newExprLexer returns a tokenizer over expr with the options the
// expression parser uses. engine resolves operator names, which decide
// whether a '/' right after a name begins a reference. A nil engine uses
// the default registry.
func newExprLexer(expr string, engine graft.Engine) *interfaces.AdvancedTokenizer {
	return interfaces.NewAdvancedTokenizer(expr, exprlex.Options(func(name string) bool {
		_, isNull := graft.OperatorForEngine(engine, name).(graft.NullOperator)
		return !isNull
	}))
}

// pathNameSpans lexes expr with the tokenizer and the options the
// expression parser uses, and returns every name in it that the
// evaluator resolves from the top of the document, in order. That is
// each identifier, the first segment of each reference path, and the
// first segment of the path inside each bracket index. Because the
// lexer itself decides where each reference starts and ends, a name
// inside a quoted string, an environment variable, a key that continues
// a path, or the value side of a "field=value" selector is never
// returned, and neither is the modifier or target name of an operator
// head, such as "prod" in "vault@prod". engine resolves operator names,
// which decide whether a '/' right after a name begins a reference. A
// nil engine uses the default registry.
//
// calls, when non-nil, maps the offset of each identifier the parser
// reads as the name of an operator call to that name, as
// graft.OperatorCallNames returns it. Such an identifier names no key,
// so it is left out. A string right after a calc call's name holds an
// expression in calc's own language, and the names calc reads in it are
// returned as well (calcNameSpans).
func pathNameSpans(expr string, engine graft.Engine, calls map[int]string) []nameSpan {
	tz := newExprLexer(expr, engine)
	var spans []nameSpan
	afterCalc := false
	for tz.HasMore() {
		before := tz.Position()
		tok := tz.NextToken()
		if tok.Type == interfaces.TokenEOF || tz.Position() <= before {
			break
		}
		calcString := afterCalc
		afterCalc = false
		switch tok.Type {
		case interfaces.TokenIdentifier:
			if tz.HeadName() {
				// The modifier or target of an operator head, as
				// "prod" in "vault@prod", names no key.
				continue
			}
			if name, ok := calls[tok.Pos.Offset]; ok {
				afterCalc = name == "calc"
				continue
			}
			spans = append(spans, nameSpan{start: tok.Pos.Offset, end: tok.Pos.Offset + len(tok.Literal), bare: true})
		case interfaces.TokenReference:
			spans = append(spans, referenceNameSpans(tok.Literal, tok.Pos.Offset)...)
		case interfaces.TokenString, interfaces.TokenRawString:
			if calcString {
				spans = append(spans, calcNameSpans(tok.Literal, tok.Pos.Offset)...)
			}
		default:
		}
	}
	return spans
}

// calcNameSpans returns the names that calc reads from the document in
// lit, a quoted string token that starts at byte offset base and holds a
// calc expression. calc reads a name that starts with a letter and runs
// on with letters, digits, '_', and '.', and it reads every dotted part
// of it as one path, so only the part before the first '.' is returned,
// marked dotted when a path follows it. A name inside a quoted string of
// the expression, inside an escaped "[...]" name, or before a '(' that
// makes it a function call is left out, and so are the keywords true,
// false, in, and IN. A backslash before a quote in lit is read as that
// quote, because the string's value holds the quote on its own.
func calcNameSpans(lit string, base int) []nameSpan {
	if len(lit) < 2 || (lit[0] != '"' && lit[0] != '\'') || lit[len(lit)-1] != lit[0] {
		return nil
	}
	content := lit[1 : len(lit)-1]
	var spans []nameSpan
	var quote byte
	for i := 0; i < len(content); {
		if quote != 0 || !isCalcWordByte(content[i]) {
			if i = skipCalcByte(content, i, &quote); i < 0 {
				break
			}
			continue
		}
		start := i
		for i < len(content) && (isCalcWordByte(content[i]) || content[i] == '.') {
			i++
		}
		if sp, ok := calcName(content, start, i); ok {
			sp.start += base + 1
			sp.end += base + 1
			spans = append(spans, sp)
		}
	}
	return spans
}

// skipCalcByte steps past the byte of a calc expression at content[i]
// that does not begin a name, and returns where the scan goes on, or -1
// when an escaped "[...]" name never closes. quote holds the quote
// that opened the string the scan is in, or 0 outside one. A
// backslash and the byte after it are stepped past together, and an
// escaped quote counts as that quote.
func skipCalcByte(content string, i int, quote *byte) int {
	ch := content[i]
	i++
	if ch == '\\' && i < len(content) {
		ch = content[i]
		i++
		if ch != '"' && ch != '\'' {
			return i
		}
	}
	switch {
	case *quote != 0:
		if ch == *quote {
			*quote = 0
		}
	case ch == '"' || ch == '\'':
		*quote = ch
	case ch == '[':
		closing := strings.IndexByte(content[i:], ']')
		if closing < 0 {
			return -1
		}
		return i + closing + 1
	}
	return i
}

// calcName reports whether content[start:end], a run of word bytes and
// dots, is a name calc reads from the document, and returns the span of
// its first dotted part.
func calcName(content string, start, end int) (nameSpan, bool) {
	c := content[start]
	if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
		return nameSpan{}, false
	}
	if start > 0 && content[start-1] == '$' {
		return nameSpan{}, false
	}
	run := content[start:end]
	switch run {
	case "true", "false", "in", "IN":
		return nameSpan{}, false
	}
	if rest := strings.TrimLeft(content[end:], " \t"); strings.HasPrefix(rest, "(") {
		return nameSpan{}, false
	}
	name := run
	if dot := strings.IndexByte(run, '.'); dot >= 0 {
		name = run[:dot]
	}
	return nameSpan{start: start, end: start + len(name), calc: true, dotted: len(name) < len(run)}, true
}

// isCalcWordByte reports whether c can continue a name or a number in a
// calc expression. Any byte of a multi-byte character counts, so such a
// character never splits a run.
func isCalcWordByte(c byte) bool {
	return c == '_' || c >= 0x80 || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// referenceNameSpans returns the names in one reference token, lit, that
// starts at byte offset base. It splits lit the way tree.ParseCursor
// does, where a '.' or a '[' ends a node, and it skips a reference that
// ParseCursor refuses. The first node is a name unless lit starts with a
// '$'. After "$." it is a name from the document root, and after "$[" or
// a "$NAME" environment variable there is none. The first dotted part of
// each bracket index is a name as well, because grab resolves a bracket
// index that is not a number, an environment variable, or a selector as
// a path of its own. Every node is returned whole, so "svc/0", "svc-1",
// and "name=svc" never equal a loop variable's name.
func referenceNameSpans(lit string, base int) []nameSpan {
	if _, err := tree.ParseCursor(lit); err != nil {
		return nil
	}
	var spans []nameSpan
	first, rooted := 0, false
	switch {
	case strings.HasPrefix(lit, "$."):
		first, rooted = 2, true
	case strings.HasPrefix(lit, "$"):
		first = -1
	}
	if first >= 0 {
		end := len(lit)
		if i := strings.IndexAny(lit[first:], ".["); i >= 0 {
			end = first + i
		}
		if end > first {
			spans = append(spans, nameSpan{start: base + first, end: base + end, rooted: rooted, tokenEnd: base + len(lit)})
		}
	}
	for i := 0; i < len(lit); i++ {
		if lit[i] != '[' {
			continue
		}
		// ParseCursor accepted lit, so brackets do not nest.
		closing := strings.IndexByte(lit[i+1:], ']')
		if closing < 0 {
			break
		}
		content := lit[i+1 : i+1+closing]
		end := len(content)
		if dot := strings.IndexByte(content, '.'); dot >= 0 {
			end = dot
		}
		if end > 0 {
			spans = append(spans, nameSpan{start: base + i + 1, end: base + i + 1 + end})
		}
		i += closing + 1
	}
	return spans
}

// rewriteVarRefs rewrites every name in inner that pathNameSpans finds
// and that names a bound loop variable, replacing it with that
// variable's absolute __graft_loop path and leaving the rest of the path
// as written: "svc" -> "__graft_loop.l1.0.svc", "svc.name" ->
// "__graft_loop.l1.0.svc.name", "list[svc]" ->
// "list[__graft_loop.l1.0.svc]". A name after "$." stays as written,
// because a "$." path always names the document root, so "$.svc" reads
// the top-level svc even inside a loop over svc. A rewritten identifier
// directly followed by a '+' or a '=' gets a space after it. The lexer
// glues "+name" and "=value" onto a dotted segment but not onto a bare
// identifier, so without the space "svc+x" would become one key instead
// of an addition.
func rewriteVarRefs(inner string, targets map[string]string, engine graft.Engine) string {
	if !mentionsAny(inner, targets) {
		return inner
	}
	calls := graft.OperatorCallNames(engine, inner)
	var out strings.Builder
	last := 0
	for _, sp := range pathNameSpans(inner, engine, calls) {
		if sp.rooted {
			continue
		}
		target, ok := targets[inner[sp.start:sp.end]]
		if !ok {
			continue
		}
		out.WriteString(inner[last:sp.start])
		out.WriteString(target)
		if sp.bare && sp.end < len(inner) && (inner[sp.end] == '+' || inner[sp.end] == '=') {
			out.WriteByte(' ')
		}
		if sp.calc && !sp.dotted && sp.end < len(inner) && inner[sp.end] == '-' {
			// calc reads a '-' in a dotted path as part of the path, so
			// the rewritten path would swallow the subtraction.
			out.WriteByte(' ')
		}
		last = sp.end
	}
	if last == 0 {
		return inner
	}
	out.WriteString(inner[last:])
	return out.String()
}

// mentionsAny reports whether inner holds the text of any target name, so
// rewriteVarRefs can skip lexing an expression that cannot name a loop
// variable.
func mentionsAny(inner string, targets map[string]string) bool {
	for name := range targets {
		if strings.Contains(inner, name) {
			return true
		}
	}
	return false
}

// rewriteLoopRefs applies rewriteVarRefs to every "(( ... ))" span found in
// each of bodyLines, leaving lines with no such span untouched. engine is
// the engine performing the parse, whose operators the lexer consults.
func rewriteLoopRefs(bodyLines []string, targets map[string]string, engine graft.Engine) []string {
	if len(targets) == 0 || len(bodyLines) == 0 {
		return bodyLines
	}
	out := make([]string, len(bodyLines))
	for i, line := range bodyLines {
		out[i] = rewriteLine(line, targets, engine)
	}
	return out
}

func rewriteLine(line string, targets map[string]string, engine graft.Engine) string {
	spans := findOpcallSpans(line)
	if len(spans) == 0 {
		return line
	}
	var b strings.Builder
	last := 0
	for _, sp := range spans {
		b.WriteString(line[last:sp.outerStart])
		b.WriteString("((")
		b.WriteString(rewriteVarRefs(line[sp.innerStart:sp.innerEnd], targets, engine))
		b.WriteString("))")
		last = sp.outerEnd
	}
	b.WriteString(line[last:])
	return b.String()
}

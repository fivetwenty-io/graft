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
// so it is left out.
func pathNameSpans(expr string, engine graft.Engine, calls map[int]string) []nameSpan {
	tz := newExprLexer(expr, engine)
	var spans []nameSpan
	for tz.HasMore() {
		before := tz.Position()
		tok := tz.NextToken()
		if tok.Type == interfaces.TokenEOF || tz.Position() <= before {
			break
		}
		switch tok.Type {
		case interfaces.TokenIdentifier:
			if tz.HeadName() {
				// The modifier or target of an operator head, as
				// "prod" in "vault@prod", names no key.
				continue
			}
			if _, ok := calls[tok.Pos.Offset]; ok {
				continue
			}
			spans = append(spans, nameSpan{start: tok.Pos.Offset, end: tok.Pos.Offset + len(tok.Literal), bare: true})
		case interfaces.TokenReference:
			spans = append(spans, referenceNameSpans(tok.Literal, tok.Pos.Offset)...)
		default:
		}
	}
	return spans
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

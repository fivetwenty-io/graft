package controlflow

import (
	"regexp"
	"strings"
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

// pathSegmentsPattern matches the continuations of a reference path the
// way the expression lexer (interfaces.ArrayReferencePattern) reads them:
// a dotted name, which may glue "+name" parts as genesis vaultified keys
// do, or a dotted run of digits; and a '/' segment, which may follow a
// '.' directly and holds either a "$name" or a run of name characters
// with "+name" parts glued on. A name may hold '-', as in "web-svc".
const pathSegmentsPattern = `(?:` +
	`\.(?:[A-Za-z_][A-Za-z0-9_-]*(?:\+[A-Za-z_][A-Za-z0-9_-]*)*|[0-9]+)` +
	`|\.?/(?:\$[A-Za-z_][A-Za-z0-9_-]*|[A-Za-z0-9_-]*(?:\+[A-Za-z_][A-Za-z0-9_-]*)*)` +
	`)`

// identPathRe matches a whole reference path that starts with a name,
// e.g. "svc", "svc.name", "svc.ports.0", "jobs.web/svc.ip", or
// "jobs.web-svc.ip".
var identPathRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*` + pathSegmentsPattern + `*`)

// pathContinuationRe matches the '.' and '/' segments that continue a
// path after a closing bracket or a leading run of digits, as in
// "arr[0]/svc" or "0/svc".
var pathContinuationRe = regexp.MustCompile(`^` + pathSegmentsPattern + `+`)

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// continuesPath reports whether a '.' or '/' at inner[i] joins the text
// before it, which is the case when it directly follows a name
// character, a digit, or a closing bracket. A '/' after whitespace, a
// closing paren, or a quote is division, and the name after it may be a
// loop variable.
func continuesPath(inner string, i int) bool {
	if i == 0 || (inner[i] != '.' && inner[i] != '/') {
		return false
	}
	p := inner[i-1]
	return isIdentStart(p) || (p >= '0' && p <= '9') || p == '-' || p == ']'
}

// rewriteVarRefs rewrites every identifier-path token in inner whose first
// dotted segment names a bound loop variable, replacing that first segment
// with its absolute __graft_loop path and leaving the remaining segments
// untouched: "svc" -> "__graft_loop.l1.0.svc", "svc.name" ->
// "__graft_loop.l1.0.svc.name". Content inside quoted strings is left
// verbatim so a literal string that happens to contain the variable name
// (a URL, a hostname) is never touched. A name that continues a path,
// after a '/', a '-', or a '.' that joins it to the text before, is part
// of a document key rather than a variable, so "jobs.web/svc.ip",
// "jobs.web-svc.ip", and "arr[0].svc" are never touched either. The first
// segment is compared whole, so "svc/0" and "svc-name" are keys of their
// own, as the expression lexer reads them.
func rewriteVarRefs(inner string, targets map[string]string) string {
	if len(targets) == 0 {
		return inner
	}
	var out strings.Builder
	n := len(inner)
	i := 0
	for i < n {
		c := inner[i]
		if c == '"' || c == '\'' {
			j := skipQuoted(inner, i, c)
			if j < 0 {
				out.WriteString(inner[i:])
				break
			}
			out.WriteString(inner[i:j])
			i = j
			continue
		}
		if continuesPath(inner, i) {
			if tok := pathContinuationRe.FindString(inner[i:]); tok != "" {
				out.WriteString(tok)
				i += len(tok)
				continue
			}
		}
		if isIdentStart(c) {
			tok := identPathRe.FindString(inner[i:])
			if tok == "" {
				out.WriteByte(c)
				i++
				continue
			}
			first := tok
			rest := ""
			if idx := strings.IndexByte(tok, '.'); idx >= 0 {
				first = tok[:idx]
				rest = tok[idx:]
			}
			if target, ok := targets[first]; ok {
				out.WriteString(target)
				out.WriteString(rest)
			} else {
				out.WriteString(tok)
			}
			i += len(tok)
			continue
		}
		out.WriteByte(c)
		i++
	}
	return out.String()
}

// rewriteLoopRefs applies rewriteVarRefs to every "(( ... ))" span found in
// each of bodyLines, leaving lines with no such span untouched.
func rewriteLoopRefs(bodyLines []string, targets map[string]string) []string {
	if len(targets) == 0 || len(bodyLines) == 0 {
		return bodyLines
	}
	out := make([]string, len(bodyLines))
	for i, line := range bodyLines {
		out[i] = rewriteLine(line, targets)
	}
	return out
}

func rewriteLine(line string, targets map[string]string) string {
	spans := findOpcallSpans(line)
	if len(spans) == 0 {
		return line
	}
	var b strings.Builder
	last := 0
	for _, sp := range spans {
		b.WriteString(line[last:sp.outerStart])
		b.WriteString("((")
		b.WriteString(rewriteVarRefs(line[sp.innerStart:sp.innerEnd], targets))
		b.WriteString("))")
		last = sp.outerEnd
	}
	b.WriteString(line[last:])
	return b.String()
}

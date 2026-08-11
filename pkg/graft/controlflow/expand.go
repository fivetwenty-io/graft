package controlflow

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

//nolint:gochecknoinits // Registers this package's preprocessor as pkg/graft's ControlFlowExpander hook at load time, mirroring pkg/graft/operators' own init()-based operator registration.
func init() {
	graft.ControlFlowExpander = Expand
}

// Expand runs the control-flow preprocessing pass over source. A document
// with no control-flow markers is returned unchanged (same underlying
// bytes) — the hard backward-compatibility requirement that no document
// without control-flow constructs changes behavior at all.
func Expand(source []byte) ([]byte, error) {
	text := string(source)
	if !strings.Contains(text, "((") {
		return source, nil
	}

	lines := classifyLines(text)
	if !hasControlFlowMarkers(lines) {
		return source, nil
	}

	topLevel, err := parseDocument(lines)
	if err != nil {
		return nil, fmt.Errorf("control flow: %w", err)
	}

	scope, err := buildPrescanScope(lines, topLevel)
	if err != nil {
		return nil, err
	}

	x := newExpander(source)
	bodyLines, err := x.expandItems(topLevel, newEnv(scope))
	if err != nil {
		return nil, err
	}

	return x.render(bodyLines)
}

// expander carries the mutable state threaded through one Expand call: the
// generator of unique loop-instance identifiers and the accumulated
// __graft_loop bindings every materialized for-loop variable is recorded
// into, keyed exactly the way rewriteLoopRefs' targets point at them.
type expander struct {
	salt     string
	loopSeq  int
	loopData map[string]interface{} // uid -> "<i>" -> varname -> value
}

// newExpander seeds the loop-identifier generator with a digest of the source
// being expanded. A bare per-call counter is not enough: graft merges several
// files, each expanded by its own expander, and every one of them would hand
// its first loop the same identifier. The __graft_loop subtrees then collide
// in the merge and each file's already-emitted references resolve to the last
// file's bindings. Salting by content keeps identifiers distinct across files
// while staying deterministic — the same bytes always expand to the same
// text, so repeated and concurrent runs stay reproducible.
func newExpander(source []byte) *expander {
	sum := sha256.Sum256(source)
	return &expander{
		salt:     hex.EncodeToString(sum[:6]),
		loopData: map[string]interface{}{},
	}
}

func (x *expander) nextUID() string {
	x.loopSeq++
	return fmt.Sprintf("l%s_%d", x.salt, x.loopSeq)
}

// bindLoopVar records that iteration i of loop uid bound name to value,
// growing loopData's nested uid -> index -> name structure as needed.
func (x *expander) bindLoopVar(uid string, i int, name string, value interface{}) {
	uidMap, ok := x.loopData[uid].(map[string]interface{})
	if !ok {
		uidMap = map[string]interface{}{}
		x.loopData[uid] = uidMap
	}
	key := strconv.Itoa(i)
	iterMap, ok := uidMap[key].(map[string]interface{})
	if !ok {
		iterMap = map[string]interface{}{}
		uidMap[key] = iterMap
	}
	iterMap[name] = value
}

// render assembles the final preprocessed text: the accumulated
// __graft_loop bindings (if any for-loop ran) spliced into the document
// body at documentBodyStart's insertion point.
func (x *expander) render(bodyLines []string) ([]byte, error) {
	var out strings.Builder

	if len(x.loopData) > 0 {
		loopYAML, err := graft.MarshalYAML(map[string]interface{}{"__graft_loop": x.loopData})
		if err != nil {
			return nil, fmt.Errorf("control flow: serializing loop bindings: %w", err)
		}
		at := documentBodyStart(bodyLines)
		if at > 0 {
			out.WriteString(strings.Join(bodyLines[:at], "\n"))
			out.WriteByte('\n')
			bodyLines = bodyLines[at:]
		}
		out.Write(loopYAML)
		if len(loopYAML) > 0 && loopYAML[len(loopYAML)-1] != '\n' {
			out.WriteByte('\n')
		}
	}

	out.WriteString(strings.Join(bodyLines, "\n"))
	out.WriteByte('\n')

	return []byte(out.String()), nil
}

// documentBodyStart returns the index in lines at which the __graft_loop
// bindings block must be spliced: after a leading YAML header (blank lines,
// comments, "%" directives, and a "---" document-start marker), otherwise 0.
//
// The bindings have to land inside the same YAML document as the loop bodies
// that reference them. Writing them above a leading "---" would turn a
// single-document file into a two-document stream whose first document holds
// nothing but the bindings; graft reads only the first document, so the whole
// user document would be silently dropped. A "---" carrying inline content
// ("--- a: 1") is deliberately not treated as a header, since the bindings
// cannot be spliced after it without disturbing that content's column.
func documentBodyStart(lines []string) int {
	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "%") {
			continue
		}
		if trimmed == "---" || strings.HasPrefix(trimmed, "--- #") {
			return i + 1
		}
		return 0
	}
	return 0
}

// expandItems processes one block's body (or the top-level document) under
// env e, returning the emitted lines in order. Blocks are expanded
// outermost-first, depth-first, exactly as spec §8.3 step 3 describes: a
// nested block is only visited (and only then evaluates its own condition)
// once its enclosing block has already selected the branch or iteration
// containing it.
func (x *expander) expandItems(items []item, e *env) ([]string, error) {
	var out []string
	for idx := range items {
		it := &items[idx]
		var lines []string
		var err error
		switch it.kind {
		case itemRaw:
			lines = it.rawLines
		case itemIf:
			lines, err = x.expandIf(it, e)
		case itemFor:
			lines, err = x.expandFor(it, e)
		case itemWhile:
			lines, err = x.expandWhile(it, e)
		case itemCase:
			lines, err = x.expandCase(it, e)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, lines...)
	}
	return out, nil
}

func (x *expander) expandIf(it *item, e *env) ([]string, error) {
	loc := fmt.Sprintf("controlflow.if.L%d", it.startLine+1)
	for _, c := range it.clauses {
		if c.kind == "else" {
			return x.expandItems(c.body, e)
		}
		truthy, err := evalTruthy(c.condRaw, e, loc)
		if err != nil {
			return nil, err
		}
		if truthy {
			return x.expandItems(c.body, e)
		}
	}
	return nil, nil // no clause matched and there was no else: emit nothing
}

func (x *expander) expandWhile(it *item, e *env) ([]string, error) {
	loc := fmt.Sprintf("controlflow.while.L%d", it.startLine+1)
	maxIterations := MaxLoopIterations()

	var out []string
	for iteration := 0; ; iteration++ {
		if iteration >= maxIterations {
			return nil, fmt.Errorf("$.%s: while loop exceeded maximum iterations (%d)", loc, maxIterations)
		}
		truthy, err := evalTruthy(it.condRaw, e, loc)
		if err != nil {
			return nil, err
		}
		if !truthy {
			return out, nil
		}
		body, err := x.expandItems(it.body, e)
		if err != nil {
			return nil, err
		}
		out = append(out, body...)
	}
}

func (x *expander) expandCase(it *item, e *env) ([]string, error) {
	loc := fmt.Sprintf("controlflow.case.L%d", it.startLine+1)
	subjectVal, err := evalExpr(it.subjectRaw, e, loc)
	if err != nil {
		return nil, err
	}
	subject := stringifyForCase(subjectVal)

	for _, w := range it.whens {
		for _, pat := range w.patterns {
			patVal, err := parseCasePattern(pat, loc)
			if err != nil {
				return nil, err
			}
			if stringifyForCase(patVal) == subject {
				return x.expandItems(w.body, e)
			}
		}
	}
	if it.hasDefault {
		return x.expandItems(it.defaultBody, e)
	}
	return nil, nil // C-13: no match and no default emits nothing
}

// parseCasePattern parses one when-clause pattern token into its literal
// Go value. Spec decision C-12 restricts patterns to quoted strings,
// numbers, and booleans — a bare identifier is rejected rather than
// silently treated as a reference, since that would make "(( when foo ))"
// ambiguous between a literal word and a variable lookup.
func parseCasePattern(tok string, loc string) (interface{}, error) {
	tok = strings.TrimSpace(tok)
	if len(tok) >= 2 && (tok[0] == '"' || tok[0] == '\'') && tok[len(tok)-1] == tok[0] {
		return unquoteCasePattern(tok), nil
	}
	if tok == "true" {
		return true, nil
	}
	if tok == "false" {
		return false, nil
	}
	if n, err := strconv.ParseInt(tok, 10, 64); err == nil {
		return n, nil
	}
	if f, err := strconv.ParseFloat(tok, 64); err == nil {
		return f, nil
	}
	return nil, fmt.Errorf("$.%s: (( when )) pattern must be a quoted string, a number, or true/false, got %q", loc, tok)
}

// unquoteCasePattern strips tok's surrounding quotes. Single-quoted content
// is raw (no escapes, matching the parser's own single-quoted string
// handling elsewhere in graft); double-quoted content processes the same
// small escape set graft's string literals already support.
func unquoteCasePattern(tok string) string {
	inner := tok[1 : len(tok)-1]
	if tok[0] == '\'' {
		return inner
	}
	var b strings.Builder
	for i := 0; i < len(inner); i++ {
		if inner[i] == '\\' && i+1 < len(inner) {
			switch inner[i+1] {
			case '"':
				b.WriteByte('"')
			case '\\':
				b.WriteByte('\\')
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			default:
				b.WriteByte(inner[i+1])
			}
			i++
			continue
		}
		b.WriteByte(inner[i])
	}
	return b.String()
}

// stringifyForCase renders v the way spec decision C-11 requires case
// matching to compare: exact string equality after stringifying the
// subject.
func stringifyForCase(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case int64:
		return strconv.FormatInt(t, 10)
	case int:
		return strconv.Itoa(t)
	case float64:
		if t == math.Trunc(t) && !math.IsInf(t, 0) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'g', -1, 64)
	default:
		return fmt.Sprintf("%v", t)
	}
}

// forHeaderVars and rangeHeaderRe support expandFor below.
var rangeHeaderRe = regexp.MustCompile(`^range\s+(.+)$`)

func (x *expander) expandFor(it *item, e *env) ([]string, error) {
	loc := fmt.Sprintf("controlflow.for.L%d", it.startLine+1)
	uid := x.nextUID()

	if m := rangeHeaderRe.FindStringSubmatch(strings.TrimSpace(it.iterableRaw)); m != nil {
		return x.expandForRange(it, e, uid, loc, m[1])
	}

	val, err := evalExpr(it.iterableRaw, e, loc)
	if err != nil {
		return nil, err
	}

	switch coll := val.(type) {
	case nil:
		return nil, nil // C-10: empty (a null value) emits nothing
	case []interface{}:
		return x.expandForList(it, e, uid, coll)
	case map[string]interface{}:
		return x.expandForMap(it, e, uid, coll)
	default:
		return nil, fmt.Errorf("$.%s: for-loop iterable must be a list or a map, got %T", loc, val)
	}
}

func (x *expander) expandForRange(it *item, e *env, uid, loc, argsRaw string) ([]string, error) {
	if len(it.loopVars) != 1 {
		return nil, fmt.Errorf("$.%s: range yields a single value per iteration", loc) // C-7
	}
	args := splitTopLevelFields(argsRaw)
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("$.%s: range requires 2 or 3 arguments (start, end, [step]), got %d", loc, len(args))
	}
	step := ""
	if len(args) == 3 {
		step = args[2]
	}
	values, err := x.evalRange(args[0], args[1], step, e, loc)
	if err != nil {
		return nil, err
	}

	var out []string
	name := it.loopVars[0]
	for i, v := range values {
		x.bindLoopVar(uid, i, name, v)
		iterEnv := e.withBinding(name, v)
		body, err := x.expandItems(it.body, iterEnv)
		if err != nil {
			return nil, err
		}
		out = append(out, rewriteLoopRefs(body, varTargets(uid, i, name))...)
	}
	return out, nil
}

// evalRange evaluates a range's start/end/[step] expression texts and
// produces the closed interval [start, end] (spec decision C-3) stepping by
// step (default 1). A step that is zero, or whose sign does not move start
// toward end, is decision C-5's error.
func (x *expander) evalRange(startTok, endTok, stepTok string, e *env, loc string) ([]int64, error) {
	start, err := evalRangeInt(startTok, "start", e, loc)
	if err != nil {
		return nil, err
	}
	end, err := evalRangeInt(endTok, "end", e, loc)
	if err != nil {
		return nil, err
	}
	step := int64(1)
	if stepTok != "" {
		step, err = evalRangeInt(stepTok, "step", e, loc)
		if err != nil {
			return nil, err
		}
	}
	if step == 0 || (step > 0 && start > end) || (step < 0 && start < end) {
		return nil, fmt.Errorf("$.%s: range step must be non-zero and must move start toward end", loc)
	}

	var out []int64
	if step > 0 {
		for v := start; v <= end; v += step {
			out = append(out, v)
		}
	} else {
		for v := start; v >= end; v += step {
			out = append(out, v)
		}
	}
	return out, nil
}

func evalRangeInt(tok string, which string, e *env, loc string) (int64, error) {
	val, err := evalExpr(tok, e, loc)
	if err != nil {
		return 0, err
	}
	switch n := val.(type) {
	case int64:
		return n, nil
	case int:
		return int64(n), nil
	case float64:
		if n == math.Trunc(n) {
			return int64(n), nil
		}
		return 0, fmt.Errorf("$.%s: range %s must be an integer, got %v", loc, which, val)
	default:
		return 0, fmt.Errorf("$.%s: range %s must be an integer, got %T", loc, which, val)
	}
}

// expandForList implements spec decision C-6 for a list: two loop
// variables bind (index, element); one binds the element.
func (x *expander) expandForList(it *item, e *env, uid string, coll []interface{}) ([]string, error) {
	var out []string
	for i, elem := range coll {
		var iterEnv *env
		var targets map[string]string
		if len(it.loopVars) == 2 {
			idxName, elemName := it.loopVars[0], it.loopVars[1]
			x.bindLoopVar(uid, i, idxName, int64(i))
			x.bindLoopVar(uid, i, elemName, elem)
			iterEnv = e.withBinding(idxName, int64(i)).withBinding(elemName, elem)
			targets = varTargets(uid, i, idxName, elemName)
		} else {
			name := it.loopVars[0]
			x.bindLoopVar(uid, i, name, elem)
			iterEnv = e.withBinding(name, elem)
			targets = varTargets(uid, i, name)
		}
		body, err := x.expandItems(it.body, iterEnv)
		if err != nil {
			return nil, err
		}
		out = append(out, rewriteLoopRefs(body, targets)...)
	}
	return out, nil
}

// expandForMap implements spec decisions C-6 and C-8 for a map: iteration
// order is the map's keys sorted ascending (C-19, matching graft's own
// alphabetical output convention); two loop variables bind (key, value),
// one binds the value.
func (x *expander) expandForMap(it *item, e *env, uid string, coll map[string]interface{}) ([]string, error) {
	keys := sortedMapKeys(coll)
	var out []string
	for i, k := range keys {
		v := coll[k]
		var iterEnv *env
		var targets map[string]string
		if len(it.loopVars) == 2 {
			keyName, valName := it.loopVars[0], it.loopVars[1]
			x.bindLoopVar(uid, i, keyName, k)
			x.bindLoopVar(uid, i, valName, v)
			iterEnv = e.withBinding(keyName, k).withBinding(valName, v)
			targets = varTargets(uid, i, keyName, valName)
		} else {
			name := it.loopVars[0]
			x.bindLoopVar(uid, i, name, v) // C-8: single var binds the value
			iterEnv = e.withBinding(name, v)
			targets = varTargets(uid, i, name)
		}
		body, err := x.expandItems(it.body, iterEnv)
		if err != nil {
			return nil, err
		}
		out = append(out, rewriteLoopRefs(body, targets)...)
	}
	return out, nil
}

func sortedMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// varTargets builds the rewriteLoopRefs target map for loop uid's iteration
// i, mapping each bound variable name to its absolute __graft_loop path.
func varTargets(uid string, i int, names ...string) map[string]string {
	m := make(map[string]string, len(names))
	for _, n := range names {
		m[n] = fmt.Sprintf("__graft_loop.%s.%d.%s", uid, i, n)
	}
	return m
}

// splitTopLevelFields splits s on whitespace runs that are not inside
// parentheses or quotes, so "range (grab a) (grab b)" splits into two
// fields ("(grab a)", "(grab b)") rather than four. Used only for range's
// "start end [step]" argument list, which the grammar defines as
// space-separated primaries — never for condition or plain-iterable text,
// which is always handed to the real expression parser as a single string.
func splitTopLevelFields(s string) []string {
	var fields []string
	var buf strings.Builder
	depth := 0
	n := len(s)
	for i := 0; i < n; i++ {
		c := s[i]
		switch {
		case c == '"' || c == '\'':
			j := skipQuoted(s, i, c)
			if j < 0 {
				buf.WriteByte(c)
				continue
			}
			buf.WriteString(s[i:j])
			i = j - 1
		case c == '(':
			depth++
			buf.WriteByte(c)
		case c == ')':
			if depth > 0 {
				depth--
			}
			buf.WriteByte(c)
		case (c == ' ' || c == '\t') && depth == 0:
			if buf.Len() > 0 {
				fields = append(fields, buf.String())
				buf.Reset()
			}
		default:
			buf.WriteByte(c)
		}
	}
	if buf.Len() > 0 {
		fields = append(fields, buf.String())
	}
	return fields
}

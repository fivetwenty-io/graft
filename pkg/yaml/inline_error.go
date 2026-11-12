package yaml

import (
	"math"
	"math/bits"
	"reflect"
	"slices"
	"strings"

	"github.com/goccy/go-yaml/ast"
)

// reachesInlineMap reports whether a value of type t holds, at any
// depth, a struct field tagged ",inline" whose type is a map.
//
// goccy fills such a field from a Go map of the struct's keys, so when
// several of its values are bad, which one the error names changes from
// run to run.
func reachesInlineMap(t reflect.Type) bool {
	w := typeWalk{leaf: inlinesMap, field: decodesField, seen: make(map[reflect.Type]bool)}
	return w.reaches(t)
}

// inlinesMap reports whether t is a struct with an ,inline field that
// goccy decodes and whose type is a map or a pointer to one.
func inlinesMap(t reflect.Type) bool {
	if t.Kind() != reflect.Struct {
		return false
	}
	for i := range t.NumField() {
		f := t.Field(i)
		if !decodesField(f) {
			continue
		}
		_, opts, _ := strings.Cut(fieldTag(f), ",")
		if !hasOption(opts, "inline") {
			continue
		}
		ft := f.Type
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Map {
			return true
		}
	}
	return false
}

// earliestTypeError returns the error with the smallest line and column
// among the errors goccy can report for body decoded into a value of
// v's type, given first, the error the original decode returned. It
// decodes copies of body into new values of that type, so body and v
// are never changed. own holds the scalars asWritten found in body, so
// each error goes through the same steps as the first one did.
//
// The search works on units, which are the pairs of a mapping and the
// items of a sequence. A unit starts at the earliest position of any
// node under it, aliases included, since alias expansion puts the
// anchor's node in the alias's slot and a merge folds the anchor's pairs
// into the mapping that names them. A prefix of body keeps only the
// units that start at or before a position, so it holds every node at
// or before that position, with the same Go type to fill as in body.
// Whether a value fits depends only on the value and that type, so
// whether a prefix fails can only go from false to true as the prefix
// grows.
//
// Each round drops every innermost unit that holds the last error's
// position, and then searches by halves for the shortest failing
// prefix that ends at or before the best error so far. That prefix
// either fails at the earliest bad value left or at a value that sits
// after it in a unit that starts before it, and the next round drops
// that value. When no such prefix fails, nothing at or before the best
// error is left, and the best error is the earliest. A round costs one
// decode and one for each halving, and an input reaches the answer in
// two rounds unless aliases put values out of document order. The
// rounds are capped at a small multiple of the bits in the unit count,
// and past the cap the best error so far is returned.
//
// A caller's own unmarshal method can panic when a prefix hands it a
// mapping with keys missing. Such a panic stops the search, and the
// best error so far is returned. Every error the search returns is one
// goccy reported, so a stopped search names a real bad value, only
// perhaps not the earliest.
func earliestTypeError(body ast.Node, v any, opts DecodeOptions, own ownScalars, first *DecodeError) *DecodeError {
	s := prefixSearch{target: reflect.TypeOf(v), opts: opts, own: own}
	best, last := first, first
	limit := 1
	for round := 0; round < limit; round++ {
		next, dropped := unitDrop{at: errorSrcPos(last), memo: make(map[ast.Node]dropped)}.node(body)
		if !dropped {
			break
		}
		body = next
		found, units, stopped := s.firstFailure(body, errorSrcPos(best))
		if round == 0 {
			limit = 4 + 2*bits.Len(uint(units))
		}
		if found == nil || found.Kind != ErrorKindType || found.Line == 0 {
			break
		}
		last = found
		if earlier(last, best) {
			best = last
		}
		if stopped {
			break
		}
	}
	return best
}

// earlier reports whether a comes before b by line, then column, then
// message.
func earlier(a, b *DecodeError) bool {
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	if a.Column != b.Column {
		return a.Column < b.Column
	}
	return a.Msg < b.Msg
}

// srcPos is a line and column in the input. A node with no position
// sits at noSrcPos, after every other.
type srcPos struct {
	line, column int
}

var noSrcPos = srcPos{line: math.MaxInt, column: math.MaxInt}

// after reports whether p comes after q.
func (p srcPos) after(q srcPos) bool {
	return p.line > q.line || p.line == q.line && p.column > q.column
}

// errorSrcPos returns where e points.
func errorSrcPos(e *DecodeError) srcPos {
	return srcPos{line: e.Line, column: e.Column}
}

// srcPosOf returns where n's token sits.
func srcPosOf(n ast.Node) srcPos {
	if tk := n.GetToken(); tk != nil && tk.Position != nil {
		return srcPos{line: tk.Position.Line, column: tk.Position.Column}
	}
	return noSrcPos
}

// prefixSearch decodes prefixes of a body into new values of target.
type prefixSearch struct {
	target reflect.Type
	opts   DecodeOptions
	own    ownScalars
}

// firstFailure returns the error of the shortest prefix of body that
// ends at a unit start no later than bound and fails, or nil when none
// fails. It also returns how many units body holds, and whether a decode
// panicked. A panic ends the search, which then returns the error of the
// shortest failing prefix it has decoded, or nil when it has none.
func (s prefixSearch) firstFailure(body ast.Node, bound srcPos) (found *DecodeError, units int, stopped bool) {
	st := &unitStarts{memo: make(map[ast.Node]srcPos), seen: make(map[ast.Node]bool)}
	st.collect(body)
	units = len(st.all)
	ends := st.upTo(bound)
	if len(ends) == 0 {
		return nil, units, false
	}
	failed := st.prefix(body, ends[len(ends)-1])
	panicked, err := s.decode(failed)
	if panicked || err == nil {
		return nil, units, panicked
	}
	lo, hi := 0, len(ends)-1
	for lo < hi {
		mid := lo + (hi-lo)/2
		b := st.prefix(body, ends[mid])
		panicked, got := s.decode(b)
		if panicked {
			stopped = true
			break
		}
		if got != nil {
			hi, err, failed = mid, got, b
		} else {
			lo = mid + 1
		}
	}
	return translate(s.own.locate(locate(err, failed, s.target), s.target.Elem())), units, stopped
}

// decode decodes body into a new value of target. It recovers any panic
// and reports it, since a caller's own unmarshal method may not expect
// a mapping that the search has cut short.
func (s prefixSearch) decode(body ast.Node) (panicked bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			panicked, err = true, nil
		}
	}()
	return false, decodeBody(body, reflect.New(s.target.Elem()).Interface(), s.opts)
}

// unitStarts computes where each node's subtree starts, once for each
// node, since alias expansion shares subtrees.
type unitStarts struct {
	memo map[ast.Node]srcPos
	seen map[ast.Node]bool
	all  []srcPos
}

// start returns the earliest position of n or any node under it.
func (st *unitStarts) start(n ast.Node) srcPos {
	if n == nil {
		return noSrcPos
	}
	if got, ok := st.memo[n]; ok {
		return got
	}
	got := srcPosOf(n)
	earliest := func(c ast.Node) {
		if c := st.start(c); got.after(c) {
			got = c
		}
	}
	switch n := n.(type) {
	case *ast.MappingNode:
		for _, pair := range n.Values {
			earliest(pair)
		}
	case *ast.MappingValueNode:
		earliest(n.Key)
		earliest(n.Value)
	case *ast.SequenceNode:
		for _, item := range n.Values {
			earliest(item)
		}
	default:
		if child, rebuild := inner(n); rebuild != nil {
			earliest(child)
		}
	}
	st.memo[n] = got
	return got
}

// collect records the start of every unit under n.
func (st *unitStarts) collect(n ast.Node) {
	if n == nil || st.seen[n] {
		return
	}
	st.seen[n] = true
	switch n := n.(type) {
	case *ast.MappingNode:
		for _, pair := range n.Values {
			st.all = append(st.all, st.start(pair))
			st.collect(pair.Key)
			st.collect(pair.Value)
		}
	case *ast.MappingValueNode:
		st.collect(n.Key)
		st.collect(n.Value)
	case *ast.SequenceNode:
		for _, item := range n.Values {
			st.all = append(st.all, st.start(item))
			st.collect(item)
		}
	default:
		if child, rebuild := inner(n); rebuild != nil {
			st.collect(child)
		}
	}
}

// upTo returns the distinct unit starts no later than bound, in order.
func (st *unitStarts) upTo(bound srcPos) []srcPos {
	out := make([]srcPos, 0, len(st.all))
	for _, p := range st.all {
		if !p.after(bound) {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b srcPos) int {
		switch {
		case a == b:
			return 0
		case b.after(a):
			return -1
		default:
			return 1
		}
	})
	return slices.Compact(out)
}

// prefix returns body with only the units that start at or before end.
// It copies each node on the path to one it changes and shares every
// other subtree.
func (st *unitStarts) prefix(body ast.Node, end srcPos) ast.Node {
	pr := unitPrefix{starts: st, end: end, memo: make(map[ast.Node]ast.Node)}
	return pr.node(body)
}

// unitPrefix cuts a body down to the units that start at or before end.
type unitPrefix struct {
	starts *unitStarts
	end    srcPos
	memo   map[ast.Node]ast.Node
}

func (pr unitPrefix) node(n ast.Node) ast.Node {
	if n == nil {
		return nil
	}
	if got, ok := pr.memo[n]; ok {
		return got
	}
	out := n
	switch n := n.(type) {
	case *ast.MappingNode:
		out = pr.mapping(n)
	case *ast.SequenceNode:
		out = pr.sequence(n)
	case *ast.MappingValueNode:
		if got := pr.node(n.Value); got != n.Value {
			c := *n
			c.Value = got
			out = &c
		}
	default:
		if child, rebuild := inner(n); rebuild != nil {
			if got := pr.node(child); got != child {
				out = rebuild(got)
			}
		}
	}
	pr.memo[n] = out
	return out
}

func (pr unitPrefix) mapping(m *ast.MappingNode) ast.Node {
	kept := make([]*ast.MappingValueNode, 0, len(m.Values))
	changed := false
	for _, pair := range m.Values {
		if pr.starts.start(pair).after(pr.end) {
			changed = true
			continue
		}
		got := asPair(pr.node(pair), pair)
		if got != pair {
			changed = true
		}
		kept = append(kept, got)
	}
	if !changed {
		return m
	}
	c := *m
	c.Values = kept
	return &c
}

func (pr unitPrefix) sequence(s *ast.SequenceNode) ast.Node {
	kept := make([]ast.Node, 0, len(s.Values))
	changed := false
	for _, item := range s.Values {
		if pr.starts.start(item).after(pr.end) {
			changed = true
			continue
		}
		got := pr.node(item)
		if got != item {
			changed = true
		}
		kept = append(kept, got)
	}
	if !changed {
		return s
	}
	c := *s
	c.Values = kept
	return &c
}

// dropped is what unitDrop made of a node: the node without the units
// it removed, and whether a node at the position is left in it outside
// any unit it removed.
type dropped struct {
	out   ast.Node
	found bool
}

// unitDrop removes from a body every innermost unit that holds a node
// at a position, whether in a key or a value. The token of a block
// mapping or sequence is the token of its first entry's ':' or '-', so
// a container whose own token sits at the position is the node found,
// and its own unit goes. It copies each node on the path to one it
// changes and shares every other subtree.
type unitDrop struct {
	at   srcPos
	memo map[ast.Node]dropped
}

// node returns n without the units that hold the position, and whether
// it removed any.
func (d unitDrop) node(n ast.Node) (ast.Node, bool) {
	got := d.walk(n)
	return got.out, got.out != n
}

func (d unitDrop) walk(n ast.Node) dropped {
	if n == nil {
		return dropped{}
	}
	if got, ok := d.memo[n]; ok {
		return got
	}
	var got dropped
	switch n := n.(type) {
	case *ast.MappingNode:
		got = d.mapping(n)
	case *ast.SequenceNode:
		got = d.sequence(n)
	case *ast.MappingValueNode:
		got = d.pair(n)
	default:
		got = dropped{out: n, found: srcPosOf(n) == d.at}
		if child, rebuild := inner(n); rebuild != nil {
			c := d.walk(child)
			if c.out != child {
				got.out = rebuild(c.out)
			}
			got.found = got.found || c.found
		}
	}
	d.memo[n] = got
	return got
}

// pair walks the key and the value of a pair. The pair's own token is
// its ':', which a block mapping shares, so it is never the node found.
func (d unitDrop) pair(n *ast.MappingValueNode) dropped {
	key, val := d.walk(n.Key), d.walk(n.Value)
	got := dropped{out: n, found: key.found || val.found}
	if key.out != n.Key || val.out != n.Value {
		c := *n
		if k, ok := key.out.(ast.MapKeyNode); ok {
			c.Key = k
		}
		c.Value = val.out
		got.out = &c
	}
	return got
}

func (d unitDrop) mapping(m *ast.MappingNode) dropped {
	kept := make([]*ast.MappingValueNode, 0, len(m.Values))
	changed := false
	for _, pair := range m.Values {
		got := d.walk(pair)
		if got.found {
			changed = true
			continue
		}
		out := asPair(got.out, pair)
		if out != pair {
			changed = true
		}
		kept = append(kept, out)
	}
	got := dropped{out: m, found: srcPosOf(m) == d.at}
	if changed {
		c := *m
		c.Values = kept
		got.out = &c
	}
	return got
}

func (d unitDrop) sequence(s *ast.SequenceNode) dropped {
	kept := make([]ast.Node, 0, len(s.Values))
	changed := false
	for _, item := range s.Values {
		got := d.walk(item)
		if got.found {
			changed = true
			continue
		}
		if got.out != item {
			changed = true
		}
		kept = append(kept, got.out)
	}
	got := dropped{out: s, found: srcPosOf(s) == d.at}
	if changed {
		c := *s
		c.Values = kept
		got.out = &c
	}
	return got
}

// asPair returns n, the copy of pair that a walk made, as a pair, or
// pair itself when n is not one, which a walk never returns.
func asPair(n ast.Node, pair *ast.MappingValueNode) *ast.MappingValueNode {
	if got, ok := n.(*ast.MappingValueNode); ok {
		return got
	}
	return pair
}

// inner returns the one node that n holds, when n is a document, a tag,
// or an anchor, and a function that returns a copy of n holding another
// node in its place. For any other node both are nil.
func inner(n ast.Node) (ast.Node, func(ast.Node) ast.Node) {
	switch n := n.(type) {
	case *ast.DocumentNode:
		return n.Body, func(v ast.Node) ast.Node {
			c := *n
			c.Body = v
			return &c
		}
	case *ast.TagNode:
		return n.Value, func(v ast.Node) ast.Node {
			c := *n
			c.Value = v
			return &c
		}
	case *ast.AnchorNode:
		return n.Value, func(v ast.Node) ast.Node {
			c := *n
			c.Value = v
			return &c
		}
	default:
		return nil, nil
	}
}

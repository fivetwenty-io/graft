package yaml

import (
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

// earliestTypeError decodes body again into new values of v's type,
// each time without the pair the last error pointed at, until the
// decode passes or no pair is left to drop, and returns the error with
// the smallest line and column. first is the error the original decode
// returned. It never runs more decodes than body holds pairs.
//
// Each round also drops every pair whose key comes after the earliest
// error so far, since an error in such a pair can only come later. goccy
// meets the bad values of an ,inline map in random order, so the rounds
// that remain fall by about half each time, and a map with many bad
// values costs a few decodes rather than one for each.
//
// own holds the scalars asWritten found in body, so each error goes
// through the same steps as the first one did. Pairs are dropped from
// copies of the nodes on the path to them, so body and v are never
// changed. Errors at the same position are ordered by their messages,
// so the result does not depend on which one goccy met first.
func earliestTypeError(body ast.Node, v any, opts DecodeOptions, own ownScalars, first *DecodeError) *DecodeError {
	target := reflect.TypeOf(v)
	best, last := first, first
	for range len(ast.Filter(ast.MappingValueType, body)) {
		next, _, dropped := pairDrop{line: last.Line, column: last.Column}.in(body)
		if !dropped {
			break
		}
		body = pairDrop{line: best.Line, column: best.Column}.after(next)
		err := decodeBody(body, reflect.New(target.Elem()).Interface(), opts)
		if err == nil {
			break
		}
		last = translate(own.locate(locate(err, body, target), target.Elem()))
		if last.Kind != ErrorKindType || last.Line == 0 {
			break
		}
		if earlier(last, best) {
			best = last
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

// pairDrop removes from a body the innermost mapping pair whose value
// holds a node at a line and column. Alias expansion shares subtrees,
// so it never changes a node in place. It copies each node on the path
// to the pair and leaves every other subtree shared.
type pairDrop struct {
	line, column int
}

// in returns n with the first such pair under n removed, in document
// order. found reports whether n holds a node at the position, and
// dropped whether a pair under n was removed, in which case out is the
// copy without it and n is unchanged.
func (p pairDrop) in(n ast.Node) (out ast.Node, found, dropped bool) {
	switch n := n.(type) {
	case nil:
		return nil, false, false
	case *ast.MappingNode:
		return p.mapping(n)
	case *ast.SequenceNode:
		return p.sequence(n)
	default:
	}
	child, rebuild := inner(n)
	if rebuild == nil {
		return n, p.at(n), false
	}
	got, found, dropped := p.in(child)
	if !dropped {
		return n, found || p.at(n), false
	}
	return rebuild(got), true, true
}

// inner returns the one node that n holds, when n is a pair, a document,
// a tag, or an anchor, and a function that returns a copy of n holding
// another node in its place. For any other node both are nil.
func inner(n ast.Node) (ast.Node, func(ast.Node) ast.Node) {
	switch n := n.(type) {
	case *ast.MappingValueNode:
		return n.Value, func(v ast.Node) ast.Node { c := *n; c.Value = v; return &c }
	case *ast.DocumentNode:
		return n.Body, func(v ast.Node) ast.Node { c := *n; c.Body = v; return &c }
	case *ast.TagNode:
		return n.Value, func(v ast.Node) ast.Node { c := *n; c.Value = v; return &c }
	case *ast.AnchorNode:
		return n.Value, func(v ast.Node) ast.Node { c := *n; c.Value = v; return &c }
	default:
		return nil, nil
	}
}

// mapping drops the first pair of m whose value holds a pair to drop or
// a node at the position, and otherwise reports whether m's own token
// sits there.
func (p pairDrop) mapping(m *ast.MappingNode) (ast.Node, bool, bool) {
	for i, pair := range m.Values {
		val, found, dropped := p.in(pair.Value)
		if !dropped && !found {
			continue
		}
		c := *m
		c.Values = slices.Clone(m.Values)
		if dropped {
			kept := *pair
			kept.Value = val
			c.Values[i] = &kept
		} else {
			c.Values = slices.Delete(c.Values, i, i+1)
		}
		return &c, true, true
	}
	return m, p.at(m), false
}

// sequence drops a pair from the first item of s that holds one to
// drop, and otherwise reports whether s holds a node at the position.
func (p pairDrop) sequence(s *ast.SequenceNode) (ast.Node, bool, bool) {
	found := p.at(s)
	for i, item := range s.Values {
		got, at, dropped := p.in(item)
		if dropped {
			c := *s
			c.Values = slices.Clone(s.Values)
			c.Values[i] = got
			return &c, true, true
		}
		found = found || at
	}
	return s, found, false
}

// at reports whether n's token sits at the position.
func (p pairDrop) at(n ast.Node) bool {
	tk := n.GetToken()
	return tk != nil && tk.Position != nil && tk.Position.Line == p.line && tk.Position.Column == p.column
}

// after returns n without every mapping pair whose key sits after the
// position, copying each node on the path to one it changes, or n itself
// when nothing changes. A pair whose key has no position is kept.
func (p pairDrop) after(n ast.Node) ast.Node {
	switch n := n.(type) {
	case *ast.MappingNode:
		return p.mappingAfter(n)
	case *ast.SequenceNode:
		var out *ast.SequenceNode
		for i, item := range n.Values {
			got := p.after(item)
			if got == item {
				continue
			}
			if out == nil {
				c := *n
				c.Values = slices.Clone(n.Values)
				out = &c
			}
			out.Values[i] = got
		}
		if out != nil {
			return out
		}
	default:
		if child, rebuild := inner(n); rebuild != nil {
			if got := p.after(child); got != child {
				return rebuild(got)
			}
		}
	}
	return n
}

// mappingAfter returns m without the pairs whose keys sit after the
// position, and with the pairs it keeps trimmed in turn.
func (p pairDrop) mappingAfter(m *ast.MappingNode) ast.Node {
	var out []*ast.MappingValueNode
	for i, pair := range m.Values {
		if p.before(pair.Key) {
			if out == nil {
				out = slices.Clone(m.Values[:i])
			}
			continue
		}
		kept := pair
		if val := p.after(pair.Value); val != pair.Value {
			c := *pair
			c.Value = val
			kept = &c
		}
		if out != nil {
			out = append(out, kept)
		} else if kept != pair {
			out = slices.Clone(m.Values[:i])
			out = append(out, kept)
		}
	}
	if out == nil {
		return m
	}
	c := *m
	c.Values = out
	return &c
}

// before reports whether the position comes before n's token.
func (p pairDrop) before(n ast.Node) bool {
	if n == nil {
		return false
	}
	tk := n.GetToken()
	if tk == nil || tk.Position == nil {
		return false
	}
	return tk.Position.Line > p.line || tk.Position.Line == p.line && tk.Position.Column > p.column
}

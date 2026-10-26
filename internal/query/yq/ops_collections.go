package yq

import (
	"bytes"
	"slices"
	"strings"

	"github.com/fivetwenty-io/graft/internal/query/builtin"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/internal/query/yamlout"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// registerCollections adds yq's collection operators. yq sorts and
// dedupes by its own ordering rather than jq's, keeps the first of each
// run of equal keys in input order, and writes a collection it rebuilds
// in the style of the one it was given.
func registerCollections(r *runtime.Registry) {
	r.RegisterSingle("_yq_like", 1, func(_ *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		return args[0].Each(in, func(v *value.Value, _ value.Path) error { return emit(like(in, v), nil) })
	})
	r.RegisterSingle("sort", 0, func(ctx *runtime.Ctx, in *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		return sortBy(ctx, in, nil, emit)
	})
	r.RegisterSingle("_yq_sort_by", 1, func(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		return sortBy(ctx, in, args[0], emit)
	})
	r.RegisterSingle("unique", 0, func(_ *runtime.Ctx, in *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		return uniqueBy(in, nil, emit)
	})
	r.RegisterSingle("_yq_unique_by", 1, func(_ *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		return uniqueBy(in, args[0], emit)
	})
	r.RegisterSingle("_yq_group_by", 1, groupBy)
	r.RegisterSingle("_yq_reverse", 0, func(ctx *runtime.Ctx, in *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		if in.Kind() != value.KindArray {
			return &query.RuntimeError{Msg: "node at path " + cursorPath(ctx) + " is not an array (it's a " + tagOf(in) + ")"}
		}
		out := slices.Clone(in.Elems())
		slices.Reverse(out)
		return emit(like(in, value.Arr(out...)), nil)
	})
	r.RegisterSingle("_yq_flatten", 1, func(_ *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		return args[0].Each(in, func(depth *value.Value, _ value.Path) error {
			if in.Kind() != value.KindArray {
				return &query.RuntimeError{Msg: "only arrays are supported for flatten"}
			}
			n := -1
			if depth.Kind() == value.KindNumber {
				n = int(depth.Float())
			}
			return emit(like(in, value.Arr(flatten(in, n)...)), nil)
		})
	})
	r.RegisterSingle("sort_keys", 1, sortKeys)
	r.Register("min", 0, func(_ *runtime.Ctx, in *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		return extreme(in, -1, emit)
	})
	r.Register("max", 0, func(_ *runtime.Ctx, in *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		return extreme(in, 1, emit)
	})
	r.RegisterSingle("any_c", 1, func(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		return anyAll(ctx, "any", in, args[0], true, emit)
	})
	r.RegisterSingle("all_c", 1, func(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		return anyAll(ctx, "all", in, args[0], false, emit)
	})
}

// like returns v written in the style of in, the collection it was built
// from, with in's comments, as yq rebuilds a collection in place.
func like(in, v *value.Value) *value.Value {
	m := in.Meta()
	if m == nil || v.Kind() != value.KindArray && v.Kind() != value.KindObject {
		return v
	}
	nm := value.Meta{Style: m.Style & value.Flow, HeadComment: m.HeadComment, LineComment: m.LineComment, FootComment: m.FootComment}
	if nm == (value.Meta{}) {
		return v
	}
	return v.WithMeta(&nm)
}

// members are a collection's elements, or a map's values.
func members(v *value.Value) []*value.Value {
	if v.Kind() == value.KindObject {
		out := make([]*value.Value, 0, v.Len())
		for _, e := range v.Entries() {
			out = append(out, e.Value)
		}
		return out
	}
	return v.Elems()
}

// keysOf runs f on each member, or takes the member itself when f is nil,
// and gathers every output as that member's sort key.
func keysOf(in *value.Value, f runtime.Thunk) ([][]*value.Value, error) {
	ms := members(in)
	keys := make([][]*value.Value, len(ms))
	for i, m := range ms {
		if f == nil {
			keys[i] = []*value.Value{m}
			continue
		}
		err := f.Each(m, func(k *value.Value, _ value.Path) error {
			keys[i] = append(keys[i], k)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return keys, nil
}

// sortBy sorts a sequence, or a map by its values, stably by yq's order of
// the keys f gives.
func sortBy(ctx *runtime.Ctx, in *value.Value, f runtime.Thunk, emit runtime.Emit) error {
	if in.Kind() != value.KindArray && in.Kind() != value.KindObject {
		return &query.RuntimeError{Msg: "node at path " + cursorPath(ctx) + " is not an array or map (it's a " + tagOf(in) + ")"}
	}
	keys, err := keysOf(in, f)
	if err != nil {
		return err
	}
	order := make([]int, len(keys))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return compareKeys(keys[a], keys[b]) })
	if in.Kind() == value.KindObject {
		entries := in.Entries()
		out := make([]value.Entry, len(order))
		for i, j := range order {
			out[i] = entries[j]
		}
		return emit(like(in, value.ObjEntries(out...)), nil)
	}
	out := make([]*value.Value, len(order))
	for i, j := range order {
		out[i] = in.Elems()[j]
	}
	return emit(like(in, value.Arr(out...)), nil)
}

// cursorPath is the cursor's path as yq writes it in a message, such as
// [a.b[0]], and [] at a root.
func cursorPath(ctx *runtime.Ctx) string {
	if c := ctx.Cursor; c != nil {
		rel, _ := nodePath(c)
		return "[" + keysText(c.Root, rel) + "]"
	}
	return "[]"
}

// pathText is where a value came from as yq writes it in a message, and
// empty for a value that came from no document.
func pathText(p value.Path) string {
	root, _, rel := runtime.Unroot(p)
	if root == nil {
		return ""
	}
	return keysText(root, rel)
}

// keysText writes the path rel below root as yq does, as a.b[0].
func keysText(root *value.Value, rel value.Path) string {
	var b strings.Builder
	p, _ := resolve(root, rel)
	for i, k := range p {
		switch {
		case k.Kind() == value.KindNumber:
			b.WriteString("[" + numberText(k) + "]")
		case i == 0:
			b.WriteString(k.Str())
		default:
			b.WriteString("." + k.Str())
		}
	}
	return b.String()
}

// compareKeys orders two key lists element by element.
func compareKeys(a, b []*value.Value) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if c := compareNodes(a[i], b[i]); c != 0 {
			return c
		}
	}
	return len(a) - len(b)
}

// compareNodes is yq's sort order. Null comes first and the booleans
// next, false before true. Two numbers compare by value, and any other
// pair compares by its text, which is empty for a collection.
func compareNodes(a, b *value.Value) int {
	if c := rank(a) - rank(b); c != 0 || rank(a) == 0 {
		return c
	}
	if rank(a) == 1 {
		return boolRank(a) - boolRank(b)
	}
	if a.Kind() == value.KindNumber && b.Kind() == value.KindNumber {
		return value.Compare(a.WithMeta(nil), b.WithMeta(nil))
	}
	return strings.Compare(nodeText(a), nodeText(b))
}

func rank(v *value.Value) int {
	switch v.Kind() {
	case value.KindNull:
		return 0
	case value.KindTrue, value.KindFalse:
		return 1
	default:
	}
	return 2
}

func boolRank(v *value.Value) int {
	if v.Kind() == value.KindTrue {
		return 1
	}
	return 0
}

// identity is the text yq tells equal nodes apart by in unique and
// group_by: a scalar's text, or a collection's YAML, so "1" and 1 are
// the same key.
func identity(keys []*value.Value) string {
	var b strings.Builder
	for _, k := range keys {
		if k.Kind() == value.KindArray || k.Kind() == value.KindObject {
			b.WriteString(yamlText(k, 2))
		} else {
			b.WriteString(nodeText(k))
		}
		b.WriteByte(0)
	}
	return b.String()
}

// uniqueBy keeps the first member for each key f gives, in input order.
func uniqueBy(in *value.Value, f runtime.Thunk, emit runtime.Emit) error {
	if in.Kind() != value.KindArray {
		return &query.RuntimeError{Msg: "only arrays are supported for unique, got " + tagOf(in)}
	}
	keys, err := keysOf(in, f)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	var out []*value.Value
	for i, e := range in.Elems() {
		id := identity(keys[i])
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, e)
	}
	return emit(like(in, value.Arr(out...)), nil)
}

// groupBy gathers members with the same key into new block sequences, the
// groups in the order their first members come.
func groupBy(_ *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
	if in.Kind() != value.KindArray {
		return &query.RuntimeError{Msg: "only arrays are supported for group by, got " + tagOf(in)}
	}
	keys, err := keysOf(in, args[0])
	if err != nil {
		return err
	}
	index := map[string]int{}
	var groups [][]*value.Value
	for i, e := range in.Elems() {
		id := identity(keys[i])
		g, ok := index[id]
		if !ok {
			g = len(groups)
			index[id] = g
			groups = append(groups, nil)
		}
		groups[g] = append(groups[g], e)
	}
	out := make([]*value.Value, len(groups))
	for i, g := range groups {
		out[i] = value.Arr(g...)
	}
	return emit(value.Arr(out...), nil)
}

// flatten lifts nested sequences' elements into place, depth levels deep,
// or all the way down when depth is negative.
func flatten(v *value.Value, depth int) []*value.Value {
	var out []*value.Value
	for _, e := range v.Elems() {
		if e.Kind() == value.KindArray && depth != 0 {
			out = append(out, flatten(e, depth-1)...)
			continue
		}
		out = append(out, e)
	}
	return out
}

// sortKeys sorts the keys of each map the argument names and gives the
// input back with them sorted, which with ".." sorts every map in it.
func sortKeys(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
	var base value.Path
	if c := ctx.Cursor; c != nil {
		base, _ = nodePath(c)
	}
	var paths []value.Path
	err := args[0].Each(in, func(_ *value.Value, p value.Path) error {
		if p == nil {
			return nil
		}
		root, _, rel := runtime.Unroot(p)
		if root != nil {
			if !hasPrefix(rel, base) {
				return nil
			}
			rel = rel[len(base):]
		}
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		return err
	}
	ed := builtin.NewEditor(ctx, in)
	for i := len(paths) - 1; i >= 0; i-- {
		m, err := ed.Get(paths[i])
		if err != nil || m.Kind() != value.KindObject {
			continue
		}
		entries := slices.Clone(m.Entries())
		slices.SortStableFunc(entries, func(a, b value.Entry) int { return strings.Compare(a.Key.Str(), b.Key.Str()) })
		sorted := value.ObjEntries(entries...)
		if mm := m.Meta(); mm != nil {
			sorted = sorted.WithMeta(mm)
		}
		if err := ed.Set(paths[i], sorted); err != nil {
			return err
		}
	}
	return emit(ed.Value(), nil)
}

// hasPrefix reports whether p starts with the components of prefix.
func hasPrefix(p, prefix value.Path) bool {
	if len(p) < len(prefix) {
		return false
	}
	for i, k := range prefix {
		if !value.Equal(k.WithMeta(nil), p[i].WithMeta(nil)) {
			return false
		}
	}
	return true
}

// extreme emits the least member for sign -1 and the greatest for 1, and
// nothing for an empty collection. The members must be all numbers or all
// of one tag.
func extreme(in *value.Value, sign int, emit runtime.Emit) error {
	if in.Kind() != value.KindArray && in.Kind() != value.KindObject {
		return &query.RuntimeError{Msg: tagOf(in) + " not yet supported for comparison"}
	}
	ms := members(in)
	if len(ms) == 0 {
		return nil
	}
	best := ms[0]
	for _, m := range ms[1:] {
		if orderClass(best) != orderClass(m) {
			return &query.RuntimeError{Msg: tagOf(m) + " not yet supported for comparison"}
		}
		if compareNodes(m, best)*sign > 0 {
			best = m
		}
	}
	if orderClass(best) == "" {
		return &query.RuntimeError{Msg: tagOf(best) + " not yet supported for comparison"}
	}
	return emit(best, nil)
}

// orderClass is the class a node compares within for min and max, and
// empty for one that does not compare.
func orderClass(v *value.Value) string {
	switch v.Kind() {
	case value.KindNumber:
		return "number"
	case value.KindString:
		return tagOf(v)
	case value.KindTrue, value.KindFalse:
		return "bool"
	default:
	}
	return ""
}

// anyAll reports whether cond holds for any member, or for all of them.
func anyAll(ctx *runtime.Ctx, name string, in *value.Value, cond runtime.Thunk, want bool, emit runtime.Emit) error {
	if in.Kind() != value.KindArray {
		return &query.RuntimeError{Msg: name + " only supports arrays, was " + tagOf(in)}
	}
	for _, e := range in.Elems() {
		hit := false
		err := cond.Each(e, func(v *value.Value, _ value.Path) error {
			if ctx.Dialect.Truthy(v) {
				hit = true
			}
			return nil
		})
		if err != nil {
			return err
		}
		if hit == want {
			return emit(value.Bool(want), nil)
		}
	}
	return emit(value.Bool(!want), nil)
}

// yamlText is v written as a YAML document with indent spaces a level,
// without the line break that ends it.
func yamlText(v *value.Value, indent int) string {
	var b bytes.Buffer
	enc := yamlout.New(&b, yamlout.Options{Indent: indent})
	if err := enc.Write(v); err != nil {
		return ""
	}
	if err := enc.Flush(); err != nil {
		return ""
	}
	return strings.TrimSuffix(b.String(), "\n")
}

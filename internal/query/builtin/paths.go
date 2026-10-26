package builtin

import (
	"math"
	"slices"

	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// maxArrayIndex is the largest index setpath grows an array to.
const maxArrayIndex = 1 << 29

func registerPaths(r *runtime.Registry) {
	binary(r, "getpath", func(ctx *runtime.Ctx, in, path *value.Value) (*value.Value, error) {
		if path.Kind() != value.KindArray {
			return nil, fail("Path must be specified as an array")
		}
		return GetPath(ctx, in, path.Elems())
	})
	r.RegisterSingle("setpath", 2, func(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		return cartesian(in, args, func(vals []*value.Value) error {
			if vals[0].Kind() != value.KindArray {
				return fail("Path must be specified as an array")
			}
			out, err := SetPath(ctx, in, vals[0].Elems(), vals[1])
			if err != nil {
				return err
			}
			return emit(out, nil)
		})
	})
	binary(r, "delpaths", func(ctx *runtime.Ctx, in, paths *value.Value) (*value.Value, error) {
		if paths.Kind() != value.KindArray {
			return nil, fail("Paths must be specified as an array")
		}
		list := make([]value.Path, 0, paths.Len())
		for _, p := range paths.Elems() {
			if p.Kind() != value.KindArray {
				return nil, fail("Path must be specified as array, not %s", runtime.TypeName(p))
			}
			list = append(list, p.Elems())
		}
		return DelPaths(ctx, in, list)
	})
}

// Index is jq's .[key]. A null input gives null for a string, a number,
// or an object key, an object gives the value under a string key, and an
// array gives the element at a number, the slice at a slice object, or
// the indices where an array key starts, matching elements with
// ctx.Equal. It returns nil, nil for no output, which jq's index never
// asks for.
//
//nolint:gocyclo // one case for each container and key kind jq indexes
func Index(ctx *runtime.Ctx, c, key *value.Value) (*value.Value, error) {
	ck, kk := c.Kind(), key.Kind()
	if kk == value.KindObject && ck != value.KindObject && ck != value.KindNull {
		start, end, ok := value.IsSliceComponent(key)
		if !ok && (ck == value.KindArray || ck == value.KindString) {
			return nil, fail("Array/string slice indices must be integers")
		}
		if ok {
			return Slice(c, start, end)
		}
	}
	switch {
	case ck == value.KindNull && (kk == value.KindString || kk == value.KindNumber || kk == value.KindObject):
		return value.Null(), nil
	case ck == value.KindObject && kk == value.KindString:
		if v, ok := c.Get(key.Str()); ok {
			return v, nil
		}
		return value.Null(), nil
	case ck == value.KindArray && kk == value.KindNumber:
		i, ok := arrayIndex(key, c.Len())
		if !ok || i < 0 {
			return value.Null(), nil
		}
		if v, ok := c.Index(i); ok {
			return v, nil
		}
		return value.Null(), nil
	case ck == value.KindArray && kk == value.KindArray:
		return indicesOf(c.Elems(), key.Elems(), ctx.Equal), nil
	}
	return nil, fail("Cannot index %s with %s", runtime.TypeName(c), describe(key))
}

// arrayIndex turns a number into an index into n elements, truncated
// toward zero and counted from the end when negative. It reports false
// for NaN.
func arrayIndex(key *value.Value, n int) (int, bool) {
	f := key.Float()
	if math.IsNaN(f) {
		return 0, false
	}
	f = math.Trunc(f)
	if f >= maxArrayIndex*4 {
		return math.MaxInt32, true
	}
	if f <= -maxArrayIndex*4 {
		return -1, true
	}
	i := int(f)
	if i < 0 {
		i += n
	}
	return i, true
}

// indicesOf returns the positions in elems where sub starts, and an empty
// array for an empty sub.
func indicesOf(elems, sub []*value.Value, eq func(a, b *value.Value) bool) *value.Value {
	b := value.NewArrBuilder(0)
	if len(sub) == 0 {
		return b.Seal()
	}
	for i := 0; i+len(sub) <= len(elems); i++ {
		match := true
		for j, s := range sub {
			if !eq(elems[i+j], s) {
				match = false
				break
			}
		}
		if match {
			b.Append(value.Num(float64(i)))
		}
	}
	return b.Seal()
}

// Slice is jq's .[start:end] on an array, a string, or null. Bounds count
// from the end when negative, the start rounds down and the end rounds up,
// and both clamp to the value's length. Strings slice by codepoint.
func Slice(c, start, end *value.Value) (*value.Value, error) {
	for _, b := range []*value.Value{start, end} {
		if b.Kind() != value.KindNull && b.Kind() != value.KindNumber {
			return nil, fail("Array/string slice indices must be integers")
		}
	}
	switch c.Kind() {
	case value.KindNull:
		return value.Null(), nil
	case value.KindArray:
		s, e := sliceBounds(c.Len(), start, end)
		return value.Arr(slices.Clone(c.Elems()[s:e])...), nil
	case value.KindString:
		runes := []rune(c.Str())
		s, e := sliceBounds(len(runes), start, end)
		return value.Str(string(runes[s:e])), nil
	default:
	}
	key := value.Obj(value.Pair{Key: "start", Value: start}, value.Pair{Key: "end", Value: end})
	return nil, fail("Cannot index %s with %s", runtime.TypeName(c), describe(key))
}

// sliceBounds resolves a slice's bounds against n elements.
func sliceBounds(n int, start, end *value.Value) (int, int) {
	bound := func(v *value.Value, def int, round func(float64) float64) int {
		if v.Kind() == value.KindNull {
			return def
		}
		f := round(v.Float())
		if math.IsNaN(f) {
			return 0
		}
		if f < 0 {
			f += float64(n)
		}
		return int(min(max(f, 0), float64(n)))
	}
	s, e := bound(start, 0, math.Floor), bound(end, n, math.Ceil)
	return s, max(s, e)
}

// GetPath is jq's getpath. A null anywhere along the path gives null.
func GetPath(ctx *runtime.Ctx, v *value.Value, path value.Path) (*value.Value, error) {
	for _, c := range path {
		if v.Kind() == value.KindNull {
			return value.Null(), nil
		}
		var err error
		if v, err = Index(ctx, v, c); err != nil {
			return nil, err
		}
	}
	return v, nil
}

// SetPath is jq's setpath. A null along the way becomes the container its
// key asks for, and an index past the end of an array pads it with nulls.
func SetPath(ctx *runtime.Ctx, v *value.Value, path value.Path, nv *value.Value) (*value.Value, error) {
	e := NewEditor(ctx, v)
	if err := e.Set(path, nv); err != nil {
		return nil, err
	}
	return e.Value(), nil
}

// DelPaths is jq's delpaths. It sorts the paths, and on each container it
// removes every key that ends a path in one pass, with negative indices
// and slices resolved against the container as it was, after deleting
// inside the children that longer paths reach. A null along the way stops
// a path quietly.
func DelPaths(ctx *runtime.Ctx, v *value.Value, paths []value.Path) (*value.Value, error) {
	sorted := slices.Clone(paths)
	slices.SortStableFunc(sorted, func(a, b value.Path) int {
		return comparePaths(ctx, a, b)
	})
	return delSorted(ctx, v, sorted)
}

func comparePaths(ctx *runtime.Ctx, a, b value.Path) int {
	for i := range min(len(a), len(b)) {
		if c := ctx.Compare(a[i], b[i]); c != 0 {
			return c
		}
	}
	return len(a) - len(b)
}

func delSorted(ctx *runtime.Ctx, v *value.Value, paths []value.Path) (*value.Value, error) {
	if len(paths) == 0 {
		return v, nil
	}
	if len(paths[0]) == 0 {
		return value.Null(), nil
	}
	if v.Kind() == value.KindNull {
		return v, nil
	}
	var keys []*value.Value
	// Groups run from the last key down, so a deletion inside a child
	// never shifts a key that a later group still has to find.
	for hi := len(paths); hi > 0; {
		lo := hi - 1
		for lo > 0 && ctx.Compare(paths[lo-1][0], paths[hi-1][0]) == 0 {
			lo--
		}
		group := paths[lo:hi]
		key := group[0][0]
		if len(group[0]) == 1 {
			keys = append(keys, key)
		} else {
			if err := deletable(v, key); err != nil {
				return nil, err
			}
			child, err := Index(ctx, v, key)
			if err != nil {
				return nil, err
			}
			rest := make([]value.Path, len(group))
			for i, p := range group {
				rest[i] = p[1:]
			}
			kept, err := delSorted(ctx, child, rest)
			if err != nil {
				return nil, err
			}
			if kept != child {
				if v, err = SetPath(ctx, v, value.Path{key}, kept); err != nil {
					return nil, err
				}
			}
		}
		hi = lo
	}
	return deleteKeys(v, keys)
}

// deletable checks that key can address something to delete inside v.
func deletable(v, key *value.Value) error {
	switch v.Kind() {
	case value.KindObject:
		if key.Kind() != value.KindString {
			return fail("Cannot delete %s field of object", runtime.TypeName(key))
		}
	case value.KindArray:
		if key.Kind() == value.KindNumber {
			return nil
		}
		if _, _, ok := value.IsSliceComponent(key); !ok {
			return fail("Cannot delete %s element of array", runtime.TypeName(key))
		}
	default:
		return fail("Cannot delete fields from %s", runtime.TypeName(v))
	}
	return nil
}

// deleteKeys removes every key in keys from v at once.
func deleteKeys(v *value.Value, keys []*value.Value) (*value.Value, error) {
	if len(keys) == 0 {
		return v, nil
	}
	for _, k := range keys {
		if err := deletable(v, k); err != nil {
			return nil, err
		}
	}
	if v.Kind() == value.KindObject {
		b := value.NewObjBuilderFrom(v)
		for _, k := range keys {
			b.Delete(k.Str())
		}
		return b.Seal(), nil
	}
	n := v.Len()
	drop := make([]bool, n)
	for _, k := range keys {
		if start, end, ok := value.IsSliceComponent(k); ok {
			s, e := sliceBounds(n, start, end)
			for i := s; i < e; i++ {
				drop[i] = true
			}
			continue
		}
		if i, ok := arrayIndex(k, n); ok && i >= 0 && i < n {
			drop[i] = true
		}
	}
	b := value.NewArrBuilder(n)
	for i, e := range v.Elems() {
		if !drop[i] {
			b.Append(e)
		}
	}
	// The array keeps its details, as the object's builder keeps them.
	if m := v.Meta(); m != nil {
		return b.Seal().WithMeta(m), nil
	}
	return b.Seal(), nil
}

// Editor applies a run of setpath edits to one value and copies each
// container at most once while it stays open. An edit that reaches into a
// container opens it on a builder and records the child it changed, and
// Value seals every open container from the leaves up. So `.[] |= f` over
// n elements copies the array once rather than n times.
type Editor struct {
	ctx  *runtime.Ctx
	root slot
}

// slot holds a value as it stands, either as a sealed value or as an
// open container.
type slot struct {
	v *value.Value
	n *openNode
}

// openNode is a container being edited. base is the container as it was
// opened. An object's kids are the children edited since, in the order
// first touched, and added counts the kids whose keys base lacked. An
// array's elems hold a slot for every element, and a slot whose value is
// still nil was never touched, so opening an array costs one allocation
// however many elements change.
type openNode struct {
	base  *value.Value
	order []string
	kids  map[string]*slot
	added int
	elems []slot
}

// NewEditor starts editing v under the run's context.
func NewEditor(ctx *runtime.Ctx, v *value.Value) *Editor {
	return &Editor{ctx: ctx, root: slot{v: v}}
}

// Value seals the edits so far and returns the result. The editor can go
// on editing after it.
func (e *Editor) Value() *value.Value {
	return e.root.seal()
}

// Get is GetPath on the value as edited so far.
func (e *Editor) Get(path value.Path) (*value.Value, error) {
	s := &e.root
	for i, c := range path {
		if s.n == nil {
			return GetPath(e.ctx, s.v, path[i:])
		}
		kid, ok := s.n.child(c, false)
		if !ok {
			return GetPath(e.ctx, s.seal(), path[i:])
		}
		s = kid
	}
	return s.seal(), nil
}

// Set is SetPath on the value as edited so far.
func (e *Editor) Set(path value.Path, nv *value.Value) error {
	if o := e.ctx.Owner; o != nil {
		// nv is about to be shared, so the run's owner lets it go.
		o.Disown(nv)
		if e.root.n == nil && len(path) > 0 {
			if done, err := e.setOwned(o, path, nv); done {
				return err
			}
		}
	}
	s := &e.root
	for i, c := range path {
		if s.n == nil && !s.open(c) {
			out, err := setPersistent(s.v, path[i:], nv)
			if err != nil {
				return err
			}
			*s = slot{v: out}
			return nil
		}
		kid, ok := s.n.child(c, true)
		if !ok {
			out, err := setPersistent(s.seal(), path[i:], nv)
			if err != nil {
				return err
			}
			*s = slot{v: out}
			return nil
		}
		s = kid
	}
	*s = slot{v: nv}
	return nil
}

// setOwned sets path in a root that o holds, or takes and holds a copy
// of, changing the root in place. Only the root is held, so a longer path
// copies the child it passes through. It reports false, having done
// nothing, for a path the root cannot hold in place, which Set then
// handles as usual, errors included.
func (e *Editor) setOwned(o *value.Owner, path value.Path, nv *value.Value) (bool, error) {
	root, c := e.root.v, path[0]
	switch {
	case root.Kind() == value.KindObject && c.Kind() == value.KindString:
		child, err := ownedChild(path, nv, func() *value.Value {
			v, _ := root.Get(c.Str())
			return v
		})
		if err != nil {
			return true, err
		}
		e.root.v = o.Set(root, c.Str(), child)
		return true, nil
	case root.Kind() == value.KindArray && c.Kind() == value.KindNumber:
		i, ok := arrayIndex(c, root.Len())
		if !ok || i < 0 || i > root.Len() {
			return false, nil
		}
		child, err := ownedChild(path, nv, func() *value.Value {
			v, _ := root.Index(i)
			return v
		})
		if err != nil {
			return true, err
		}
		e.root.v = o.SetIndex(root, i, child)
		return true, nil
	}
	return false, nil
}

// ownedChild is the value setOwned puts in the root: nv itself for a path
// of one key, and otherwise the child that cur returns, or null, with the
// rest of the path set in a copy of it.
func ownedChild(path value.Path, nv *value.Value, cur func() *value.Value) (*value.Value, error) {
	if len(path) == 1 {
		return nv, nil
	}
	c := cur()
	if c == nil {
		c = value.Null()
	}
	return setPersistent(c, path[1:], nv)
}

// open starts editing a sealed container when c addresses an existing
// child of it, and reports whether it did.
func (s *slot) open(c *value.Value) bool {
	switch {
	case s.v.Kind() == value.KindObject && c.Kind() == value.KindString:
	case s.v.Kind() == value.KindArray && c.Kind() == value.KindNumber:
		if i, ok := arrayIndex(c, s.v.Len()); !ok || i < 0 || i >= s.v.Len() {
			return false
		}
	default:
		return false
	}
	s.n = &openNode{base: s.v}
	s.v = nil
	return true
}

// child returns the slot for the child that c addresses, creating it from
// the base when create is set. It reports false when c is not a plain key
// of this container or names an array element out of range.
func (n *openNode) child(c *value.Value, create bool) (*slot, bool) {
	switch {
	case n.base.Kind() == value.KindObject && c.Kind() == value.KindString:
		key := c.Str()
		if kid, ok := n.kids[key]; ok {
			return kid, true
		}
		cur, ok := n.base.Get(key)
		if !ok {
			if !create {
				return nil, false
			}
			cur = value.Null()
			n.added++
		}
		if n.kids == nil {
			n.kids = map[string]*slot{}
		}
		kid := &slot{v: cur}
		n.kids[key] = kid
		n.order = append(n.order, key)
		return kid, true
	case n.base.Kind() == value.KindArray && c.Kind() == value.KindNumber:
		i, ok := arrayIndex(c, n.base.Len())
		if !ok || i < 0 || i >= n.base.Len() {
			return nil, false
		}
		if n.elems == nil {
			n.elems = make([]slot, n.base.Len())
		}
		kid := &n.elems[i]
		if kid.v == nil && kid.n == nil {
			kid.v = n.base.Elems()[i]
		}
		return kid, true
	}
	return nil, false
}

// seal closes an open container and every open container under it, and
// returns the value the slot now holds.
func (s *slot) seal() *value.Value {
	if s.n == nil {
		return s.v
	}
	n := s.n
	if n.base.Kind() == value.KindArray {
		b := value.NewArrBuilderFrom(n.base)
		for i := range n.elems {
			if kid := &n.elems[i]; kid.v != nil || kid.n != nil {
				b.Set(i, kid.seal())
			}
		}
		s.v = b.Seal()
	} else if n.added == len(n.order) {
		// Only new keys, which Set adds in the base's spare room when it
		// has some, so a loop that adds a key a step stays linear.
		v := n.base
		for _, k := range n.order {
			v = v.Set(k, n.kids[k].seal())
		}
		s.v = v
	} else {
		b := value.NewObjBuilderFrom(n.base)
		for _, k := range n.order {
			b.Set(k, n.kids[k].seal())
		}
		s.v = b.Seal()
	}
	s.n = nil
	return s.v
}

// grownFrom is the container a null grows into, keeping the null's
// comments, anchor, leading content, and position, as yq keeps them on
// the node it turns into a container. A null with no details, which is
// every null jq loads, grows into the bare container.
func grownFrom(null, container *value.Value) *value.Value {
	m := null.Meta()
	if m == nil {
		return container
	}
	kept := *m
	kept.Tag, kept.Style, kept.Text = "", 0, ""
	return container.WithMeta(&kept)
}

// setPersistent is setpath on a sealed value for the edits the editor does
// not keep open: growing null into a container, padding an array, slices,
// and every error.
//
//nolint:gocyclo // one case for each container and key kind jq sets
func setPersistent(v *value.Value, path value.Path, nv *value.Value) (*value.Value, error) {
	if len(path) == 0 {
		return nv, nil
	}
	c := path[0]
	if v.Kind() == value.KindNull {
		null := v
		switch c.Kind() {
		case value.KindString:
			v = grownFrom(null, value.Obj())
		case value.KindNumber, value.KindObject:
			v = grownFrom(null, value.Arr())
		default:
		}
	}
	switch {
	case v.Kind() == value.KindObject && c.Kind() == value.KindString:
		cur, ok := v.Get(c.Str())
		if !ok {
			cur = value.Null()
		}
		child, err := setPersistent(cur, path[1:], nv)
		if err != nil {
			return nil, err
		}
		return v.Set(c.Str(), child), nil
	case v.Kind() == value.KindArray && c.Kind() == value.KindNumber:
		f := c.Float()
		if math.IsNaN(f) {
			return nil, fail("Cannot set array element at NaN index")
		}
		if math.Trunc(f) > maxArrayIndex {
			return nil, fail("Array index too large")
		}
		i, _ := arrayIndex(c, v.Len())
		if i < 0 {
			return nil, fail("Out of bounds negative array index")
		}
		cur, ok := v.Index(i)
		if !ok {
			cur = value.Null()
		}
		child, err := setPersistent(cur, path[1:], nv)
		if err != nil {
			return nil, err
		}
		return v.SetIndex(i, child), nil
	case v.Kind() == value.KindArray && c.Kind() == value.KindObject:
		start, end, ok := value.IsSliceComponent(c)
		if !ok {
			return nil, fail("Cannot update field at object index of array")
		}
		cur, err := Slice(v, start, end)
		if err != nil {
			return nil, err
		}
		repl, err := setPersistent(cur, path[1:], nv)
		if err != nil {
			return nil, err
		}
		if repl.Kind() != value.KindArray {
			return nil, fail("A slice of an array can only be assigned another array")
		}
		s, e := sliceBounds(v.Len(), start, end)
		return value.Arr(slices.Concat(v.Elems()[:s], repl.Elems(), v.Elems()[e:])...), nil
	case v.Kind() == value.KindArray && c.Kind() == value.KindArray:
		return nil, fail("Cannot update field at array index of array")
	}
	return nil, fail("Cannot index %s with %s", runtime.TypeName(v), describe(c))
}

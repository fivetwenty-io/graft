package yq

import (
	"fmt"
	"math"

	"github.com/fivetwenty-io/graft/internal/query/builtin"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// registerPaths adds the operators that say where a node is: its line
// and column, its document and file, the nodes that hold it, and its
// document's root.
func registerPaths(r *runtime.Registry) {
	r.Register("line", 0, position(func(m *value.Meta) int { return m.Line }))
	r.Register("column", 0, position(func(m *value.Meta) int { return m.Column }))
	for _, name := range []string{"document_index", "documentIndex", "di"} {
		r.Register(name, 0, source(func(m *value.Meta) *value.Value { return value.Num(float64(m.DocIndex)) }, value.Num(0)))
	}
	for _, name := range []string{"file_index", "fileIndex", "fi"} {
		r.Register(name, 0, source(func(m *value.Meta) *value.Value { return value.Num(float64(m.FileIndex)) }, value.Num(0)))
	}
	for _, name := range []string{"filename", "file_name", "fileName"} {
		r.Register(name, 0, source(func(m *value.Meta) *value.Value { return value.Str(m.Filename) }, value.Str("")))
	}
	r.Register("parent", 0, func(ctx *runtime.Ctx, in *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		return parentAt(ctx, in, 1, emit)
	})
	r.Register("parent", 1, func(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		return args[0].Each(in, func(n *value.Value, _ value.Path) error {
			if f := n.Float(); n.Kind() != value.KindNumber || f != math.Trunc(f) {
				return &query.RuntimeError{Msg: fmt.Sprintf("parent needs an integer, not %s", nodeText(n))}
			}
			return parentAt(ctx, in, int(n.Float()), emit)
		})
	})
	r.Register("parents", 0, parents)
	r.Register("root", 0, func(ctx *runtime.Ctx, in *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		c := ctx.Cursor
		if c == nil {
			return emit(in, nil)
		}
		return emit(c.Root, runtime.Rooted(c.Root, c.Doc, value.Path{}))
	})
}

// position reads a line or column of the input, which is 0 for a node
// the input did not have, at the cursor's place as yq puts it.
func position(field func(*value.Meta) int) runtime.Native {
	return func(ctx *runtime.Ctx, in *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		n := 0
		if m := in.Meta(); m != nil {
			n = field(m)
		}
		return emitAtCursor(ctx, value.Num(float64(n)), emit)
	}
}

// source reads the document index, the file index, or the file name of
// the cursor's document from its root, so a node the document lacks has
// them too. A value the run built has none and reads as none.
func source(field func(*value.Meta) *value.Value, none *value.Value) runtime.Native {
	return func(ctx *runtime.Ctx, _ *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		v := none
		if c := ctx.Cursor; c != nil && c.Root != nil {
			if m := c.Root.Meta(); m != nil {
				v = field(m)
			}
		}
		return emitAtCursor(ctx, v, emit)
	}
}

// parentAt emits the node n levels above the cursor's node, and nothing
// when there are not that many. A negative n counts down from the root,
// so -1 is the root itself, and one past the node clamps to the node. A
// map key's levels are its value's, and 0 is the key itself. When the
// cursor's node is a key or element the input lacks, yq adds it as null
// so it has a parent, and the parent emitted is the one in the input with
// that null added.
func parentAt(ctx *runtime.Ctx, in *value.Value, n int, emit runtime.Emit) error {
	c := ctx.Cursor
	var rel value.Path
	if c != nil {
		rel, _ = nodePath(c)
	}
	depth := len(rel)
	at := depth - n
	if n < 0 {
		at = min(-n-1, depth)
	}
	switch {
	case at < 0:
		return nil
	case at == depth:
		var p value.Path
		if c != nil {
			p = runtime.Rooted(c.Root, c.Doc, c.Path)
		}
		return emit(in, p)
	default:
	}
	root, p, err := withNode(ctx, in, c, rel)
	if err != nil {
		return err
	}
	v, err := builtin.GetPath(ctx, root, p[:at])
	if err != nil {
		return err
	}
	return emit(v, runtime.Rooted(root, c.Doc, p[:at]))
}

// parents emits one new array of the nodes that hold the cursor's node,
// nearest first.
func parents(ctx *runtime.Ctx, in *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
	c := ctx.Cursor
	if c == nil {
		return emitDetached(value.Arr(), emit)
	}
	rel, _ := nodePath(c)
	root, p, err := withNode(ctx, in, c, rel)
	if err != nil {
		return err
	}
	out := make([]*value.Value, 0, len(p))
	for at := len(p) - 1; at >= 0; at-- {
		v, err := builtin.GetPath(ctx, root, p[:at])
		if err != nil {
			return err
		}
		out = append(out, v)
	}
	return emitDetached(value.Arr(out...), emit)
}

// emitDetached emits v as a new node that belongs to no document and has
// no place, which an output with no path of its own would take from the
// input.
func emitDetached(v *value.Value, emit runtime.Emit) error {
	return emit(v, runtime.Rooted(v, runtime.Detached, nil))
}

// withNode resolves rel in the cursor's root and returns the root with
// the node at rel added as null when the input lacks it, as yq adds it on
// reading it. The cursor's value must still be null, unless the cursor is
// on the node's key, which yq adds along with it.
func withNode(ctx *runtime.Ctx, in *value.Value, c *runtime.Cursor, rel value.Path) (*value.Value, value.Path, error) {
	p, found := resolve(c.Root, rel)
	_, isKey := nodePath(c)
	if found || len(p) == 0 || !isKey && in.Kind() != value.KindNull {
		return c.Root, p, nil
	}
	root, err := builtin.SetPath(ctx, c.Root, p, value.Null())
	return root, p, err
}

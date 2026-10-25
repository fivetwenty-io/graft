// Package jq is the jq dialect: jq's truthiness, indexing, iteration, and
// assignment, its builtins and prelude, and Compile, which turns a jq
// program into an evaluator program with every name resolved.
package jq

import (
	"iter"

	"github.com/fivetwenty-io/graft/internal/query/builtin"
	"github.com/fivetwenty-io/graft/internal/query/parser"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// Dialect implements runtime.Dialect for jq.
type Dialect struct{}

var _ runtime.Dialect = (*Dialect)(nil)

// New returns the jq dialect.
func New() *Dialect { return &Dialect{} }

// Grammar returns the grammar jq programs parse under.
func Grammar() parser.Grammar { return parser.JQ() }

// Name returns "jq".
func (*Dialect) Name() string { return "jq" }

// Builtins returns the registry of jq's natives and prelude, which is
// built once and shared.
func (*Dialect) Builtins() *runtime.Registry { return registry() }

// Capabilities lets jq read the environment and files, and nothing that
// runs other programs.
func (*Dialect) Capabilities() runtime.Capabilities {
	return runtime.Capabilities{Env: true, Files: true, System: false}
}

// Truthy is false for null and false and true for everything else.
func (*Dialect) Truthy(v *value.Value) bool {
	k := v.Kind()
	return k != value.KindNull && k != value.KindFalse
}

// Compare is jq's total order.
func (*Dialect) Compare(a, b *value.Value) int { return value.Compare(a, b) }

// Index is jq's .[key]. jq's index raises the same errors in every mode,
// and the evaluator drops them for the optional forms.
func (*Dialect) Index(container, key *value.Value, _ runtime.IndexMode) (*value.Value, error) {
	return builtin.Index(container, key)
}

// Iterate emits an array's elements with their indices and an object's
// values with their keys.
func (*Dialect) Iterate(v *value.Value, emit func(key, elem *value.Value) error) error {
	switch v.Kind() {
	case value.KindArray:
		for i, e := range v.Elems() {
			if err := emit(value.Num(float64(i)), e); err != nil {
				return err
			}
		}
		return nil
	case value.KindObject:
		for _, e := range v.Entries() {
			if err := emit(e.Key, e.Value); err != nil {
				return err
			}
		}
		return nil
	default:
	}
	return &query.RuntimeError{Msg: "Cannot iterate over " + runtime.TypeName(v) + " (" + runtime.Dump(v) + ")"}
}

// Recurse emits v and everything under it, depth first, each with its
// path below v. jq has no keys to visit, so includeKeys is ignored.
func (*Dialect) Recurse(v *value.Value, _ bool, emit func(*value.Value, value.Path) error) error {
	var walk func(v *value.Value, p value.Path) error
	walk = func(v *value.Value, p value.Path) error {
		if err := emit(v, p); err != nil {
			return err
		}
		switch v.Kind() {
		case value.KindArray:
			for i, e := range v.Elems() {
				if err := walk(e, extend(p, value.Num(float64(i)))); err != nil {
					return err
				}
			}
		case value.KindObject:
			for _, e := range v.Entries() {
				if err := walk(e.Value, extend(p, e.Key)); err != nil {
					return err
				}
			}
		default:
		}
		return nil
	}
	return walk(v, value.Path{})
}

// extend returns p with c appended, never sharing p's spare capacity.
func extend(p value.Path, c *value.Value) value.Path {
	out := make(value.Path, len(p)+1)
	copy(out, p)
	out[len(p)] = c
	return out
}

// Assign is jq's assignment. "=" and the arithmetic forms run rhs once on
// the root and emit one result for each of its outputs, with every path
// set. "|=" sets each path to the first output of rhs on its value, and
// deletes in one pass at the end every path whose update had no output.
// All edits to one result go through one builtin.Editor, so a container
// that many paths share is copied once.
func (*Dialect) Assign(root *value.Value, paths []value.Path, rhs func(cur *value.Value) iter.Seq2[*value.Value, error], mode runtime.AssignMode) iter.Seq2[*value.Value, error] {
	return func(yield func(*value.Value, error) bool) {
		if mode.Op() == runtime.AssignUpdate.Op() {
			yield(update(root, paths, rhs))
			return
		}
		for v, err := range rhs(root) {
			if err != nil {
				yield(nil, err)
				return
			}
			out, err := setAll(root, paths, v, mode)
			if !yield(out, err) || err != nil {
				return
			}
		}
	}
}

func setAll(root *value.Value, paths []value.Path, v *value.Value, mode runtime.AssignMode) (*value.Value, error) {
	ed := builtin.NewEditor(root)
	for _, p := range paths {
		nv := v
		if mode.Arith() {
			cur, err := ed.Get(p)
			if err != nil {
				return nil, err
			}
			if nv, err = mode.Apply(cur, v); err != nil {
				return nil, err
			}
		}
		if err := ed.Set(p, nv); err != nil {
			return nil, err
		}
	}
	return ed.Value(), nil
}

func update(root *value.Value, paths []value.Path, rhs func(cur *value.Value) iter.Seq2[*value.Value, error]) (*value.Value, error) {
	ed := builtin.NewEditor(root)
	var dels []value.Path
	// One yield serves every path, so taking the first output costs no
	// allocation per path.
	var first *value.Value
	var ferr error
	take := func(v *value.Value, err error) bool {
		first, ferr = v, err
		return false
	}
	for _, p := range paths {
		cur, err := ed.Get(p)
		if err != nil {
			return nil, err
		}
		first, ferr = nil, nil
		rhs(cur)(take)
		if ferr != nil {
			return nil, ferr
		}
		if first == nil {
			dels = append(dels, p)
			continue
		}
		if err := ed.Set(p, first); err != nil {
			return nil, err
		}
	}
	out := ed.Value()
	if len(dels) == 0 {
		return out, nil
	}
	return builtin.DelPaths(runtime.NewCtx(nil, runtime.NumbersExact), out, dels)
}

// FormatError returns the error's message, which the command prefixes.
func (*Dialect) FormatError(err error) string { return err.Error() }

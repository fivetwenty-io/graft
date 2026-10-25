package runtime

import (
	"fmt"
	"slices"

	"github.com/fivetwenty-io/graft/internal/query/ast"
)

// Registry holds a dialect's builtins, keyed by name and arity, and the
// prelude programs that define the rest in the dialect's own language.
// The zero value is empty and ready to use.
type Registry struct {
	natives map[nameArity]Native
	single  map[nameArity]bool
	prelude []ast.Node
}

type nameArity struct {
	name  string
	arity int
}

// Register adds fn as name/arity, replacing any earlier registration.
func (r *Registry) Register(name string, arity int, fn Native) {
	if r.natives == nil {
		r.natives = map[nameArity]Native{}
	}
	r.natives[nameArity{name, arity}] = fn
	delete(r.single, nameArity{name, arity})
}

// RegisterSingle adds fn as name/arity like Register, and records that fn
// emits at most once for each combination of its arguments' outputs and
// returns as soon as that emit returns. A self call can then loop as a
// tail call behind it, as in jq's until, so fn must not do anything after
// its emit but return the emit's error.
func (r *Registry) RegisterSingle(name string, arity int, fn Native) {
	r.Register(name, arity, fn)
	if r.single == nil {
		r.single = map[nameArity]bool{}
	}
	r.single[nameArity{name, arity}] = true
}

// Single reports whether name/arity was added with RegisterSingle.
func (r *Registry) Single(name string, arity int) bool {
	return r != nil && r.single[nameArity{name, arity}]
}

// Lookup returns the native registered as name/arity.
func (r *Registry) Lookup(name string, arity int) (Native, bool) {
	fn, ok := r.natives[nameArity{name, arity}]
	return fn, ok
}

// AddPrelude adds a parsed program whose definitions become builtins. Each
// program is a chain of definitions, and later programs see earlier ones.
func (r *Registry) AddPrelude(prog ast.Node) {
	r.prelude = append(r.prelude, prog)
}

// Prelude returns the prelude programs in the order they were added.
func (r *Registry) Prelude() []ast.Node {
	return r.prelude
}

// Names returns every builtin as name/arity, natives and prelude
// definitions together, sorted and without repeats.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.natives))
	for k := range r.natives {
		names = append(names, fmt.Sprintf("%s/%d", k.name, k.arity))
	}
	for _, prog := range r.prelude {
		for def, ok := prog.(*ast.FuncDef); ok; def, ok = def.Rest.(*ast.FuncDef) {
			names = append(names, fmt.Sprintf("%s/%d", def.Name, len(def.Params)))
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

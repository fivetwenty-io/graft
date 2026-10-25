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

package runtime

import (
	"fmt"
	"slices"
	"strings"

	"github.com/fivetwenty-io/graft/internal/query/ast"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// Registry holds a dialect's builtins, keyed by name and arity, and the
// prelude programs that define the rest in the dialect's own language.
// The zero value is empty and ready to use.
type Registry struct {
	natives map[nameArity]Native
	single  map[nameArity]bool
	ops     map[string]Operator
	prelude []ast.Node
}

// Operator is a binary operator's work on one pair of operands.
type Operator func(ctx *Ctx, l, r *value.Value) (*value.Value, error)

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
	if arity == 2 {
		delete(r.ops, name)
	}
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

// RegisterOperator adds op as the binary operator name/2. The native it
// registers runs op on every pair of its arguments' outputs, with the
// right one outermost, and Operator returns op itself, so the evaluator
// can run one pair of operands without building thunks. A later Register
// of name/2 replaces both.
func (r *Registry) RegisterOperator(name string, op Operator) {
	r.RegisterSingle(name, 2, func(ctx *Ctx, in *value.Value, args []Thunk, emit Emit) error {
		// The two loops are written out rather than run through a helper,
		// since arithmetic runs once for every element an update touches.
		return args[1].Each(in, func(rv *value.Value, _ value.Path) error {
			return args[0].Each(in, func(lv *value.Value, _ value.Path) error {
				v, err := op(ctx, lv, rv)
				if err != nil {
					return err
				}
				return emit(v, nil)
			})
		})
	})
	if r.ops == nil {
		r.ops = map[string]Operator{}
	}
	r.ops[name] = op
}

// Operator returns the operator registered as name with RegisterOperator,
// unless a later Register replaced it.
func (r *Registry) Operator(name string) (Operator, bool) {
	op, ok := r.ops[name]
	return op, ok
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

// Format returns the format native registered as name, such as
// "@base64". A name that isn't one gives the error jq gives, which names
// the format without its @, so @foo and format("foo") say the same thing.
func (r *Registry) Format(name string) (Native, error) {
	if fn, ok := r.Lookup(name, 0); ok {
		return fn, nil
	}
	return nil, &query.RuntimeError{Msg: strings.TrimPrefix(name, "@") + " is not a valid format"}
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

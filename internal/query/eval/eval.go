// Package eval runs a query syntax tree as a generator. Every node hands
// its outputs to a continuation, so a program streams results without
// collecting them, and errors travel back up the same calls.
package eval

import (
	"errors"
	"slices"
	"strings"

	"github.com/fivetwenty-io/graft/internal/query/ast"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/decimal"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// state is what every evaluator in one run shares.
type state struct {
	ctx   *runtime.Ctx
	reg   *runtime.Registry
	roots []*value.Value
	lits  map[*ast.Literal]*value.Value
	tails map[*ast.Call]tailSite
	used  map[callKey]bool

	labels uintptr
	envVal *value.Value
}

// evaluator walks the tree in one mode. With paths set it tracks the path
// of every output alongside the value, and mode is the index mode it hands
// the dialect.
type evaluator struct {
	*state
	paths bool
	mode  runtime.IndexMode
}

// Run evaluates prog against roots and passes every output to emit. A
// single root is the input. More than one root forms a candidate set, and
// each node that reads its input runs once for every root.
func Run(ctx *runtime.Ctx, prog ast.Node, env *Env, roots []*value.Value, emit runtime.Emit) error {
	ev := newEvaluator(ctx, prog, roots, false, runtime.IndexRead)
	return unwrapDownstream(ev.eval(prog, env, ev.input(), nil, emit))
}

// RunPaths is Run in path mode. It passes emit each output with its path,
// and it fails when an output is not a path into the input.
func RunPaths(ctx *runtime.Ctx, prog ast.Node, env *Env, roots []*value.Value, emit runtime.Emit) error {
	ev := newEvaluator(ctx, prog, roots, true, runtime.IndexRead)
	var root value.Path
	if len(roots) == 1 {
		root = value.Path{}
	}
	err := ev.eval(prog, env, ev.input(), root, func(v *value.Value, p value.Path) error {
		if p == nil {
			return invalidPathResult(v)
		}
		return emit(v, p)
	})
	return unwrapDownstream(err)
}

func newEvaluator(ctx *runtime.Ctx, prog ast.Node, roots []*value.Value, paths bool, mode runtime.IndexMode) evaluator {
	st := &state{
		ctx:   ctx,
		roots: roots,
		lits:  map[*ast.Literal]*value.Value{},
		reg:   &runtime.Registry{},
		used:  map[callKey]bool{},
	}
	if ctx.Dialect != nil {
		st.reg = ctx.Dialect.Builtins()
	}
	st.tails = tailSites(append([]ast.Node{prog}, st.reg.Prelude()...)...)
	if len(roots) == 1 {
		ctx.Cursor = &runtime.Cursor{Root: roots[0]}
	}
	return evaluator{state: st, paths: paths, mode: mode}
}

// input is the program's input, which is nil for a candidate set.
func (ev evaluator) input() *value.Value {
	if len(ev.roots) == 1 {
		return ev.roots[0]
	}
	return nil
}

// unwrapDownstream strips every downstream marker, so a caller sees the
// error its own continuation returned.
func unwrapDownstream(err error) error {
	for {
		d, ok := err.(*DownstreamError) //nolint:errorlint // only the outermost marker is stripped on each round
		if !ok {
			return err
		}
		err = d.Err
	}
}

// values returns the evaluator for subexpressions whose outputs are values
// rather than paths.
func (ev evaluator) values() evaluator {
	return evaluator{state: ev.state, mode: runtime.IndexRead}
}

// pathsFor returns the evaluator that collects the paths an assignment
// changes.
func (ev evaluator) pathsFor(mode runtime.IndexMode) evaluator {
	return evaluator{state: ev.state, paths: true, mode: mode}
}

// each runs f on in, or on every root in turn when in is the candidate
// set.
func (ev evaluator) each(in *value.Value, p value.Path, f func(in *value.Value, p value.Path) error) error {
	if in != nil {
		return f(in, p)
	}
	for i, r := range ev.roots {
		ev.ctx.Cursor = &runtime.Cursor{Root: r, Doc: i}
		var rp value.Path
		if ev.paths {
			rp = value.Path{}
		}
		if err := f(r, rp); err != nil {
			return err
		}
	}
	return nil
}

// eval runs n on in, whose path is p, and passes each output to emit.
//
//nolint:gocyclo // switch handles all expression types for evaluation
func (ev evaluator) eval(n ast.Node, env *Env, in *value.Value, p value.Path, emit runtime.Emit) error {
	switch n := n.(type) {
	case *ast.Identity:
		return ev.each(in, p, emit)
	case *ast.RecurseDefault:
		return ev.each(in, p, func(in *value.Value, p value.Path) error { return ev.recurse(in, p, emit) })
	case *ast.Field:
		return ev.field(n, env, in, p, emit)
	case *ast.Index:
		return ev.index(n, env, in, p, emit)
	case *ast.Slice:
		return ev.slice(n, env, in, p, emit)
	case *ast.Iterate:
		return ev.iterate(n, env, in, p, emit)
	case *ast.Literal:
		return emit(ev.literal(n), nil)
	case *ast.Str:
		return ev.str(n, env, in, emit)
	case *ast.FormatRef:
		return ev.each(in, p, func(in *value.Value, _ value.Path) error { return ev.format(n.Name, in, emit) })
	case *ast.ArrayCons:
		return ev.array(n, env, in, emit)
	case *ast.ObjectCons:
		return ev.object(n, env, in, emit)
	case *ast.Pipe:
		return ev.eval(n.L, env, in, p, func(v *value.Value, vp value.Path) error {
			return ev.eval(n.R, env, v, vp, emit)
		})
	case *ast.Comma:
		if err := ev.eval(n.L, env, in, p, emit); err != nil {
			return err
		}
		return ev.eval(n.R, env, in, p, emit)
	case *ast.Binary:
		return ev.binary(n, env, in, emit)
	case *ast.Unary:
		return ev.values().eval(n.X, env, in, nil, func(v *value.Value, _ value.Path) error {
			return ev.native("_negate", v, nil, emit)
		})
	case *ast.Alt:
		return ev.alt(n, env, in, p, emit)
	case *ast.Assign:
		return ev.assign(n, env, in, p, emit)
	case *ast.If:
		return ev.cond(n, 0, env, in, p, emit)
	case *ast.Try:
		return ev.try(n.Body, n.Catch, env, in, p, emit)
	case *ast.Optional:
		return ev.try(n.X, nil, env, in, p, emit)
	case *ast.Reduce:
		return ev.reduce(n, env, in, p, emit)
	case *ast.Foreach:
		return ev.foreach(n, env, in, p, emit)
	case *ast.FuncDef:
		fn := &Closure{Name: n.Name, Params: n.Params, Body: n.Body}
		inner := env.Def(fn)
		if n.Rest == nil {
			return ev.each(in, p, emit)
		}
		return ev.eval(n.Rest, inner, in, p, emit)
	case *ast.Call:
		return ev.call(n, env, in, p, emit)
	case *ast.Var:
		v, err := ev.variable(n.Name, env)
		if err != nil {
			return err
		}
		return emit(v, nil)
	case *ast.Bind:
		return ev.bind(n, env, in, p, emit)
	case *ast.Label:
		return ev.label(n, env, in, p, emit)
	case *ast.Break:
		id, ok := env.lookupLabel(n.Name)
		if !ok {
			return RuntimeError("$*label-" + n.Name + " is not defined")
		}
		return &BreakError{Label: id}
	case *ast.Loc:
		return emit(value.Obj(value.Pair{Key: "file", Value: value.Str(n.File)}, value.Pair{Key: "line", Value: value.Num(float64(n.Line))}), nil)
	case *ast.Module:
		if n.Rest == nil {
			return ev.each(in, p, emit)
		}
		return ev.eval(n.Rest, env, in, p, emit)
	case *ast.Import:
		return RuntimeError("cannot load module \"" + n.Path + "\": modules are not supported")
	case *ast.Include:
		return RuntimeError("cannot include \"" + n.Path + "\": modules are not supported")
	default:
		return RuntimeError("unsupported expression")
	}
}

// literal returns the value of a literal, parsing a number once per run.
func (ev evaluator) literal(n *ast.Literal) *value.Value {
	if v, ok := ev.lits[n]; ok {
		return v
	}
	var v *value.Value
	switch lit := n.Value.(type) {
	case bool:
		v = value.Bool(lit)
	case string:
		v = value.Str(lit)
	case ast.Number:
		if d, err := decimal.Parse(string(lit)); err == nil {
			v = value.NumLit(d, string(lit))
		} else {
			v = value.NumText(string(lit))
		}
	default:
		v = value.Null()
	}
	ev.lits[n] = v
	return v
}

// variable returns the value bound to $name. $ENV is read lazily and only
// when the environment capability is on.
func (ev evaluator) variable(name string, env *Env) (*value.Value, error) {
	if v, ok := env.Lookup(name); ok {
		return v, nil
	}
	if name != "ENV" {
		return nil, RuntimeError("$" + name + " is not defined")
	}
	if err := ev.ctx.Caps.RequireEnv(); err != nil {
		return nil, err
	}
	if ev.envVal == nil {
		vars := ev.ctx.Env()
		keys := make([]string, 0, len(vars))
		for k := range vars {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		b := value.NewObjBuilder(len(keys))
		for _, k := range keys {
			b.Set(k, value.Str(vars[k]))
		}
		ev.envVal = b.Seal()
	}
	return ev.envVal, nil
}

// str builds an interpolated string. The last interpolation varies
// slowest, as in jq, and a format renders each interpolated value through
// its native.
func (ev evaluator) str(n *ast.Str, env *Env, in *value.Value, emit runtime.Emit) error {
	if n.Format != "" {
		if _, ok := ev.reg.Lookup(n.Format, 0); !ok {
			return RuntimeError(n.Format + " is not a valid format")
		}
	}
	vev := ev.values()
	pieces := make([]string, len(n.Parts))
	var build func(i int) error
	build = func(i int) error {
		if i < 0 {
			return emit(value.Str(strings.Join(pieces, "")), nil)
		}
		if lit, ok := n.Parts[i].(*ast.Literal); ok {
			if s, ok := lit.Value.(string); ok {
				pieces[i] = s
				return build(i - 1)
			}
		}
		return vev.eval(n.Parts[i], env, in, nil, func(v *value.Value, _ value.Path) error {
			if n.Format == "" {
				pieces[i] = text(v)
				return build(i - 1)
			}
			return ev.format(n.Format, v, func(s *value.Value, _ value.Path) error {
				pieces[i] = text(s)
				return build(i - 1)
			})
		})
	}
	return build(len(n.Parts) - 1)
}

// text is a string's own text and any other value's JSON.
func text(v *value.Value) string {
	if v.Kind() == value.KindString {
		return v.Str()
	}
	return toJSON(v)
}

// format applies the format native called name, such as "@base64", to in.
func (ev evaluator) format(name string, in *value.Value, emit runtime.Emit) error {
	fn, ok := ev.reg.Lookup(name, 0)
	if !ok {
		return RuntimeError(name + " is not a valid format")
	}
	return fn(ev.ctx, in, nil, func(v *value.Value, _ value.Path) error { return emit(v, nil) })
}

// array collects every output of the body into one array.
func (ev evaluator) array(n *ast.ArrayCons, env *Env, in *value.Value, emit runtime.Emit) error {
	var elems []*value.Value
	if n.Body != nil {
		err := ev.values().eval(n.Body, env, in, nil, func(v *value.Value, _ value.Path) error {
			elems = append(elems, v)
			return nil
		})
		if err != nil {
			return err
		}
	}
	return emit(value.Arr(elems...), nil)
}

// object builds one object for every combination of its entries' outputs.
// The first entry varies slowest, and within an entry the key varies
// slower than the value.
func (ev evaluator) object(n *ast.ObjectCons, env *Env, in *value.Value, emit runtime.Emit) error {
	vev := ev.values()
	keys := make([]string, len(n.Entries))
	vals := make([]*value.Value, len(n.Entries))
	var build func(i int) error
	build = func(i int) error {
		if i == len(n.Entries) {
			b := value.NewObjBuilder(len(keys))
			for j, k := range keys {
				b.Set(k, vals[j])
			}
			return emit(b.Seal(), nil)
		}
		e := n.Entries[i]
		return vev.eval(e.Key, env, in, nil, func(k *value.Value, _ value.Path) error {
			if k.Kind() != value.KindString {
				return RuntimeError("Cannot use " + typeName(k) + " (" + dump(k) + ") as object key")
			}
			return vev.eval(e.Val, env, in, nil, func(v *value.Value, _ value.Path) error {
				keys[i], vals[i] = k.Str(), v
				return build(i + 1)
			})
		})
	}
	return build(0)
}

// typeName is the name jq gives a value's type in error messages.
func typeName(v *value.Value) string {
	switch v.Kind() {
	case value.KindNull:
		return "null"
	case value.KindFalse, value.KindTrue:
		return "boolean"
	case value.KindNumber:
		return "number"
	case value.KindString:
		return "string"
	case value.KindArray:
		return "array"
	default:
		return "object"
	}
}

// arithmetic maps each arithmetic operator to the native that implements
// it, so a dialect decides what the operators mean.
var arithmetic = map[string]string{
	"+": "_plus",
	"-": "_minus",
	"*": "_multiply",
	"/": "_divide",
	"%": "_mod",
}

// binary runs an operator. Arithmetic and comparison run the right operand
// outermost, as jq does. The boolean operators run the left one outermost
// and skip the right one when the left decides.
func (ev evaluator) binary(n *ast.Binary, env *Env, in *value.Value, emit runtime.Emit) error {
	vev := ev.values()
	switch n.Op {
	case "and", "or":
		return vev.eval(n.L, env, in, nil, func(l *value.Value, _ value.Path) error {
			lt := ev.ctx.Dialect.Truthy(l)
			if lt == (n.Op == "or") {
				return emit(value.Bool(lt), nil)
			}
			return vev.eval(n.R, env, in, nil, func(r *value.Value, _ value.Path) error {
				return emit(value.Bool(ev.ctx.Dialect.Truthy(r)), nil)
			})
		})
	}
	if name, ok := arithmetic[n.Op]; ok {
		return vev.eval(n.R, env, in, nil, func(r *value.Value, _ value.Path) error {
			return vev.eval(n.L, env, in, nil, func(l *value.Value, _ value.Path) error {
				return ev.native(name, value.Null(), []runtime.Thunk{constThunk{l}, constThunk{r}}, emit)
			})
		})
	}
	return vev.eval(n.R, env, in, nil, func(r *value.Value, _ value.Path) error {
		if err := ev.ctx.CheckNumbers(r); err != nil {
			return err
		}
		return vev.eval(n.L, env, in, nil, func(l *value.Value, _ value.Path) error {
			if err := ev.ctx.CheckNumbers(l); err != nil {
				return err
			}
			b, err := ev.compare(n.Op, l, r)
			if err != nil {
				return err
			}
			return emit(value.Bool(b), nil)
		})
	})
}

func (ev evaluator) compare(op string, l, r *value.Value) (bool, error) {
	switch op {
	case "==":
		return ev.ctx.Equal(l, r), nil
	case "!=":
		return !ev.ctx.Equal(l, r), nil
	case "<":
		return ev.ctx.Compare(l, r) < 0, nil
	case "<=":
		return ev.ctx.Compare(l, r) <= 0, nil
	case ">":
		return ev.ctx.Compare(l, r) > 0, nil
	case ">=":
		return ev.ctx.Compare(l, r) >= 0, nil
	default:
		return false, RuntimeError("unknown operator " + op)
	}
}

// native calls the registered native name with in and args, and its
// outputs are values rather than paths.
func (ev evaluator) native(name string, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
	fn, ok := ev.reg.Lookup(name, len(args))
	if !ok {
		return notDefined(name, len(args))
	}
	return fn(ev.ctx, in, args, func(v *value.Value, _ value.Path) error { return emit(v, nil) })
}

// first returns the first output of the native name, or nil when it has
// none.
func (ev evaluator) first(name string, in *value.Value, args []runtime.Thunk) (*value.Value, error) {
	var out *value.Value
	stop := errors.New("stop")
	err := ev.native(name, in, args, func(v *value.Value, _ value.Path) error {
		out = v
		return stop
	})
	if err != nil && !errors.Is(err, stop) {
		return nil, err
	}
	return out, nil
}

// constThunk is an argument that emits one fixed value.
type constThunk struct{ v *value.Value }

// Each emits the value.
func (c constThunk) Each(_ *value.Value, emit runtime.Emit) error { return emit(c.v, nil) }

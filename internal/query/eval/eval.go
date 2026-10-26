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
	// track is set when the run tracks cursors, so values carry their
	// paths outside path mode too.
	track bool
}

// evaluator walks the tree in one mode. With paths set it tracks the path
// of every output alongside the value, and mode is the index mode it hands
// the dialect.
type evaluator struct {
	*state
	paths bool
	mode  runtime.IndexMode
}

// Program is a parsed program ready to run any number of times. Env is
// the environment it runs in, which holds the prelude and any variables
// bound when it was compiled. The self calls in tail position of the
// program and of the registry's prelude are found once here, not on every
// run.
type Program struct {
	Node  ast.Node
	Env   *Env
	tails map[*ast.Call]tailSite
}

// NewProgram returns node ready to run in env, whose prelude is reg's. A
// nil reg has no prelude.
func NewProgram(node ast.Node, env *Env, reg *runtime.Registry) *Program {
	progs := []ast.Node{node}
	if reg != nil {
		progs = append(progs, reg.Prelude()...)
	}
	return &Program{Node: node, Env: env, tails: tailSites(progs...)}
}

// Run is the package Run for the compiled program. env is usually p.Env
// with the caller's variables bound on top.
func (p *Program) Run(ctx *runtime.Ctx, env *Env, roots []*value.Value, emit runtime.Emit) error {
	return run(newEvaluator(ctx, roots, p.tails, false), p.Node, env, emit)
}

// RunPaths is the package RunPaths for the compiled program.
func (p *Program) RunPaths(ctx *runtime.Ctx, env *Env, roots []*value.Value, emit runtime.Emit) error {
	return runPaths(newEvaluator(ctx, roots, p.tails, true), p.Node, env, emit)
}

// Run evaluates prog against roots and passes every output to emit. A
// single root is the input. More than one root forms a candidate set, and
// each node that reads its input runs once for every root.
func Run(ctx *runtime.Ctx, prog ast.Node, env *Env, roots []*value.Value, emit runtime.Emit) error {
	return run(newEvaluator(ctx, roots, programTails(ctx, prog), false), prog, env, emit)
}

// RunPaths is Run in path mode. It passes emit each output with its path,
// and it fails when an output is not a path into the input.
func RunPaths(ctx *runtime.Ctx, prog ast.Node, env *Env, roots []*value.Value, emit runtime.Emit) error {
	return runPaths(newEvaluator(ctx, roots, programTails(ctx, prog), true), prog, env, emit)
}

func run(ev evaluator, prog ast.Node, env *Env, emit runtime.Emit) error {
	var root value.Path
	if ev.track && len(ev.roots) == 1 {
		root = runtime.Rooted(ev.roots[0], 0, nil)
	}
	return unwrapDownstream(ev.eval(prog, env, ev.input(), root, emit))
}

func runPaths(ev evaluator, prog ast.Node, env *Env, emit runtime.Emit) error {
	var root value.Path
	if len(ev.roots) == 1 {
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

// programTails finds the tail sites of prog and of the dialect's prelude
// for a run that was not compiled into a Program.
func programTails(ctx *runtime.Ctx, prog ast.Node) map[*ast.Call]tailSite {
	var reg *runtime.Registry
	if ctx.Dialect != nil {
		reg = ctx.Dialect.Builtins()
	}
	return NewProgram(prog, nil, reg).tails
}

func newEvaluator(ctx *runtime.Ctx, roots []*value.Value, tails map[*ast.Call]tailSite, paths bool) evaluator {
	st := &state{
		ctx:   ctx,
		roots: roots,
		lits:  map[*ast.Literal]*value.Value{},
		reg:   &runtime.Registry{},
		tails: tails,
		used:  map[callKey]bool{},
		track: ctx.Cursors,
	}
	if ctx.Dialect != nil {
		st.reg = ctx.Dialect.Builtins()
	}
	if len(roots) == 1 {
		ctx.Cursor = &runtime.Cursor{Root: roots[0]}
	}
	return evaluator{state: st, paths: paths, mode: runtime.IndexRead}
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
// rather than paths. The right side of an assignment that reads without
// creating keeps doing so all the way down.
func (ev evaluator) values() evaluator {
	mode := runtime.IndexRead
	if ev.mode == runtime.IndexSource {
		mode = ev.mode
	}
	return evaluator{state: ev.state, mode: mode}
}

// derives reports whether values carry the places they came from, which
// is when the run tracks cursors outside path mode.
func (ev evaluator) derives() bool { return ev.track && !ev.paths }

// pathsFor returns the evaluator that collects the paths an assignment
// changes.
func (ev evaluator) pathsFor(mode runtime.IndexMode) evaluator {
	return evaluator{state: ev.state, paths: true, mode: mode}
}

// each runs f on in, or on every root in turn when in is the candidate
// set. Tracking cursors, a root's path carries the root and its document.
func (ev evaluator) each(in *value.Value, p value.Path, f func(in *value.Value, p value.Path) error) error {
	if in != nil {
		return f(in, p)
	}
	for i, r := range ev.roots {
		ev.ctx.Cursor = &runtime.Cursor{Root: r, Doc: i}
		var rp value.Path
		switch {
		case ev.paths:
			rp = value.Path{}
		case ev.track:
			rp = runtime.Rooted(r, i, nil)
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
	if in == nil && len(ev.roots) > 1 && perCandidate(n) {
		return ev.each(in, p, func(in *value.Value, p value.Path) error {
			return ev.eval(n, env, in, p, emit)
		})
	}
	switch n := n.(type) {
	case *ast.Identity:
		return ev.each(in, p, emit)
	case *ast.RecurseDefault:
		return ev.each(in, p, func(in *value.Value, p value.Path) error { return ev.recurse(in, p, n.Keys, emit) })
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
		return ev.array(n, env, in, p, emit)
	case *ast.ObjectCons:
		return ev.object(n, env, in, p, emit)
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
		return ev.binary(n, env, in, p, emit)
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
		v, at, err := ev.variable(n.Name, env)
		if err != nil || v == nil {
			return err
		}
		if !ev.derives() {
			at = nil
		}
		return emit(v, at)
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
		// A literal such as .1 or 1. is valid jq but not valid JSON.
		text := string(lit)
		if norm, ok := ast.NormalizeNumber(text); ok {
			text = norm
		}
		if d, err := decimal.Parse(text); err == nil {
			v = value.NumLit(d, text)
		} else {
			v = value.NumText(text)
		}
	default:
		v = value.Null()
	}
	ev.lits[n] = v
	return v
}

// variable returns the value bound to $name and the path it was bound
// at. $ENV is read lazily and only when the environment capability is on.
// Under Lax a variable that is not bound, $ENV among them, has no value.
func (ev evaluator) variable(name string, env *Env) (*value.Value, value.Path, error) {
	if v, at, ok := env.LookupAt(name); ok {
		return v, at, nil
	}
	if ev.ctx.Lax {
		return nil, nil, nil
	}
	if name != "ENV" {
		return nil, nil, RuntimeError("$" + name + " is not defined")
	}
	if err := ev.ctx.Caps.RequireEnv(); err != nil {
		return nil, nil, err
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
	return ev.envVal, nil, nil
}

// perCandidate reports whether n, given the candidate set, runs once for
// every candidate rather than once over all of them. These are the forms
// that build a value of their own: a literal, a string, an if, a
// negation, $__loc__, and an object with entries. A form that passes its
// input on, such as a pipe, a comma, or [...], leaves the set to its
// parts, a variable stands for its one value, and an empty object is
// built once.
func perCandidate(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.Literal, *ast.Str, *ast.If, *ast.Unary, *ast.Loc:
		return true
	case *ast.ObjectCons:
		return len(n.Entries) > 0
	default:
	}
	return false
}

// str builds an interpolated string. The last interpolation varies
// slowest, as in jq, and a format renders each interpolated value through
// its native.
func (ev evaluator) str(n *ast.Str, env *Env, in *value.Value, emit runtime.Emit) error {
	if n.Format != "" {
		if _, err := ev.reg.Format(n.Format); err != nil {
			return err
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
				pieces[i] = text(v, ev.ctx.Numbers)
				return build(i - 1)
			}
			return ev.format(n.Format, v, func(s *value.Value, _ value.Path) error {
				pieces[i] = text(s, ev.ctx.Numbers)
				return build(i - 1)
			})
		})
	}
	return build(len(n.Parts) - 1)
}

// text is a string's own text and any other value's JSON.
func text(v *value.Value, numbers runtime.NumberMode) string {
	if v.Kind() == value.KindString {
		return v.Str()
	}
	return runtime.JSONIn(v, numbers)
}

// format applies the format native called name, such as "@base64", to in.
func (ev evaluator) format(name string, in *value.Value, emit runtime.Emit) error {
	fn, err := ev.reg.Format(name)
	if err != nil {
		return err
	}
	return fn(ev.ctx, in, nil, func(v *value.Value, _ value.Path) error { return emit(v, nil) })
}

// array collects every output of the body into one array. Given the
// candidate set, the body runs on each candidate in turn and the array
// gathers them all. Tracking cursors, the array takes the place of an
// input that is not a root.
func (ev evaluator) array(n *ast.ArrayCons, env *Env, in *value.Value, p value.Path, emit runtime.Emit) error {
	var elems []*value.Value
	if n.Body != nil {
		vev := ev.values()
		err := ev.each(in, p, func(in *value.Value, p value.Path) error {
			if !ev.derives() {
				p = nil
			}
			return vev.eval(n.Body, env, in, p, func(v *value.Value, _ value.Path) error {
				elems = append(elems, v)
				return nil
			})
		})
		if err != nil {
			return err
		}
	}
	var ap value.Path
	if ev.derives() && in != nil {
		if _, _, rel := runtime.Unroot(p); len(rel) > 0 {
			ap = p
		}
	}
	return emit(value.Arr(elems...), ap)
}

// object builds one object for every combination of its entries' outputs.
// The first entry varies slowest, and within an entry the key varies
// slower than the value. Tracking cursors, the object is a root of its
// own in its input's document.
func (ev evaluator) object(n *ast.ObjectCons, env *Env, in *value.Value, p value.Path, emit runtime.Emit) error {
	vev := ev.values()
	var vp value.Path
	if ev.derives() {
		vp = p
	}
	keys := make([]string, len(n.Entries))
	vals := make([]*value.Value, len(n.Entries))
	var build func(i int) error
	build = func(i int) error {
		if i == len(n.Entries) {
			b := value.NewObjBuilder(len(keys))
			for j, k := range keys {
				b.Set(k, vals[j])
			}
			out := b.Seal()
			if !ev.derives() {
				return emit(out, nil)
			}
			_, doc, _ := runtime.Unroot(p)
			return emit(out, runtime.Rooted(out, doc, nil))
		}
		e := n.Entries[i]
		return vev.eval(e.Key, env, in, vp, func(k *value.Value, _ value.Path) error {
			if k.Kind() != value.KindString {
				return RuntimeError("Cannot use " + typeName(k) + " (" + dump(k) + ") as object key")
			}
			return vev.eval(e.Val, env, in, vp, func(v *value.Value, _ value.Path) error {
				keys[i], vals[i] = k.Str(), v
				return build(i + 1)
			})
		})
	}
	return build(0)
}

// typeName is the name jq gives a value's type in error messages.
func typeName(v *value.Value) string { return runtime.TypeName(v) }

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
// outermost, as jq does, or the left one under LeftFirst, as yq does. The
// boolean operators run the left one outermost and skip the right one
// when the left decides.
func (ev evaluator) binary(n *ast.Binary, env *Env, in *value.Value, p value.Path, emit runtime.Emit) error {
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
	// Tracking cursors, the result takes the left operand's place.
	var lp value.Path
	if ev.derives() {
		lp = p
	}
	if name, ok := arithmetic[n.Op]; ok {
		return ev.operands(n, env, in, lp, func(l *value.Value, p value.Path, r *value.Value) error {
			return ev.native(name, value.Null(), []runtime.Thunk{constThunk{l}, constThunk{r}}, ev.derive(l, p, emit))
		})
	}
	return ev.operands(n, env, in, lp, func(l *value.Value, p value.Path, r *value.Value) error {
		if err := ev.ctx.CheckNumbers(r); err != nil {
			return err
		}
		if err := ev.ctx.CheckNumbers(l); err != nil {
			return err
		}
		b, err := ev.compare(n.Op, l, r)
		if err != nil {
			return err
		}
		out := value.Bool(b)
		if ev.derives() {
			return emit(out, derived(l, p, out))
		}
		return emit(out, nil)
	})
}

// operands calls f on every pair of the outputs of an operator's two
// sides, with the left output's path, in the order binary describes.
func (ev evaluator) operands(n *ast.Binary, env *Env, in *value.Value, lp value.Path, f func(l *value.Value, p value.Path, r *value.Value) error) error {
	vev := ev.values()
	if ev.ctx.LeftFirst {
		return vev.eval(n.L, env, in, lp, func(l *value.Value, p value.Path) error {
			return vev.eval(n.R, env, in, nil, func(r *value.Value, _ value.Path) error { return f(l, p, r) })
		})
	}
	return vev.eval(n.R, env, in, nil, func(r *value.Value, _ value.Path) error {
		return vev.eval(n.L, env, in, lp, func(l *value.Value, p value.Path) error { return f(l, p, r) })
	})
}

// derive passes on each output of a native that came from in, whose path
// is p. An output with no path of its own takes the place derived gives
// it. Outside cursor tracking emit comes back as it is.
func (ev evaluator) derive(in *value.Value, p value.Path, emit runtime.Emit) runtime.Emit {
	if !ev.derives() || p == nil {
		return emit
	}
	return func(v *value.Value, vp value.Path) error {
		if vp == nil {
			vp = derived(in, p, v)
		}
		return emit(v, vp)
	}
}

// derived is the place of out, a value computed from in at path p. Like
// yq's replacement nodes, it takes the place of an input below a root.
// One computed from a root is a root of its own in the same document when
// it is the same kind of node, and has no place otherwise.
func derived(in *value.Value, p value.Path, out *value.Value) value.Path {
	if p == nil {
		return nil
	}
	_, doc, rel := runtime.Unroot(p)
	if len(rel) > 0 {
		return p
	}
	if shape(in) != shape(out) {
		return nil
	}
	return runtime.Rooted(out, doc, nil)
}

// shape sorts a value into a map, a sequence, or a scalar.
func shape(v *value.Value) value.Kind {
	switch k := v.Kind(); k {
	case value.KindObject, value.KindArray:
		return k
	default:
	}
	return value.KindNull
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

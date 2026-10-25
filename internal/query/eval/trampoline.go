package eval

import (
	"strings"

	"github.com/fivetwenty-io/graft/internal/query/ast"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// tailCall is the signal a self call in tail position returns to the
// runBody that runs its definition. It carries the next round's frame.
type tailCall struct {
	fn    *Closure
	frame *Env
	in    *value.Value
	p     value.Path
}

func (*tailCall) Error() string { return "tail call" }

// runBody runs fn's body, and runs it again with the new frame each time
// the body ends in a tail call of its own, so a tail-recursive definition
// loops in constant stack. A tail call of another definition passes
// through to the runBody that runs that one.
func (ev evaluator) runBody(fn *Closure, frame *Env, in *value.Value, p value.Path, emit func(*value.Value, value.Path) error) error {
	for {
		err := ev.eval(fn.Body, frame, in, p, emit)
		tc, ok := err.(*tailCall) //nolint:errorlint // the signal is never wrapped
		if !ok || tc.fn.Body != fn.Body {
			return err
		}
		fn, frame, in, p = tc.fn, tc.frame, tc.in, tc.p
	}
}

// tailFrame builds the frame for a tail call. It reports ok when the call
// is settled, either by a frame or by an argument that has no output or
// fails. It does not settle the call when an argument has more than one
// output, which the analysis rules out, and the caller then makes an
// ordinary call.
func (ev evaluator) tailFrame(n *ast.Call, fn *Closure, env *Env, in *value.Value, p value.Path) (*tailCall, bool, error) {
	var frames []*Env
	err := ev.frames(n, fn, env, in, func(frame *Env) error {
		frames = append(frames, frame)
		return nil
	})
	switch {
	case err != nil:
		return nil, true, err
	case len(frames) == 0:
		return nil, true, nil
	case len(frames) > 1:
		return nil, false, nil
	}
	return &tailCall{fn: fn, frame: frames[0], in: in, p: p}, true, nil
}

// tailSite is a self call in tail position. Body is the body of the
// definition it calls. Guards are the expressions on its way there whose
// one output depends on calls, such as the update in jq's until, and each
// must have one output when the call runs for the call to be a tail call.
type tailSite struct {
	body   ast.Node
	guards []ast.Node
}

// tailSites finds the self calls in tail position of every definition in
// prog. A call is in tail position when its outputs are its definition's
// outputs and nothing runs after it: the right side of a pipe whose left
// side has one output, the right side of a comma, an if's branches when
// its conditions have one output, a single-variable bind's body, and what
// follows a nested definition that does not shadow the name. Every value
// argument must have one output. A left side, condition, or bind source
// whose one output depends on what its calls do becomes a guard.
func tailSites(progs ...ast.Node) map[*ast.Call]tailSite {
	sites := map[*ast.Call]tailSite{}
	for _, prog := range progs {
		walk(prog, func(n ast.Node) {
			if def, ok := n.(*ast.FuncDef); ok {
				markTails(def.Body, def, nil, sites)
			}
		})
	}
	return sites
}

// markTails records the self calls of def that sit in tail position
// under n, behind guards.
//
//nolint:gocyclo // switch handles every tail position
func markTails(n ast.Node, def *ast.FuncDef, guards []ast.Node, sites map[*ast.Call]tailSite) {
	switch n := n.(type) {
	case *ast.Call:
		if n.Name != def.Name || len(n.Args) != len(def.Params) {
			return
		}
		for i, param := range def.Params {
			if strings.HasPrefix(param, "$") && !singleOutput(n.Args[i]) {
				return
			}
		}
		sites[n] = tailSite{body: def.Body, guards: guards}
	case *ast.Pipe:
		if guards, ok := guard(guards, n.L); ok {
			markTails(n.R, def, guards, sites)
		}
	case *ast.Comma:
		markTails(n.R, def, guards, sites)
	case *ast.If:
		guards, ok := guard(guards, n.Cond)
		for _, e := range n.Elifs {
			if !ok {
				return
			}
			guards, ok = guard(guards, e.Cond)
		}
		if !ok {
			return
		}
		markTails(n.Then, def, guards, sites)
		for _, e := range n.Elifs {
			markTails(e.Then, def, guards, sites)
		}
		if n.Else != nil {
			markTails(n.Else, def, guards, sites)
		}
	case *ast.Bind:
		if len(n.Pats) != 1 || n.Pats[0].Kind != ast.PatVar {
			return
		}
		if guards, ok := guard(guards, n.Src); ok {
			markTails(n.Body, def, guards, sites)
		}
	case *ast.FuncDef:
		if n.Rest != nil && (n.Name != def.Name || len(n.Params) != len(def.Params)) && !shadows(n, guards) {
			markTails(n.Rest, def, guards, sites)
		}
	}
}

// guard adds n to guards when its one output depends on its calls. It
// reports false when n can have more than one output whatever its calls
// do.
func guard(guards []ast.Node, n ast.Node) ([]ast.Node, bool) {
	switch {
	case singleOutput(n):
		return guards, true
	case oneOutput(n, func(*ast.Call) bool { return true }):
		return append(guards[:len(guards):len(guards)], n), true
	default:
		return guards, false
	}
}

// shadows reports whether def redefines a function a guard calls, which
// would make the guard mean something else where the call runs.
func shadows(def *ast.FuncDef, guards []ast.Node) bool {
	found := false
	for _, g := range guards {
		walk(g, func(n ast.Node) {
			if c, ok := n.(*ast.Call); ok && c.Name == def.Name && len(c.Args) == len(def.Params) {
				found = true
			}
		})
	}
	return found
}

// guardBudget is how many calls singleAt looks through before it gives up.
const guardBudget = 64

// guarded reports whether every guard has one output in env.
func (ev evaluator) guarded(guards []ast.Node, env *Env) bool {
	budget := guardBudget
	return ev.allSingleAt(guards, env, &budget)
}

// singleAt reports whether n has one output when it runs in env. It looks
// through each call to the filter argument or definition env binds, with
// a definition's parameters bound to the call's arguments, and trusts a
// native added with RegisterSingle whose arguments have one output each.
// Any other native, a recursive definition, or a chain of calls longer
// than the budget does not count.
func (ev evaluator) singleAt(n ast.Node, env *Env, budget *int) bool {
	return oneOutput(n, func(c *ast.Call) bool {
		if *budget <= 0 {
			return false
		}
		*budget--
		fn, ok := env.lookupFunc(c.Name, len(c.Args))
		if !ok {
			return ev.reg.Single(c.Name, len(c.Args)) && ev.allSingleAt(c.Args, env, budget)
		}
		if fn.param {
			return ev.singleAt(fn.Body, fn.Env, budget)
		}
		frame := fn.Env
		for i, name := range fn.Params {
			if strings.HasPrefix(name, "$") {
				if !ev.singleAt(c.Args[i], env, budget) {
					return false
				}
				name = name[1:]
			}
			frame = frame.defAs(name, 0, ev.argClosure(c.Args[i], env))
		}
		return ev.singleAt(fn.Body, frame, budget)
	})
}

// allSingleAt reports whether every node in ns has one output in env.
func (ev evaluator) allSingleAt(ns []ast.Node, env *Env, budget *int) bool {
	for _, n := range ns {
		if !ev.singleAt(n, env, budget) {
			return false
		}
	}
	return true
}

// singleOutput reports whether n has at most one output and calls nothing
// that could run code of its own between that output and its return.
func singleOutput(n ast.Node) bool {
	return oneOutput(n, func(*ast.Call) bool { return false })
}

// oneOutput is singleOutput with call deciding each call.
//
//nolint:gocyclo // switch handles all expression types
func oneOutput(n ast.Node, call func(*ast.Call) bool) bool {
	one := func(n ast.Node) bool { return oneOutput(n, call) }
	switch n := n.(type) {
	case nil:
		return true
	case *ast.Identity, *ast.Literal, *ast.Var, *ast.Loc, *ast.ArrayCons:
		return true
	case *ast.Call:
		return call(n)
	case *ast.Field:
		return one(n.Target)
	case *ast.Index:
		return one(n.Target) && one(n.Key)
	case *ast.Slice:
		return one(n.Target) && one(n.From) && one(n.To)
	case *ast.Binary:
		return one(n.L) && one(n.R)
	case *ast.Unary:
		return one(n.X)
	case *ast.Pipe:
		return one(n.L) && one(n.R)
	case *ast.If:
		if !one(n.Cond) || !one(n.Then) || !one(n.Else) {
			return false
		}
		for _, e := range n.Elifs {
			if !one(e.Cond) || !one(e.Then) {
				return false
			}
		}
		return true
	case *ast.ObjectCons:
		for _, e := range n.Entries {
			if !one(e.Key) || !one(e.Val) {
				return false
			}
		}
		return true
	case *ast.Str:
		if n.Format != "" {
			return false
		}
		for _, part := range n.Parts {
			if !one(part) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// walk calls f on n and every node under it.
func walk(n ast.Node, f func(ast.Node)) {
	if n == nil {
		return
	}
	f(n)
	for _, c := range children(n) {
		walk(c, f)
	}
}

// children lists the nodes directly under n, nils included.
//
//nolint:gocyclo // switch handles all expression types
func children(n ast.Node) []ast.Node {
	switch n := n.(type) {
	case *ast.Field:
		return []ast.Node{n.Target}
	case *ast.Index:
		return []ast.Node{n.Target, n.Key}
	case *ast.Slice:
		return []ast.Node{n.Target, n.From, n.To}
	case *ast.Iterate:
		return []ast.Node{n.Target}
	case *ast.Str:
		return n.Parts
	case *ast.ArrayCons:
		return []ast.Node{n.Body}
	case *ast.ObjectCons:
		out := make([]ast.Node, 0, 2*len(n.Entries))
		for _, e := range n.Entries {
			out = append(out, e.Key, e.Val)
		}
		return out
	case *ast.Pipe:
		return []ast.Node{n.L, n.R}
	case *ast.Comma:
		return []ast.Node{n.L, n.R}
	case *ast.Binary:
		return []ast.Node{n.L, n.R}
	case *ast.Alt:
		return []ast.Node{n.L, n.R}
	case *ast.Assign:
		return []ast.Node{n.L, n.R}
	case *ast.Unary:
		return []ast.Node{n.X}
	case *ast.Optional:
		return []ast.Node{n.X}
	case *ast.If:
		out := []ast.Node{n.Cond, n.Then, n.Else}
		for _, e := range n.Elifs {
			out = append(out, e.Cond, e.Then)
		}
		return out
	case *ast.Try:
		return []ast.Node{n.Body, n.Catch}
	case *ast.Reduce:
		return patternKeys(n.Pats, []ast.Node{n.Src, n.Init, n.Update})
	case *ast.Foreach:
		return patternKeys(n.Pats, []ast.Node{n.Src, n.Init, n.Update, n.Extract})
	case *ast.FuncDef:
		return []ast.Node{n.Body, n.Rest}
	case *ast.Call:
		return n.Args
	case *ast.Bind:
		return patternKeys(n.Pats, []ast.Node{n.Src, n.Body})
	case *ast.Label:
		return []ast.Node{n.Body}
	case *ast.Module:
		return []ast.Node{n.Meta, n.Rest}
	case *ast.Import:
		return []ast.Node{n.Meta, n.Rest}
	case *ast.Include:
		return []ast.Node{n.Meta, n.Rest}
	default:
		return nil
	}
}

// patternKeys appends the key expressions of pats to out.
func patternKeys(pats []ast.Pattern, out []ast.Node) []ast.Node {
	for i := range pats {
		pat := &pats[i]
		out = patternKeys(pat.Elems, out)
		for _, e := range pat.Entries {
			if e.Key != nil {
				out = append(out, e.Key)
			}
			if e.Val != nil {
				out = patternKeys([]ast.Pattern{*e.Val}, out)
			}
		}
	}
	return out
}

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
// the body ends in a tail call, so a tail-recursive definition loops in
// constant stack.
func (ev evaluator) runBody(fn *Closure, frame *Env, in *value.Value, p value.Path, emit func(*value.Value, value.Path) error) error {
	for {
		err := ev.eval(fn.Body, frame, in, p, emit)
		tc, ok := err.(*tailCall) //nolint:errorlint // the signal is never wrapped
		if !ok {
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

// tailSites finds the self calls in tail position of every definition in
// prog and maps each to its definition's body. A call is in tail position
// when its outputs are its definition's outputs and nothing runs after it:
// the right side of a pipe whose left side has one output, the right side
// of a comma, an if's branches when its conditions have one output, a
// single-variable bind's body, and what follows a nested definition that
// does not shadow the name. Every value argument must have one output.
func tailSites(progs ...ast.Node) map[*ast.Call]ast.Node {
	sites := map[*ast.Call]ast.Node{}
	for _, prog := range progs {
		walk(prog, func(n ast.Node) {
			if def, ok := n.(*ast.FuncDef); ok {
				markTails(def.Body, def, sites)
			}
		})
	}
	return sites
}

// markTails records the self calls of def that sit in tail position
// under n.
//
//nolint:gocyclo // switch handles every tail position
func markTails(n ast.Node, def *ast.FuncDef, sites map[*ast.Call]ast.Node) {
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
		sites[n] = def.Body
	case *ast.Pipe:
		if singleOutput(n.L) {
			markTails(n.R, def, sites)
		}
	case *ast.Comma:
		markTails(n.R, def, sites)
	case *ast.If:
		if !singleOutput(n.Cond) {
			return
		}
		for _, e := range n.Elifs {
			if !singleOutput(e.Cond) {
				return
			}
		}
		markTails(n.Then, def, sites)
		for _, e := range n.Elifs {
			markTails(e.Then, def, sites)
		}
		if n.Else != nil {
			markTails(n.Else, def, sites)
		}
	case *ast.Bind:
		if len(n.Pats) == 1 && n.Pats[0].Kind == ast.PatVar && singleOutput(n.Src) {
			markTails(n.Body, def, sites)
		}
	case *ast.FuncDef:
		if n.Rest != nil && (n.Name != def.Name || len(n.Params) != len(def.Params)) {
			markTails(n.Rest, def, sites)
		}
	}
}

// singleOutput reports whether n has at most one output and calls nothing
// that could run code of its own between that output and its return.
//
//nolint:gocyclo // switch handles all expression types
func singleOutput(n ast.Node) bool {
	switch n := n.(type) {
	case nil:
		return true
	case *ast.Identity, *ast.Literal, *ast.Var, *ast.Loc, *ast.ArrayCons:
		return true
	case *ast.Field:
		return singleOutput(n.Target)
	case *ast.Index:
		return singleOutput(n.Target) && singleOutput(n.Key)
	case *ast.Slice:
		return singleOutput(n.Target) && singleOutput(n.From) && singleOutput(n.To)
	case *ast.Binary:
		return singleOutput(n.L) && singleOutput(n.R)
	case *ast.Unary:
		return singleOutput(n.X)
	case *ast.Pipe:
		return singleOutput(n.L) && singleOutput(n.R)
	case *ast.ObjectCons:
		for _, e := range n.Entries {
			if !singleOutput(e.Key) || !singleOutput(e.Val) {
				return false
			}
		}
		return true
	case *ast.Str:
		if n.Format != "" {
			return false
		}
		for _, part := range n.Parts {
			if !singleOutput(part) {
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

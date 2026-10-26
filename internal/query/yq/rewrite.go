package yq

import (
	"strconv"
	"strings"

	"github.com/fivetwenty-io/graft/internal/query/ast"
)

// rewrite adjusts a parsed expression for the yq operators whose argument
// yq reads as text: env and strenv take the variable name as written,
// envsubst takes its flags, and with must have two arguments. It also
// makes an interpolated value read as tostring gives it rather than as
// JSON.
func rewrite(expr string, n ast.Node) (ast.Node, error) {
	var err error
	walk(n, func(n ast.Node) {
		switch n := n.(type) {
		case *ast.Call:
			if e := rewriteCall(expr, n); e != nil && err == nil {
				err = e
			}
		case *ast.Str:
			if n.Format != "" {
				return
			}
			for i, p := range n.Parts {
				if lit, ok := p.(*ast.Literal); ok {
					if _, ok := lit.Value.(string); ok {
						continue
					}
				}
				n.Parts[i] = &ast.Pipe{L: p, R: &ast.Call{Name: "tostring"}}
			}
		default:
		}
	})
	if err != nil {
		return nil, err
	}
	return n, nil
}

// rewriteCall adjusts a call to env, strenv, envsubst, or with.
func rewriteCall(expr string, n *ast.Call) error {
	switch {
	case (n.Name == "env" || n.Name == "strenv") && len(n.Args) == 1:
		n.Args[0] = &ast.Literal{Value: ast.Print(n.Args[0])}
	case n.Name == "envsubst" && len(n.Args) == 1:
		flags := strings.Trim(ast.Print(n.Args[0]), "()")
		n.Args[0] = &ast.Literal{Value: flags}
		return checkFlags(expr, flags)
	case n.Name == "with" && len(n.Args) == 1:
		msg := "with must be given a block (;), got " + opName(n.Args[0]) + " instead"
		n.Name, n.Args = "error", []ast.Node{&ast.Literal{Value: msg}}
	default:
	}
	return nil
}

// checkFlags fails envsubst flags other than ne, nu, and ff with yq's
// lexer message, which quotes the expression from the first bad flag on.
func checkFlags(expr, flags string) error {
	for _, f := range strings.FieldsFunc(flags, func(r rune) bool { return r == ',' || r == ' ' }) {
		switch f {
		case "ne", "nu", "ff":
			continue
		default:
		}
		at := strings.Index(expr, "envsubst(")
		if at < 0 {
			at = 0
		} else {
			at += len("envsubst(")
			at += len(expr[at:]) - len(strings.TrimLeft(expr[at:], " "))
		}
		return &ParseError{Msg: "1:" + strconv.Itoa(at+1) + ": lexer: invalid input text " + strconv.Quote(expr[at:]), Pos: at}
	}
	return nil
}

// opName is the name yq gives the operation at the top of n.
func opName(n ast.Node) string {
	switch n.(type) {
	case *ast.Comma:
		return "UNION"
	case *ast.Pipe:
		return "PIPE"
	case *ast.Field, *ast.Index, *ast.Iterate, *ast.Identity:
		return "TRAVERSE_PATH"
	default:
	}
	return "an expression"
}

// walk calls f on n and on every node below it, parents first.
//
//nolint:gocyclo // one case for each kind of node that has children
func walk(n ast.Node, f func(ast.Node)) {
	if n == nil {
		return
	}
	f(n)
	switch n := n.(type) {
	case *ast.Field:
		walk(n.Target, f)
	case *ast.Index:
		walk(n.Target, f)
		walk(n.Key, f)
	case *ast.Slice:
		walk(n.Target, f)
		walk(n.From, f)
		walk(n.To, f)
	case *ast.Iterate:
		walk(n.Target, f)
	case *ast.Str:
		for _, p := range n.Parts {
			walk(p, f)
		}
	case *ast.ArrayCons:
		walk(n.Body, f)
	case *ast.ObjectCons:
		for _, e := range n.Entries {
			walk(e.Key, f)
			walk(e.Val, f)
		}
	case *ast.Pipe:
		walk(n.L, f)
		walk(n.R, f)
	case *ast.Comma:
		walk(n.L, f)
		walk(n.R, f)
	case *ast.Binary:
		walk(n.L, f)
		walk(n.R, f)
	case *ast.Unary:
		walk(n.X, f)
	case *ast.Alt:
		walk(n.L, f)
		walk(n.R, f)
	case *ast.Assign:
		walk(n.L, f)
		walk(n.R, f)
	case *ast.If:
		walk(n.Cond, f)
		walk(n.Then, f)
		for _, e := range n.Elifs {
			walk(e.Cond, f)
			walk(e.Then, f)
		}
		walk(n.Else, f)
	case *ast.Try:
		walk(n.Body, f)
		walk(n.Catch, f)
	case *ast.Reduce:
		walk(n.Src, f)
		walk(n.Init, f)
		walk(n.Update, f)
	case *ast.Foreach:
		walk(n.Src, f)
		walk(n.Init, f)
		walk(n.Update, f)
		walk(n.Extract, f)
	case *ast.FuncDef:
		walk(n.Body, f)
		walk(n.Rest, f)
	case *ast.Call:
		for _, a := range n.Args {
			walk(a, f)
		}
	case *ast.Bind:
		walk(n.Src, f)
		walk(n.Body, f)
	case *ast.Label:
		walk(n.Body, f)
	case *ast.Optional:
		walk(n.X, f)
	case *ast.Module:
		walk(n.Rest, f)
	default:
	}
}

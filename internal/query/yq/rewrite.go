package yq

import (
	"strconv"
	"strings"

	"github.com/fivetwenty-io/graft/internal/query/ast"
	"github.com/fivetwenty-io/graft/internal/query/lexer"
)

// rewrite adjusts a parsed expression for the yq operators whose argument
// yq reads as text: env and strenv take the variable name as written,
// envsubst takes its flags, and with must have two arguments. It also
// makes an interpolated value read as tostring gives it rather than as
// JSON. A comma whose sides both give the list they were handed keeps
// only its left side, since yq's union gives that list once.
func rewrite(expr string, n ast.Node) (ast.Node, error) {
	var err error
	walk(n, func(n ast.Node) {
		if op, r, ok := infixOperands(n); ok && bareError(r) && err == nil {
			err = &ParseError{Msg: "'" + op + "' expects 2 args but there is 1"}
		}
		switch n := n.(type) {
		case *ast.Comma:
			if sameList(n.L, n.R) {
				n.R = &ast.Call{Name: "_yq_none"}
			}
		case *ast.Call:
			if e := rewriteCall(expr, n); e != nil && err == nil {
				err = e
			}
		case *ast.Str:
			stringify(n)
		default:
		}
	})
	if err != nil {
		return nil, err
	}
	return n, nil
}

// stringify makes each non-literal part of an interpolated string read
// as tostring gives it.
func stringify(n *ast.Str) {
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
}

// rewriteCall refuses error without an argument and env without one, and
// adjusts a call to env, strenv, envsubst, or with.
func rewriteCall(expr string, n *ast.Call) error {
	switch {
	case bareError(n):
		return &ParseError{Msg: errorArgMissing}
	case n.Name == "env" && len(n.Args) == 0:
		return bareEnv(expr)
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

// sameList reports whether l and r both give the very list a yq
// operator is handed: each is . (or a pipe of them), or both read one
// variable.
func sameList(l, r ast.Node) bool {
	if lv, ok := l.(*ast.Var); ok {
		rv, ok := r.(*ast.Var)
		return ok && lv.Name == rv.Name
	}
	return isSelf(l) && isSelf(r)
}

// isSelf reports whether n gives its input list unchanged.
func isSelf(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.Identity:
		return true
	case *ast.Pipe:
		return isSelf(n.L) && isSelf(n.R)
	default:
		return false
	}
}

// bareError reports whether n is error without an argument, which yq
// reads as an operator missing its operand.
func bareError(n ast.Node) bool {
	c, ok := n.(*ast.Call)
	return ok && c.Name == "error" && len(c.Args) == 0
}

// infixOperands gives the spelling and right operand of an infix
// operator.
func infixOperands(n ast.Node) (string, ast.Node, bool) {
	switch n := n.(type) {
	case *ast.Pipe:
		return "|", n.R, true
	case *ast.Comma:
		return ",", n.R, true
	case *ast.Alt:
		return "//", n.R, true
	case *ast.Binary:
		return n.Op, n.R, true
	case *ast.Assign:
		return n.Op, n.R, true
	default:
	}
	return "", nil, false
}

// bareEnv is yq's lexer error for env without an argument, which yq
// does not have, so that jq's whole-environment env is out of reach. yq
// reads an interpolation's text on its own, so the error inside one
// counts from the interpolation's start and quotes only its text.
func bareEnv(expr string) error {
	toks, err := lexer.Tokenize(expr, Grammar().Tokens)
	if err != nil {
		return &ParseError{Msg: "lexer: invalid input text " + strconv.Quote("env")}
	}
	var opens []int
	for i, tok := range toks {
		switch tok.Kind {
		case lexer.InterpStart:
			opens = append(opens, i)
			continue
		case lexer.InterpEnd:
			opens = opens[:len(opens)-1]
			continue
		default:
		}
		if tok.Kind != lexer.Ident || tok.Text != "env" || toks[i+1].Kind == lexer.LParen {
			continue
		}
		start, end := 0, len(expr)
		if len(opens) > 0 {
			open := opens[len(opens)-1]
			start, end = toks[open].End, interpEnd(toks, open)
		}
		at := tok.Pos - start
		return &ParseError{Msg: "1:" + strconv.Itoa(at+1) + ": lexer: invalid input text " + strconv.Quote(expr[tok.Pos:end]), Pos: tok.Pos}
	}
	return &ParseError{Msg: "lexer: invalid input text " + strconv.Quote("env")}
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

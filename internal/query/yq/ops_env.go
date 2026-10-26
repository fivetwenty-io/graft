package yq

import (
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/fivetwenty-io/graft/internal/query/ast"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// errEnvDisabled is yq's message when --security-disable-env-ops is set.
var errEnvDisabled = &query.RuntimeError{Msg: "env operations have been disabled"}

// registerEnv adds env, strenv, and envsubst. Each reads the process
// environment only while the dialect's capabilities allow it.
func registerEnv(r *runtime.Registry) {
	r.RegisterSingle("env", 1, func(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		return envName(ctx, in, args[0], func(name string) error {
			text, ok := os.LookupEnv(name)
			if !ok {
				return &query.RuntimeError{Msg: "value for env variable '" + name + "' not provided in env()"}
			}
			v, err := parseYAML(text, false)
			if err != nil {
				return err
			}
			return emit(v, nil)
		})
	})
	r.RegisterSingle("strenv", 1, func(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		return envName(ctx, in, args[0], func(name string) error {
			return emit(value.Str(os.Getenv(name)), nil)
		})
	})
	r.RegisterSingle("envsubst", 0, func(ctx *runtime.Ctx, in *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		return envsubst(ctx, in, "", emit)
	})
	r.RegisterSingle("envsubst", 1, func(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		return args[0].Each(in, func(flags *value.Value, _ value.Path) error {
			return envsubst(ctx, in, flags.Str(), emit)
		})
	})
}

// envName calls f with each variable name the argument gives, once the
// capabilities allow reading the environment.
func envName(ctx *runtime.Ctx, in *value.Value, arg runtime.Thunk, f func(string) error) error {
	if !ctx.Caps.Env {
		return errEnvDisabled
	}
	return arg.Each(in, func(name *value.Value, _ value.Path) error { return f(nodeText(name)) })
}

// envsubst replaces the variable references in a string, as yq's
// envsubst does, under flags ne (fail on an empty variable), nu (fail on
// an unset one), and ff (stop at the first failure).
func envsubst(ctx *runtime.Ctx, in *value.Value, flags string, emit runtime.Emit) error {
	if !ctx.Caps.Env {
		return errEnvDisabled
	}
	if in.Kind() != value.KindString {
		return &query.RuntimeError{Msg: "cannot substitute with " + tagOf(in) + ", can only substitute strings. " + substituteHint}
	}
	s := substituter{noEmpty: strings.Contains(flags, "ne"), noUnset: strings.Contains(flags, "nu"), failFast: strings.Contains(flags, "ff")}
	out, err := s.run(in.Str())
	if err != nil {
		return err
	}
	if len(s.errs) > 0 {
		return &query.RuntimeError{Msg: strings.Join(s.errs, "\n")}
	}
	return emit(value.Str(out), nil)
}

// errStop ends a substitution at its first failure under ff.
var errStop = errors.New("stop")

type substituter struct {
	noEmpty, noUnset, failFast bool
	errs                       []string
}

// fail records a failure, and under ff stops the substitution.
func (s *substituter) fail(msg string) error {
	s.errs = append(s.errs, msg)
	if s.failFast {
		return errStop
	}
	return nil
}

// run substitutes every reference in text.
func (s *substituter) run(text string) (string, error) {
	var b strings.Builder
	for {
		i := strings.IndexByte(text, '$')
		if i < 0 {
			b.WriteString(text)
			return b.String(), nil
		}
		b.WriteString(text[:i])
		text = text[i+1:]
		switch {
		case strings.HasPrefix(text, "$"):
			b.WriteByte('$')
			text = text[1:]
		case strings.HasPrefix(text, "{"):
			end := strings.IndexByte(text, '}')
			if end < 0 {
				return "", &query.RuntimeError{Msg: "closing brace expected"}
			}
			expr := text[1:end]
			text = text[end+1:]
			if expr == "" {
				b.WriteString("${}")
				continue
			}
			v, err := s.braced(expr)
			if errors.Is(err, errStop) {
				return "", nil
			}
			b.WriteString(v)
		default:
			n := nameLen(text)
			if n == 0 {
				b.WriteByte('$')
				continue
			}
			name := text[:n]
			text = text[n:]
			if name[0] >= '0' && name[0] <= '9' {
				continue
			}
			v, err := s.lookup(name)
			if errors.Is(err, errStop) {
				return "", nil
			}
			b.WriteString(v)
		}
	}
}

// braced substitutes the reference inside ${...}.
func (s *substituter) braced(expr string) (string, error) {
	if strings.HasPrefix(expr, "#") {
		return "", nil
	}
	n := nameLen(expr)
	name, mod := expr[:n], expr[n:]
	if name == "" {
		return "${" + expr + "}", nil
	}
	v, set := os.LookupEnv(name)
	for _, op := range []string{":-", ":=", ":+", "-", "=", "+"} {
		arg, ok := strings.CutPrefix(mod, op)
		if !ok {
			continue
		}
		def, err := s.nested(arg)
		if err != nil {
			return "", err
		}
		switch {
		case strings.HasSuffix(op, "+"):
			if set {
				return def, nil
			}
			return "", nil
		case strings.HasPrefix(op, ":") && v == "":
			return def, nil
		case !set:
			return def, nil
		default:
		}
		return s.check(name, v, set)
	}
	return s.check(name, v, set)
}

// nested substitutes a default, which may itself refer to variables. A
// default that does not substitute cleanly gives nothing.
func (s *substituter) nested(text string) (string, error) {
	inner := substituter{noEmpty: s.noEmpty, noUnset: s.noUnset, failFast: s.failFast}
	out, err := inner.run(text)
	if err != nil {
		out, inner.errs = "", nil
	}
	for _, msg := range inner.errs {
		if err := s.fail(msg); err != nil {
			return "", err
		}
	}
	return out, nil
}

// lookup is a plain reference's value.
func (s *substituter) lookup(name string) (string, error) {
	v, set := os.LookupEnv(name)
	return s.check(name, v, set)
}

// check fails a variable that ne or nu rules out, and otherwise gives its
// value.
func (s *substituter) check(name, v string, set bool) (string, error) {
	switch {
	case !set && s.noUnset:
		return "", s.fail("variable ${" + name + "} not set")
	case set && v == "" && s.noEmpty:
		return "", s.fail("variable ${" + name + "} set but empty")
	default:
	}
	return v, nil
}

// nameLen is the length of the variable name text starts with.
func nameLen(text string) int {
	for i, c := range text {
		if c != '_' && (c < '0' || c > '9') && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
			return i
		}
	}
	return len(text)
}

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

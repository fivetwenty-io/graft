package yq

import (
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// errEnvDisabled is yq's message when --security-disable-env-ops is set.
var errEnvDisabled = &query.RuntimeError{Msg: "env operations have been disabled"}

// registerEnv adds env, strenv, and envsubst. Each reads the process
// environment only while the dialect's capabilities allow it. env reads
// a variable that is set but empty as not provided, as yq does.
func registerEnv(r *runtime.Registry) {
	// yq has no env without an argument, and Parse already refuses one.
	// This keeps jq's whole-environment env out of reach all the same.
	r.RegisterSingle("env", 0, func(*runtime.Ctx, *value.Value, []runtime.Thunk, runtime.Emit) error {
		return &query.RuntimeError{Msg: "lexer: invalid input text " + strconv.Quote("env")}
	})
	r.RegisterSingle("env", 1, func(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		return envName(ctx, in, args[0], func(name string) error {
			text, ok := os.LookupEnv(name)
			if !ok || text == "" {
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

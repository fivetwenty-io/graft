package jq

import (
	"io"
	"strings"

	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// registerJQ adds the natives only jq has: the ones that end a run, write
// to stderr, read further inputs, or describe the builtins themselves.
func registerJQ(r *runtime.Registry) {
	r.RegisterSingle("halt", 0, func(*runtime.Ctx, *value.Value, []runtime.Thunk, runtime.Emit) error {
		return &query.HaltError{Code: 0}
	})
	r.RegisterSingle("halt_error", 0, func(_ *runtime.Ctx, in *value.Value, _ []runtime.Thunk, _ runtime.Emit) error {
		return &query.HaltError{Code: 5, Message: in}
	})
	r.RegisterSingle("halt_error", 1, func(_ *runtime.Ctx, in *value.Value, args []runtime.Thunk, _ runtime.Emit) error {
		return args[0].Each(in, func(code *value.Value, _ value.Path) error {
			if code.Kind() != value.KindNumber {
				return &query.RuntimeError{Msg: runtime.TypeName(code) + " (" + runtime.Dump(code) + ") halt_error/1: number required"}
			}
			return &query.HaltError{Code: int(code.Float()), Message: in}
		})
	})
	r.RegisterSingle("debug", 0, func(ctx *runtime.Ctx, in *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		debug(ctx.Stderr, in)
		return emit(in, nil)
	})
	r.RegisterSingle("debug", 1, func(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		err := args[0].Each(in, func(msg *value.Value, _ value.Path) error {
			debug(ctx.Stderr, msg)
			return nil
		})
		if err != nil {
			return err
		}
		return emit(in, nil)
	})
	r.RegisterSingle("stderr", 0, func(ctx *runtime.Ctx, in *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		if ctx.Stderr != nil {
			text := runtime.JSON(in)
			if in.Kind() == value.KindString {
				text = in.Str()
			}
			_, _ = io.WriteString(ctx.Stderr, text)
		}
		return emit(in, nil)
	})
	r.RegisterSingle("input", 0, func(ctx *runtime.Ctx, _ *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		v, ok, err := next(ctx)
		if err != nil {
			return err
		}
		if !ok {
			return &query.RuntimeError{Msg: "No more inputs"}
		}
		return emit(v, nil)
	})
	r.Register("inputs", 0, func(ctx *runtime.Ctx, _ *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		for {
			v, ok, err := next(ctx)
			if err != nil || !ok {
				return err
			}
			if err := emit(v, nil); err != nil {
				return err
			}
		}
	})
	r.RegisterSingle("input_filename", 0, func(ctx *runtime.Ctx, _ *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		if ctx.Inputs == nil || ctx.Inputs.Filename() == "" {
			return emit(value.Null(), nil)
		}
		return emit(value.Str(ctx.Inputs.Filename()), nil)
	})
	registerModules(r)
	r.RegisterSingle("input_line_number", 0, func(ctx *runtime.Ctx, _ *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		if ctx.Inputs == nil {
			return emit(value.Num(0), nil)
		}
		return emit(value.Num(float64(ctx.Inputs.Line())), nil)
	})
	constant := func(name string, v *value.Value) {
		r.RegisterSingle(name, 0, func(_ *runtime.Ctx, _ *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
			return emit(v, nil)
		})
	}
	constant("have_literal_numbers", value.Bool(true))
	constant("have_decnum", value.Bool(true))
	r.RegisterSingle("builtins", 0, func(ctx *runtime.Ctx, _ *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		return emit(builtinNames(ctx.Dialect.Builtins()), nil)
	})
}

// next reads the next input, reporting false when there is none.
func next(ctx *runtime.Ctx) (*value.Value, bool, error) {
	if ctx.Inputs == nil {
		return nil, false, nil
	}
	v, _, ok, err := ctx.Inputs.Next()
	return v, ok, err
}

// debug writes a value to w as jq's debug message.
func debug(w io.Writer, v *value.Value) {
	if w != nil {
		_, _ = io.WriteString(w, `["DEBUG:",`+runtime.JSON(v)+"]\n")
	}
}

// builtinNames lists r's builtins as "name/arity" strings, leaving out the
// private helpers, whose names start with an underscore, and the @formats.
// path/1 is the evaluator's own, so it is added by name.
func builtinNames(r *runtime.Registry) *value.Value {
	b := value.NewArrBuilder(0)
	b.Append(value.Str("path/1"))
	for _, n := range r.Names() {
		if !strings.HasPrefix(n, "_") && !strings.HasPrefix(n, "@") {
			b.Append(value.Str(n))
		}
	}
	return b.Seal()
}

// registerModules adds modulemeta and the builtins that report where
// modules are searched for.
func registerModules(r *runtime.Registry) {
	lib := func(ctx *runtime.Ctx) runtime.ModuleLibrary {
		if ctx.Modules != nil {
			return ctx.Modules
		}
		return NewModuleResolver(CompileOptions{})
	}
	r.RegisterSingle("modulemeta", 0, func(ctx *runtime.Ctx, in *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		if in.Kind() != value.KindString {
			return &query.RuntimeError{Msg: "modulemeta input module name must be a string"}
		}
		meta, err := lib(ctx).Meta(in.Str())
		if err != nil {
			return err
		}
		return emit(meta, nil)
	})
	r.RegisterSingle("get_search_list", 0, func(ctx *runtime.Ctx, _ *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		paths := lib(ctx).SearchList()
		vals := make([]*value.Value, len(paths))
		for i, p := range paths {
			vals[i] = value.Str(p)
		}
		return emit(value.Arr(vals...), nil)
	})
	r.RegisterSingle("get_prog_origin", 0, func(ctx *runtime.Ctx, _ *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		return emit(value.Str(lib(ctx).ProgOrigin()), nil)
	})
	r.RegisterSingle("get_jq_origin", 0, func(ctx *runtime.Ctx, _ *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		return emit(value.Str(lib(ctx).JQOrigin()), nil)
	})
}

package jq

import (
	"context"
	"errors"
	"io"
	"iter"
	"slices"
	"sync"

	"github.com/fivetwenty-io/graft/internal/query/eval"
	jqdialect "github.com/fivetwenty-io/graft/internal/query/jq"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/internal/query/suite"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// Program is a jq program whose syntax has been checked and whose names
// have not yet been resolved.
type Program struct {
	src string
}

// Parse checks the syntax of the jq program src. A program that does not
// parse returns a *query.CompileError. Parse does not look up the names the
// program uses, so a call to a function that does not exist is reported by
// Compile.
func Parse(src string) (*Program, error) {
	if _, err := jqdialect.Parse(src); err != nil {
		return nil, err
	}
	return &Program{src: src}, nil
}

// Compile resolves the program's names against jq's builtins and the
// variables that opts bind, and returns a Query ready to run.
func (p *Program) Compile(opts ...Option) (*Query, error) {
	return Compile(p.src, opts...)
}

// Query is a compiled jq program with its options applied. A Query may be
// run any number of times.
type Query struct {
	prog *eval.Program
	env  *eval.Env
	cfg  config
}

// Compile parses the jq program src, resolves every name it uses, and
// applies opts. A program that does not parse or names something that is
// not defined returns a *query.CompileError, and an option that cannot
// apply returns its own error.
func Compile(src string, opts ...Option) (*Query, error) {
	var cfg config
	for _, opt := range opts {
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}
	names := make([]string, 0, len(cfg.named))
	named := value.NewObjBuilder(len(cfg.named))
	for _, a := range cfg.named {
		if !builtinVariable(a.name) {
			names = append(names, a.name)
		}
		named.Set(a.name, a.val)
	}
	prog, err := jqdialect.CompileWith(src, cfg.compileOptions(), names...)
	if err != nil {
		return nil, err
	}
	args := value.Obj(
		value.Pair{Key: "positional", Value: value.Arr(cfg.positional...)},
		value.Pair{Key: "named", Value: named.Seal()},
	)
	env := prog.Env.Bind("ARGS", args)
	for _, a := range cfg.named {
		if !builtinVariable(a.name) {
			env = env.Bind(a.name, a.val)
		}
	}
	return &Query{prog: prog, env: env, cfg: cfg}, nil
}

// compileOptions are the module settings the dialect compiles with.
func (c config) compileOptions() jqdialect.CompileOptions {
	return jqdialect.CompileOptions{SearchPaths: c.modulePaths, ProgramOrigin: c.origin, AutoInclude: c.autoInclude, Unconfined: c.unconfined}
}

// builtinVariable reports whether name is $ARGS or $ENV, which an argument
// of the same name never shadows. As in jq, such an argument appears only
// under $ARGS.named.
func builtinVariable(name string) bool {
	return name == "ARGS" || name == "ENV"
}

// errStopped is how Run unwinds the evaluator once its caller stops
// ranging. It is not a *query.RuntimeError, so no try in the program can
// catch it.
var errStopped = errors.New("the caller stopped reading outputs")

// Run runs the query on input and yields each output in turn. An error ends
// the sequence as its last pair, after every output produced before it, and
// is a *query.RuntimeError, a *query.HaltError, the context's error when
// ctx ends the run, or an error from the WithInputs function. Stopping the
// range early stops the evaluation too. A nil input is null.
func (q *Query) Run(ctx context.Context, input *value.Value) iter.Seq2[*value.Value, error] {
	if input == nil {
		input = value.Null()
	}
	return func(yield func(*value.Value, error) bool) {
		stopped := false
		err := q.prog.Run(q.newCtx(ctx), q.env, []*value.Value{input}, func(v *value.Value, _ value.Path) error {
			if stopped || !yield(v, nil) {
				stopped = true
				return errStopped
			}
			return nil
		})
		if err != nil && !stopped {
			yield(nil, err)
		}
	}
}

// RunAll runs the query on input and returns every output. When the run
// fails, it returns the outputs produced before the error along with it.
func (q *Query) RunAll(ctx context.Context, input *value.Value) ([]*value.Value, error) {
	var out []*value.Value
	for v, err := range q.Run(ctx, input) {
		if err != nil {
			return out, err
		}
		out = append(out, v)
	}
	return out, nil
}

// Builtins returns the name/arity of every builtin a program can call, in
// the order jq's builtins function lists them.
func (q *Query) Builtins() []string {
	return slices.Clone(builtinNames())
}

// builtinNames runs jq's own builtins function once, so the list matches
// what a program sees.
var builtinNames = sync.OnceValue(func() []string {
	prog, err := jqdialect.Compile("builtins")
	if err != nil {
		panic(err)
	}
	var names []string
	ctx := runtime.NewCtx(jqdialect.New(), runtime.NumbersExact)
	err = prog.Run(ctx, prog.Env, []*value.Value{value.Null()}, func(v *value.Value, _ value.Path) error {
		for _, n := range v.Elems() {
			names = append(names, n.Str())
		}
		return nil
	})
	if err != nil {
		panic(err)
	}
	return names
})

// newCtx returns the context for one run, with the options applied over
// jq's defaults.
func (q *Query) newCtx(ctx context.Context) *runtime.Ctx {
	c := runtime.NewCtx(jqdialect.New(), q.cfg.numbers)
	if ctx != nil {
		c.Deadline = ctx
	}
	if env := q.cfg.env; env != nil {
		c.Env = func() map[string]string { return env }
	}
	if q.cfg.stderrSet {
		c.Stderr = q.cfg.stderr
	}
	c.Modules = jqdialect.NewModuleResolver(q.cfg.compileOptions())
	c.DepthLimit = q.cfg.depth
	c.RegexBudget = q.cfg.budget
	if q.cfg.inputs != nil || q.cfg.filename != nil {
		c.Inputs = inputSource{next: q.cfg.inputs, name: q.cfg.filename}
	}
	return c
}

// inputSource adapts the WithInputs and WithInputFilename functions to the
// stream input and inputs read.
type inputSource struct {
	next func() (*value.Value, error)
	name func() string
}

func (s inputSource) Next() (*value.Value, string, bool, error) {
	if s.next == nil {
		return nil, "", false, nil
	}
	v, err := s.next()
	if errors.Is(err, io.EOF) {
		return nil, "", false, nil
	}
	if err != nil {
		return nil, "", false, err
	}
	return v, s.Filename(), true, nil
}

func (s inputSource) Filename() string {
	if s.name == nil {
		return ""
	}
	return s.name()
}

// Line reports no line, because a WithInputs function does not say where
// its values came from.
func (inputSource) Line() int { return 0 }

// RunTestFile runs a test file in jq's --run-tests format, writes jq's line
// for each case and each failure to w followed by the summary, and returns
// how many cases passed and failed. A case that cannot be judged, such as
// a program that should compile and does not, counts as a failure.
func RunTestFile(r io.Reader, w io.Writer) (passed, failed int, err error) {
	cases, err := suite.ParseFile(r)
	if err != nil {
		return 0, 0, err
	}
	passed, failed, _ = suite.Run(cases, jqdialect.New(), w)
	return passed, failed, nil
}

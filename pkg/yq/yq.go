package yq

import (
	"context"
	"errors"
	"iter"
	"strings"

	"github.com/fivetwenty-io/graft/internal/query/ast"
	"github.com/fivetwenty-io/graft/internal/query/load"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	yqdialect "github.com/fivetwenty-io/graft/internal/query/yq"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// Expression is a yq expression whose syntax has been checked.
type Expression struct {
	src  string
	node ast.Node
}

// Parse checks the syntax of the yq expression src. An expression that does
// not parse returns a *query.CompileError. A blank expression is the
// identity, as it is in yq.
func Parse(src string) (*Expression, error) {
	node, err := yqdialect.Parse(src)
	if err != nil {
		return nil, compileError(src, err)
	}
	return &Expression{src: src, node: node}, nil
}

// Compile applies opts to the expression and returns an Evaluator ready to
// run. The expression is not parsed a second time.
func (x *Expression) Compile(opts ...Option) (*Evaluator, error) {
	return newEvaluator(x, opts)
}

// compileError turns a parse failure into the public error. The yq parser
// gives a byte offset, and the public error carries a line and column.
func compileError(src string, err error) error {
	var pe *yqdialect.ParseError
	if !errors.As(err, &pe) {
		return &query.CompileError{Msg: err.Error()}
	}
	off := min(max(pe.Pos, 0), len(src))
	line := 1 + strings.Count(src[:off], "\n")
	col := off - strings.LastIndexByte(src[:off], '\n')
	return &query.CompileError{Msg: pe.Msg, Line: line, Col: col}
}

// Evaluator is a parsed expression with its options applied. An Evaluator
// may be run any number of times, and from many goroutines at once.
type Evaluator struct {
	node    ast.Node
	dialect *yqdialect.Dialect
	cfg     Config
}

// Compile parses the yq expression src and applies opts. An expression that
// does not parse returns a *query.CompileError, and an option that cannot
// apply returns its own error.
func Compile(src string, opts ...Option) (*Evaluator, error) {
	cfg, err := configure(opts)
	if err != nil {
		return nil, err
	}
	x, err := parseWith(src, cfg)
	if err != nil {
		return nil, err
	}
	return evaluatorFor(x, cfg), nil
}

// parseWith is Parse for a run that cfg configures. The lexer decides what
// "\(" in a string means, so interpolation has to be known before the
// expression is read.
func parseWith(src string, cfg Config) (*Expression, error) {
	node, err := yqdialect.ParseWith(src, yqdialect.ParseOptions{NoInterpolation: !cfg.interpolate})
	if err != nil {
		return nil, compileError(src, err)
	}
	return &Expression{src: src, node: node}, nil
}

// configure applies opts to the default configuration.
func configure(opts []Option) (Config, error) {
	cfg := defaultConfig()
	for _, opt := range opts {
		if err := opt(&cfg); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}

func newEvaluator(x *Expression, opts []Option) (*Evaluator, error) {
	cfg, err := configure(opts)
	if err != nil {
		return nil, err
	}
	if !cfg.interpolate {
		// x was parsed with interpolation on, so it is read again.
		if x, err = parseWith(x.src, cfg); err != nil {
			return nil, err
		}
	}
	return evaluatorFor(x, cfg), nil
}

func evaluatorFor(x *Expression, cfg Config) *Evaluator {
	d := yqdialect.New(yqdialect.Options{
		Caps:                cfg.caps,
		MergeAnchorToSpec:   cfg.mergeFix,
		StringInterpolation: cfg.interpolate,
		FloatNumbers:        cfg.numbers == runtime.NumbersFloat,
		NoHeaderPreprocess:  !cfg.headerPre,
	})
	return &Evaluator{node: x.node, dialect: d, cfg: cfg}
}

// errStopped is how a run unwinds once its caller stops ranging. It is not
// a *query.RuntimeError, so nothing in the expression can catch it.
var errStopped = errors.New("the caller stopped reading outputs")

// Eval runs the expression with doc as its only root, as yq's eval does
// for each document, and yields each output in turn. An error ends the
// sequence as its last pair, after every output produced before it, and is
// a *query.RuntimeError or the context's error when ctx ends the run.
// Stopping the range early stops the evaluation too. A nil doc is null.
func (e *Evaluator) Eval(ctx context.Context, doc *value.Value) iter.Seq2[*value.Value, error] {
	return e.run(ctx, []load.Document{documentOf(doc)})
}

// EvalAll runs the expression once with every document in docs as a root,
// as yq's eval-all does, so an expression that gathers, such as [.a], sees
// all of them, and yields each output in turn. It ends the way Eval does.
func (e *Evaluator) EvalAll(ctx context.Context, docs []*value.Value) iter.Seq2[*value.Value, error] {
	loaded := make([]load.Document, len(docs))
	for i, d := range docs {
		loaded[i] = documentOf(d)
	}
	return e.run(ctx, loaded)
}

// EvalWith is Eval with the variables in vars bound for the run, each name
// without its "$", so that {"index": v} makes $index read v.
func (e *Evaluator) EvalWith(ctx context.Context, doc *value.Value, vars map[string]*value.Value) iter.Seq2[*value.Value, error] {
	return e.runWith(ctx, []load.Document{documentOf(doc)}, vars)
}

func (e *Evaluator) run(ctx context.Context, docs []load.Document) iter.Seq2[*value.Value, error] {
	return e.runWith(ctx, docs, nil)
}

func (e *Evaluator) runWith(ctx context.Context, docs []load.Document, vars map[string]*value.Value) iter.Seq2[*value.Value, error] {
	return func(yield func(*value.Value, error) bool) {
		stopped := false
		var resolve func(root, v *value.Value) (*value.Value, error)
		if e.cfg.resolve {
			resolve = e.dialect.Resolver(e.cfg.stderr)
		}
		err := yqdialect.RunNodeVars(e.dialect, e.node, docs, e.setup(ctx), vars, func(d load.Document, v *value.Value) error {
			if resolve != nil {
				var err error
				if v, err = resolve(d.Value, v); err != nil {
					return err
				}
			}
			if stopped || !yield(stamp(d, v), nil) {
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

// setup applies the options and the caller's context to one run.
func (e *Evaluator) setup(ctx context.Context) func(*runtime.Ctx) {
	return func(c *runtime.Ctx) {
		if ctx != nil {
			c.Deadline = ctx
		}
		if e.cfg.envSet {
			env := e.cfg.env
			c.Env = func() map[string]string { return env }
		}
		if e.cfg.stderrSet {
			c.Stderr = e.cfg.stderr
		}
		c.RegexBudget = e.cfg.regexBudget
	}
}

// documentOf reads the position of a decoded root from its Meta, which is
// where a Decoder leaves the document index, file index, and name.
func documentOf(v *value.Value) load.Document {
	if v == nil {
		v = value.Null()
	}
	d := load.Document{Value: v}
	if m := v.Meta(); m != nil {
		d.Source, d.FileIndex, d.DocIndex, d.Line, d.LeadingContent = m.Filename, m.FileIndex, m.DocIndex, m.Line, m.LeadingContent
	}
	return d
}

// stamp makes v carry the document an output belongs to, so an Encoder can
// place the "---" lines and the document's leading comments from the value
// alone. A value that already says so is returned as it is.
func stamp(d load.Document, v *value.Value) *value.Value {
	m := v.Meta()
	if m == nil && d.DocIndex == 0 && d.FileIndex == 0 && d.LeadingContent == "" {
		return v
	}
	var c value.Meta
	if m != nil {
		if m.DocIndex == d.DocIndex && m.FileIndex == d.FileIndex && m.LeadingContent == d.LeadingContent {
			return v
		}
		c = *m
	} else if v.Kind() == value.KindNull {
		// A null with no text prints as an empty line, which is how a
		// null read from "a:" prints. A null the run built has no
		// details, and yq prints it as null in every document.
		c.Text = "null"
	}
	c.DocIndex, c.FileIndex, c.LeadingContent = d.DocIndex, d.FileIndex, d.LeadingContent
	return v.WithMeta(&c)
}

// Package runtime holds the contracts that the evaluator and the builtins
// share, so that the builtins can import them without importing the
// evaluator. The import graph is builtin -> runtime <- eval.
package runtime

import (
	"context"
	"io"
	"iter"

	"github.com/fivetwenty-io/graft/pkg/query/value"
	"github.com/fivetwenty-io/graft/pkg/regex"
)

// Emit receives one output of a generator. p is the output's path from
// the root while paths are being tracked, and nil otherwise or when the
// output is not a path. An Emit returns an error to stop the generator
// that called it, and the generator must return that error unchanged,
// neither wrapped nor replaced. The evaluator signals control flow through
// these errors and recognizes each one by its type: a tail call handing
// its frame back to the loop that runs its definition, eval.StopError
// when a consumer such as first or limit has seen enough, eval.BreakError
// for break, and eval.DownstreamError marking an error a try's consumer
// raised. A generator that wraps one hides it, and a tail call then runs
// in the wrong place, a stop or a break turns into an error, or a try
// catches an error that is not its own.
type Emit func(v *value.Value, p value.Path) error

// Thunk is an unevaluated argument to a builtin. Each runs it against
// input and emits every output.
type Thunk interface {
	Each(input *value.Value, emit Emit) error
}

// Native is a builtin written in Go. It emits each output for input and
// returns the first error. When emit returns an error, the native stops
// and returns that error unchanged, because the evaluator's control
// signals travel as these errors, as Emit describes. The same goes for an
// argument's Each, except that a native which catches errors may handle
// a *query.RuntimeError its argument raised. A native wraps or replaces
// only the errors it raises itself.
type Native func(ctx *Ctx, input *value.Value, args []Thunk, emit Emit) error

// NumberMode chooses how numbers compute.
type NumberMode uint8

// The number modes. NumbersExact is decimal arithmetic and the default,
// and NumbersFloat is jq's double arithmetic for --float-numbers.
const (
	NumbersExact NumberMode = iota
	NumbersFloat
)

// Ctx is the state one run of a program shares with every builtin it
// calls. NewCtx fills in the defaults.
type Ctx struct {
	Dialect Dialect
	Inputs  InputSource
	// Env returns the process environment. It is a function so that the
	// environment is read only when a builtin asks and Caps.Env allows.
	Env      func() map[string]string
	Stderr   io.Writer
	Deadline context.Context
	Caps     Capabilities
	// DepthLimit bounds non-tail calls and recursive builtins, and zero
	// means DefaultDepthLimit.
	DepthLimit int
	// RegexBudget bounds every regular expression the run compiles and
	// matches, and its zero fields mean pkg/regex's defaults.
	RegexBudget regex.Budget
	Cursor      *Cursor
	// Cursors makes the evaluator track where every value it reads came
	// from and set Cursor before each native it calls, which yq's parent,
	// key, and path read.
	Cursors bool
	Numbers NumberMode
	// Compare and Equal are the value comparisons for Numbers, which every
	// comparison in the evaluator and the builtins calls.
	Compare func(a, b *value.Value) int
	Equal   func(a, b *value.Value) bool

	depth int
	ticks int
}

// InputSource is the stream that input and inputs read. Next returns the
// next value with the name of the file it came from, and false at the end.
type InputSource interface {
	Next() (*value.Value, string, bool, error)
	Filename() string
	Line() int
}

// IndexMode tells Dialect.Index why it is indexing.
type IndexMode uint8

// The index modes. IndexUpdate is a step on the left of an assignment,
// where yq creates what is missing, and IndexOptional is a step under ?.
const (
	IndexRead IndexMode = iota
	IndexUpdate
	IndexOptional
)

// AssignMode tells Dialect.Assign which assignment it runs. AssignSet is
// =, AssignUpdate is |=, and AssignArith builds the arithmetic forms such
// as += and //=.
type AssignMode struct {
	op    string
	apply func(cur, rhs *value.Value) (*value.Value, error)
}

// Dialect is what makes the engine jq or yq. Each method is jq's semantics
// in the jq dialect, and yq's where yq differs.
type Dialect interface {
	Name() string
	Builtins() *Registry
	Capabilities() Capabilities
	Truthy(v *value.Value) bool
	Compare(a, b *value.Value) int
	// Index returns container[key] under the run's context, and nil with
	// no error when there is no output.
	Index(ctx *Ctx, container, key *value.Value, mode IndexMode) (*value.Value, error)
	Iterate(v *value.Value, emit func(key, elem *value.Value) error) error
	// Recurse emits v and everything under it, each with its path
	// relative to v.
	Recurse(v *value.Value, includeKeys bool, emit func(*value.Value, value.Path) error) error
	// Assign runs an assignment over the paths the left side produced
	// against root, and yields each resulting root. rhs runs the right
	// side against a value. ctx is the run's context.
	Assign(ctx *Ctx, root *value.Value, paths []value.Path, rhs func(cur *value.Value) iter.Seq2[*value.Value, error], mode AssignMode) iter.Seq2[*value.Value, error]
	FormatError(err error) string
}

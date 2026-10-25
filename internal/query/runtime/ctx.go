package runtime

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/fivetwenty-io/graft/pkg/decimal"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// DefaultDepthLimit is the depth a Ctx allows when DepthLimit is zero,
// which matches jq's parser limit.
const DefaultDepthLimit = 10000

// tickInterval is how many calls pass between checks of the deadline.
const tickInterval = 1024

// AssignSet is =, and AssignUpdate is |=.
var (
	AssignSet    = AssignMode{op: "="}
	AssignUpdate = AssignMode{op: "|="}
)

// AssignArith returns the mode for an arithmetic assignment such as += or
// //=. apply combines a path's current value with one right-hand output.
func AssignArith(op string, apply func(cur, rhs *value.Value) (*value.Value, error)) AssignMode {
	return AssignMode{op: op, apply: apply}
}

// Op returns the assignment operator as written.
func (m AssignMode) Op() string { return m.op }

// Arith reports whether the mode is an arithmetic assignment.
func (m AssignMode) Arith() bool { return m.apply != nil }

// Apply combines cur and rhs for an arithmetic assignment, and returns rhs
// for any other mode.
func (m AssignMode) Apply(cur, rhs *value.Value) (*value.Value, error) {
	if m.apply == nil {
		return rhs, nil
	}
	return m.apply(cur, rhs)
}

// NewCtx returns a Ctx for dialect d with the comparisons for numbers, the
// dialect's capabilities, the default depth limit, no deadline, stderr,
// and the process environment.
func NewCtx(d Dialect, numbers NumberMode) *Ctx {
	c := &Ctx{
		Dialect:    d,
		Env:        processEnv,
		Stderr:     os.Stderr,
		Deadline:   context.Background(),
		DepthLimit: DefaultDepthLimit,
		Numbers:    numbers,
		Compare:    value.Compare,
		Equal:      value.Equal,
	}
	if numbers == NumbersFloat {
		c.Compare, c.Equal = value.CompareFloat, value.EqualFloat
	}
	if d != nil {
		c.Caps = d.Capabilities()
	}
	return c
}

func processEnv() map[string]string {
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	return env
}

// Number reads a number's value, which is the one way arithmetic,
// comparison, and conversions read one. A literal past the decimal bounds
// reads as its nearest double under NumbersFloat, and otherwise fails with
// the bound it broke, so it passes through untouched until something
// needs its value.
func (c *Ctx) Number(v *value.Value) (decimal.Decimal, error) {
	d := v.Dec()
	err := d.Err()
	if err == nil {
		return d, nil
	}
	if c.Numbers == NumbersFloat {
		return decimal.FromFloat64(v.Float()), nil
	}
	return decimal.Decimal{}, &query.RuntimeError{Msg: err.Error()}
}

// CheckNumbers returns the bound error of the first number in v that
// Number would refuse, so a comparison fails the way arithmetic does
// while value.Compare stays total.
func (c *Ctx) CheckNumbers(v *value.Value) error {
	if c.Numbers == NumbersFloat {
		return nil
	}
	switch v.Kind() {
	case value.KindNumber:
		_, err := c.Number(v)
		return err
	case value.KindArray:
		for _, e := range v.Elems() {
			if err := c.CheckNumbers(e); err != nil {
				return err
			}
		}
	case value.KindObject:
		for _, e := range v.Entries() {
			if err := c.CheckNumbers(e.Value); err != nil {
				return err
			}
		}
	default:
	}
	return nil
}

// Enter counts one level of depth and fails once the limit is passed. A
// successful Enter is paired with Leave.
func (c *Ctx) Enter() error {
	limit := c.DepthLimit
	if limit <= 0 {
		limit = DefaultDepthLimit
	}
	if c.depth >= limit {
		return &query.RuntimeError{Msg: fmt.Sprintf("exceeds depth limit of %d", limit)}
	}
	c.depth++
	return nil
}

// Leave gives back the level a successful Enter took.
func (c *Ctx) Leave() { c.depth-- }

// Tick counts one call and checks the deadline on every 1,024th. The error
// is the context's own, which no try catches.
func (c *Ctx) Tick() error {
	c.ticks++
	if c.ticks%tickInterval != 0 || c.Deadline == nil {
		return nil
	}
	return c.Deadline.Err()
}

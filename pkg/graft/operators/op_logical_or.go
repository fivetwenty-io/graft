package operators

import (
	"fmt"

	"github.com/fivetwenty-io/graft/log"
	"github.com/fivetwenty-io/graft/pkg/graft"
	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// LogicalOrOperator implements true logical OR (not fallback)
// This is separate from OrElseOperator which implements fallback/coalesce behavior
// The || operator is registered as OrElseOperator by default, but this can be
// used explicitly when true boolean OR semantics are needed.
type LogicalOrOperator struct {
	*BooleanOperatorBase
}

// NewLogicalOrOperator creates a new logical OR operator.
func NewLogicalOrOperator() *LogicalOrOperator {
	return &LogicalOrOperator{
		BooleanOperatorBase: NewBooleanOperatorBase("||", true), // true = short-circuit
	}
}

// BooleanOrOperator implements true logical OR using the BooleanOperatorBase
// This is a simpler implementation that doesn't use BooleanOperatorBase embedding.
type BooleanOrOperator struct{}

// Setup initializes the operator.
func (BooleanOrOperator) Setup() error {
	return nil
}

// Phase returns the operator phase.
func (BooleanOrOperator) Phase() graft.OperatorPhase {
	return graft.EvalPhase
}

// Dependencies returns operator dependencies.
func (BooleanOrOperator) Dependencies(_ *graft.Evaluator, args []*graft.Expr, _ []*tree.Cursor, auto []*tree.Cursor) []*tree.Cursor {
	deps := make([]*tree.Cursor, 0)
	for _, arg := range args {
		if arg.Type == graft.Reference && arg.Reference != nil {
			deps = append(deps, arg.Reference)
		}
	}
	return append(auto, deps...)
}

// Run executes the logical OR with short-circuit evaluation.
func (BooleanOrOperator) Run(ev *graft.Evaluator, args []*graft.Expr) (*graft.Response, error) {
	log.DEBUG("BooleanOrOperator.Run called")
	log.DEBUG("running (( or ... )) operation at $.%s", ev.Here)
	defer log.DEBUG("done with (( or ... )) operation at $.%s\n", ev.Here)

	if len(args) != 2 {
		return nil, fmt.Errorf("or operator requires exactly 2 arguments, got %d", len(args))
	}

	// Short-circuit evaluation: evaluate left first
	left, err := ResolveOperatorArgument(ev, args[0])
	if err != nil {
		return nil, err
	}

	leftTruthy := IsTruthy(left)
	log.DEBUG("  left = %v (truthy: %v)", left, leftTruthy)

	if leftTruthy {
		// Left is truthy, return true without evaluating right
		return &graft.Response{
			Type:  graft.Replace,
			Value: true,
		}, nil
	}

	// Left is falsy, evaluate right
	right, err := ResolveOperatorArgument(ev, args[1])
	if err != nil {
		return nil, err
	}

	rightTruthy := IsTruthy(right)
	log.DEBUG("  right = %v (truthy: %v)", right, rightTruthy)

	return &graft.Response{
		Type:  graft.Replace,
		Value: rightTruthy,
	}, nil
}

// TypeAwareLogicalOrOperator implements true logical OR with type awareness.
type TypeAwareLogicalOrOperator struct {
	*BooleanOperatorBase
}

// NewTypeAwareLogicalOrOperator creates a new type-aware logical OR operator.
func NewTypeAwareLogicalOrOperator() *TypeAwareLogicalOrOperator {
	return &TypeAwareLogicalOrOperator{
		BooleanOperatorBase: NewBooleanOperatorBase("or", true),
	}
}

// Note: We don't register this operator as || in init() to avoid conflicts
// with the existing OrElseOperator. The || operator uses fallback semantics.
// If true logical OR is needed, it can be registered under a different name
// like "or" or used programmatically.
//
// To use true logical OR, register it explicitly:
//   RegisterOp("or", NewTypeAwareLogicalOrOperator())

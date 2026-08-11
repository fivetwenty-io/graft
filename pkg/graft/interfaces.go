package graft

import (
	"context"
	"fmt"
	"os"

	"github.com/goccy/go-yaml"

	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// Action represents the type of action an operator should take.
type Action int

const (
	// Replace the current value.
	Replace Action = iota
	// Inject into the parent structure.
	Inject
)

// OperatorPhase represents when an operator runs.
type OperatorPhase int

const (
	// MergePhase runs during document merging.
	MergePhase OperatorPhase = iota
	// EvalPhase runs during evaluation.
	EvalPhase
	// ParamPhase runs during parameter resolution.
	ParamPhase
)

// Response from an operator execution.
type Response struct {
	Type  Action
	Value interface{}
}

// Expr represents a parsed expression.
type Expr struct {
	Type      ExprType
	Operator  string
	Name      string
	Target    string // Target for operator (e.g., "production" in "vault@production")
	Left      *Expr
	Right     *Expr
	Literal   interface{}
	Reference *tree.Cursor
	// BracketedNodes marks, by index, which nodes of Reference.Nodes were
	// written using bracket notation (e.g. "key[lookup]") rather than dot
	// notation, as recovered by tree.BracketsOf. Only populated for
	// Reference expressions; nil/empty for every other expression type.
	BracketedNodes []bool
	Call           *Opcall
	Pos            Position
	evaluator      *Evaluator // Optional evaluator for nested operator calls
}

// ExprType represents the type of expression.
type ExprType int

const (
	// Literal value.
	Literal ExprType = iota
	// Reference to another part of the document.
	Reference
	// List expression.
	List
	// Or expression (||).
	Or
	// Negate expression (!)
	Negate
	// Addition operator.
	Addition
	// Subtraction operator.
	Subtraction
	// Multiplication operator.
	Multiplication
	// Division operator.
	Division
	// Modulo operator.
	Modulo
	// Equal comparison operator.
	Equal
	// NotEqual comparison operator.
	NotEqual
	// LessThan comparison operator.
	LessThan
	// LessThanOrEqual comparison operator.
	LessThanOrEqual
	// GreaterThan comparison operator.
	GreaterThan
	// GreaterThanOrEqual comparison operator.
	GreaterThanOrEqual
	// LogicalAnd logical operator.
	LogicalAnd
	// LogicalOr logical operator.
	LogicalOr
	// RegexpMatch operator.
	RegexpMatch
	// EnvVar reference.
	EnvVar
	// BoshVar variable reference.
	BoshVar
	// OperatorCall represents a nested operator call.
	OperatorCall
	// VaultGroup represents a () grouping expression for vault sub-operators.
	VaultGroup
	// VaultChoice represents a | choice expression for vault sub-operators.
	VaultChoice
)

// Operator interface that all operators must implement.
type Operator interface {
	// Setup performs any necessary initialization
	Setup() error

	// Run evaluates the operator with given arguments
	Run(ev *Evaluator, args []*Expr) (*Response, error)

	// Dependencies returns paths this operator depends on
	Dependencies(ev *Evaluator, args []*Expr, locs []*tree.Cursor, auto []*tree.Cursor) []*tree.Cursor

	// Phase returns when this operator should run
	Phase() OperatorPhase
}

// Opcall represents an operator call.
type Opcall struct {
	src       string
	where     *tree.Cursor
	canonical *tree.Cursor
	op        Operator
	args      []*Expr
}

// Args returns the arguments for this operator call.
func (op *Opcall) Args() []*Expr {
	return op.args
}

// Canonical returns the canonical cursor for this operator call.
func (op *Opcall) Canonical() *tree.Cursor {
	return op.canonical
}

// Operator returns the operator for this call.
func (op *Opcall) Operator() Operator {
	return op.op
}

// Where returns the cursor location for this operator call.
func (op *Opcall) Where() *tree.Cursor {
	return op.where
}

// SetWhere sets the cursor location for this operator call.
func (op *Opcall) SetWhere(cursor *tree.Cursor) {
	op.where = cursor
}

// Src returns the source string for this operator call.
func (op *Opcall) Src() string {
	return op.src
}

// Dependencies returns the dependencies for this operator call.
func (op *Opcall) Dependencies(ev *Evaluator, locs []*tree.Cursor) []*tree.Cursor {
	l := []*tree.Cursor{}
	for _, arg := range op.args {
		if arg != nil {
			l = append(l, arg.Dependencies(ev, locs)...)
		}
	}
	return op.op.Dependencies(ev, op.args, locs, l)
}

// Run executes this operator call.
func (op *Opcall) Run(ev *Evaluator) (*Response, error) {
	was := ev.Here
	ev.Here = op.where
	r, err := op.op.Run(ev, op.args)
	ev.Here = was

	if err != nil {
		if op.where != nil {
			return nil, fmt.Errorf("$.%s: %w", op.where, err)
		}
		return nil, fmt.Errorf("$.<generated>: %w", err)
	}
	return r, nil
}

// IsOperator checks if an expression is an operator call.
func (e *Expr) IsOperator() bool {
	return e != nil && e.Type == OperatorCall
}

// IsOperatorNamed checks if an expression is a specific operator.
func (e *Expr) IsOperatorNamed(name string) bool {
	return e.IsOperator() && e.Operator == name
}

// GetOperatorName returns the operator name if this is an operator expression.
func (e *Expr) GetOperatorName() string {
	if e.IsOperator() {
		return e.Operator
	}
	return ""
}

// Op returns the operator name for compatibility.
func (e *Expr) Op() string {
	return e.Operator
}

// Args returns the arguments for an operator call expression.
func (e *Expr) Args() []*Expr {
	if e.Call != nil {
		return e.Call.Args()
	}
	// For binary operators, return left and right as args
	if e.Left != nil && e.Right != nil {
		return []*Expr{e.Left, e.Right}
	}
	if e.Left != nil {
		return []*Expr{e.Left}
	}
	return nil
}

// containsLiteral checks if an expression contains a literal value
// that would cause a LogicalOr to short-circuit.
func containsLiteral(e *Expr) bool {
	if e == nil {
		return false
	}

	switch e.Type {
	case Literal:
		// Any non-nil literal will cause short-circuit
		return e.Literal != nil
	case LogicalOr:
		// For nested OR, only check left side to see if it will short-circuit
		// Don't recursively check the right side
		return containsLiteral(e.Left)
	case Reference, List, Or, Negate, Addition, Subtraction, Multiplication, Division, Modulo,
		Equal, NotEqual, LessThan, LessThanOrEqual, GreaterThan, GreaterThanOrEqual,
		LogicalAnd, RegexpMatch, EnvVar, BoshVar, OperatorCall, VaultGroup, VaultChoice:
		// Other expression types don't contain literals that would short-circuit
		return false
	}
	return false
}

// Dependencies returns the dependencies for this expression.
func (e *Expr) Dependencies(ev *Evaluator, locs []*tree.Cursor) []*tree.Cursor {
	deps := []*tree.Cursor{}

	switch e.Type {
	case Reference:
		if e.Reference != nil {
			deps = append(deps, e.Reference)
		}
	case OperatorCall:
		if e.Call != nil {
			deps = append(deps, e.Call.Dependencies(ev, locs)...)
		}
	case LogicalOr:
		// For LogicalOr (||), we need sophisticated handling:
		// 1. Left side is always evaluated (unconditional dependency)
		// 2. Right side is only evaluated if left side fails
		// 3. If left contains a literal, right side will never be evaluated

		if e.Left != nil {
			deps = append(deps, e.Left.Dependencies(ev, locs)...)

			// Check if left side will always short-circuit (contains a literal)
			if containsLiteral(e.Left) {
				// Left side has a literal, so right side will never be evaluated
				// Don't include right side dependencies
				return deps
			}
		}

		// The right side is conditional - only evaluated if left side fails
		// Include it for cycle detection, but only if left side might fail
		if e.Right != nil {
			deps = append(deps, e.Right.Dependencies(ev, locs)...)
		}
		return deps
	case Literal, List, Or, Negate, Addition, Subtraction, Multiplication, Division, Modulo,
		Equal, NotEqual, LessThan, LessThanOrEqual, GreaterThan, GreaterThanOrEqual,
		LogicalAnd, RegexpMatch, EnvVar, BoshVar, VaultGroup, VaultChoice:
		// Fall through to check left and right expressions
	}

	// Check left and right expressions for other expression types
	if e.Left != nil {
		deps = append(deps, e.Left.Dependencies(ev, locs)...)
	}
	if e.Right != nil {
		deps = append(deps, e.Right.Dependencies(ev, locs)...)
	}

	return deps
}

// String returns a string representation of the expression.
//
//nolint:gocyclo // switch handles all expression types for string formatting
func (e *Expr) String() string {
	if e == nil {
		return "<nil>"
	}

	switch e.Type {
	case Literal:
		return fmt.Sprintf("%v", e.Literal)
	case Reference:
		if e.Reference != nil {
			return e.Reference.String()
		}
		return "<nil reference>"
	case OperatorCall:
		return fmt.Sprintf("%s(...)", e.Operator)
	case EnvVar:
		return fmt.Sprintf("$%s", e.Name)
	case BoshVar:
		return fmt.Sprintf("((%s))", e.Name)
	case LogicalOr:
		return fmt.Sprintf("(%s || %s)", e.Left.String(), e.Right.String())
	case LogicalAnd:
		return fmt.Sprintf("(%s && %s)", e.Left.String(), e.Right.String())
	case Addition:
		return fmt.Sprintf("(%s + %s)", e.Left.String(), e.Right.String())
	case Subtraction:
		return fmt.Sprintf("(%s - %s)", e.Left.String(), e.Right.String())
	case Multiplication:
		return fmt.Sprintf("(%s * %s)", e.Left.String(), e.Right.String())
	case Division:
		return fmt.Sprintf("(%s / %s)", e.Left.String(), e.Right.String())
	case Modulo:
		return fmt.Sprintf("(%s %% %s)", e.Left.String(), e.Right.String())
	case Equal:
		return fmt.Sprintf("(%s == %s)", e.Left.String(), e.Right.String())
	case NotEqual:
		return fmt.Sprintf("(%s != %s)", e.Left.String(), e.Right.String())
	case LessThan:
		return fmt.Sprintf("(%s < %s)", e.Left.String(), e.Right.String())
	case LessThanOrEqual:
		return fmt.Sprintf("(%s <= %s)", e.Left.String(), e.Right.String())
	case GreaterThan:
		return fmt.Sprintf("(%s > %s)", e.Left.String(), e.Right.String())
	case GreaterThanOrEqual:
		return fmt.Sprintf("(%s >= %s)", e.Left.String(), e.Right.String())
	case Negate:
		return fmt.Sprintf("!%s", e.Left.String())
	case List:
		return "[list]"
	case Or:
		return fmt.Sprintf("(%s | %s)", e.Left.String(), e.Right.String())
	case RegexpMatch:
		return fmt.Sprintf("(%s =~ %s)", e.Left.String(), e.Right.String())
	case VaultGroup:
		return fmt.Sprintf("(%s)", e.Left.String())
	case VaultChoice:
		return fmt.Sprintf("(%s | %s)", e.Left.String(), e.Right.String())
	}
	return fmt.Sprintf("<unknown type %d>", e.Type)
}

// SetArgs sets the arguments for an operator call expression.
func (e *Expr) SetArgs(args []*Expr) {
	if e.Call != nil {
		e.Call.args = args
	}
}

// SetEvaluator sets the evaluator for this expression tree.
// This is needed for evaluating nested operator calls within expressions.
func (e *Expr) SetEvaluator(ev *Evaluator) {
	if e == nil {
		return
	}
	e.evaluator = ev
	if e.Left != nil {
		e.Left.SetEvaluator(ev)
	}
	if e.Right != nil {
		e.Right.SetEvaluator(ev)
	}
	if e.Call != nil {
		for _, arg := range e.Call.Args() {
			if arg != nil {
				arg.SetEvaluator(ev)
			}
		}
	}
}

// Evaluate evaluates the expression against the given tree.
//
//nolint:gocyclo // switch handles all expression types for evaluation
func (e *Expr) Evaluate(treeData interface{}) (interface{}, error) {
	switch e.Type {
	case Literal:
		return e.Literal, nil
	case Reference:
		if e.Reference != nil {
			return e.Reference.Resolve(treeData)
		}
		return nil, fmt.Errorf("nil reference")
	case EnvVar:
		val := os.Getenv(e.Name)
		if val == "" {
			return val, nil
		}
		var unmarshaled interface{}
		if err := yaml.Unmarshal([]byte(val), &unmarshaled); err == nil {
			unmarshaled = normalizeValue(unmarshaled)
			if _, isString := unmarshaled.(string); !isString {
				return unmarshaled, nil
			}
		}
		return val, nil
	case OperatorCall:
		// Evaluate nested operator call using the evaluator
		if e.evaluator == nil {
			return nil, fmt.Errorf("operator call evaluation requires an evaluator; use SetEvaluator() first")
		}
		if e.Call != nil {
			resp, err := e.Call.Run(e.evaluator)
			if err != nil {
				return nil, err
			}
			return resp.Value, nil
		}
		return nil, fmt.Errorf("operator call has no Call object")
	case LogicalOr:
		// Handle || operator - try left, if it fails try right
		// Treat nil as a valid "found" value - only continue on error
		if e.Left != nil {
			// Propagate evaluator to left side before evaluating
			if e.evaluator != nil && e.Left.evaluator == nil {
				e.Left.SetEvaluator(e.evaluator)
			}
			left, err := e.Left.Evaluate(treeData)
			if err == nil {
				// Return the value even if it's nil
				return left, nil
			}
		}
		if e.Right != nil {
			// Propagate evaluator to right side before evaluating
			if e.evaluator != nil && e.Right.evaluator == nil {
				e.Right.SetEvaluator(e.evaluator)
			}
			return e.Right.Evaluate(treeData)
		}
		return nil, nil
	case Negate, Addition, Subtraction, Multiplication, Division, Modulo,
		Equal, NotEqual, LessThan, LessThanOrEqual, GreaterThan, GreaterThanOrEqual, LogicalAnd:
		// Infix nodes dispatch through the shared EvaluateInfix entry point to
		// the operator already registered for their symbol (see
		// evaluate_infix.go). Requiring an evaluator here mirrors the
		// OperatorCall arm above.
		if e.evaluator == nil {
			return nil, fmt.Errorf("operator call evaluation requires an evaluator; use SetEvaluator() first")
		}
		return EvaluateInfix(e.evaluator, e)
	case List, Or, RegexpMatch, BoshVar, VaultGroup, VaultChoice:
		return nil, fmt.Errorf("unsupported expression type for evaluation: %d", e.Type)
	}
	return nil, fmt.Errorf("unsupported expression type for evaluation: %d", e.Type)
}

// cherryPickPathsKey is the context key for cherry-pick paths
// This allows cherry-pick paths to be passed through the evaluation pipeline
// without modifying all method signatures.
type cherryPickPathsKey struct{}

// WithCherryPickPaths adds cherry-pick paths to the context.
// This is used by MergeBuilder to pass cherry-pick paths to the evaluator
// enabling selective evaluation of operators.
func WithCherryPickPaths(ctx context.Context, paths []string) context.Context {
	return context.WithValue(ctx, cherryPickPathsKey{}, paths)
}

// GetCherryPickPaths extracts cherry-pick paths from the context.
// Used by the engine to retrieve cherry-pick paths and set them on the evaluator.
func GetCherryPickPaths(ctx context.Context) []string {
	if paths, ok := ctx.Value(cherryPickPathsKey{}).([]string); ok {
		return paths
	}
	return nil
}

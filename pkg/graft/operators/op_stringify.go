package operators

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// StringifyOperator handles nested operator calls.
type StringifyOperator struct{}

// Setup ...
func (StringifyOperator) Setup() error {
	return nil
}

// Phase ...
func (StringifyOperator) Phase() OperatorPhase {
	return EvalPhase
}

// Dependencies ...
func (StringifyOperator) Dependencies(_ *Evaluator, _ []*Expr, _ []*tree.Cursor, auto []*tree.Cursor) []*tree.Cursor {
	return auto
}

// Run ...
func (StringifyOperator) Run(ev *Evaluator, args []*Expr) (*Response, error) {
	DEBUG("running (( stringify ... )) operation at $.%s", ev.Here)
	defer DEBUG("done with (( stringify ... )) operation at $.%s\n", ev.Here)

	if len(args) != 1 {
		return nil, fmt.Errorf("stringify operator requires exactly one argument")
	}

	// Use ResolveOperatorArgument to handle nested expressions
	val, err := ResolveOperatorArgument(ev, args[0])
	if err != nil {
		DEBUG("failed to resolve expression to a concrete value")
		DEBUG("error was: %s", err)
		return nil, err
	}

	// Handle nil specially
	if val == nil {
		DEBUG("resolved to nil, returning nil")
		return &Response{
			Type:  Replace,
			Value: nil,
		}, nil
	}

	// For scalars, convert directly to string
	switch v := val.(type) {
	case string:
		DEBUG("already a string: %s", v)
		return &Response{
			Type:  Replace,
			Value: v,
		}, nil

	case int, int64, float64, bool:
		DEBUG("converting scalar to string: %v", v)
		return &Response{
			Type:  Replace,
			Value: fmt.Sprintf("%v", v),
		}, nil
	}

	// For complex types, use YAML marshaling with 2-space indent
	DEBUG("converting complex type to YAML string")
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(val); err != nil {
		DEBUG("YAML marshaling failed: %s", err)
		return nil, fmt.Errorf("unable to stringify value: %w", err)
	}
	enc.Close()

	result := buf.String()

	return &Response{
		Type:  Replace,
		Value: result,
	}, nil
}

//nolint:gochecknoinits // Operator registration must happen at package load time
func init() {
	RegisterOp("stringify", StringifyOperator{})
}

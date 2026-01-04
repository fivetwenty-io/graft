package operators

import (
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// TestIsTruthy tests the IsTruthy function with various types.
func TestIsTruthy(t *testing.T) {
	tests := []struct {
		name     string
		value    interface{}
		expected bool
	}{
		// nil/null cases
		{name: "nil is falsy", value: nil, expected: false},

		// boolean cases
		{name: "true is truthy", value: true, expected: true},
		{name: "false is falsy", value: false, expected: false},

		// integer cases
		{name: "positive int is truthy", value: 1, expected: true},
		{name: "negative int is truthy", value: -1, expected: true},
		{name: "zero int is falsy", value: 0, expected: false},
		{name: "int64 positive is truthy", value: int64(1), expected: true},
		{name: "int64 zero is falsy", value: int64(0), expected: false},

		// float cases
		{name: "positive float is truthy", value: 1.5, expected: true},
		{name: "negative float is truthy", value: -1.5, expected: true},
		{name: "zero float is falsy", value: 0.0, expected: false},
		{name: "float32 positive is truthy", value: float32(1.5), expected: true},
		{name: "float32 zero is falsy", value: float32(0.0), expected: false},

		// string cases
		{name: "non-empty string is truthy", value: "hello", expected: true},
		{name: "empty string is falsy", value: "", expected: false},
		{name: "whitespace string is truthy", value: " ", expected: true},
		{name: "string zero is truthy", value: "0", expected: true},
		{name: "string false is truthy", value: "false", expected: true},

		// slice/array cases
		{name: "non-empty slice is truthy", value: []interface{}{1, 2, 3}, expected: true},
		{name: "empty slice is falsy", value: []interface{}{}, expected: false},
		{name: "string slice non-empty is truthy", value: []string{"a"}, expected: true},
		{name: "string slice empty is falsy", value: []string{}, expected: false},

		// map cases
		{name: "non-empty map is truthy", value: map[string]interface{}{"key": "value"}, expected: true},
		{name: "empty string map is falsy", value: map[string]interface{}{}, expected: false},
		{name: "non-empty interface map is truthy", value: map[interface{}]interface{}{"key": "value"}, expected: true},
		{name: "empty interface map is falsy", value: map[interface{}]interface{}{}, expected: false},

		// other types (always truthy when non-nil)
		{name: "struct is truthy", value: struct{}{}, expected: true},
		{name: "pointer to int is truthy", value: new(int), expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsTruthy(tt.value)
			if result != tt.expected {
				t.Errorf("IsTruthy(%v) = %v, expected %v", tt.value, result, tt.expected)
			}
		})
	}
}

// TestNegateOperator tests the negate operator.
func TestNegateOperator(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected bool
	}{
		// nil/null cases
		{name: "negate nil", input: nil, expected: true},

		// boolean cases
		{name: "negate true", input: true, expected: false},
		{name: "negate false", input: false, expected: true},

		// integer cases
		{name: "negate positive int", input: 1, expected: false},
		{name: "negate negative int", input: -1, expected: false},
		{name: "negate zero int", input: 0, expected: true},
		{name: "negate int64 positive", input: int64(42), expected: false},
		{name: "negate int64 zero", input: int64(0), expected: true},

		// float cases
		{name: "negate positive float", input: 3.14, expected: false},
		{name: "negate negative float", input: -3.14, expected: false},
		{name: "negate zero float", input: 0.0, expected: true},

		// string cases
		{name: "negate non-empty string", input: "hello", expected: false},
		{name: "negate empty string", input: "", expected: true},

		// slice cases
		{name: "negate non-empty slice", input: []interface{}{1, 2}, expected: false},
		{name: "negate empty slice", input: []interface{}{}, expected: true},

		// map cases
		{name: "negate non-empty map", input: map[string]interface{}{"k": "v"}, expected: false},
		{name: "negate empty map", input: map[string]interface{}{}, expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op := NegateOperator{}

			// Create a minimal evaluator
			ev := &graft.Evaluator{
				Tree: make(map[interface{}]interface{}),
			}

			// Create a literal expression
			args := []*graft.Expr{
				{Type: graft.Literal, Literal: tt.input},
			}

			resp, err := op.Run(ev, args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if resp.Type != graft.Replace {
				t.Errorf("expected Replace response type, got %v", resp.Type)
			}

			result, ok := resp.Value.(bool)
			if !ok {
				t.Fatalf("expected bool result, got %T", resp.Value)
			}

			if result != tt.expected {
				t.Errorf("negate(%v) = %v, expected %v", tt.input, result, tt.expected)
			}
		})
	}
}

// TestNegateOperatorArgCount tests that negate requires exactly one argument.
func TestNegateOperatorArgCount(t *testing.T) {
	op := NegateOperator{}
	ev := &graft.Evaluator{
		Tree: make(map[interface{}]interface{}),
	}

	// No arguments
	_, err := op.Run(ev, []*graft.Expr{})
	if err == nil {
		t.Error("expected error for no arguments")
	}

	// Too many arguments
	_, err = op.Run(ev, []*graft.Expr{
		{Type: graft.Literal, Literal: true},
		{Type: graft.Literal, Literal: false},
	})
	if err == nil {
		t.Error("expected error for too many arguments")
	}
}

// TestTypeAwareTernaryOperator tests the type-aware ternary operator.
func TestTypeAwareTernaryOperator(t *testing.T) {
	tests := []struct {
		name       string
		condition  interface{}
		trueValue  interface{}
		falseValue interface{}
		expected   interface{}
	}{
		// Boolean conditions
		{
			name:       "true condition returns true value",
			condition:  true,
			trueValue:  "yes",
			falseValue: "no",
			expected:   "yes",
		},
		{
			name:       "false condition returns false value",
			condition:  false,
			trueValue:  "yes",
			falseValue: "no",
			expected:   "no",
		},

		// Numeric conditions (truthy/falsy)
		{
			name:       "non-zero int condition returns true value",
			condition:  42,
			trueValue:  "non-zero",
			falseValue: "zero",
			expected:   "non-zero",
		},
		{
			name:       "zero int condition returns false value",
			condition:  0,
			trueValue:  "non-zero",
			falseValue: "zero",
			expected:   "zero",
		},
		{
			name:       "non-zero float condition returns true value",
			condition:  3.14,
			trueValue:  "non-zero",
			falseValue: "zero",
			expected:   "non-zero",
		},
		{
			name:       "zero float condition returns false value",
			condition:  0.0,
			trueValue:  "non-zero",
			falseValue: "zero",
			expected:   "zero",
		},

		// String conditions
		{
			name:       "non-empty string condition returns true value",
			condition:  "hello",
			trueValue:  "non-empty",
			falseValue: "empty",
			expected:   "non-empty",
		},
		{
			name:       "empty string condition returns false value",
			condition:  "",
			trueValue:  "non-empty",
			falseValue: "empty",
			expected:   "empty",
		},

		// Nil condition
		{
			name:       "nil condition returns false value",
			condition:  nil,
			trueValue:  "not-nil",
			falseValue: "nil",
			expected:   "nil",
		},

		// List conditions
		{
			name:       "non-empty list condition returns true value",
			condition:  []interface{}{1, 2, 3},
			trueValue:  "has-items",
			falseValue: "empty",
			expected:   "has-items",
		},
		{
			name:       "empty list condition returns false value",
			condition:  []interface{}{},
			trueValue:  "has-items",
			falseValue: "empty",
			expected:   "empty",
		},

		// Map conditions
		{
			name:       "non-empty map condition returns true value",
			condition:  map[string]interface{}{"key": "value"},
			trueValue:  "has-keys",
			falseValue: "empty",
			expected:   "has-keys",
		},
		{
			name:       "empty map condition returns false value",
			condition:  map[string]interface{}{},
			trueValue:  "has-keys",
			falseValue: "empty",
			expected:   "empty",
		},

		// Type preservation tests
		{
			name:       "preserves integer type in true branch",
			condition:  true,
			trueValue:  int64(42),
			falseValue: int64(0),
			expected:   int64(42),
		},
		{
			name:       "preserves integer type in false branch",
			condition:  false,
			trueValue:  int64(42),
			falseValue: int64(0),
			expected:   int64(0),
		},
		{
			name:       "preserves float type",
			condition:  true,
			trueValue:  3.14159,
			falseValue: 0.0,
			expected:   3.14159,
		},
		{
			name:       "preserves list type",
			condition:  true,
			trueValue:  []interface{}{"a", "b", "c"},
			falseValue: []interface{}{},
			expected:   []interface{}{"a", "b", "c"},
		},
		{
			name:       "preserves map type",
			condition:  true,
			trueValue:  map[string]interface{}{"key": "value"},
			falseValue: map[string]interface{}{},
			expected:   map[string]interface{}{"key": "value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op := TypeAwareTernaryOperator{}

			// Create a minimal evaluator
			ev := &graft.Evaluator{
				Tree: make(map[interface{}]interface{}),
			}

			// Create literal expressions for the arguments
			args := []*graft.Expr{
				{Type: graft.Literal, Literal: tt.condition},
				{Type: graft.Literal, Literal: tt.trueValue},
				{Type: graft.Literal, Literal: tt.falseValue},
			}

			resp, err := op.Run(ev, args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if resp.Type != graft.Replace {
				t.Errorf("expected Replace response type, got %v", resp.Type)
			}

			if !deepEqual(resp.Value, tt.expected) {
				t.Errorf("ternary(%v ? %v : %v) = %v (type %T), expected %v (type %T)",
					tt.condition, tt.trueValue, tt.falseValue,
					resp.Value, resp.Value, tt.expected, tt.expected)
			}
		})
	}
}

// TestTernaryOperatorArgCount tests that ternary requires exactly three arguments.
func TestTernaryOperatorArgCount(t *testing.T) {
	op := TypeAwareTernaryOperator{}
	ev := &graft.Evaluator{
		Tree: make(map[interface{}]interface{}),
	}

	// No arguments
	_, err := op.Run(ev, []*graft.Expr{})
	if err == nil {
		t.Error("expected error for no arguments")
	}

	// One argument
	_, err = op.Run(ev, []*graft.Expr{
		{Type: graft.Literal, Literal: true},
	})
	if err == nil {
		t.Error("expected error for one argument")
	}

	// Two arguments
	_, err = op.Run(ev, []*graft.Expr{
		{Type: graft.Literal, Literal: true},
		{Type: graft.Literal, Literal: "yes"},
	})
	if err == nil {
		t.Error("expected error for two arguments")
	}

	// Four arguments
	_, err = op.Run(ev, []*graft.Expr{
		{Type: graft.Literal, Literal: true},
		{Type: graft.Literal, Literal: "yes"},
		{Type: graft.Literal, Literal: "no"},
		{Type: graft.Literal, Literal: "extra"},
	})
	if err == nil {
		t.Error("expected error for four arguments")
	}
}

// TestTypeAwareNotOperator tests the type-aware NOT operator (!)
func TestTypeAwareNotOperator(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected bool
	}{
		// nil/null cases
		{name: "not nil", input: nil, expected: true},

		// boolean cases
		{name: "not true", input: true, expected: false},
		{name: "not false", input: false, expected: true},

		// integer cases
		{name: "not positive int", input: 1, expected: false},
		{name: "not zero int", input: 0, expected: true},

		// float cases
		{name: "not positive float", input: 1.5, expected: false},
		{name: "not zero float", input: 0.0, expected: true},

		// string cases
		{name: "not non-empty string", input: "hello", expected: false},
		{name: "not empty string", input: "", expected: true},

		// slice cases
		{name: "not non-empty slice", input: []interface{}{1}, expected: false},
		{name: "not empty slice", input: []interface{}{}, expected: true},

		// map cases
		{name: "not non-empty map", input: map[string]interface{}{"k": "v"}, expected: false},
		{name: "not empty map", input: map[string]interface{}{}, expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op := NewTypeAwareNotOperator()

			// Create a minimal evaluator
			ev := &graft.Evaluator{
				Tree: make(map[interface{}]interface{}),
			}

			// Create a literal expression
			args := []*graft.Expr{
				{Type: graft.Literal, Literal: tt.input},
			}

			resp, err := op.Run(ev, args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if resp.Type != graft.Replace {
				t.Errorf("expected Replace response type, got %v", resp.Type)
			}

			result, ok := resp.Value.(bool)
			if !ok {
				t.Fatalf("expected bool result, got %T", resp.Value)
			}

			if result != tt.expected {
				t.Errorf("!(%v) = %v, expected %v", tt.input, result, tt.expected)
			}
		})
	}
}

// TestTypeAwareAndOperator tests the type-aware AND operator (&&).
func TestTypeAwareAndOperator(t *testing.T) {
	tests := []struct {
		name     string
		left     interface{}
		right    interface{}
		expected bool
	}{
		// Boolean cases
		{name: "true && true", left: true, right: true, expected: true},
		{name: "true && false", left: true, right: false, expected: false},
		{name: "false && true", left: false, right: true, expected: false},
		{name: "false && false", left: false, right: false, expected: false},

		// Mixed truthy types
		{name: "1 && 2", left: 1, right: 2, expected: true},
		{name: "0 && 1", left: 0, right: 1, expected: false},
		{name: "string && int", left: "hello", right: 42, expected: true},
		{name: "empty string && true", left: "", right: true, expected: false},

		// Short-circuit - right should not matter if left is falsy
		{name: "nil && anything", left: nil, right: "anything", expected: false},
		{name: "0 && anything", left: 0, right: "anything", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op := NewTypeAwareAndOperator()

			// Create a minimal evaluator
			ev := &graft.Evaluator{
				Tree: make(map[interface{}]interface{}),
			}

			// Create literal expressions
			args := []*graft.Expr{
				{Type: graft.Literal, Literal: tt.left},
				{Type: graft.Literal, Literal: tt.right},
			}

			resp, err := op.Run(ev, args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if resp.Type != graft.Replace {
				t.Errorf("expected Replace response type, got %v", resp.Type)
			}

			result, ok := resp.Value.(bool)
			if !ok {
				t.Fatalf("expected bool result, got %T", resp.Value)
			}

			if result != tt.expected {
				t.Errorf("(%v && %v) = %v, expected %v", tt.left, tt.right, result, tt.expected)
			}
		})
	}
}

// TestBooleanLogicalOps tests the BooleanLogicalOps helper functions.
func TestBooleanLogicalOps(t *testing.T) {
	handler := NewBooleanTypeHandler()
	ops := NewBooleanLogicalOps(handler)

	t.Run("And operations", func(t *testing.T) {
		tests := []struct {
			a, b     interface{}
			expected bool
		}{
			{true, true, true},
			{true, false, false},
			{1, 1, true},
			{1, 0, false},
			{"hello", "world", true},
			{"", "world", false},
		}

		for _, tt := range tests {
			result, err := ops.And(tt.a, tt.b)
			if err != nil {
				t.Errorf("And(%v, %v) error: %v", tt.a, tt.b, err)
			}
			if result != tt.expected {
				t.Errorf("And(%v, %v) = %v, expected %v", tt.a, tt.b, result, tt.expected)
			}
		}
	})

	t.Run("Or operations", func(t *testing.T) {
		tests := []struct {
			a, b     interface{}
			expected bool
		}{
			{true, true, true},
			{true, false, true},
			{false, false, false},
			{1, 0, true},
			{0, 0, false},
			{"", "", false},
			{"hello", "", true},
		}

		for _, tt := range tests {
			result, err := ops.Or(tt.a, tt.b)
			if err != nil {
				t.Errorf("Or(%v, %v) error: %v", tt.a, tt.b, err)
			}
			if result != tt.expected {
				t.Errorf("Or(%v, %v) = %v, expected %v", tt.a, tt.b, result, tt.expected)
			}
		}
	})

	t.Run("Not operations", func(t *testing.T) {
		tests := []struct {
			a        interface{}
			expected bool
		}{
			{true, false},
			{false, true},
			{1, false},
			{0, true},
			{"hello", false},
			{"", true},
			{nil, true},
		}

		for _, tt := range tests {
			result, err := ops.Not(tt.a)
			if err != nil {
				t.Errorf("Not(%v) error: %v", tt.a, err)
			}
			if result != tt.expected {
				t.Errorf("Not(%v) = %v, expected %v", tt.a, result, tt.expected)
			}
		}
	})

	t.Run("Xor operations", func(t *testing.T) {
		tests := []struct {
			a, b     interface{}
			expected bool
		}{
			{true, true, false},
			{true, false, true},
			{false, true, true},
			{false, false, false},
			{1, 0, true},
			{1, 1, false},
			{0, 0, false},
		}

		for _, tt := range tests {
			result, err := ops.Xor(tt.a, tt.b)
			if err != nil {
				t.Errorf("Xor(%v, %v) error: %v", tt.a, tt.b, err)
			}
			if result != tt.expected {
				t.Errorf("Xor(%v, %v) = %v, expected %v", tt.a, tt.b, result, tt.expected)
			}
		}
	})
}

// TestOperatorPhases tests that operators are in the correct phase.
func TestOperatorPhases(t *testing.T) {
	tests := []struct {
		name     string
		op       graft.Operator
		expected graft.OperatorPhase
	}{
		{name: "NegateOperator", op: NegateOperator{}, expected: graft.EvalPhase},
		{name: "TypeAwareTernaryOperator", op: TypeAwareTernaryOperator{}, expected: graft.EvalPhase},
		{name: "TypeAwareNotOperator", op: NewTypeAwareNotOperator(), expected: graft.EvalPhase},
		{name: "TypeAwareAndOperator", op: NewTypeAwareAndOperator(), expected: graft.EvalPhase},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.op.Phase() != tt.expected {
				t.Errorf("%s.Phase() = %v, expected %v", tt.name, tt.op.Phase(), tt.expected)
			}
		})
	}
}

// TestOperatorSetup tests that operators setup correctly.
func TestOperatorSetup(t *testing.T) {
	operators := []graft.Operator{
		NegateOperator{},
		TypeAwareTernaryOperator{},
		NewTypeAwareNotOperator(),
		NewTypeAwareAndOperator(),
	}

	for _, op := range operators {
		if err := op.Setup(); err != nil {
			t.Errorf("Setup() failed for %T: %v", op, err)
		}
	}
}

// deepEqual performs deep equality comparison for test assertions.
//
//nolint:gocyclo // deep comparison handles many type combinations
func deepEqual(a, b interface{}) bool {
	// Handle nil cases
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	// Handle slices
	if aSlice, ok := a.([]interface{}); ok {
		if bSlice, ok := b.([]interface{}); ok {
			if len(aSlice) != len(bSlice) {
				return false
			}
			for i := range aSlice {
				if !deepEqual(aSlice[i], bSlice[i]) {
					return false
				}
			}
			return true
		}
		return false
	}

	// Handle maps
	if aMap, ok := a.(map[string]interface{}); ok {
		if bMap, ok := b.(map[string]interface{}); ok {
			if len(aMap) != len(bMap) {
				return false
			}
			for k, v := range aMap {
				if bv, exists := bMap[k]; !exists || !deepEqual(v, bv) {
					return false
				}
			}
			return true
		}
		return false
	}

	if aMap, ok := a.(map[interface{}]interface{}); ok {
		if bMap, ok := b.(map[interface{}]interface{}); ok {
			if len(aMap) != len(bMap) {
				return false
			}
			for k, v := range aMap {
				if bv, exists := bMap[k]; !exists || !deepEqual(v, bv) {
					return false
				}
			}
			return true
		}
		return false
	}

	// Default comparison
	return a == b
}

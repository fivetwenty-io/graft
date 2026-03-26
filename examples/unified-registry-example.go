// Package main demonstrates the unified operator registry
package main

import (
	"fmt"

	"github.com/fivetwenty-io/graft/pkg/graft"
	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// CustomOperator demonstrates creating a new operator.
type CustomOperator struct {
	name string
}

func (op CustomOperator) Setup() error {
	fmt.Printf("Setting up %s operator\n", op.name)
	return nil
}

func (op CustomOperator) Phase() graft.OperatorPhase {
	return graft.EvalPhase
}

func (op CustomOperator) Dependencies(ev *graft.Evaluator, args []*graft.Expr, locs, auto []*tree.Cursor) []*tree.Cursor {
	// Return any dependencies
	return auto
}

func (op CustomOperator) Run(ev *graft.Evaluator, args []*graft.Expr) (*graft.Response, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s requires exactly 1 argument", op.name)
	}

	// Simple echo operator
	return &graft.Response{
		Type:  graft.Replace,
		Value: fmt.Sprintf("Custom[%v]", args[0].Literal),
	}, nil
}

func main() {
	fmt.Println("=== Unified Operator Registry Example ===")

	// 1. Register a custom operator the new way
	fmt.Println("1. Registering custom operator...")
	customOp := CustomOperator{name: "custom"}
	err := graft.RegisterUnifiedOperator("custom", customOp)
	if err != nil {
		fmt.Printf("Failed to register: %v\n", err)
	} else {
		fmt.Println("Successfully registered 'custom' operator")
	}

	// 2. Query the registry
	fmt.Println("\n2. Querying registry...")

	// Check if registered
	if graft.UnifiedRegistry.IsRegistered("custom") {
		fmt.Println("✓ 'custom' operator is registered")
	}

	// Get metadata
	if meta, exists := graft.UnifiedRegistry.GetMetadata("custom"); exists {
		fmt.Printf("✓ Metadata: Phase=%v, Args=%d-%d\n",
			meta.Phase, meta.MinArgs, meta.MaxArgs)
	}

	// Get implementation
	if impl, exists := graft.UnifiedRegistry.GetImplementation("custom"); exists {
		fmt.Printf("✓ Implementation type: %T\n", impl)
	}

	// 3. List operators by phase
	fmt.Println("\n3. Operators by phase...")
	phases := map[graft.OperatorPhase]string{
		graft.MergePhase: "Merge",
		graft.EvalPhase:  "Eval",
		graft.ParamPhase: "Param",
	}

	for phase, name := range phases {
		ops := graft.UnifiedRegistry.GetByPhase(phase)
		fmt.Printf("%s phase: %d operators\n", name, len(ops))
	}

	// 4. Validate arguments
	fmt.Println("\n4. Argument validation...")
	testOps := []struct {
		name string
		args int
	}{
		{"custom", 1}, // Valid
		{"custom", 2}, // Invalid
		{"concat", 5}, // Valid (unlimited)
		{"grab", 0},   // Invalid
	}

	for _, test := range testOps {
		err := graft.UnifiedRegistry.ValidateArgs(test.name, test.args)
		if err == nil {
			fmt.Printf("✓ %s with %d args: Valid\n", test.name, test.args)
		} else {
			fmt.Printf("✗ %s with %d args: %v\n", test.name, test.args, err)
		}
	}

	// 5. Advanced usage - clone registry
	fmt.Println("\n5. Advanced features...")
	cloned := graft.UnifiedRegistry.Clone()
	fmt.Printf("Cloned registry has %d operators\n", cloned.Count())

	// 6. Direct usage example
	fmt.Println("\n6. Using the operator...")
	if op, exists := graft.UnifiedRegistry.GetImplementation("custom"); exists {
		// Create a simple evaluator context
		ev := &graft.Evaluator{
			Tree: map[string]interface{}{},
		}

		// Create arguments
		args := []*graft.Expr{
			{Type: graft.Literal, Literal: "Hello, World!"},
		}

		// Run the operator
		result, err := op.Run(ev, args)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
		} else {
			fmt.Printf("Result: %v\n", result.Value)
		}
	}

	fmt.Println("\n=== Example Complete ===")
}

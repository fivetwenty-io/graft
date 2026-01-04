package operators

import (
	"github.com/fivetwenty-io/graft/pkg/graft"
)

// RegisterOp registers an operator in the main graft package registry.
func RegisterOp(name string, op graft.Operator) {
	// Register in legacy registry for backward compatibility
	graft.OpRegistry[name] = op

	// Also register in unified registry
	if err := graft.RegisterUnifiedOperator(name, op); err != nil {
		// Log error but don't panic - this maintains backward compatibility
		graft.DEBUG("Warning: Failed to register %s in unified registry: %v", name, err)
	}
}

// SetupOperators initializes all operators for a given phase.
func SetupOperators(phase graft.OperatorPhase) error {
	errors := graft.MultiError{Errors: []error{}}
	for _, op := range graft.OpRegistry {
		if op.Phase() == phase {
			if err := op.Setup(); err != nil {
				errors.Append(err)
			}
		}
	}
	if len(errors.Errors) > 0 {
		return errors
	}
	return nil
}

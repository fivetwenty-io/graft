package graft

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/fivetwenty-io/graft/log"
	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

//nolint:gochecknoinits // Operator registry initialization must happen at package load time
func init() {
	// Initialize operator registry if not already done
	if OpRegistry == nil {
		OpRegistry = make(map[string]Operator)
	}

	// Migrate legacy registries to unified registry on first init
	// This will be called after all operator init() functions have run
	// since package init order ensures this runs last
	if UnifiedRegistry.Count() == 0 {
		// First migrate from legacy registries
		if err := MigrateFromLegacyRegistries(); err != nil {
			log.DEBUG("Warning: Failed to migrate to unified registry: %v", err)
		}

		// Then ensure all operators are registered with complete metadata
		if err := PopulateCompleteRegistry(); err != nil {
			log.DEBUG("Warning: Failed to populate complete registry: %v", err)
		}
	}

	log.DEBUG("Operators initialized")
}

// ParseOpcallFunc is the registered parser function.
// The operators package sets this to its actual implementation.
var ParseOpcallFunc func(phase OperatorPhase, src string) (*Opcall, error)

// ParseOpcall parses an operator call expression.
// It delegates to the registered parser function.
func ParseOpcall(phase OperatorPhase, src string) (*Opcall, error) {
	if ParseOpcallFunc != nil {
		return ParseOpcallFunc(phase, src)
	}
	return nil, fmt.Errorf("parser not initialized - operators package must be imported")
}

// RegisterOp is a helper function to register operators.
//
// Deprecated: Use RegisterUnifiedOperator instead for new code.
func RegisterOp(name string, op Operator) {
	// Register in legacy registry for backward compatibility
	OpRegistry[name] = op

	// Also register in unified registry
	if err := RegisterUnifiedOperator(name, op); err != nil {
		log.DEBUG("Warning: Failed to register %s in unified registry: %v", name, err)
	}
}

// DEBUG is a helper function for debug logging.
//
//nolint:goprintffuncname // DEBUG is an established name used throughout the codebase
func DEBUG(format string, args ...interface{}) {
	log.DEBUG(format, args...)
}

// TRACE is a helper function for trace logging.
//
//nolint:goprintffuncname // TRACE is an established name used throughout the codebase
func TRACE(format string, args ...interface{}) {
	log.TRACE(format, args...)
}

// SetupOperators initializes operators for a given phase.
func SetupOperators(phase OperatorPhase) error {
	// Operators are now registered through the engine or globally through init()
	// This function is kept for backward compatibility
	return nil
}

// OperatorFor returns the operator for the given name.
func OperatorFor(name string) Operator {
	// First check unified registry
	if op, exists := UnifiedRegistry.GetImplementation(name); exists {
		return op
	}

	// Fall back to legacy registry for backward compatibility
	if op, exists := OpRegistry[name]; exists {
		return op
	}

	// Return a NullOperator for unknown operators
	return NullOperator{Missing: name}
}

// NullOperator is a placeholder operator for unknown operations.
type NullOperator struct {
	Missing string
}

// Setup initializes the operator.
func (NullOperator) Setup() error {
	return nil
}

// Phase returns which phase this operator should run in.
func (NullOperator) Phase() OperatorPhase {
	return EvalPhase
}

// Dependencies returns what keys the operator depends on.
func (NullOperator) Dependencies(ev *Evaluator, args []*Expr, locs, auto []*tree.Cursor) []*tree.Cursor {
	return nil
}

// Run executes the operator.
func (n NullOperator) Run(ev *Evaluator, args []*Expr) (*Response, error) {
	// For unknown operators, return the original operator call string unchanged
	// This allows the template to be processed again later or remain as-is

	// Reconstruct the original operator call
	var argStrings []string
	for _, arg := range args {
		if arg.Type == Literal {
			if s, ok := arg.Literal.(string); ok {
				argStrings = append(argStrings, fmt.Sprintf("%q", s))
			} else {
				argStrings = append(argStrings, fmt.Sprintf("%v", arg.Literal))
			}
		} else {
			// For non-literal args, use a placeholder
			argStrings = append(argStrings, "...")
		}
	}

	var argsStr string
	if len(argStrings) > 0 {
		argsStr = " " + argStrings[0]
		for _, arg := range argStrings[1:] {
			argsStr += " " + arg
		}
	}

	originalCall := fmt.Sprintf("(( %s%s ))", n.Missing, argsStr)

	return &Response{
		Type:  Replace,
		Value: originalCall,
	}, nil
}

// NewOpcall creates a new operator call.
func NewOpcall(op Operator, args []*Expr, src string) *Opcall {
	return &Opcall{
		op:   op,
		args: args,
		src:  src,
	}
}

// DefaultKeyGenerator returns a key generator function
// This seems to be used for generating unique keys, possibly for caching.
func DefaultKeyGenerator() func() (string, error) {
	counter := 0
	return func() (string, error) {
		counter++
		return fmt.Sprintf("key-%d", counter), nil
	}
}

// isPruneOperator checks if a value represents a prune operator.
func isPruneOperator(val interface{}) bool {
	if str, ok := val.(string); ok {
		// Match patterns like "(( prune ))" with optional whitespace
		matched, _ := regexp.MatchString(`^\s*\(\(\s*prune\s*\)\)\s*$`, str)
		if matched {
			DEBUG("isPruneOperator: detected prune operator: %q", str)
		}
		return matched
	}

	// Also check for Opcall structures that represent prune operations
	if opcall, ok := val.(*Opcall); ok {
		if opcall != nil && opcall.op != nil {
			// Check if this is a prune operator
			if _, isPrune := opcall.op.(interface{ String() string }); isPrune {
				// This is a more complex check - for now, just check the source
				if strings.Contains(opcall.src, "prune") {
					DEBUG("isPruneOperator: detected prune opcall: %v", opcall.src)
					return true
				}
			}
		}
	}

	DEBUG("isPruneOperator: not a prune operator: %T %v", val, val)
	return false
}

// Merge merges two data structures.
func Merge(dst, src interface{}) error {
	// Deep merge implementation for maps
	dstMap, dstOk := dst.(map[string]interface{})
	srcMap, srcOk := src.(map[string]interface{})

	if !dstOk || !srcOk {
		return fmt.Errorf("Merge: both arguments must be maps")
	}

	// Deep merge all keys from src to dst
	for k, srcVal := range srcMap {
		if dstVal, exists := dstMap[k]; exists {
			// If destination has a prune operator, preserve it (prune takes precedence)
			if isPruneOperator(dstVal) {
				DEBUG("Merge: preserving prune operator at key %v", k)
				continue
			}

			// If both are maps, merge recursively
			if dstSubMap, dstIsMap := dstVal.(map[string]interface{}); dstIsMap {
				if srcSubMap, srcIsMap := srcVal.(map[string]interface{}); srcIsMap {
					err := Merge(dstSubMap, srcSubMap)
					if err != nil {
						return err
					}
					continue
				}
			}
		}
		// Otherwise just copy the value
		dstMap[k] = srcVal
	}

	return nil
}

// DebugOn returns true if debugging is enabled.
func DebugOn() bool {
	// Check environment variable or global flag
	return false
}

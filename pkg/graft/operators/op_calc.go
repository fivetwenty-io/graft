package operators

import (
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/Knetic/govaluate"

	"github.com/fivetwenty-io/graft/pkg/graft"
	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// CalcOperator is invoked with (( calc <expression> )).
type CalcOperator struct{}

// Setup initializes the operator.
func (CalcOperator) Setup() error {
	return nil
}

// Phase returns the evaluation phase.
func (CalcOperator) Phase() graft.OperatorPhase {
	return graft.EvalPhase
}

// Dependencies returns dependencies found in the expression.
func (CalcOperator) Dependencies(ev *graft.Evaluator, args []*graft.Expr, _ []*tree.Cursor, auto []*tree.Cursor) []*tree.Cursor {
	DEBUG("Calculating dependencies for (( calc ... ))")
	deps := []*tree.Cursor{}

	// Check dependencies in all arguments
	for _, arg := range args {
		deps = append(deps, arg.Dependencies(ev, nil)...)

		// Also check for references in literal strings
		if arg.Type == Literal && arg.Literal != nil {
			if str, ok := arg.Literal.(string); ok {
				cursors := searchForCursors(str)
				deps = append(deps, cursors...)
			}
		}
	}

	return append(auto, deps...)
}

// Run executes the calc operation.
//
//nolint:gocyclo // calc operator handles expression parsing with path substitution
func (CalcOperator) Run(ev *graft.Evaluator, args []*graft.Expr) (*graft.Response, error) {
	DEBUG("running (( calc ... )) operation at $.%s", ev.Here)
	defer DEBUG("done with (( calc ... )) operation at $%s\n", ev.Here)

	// The calc operator expects one argument containing the expression to be evaluated
	if len(args) != 1 {
		return nil, fmt.Errorf("calc operator only expects one argument containing the expression")
	}

	// Resolve the argument using ResolveOperatorArgument to support nested expressions
	val, err := ResolveOperatorArgument(ev, args[0])
	if err != nil {
		return nil, err
	}

	// Convert the resolved value to a string for processing
	var input string
	switch v := val.(type) {
	case string:
		input = v
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		input = fmt.Sprintf("%d", v)
	case float32, float64:
		input = fmt.Sprintf("%f", v)
	default:
		return nil, fmt.Errorf("calc operator argument must resolve to a string or number, got %T", val)
	}

	// Check if the expression starts with an operator (*, /, +, -, ^, %)
	// If so, prepend the current value (not path reference)
	trimmedInput := strings.TrimSpace(input)
	if trimmedInput != "" {
		firstChar := trimmedInput[0]
		if firstChar == '*' || firstChar == '/' || firstChar == '+' || firstChar == '-' || firstChar == '^' || firstChar == '%' {
			// Get current value or default to 0
			currentVal, err := ev.Here.Resolve(ev.Tree)
			var currentNumStr string

			if err != nil || currentVal == nil {
				// Default to 0 if current value doesn't exist
				currentNumStr = "0"
				DEBUG("  current value not found, defaulting to 0")
			} else {
				// Convert current value to string
				switch v := currentVal.(type) {
				case int, int8, int16, int32, int64:
					currentNumStr = fmt.Sprintf("%d", v)
				case uint, uint8, uint16, uint32, uint64:
					currentNumStr = fmt.Sprintf("%d", v)
				case float32, float64:
					currentNumStr = fmt.Sprintf("%f", v)
				default:
					// If current value is not numeric, default to 0
					currentNumStr = "0"
					DEBUG("  current value is not numeric (type %T), defaulting to 0", v)
				}
			}

			// Prepend the current value
			input = currentNumStr + " " + input
			DEBUG("  prepended current value: %s", currentNumStr)
		}
	}

	// Replace all Graft references with the respective value
	DEBUG("  input expression: %s", input)
	processed, replaceError := replaceCalcReferences(ev, input)
	if replaceError != nil {
		return nil, replaceError
	}

	// Once all Graft references (variables) are replaced, try to read the expression
	DEBUG("  processed expression: %s", processed)
	expression, expressionError := govaluate.NewEvaluableExpressionWithFunctions(processed, supportedCalcFunctions())
	if expressionError != nil {
		return nil, expressionError
	}

	// Check that there are no named variables in the expression that we cannot evaluate/insert
	if len(expression.Vars()) > 0 {
		return nil, fmt.Errorf("calc operator does not support named variables in expression: %s", strings.Join(expression.Vars(), ", "))
	}

	// Evaluate without a variables list (named variables are not supported)
	result, evaluateError := expression.Evaluate(nil)
	if evaluateError != nil {
		return nil, evaluateError
	}

	// Convert float results to int if they have no fractional part
	if resultFloat, ok := result.(float64); ok {
		resultInt := int64(resultFloat)
		if float64(resultInt) == resultFloat {
			result = resultInt
		}
	}

	DEBUG("  evaluated result: %v", result)
	return &graft.Response{
		Type:  graft.Replace,
		Value: result,
	}, nil
}

// searchForCursors finds path references in the input string.
func searchForCursors(input string) []*tree.Cursor {
	result := []*tree.Cursor{}

	// Search for sub-strings that contain the path separator dot character
	// https://regex101.com/r/TIEyak/1
	re := regexp.MustCompile(`(\w+|-)\.(\w+|-|\.)+`)
	candidates := re.FindAllString(input, -1)
	DEBUG("    strings found containing the path separator: %v", strings.Join(candidates, ", "))

	// If it is a path, it can be parsed (parse errors will be ignored)
	for _, candidate := range candidates {
		// Skip floats
		if _, err := strconv.ParseFloat(candidate, 64); err == nil {
			continue
		}

		// Skip ints
		if _, err := strconv.ParseInt(candidate, 10, 64); err == nil {
			continue
		}

		if cursor, parseError := tree.ParseCursor(candidate); parseError == nil {
			result = append(result, cursor)
		}
	}

	DEBUG("    result cursors: %v", result)
	return result
}

// replaceCalcReferences replaces path references with their values.
func replaceCalcReferences(ev *graft.Evaluator, input string) (string, error) {
	cursors := searchForCursors(input)

	for _, cursor := range cursors {
		value, resolveError := cursor.Resolve(ev.Tree)
		if resolveError != nil {
			return "", resolveError
		}

		path := cursor.String()
		DEBUG("    path/value: %s=%v", path, value)

		switch value.(type) {
		case int, uint8, uint16, uint32, uint64, int8, int16, int32, int64:
			input = strings.ReplaceAll(input, path, fmt.Sprintf("%d", value))

		case float32, float64:
			input = strings.ReplaceAll(input, path, fmt.Sprintf("%f", value))

		case nil:
			return "", fmt.Errorf("path %s references a nil value, which cannot be used in calculations", path)

		default:
			return "", fmt.Errorf("path %s is of type %s, which cannot be used in calculations", path, reflect.TypeOf(value).Kind())
		}
	}

	return input, nil
}

// supportedCalcFunctions returns the built-in functions available in calc expressions.
//
//nolint:gocyclo // function map initialization with type validation
func supportedCalcFunctions() map[string]govaluate.ExpressionFunction {
	return map[string]govaluate.ExpressionFunction{
		"min": func(args ...interface{}) (interface{}, error) {
			if len(args) == 2 {
				a, aOK := args[0].(float64)
				b, bOK := args[1].(float64)
				if aOK && bOK {
					return math.Min(a, b), nil
				}
			}
			return -1, fmt.Errorf("min function expects two arguments of type float64")
		},

		"max": func(args ...interface{}) (interface{}, error) {
			if len(args) == 2 {
				a, aOK := args[0].(float64)
				b, bOK := args[1].(float64)
				if aOK && bOK {
					return math.Max(a, b), nil
				}
			}
			return -1, fmt.Errorf("max function expects two arguments of type float64")
		},

		"mod": func(args ...interface{}) (interface{}, error) {
			if len(args) == 2 {
				a, aOK := args[0].(float64)
				b, bOK := args[1].(float64)
				if aOK && bOK {
					return math.Mod(a, b), nil
				}
			}
			return -1, fmt.Errorf("mod function expects two arguments of type float64")
		},

		"pow": func(args ...interface{}) (interface{}, error) {
			if len(args) == 2 {
				a, aOK := args[0].(float64)
				b, bOK := args[1].(float64)
				if aOK && bOK {
					return math.Pow(a, b), nil
				}
			}
			return -1, fmt.Errorf("pow function expects two arguments of type float64")
		},

		"sqrt": func(args ...interface{}) (interface{}, error) {
			if len(args) == 1 {
				if a, ok := args[0].(float64); ok {
					return math.Sqrt(a), nil
				}
			}
			return -1, fmt.Errorf("sqrt function expects one argument of type float64")
		},

		"floor": func(args ...interface{}) (interface{}, error) {
			if len(args) == 1 {
				if a, ok := args[0].(float64); ok {
					return math.Floor(a), nil
				}
			}
			return -1, fmt.Errorf("floor function expects one argument of type float64")
		},

		"ceil": func(args ...interface{}) (interface{}, error) {
			if len(args) == 1 {
				if a, ok := args[0].(float64); ok {
					return math.Ceil(a), nil
				}
			}
			return -1, fmt.Errorf("ceil function expects one argument of type float64")
		},
	}
}

//nolint:gochecknoinits // Operator registration must happen at package load time
func init() {
	RegisterOp("calc", CalcOperator{})
}

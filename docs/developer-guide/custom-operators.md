# Creating Custom Operators

Graft's operator system is extensible, allowing you to create custom operators for domain-specific functionality. This guide covers the operator interface, registration, and best practices.

## Operator Interface

All operators implement the `Operator` interface:

```go
type Operator interface {
    // Evaluate executes the operator with given context and arguments
    Evaluate(ctx EvalContext, args []interface{}) (interface{}, error)

    // Info returns metadata about the operator
    Info() OperatorInfo
}

type OperatorInfo struct {
    Name        string   // Operator name (e.g., "concat")
    Description string   // Human-readable description
    MinArgs     int      // Minimum required arguments
    MaxArgs     int      // Maximum arguments (-1 = unlimited)
    ArgTypes    []string // Expected argument types for documentation
    Returns     string   // Return type description
    Examples    []string // Usage examples
    Category    string   // Operator category (e.g., "string", "math")
}

type EvalContext interface {
    // Document access
    Document() Document
    Path() string

    // Reference resolution
    Resolve(path string) (interface{}, error)

    // Sub-expression evaluation
    Evaluate(expr interface{}) (interface{}, error)

    // Context and cancellation
    Context() context.Context

    // Engine access (for advanced operators)
    Engine() Engine
}
```

## Creating a Simple Operator

### Using OperatorFunc

For simple operators, use the `OperatorFunc` helper:

```go
// Operator that returns the current timestamp
timestampOp := graft.OperatorFunc(
    func(ctx graft.EvalContext, args []interface{}) (interface{}, error) {
        return time.Now().Unix(), nil
    },
)

engine.RegisterOperator("timestamp", timestampOp)
```

**Usage:**

```yaml
created_at: (( timestamp ))
```

### Full Operator Implementation

For operators with metadata and validation:

```go
type EnvOperator struct{}

func (o *EnvOperator) Evaluate(ctx graft.EvalContext, args []interface{}) (interface{}, error) {
    if len(args) != 1 {
        return nil, fmt.Errorf("env requires exactly 1 argument, got %d", len(args))
    }

    name, ok := args[0].(string)
    if !ok {
        return nil, fmt.Errorf("env argument must be string, got %T", args[0])
    }

    value := os.Getenv(name)
    if value == "" {
        // Check if second arg provided as default
        if len(args) > 1 {
            return args[1], nil
        }
    }

    return value, nil
}

func (o *EnvOperator) Info() graft.OperatorInfo {
    return graft.OperatorInfo{
        Name:        "env",
        Description: "Read environment variable value",
        MinArgs:     1,
        MaxArgs:     2,
        ArgTypes:    []string{"string", "any"},
        Returns:     "string",
        Examples: []string{
            `(( env "HOME" ))`,
            `(( env "OPTIONAL_VAR" || "default" ))`,
        },
        Category: "environment",
    }
}
```

## Registering Operators

### At Engine Creation

```go
engine, _ := graft.NewEngine(
    graft.WithCustomOperator("env", &EnvOperator{}),
    graft.WithCustomOperator("uuid", &UUIDOperator{}),
)
```

### At Runtime

```go
engine, _ := graft.NewEngine()

// Register single operator
err := engine.RegisterOperator("env", &EnvOperator{})

// Check if operator exists
if op, exists := engine.GetOperator("env"); exists {
    fmt.Println("env operator registered:", op.Info().Description)
}

// List all operators
for _, info := range engine.ListOperators() {
    fmt.Printf("%s: %s\n", info.Name, info.Description)
}

// Unregister operator
err = engine.UnregisterOperator("env")
```

## Argument Handling

### Accessing Arguments

```go
func (o *MyOperator) Evaluate(ctx graft.EvalContext, args []interface{}) (interface{}, error) {
    // Arguments are already evaluated before being passed
    // They can be any Go type: string, int, float64, bool, []interface{}, map[string]interface{}

    // Type assertion
    if str, ok := args[0].(string); ok {
        // Handle string argument
    }

    // Handle numeric types (YAML numbers can be int or float64)
    switch v := args[0].(type) {
    case int:
        // Handle int
    case int64:
        // Handle int64
    case float64:
        // Handle float64
    }

    return result, nil
}
```

### Variadic Arguments

```go
type ConcatOperator struct{}

func (o *ConcatOperator) Evaluate(ctx graft.EvalContext, args []interface{}) (interface{}, error) {
    var result strings.Builder

    for _, arg := range args {
        result.WriteString(fmt.Sprintf("%v", arg))
    }

    return result.String(), nil
}

func (o *ConcatOperator) Info() graft.OperatorInfo {
    return graft.OperatorInfo{
        Name:    "concat",
        MinArgs: 1,
        MaxArgs: -1, // Unlimited
    }
}
```

## Using the Evaluation Context

### Resolving References

```go
func (o *MyOperator) Evaluate(ctx graft.EvalContext, args []interface{}) (interface{}, error) {
    // Resolve a path in the document
    value, err := ctx.Resolve("database.host")
    if err != nil {
        return nil, err
    }

    // Get current document
    doc := ctx.Document()

    // Get the current path being evaluated
    currentPath := ctx.Path()

    return value, nil
}
```

### Evaluating Sub-expressions

```go
func (o *ConditionalOperator) Evaluate(ctx graft.EvalContext, args []interface{}) (interface{}, error) {
    // First arg is condition, evaluate it
    condition, err := ctx.Evaluate(args[0])
    if err != nil {
        return nil, err
    }

    // Convert to bool
    cond, _ := toBool(condition)

    if cond {
        return ctx.Evaluate(args[1]) // Then branch
    }
    return ctx.Evaluate(args[2]) // Else branch
}
```

### Context and Cancellation

```go
func (o *SlowOperator) Evaluate(ctx graft.EvalContext, args []interface{}) (interface{}, error) {
    // Check context for cancellation
    select {
    case <-ctx.Context().Done():
        return nil, ctx.Context().Err()
    default:
    }

    // Long-running operation with periodic cancellation checks
    for i := 0; i < 1000; i++ {
        select {
        case <-ctx.Context().Done():
            return nil, ctx.Context().Err()
        default:
            // Continue processing
        }
    }

    return result, nil
}
```

## Error Handling

### Creating Descriptive Errors

```go
func (o *MyOperator) Evaluate(ctx graft.EvalContext, args []interface{}) (interface{}, error) {
    // Validation error
    if len(args) < 2 {
        return nil, &graft.EvaluationError{
            Operator: "myop",
            Path:     ctx.Path(),
            Message:  fmt.Sprintf("requires 2 arguments, got %d", len(args)),
            Hint:     "Usage: (( myop arg1 arg2 ))",
        }
    }

    // Type error
    str, ok := args[0].(string)
    if !ok {
        return nil, &graft.EvaluationError{
            Operator:  "myop",
            Path:      ctx.Path(),
            Message:   fmt.Sprintf("first argument must be string, got %T", args[0]),
            Arguments: args,
        }
    }

    return result, nil
}
```

### Error Types

```go
// Evaluation errors
&graft.EvaluationError{
    Operator:  "myop",
    Path:      "/database/host",
    Message:   "description of error",
    Arguments: []interface{}{arg1, arg2},
    Cause:     underlyingErr,
    Hint:      "helpful suggestion",
}

// Not found errors
&graft.NotFoundError{
    Path: "some.missing.path",
}

// Type mismatch errors
&graft.TypeMismatchError{
    Expected: "string",
    Actual:   "int",
    Path:     ctx.Path(),
}
```

## Advanced Patterns

### Stateful Operators

For operators that maintain state (use with caution regarding thread safety):

```go
type CounterOperator struct {
    mu    sync.Mutex
    count int
}

func (o *CounterOperator) Evaluate(ctx graft.EvalContext, args []interface{}) (interface{}, error) {
    o.mu.Lock()
    defer o.mu.Unlock()

    o.count++
    return o.count, nil
}
```

### Operators with Dependencies

```go
type HTTPOperator struct {
    client *http.Client
}

func NewHTTPOperator(timeout time.Duration) *HTTPOperator {
    return &HTTPOperator{
        client: &http.Client{Timeout: timeout},
    }
}

func (o *HTTPOperator) Evaluate(ctx graft.EvalContext, args []interface{}) (interface{}, error) {
    url := args[0].(string)

    req, _ := http.NewRequestWithContext(ctx.Context(), "GET", url, nil)
    resp, err := o.client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    body, _ := io.ReadAll(resp.Body)
    return string(body), nil
}

// Registration
httpOp := NewHTTPOperator(30 * time.Second)
engine.RegisterOperator("http", httpOp)
```

### Caching Operators

```go
type CachedVaultOperator struct {
    cache sync.Map
    ttl   time.Duration
}

type cacheEntry struct {
    value     interface{}
    expiresAt time.Time
}

func (o *CachedVaultOperator) Evaluate(ctx graft.EvalContext, args []interface{}) (interface{}, error) {
    key := args[0].(string)

    // Check cache
    if entry, ok := o.cache.Load(key); ok {
        ce := entry.(*cacheEntry)
        if time.Now().Before(ce.expiresAt) {
            return ce.value, nil
        }
    }

    // Fetch and cache
    value, err := fetchFromVault(ctx.Context(), key)
    if err != nil {
        return nil, err
    }

    o.cache.Store(key, &cacheEntry{
        value:     value,
        expiresAt: time.Now().Add(o.ttl),
    })

    return value, nil
}
```

## Testing Operators

### Unit Testing

```go
func TestEnvOperator(t *testing.T) {
    // Set test environment
    os.Setenv("TEST_VAR", "test_value")
    defer os.Unsetenv("TEST_VAR")

    op := &EnvOperator{}

    // Create mock context
    ctx := graft.NewMockEvalContext()

    // Test evaluation
    result, err := op.Evaluate(ctx, []interface{}{"TEST_VAR"})

    assert.NoError(t, err)
    assert.Equal(t, "test_value", result)
}
```

### Integration Testing

```go
func TestCustomOperatorIntegration(t *testing.T) {
    engine, _ := graft.NewEngine(
        graft.WithCustomOperator("env", &EnvOperator{}),
    )

    os.Setenv("DB_HOST", "localhost")
    defer os.Unsetenv("DB_HOST")

    doc, _ := engine.ParseYAML([]byte(`
database:
  host: (( env "DB_HOST" ))
`))

    result, err := engine.Evaluate(context.Background(), doc)

    assert.NoError(t, err)
    assert.Equal(t, "localhost", result.String("database.host"))
}
```

## Best Practices

### Do

- Validate argument count and types early

- Return descriptive errors with hints

- Support context cancellation for long operations

- Document operators with Info() metadata

- Use thread-safe patterns for stateful operators

- Test edge cases (nil args, wrong types, empty values)

### Don't

- Modify the document directly during evaluation

- Ignore context cancellation

- Assume argument types without checking

- Create side effects without documentation

- Block indefinitely without timeout

## Example: Complete Custom Operator

```go
package operators

import (
    "encoding/json"
    "fmt"
    "net/http"
    "time"

    "github.com/fivetwenty/graft"
)

// JSONAPIOperator fetches JSON from an API and extracts a value
type JSONAPIOperator struct {
    client *http.Client
}

func NewJSONAPIOperator(timeout time.Duration) *JSONAPIOperator {
    return &JSONAPIOperator{
        client: &http.Client{Timeout: timeout},
    }
}

func (o *JSONAPIOperator) Evaluate(ctx graft.EvalContext, args []interface{}) (interface{}, error) {
    // Validate arguments
    if len(args) < 1 || len(args) > 2 {
        return nil, &graft.EvaluationError{
            Operator: "jsonapi",
            Path:     ctx.Path(),
            Message:  fmt.Sprintf("requires 1-2 arguments, got %d", len(args)),
            Hint:     `Usage: (( jsonapi "url" )) or (( jsonapi "url" "json.path" ))`,
        }
    }

    url, ok := args[0].(string)
    if !ok {
        return nil, &graft.EvaluationError{
            Operator: "jsonapi",
            Path:     ctx.Path(),
            Message:  fmt.Sprintf("URL must be string, got %T", args[0]),
        }
    }

    // Make HTTP request
    req, err := http.NewRequestWithContext(ctx.Context(), "GET", url, nil)
    if err != nil {
        return nil, err
    }

    resp, err := o.client.Do(req)
    if err != nil {
        return nil, &graft.EvaluationError{
            Operator: "jsonapi",
            Path:     ctx.Path(),
            Message:  fmt.Sprintf("HTTP request failed: %v", err),
            Cause:    err,
        }
    }
    defer resp.Body.Close()

    // Parse JSON
    var data interface{}
    if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
        return nil, err
    }

    // Extract path if provided
    if len(args) == 2 {
        jsonPath, _ := args[1].(string)
        return extractJSONPath(data, jsonPath)
    }

    return data, nil
}

func (o *JSONAPIOperator) Info() graft.OperatorInfo {
    return graft.OperatorInfo{
        Name:        "jsonapi",
        Description: "Fetch JSON from API and optionally extract a value",
        MinArgs:     1,
        MaxArgs:     2,
        ArgTypes:    []string{"string (URL)", "string (JSON path, optional)"},
        Returns:     "any",
        Examples: []string{
            `(( jsonapi "https://api.example.com/config" ))`,
            `(( jsonapi "https://api.example.com/data" "items[0].name" ))`,
        },
        Category: "network",
    }
}

func extractJSONPath(data interface{}, path string) (interface{}, error) {
    // Implementation of JSON path extraction
    // ...
    return data, nil
}
```

## Related Documentation

- [Operator Reference](../user-guide/operators/index.md) - Built-in operators

- [Configuration Options](library-api/options.md) - Engine configuration

- [Testing Guide](testing.md) - Testing with mock engine

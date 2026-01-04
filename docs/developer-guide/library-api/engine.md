# Engine Interface

The `Engine` interface is the main entry point for all Graft operations. It provides methods for parsing documents, merging, diffing, evaluation, and operator management.

## Interface Definition

```go
type Engine interface {
    // Parsing
    ParseYAML(data []byte) (Document, error)
    ParseJSON(data []byte) (Document, error)
    ParseFile(path string) (Document, error)
    ParseReader(r io.Reader) (Document, error)

    // Merging
    Merge(ctx context.Context, docs ...Document) MergeBuilder

    // Diffing
    Diff(a, b Document) Diff
    DiffWithOptions(a, b Document, opts *DiffOptions) Diff

    // Evaluation
    Evaluate(ctx context.Context, doc Document) (Document, error)

    // Operators
    RegisterOperator(name string, op Operator) error
    GetOperator(name string) (Operator, bool)
    ListOperators() []OperatorInfo
    UnregisterOperator(name string) error

    // Configuration
    Configure(opts ...Option) error
}
```

## Creating an Engine

### NewEngine

Creates a new Engine instance with optional configuration.

```go
func NewEngine(opts ...Option) (Engine, error)
```

**Parameters:**

- `opts` - Zero or more functional options for configuration

**Returns:**

- `Engine` - The configured engine instance

- `error` - Non-nil if configuration fails

**Examples:**

```go
// Default engine
engine, err := graft.NewEngine()

// With configuration options
engine, err := graft.NewEngine(
    graft.WithCacheSize(500),
    graft.WithCacheTTL(1 * time.Minute),
    graft.WithHistoryTracking(true),
    graft.WithVault(graft.VaultConfig{
        Address: "https://vault.example.com",
        Token:   os.Getenv("VAULT_TOKEN"),
    }),
)
if err != nil {
    log.Fatalf("Failed to create engine: %v", err)
}
```

## Parsing Methods

### ParseYAML

Parses YAML-formatted bytes into a Document.

```go
func (e *Engine) ParseYAML(data []byte) (Document, error)
```

**Parameters:**

- `data` - YAML content as bytes

**Returns:**

- `Document` - The parsed document

- `error` - Non-nil if parsing fails

**Example:**

```go
yamlContent := []byte(`
name: myapp
database:
  host: localhost
  port: 5432
`)

doc, err := engine.ParseYAML(yamlContent)
if err != nil {
    var parseErr *graft.ParseError
    if errors.As(err, &parseErr) {
        fmt.Printf("Parse error at line %d: %s\n",
            parseErr.Position.Line, parseErr.Message)
        if parseErr.Hint != "" {
            fmt.Printf("Hint: %s\n", parseErr.Hint)
        }
    }
    return
}
```

### ParseJSON

Parses JSON-formatted bytes into a Document.

```go
func (e *Engine) ParseJSON(data []byte) (Document, error)
```

**Parameters:**

- `data` - JSON content as bytes

**Returns:**

- `Document` - The parsed document

- `error` - Non-nil if parsing fails

**Example:**

```go
jsonContent := []byte(`{
    "name": "myapp",
    "database": {
        "host": "localhost",
        "port": 5432
    }
}`)

doc, err := engine.ParseJSON(jsonContent)
if err != nil {
    log.Printf("JSON parse error: %v", err)
    return
}
```

### ParseFile

Parses a file from disk, auto-detecting the format based on extension.

```go
func (e *Engine) ParseFile(path string) (Document, error)
```

**Parameters:**

- `path` - Path to the file (`.yml`, `.yaml`, or `.json`)

**Returns:**

- `Document` - The parsed document

- `error` - Non-nil if file cannot be read or parsing fails

**Supported Extensions:**

- `.yml`, `.yaml` - Parsed as YAML

- `.json` - Parsed as JSON

**Example:**

```go
// Parse YAML file
doc, err := engine.ParseFile("/etc/myapp/config.yml")
if err != nil {
    if os.IsNotExist(err) {
        log.Println("Config file not found, using defaults")
    } else {
        log.Fatalf("Failed to parse config: %v", err)
    }
}

// Parse JSON file
jsonDoc, err := engine.ParseFile("settings.json")
```

### ParseReader

Parses content from an io.Reader. Content is assumed to be YAML (which is a superset of JSON).

```go
func (e *Engine) ParseReader(r io.Reader) (Document, error)
```

**Parameters:**

- `r` - Reader providing the document content

**Returns:**

- `Document` - The parsed document

- `error` - Non-nil if reading or parsing fails

**Example:**

```go
// Parse from HTTP response
resp, _ := http.Get("https://config.example.com/app.yml")
defer resp.Body.Close()

doc, err := engine.ParseReader(resp.Body)

// Parse from embedded content
//go:embed config.yml
var configContent string
doc, err := engine.ParseReader(strings.NewReader(configContent))

// Parse from stdin
doc, err := engine.ParseReader(os.Stdin)
```

## Merging Methods

### Merge

Initiates a merge operation and returns a MergeBuilder for configuration.

```go
func (e *Engine) Merge(ctx context.Context, docs ...Document) MergeBuilder
```

**Parameters:**

- `ctx` - Context for cancellation and timeout

- `docs` - One or more documents to merge (first is base, rest are overlays)

**Returns:**

- `MergeBuilder` - Builder for configuring and executing the merge

**Example:**

```go
ctx := context.Background()

// Simple merge
result, err := engine.Merge(ctx, base, overlay).Execute()

// Merge multiple overlays
result, err := engine.Merge(ctx, base, defaults, env, overrides).Execute()

// Merge with options
result, err := engine.Merge(ctx, base, overlay).
    Prune("meta", "internal").
    CherryPick("database", "server").
    TrackHistory().
    Execute()

// With context timeout
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
result, err := engine.Merge(ctx, base, overlay).Execute()
```

See [MergeBuilder API](merge-builder.md) for complete merge configuration options.

## Diffing Methods

### Diff

Compares two documents and returns their differences.

```go
func (e *Engine) Diff(a, b Document) Diff
```

**Parameters:**

- `a` - First document (typically "before" or "original")

- `b` - Second document (typically "after" or "modified")

**Returns:**

- `Diff` - Object representing the differences

**Example:**

```go
before, _ := engine.ParseFile("config-v1.yml")
after, _ := engine.ParseFile("config-v2.yml")

diff := engine.Diff(before, after)

if diff.HasChanges() {
    fmt.Println("Documents differ:")
    for _, change := range diff.Changes() {
        switch change.Type {
        case graft.ChangeAdded:
            fmt.Printf("  + %s: %v\n", change.Path, change.NewValue)
        case graft.ChangeRemoved:
            fmt.Printf("  - %s: %v\n", change.Path, change.OldValue)
        case graft.ChangeModified:
            fmt.Printf("  ~ %s: %v -> %v\n",
                change.Path, change.OldValue, change.NewValue)
        }
    }
}
```

### DiffWithOptions

Compares two documents with custom diff options.

```go
func (e *Engine) DiffWithOptions(a, b Document, opts *DiffOptions) Diff
```

**Parameters:**

- `a` - First document

- `b` - Second document

- `opts` - Configuration options for the diff

**Returns:**

- `Diff` - Object representing the differences

**DiffOptions:**

```go
type DiffOptions struct {
    Color            bool     // Enable ANSI color codes
    Width            int      // Output width for side-by-side
    Context          int      // Lines of context in unified format
    IgnorePaths      []string // Paths to exclude from comparison
    OnlyPaths        []string // Only compare these paths
    IgnoreArrayOrder bool     // Treat arrays as sets
    IgnoreWhitespace bool     // Ignore whitespace differences
    OmitHeader       bool     // Omit diff header
    ShowTypes        bool     // Show type information
}
```

**Example:**

```go
diff := engine.DiffWithOptions(before, after, &graft.DiffOptions{
    IgnorePaths:      []string{"metadata", "timestamp"},
    IgnoreArrayOrder: true,
    Color:            true,
    Width:            120,
})

// Output formatted diff
diff.WriteSideBySide(os.Stdout, nil)
```

See [Diff Interface](diff-api.md) for complete diff functionality.

## Evaluation Methods

### Evaluate

Evaluates all operators in a document, resolving references and external lookups.

```go
func (e *Engine) Evaluate(ctx context.Context, doc Document) (Document, error)
```

**Parameters:**

- `ctx` - Context for cancellation and timeout

- `doc` - Document containing operator expressions

**Returns:**

- `Document` - New document with all operators evaluated

- `error` - Non-nil if evaluation fails

**Example:**

```go
doc, _ := engine.ParseYAML([]byte(`
database:
  host: (( grab meta.db_host || "localhost" ))
  password: (( vault "secret/db:password" ))
  connection_string: (( concat "postgres://" database.host ":5432" ))
`))

ctx := context.Background()
result, err := engine.Evaluate(ctx, doc)
if err != nil {
    var evalErr *graft.EvaluationError
    if errors.As(err, &evalErr) {
        fmt.Printf("Evaluation failed at path: %s\n", evalErr.Path)
        fmt.Printf("Operator: %s\n", evalErr.Operator)
        fmt.Printf("Arguments: %v\n", evalErr.Arguments)
    }
    return
}

// All operators are now resolved
host, _ := result.GetString("database.host")
fmt.Println("Database host:", host)
```

**Edge Cases:**

- Circular references are detected and return an error

- Missing required references (without fallback) return an error

- Backend failures (Vault, AWS, etc.) return a `BackendError`

## Operator Management Methods

### RegisterOperator

Registers a custom operator with the engine.

```go
func (e *Engine) RegisterOperator(name string, op Operator) error
```

**Parameters:**

- `name` - Name of the operator (used in expressions like `(( name args ))`)

- `op` - The operator implementation

**Returns:**

- `error` - Non-nil if registration fails (e.g., name already registered)

**Example:**

```go
// Register a custom environment variable operator
engine.RegisterOperator("env", graft.OperatorFunc(
    func(ctx graft.EvalContext, args []interface{}) (interface{}, error) {
        if len(args) != 1 {
            return nil, fmt.Errorf("env requires exactly 1 argument")
        }
        name, ok := args[0].(string)
        if !ok {
            return nil, fmt.Errorf("env argument must be string")
        }
        value := os.Getenv(name)
        if value == "" {
            return nil, fmt.Errorf("environment variable %s not set", name)
        }
        return value, nil
    },
))

// Now usable in documents
doc, _ := engine.ParseYAML([]byte(`
api_key: (( env "API_KEY" ))
`))
```

See [Custom Operators](../custom-operators.md) for complete operator implementation guide.

### GetOperator

Retrieves a registered operator by name.

```go
func (e *Engine) GetOperator(name string) (Operator, bool)
```

**Parameters:**

- `name` - Name of the operator

**Returns:**

- `Operator` - The operator implementation

- `bool` - True if the operator exists

**Example:**

```go
if op, exists := engine.GetOperator("vault"); exists {
    fmt.Println("Vault operator is registered")
}

// Check before registering
if _, exists := engine.GetOperator("custom"); !exists {
    engine.RegisterOperator("custom", myOperator)
}
```

### ListOperators

Returns information about all registered operators.

```go
func (e *Engine) ListOperators() []OperatorInfo
```

**Returns:**

- `[]OperatorInfo` - Information about each registered operator

**OperatorInfo:**

```go
type OperatorInfo struct {
    Name        string
    MinArgs     int
    MaxArgs     int
    Phase       OperatorPhase
    Description string
}
```

**Example:**

```go
operators := engine.ListOperators()
for _, op := range operators {
    fmt.Printf("%-15s args: %d-%d  phase: %v\n",
        op.Name, op.MinArgs, op.MaxArgs, op.Phase)
}
```

### UnregisterOperator

Removes a registered operator.

```go
func (e *Engine) UnregisterOperator(name string) error
```

**Parameters:**

- `name` - Name of the operator to remove

**Returns:**

- `error` - Non-nil if the operator doesn't exist or cannot be removed

**Example:**

```go
// Remove a custom operator
err := engine.UnregisterOperator("custom")

// Note: Built-in operators cannot be unregistered
err := engine.UnregisterOperator("grab")
// Returns error: cannot unregister built-in operator
```

## Configuration Methods

### Configure

Applies additional configuration options to an existing engine.

```go
func (e *Engine) Configure(opts ...Option) error
```

**Parameters:**

- `opts` - One or more configuration options

**Returns:**

- `error` - Non-nil if configuration fails

**Example:**

```go
engine, _ := graft.NewEngine()

// Add Vault configuration later
err := engine.Configure(
    graft.WithVault(graft.VaultConfig{
        Address: vaultAddr,
        Token:   vaultToken,
    }),
)

// Add a secondary Vault target
err = engine.Configure(
    graft.WithVaultTarget("staging", graft.VaultConfig{
        Address: stagingVaultAddr,
        Token:   stagingToken,
    }),
)
```

See [Configuration Options](options.md) for all available options.

## Thread Safety

The Engine interface is fully thread-safe. Multiple goroutines can safely:

- Parse documents concurrently

- Execute merges concurrently

- Perform diffs concurrently

- Evaluate documents concurrently

**Example: Concurrent Operations**

```go
engine, _ := graft.NewEngine(
    graft.WithVault(vaultConfig),
)

var wg sync.WaitGroup
results := make(chan graft.Document, len(files))

for _, file := range files {
    wg.Add(1)
    go func(path string) {
        defer wg.Done()

        doc, err := engine.ParseFile(path)
        if err != nil {
            log.Printf("Failed to parse %s: %v", path, err)
            return
        }

        result, err := engine.Evaluate(context.Background(), doc)
        if err != nil {
            log.Printf("Failed to evaluate %s: %v", path, err)
            return
        }

        results <- result
    }(file)
}

wg.Wait()
close(results)
```

## Error Handling

All engine methods return detailed, structured errors:

```go
result, err := engine.Merge(ctx, base, overlay).Execute()
if err != nil {
    // Check for specific error types
    var parseErr *graft.ParseError
    var evalErr *graft.EvaluationError
    var mergeErr *graft.MergeError
    var backendErr *graft.BackendError

    switch {
    case errors.As(err, &parseErr):
        fmt.Printf("Parse error in %s at line %d: %s\n",
            parseErr.Source, parseErr.Position.Line, parseErr.Message)

    case errors.As(err, &evalErr):
        fmt.Printf("Evaluation error at %s: %s\n",
            evalErr.Path, evalErr.Message)

    case errors.As(err, &mergeErr):
        fmt.Printf("Merge conflict at %s between %s and %s\n",
            mergeErr.Path, mergeErr.SourceA, mergeErr.SourceB)

    case errors.As(err, &backendErr):
        fmt.Printf("Backend %s failed after %d retries: %s\n",
            backendErr.Backend, backendErr.RetryCount, backendErr.Cause)

    default:
        fmt.Printf("Unknown error: %v\n", err)
    }
}
```

## Related Documentation

- [Document Interface](document.md) - Working with parsed documents

- [MergeBuilder API](merge-builder.md) - Configuring merge operations

- [Diff Interface](diff-api.md) - Document comparison

- [Configuration Options](options.md) - Engine configuration

- [Custom Operators](../custom-operators.md) - Creating custom operators

# Engine Interface

The `Engine` interface is the main entry point for all Graft operations. It provides methods for parsing documents, merging, diffing, evaluation, and operator management.

## Interface Definition

```go
type Engine interface {
    // Parsing
    ParseYAML(data []byte) (Document, error)
    ParseJSON(data []byte) (Document, error)
    ParseFile(path string) (Document, error)
    ParseReader(reader io.Reader) (Document, error)

    // Merging
    Merge(ctx context.Context, docs ...Document) MergeBuilder
    MergeFiles(ctx context.Context, paths ...string) MergeBuilder
    MergeReaders(ctx context.Context, readers ...io.Reader) MergeBuilder

    // Evaluation
    Evaluate(ctx context.Context, doc Document) (Document, error)

    // Diffing
    Diff(a, b Document) DiffResult
    DiffWithOptions(a, b Document, opts *DiffOptions) DiffResult

    // Output
    ToYAML(doc Document) ([]byte, error)
    ToJSON(doc Document) ([]byte, error)
    ToJSONIndent(doc Document, indent string) ([]byte, error)

    // Operators
    RegisterOperator(name string, op Operator) error
    UnregisterOperator(name string) error
    ListOperators() []string
    GetOperator(name string) (Operator, bool)

    // Configuration
    WithLogger(logger Logger) Engine
    WithVaultClient(client VaultClient) Engine
    WithAWSConfig(config AWSConfig) Engine

    // State access for operators
    GetOperatorState() OperatorState
    GetMemoryTracker() interfaces.MemoryTracker
}
```

`Engine` is not intended to be implemented outside this package; `NewEngine` only ever returns `*DefaultEngine`. `Configure` (see [Configuration Methods](#configuration-methods) below) is a method on `*DefaultEngine`, not part of this interface.

`WithLogger`/`WithVaultClient`/`WithAWSConfig` are no-op methods that return the receiver unchanged — see [Deprecated Options](options.md#deprecated-options) for why, and for the distinct, functional `graft.WithLogger(logger) Option` of a similar name.

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
    graft.WithTraceLevel(graft.TraceLevelDebug),
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
    var graftErr *graft.GraftError
    if errors.As(err, &graftErr) && graftErr.Type == graft.ParseError {
        fmt.Printf("Parse error: %s\n", graftErr.Message)
    }
    return
}
```

See [Error Handling](#error-handling) below for the full `*graft.GraftError` shape and `ErrorType` values.

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
    WithPrune("meta", "internal").
    WithCherryPick("database", "server").
    Execute()

// With context timeout
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
result, err := engine.Merge(ctx, base, overlay).Execute()
```

See [MergeBuilder API](merge-builder.md) for complete merge configuration options.

### MergeFiles

Loads each path via `ParseFile` and merges the results, in argument order.

```go
func (e *Engine) MergeFiles(ctx context.Context, paths ...string) MergeBuilder
```

**Parameters:**

- `ctx` - Context for cancellation and timeout (a `nil` context is treated as `context.Background()`)

- `paths` - One or more file paths to load and merge (first is base, rest are overlays)

**Returns:**

- `MergeBuilder` - Builder for configuring and executing the merge. If any path fails to load, the returned builder carries that error (wrapped as `failed to load merge file %s: %w`) instead of panicking or returning a bare `nil`; the error surfaces from `Execute()`.

**Example:**

```go
ctx := context.Background()

result, err := engine.MergeFiles(ctx, "base.yml", "override.yml").Execute()
if err != nil {
    log.Fatalf("merge failed: %v", err)
}

yaml, _ := result.ToYAML()
fmt.Println(string(yaml))
```

### MergeReaders

Loads each reader via `ParseReader` and merges the results, in argument order. Error handling mirrors `MergeFiles`: a load failure is captured on the returned builder rather than panicking.

```go
func (e *Engine) MergeReaders(ctx context.Context, readers ...io.Reader) MergeBuilder
```

**Example:**

```go
base := strings.NewReader("name: myapp\n")
overlay := strings.NewReader("name: myapp-prod\n")

result, err := engine.MergeReaders(ctx, base, overlay).Execute()
```

## Diffing Methods

### Diff

Compares two documents and returns their differences, using `DefaultDiffOptions()`.

```go
func (e *Engine) Diff(a, b Document) DiffResult
```

**Parameters:**

- `a` - First document (typically "before" or "original")

- `b` - Second document (typically "after" or "modified")

**Returns:**

- `DiffResult` - Object representing the differences (never `nil`; a `nil` `a`/`b` produces an empty result rather than an error, since this method has no error return — use the package-level `DiffDocuments` when you need that error)

**Example:**

```go
before, _ := engine.ParseFile("config-v1.yml")
after, _ := engine.ParseFile("config-v2.yml")

result := engine.Diff(before, after)

if result.HasChanges() {
    fmt.Println("Documents differ:")
    for _, change := range result.Changes() {
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
func (e *Engine) DiffWithOptions(a, b Document, opts *DiffOptions) DiffResult
```

**Parameters:**

- `a` - First document

- `b` - Second document

- `opts` - Configuration options for the diff (`nil` selects `DefaultDiffOptions()`)

**Returns:**

- `DiffResult` - Object representing the differences

**DiffOptions:**

```go
type DiffOptions struct {
    Color            bool     // Enable ANSI color codes
    Width            int      // Output width for side-by-side (default 80)
    Context          int      // Max lines shown per changed value in unified format (0 = unbounded, the default)
    IgnorePaths      []string // Paths to exclude from comparison
    OnlyPaths        []string // Only compare these paths
    IgnoreArrayOrder bool     // Treat simple (non-keyed) arrays as multisets
    IgnoreWhitespace bool     // Ignore whitespace differences in string scalars
    OmitHeader       bool     // Omit the "N changes detected:" summary line
    ShowTypes        bool     // Show type information in WriteChangeList
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
    var graftErr *graft.GraftError
    if errors.As(err, &graftErr) && graftErr.Type == graft.EvaluationError {
        fmt.Printf("Evaluation failed at %s: %s\n", graftErr.Path, graftErr.Message)
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

- Backend failures (Vault, AWS, etc.) return a `*GraftError` with `Type == graft.ExternalError`

## Output Methods

### ToYAML, ToJSON, ToJSONIndent

Evaluate a document's operators (see [Evaluate](#evaluate)), then serialize the result.

```go
func (e *Engine) ToYAML(doc Document) ([]byte, error)
func (e *Engine) ToJSON(doc Document) ([]byte, error)
func (e *Engine) ToJSONIndent(doc Document, indent string) ([]byte, error)
```

**Parameters:**

- `doc` - Document to evaluate and serialize

- `indent` (`ToJSONIndent` only) - Per-level indentation string

**Returns:**

- `[]byte` - The evaluated document, serialized as YAML or JSON

- `error` - Non-nil if `doc` is `nil` or evaluation fails

**These evaluate first, unlike the same-named `Document` methods.** `Document.ToYAML`/`Document.ToJSON`/`Document.ToJSONIndent` (see [document.md](document.md)) serialize the document exactly as it stands, operator expressions included if any are still unresolved. `Engine.ToYAML`/`Engine.ToJSON`/`Engine.ToJSONIndent` evaluate `doc` first and serialize the evaluated result, combining `Evaluate` and the `Document`-level method in one call — useful for a document that was only parsed, not yet merged or evaluated.

**Mutates `doc`.** Like `Evaluate`, whose in-place behavior these three inherit, `doc` is resolved in place — this is not a read-only serialization call despite the name. A caller that still needs `doc`'s pre-evaluation state should pass `doc.Clone()` (a genuine deep copy) instead of `doc`.

**Example:**

```go
doc, _ := engine.ParseYAML([]byte(`
meta:
  env: production
database:
  host: (( grab meta.env ))
`))

// doc is evaluated in place by this call; calling ToJSON/ToJSONIndent on
// the same doc below re-evaluates an already-evaluated document, which is
// a no-op here since no unresolved operators remain. Pass doc.Clone() to
// each call instead if doc's own pre-evaluation state still matters.
yamlBytes, _ := engine.ToYAML(doc)
fmt.Print(string(yamlBytes))
// database:
//   host: production
// meta:
//   env: production

jsonBytes, _ := engine.ToJSON(doc)
fmt.Println(string(jsonBytes))
// {"database":{"host":"production"},"meta":{"env":"production"}}

indentBytes, _ := engine.ToJSONIndent(doc, "  ")
fmt.Println(string(indentBytes))
// {
//   "database": {
//     "host": "production"
//   },
//   "meta": {
//     "env": "production"
//   }
// }
```

**Edge Cases:**

- A `nil` `doc` returns a `*GraftError` without attempting evaluation

- An evaluation failure (missing reference, circular reference, backend error) is returned as-is, wrapped with `"failed to evaluate document: "` — nothing is serialized

- A `Document` whose underlying data is not evaluable (e.g. a go-patch operation list from `NewGoPatchDocument`) fails at the `Evaluate` step, the same as calling `Evaluate` directly on it

- `doc` itself ends up evaluated after the call, not just the returned bytes — see the mutation note above

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

Returns the names of all registered operators.

```go
func (e *Engine) ListOperators() []string
```

**Returns:**

- `[]string` - Registered operator names

**Example:**

```go
for _, name := range engine.ListOperators() {
    fmt.Println(name)
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

Applies additional configuration options to an existing engine, as an incremental change over its current configuration.

```go
func (e *DefaultEngine) Configure(opts ...Option) error
```

`Configure` is a method on the concrete `*DefaultEngine`, not part of the `Engine` interface — type-assert an `Engine` down to `*DefaultEngine` to call it (`NewEngine` only ever returns a `*DefaultEngine`, so the assertion is safe).

**Parameters:**

- `opts` - One or more configuration options, applied on top of the engine's existing configuration (a field `opts` doesn't touch keeps its current value)

**Returns:**

- `error` - Non-nil (leaving the engine's configuration unchanged) if the result is invalid, e.g. a negative concurrency

**Example:**

```go
engine, _ := graft.NewEngine()
de := engine.(*graft.DefaultEngine)

err := de.Configure(
    graft.WithCacheSize(2000),
    graft.WithTraceLevel(graft.TraceLevelDebug),
)
```

See [Configuration Options](options.md) for the full option list and `Configure`'s cache/trace/operator re-derivation behavior.

## Thread Safety

The Engine interface is fully thread-safe. Multiple goroutines can safely:

- Parse documents concurrently

- Execute merges concurrently

- Perform diffs concurrently

- Evaluate documents concurrently

**Example: Concurrent Operations**

```go
engine, _ := graft.NewEngine(
    graft.WithCacheSize(1000),
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

Every structured error engine methods return is a `*graft.GraftError`:

```go
type GraftError struct {
    Type    ErrorType // "parse_error", "merge_error", "evaluation_error", "operator_error", "configuration_error", "validation_error", "external_error"
    Message string
    Path    string // set for evaluation/validation errors; "" otherwise
    Cause   error  // unwraps via Unwrap(), so errors.Is/errors.As see through it
}

func (e *GraftError) Error() string
func (e *GraftError) Unwrap() error
```

There is one error type, not one struct per failure category — switch on `Type`, not on distinct Go types:

```go
result, err := engine.Merge(ctx, base, overlay).Execute()
if err != nil {
    var graftErr *graft.GraftError
    if errors.As(err, &graftErr) {
        switch graftErr.Type {
        case graft.ParseError:
            fmt.Printf("Parse error: %s\n", graftErr.Message)
        case graft.EvaluationError:
            fmt.Printf("Evaluation error at %s: %s\n", graftErr.Path, graftErr.Message)
        case graft.MergeError:
            fmt.Printf("Merge error: %s\n", graftErr.Message)
        case graft.ExternalError:
            fmt.Printf("Backend error: %s\n", graftErr.Message)
        default:
            fmt.Printf("%s: %s\n", graftErr.Type, graftErr.Message)
        }
        return
    }
    fmt.Printf("Unknown error: %v\n", err)
}
```

`ClassifyError(err) ErrorCode` additionally maps a `*GraftError` (or a well-known `tree`/filesystem error) to a stable, opt-in `ErrorCode` (e.g. `graft.CodeReferenceNotFound`, `graft.CodeTypeMismatch`) for machine-readable classification; see `pkg/graft/errors.go` and `docs/reference/error-codes.md`.

## Related Documentation

- [Document Interface](document.md) - Working with parsed documents

- [MergeBuilder API](merge-builder.md) - Configuring merge operations

- [Diff Interface](diff-api.md) - Document comparison

- [Configuration Options](options.md) - Engine configuration

- [Custom Operators](../custom-operators.md) - Creating custom operators

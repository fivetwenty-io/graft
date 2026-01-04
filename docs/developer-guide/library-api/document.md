# Document Interface

The `Document` interface represents a parsed YAML/JSON document. It provides type-safe access to values, mutation methods, serialization, and cloning capabilities.

## Interface Definition

```go
type Document interface {
    // Type-safe getters (return error if not found or wrong type)
    GetString(path string) (string, error)
    GetInt(path string) (int, error)
    GetInt64(path string) (int64, error)
    GetFloat64(path string) (float64, error)
    GetBool(path string) (bool, error)
    GetSlice(path string) ([]interface{}, error)
    GetStringSlice(path string) ([]string, error)
    GetMap(path string) (map[string]interface{}, error)
    GetMapStringString(path string) (map[string]string, error)
    Get(path string) (interface{}, error)

    // Checked getters (return zero value if not found)
    String(path string) string
    Int(path string) int
    Int64(path string) int64
    Float64(path string) float64
    Bool(path string) bool

    // Mutation
    Set(path string, value interface{}) error
    Delete(path string) error

    // Structure
    Keys() []string
    Paths() []string
    Has(path string) bool
    RawData() interface{}

    // History (if tracking enabled)
    History() History

    // Output
    ToYAML() ([]byte, error)
    ToJSON() ([]byte, error)
    ToJSONIndent(prefix, indent string) ([]byte, error)

    // Operations
    Clone() Document
    Prune(keys ...string) Document
    CherryPick(keys ...string) Document
}
```

## Path Syntax

Document methods use a dot-notation path syntax to access nested values:

| Path | Description |
|------|-------------|
| `key` | Top-level key |
| `parent.child` | Nested key |
| `array.0` | Array element by index |
| `array.0.field` | Field within array element |
| `parent.child.grandchild` | Deeply nested key |

**Examples:**

```yaml
database:
  host: localhost
  port: 5432
servers:
  - name: web1
    port: 8080
  - name: web2
    port: 8081
```

| Path | Value |
|------|-------|
| `database` | `{host: localhost, port: 5432}` |
| `database.host` | `"localhost"` |
| `database.port` | `5432` |
| `servers.0` | `{name: web1, port: 8080}` |
| `servers.0.name` | `"web1"` |
| `servers.1.port` | `8081` |

## Type-Safe Getters

These methods return an error if the path doesn't exist or the value is the wrong type.

### GetString

Returns a string value at the specified path.

```go
func (d *Document) GetString(path string) (string, error)
```

**Parameters:**

- `path` - Dot-notation path to the value

**Returns:**

- `string` - The string value

- `error` - Non-nil if path not found or value is not a string

**Example:**

```go
host, err := doc.GetString("database.host")
if err != nil {
    if errors.Is(err, graft.ErrNotFound) {
        log.Println("database.host not configured")
    } else if errors.Is(err, graft.ErrTypeMismatch) {
        log.Println("database.host is not a string")
    }
    return
}
fmt.Println("Host:", host)
```

### GetInt

Returns an integer value at the specified path.

```go
func (d *Document) GetInt(path string) (int, error)
```

**Parameters:**

- `path` - Dot-notation path to the value

**Returns:**

- `int` - The integer value

- `error` - Non-nil if path not found or value is not an integer

**Example:**

```go
port, err := doc.GetInt("database.port")
if err != nil {
    return err
}
fmt.Printf("Connecting to port %d\n", port)
```

**Note:** This method will convert float64 values to int if they have no decimal component.

### GetInt64

Returns a 64-bit integer value at the specified path.

```go
func (d *Document) GetInt64(path string) (int64, error)
```

**Parameters:**

- `path` - Dot-notation path to the value

**Returns:**

- `int64` - The 64-bit integer value

- `error` - Non-nil if path not found or value is not an integer

**Example:**

```go
timestamp, err := doc.GetInt64("metadata.created_at")
if err != nil {
    return err
}
t := time.Unix(timestamp, 0)
```

### GetFloat64

Returns a floating-point value at the specified path.

```go
func (d *Document) GetFloat64(path string) (float64, error)
```

**Parameters:**

- `path` - Dot-notation path to the value

**Returns:**

- `float64` - The floating-point value

- `error` - Non-nil if path not found or value is not numeric

**Example:**

```go
rate, err := doc.GetFloat64("pricing.discount_rate")
if err != nil {
    return err
}
finalPrice := basePrice * (1 - rate)
```

### GetBool

Returns a boolean value at the specified path.

```go
func (d *Document) GetBool(path string) (bool, error)
```

**Parameters:**

- `path` - Dot-notation path to the value

**Returns:**

- `bool` - The boolean value

- `error` - Non-nil if path not found or value is not a boolean

**Example:**

```go
enabled, err := doc.GetBool("features.debug")
if err != nil {
    return err
}
if enabled {
    log.SetLevel(log.DebugLevel)
}
```

### GetSlice

Returns a slice value at the specified path.

```go
func (d *Document) GetSlice(path string) ([]interface{}, error)
```

**Parameters:**

- `path` - Dot-notation path to the value

**Returns:**

- `[]interface{}` - The slice value

- `error` - Non-nil if path not found or value is not a slice

**Example:**

```go
items, err := doc.GetSlice("shopping.cart.items")
if err != nil {
    return err
}
for i, item := range items {
    fmt.Printf("Item %d: %v\n", i, item)
}
```

### GetStringSlice

Returns a slice of strings at the specified path.

```go
func (d *Document) GetStringSlice(path string) ([]string, error)
```

**Parameters:**

- `path` - Dot-notation path to the value

**Returns:**

- `[]string` - The string slice value

- `error` - Non-nil if path not found or any element is not a string

**Example:**

```go
tags, err := doc.GetStringSlice("resource.tags")
if err != nil {
    return err
}
for _, tag := range tags {
    fmt.Println("Tag:", tag)
}
```

### GetMap

Returns a map value at the specified path.

```go
func (d *Document) GetMap(path string) (map[string]interface{}, error)
```

**Parameters:**

- `path` - Dot-notation path to the value

**Returns:**

- `map[string]interface{}` - The map value

- `error` - Non-nil if path not found or value is not a map

**Example:**

```go
config, err := doc.GetMap("database")
if err != nil {
    return err
}
for key, value := range config {
    fmt.Printf("%s: %v\n", key, value)
}
```

### GetMapStringString

Returns a map of strings to strings at the specified path.

```go
func (d *Document) GetMapStringString(path string) (map[string]string, error)
```

**Parameters:**

- `path` - Dot-notation path to the value

**Returns:**

- `map[string]string` - The string-to-string map

- `error` - Non-nil if path not found or any key/value is not a string

**Example:**

```go
envVars, err := doc.GetMapStringString("deployment.environment")
if err != nil {
    return err
}
for key, value := range envVars {
    os.Setenv(key, value)
}
```

### Get

Returns a value of any type at the specified path.

```go
func (d *Document) Get(path string) (interface{}, error)
```

**Parameters:**

- `path` - Dot-notation path to the value

**Returns:**

- `interface{}` - The value (type depends on the document content)

- `error` - Non-nil if path not found

**Example:**

```go
value, err := doc.Get("configuration.setting")
if err != nil {
    return err
}

switch v := value.(type) {
case string:
    fmt.Println("String:", v)
case int:
    fmt.Println("Integer:", v)
case bool:
    fmt.Println("Boolean:", v)
case map[string]interface{}:
    fmt.Println("Map with", len(v), "keys")
case []interface{}:
    fmt.Println("Array with", len(v), "elements")
default:
    fmt.Printf("Unknown type: %T\n", v)
}
```

## Checked Getters

These methods return zero values instead of errors when the path doesn't exist or the type is wrong. Useful for optional values with defaults.

### String

Returns a string value or empty string if not found.

```go
func (d *Document) String(path string) string
```

**Example:**

```go
// Returns empty string if not found
host := doc.String("database.host")
if host == "" {
    host = "localhost"
}
```

### Int

Returns an integer value or 0 if not found.

```go
func (d *Document) Int(path string) int
```

**Example:**

```go
// Returns 0 if not found
port := doc.Int("database.port")
if port == 0 {
    port = 5432
}
```

### Int64

Returns a 64-bit integer value or 0 if not found.

```go
func (d *Document) Int64(path string) int64
```

### Float64

Returns a floating-point value or 0.0 if not found.

```go
func (d *Document) Float64(path string) float64
```

### Bool

Returns a boolean value or false if not found.

```go
func (d *Document) Bool(path string) bool
```

**Example:**

```go
// Convenient for feature flags
if doc.Bool("features.experimental") {
    enableExperimentalFeatures()
}
```

## Mutation Methods

### Set

Sets a value at the specified path, creating intermediate nodes as needed.

```go
func (d *Document) Set(path string, value interface{}) error
```

**Parameters:**

- `path` - Dot-notation path for the value

- `value` - The value to set (any type)

**Returns:**

- `error` - Non-nil if the path is invalid

**Example:**

```go
// Set simple value
err := doc.Set("database.host", "db.example.com")

// Set nested value (creates intermediate maps)
err = doc.Set("deeply.nested.configuration.value", 42)

// Set complex value
err = doc.Set("servers", []map[string]interface{}{
    {"name": "web1", "port": 8080},
    {"name": "web2", "port": 8081},
})

// Set array element
err = doc.Set("servers.0.port", 9090)
```

**Edge Cases:**

```go
// Creating array elements
doc.Set("items.0", "first")   // Creates array with one element
doc.Set("items.1", "second")  // Extends array
doc.Set("items.5", "sixth")   // Error: gap in array indices

// Type conflicts
doc.Set("value", "string")
doc.Set("value.nested", "x")  // Error: cannot nest under string
```

### Delete

Removes a value at the specified path.

```go
func (d *Document) Delete(path string) error
```

**Parameters:**

- `path` - Dot-notation path to delete

**Returns:**

- `error` - Non-nil if the path doesn't exist

**Example:**

```go
// Delete a key
err := doc.Delete("temporary_data")

// Delete nested key
err = doc.Delete("database.password")

// Delete array element
err = doc.Delete("servers.1")  // Removes second element, reindexes array
```

## Structure Methods

### Keys

Returns all top-level keys in the document.

```go
func (d *Document) Keys() []string
```

**Returns:**

- `[]string` - List of top-level keys

**Example:**

```go
keys := doc.Keys()
fmt.Println("Top-level keys:", strings.Join(keys, ", "))
// Output: Top-level keys: database, servers, metadata
```

### Paths

Returns all paths in the document (flattened).

```go
func (d *Document) Paths() []string
```

**Returns:**

- `[]string` - List of all paths in the document

**Example:**

```go
paths := doc.Paths()
for _, path := range paths {
    value, _ := doc.Get(path)
    fmt.Printf("%s = %v\n", path, value)
}
// Output:
// database.host = localhost
// database.port = 5432
// servers.0.name = web1
// servers.0.port = 8080
// ...
```

### Has

Checks if a path exists in the document.

```go
func (d *Document) Has(path string) bool
```

**Parameters:**

- `path` - Dot-notation path to check

**Returns:**

- `bool` - True if the path exists

**Example:**

```go
if doc.Has("database.password") {
    fmt.Println("Password is configured")
} else {
    fmt.Println("Using default password")
}

// Check before accessing
if doc.Has("optional.setting") {
    value, _ := doc.GetString("optional.setting")
    useValue(value)
}
```

### RawData

Returns the underlying data structure.

```go
func (d *Document) RawData() interface{}
```

**Returns:**

- `interface{}` - The raw data (typically `map[string]interface{}`)

**Example:**

```go
raw := doc.RawData()

// Type assert to map
if m, ok := raw.(map[string]interface{}); ok {
    for key, value := range m {
        fmt.Printf("Key: %s, Type: %T\n", key, value)
    }
}

// Useful for passing to JSON/YAML encoders
jsonBytes, _ := json.Marshal(doc.RawData())
```

**Warning:** Modifying the raw data directly bypasses history tracking and can lead to inconsistent state.

## History Methods

### History

Returns the history of changes if tracking was enabled.

```go
func (d *Document) History() History
```

**Returns:**

- `History` - The history interface (may be nil if not enabled)

**Example:**

```go
history := doc.History()
if history == nil {
    fmt.Println("History tracking not enabled")
    return
}

// Get changes for a specific path
entries := history.ForPath("database.host")
for _, entry := range entries {
    fmt.Printf("[%s] %v -> %v (from %s:%d)\n",
        entry.Phase, entry.OldValue, entry.NewValue,
        entry.Source, entry.Line)
}
```

See [History Interface](history-api.md) for complete history functionality.

## Output Methods

### ToYAML

Serializes the document to YAML format.

```go
func (d *Document) ToYAML() ([]byte, error)
```

**Returns:**

- `[]byte` - YAML-formatted content

- `error` - Non-nil if serialization fails

**Example:**

```go
yaml, err := doc.ToYAML()
if err != nil {
    return err
}
fmt.Println(string(yaml))

// Write to file
err = os.WriteFile("output.yml", yaml, 0644)
```

### ToJSON

Serializes the document to compact JSON format.

```go
func (d *Document) ToJSON() ([]byte, error)
```

**Returns:**

- `[]byte` - JSON-formatted content (compact, no indentation)

- `error` - Non-nil if serialization fails

**Example:**

```go
json, err := doc.ToJSON()
if err != nil {
    return err
}

// Send as HTTP response
w.Header().Set("Content-Type", "application/json")
w.Write(json)
```

### ToJSONIndent

Serializes the document to indented JSON format.

```go
func (d *Document) ToJSONIndent(prefix, indent string) ([]byte, error)
```

**Parameters:**

- `prefix` - Prefix for each line

- `indent` - Indentation string (e.g., "  " for 2 spaces)

**Returns:**

- `[]byte` - JSON-formatted content with indentation

- `error` - Non-nil if serialization fails

**Example:**

```go
// Pretty print with 2-space indent
json, err := doc.ToJSONIndent("", "  ")
if err != nil {
    return err
}
fmt.Println(string(json))

// With prefix (useful for embedding)
json, err = doc.ToJSONIndent("    ", "  ")
```

## Document Operations

### Clone

Creates a deep copy of the document.

```go
func (d *Document) Clone() Document
```

**Returns:**

- `Document` - A new document with copied data

**Example:**

```go
// Clone for concurrent modification
original, _ := engine.ParseFile("config.yml")

// Each goroutine gets its own copy
var wg sync.WaitGroup
for _, env := range []string{"dev", "staging", "prod"} {
    wg.Add(1)
    go func(environment string) {
        defer wg.Done()
        clone := original.Clone()
        clone.Set("environment", environment)
        processConfig(clone)
    }(env)
}
wg.Wait()
```

**Note:** Clone does NOT copy history. The new document starts with a fresh history.

### Prune

Returns a new document with specified keys removed.

```go
func (d *Document) Prune(keys ...string) Document
```

**Parameters:**

- `keys` - Top-level keys to remove

**Returns:**

- `Document` - New document without the specified keys

**Example:**

```go
// Remove internal and metadata keys
cleaned := doc.Prune("internal", "metadata", "debug")

// Original document is unchanged
fmt.Println(doc.Has("metadata"))     // true
fmt.Println(cleaned.Has("metadata")) // false

// Chain operations
result := doc.Prune("temp").Prune("cache")
```

### CherryPick

Returns a new document with only the specified keys.

```go
func (d *Document) CherryPick(keys ...string) Document
```

**Parameters:**

- `keys` - Top-level keys to keep

**Returns:**

- `Document` - New document with only the specified keys

**Example:**

```go
// Extract only database configuration
dbConfig := doc.CherryPick("database")

// Keep multiple sections
essential := doc.CherryPick("database", "server", "auth")

// Combine with other operations
minimal := doc.CherryPick("app", "database").Prune("secrets")
```

## Error Types

Document methods return specific error types:

```go
// Path not found
var ErrNotFound = errors.New("path not found")

// Type mismatch
var ErrTypeMismatch = errors.New("type mismatch")

// Invalid path syntax
var ErrInvalidPath = errors.New("invalid path")
```

**Error Handling Example:**

```go
value, err := doc.GetString("config.setting")
if err != nil {
    switch {
    case errors.Is(err, graft.ErrNotFound):
        // Use default value
        value = "default"
    case errors.Is(err, graft.ErrTypeMismatch):
        // Log warning and convert
        raw, _ := doc.Get("config.setting")
        value = fmt.Sprintf("%v", raw)
    default:
        return fmt.Errorf("unexpected error: %w", err)
    }
}
```

## Thread Safety

`Document` instances are NOT safe for concurrent modification. For concurrent access:

1. Use `Clone()` to create separate copies for each goroutine

2. Use synchronization if sharing a document

3. Read-only access is safe from multiple goroutines

**Safe Pattern:**

```go
// Each goroutine gets its own clone
for i := 0; i < workers; i++ {
    go func() {
        myDoc := sharedDoc.Clone()
        myDoc.Set("worker_id", i)
        processDocument(myDoc)
    }()
}
```

**Unsafe Pattern:**

```go
// DON'T DO THIS - concurrent modification
for i := 0; i < workers; i++ {
    go func() {
        sharedDoc.Set("counter", i)  // Race condition!
    }()
}
```

## Related Documentation

- [Engine Interface](engine.md) - Creating and parsing documents

- [MergeBuilder API](merge-builder.md) - Merging documents

- [History Interface](history-api.md) - Tracking document changes

- [Diff Interface](diff-api.md) - Comparing documents

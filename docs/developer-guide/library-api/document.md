# Document Interface

The `Document` interface represents a parsed YAML/JSON document. It provides type-safe access to values, mutation methods, serialization, and cloning capabilities.

## Interface Definition

```go
type Document interface {
    // Type-safe getters (return error if not found or wrong type)
    Get(path string) (interface{}, error)
    GetString(path string) (string, error)
    GetInt(path string) (int, error)
    GetInt64(path string) (int64, error)
    GetFloat64(path string) (float64, error)
    GetBool(path string) (bool, error)
    GetSlice(path string) ([]interface{}, error)
    GetStringSlice(path string) ([]string, error)
    GetMap(path string) (map[string]interface{}, error)
    GetMapStringString(path string) (map[string]string, error)

    // Checked getters (return zero value if not found or wrong type)
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
    GetData() interface{}
    SortKeys() Document

    // Output
    ToYAML() ([]byte, error)
    ToJSON() ([]byte, error)
    ToJSONIndent(indent string) ([]byte, error)

    // Operations
    Clone() Document
    Prune(keys ...string) Document
    CherryPick(keys ...string) Document

    // History (see the History Interface page)
    History() History
}
```

`Document` is not intended to be implemented outside this package: `NewEngine`, `ParseFile`, `ParseReader`, and every merge/diff entry point only ever return one of the two in-repo implementations (the map-backed default, and the go-patch operation holder returned by `NewGoPatchDocument`). New methods may be added to this interface in a minor release.

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

`errors.Is(err, graft.ErrNotFound)` and `errors.Is(err, graft.ErrTypeMismatch)` work against every error returned by `Get`, `GetString`, `GetInt`, `GetInt64`, `GetFloat64`, `GetBool`, `GetSlice`, `GetMap`, `GetStringSlice`, and `GetMapStringString`. `errors.Is(err, graft.ErrInvalidPath)` works against a malformed path string passed to any of those, or to `Set`/`Delete`. None of the `Error()` message text changes as a result of this: the sentinel match works through `Unwrap()`, invisibly to the printed message.

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

**Known gap:** unlike the `Get`/`GetString`/etc. family, `Set`'s (and [Delete](#delete)'s — the identical gap, for the identical reason) deeper traversal failures (descending into an existing scalar, or an out-of-range/invalid array index) are not guaranteed to satisfy `errors.Is(err, graft.ErrTypeMismatch)` or `errors.Is(err, graft.ErrNotFound)`. Check the returned error's message, not sentinel identity, for these cases.

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

Returns every leaf path in the document (every path whose value is not itself a map or a list), in dotted canonical form (no `$` prefix), sorted lexicographically for stable output. A map or list with no elements contributes no path of its own, since it has no leaf value to report. Every returned path round-trips through `Get`.

```go
func (d *Document) Paths() []string
```

**Returns:**

- `[]string` - Sorted list of every leaf path in the document

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

**Warning:** Modifying the raw data directly can lead to inconsistent state; prefer `Set`/`Delete`.

`RawData` never returns a `LiteralValue`. It replaces each one with the value it wraps, at any depth, and leaves the document itself unchanged. To do that it copies the maps and lists on the way to each wrapper, so the result is the document's own map only when the document holds no wrapper. A change made through `RawData` may therefore change a copy, which is one more reason to use `Set` and `Delete`. See [Literal Values](#literal-values).

### SortKeys

Returns a new document built by a fresh recursive rebuild of every nested map and list.

```go
func (d *Document) SortKeys() Document
```

**Returns:**

- `Document` - A new document with the same data as `d`

**Example:**

```go
sorted := doc.SortKeys()
```

Go maps have no persistent key order, so `SortKeys` does not reorder anything in memory. It exists so callers get a deterministic-output guarantee from `ToJSON`/`ToYAML` without depending on a particular marshaler's default behavior — `ToJSON` (`encoding/json`) sorts keys lexicographically, and `ToYAML` emits keys in graft's spruce-compatible order (numeric-looking keys first, then a natural string sort; see `pkg/graft/keysort.go`), with or without `SortKeys`. Iterating `sorted.RawData()`'s Go maps directly still sees Go's random map order.

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
func (d *Document) ToJSONIndent(indent string) ([]byte, error)
```

**Parameters:**

- `indent` - Per-level indentation string (e.g., "  " for 2 spaces)

**Returns:**

- `[]byte` - JSON-formatted content with indentation

- `error` - Non-nil if serialization fails

**Example:**

```go
// Pretty print with 2-space indent
json, err := doc.ToJSONIndent("  ")
if err != nil {
    return err
}
fmt.Println(string(json))
```

`Engine.ToJSONIndent(doc, indent)` is the engine-level equivalent, additionally passing evaluated documents through the same call.

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

**Note:** Clone does NOT copy history - the clone's `History()` returns an
empty `History`, even if the original document's did not.

### History

Returns this document's recorded change history.

```go
func (d *Document) History() History
```

**Returns:**

- `History` - Never a nil interface. Empty (every method returns an
  empty result) unless this exact `Document` value was returned by
  `Execute()` on a merge chain where document-memory tracking was active
  (`MergeBuilder.TrackHistory()`, or the engine was constructed with
  `WithHistoryTracking`/`WithHistoryConfig`/`WithMemoryConfig`).

**Example:**

```go
result, err := engine.Merge(ctx, base, overlay).
    TrackHistory().
    Execute()
if err != nil {
    return err
}

for _, entry := range result.History().Timeline() {
    fmt.Printf("%s: %v -> %v\n", entry.Path, entry.OldValue, entry.NewValue)
}
```

See [History Interface](history-api.md) for the full `History`/
`HistoryEntry` surface, what is and is not recorded, and history's
engine-wide (not per-merge) scope. `History()` is promoted onto every
go-patch document (`NewGoPatchDocument`'s result) the same way the
checked getters are - see that constructor's own doc comment - and
always returns an empty `History`, since a go-patch document never has
merge-phase or eval-phase changes recorded against it.

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

## Literal Values

A `LiteralValue` marks a value that graft must keep whole. We use one when a document holds data that came from outside the plan, such as the parameters a user sends with a service request, and graft must neither merge into that data nor evaluate it.

```go
func NewLiteral(v any) LiteralValue

func (l LiteralValue) Value() any
```

`NewLiteral` wraps any value, including `nil`. Wrapping a `LiteralValue` again returns it unchanged, so there is never a wrapper inside a wrapper at the same spot. `NewLiteral` wraps a deep copy of the value, so a change we make to the value after we wrap it does not reach the document. On the way it turns each `map[interface{}]interface{}` into a `map[string]interface{}`, so a path can read into a map that a YAML library built with untyped keys. The copy covers `map[string]interface{}`, `map[interface{}]interface{}`, and `[]interface{}`. It also covers every typed slice, array, and map, such as a `[]string`, a `map[string][]string`, or a `[][]string`, and every pointer to one of those, so a change to any of them after the call does not reach the document. A typed container keeps its type in the copy, and a path cannot read into it, just as it cannot when the value is not wrapped. A nil slice, map, or pointer stays nil, and an empty one stays empty. A struct is not copied, and neither is a pointer to a struct or to a scalar, so the fields of a struct and anything it points to stay shared with the original.

`NewLiteral` panics when a map or list in the value contains itself, because graft cannot hold cyclic data. It finds the cycle in a single pass. The panic message quotes nothing from the value. A map or list that appears in more than one place is not a cycle, and each place gets its own copy.

**How a merge treats it:**

- A wrapped value replaces whatever it meets as a whole unit, the way a scalar does. A wrapped map does not merge key by key into the map below it, and a wrapped list of named maps does not merge by `name` into the list below it.

- A value merged over a wrapped value replaces it whole in the same way.

- `(( append ))`, `(( replace ))`, and every other merge marker inside a wrapped value is plain text.

**How evaluation treats it:**

- The evaluator never looks inside a wrapped value, on the sequential path or the parallel one. A `(( vault ... ))` string inside it is never read from Vault, and a malformed `((` string inside it is not an error.

- A path can still read into a wrapped value. `(( grab params.size ))`, `(( keys params ))`, `(( concat params.name "-svc" ))`, `(( join "," params.tags ))`, and a `||` fallback all see through the wrapper.

- A value that `(( grab ))` or a bare `(( a || b ))` copies out of a wrapped value stays wrapped at its new place, and so does each value that `(( inject ))` copies out of one, whether it reaches the wrapped map through a reference, a `||` fallback, or a nested call. A second evaluation of the result still leaves them alone.

- A value that another operator computes from wrapped data, such as the string `(( concat ))` builds or the list `(( keys ))` returns, is wrapped in turn. That holds for every operator that reads a wrapped value on the way to its result, including `(( join ))`, `(( split ))`, `(( base64 ))`, `(( base64-decode ))`, `(( stringify ))`, `(( flatten ))`, `(( uniq ))`, `(( shuffle ))`, `(( cartesian-product ))`, `(( ips ))`, `(( raw_env ))` with a default, and the arithmetic and ternary operators. A chain of operators therefore never turns wrapped text back into plain text. A `((` string in the result stays text, so a later phase or a second evaluation never runs it, and a Vault path inside it is never read. The output still writes the plain value, so the merged document looks the same as it did before the result was wrapped.

- A wrapped value is always data, and it is never code, a path, or the name of something graft reads. The same is true of every value an operator computes from one. When an operator would read such a value as code, as a path, or as the name of an outside resource, the merge fails at that path, and it fails before graft asks any backend, opens any file, or dials any server. The operators that refuse are `(( calc ))` for its expression, `(( grab ))` for a path it computes or a bracket key, `(( vault ))` and `(( vault-try ))` for a secret path, `(( awsparam ))` and `(( awssecret ))` for a parameter or secret name, `(( nats ))` for its store path and for its server or settings, `(( file ))` for its path, `(( load ))` for its file or URL, and `(( static_ips ))` for the network name of the job it runs in.

- `(( calc ))` makes one exception. A wrapped number, or a wrapped string that reads wholly as one number, such as `"21"` or `" 2.5e1 "`, is that number. Any other wrapped value fails, whether it is an expression, a path, or malformed text.

- A refusal's error names the operator, what the value would have been used as, and the path, and it quotes nothing from the value, in `Error()` or in `graft.DetailedError`. A `||` fallback, a `(( vault ))` default, or the next path in a `(( vault-try ))` list never hides a refusal, so `(( vault params.path || "default" ))` fails rather than returning the default.

- A path that the plan itself writes still reads a wrapped value as data. `(( grab params.p ))` returns the text of `params.p` and never reads the path that the text names, and `(( vault meta.path || params.fallback ))` may return the wrapped fallback as its value.

- This has one known limit. When a document holds a wrapped value anywhere, a plain `((` string that an operator returns can stay text, even when it never came from a wrapper. The evaluator keeps every `((` string in an operator's result as text when any of the operator's arguments could read from a wrapper. An argument counts in any of four cases. It may be a path to a wrapped value, into one, or to a value that holds one. It may be a string that reads as such a path. It may hold an environment variable. Or it may be a nested call with an argument that is itself computed. The empty string reads as the path to the whole document, so a `""` argument counts whenever the document holds a wrapper. Say `meta.d` is `(( defer concat "a" "b" ))` and another key is `(( concat meta.d "" ))`. With no wrapped value in the document, the merge turns that key into `ab`. With a wrapped value anywhere in it, the key holds the text `(( concat "a" "b" ))` after the merge, and a second evaluation leaves it as text. `(( concat (grab meta.d) "" ))` and `(( join "" meta.list ))` behave the same way, while a direct `(( grab meta.d ))` still becomes `ab`. The limit fails safe, because graft never evaluates text from a wrapped value. It can only leave a plain string unevaluated.

- Suppose an operator that read from a wrapped value fails, and the `Error()` text of its error holds a key or a scalar of 3 or more characters from inside any wrapper in the document. The evaluator then withholds the message, though the error still names the path where the operator ran. A shorter text, such as `80` or `on`, is not withheld, because it turns up in too many ordinary messages, and an operator that read no wrapped value keeps its message even when it happens to hold the same text. `errors.Is` still finds a sentinel in the original error, and `errors.As` still finds graft's own error types in it when their text quotes nothing from a wrapper.

- `graft.DetailedError` is not scrubbed. It returns the original message, with any wrapped text it quoted, so we show it only where the wrapped content may be seen and never log it as a matter of course.

- `CheckForCycles` and the evaluator's own cycle check look inside a wrapped value, so a wrapped value that refers to itself is reported as a cycle.

**How output treats it:**

- `RawData`, `GetData`, `ToMap`, `Get` and the other getters, `Paths`, `ToYAML`, `ToJSON`, `ToJSONIndent`, `graft.MarshalYAML`, `pkg/yaml` `Marshal`, and `encoding/json` all write the plain value with every wrapper removed.

- `Clone` and the engine's own copies keep the wrapper and copy the value inside it.

- `Get`, `GetMap`, and `GetSlice` return the document's own maps and lists from inside a wrapped value, just as they do for plain data, so a change to what they return changes the document. `Set` and `Delete` cannot reach inside a wrapped value and return an error when a path goes through one.

**How post-processors treat it:**

- `Execute` passes each post-processor the document with its wrappers in place, so the document it returns still holds them. The built-in prune and inject processors in `postprocess` leave a `(( prune ))` string or a `<<` key inside a wrapped value alone.

- `NewPruner` and `NewCherryPicker` can name a path inside a wrapped value. The pruner removes it from a copy of the wrapped value, and the cherry picker keeps what it picks wrapped. `NewSecurityRedactor` masks a matching key inside a wrapped value and keeps the wrapper.

- A custom processor should change the `Document` it receives through `Set` and `Delete` and return it. A processor that builds a new document from `RawData` drops every wrapper, and a later evaluation of that result treats what was inside them as plain data.

**Example:**

```go
plan, _ := engine.ParseYAML([]byte(`
params:
  size: small
  tags: [a, b]
meta:
  size: (( grab params.size ))
`))

user := graft.NewDocument(map[string]interface{}{
    "params": graft.NewLiteral(map[string]interface{}{
        "size": "large",
        "note": `(( vault "secret/never:read" ))`,
    }),
})

result, err := engine.Merge(ctx, plan, user).Execute()
// params is now {size: large, note: (( vault "secret/never:read" ))}.
// tags is gone, note is plain text, and meta.size is large.
```

`WithPrune` and `WithCherryPick` can name a wrapped value itself but not a path inside it, because those steps treat the wrapper as a single leaf. A path inside a wrapped value that either one names is left as it is, with no error.

## Error Types

`Document` getters return a `*graft.GraftError` wrapping one of three sentinel errors, comparable with `errors.Is`:

```go
var (
    ErrNotFound     = errors.New("path not found")
    ErrTypeMismatch = errors.New("type mismatch")
    ErrInvalidPath  = errors.New("invalid path")
)
```

The printed `Error()` text is not one of these three strings verbatim — it is the existing, unchanged `GraftError`/`tree` message (e.g. `` `$.database.host` could not be found in the datastructure ``). Match on the sentinel with `errors.Is`, not on the message text.

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

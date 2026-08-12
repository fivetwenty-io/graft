# Diff Interface

`DiffResult` represents the differences between two documents. It provides methods to access changes, filter by type or path, format output in various styles, and serialize to JSON/YAML.

## Interface Definition

```go
type DiffResult interface {
    // Access to changes
    Changes() []Change
    HasChanges() bool

    // Filtered access
    Added() []Change
    Removed() []Change
    Modified() []Change

    // Path-specific
    ChangesAtPath(path string) []Change

    // Output formatters
    WriteSideBySide(w io.Writer, opts *DiffOptions) error
    WriteUnified(w io.Writer, opts *DiffOptions) error
    WriteChangeList(w io.Writer, opts *DiffOptions) error
    WriteMergeTree(w io.Writer, opts *DiffOptions) error

    // Serialization
    ToJSON() ([]byte, error)
    ToYAML() ([]byte, error)
}
```

The interface is named `DiffResult`, not `Diff`: `pkg/graft` already exports a package-level function `Diff(a, b interface{}) (Diffable, error)` (the underlying spruce-inherited comparison engine `DiffResult` is built on), and a package cannot export both a function and a type of the same name.

`DiffResult` is not intended to be implemented outside this package; `DiffDocuments` and `Engine.Diff`/`Engine.DiffWithOptions` are its only producers.

## Types

### Change

Represents a single addition, removal, or modification found by diffing two documents.

```go
type Change struct {
    Type     ChangeType
    Path     string
    OldValue interface{}
    NewValue interface{}
}
```

| Field | Type | Description |
|-------|------|-------------|
| `Type` | `ChangeType` | The kind of change (added, removed, modified, type changed) |
| `Path` | `string` | Path to the changed value: dot-separated for map fields (`meta.name`), bracketed for list entries — a numeric index for simple lists (`servers[0]`) or a `field=value` predicate for keyed lists (`servers[name=web]`) |
| `OldValue` | `interface{}` | Value at `Path` in the old document. `nil` for `ChangeAdded` |
| `NewValue` | `interface{}` | Value at `Path` in the new document. `nil` for `ChangeRemoved` |

`Change` has no `Source` (input filename) or `Line` (line number) field. Graft does not track value-level file/line provenance anywhere today; a `Change` only knows the path and the two values.

### ChangeType

Indicates the kind of change.

```go
type ChangeType int

const (
    ChangeAdded ChangeType = iota
    ChangeRemoved
    ChangeModified
    ChangeTypeChanged
)
```

| Constant | Value | Description |
|----------|-------|-------------|
| `ChangeAdded` | 0 | Path exists in the new document but not the old |
| `ChangeRemoved` | 1 | Path exists in the old document but not the new |
| `ChangeModified` | 2 | Path exists in both, same graft value type, different value |
| `ChangeTypeChanged` | 3 | Path exists in both, but the graft value type changed (e.g. a scalar became a map) |

`ChangeType.String()` returns `"added"`, `"removed"`, `"modified"`, or `"type changed"` respectively; this is the value used in `ToJSON`/`ToYAML`'s `"type"` field and the `WriteChangeList`/`WriteMergeTree` renderers.

### DiffOptions

Configures diff computation and rendering.

```go
type DiffOptions struct {
    Color            bool
    Width            int
    Context          int
    IgnorePaths      []string
    OnlyPaths        []string
    IgnoreArrayOrder bool
    IgnoreWhitespace bool
    OmitHeader       bool
    ShowTypes        bool
}

func DefaultDiffOptions() *DiffOptions
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `Color` | `bool` | `false` | Enable ANSI color codes in rendered output |
| `Width` | `int` | `80` | Target column width for `WriteSideBySide`; values `<= 0` fall back to 80 |
| `Context` | `int` | `0` (unbounded) | Max lines of a changed value's YAML rendering `WriteUnified` shows before eliding the rest with a `... (N more lines)` marker |
| `IgnorePaths` | `[]string` | `nil` | Exclude changes whose path matches one of these `PathMatches`-grammar patterns (`*` matches one segment, `**` matches zero or more). Applied after `OnlyPaths` |
| `OnlyPaths` | `[]string` | `nil` | If non-empty, restrict to changes matching at least one pattern (same grammar). Applied before `IgnorePaths` |
| `IgnoreArrayOrder` | `bool` | `false` | Treat non-keyed lists as multisets at any nesting depth. Keyed lists (entries with a `name`/`id`/`key` field) are already matched by key and are unaffected |
| `IgnoreWhitespace` | `bool` | `false` | Trim and collapse whitespace runs in every string scalar, at any nesting depth, before comparing |
| `OmitHeader` | `bool` | `false` | Suppress the `N changes detected:` summary line every renderer otherwise prints first |
| `ShowTypes` | `bool` | `false` | Annotate each `WriteChangeList` entry with the graft value type (`scalar`/`map`/`simple list`/`keyed list`) of its old and new value |

`DefaultDiffOptions()` returns `{Width: 80, Context: 0}` with every other field at its zero value — no color, no path filters, order- and whitespace-sensitive comparison. Passing `nil` for `opts` anywhere in this API is equivalent to passing `DefaultDiffOptions()`.

`IgnorePaths`/`OnlyPaths`/`IgnoreArrayOrder`/`IgnoreWhitespace` only take effect at diff-computation time (`DiffDocuments`, `Engine.Diff`, `Engine.DiffWithOptions`). Passing them to a `Write*` renderer call has no effect there — only `Width`/`Context`/`OmitHeader`/`ShowTypes`/`Color` are read at render time.

## Computing a Diff

```go
engine, _ := graft.NewEngine()

doc1, _ := engine.ParseFile("before.yml")
doc2, _ := engine.ParseFile("after.yml")

// Default options
result := engine.Diff(doc1, doc2)

// Custom options
result := engine.DiffWithOptions(doc1, doc2, &graft.DiffOptions{
    IgnorePaths:      []string{"metadata.timestamp"},
    IgnoreArrayOrder: true,
})

// Package-level equivalent, returning an error instead of swallowing it
result, err := graft.DiffDocuments(doc1, doc2, nil)
```

`Engine.Diff(a, b)` is `DiffWithOptions(a, b, DefaultDiffOptions())`. `Engine.DiffWithOptions` swallows any error from the underlying comparison into an empty `DiffResult` (its method signature has no `error` return); use the package-level `DiffDocuments` directly when you need the error — for example, a `nil` `Document` argument.

## Access Methods

### Changes

Returns every change found, in a deterministic order: within each map/list node, additions are listed before removals, which are listed before recursing into changed common entries (each in sorted-key order). This is not a globally path-sorted order, but it is stable across runs for the same input.

```go
func (r DiffResult) Changes() []Change
```

**Example:**

```go
result := engine.Diff(doc1, doc2)

for _, change := range result.Changes() {
    fmt.Printf("%s at %s\n", change.Type, change.Path)
}
```

### HasChanges

```go
func (r DiffResult) HasChanges() bool
```

Reports whether `Changes()` is non-empty.

```go
result := engine.Diff(current, proposed)

if !result.HasChanges() {
    fmt.Println("No changes detected")
    return
}
```

### Added / Removed / Modified

```go
func (r DiffResult) Added() []Change    // Type == ChangeAdded
func (r DiffResult) Removed() []Change  // Type == ChangeRemoved
func (r DiffResult) Modified() []Change // Type == ChangeModified || Type == ChangeTypeChanged
```

`Modified()` includes both value modifications and type changes — everything that is neither a pure addition nor a pure removal.

```go
for _, change := range result.Added() {
    fmt.Printf("+ %s: %v\n", change.Path, change.NewValue)
}
for _, change := range result.Removed() {
    fmt.Printf("- %s: %v\n", change.Path, change.OldValue)
}
for _, change := range result.Modified() {
    fmt.Printf("~ %s: %v -> %v\n", change.Path, change.OldValue, change.NewValue)
}
```

### ChangesAtPath

Returns every change whose path is exactly `path`, or a descendant of it.

```go
func (r DiffResult) ChangesAtPath(path string) []Change
```

```go
dbChanges := result.ChangesAtPath("database")
for _, change := range dbChanges {
    fmt.Printf("%s: %v -> %v\n", change.Path, change.OldValue, change.NewValue)
}
```

## Output Formatters

Every `Write*` method accepts a `nil` `opts` (falls back to `DefaultDiffOptions()`).

### WriteChangeList

One line per change, prefixed with `+`/`-`/`~`/`!`.

```go
func (r DiffResult) WriteChangeList(w io.Writer, opts *DiffOptions) error
```

```go
result.WriteChangeList(os.Stdout, &graft.DiffOptions{ShowTypes: true})
```

**Output:**

```
2 changes detected:
  + database.pool_size (none -> scalar) added: 10
  ~ database.host (scalar -> scalar) modified: localhost -> db.example.com
```

### WriteUnified

One `@@ path @@` hunk per change, with `-`/`+` lines for the old/new value's YAML rendering.

```go
func (r DiffResult) WriteUnified(w io.Writer, opts *DiffOptions) error
```

```go
result.WriteUnified(os.Stdout, &graft.DiffOptions{Context: 3})
```

**Output:**

```
1 change detected:
@@ database.host @@
-localhost
+db.example.com
```

### WriteSideBySide

Two columns (old value | new value) per change, each sized from `opts.Width`.

```go
func (r DiffResult) WriteSideBySide(w io.Writer, opts *DiffOptions) error
```

```go
result.WriteSideBySide(os.Stdout, &graft.DiffOptions{Color: true, Width: 60})
```

**Output (uncolored):**

```
1 change detected:
database.host
  localhost                    | db.example.com
```

### WriteMergeTree

Changes grouped into a tree that mirrors the document's path structure, one node per line, indented by depth.

```go
func (r DiffResult) WriteMergeTree(w io.Writer, opts *DiffOptions) error
```

```go
result.WriteMergeTree(os.Stdout, nil)
```

**Output:**

```
2 changes detected:
database:
  pool_size: + 10
  host: ~ localhost -> db.example.com
```

Each leaf line is `key: <marker> <value>` (add/remove) or `key: <marker> <old> -> <new>` (modify/type-change), where `<marker>` is `+`, `-`, `~`, or `!`.

## Serialization Methods

### ToJSON

Serializes `Changes()` as a compact (`json.Marshal`, no indentation), single-line JSON array of `{"type", "path", "old_value", "new_value"}` objects — not a `{"changes": [...]}` wrapper object, and with no `has_changes`/`summary` fields.

```go
func (r DiffResult) ToJSON() ([]byte, error)
```

```go
data, err := result.ToJSON()
```

**Output:**

```json
[{"type":"added","path":"database.pool_size","old_value":null,"new_value":10},{"type":"modified","path":"database.host","old_value":"localhost","new_value":"db.example.com"}]
```

### ToYAML

Same shape as `ToJSON`, rendered as a YAML sequence.

```go
func (r DiffResult) ToYAML() ([]byte, error)
```

```go
data, err := result.ToYAML()
```

**Output:**

```yaml
- type: added
  path: database.pool_size
  old_value: null
  new_value: 10
- type: modified
  path: database.host
  old_value: localhost
  new_value: db.example.com
```

## Complete Examples

### Configuration Comparison

```go
func compareConfigs(oldFile, newFile string) error {
    engine, _ := graft.NewEngine()

    oldDoc, err := engine.ParseFile(oldFile)
    if err != nil {
        return fmt.Errorf("parse old: %w", err)
    }

    newDoc, err := engine.ParseFile(newFile)
    if err != nil {
        return fmt.Errorf("parse new: %w", err)
    }

    result := engine.DiffWithOptions(oldDoc, newDoc, &graft.DiffOptions{
        IgnorePaths: []string{"metadata.generated_at"},
    })

    if !result.HasChanges() {
        fmt.Println("Configurations are identical")
        return nil
    }

    fmt.Printf("Found %d changes:\n", len(result.Changes()))
    return result.WriteChangeList(os.Stdout, &graft.DiffOptions{
        Color:     true,
        ShowTypes: true,
    })
}
```

### Change Validation

```go
func validateChanges(before, after graft.Document) error {
    engine, _ := graft.NewEngine()

    result := engine.Diff(before, after)

    // Check for disallowed changes
    for _, change := range result.Removed() {
        if strings.HasPrefix(change.Path, "security.") {
            return fmt.Errorf("cannot remove security settings: %s", change.Path)
        }
    }

    // Validate modifications
    for _, change := range result.Modified() {
        if change.Path == "database.host" {
            if host, ok := change.NewValue.(string); ok {
                if err := validateHost(host); err != nil {
                    return fmt.Errorf("invalid database host: %w", err)
                }
            }
        }
    }

    // Log additions for audit
    for _, change := range result.Added() {
        log.Printf("New configuration: %s = %v", change.Path, change.NewValue)
    }

    return nil
}
```

### Diff with Array Handling

```go
func compareWithArrays(before, after graft.Document) {
    engine, _ := graft.NewEngine()

    // Standard comparison (arrays compared by index)
    strict := engine.Diff(before, after)

    // Set-based comparison (arrays compared by content, at any depth)
    unordered := engine.DiffWithOptions(before, after, &graft.DiffOptions{
        IgnoreArrayOrder: true,
    })

    fmt.Println("Strict comparison:")
    strict.WriteChangeList(os.Stdout, nil)

    fmt.Println("\nSet-based comparison:")
    unordered.WriteChangeList(os.Stdout, nil)
}
```

With:

```yaml
# before.yml
servers: [web1, web2, web3]
# after.yml
servers: [web2, web3, web1]
```

the strict comparison reports `servers[0]` and `servers[2]` as modified; the `IgnoreArrayOrder` comparison reports no changes.

## Edge Cases

### Empty documents

```go
empty, _ := engine.ParseYAML([]byte("{}"))
populated, _ := engine.ParseFile("config.yml")

added := engine.Diff(empty, populated)   // every path in populated is "added"
removed := engine.Diff(populated, empty) // every path in populated is "removed"
```

### Null values

```go
doc1, _ := engine.ParseYAML([]byte("key: null\n"))
doc2, _ := engine.ParseYAML([]byte("other: value\n"))

result := engine.Diff(doc1, doc2)
// "key" is removed (an explicit null is still a value) and "other" is added
```

### Type changes

```go
doc1, _ := engine.ParseYAML([]byte(`port: "8080"`))
doc2, _ := engine.ParseYAML([]byte(`port: 8080`))

result := engine.Diff(doc1, doc2)
// "port" is a ChangeTypeChanged (string -> int)
```

## Thread Safety

`DiffResult` is safe for concurrent reads: accessing changes, formatting output, and serializing to JSON/YAML from multiple goroutines is safe once the `DiffResult` has been produced.

## Related Documentation

- [Engine Interface](engine.md) - Creating diffs

- [Document Interface](document.md) - Working with documents

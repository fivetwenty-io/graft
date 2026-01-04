# Diff Interface

The `Diff` interface represents differences between two documents. It provides methods to access changes, filter by type or path, and format output in various styles.

## Interface Definition

```go
type Diff interface {
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

## Types

### Change

Represents a single change between documents.

```go
type Change struct {
    Type     ChangeType
    Path     string
    OldValue interface{}
    NewValue interface{}
    Source   string
    Line     int
}
```

| Field | Type | Description |
|-------|------|-------------|
| `Type` | `ChangeType` | The type of change (added, removed, modified, type changed) |
| `Path` | `string` | Dot-notation path to the changed value |
| `OldValue` | `interface{}` | Value in the first document (nil for additions) |
| `NewValue` | `interface{}` | Value in the second document (nil for removals) |
| `Source` | `string` | Source file or identifier |
| `Line` | `int` | Line number in source (if available) |

### ChangeType

Indicates the type of change.

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
| `ChangeAdded` | 0 | Path exists in second document but not first |
| `ChangeRemoved` | 1 | Path exists in first document but not second |
| `ChangeModified` | 2 | Path exists in both with different values |
| `ChangeTypeChanged` | 3 | Path exists in both with different types |

### DiffOptions

Configures diff behavior and formatting.

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
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `Color` | `bool` | `false` | Enable ANSI color codes in output |
| `Width` | `int` | `80` | Output width for side-by-side format |
| `Context` | `int` | `3` | Lines of context in unified format |
| `IgnorePaths` | `[]string` | `nil` | Paths to exclude from comparison |
| `OnlyPaths` | `[]string` | `nil` | Only compare these paths |
| `IgnoreArrayOrder` | `bool` | `false` | Treat arrays as unordered sets |
| `IgnoreWhitespace` | `bool` | `false` | Ignore whitespace in string comparison |
| `OmitHeader` | `bool` | `false` | Omit header in formatted output |
| `ShowTypes` | `bool` | `false` | Show type information in output |

## Creating a Diff

Diff instances are created via the Engine:

```go
engine, _ := graft.NewEngine()

doc1, _ := engine.ParseFile("before.yml")
doc2, _ := engine.ParseFile("after.yml")

// Basic diff
diff := engine.Diff(doc1, doc2)

// Diff with options
diff := engine.DiffWithOptions(doc1, doc2, &graft.DiffOptions{
    IgnorePaths:      []string{"metadata.timestamp"},
    IgnoreArrayOrder: true,
})
```

## Access Methods

### Changes

Returns all changes between the documents.

```go
func (d *Diff) Changes() []Change
```

**Returns:**

- `[]Change` - Slice of all changes

**Example:**

```go
diff := engine.Diff(doc1, doc2)

for _, change := range diff.Changes() {
    fmt.Printf("%s at %s\n", change.Type, change.Path)
}
```

### HasChanges

Returns whether any differences exist.

```go
func (d *Diff) HasChanges() bool
```

**Returns:**

- `bool` - True if documents differ

**Example:**

```go
diff := engine.Diff(current, proposed)

if !diff.HasChanges() {
    fmt.Println("No changes detected")
    return
}

// Process changes
for _, change := range diff.Changes() {
    handleChange(change)
}
```

### Added

Returns only additions (paths in second document but not first).

```go
func (d *Diff) Added() []Change
```

**Returns:**

- `[]Change` - Slice of added changes

**Example:**

```go
for _, change := range diff.Added() {
    fmt.Printf("+ %s: %v\n", change.Path, change.NewValue)
}
```

### Removed

Returns only removals (paths in first document but not second).

```go
func (d *Diff) Removed() []Change
```

**Returns:**

- `[]Change` - Slice of removed changes

**Example:**

```go
for _, change := range diff.Removed() {
    fmt.Printf("- %s: %v\n", change.Path, change.OldValue)
}
```

### Modified

Returns only modifications (same path, different value).

```go
func (d *Diff) Modified() []Change
```

**Returns:**

- `[]Change` - Slice of modified changes

**Example:**

```go
for _, change := range diff.Modified() {
    fmt.Printf("~ %s: %v -> %v\n",
        change.Path, change.OldValue, change.NewValue)
}
```

### ChangesAtPath

Returns changes at or under a specific path.

```go
func (d *Diff) ChangesAtPath(path string) []Change
```

**Parameters:**

- `path` - Dot-notation path prefix

**Returns:**

- `[]Change` - Slice of changes at or under the path

**Example:**

```go
// Get all database-related changes
dbChanges := diff.ChangesAtPath("database")
for _, change := range dbChanges {
    fmt.Printf("%s: %v -> %v\n",
        change.Path, change.OldValue, change.NewValue)
}

// Get changes to a specific key
hostChanges := diff.ChangesAtPath("database.host")
```

## Output Formatters

### WriteSideBySide

Writes a side-by-side comparison of the documents.

```go
func (d *Diff) WriteSideBySide(w io.Writer, opts *DiffOptions) error
```

**Parameters:**

- `w` - Writer for output

- `opts` - Formatting options (nil for defaults)

**Returns:**

- `error` - Non-nil if writing fails

**Example:**

```go
diff := engine.Diff(before, after)

diff.WriteSideBySide(os.Stdout, &graft.DiffOptions{
    Color: true,
    Width: 120,
})
```

**Output:**

```
before.yml                              | after.yml
----------------------------------------|----------------------------------------
database:                               | database:
  host: localhost                       |   host: db.example.com
  port: 5432                            |   port: 5432
                                        |   pool_size: 10
server:                                 | server:
  port: 8080                            |   port: 9090
  debug: true                           |
```

### WriteUnified

Writes a unified diff format (similar to `diff -u`).

```go
func (d *Diff) WriteUnified(w io.Writer, opts *DiffOptions) error
```

**Parameters:**

- `w` - Writer for output

- `opts` - Formatting options (nil for defaults)

**Returns:**

- `error` - Non-nil if writing fails

**Example:**

```go
diff := engine.Diff(before, after)

diff.WriteUnified(os.Stdout, &graft.DiffOptions{
    Context: 3,
    Color:   true,
})
```

**Output:**

```
--- before.yml
+++ after.yml
@@ -1,6 +1,7 @@
 database:
-  host: localhost
+  host: db.example.com
   port: 5432
+  pool_size: 10
 server:
-  port: 8080
-  debug: true
+  port: 9090
```

### WriteChangeList

Writes a structured list of changes.

```go
func (d *Diff) WriteChangeList(w io.Writer, opts *DiffOptions) error
```

**Parameters:**

- `w` - Writer for output

- `opts` - Formatting options (nil for defaults)

**Returns:**

- `error` - Non-nil if writing fails

**Example:**

```go
diff := engine.Diff(before, after)

diff.WriteChangeList(os.Stdout, &graft.DiffOptions{
    ShowTypes: true,
})
```

**Output:**

```
Changes:
  MODIFIED database.host: "localhost" -> "db.example.com" (string)
  ADDED    database.pool_size: 10 (int)
  MODIFIED server.port: 8080 -> 9090 (int)
  REMOVED  server.debug: true (bool)

Summary: 2 modified, 1 added, 1 removed
```

### WriteMergeTree

Writes a merge tree showing the document structure with changes highlighted.

```go
func (d *Diff) WriteMergeTree(w io.Writer, opts *DiffOptions) error
```

**Parameters:**

- `w` - Writer for output

- `opts` - Formatting options (nil for defaults)

**Returns:**

- `error` - Non-nil if writing fails

**Example:**

```go
diff := engine.Diff(before, after)

diff.WriteMergeTree(os.Stdout, &graft.DiffOptions{
    Color: true,
})
```

**Output:**

```
database:
  ~ host: localhost -> db.example.com
    port: 5432
  + pool_size: 10
server:
  ~ port: 8080 -> 9090
  - debug: true
```

## Serialization Methods

### ToJSON

Serializes the diff to JSON format.

```go
func (d *Diff) ToJSON() ([]byte, error)
```

**Returns:**

- `[]byte` - JSON representation of the diff

- `error` - Non-nil if serialization fails

**Example:**

```go
diff := engine.Diff(before, after)

json, err := diff.ToJSON()
if err != nil {
    return err
}
fmt.Println(string(json))
```

**Output:**

```json
{
  "has_changes": true,
  "changes": [
    {
      "type": "modified",
      "path": "database.host",
      "old_value": "localhost",
      "new_value": "db.example.com"
    },
    {
      "type": "added",
      "path": "database.pool_size",
      "new_value": 10
    }
  ],
  "summary": {
    "added": 1,
    "removed": 1,
    "modified": 2
  }
}
```

### ToYAML

Serializes the diff to YAML format.

```go
func (d *Diff) ToYAML() ([]byte, error)
```

**Returns:**

- `[]byte` - YAML representation of the diff

- `error` - Non-nil if serialization fails

**Example:**

```go
diff := engine.Diff(before, after)

yaml, err := diff.ToYAML()
if err != nil {
    return err
}
fmt.Println(string(yaml))
```

**Output:**

```yaml
has_changes: true
changes:
  - type: modified
    path: database.host
    old_value: localhost
    new_value: db.example.com
  - type: added
    path: database.pool_size
    new_value: 10
summary:
  added: 1
  removed: 1
  modified: 2
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

    diff := engine.DiffWithOptions(oldDoc, newDoc, &graft.DiffOptions{
        IgnorePaths: []string{"metadata.generated_at"},
    })

    if !diff.HasChanges() {
        fmt.Println("Configurations are identical")
        return nil
    }

    fmt.Printf("Found %d changes:\n", len(diff.Changes()))
    diff.WriteChangeList(os.Stdout, &graft.DiffOptions{
        Color:     true,
        ShowTypes: true,
    })

    return nil
}
```

### Change Validation

```go
func validateChanges(before, after graft.Document) error {
    engine, _ := graft.NewEngine()

    diff := engine.Diff(before, after)

    // Check for disallowed changes
    for _, change := range diff.Removed() {
        if strings.HasPrefix(change.Path, "security.") {
            return fmt.Errorf("cannot remove security settings: %s", change.Path)
        }
    }

    // Validate modifications
    for _, change := range diff.Modified() {
        if change.Path == "database.host" {
            if err := validateHost(change.NewValue.(string)); err != nil {
                return fmt.Errorf("invalid database host: %w", err)
            }
        }
    }

    // Log additions for audit
    for _, change := range diff.Added() {
        log.Printf("New configuration: %s = %v", change.Path, change.NewValue)
    }

    return nil
}
```

### Generating Change Reports

```go
func generateChangeReport(before, after graft.Document) (*ChangeReport, error) {
    engine, _ := graft.NewEngine()

    diff := engine.Diff(before, after)

    report := &ChangeReport{
        Timestamp: time.Now(),
        Summary: ChangeSummary{
            Added:    len(diff.Added()),
            Removed:  len(diff.Removed()),
            Modified: len(diff.Modified()),
        },
        Changes: make([]ChangeEntry, 0, len(diff.Changes())),
    }

    for _, change := range diff.Changes() {
        entry := ChangeEntry{
            Path:   change.Path,
            Type:   changeTypeString(change.Type),
            Before: change.OldValue,
            After:  change.NewValue,
        }

        // Categorize by severity
        if strings.HasPrefix(change.Path, "security.") {
            entry.Severity = "high"
        } else if strings.HasPrefix(change.Path, "database.") {
            entry.Severity = "medium"
        } else {
            entry.Severity = "low"
        }

        report.Changes = append(report.Changes, entry)
    }

    return report, nil
}
```

### Diff with Array Handling

```go
func compareWithArrays(before, after graft.Document) {
    engine, _ := graft.NewEngine()

    // Standard comparison (arrays compared by index)
    strictDiff := engine.Diff(before, after)

    // Set-based comparison (arrays compared by content)
    setDiff := engine.DiffWithOptions(before, after, &graft.DiffOptions{
        IgnoreArrayOrder: true,
    })

    fmt.Println("Strict comparison:")
    strictDiff.WriteChangeList(os.Stdout, nil)

    fmt.Println("\nSet-based comparison:")
    setDiff.WriteChangeList(os.Stdout, nil)
}
```

**Example Documents:**

```yaml
# before.yml
servers:
  - web1
  - web2
  - web3

# after.yml
servers:
  - web2
  - web3
  - web1
```

**Strict Comparison:**
```
Changes:
  MODIFIED servers.0: "web1" -> "web2"
  MODIFIED servers.2: "web3" -> "web1"
```

**Set-Based Comparison:**
```
No changes detected
```

## Edge Cases

### Empty Documents

```go
empty, _ := engine.ParseYAML([]byte("{}"))
populated, _ := engine.ParseFile("config.yml")

diff := engine.Diff(empty, populated)
// All paths in populated are "added"

diff := engine.Diff(populated, empty)
// All paths in populated are "removed"
```

### Null Values

```go
// Document with explicit null
doc1, _ := engine.ParseYAML([]byte(`
key: null
`))

// Document without the key
doc2, _ := engine.ParseYAML([]byte(`
other: value
`))

diff := engine.Diff(doc1, doc2)
// key is "removed" (null is still a value)
```

### Type Changes

```go
// String value
doc1, _ := engine.ParseYAML([]byte(`
port: "8080"
`))

// Integer value
doc2, _ := engine.ParseYAML([]byte(`
port: 8080
`))

diff := engine.Diff(doc1, doc2)
// port is ChangeTypeChanged (string -> int)
```

## Thread Safety

The Diff interface is thread-safe for concurrent reads. Multiple goroutines can safely:

- Access changes

- Format output

- Serialize to JSON/YAML

## Related Documentation

- [Engine Interface](engine.md) - Creating diffs

- [Document Interface](document.md) - Working with documents

- [History Interface](history-api.md) - Tracking changes over time

# Comparing Raw YAML with yaml.Diff

`yaml.Diff` compares two YAML or JSON inputs and reports what changed. It lives in `pkg/yaml`, so a program can compare two files without building an `Engine` or parsing either input into a `pkg/graft` document. It uses the same comparison engine as the `graft diff` command, and the report it prints is the report `graft diff` prints.

When we already hold two `pkg/graft` documents, or when we need to cancel the comparison with a context, [Diff Interface](diff-api.md) describes `DiffResult` and `DiffContext`. This page is for raw bytes.

## Quick start

```go
import "github.com/fivetwenty-io/graft/pkg/yaml"

r, err := yaml.Diff(before, after, nil)
if err != nil {
    return err
}
if r.Empty() {
    return nil
}
for _, c := range r.Changes() {
    fmt.Println(c.Kind, c.Path)
}
r.Write(os.Stdout, yaml.ReportStyle{})
```

## Diff

```go
func Diff(a, b []byte, opts *DiffOptions) (*DiffReport, error)
```

`Diff` reads each input as a stream that may hold several documents, and it pairs the documents by position, except that a stream made only of named Kubernetes resources, each with `apiVersion`, `kind`, and `metadata.name`, is paired by name. It reads YAML and JSON. It doesn't read TOML, which `graft diff` reads only when it opens a file. TOML that starts with a table header fails to parse, and TOML made of `key = value` lines reads as one YAML string.

A nil `opts` selects the defaults, which compare the way `graft diff` does.

Two kinds of error come back.

- A `*yaml.DecodeError` means one of the inputs failed to parse. Its text never quotes the input.

- A `*yaml.DiffError` means the inputs parse but can't be compared, for example because they hold different numbers of documents. Its `Reason` is fixed text that never quotes the input, and `Unwrap` returns the error from the comparison.

## DiffOptions

```go
type DiffOptions struct {
    IgnoreOrderChanges bool
}
```

`IgnoreOrderChanges` leaves out the changes that only reorder a list or the named Kubernetes resources of a stream. Every other change is still reported. Swapping two ordinary documents is not an order change, because they are paired by position. It gives a removed and an added change at each of the two positions, and `IgnoreOrderChanges` leaves all four in place.

## Change and ChangeKind

```go
type Change struct {
    Document int
    Path     string
    Kind     ChangeKind
    From     *Node
    To       *Node
}
```

`Document` is the index of the document in the stream, counting from 0, or -1 for a change in the order of named Kubernetes resources. No other change has a document index of -1.

`Path` locates the change in the cursor syntax that [Editing a File](yaml-document.md#paths) describes. It reads `j.name=web.p` for a field of the list entry named `web`, `m.[a.b]` for a key that holds a dot, and `j.[name=a.b].p` for a list entry whose name holds a dot. A change at the top of a document has an empty path. A `Document` that reads the input reads the path as the value the change is about, so `doc.Set(c.Path, v)` edits that value and no other.

Some names can't be written in the cursor syntax. These are an empty key, a key or a name that holds `[` or `]`, a top-level key that is just `$`, and a Kubernetes resource in a list, which has no single field that names it. A name that the syntax would read as a different value, such as an alias key whose anchor name is also a key of the same map, is treated the same way. The path segment for such a name starts with `]`, so `Get`, `Set`, `Delete`, and `Editable` refuse the whole path with `ErrInvalidPath` rather than act on some other value. The rest of the path is still there for a person to read.

`From` and `To` hold the nodes on either side. A `Node` that is missing on one side is nil.

`ChangeKind` tells what happened to the value at the path.

| Kind | Meaning | String |
|------|---------|--------|
| `ChangeAdded` | Entries were added to the collection at `Path` | `added` |
| `ChangeRemoved` | Entries were removed from the collection at `Path` | `removed` |
| `ChangeModified` | The value at `Path` changed | `modified` |
| `ChangeOrder` | The entries at `Path`, or the named Kubernetes resources of the stream, were reordered | `order` |

For an addition or a removal, `Path` points at the collection that gained or lost entries, and not at the entries themselves. Adding `n: 3` at the top of a document gives an added change with an empty path, and adding `y` to the list at `l` gives an added change at `l`. The entries that arrived are in `To`, and the entries that left are in `From`. For a modification, `Path` points at the value that changed.

## DiffReport

```go
func (r *DiffReport) Changes() []Change
func (r *DiffReport) Empty() bool
func (r *DiffReport) Write(w io.Writer, style ReportStyle) error
```

`Changes` returns a copy of the changes in report order, so a caller can sort or trim it freely. `Empty` reports whether the inputs have no differences.

`Write` prints the report the way `graft diff` prints it, without the final blank line that `graft diff` adds after it. A report that can't be rendered comes back as a `*yaml.DiffError`, and an error from the writer comes back as it is. A changed `!!binary` value whose base64 text doesn't decode is a different case. `Write` returns that decoding error as it is, after it has written the rest of the report, so the writer may hold partial output. The `graft diff` command swallows this error, but a library caller should see it.

## ReportStyle

```go
type ReportStyle struct {
    Color     bool
    TrueColor bool
    Width     int
}
```

The zero value prints plain text in the narrow layout that `graft diff` uses when it can't read a terminal width. `Color` turns on colors, `TrueColor` allows 24-bit colors, and `Width` sets the layout width in columns.

## What counts as a change

In version 1, comments and scalar styles don't count. `a: 1 # note` against `a: 1`, and `'x'` against `x`, give an empty report, because the values are the same. If a program needs to notice a comment edit or a quoting change, compare the bytes of the inputs as well.

## See also

- [Editing a File with yaml.Document](yaml-document.md) changes a value without reformatting the rest of the file.

- [Diff Interface](diff-api.md) describes the `pkg/graft` side, including `DiffContext`.

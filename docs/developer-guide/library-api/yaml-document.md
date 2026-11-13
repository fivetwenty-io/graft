# Editing a File with yaml.Document

`yaml.Document` is for changing a few values in a file that people maintain by hand without reformatting the rest of it. It holds the file as the bytes it was read from, and each edit changes only the bytes it names. Comments, blank lines, quoting, indentation, and line breaks everywhere else stay exactly as they were written.

This is not the `Document` that `pkg/graft` uses. The `Document` in [document.md](document.md) holds decoded Go values for merging and evaluation, and it writes its output fresh every time. The `Document` on this page lives in `pkg/yaml`, and it keeps the original text. Reach for `pkg/graft` when we are merging or evaluating, and reach for `pkg/yaml` when we are patching a file in place.

## Quick start

```go
import "github.com/fivetwenty-io/graft/pkg/yaml"

d, err := yaml.ParseDocument(data)
if err != nil {
    return err
}
if err := d.Set("port", 9090); err != nil {
    return err
}
if err := d.Delete("legacy"); err != nil {
    return err
}
out := d.Bytes()
```

With the input `# service`, `name: web   # shown in the UI`, and `port: 8080` on three lines, the `Set` above changes the one number and leaves the comment and the spacing alone.

## The methods

`ParseDocument(data)` reads the bytes as a YAML stream and keeps its own copy of them. It accepts anything `Unmarshal` accepts, including empty input, several documents, CRLF line breaks, tabs, and a byte order mark. A syntax error comes back as a `*yaml.DecodeError`. A path can only name a node in a stream that holds exactly one document, so the other methods refuse a stream with several documents using `ErrAmbiguousPath`, and an empty stream using `ErrPathNotFound`.

`Bytes()` returns a copy of the text. Until an edit succeeds, it is the input byte for byte.

`Get(path)` returns the `*yaml.Node` that the path names. Its `Line` and `Column` come from the input, and the tree under it is the one that `Unmarshal` into a `Node` gives for the same text. A path that reaches an alias returns the node the alias refers to. `Get` never expands a `<<` merge, so a key that only a merge brings in isn't found. The node is a copy, so changing it doesn't change the `Document`.

`Editable(path)` reports whether `Set` could replace the value at the path. It returns nil when it could, and otherwise returns the error `Set` would return. It checks the path and the slot but not the value, so `Set` can still refuse a value that `Editable` passed. A caller can use it to check a whole list of paths before it makes the first edit.

`Set(path, value)` replaces one scalar and changes no other byte.

`Delete(path)` removes one mapping entry or sequence item, along with its own comment lines.

A `Document` is not safe for concurrent use.

## Paths

A path names a node the way graft's cursor paths do. The segments are separated by dots.

| Form | What it names |
|------|---------------|
| `a.b` | The key `b` inside the key `a` |
| `list.0` | The sequence item at index 0 |
| `jobs.name=web` | The first item of `jobs` that is a mapping whose `name` field holds `web` |
| `jobs.web` | The first item of `jobs` whose `name`, `key`, or `id` field holds `web`, tried in that order |
| `meta.[a.b]` | The key spelled `a.b` inside `meta`, where the brackets keep the dot from splitting it |

Each form works for `Get`, `Editable`, `Set`, and `Delete`, and they combine, so `jobs.name=web.port` names the `port` field of the `web` job.

In a mapping, a segment must match a key's text exactly, and a key that appears twice is ambiguous. A path that names nothing is `ErrPathNotFound`, and a path that can't be read, such as one with an unclosed `[`, is `ErrInvalidPath`. A `Document` never adds a key, so `Set` on a path that names nothing fails and doesn't create it.

## What Set writes

`Set` accepts a `string`, a `bool`, any Go integer or float type, and an untyped `nil`. A named type over one of these, a slice, a map, and a struct are all refused with `ErrUnsupportedEdit`.

It replaces the text of one plain, single-quoted, or double-quoted scalar that sits on one line, which may be inside a flow collection. It also replaces the body of a literal or folded block scalar, as the next section describes.

How a string is written depends on the slot it goes into.

- A slot in double quotes keeps its double quotes.

- A slot in single quotes keeps its single quotes, unless the string holds a line break or a character that needs an escape. In that case the string is double-quoted.

- A plain slot takes the style `Marshal` would give the string. So `yes` and `1.0` come out quoted, because written plain they would read back as a boolean and a number. A string that `Marshal` writes plain but the parser reads as something else, such as `<<`, is double-quoted.

- A string with a line break is written in double quotes on one line, with an escape for the break.

- Inside a flow collection, a string that holds a flow indicator is double-quoted.

Every other value is written plain, the way `Marshal` writes it. That means a quoted slot loses its quotes when it receives a number, a boolean, or a null. An integer or float keeps the text `Marshal` gives it, and `nil` writes `null`.

Setting a value to what the slot already holds succeeds and changes nothing.

## Block scalars

A literal (`|`) or folded (`>`) block scalar takes only a string, and the string replaces the body. The header line and its comment stay as written. The body keeps its indentation, or takes the header line's own indentation plus 2 when it had no text.

The value must fit the header's chomping indicator.

- A clip header (`|` or `>` alone) takes an empty value or one that ends in exactly one line break.

- A strip header (`|-` or `>-`) takes a value with no final line break.

- A keep header (`|+` or `>+`) takes a value with at least one final line break, and each extra break is written as a blank line.

A value whose first line with text starts with a space or a tab fits no header. Neither does a value that holds a carriage return or U+0085. In a folded scalar, no line may start with a space or a tab. A folded body writes each line break between two lines of text as two breaks, and it never folds a long line, so the text reads back the same. `Set` refuses a header that has an indentation indicator, such as `|2`.

These are the edge cases we know about.

- The body of a block scalar starts at the line break after its header. A `|+` header at the very end of the input reads as the single value `"\n"`, so `Set` with `"\n"` there changes nothing.

- A clip header at the end of an input that has no final line break accepts only the empty string.

- A block scalar with an empty body, as in the sequence item `- a: |`, is refused.

## What Delete takes with it

`Delete` removes the lines that run from the entry's key, or from a sequence item's dash, through the last line of its value. Those lines include the following.

- The comment lines directly above the key or dash, at its own indentation.

- Every line under the entry that is indented further.

- When the entry carries a foot comment, the comment lines at its indentation directly after it.

Blank lines before and after the entry stay, and so do the comments that belong to the next entry. When the input has no final line break and the last line goes, the line break that ends the line before it stays.

`Delete` refuses the root, the only entry of a collection, an entry of a flow collection, a `<<` merge key, a key that shares its line with a sequence dash, and any other entry that doesn't start its own line. It refuses an entry whose value is an alias, an entry reached through an alias, and an entry that holds a node an alias outside it refers to or that sits in an anchored node an alias refers to. It refuses an entry with a tab on any line it would remove. And it refuses any entry of an input that holds a carriage return or starts with a byte order mark.

These are the limits we know about.

- A `|+` entry leaves its trailing blank lines behind.

- A `#` line inside a multi-line quoted scalar, and continuation lines that aren't indented, cause a refusal. So does a foot comment that the parser attaches to the value node and not to the key.

- Deleting a key that overrides a key brought in by `<<` reveals the merged value. Removing the override is what the call asked for, so `Delete` doesn't refuse.

## Refusals and errors

Every error from `Get`, `Editable`, `Set`, and `Delete` is a `*yaml.DocumentError`. It has an `Op` of `get`, `set`, `delete`, or `edit`, the `Path` the caller gave, the 1-based `Line` of the node involved or 0, and a `Reason`. The reason is fixed text and never quotes the input. `Error()` reads `yaml: set port: <reason>`, with ` at line <n>` after it when the error has a line. `Unwrap` returns one of four sentinel errors, so `errors.Is` tells the kinds apart.

| Sentinel | Meaning |
|----------|---------|
| `ErrPathNotFound` | The path names nothing |
| `ErrInvalidPath` | The path itself can't be read |
| `ErrAmbiguousPath` | The path could name more than one node |
| `ErrUnsupportedEdit` | The edit can't be made by changing only the text it names |

These are the reasons, grouped by the sentinel they wrap. The table paraphrases the reason text and leaves out the fill-ins, such as a position or a Go type. A caller should branch on the sentinel with `errors.Is` and should not match on the text of a `Reason`.

| Reason | Sentinel |
|--------|----------|
| The path has a syntax problem, or an unclosed `[` | `ErrInvalidPath` |
| The input holds no document | `ErrPathNotFound` |
| The document holds no value | `ErrPathNotFound` |
| The path continues past a scalar | `ErrPathNotFound` |
| The key is not in the mapping | `ErrPathNotFound` |
| No item has the field with that value | `ErrPathNotFound` |
| The index is past the end of the sequence | `ErrPathNotFound` |
| No item has that name | `ErrPathNotFound` |
| The input holds more than one document | `ErrAmbiguousPath` |
| The key appears more than once in the mapping | `ErrAmbiguousPath` |
| The input holds a carriage return | `ErrUnsupportedEdit` |
| The input starts with a byte order mark | `ErrUnsupportedEdit` |
| The path passes through an alias | `ErrUnsupportedEdit` |
| The value is an alias | `ErrUnsupportedEdit` |
| The value is anchored | `ErrUnsupportedEdit` |
| The value has a tag | `ErrUnsupportedEdit` |
| The value is a collection | `ErrUnsupportedEdit` |
| The value has no text to replace | `ErrUnsupportedEdit` |
| The value is a multi-line flow scalar | `ErrUnsupportedEdit` |
| A tab sits on a line the edit touches | `ErrUnsupportedEdit` |
| The value's source position is not exact | `ErrUnsupportedEdit` |
| A block scalar takes a string | `ErrUnsupportedEdit` |
| The block scalar has an indentation indicator | `ErrUnsupportedEdit` |
| The value does not fit the block scalar's header | `ErrUnsupportedEdit` |
| `Set` does not write a value of that Go type | `ErrUnsupportedEdit` |
| The path names the document's root | `ErrUnsupportedEdit` |
| The entry is the only one in its collection | `ErrUnsupportedEdit` |
| The entry is in a flow collection | `ErrUnsupportedEdit` |
| The key shares its line with a sequence dash | `ErrUnsupportedEdit` |
| The entry does not start its own line | `ErrUnsupportedEdit` |
| The entry is a merge key | `ErrUnsupportedEdit` |
| The edit would change another part of the document | `ErrUnsupportedEdit` |

The last reason comes from a check that runs after every edit. `Set` and `Delete` parse the edited text again, and they refuse unless the new tree is the old one with exactly that change made, every other node keeps its comments and its place in the text, and every byte outside the edit is unchanged. A refused edit leaves the bytes as they were, so a caller can try another approach on the same `Document`.

## What version 1 does not do

The first version is deliberately narrow, and it stays on the safe side whenever it can't be sure.

- It never adds a key or a sequence item, and it never reformats anything.

- It never changes a collection. `Set` takes a scalar, and `Delete` removes a whole entry.

- It refuses carriage returns and tabs on a line an edit touches. `Set` also refuses tagged and anchored values, and `Delete` refuses an anchor that an alias still needs.

- It refuses to set a plain scalar that has spaces after it at the end of its line, and the error reports that as "the value's source position is not exact". Removing the trailing spaces lets the edit through.

- It doesn't expand a `<<` merge, so it edits the document as written.

When a refusal blocks an edit we need, the answer is usually to make the change by hand or to read the file with `Unmarshal` and write it back with `Marshal`, accepting that the second route rewrites the layout.

## See also

- [Diff API for raw YAML](yaml-diff.md) compares two inputs and reports what changed.

- [Document Interface](document.md) describes `pkg/graft`'s `Document`.

- [Coming from goccy](coming-from-goccy.md) maps goccy's `parser.ParseBytes` and `ast` editing to this type.

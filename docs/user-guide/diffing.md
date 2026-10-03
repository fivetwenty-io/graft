# Diff & Comparison

Graft provides semantic comparison of YAML/JSON documents, in several
output formats, using a report that matches `spruce diff`.

## Overview

Unlike text-based diff tools, graft compares documents **semantically**:

- Key order doesn't matter
- Equivalent values are equal (e.g. `true` and `True`)
- Type-aware comparison (`8080` and `"8080"` are different)
- Understands YAML/JSON structure

All examples below are real output, captured against:

```yaml
# base.yml
database:
  host: localhost
  port: 5432
  timeout: 30
meta:
  version: "1.0"
```

```yaml
# modified.yml
database:
  host: db.prod.example.com
  port: 5432
  timeout: 60
  ssl: true
```

## Diff Formats

### Default (Human Report)

```sh
graft diff base.yml modified.yml
```

**Output:**
```

(root level)
- one map entry removed:
meta:
  version: "1.0"

database
+ one map entry added:
ssl: true

database.host
± value change
- localhost
+ db.prod.example.com

database.timeout
± value change
- 30
+ 60


```

This is graft's own spruce-compatible human-readable report. `graft diff`
with no flags prints the same bytes `spruce diff` prints, apart from the
differences listed under [Accepted differences from
spruce](#accepted-differences-from-spruce). Exit code `1` (differences
found).

### Change List

```sh
graft diff --changes base.yml modified.yml
```

**Output:**
```
Changes (2 modified, 1 added, 1 removed):

  MODIFIED  database.host
            - localhost
            + db.prod.example.com

  MODIFIED  database.timeout
            - 30
            + 60

  ADDED     database.ssl
            + true

  REMOVED   meta
            - version: "1.0"
```

### Unified (Git-Style)

```sh
graft diff --unified base.yml modified.yml
```

**Output:**
```diff
--- base.yml
+++ modified.yml
@@ database @@
-  host: localhost
+  host: db.prod.example.com
   port: 5432
-  timeout: 30
+  ssl: true
+  timeout: 60
@@ meta @@
-  version: "1.0"
```

Hunks are grouped per top-level key (`@@ <key> @@`), each with its own
git-style unified diff body, rather than one hunk per contiguous line
range across the whole file. Context lines default to 3 (git's `-u`
default); control it with `--context`:

```sh
graft diff --unified --context=0 base.yml modified.yml
```

### Side-by-Side

```sh
graft diff --side-by-side base.yml modified.yml
```

**Output** (captured at `--width=70` for a narrower example; the default
total width is 80):
```
base.yml                          │ modified.yml
──────────────────────────────────┼──────────────────────────────────
database:                         │ database:
  host: localhost                 │   host: db.prod.example.com
  port: 5432                      │   port: 5432
  timeout: 30                     │   ssl: true
meta:                             │   timeout: 60
  version: "1.0"                  │ 
```

Rows are aligned by a line-level diff of each file's full YAML text (via
[`pmezard/go-difflib`](https://github.com/pmezard/go-difflib)'s
longest-matching-block (Ratcliff/Obershelp) implementation), so an
insertion/deletion shifts the alignment rather than comparing files
line-by-line positionally. `-y` is the short form.

Control the total width (both columns plus the separator) with `--width`:

```sh
graft diff --side-by-side --width=160 base.yml modified.yml
```

## Change Types

| Type | Symbol (default/`--changes`) | Description |
|------|--------|-------------|
| Added | `+` | Key exists only in the second file |
| Removed | `-` | Key exists only in the first file |
| Modified | `±` (default) / `MODIFIED` (`--changes`) | Value changed |

Type changes (e.g. `8080` → `"8080"`) show up in the default report as
`± type change from int to string`, with both the old and new value
visible. `--changes` reports them as MODIFIED, since it has no separate
symbol for a type change.

## Color Coding

Output is colorized by default when writing to a terminal, following
graft's global `--color`/`--no-color` flags (see
[CLI Reference: Color flags](../reference/cli.md#color-flags)); `diff`
doesn't have flags of its own for this.

Disable color for one invocation:

```sh
graft diff --no-color base.yml modified.yml
```

`--color off` does the same thing:

```sh
graft diff --color off base.yml modified.yml
```

## Semantic Comparison

### Key Order Independence

```yaml
# file1.yml
database:
  host: localhost
  port: 5432

# file2.yml (same content, different order)
database:
  port: 5432
  host: localhost
```

```sh
graft diff file1.yml file2.yml
# No output, exit code 0: the two documents are semantically identical
```

### Value Equivalence

These are considered equal:

```yaml
# file1.yml
enabled: true
count: 42

# file2.yml
enabled: True      # Same as true
count: 42           # Same number
```

### Type Awareness

These are considered different:

```yaml
# file1.yml
port: 8080        # integer

# file2.yml
port: "8080"      # string
```

```
port
± type change from int to string
- 8080
+ "8080"
```

## Merge Change Tracking

`graft diff` compares two already-written files. To see what a *merge*
changed or would change, use `merge`'s own history/change flags — see
[History Tracking](history-tracking.md) for `--show-changes` and
`--changes-only` (these are `merge` flags, not `diff` flags; `graft diff`
has no `--show-changes`/`--changes-only` of its own).

## Practical Examples

### Pre-Deploy Validation

```sh
# Compare current vs proposed config
graft diff production-current.yml production-new.yml

# Only deploy if different
if ! graft diff --quiet current.yml new.yml; then
  echo "Changes detected:"
  graft diff --changes current.yml new.yml
  kubectl apply -f new.yml
fi
```

### Environment Comparison

```sh
# Compare dev and production
graft diff --changes envs/dev.yml envs/prod.yml
```

### Merge Preview

```sh
# See what merge would change
graft merge base.yml overlay.yml > merged.yml
graft diff base.yml merged.yml
```

### CI/CD Checks

```sh
#!/bin/bash
# Fail if config changed unexpectedly

graft merge base.yml env.yml > actual.yml

if ! graft diff --quiet expected.yml actual.yml; then
  echo "Configuration mismatch!"
  graft diff --side-by-side expected.yml actual.yml
  exit 1
fi
```

### Configuration Audit

```sh
# Generate change report
echo "# Configuration Changes"
echo "Generated: $(date)"
echo
graft diff --changes old-config.yml new-config.yml
```

## Input Parsing

`graft diff` reads its two inputs with graft's own YAML reader, which is
built on goccy/go-yaml and made to behave like the yaml.v3 reader spruce
uses. The two readers agree on almost every input. The subsections below
cover the places where they don't, so a diff that surprises you can be
traced to its cause.

### Template placeholders

An unquoted `{{...}}` placeholder in a value position reads as a plain
string. That matters for files such as Concourse pipeline templates, where
`a: {{x}}` against `a: {{y}}` reports a value change:

```
a
± value change
- {{x}}
+ {{y}}
```

spruce parses `{{x}}` as an empty nested map, so it reports no difference
between those two files, and it panics when the placeholders sit in a
simple list. `graft merge` reads the same placeholder as a string and
writes it back quoted, so `a: {{x}}` merges to `a: '{{x}}'`. `graft json`
and files pulled in with `(( load ))` read placeholders the same way, so a
Concourse file behaves the same wherever graft reads YAML.

The rewrite applies only in value positions. A placeholder used as a
mapping key, and a run of unbalanced braces such as `{{{{`, stay parse
errors. The rewrite also doesn't reach a placeholder that follows an
anchor or a tag, so `a: &n {{x}}` and `a: !!str {{x}}` stay parse errors
too. A triple-brace value such as `{{{x}}}` is rewritten, and it becomes
the string `{{{x}}}`.

### Parse errors

When an input does not parse, `graft diff` prints `unable to parse data
from <file>: yaml: ` and exits `2`, as spruce does, and then prints
goccy's message. A malformed file inside a directory input prints
`failed to read <path>: yaml: ` instead, as spruce does. The line number
matches spruce's where libyaml and goccy agree. For some errors it
differs, namely unterminated quotes, unclosed flow collections, bad
indentation, a document that opens as plain text over several lines
before a `key: value` line, and errors where spruce prints no line at
all. In the plain-text case spruce reports the line of the colon and
graft reports line 1, which you see when you diff an HTML page or a
directory listing fetched from a URL. `TestParseErrorLineDivergences` in
`internal/yamlnode` records those cases, and the [Genesis compatibility
contract](../spruce/genesis-compat-contract.md#parse-errors) lists them
in a table.

### Nesting depth and self-referencing anchors

Like spruce, `graft diff` stops at 10,000 levels of nesting. It counts
open flow collections and open block collections separately, and an
input one level past either limit fails with `yaml: exceeded max depth of
10000` and exit `2`. An anchor whose value contains an alias to itself,
such as `a: &x [*x]`, fails with `yaml: anchor 'x' value contains
itself` and exit `2`. spruce overflows its stack on that input and then
exits `2`, so graft's message is clearer and the exit code is the same.

Two accepted differences come with the depth limit. When a file has both
a syntax error and nesting deeper than 10,000 levels, graft reports the
depth error even if the syntax error comes first, because it checks depth
before it parses. spruce reports the error it reaches first. graft
emulates libyaml's rules for the line number in a depth error, so it can
differ from the line spruce prints. Both tools still exit `2` in both
cases. `TestParseDepthLimit` and `TestParseDepthErrorBeatsSyntaxError`
in `internal/yamlnode` pin them, and `TestDiffRejectsSelfReferencingAnchor`
in `cmd/graft` pins the anchor error.

### Accepted differences from spruce

These are the deliberate differences between `graft diff` and `spruce
diff`. Each one is pinned by a test, so a change to any of them is
caught.

graft accepts these inputs, and spruce rejects them:

- The `\/` escape in a double-quoted string.
- Raw control characters.
- A stream that holds only `...`.
- A `%YAML 1.2` directive.
- A tab after a block dash, such as `-` then a tab and `b`.
- A `...` line before the first content.
- A mapping key longer than 1,024 characters.
- A line-separator (U+2028) character inside a plain scalar. spruce reads
  it as a line break, and graft keeps it as part of the text.

graft rejects these inputs, and spruce accepts them:

- Complex mapping keys.
- An empty tagged value followed by another key, such as `a: !!str` and
  then `b: 1`.
- `!!merge <<` used as a value.
- A `%TAG !!` directive that redefines the secondary handle, when a tag
  then uses `!!`, such as `a: !!foo 1`.
- Empty tagged sequence items, such as `- !!str` then `- !!int`, which
  is the same difference as `a: !!str` before another key.
- An anchor with no value as the last thing in a document that ends at
  a `---` line or at the end of the file, such as `b: &x` as the last
  entry of a mapping or `- &x` as the last item of a list. graft fails
  with `yaml: line N: undefined anchor value`. Blank lines and comments
  after the anchor don't change that, but a document that ends with
  `...` parses.

`TestParseAcceptanceDivergences` pins the `%YAML`, empty tagged value, `!!merge`, and `%TAG !!` cases. `TestParseAcceptsInputsYamlV3Rejects` pins the `\/` escape and raw control characters. `TestParseAcceptsInputsSpruceRejects` pins the tab, leading `...`, long key, and U+2028 cases. `TestParseEmptyTaggedSequenceItemsFail` and `TestParseRejectsAnchorWithoutValue` pin the tagged sequence items and the anchor with no value. The `...`-only stream and complex mapping key cases are pinned in `internal/yamlnode`'s split and decode tests. Content that follows `...` without a new `---` line is no longer a difference. `graft diff`, `graft merge`, and `graft json` all fail it with exit code 2 and spruce's message, `yaml: line N: did not find expected <document start>`.

`graft merge` and `graft json` also reject a `...` line before the first content and a `%YAML 1.2` directive, as spruce does, so those two differences apply to `graft diff` alone.

One block scalar reads differently. When a document is a block scalar
that starts on its `---` line and has an explicit indentation indicator,
as in `--- |1-` followed by two spaces and `x`, graft keeps one more
space of indentation than spruce does. spruce reads `" x"` for that
input, and graft reads `"  x"`. The `|1`, `>1-`, and `|2` forms behave
the same way, while an indicator on a mapping value, such as `a: |1-`,
matches spruce. `TestParseIndentIndicatorOnDocumentScalar` pins graft's
value.

One comment layout lands in a different place. When a comment block sits
between a key and its block collection, and a later line of the block
starts left of the key, graft keeps the earlier line as the key's foot
comment, where yaml.v3 gives it to the first item. In this document, graft
attaches `# c22` to `y0`, and yaml.v3 attaches it to the first item:

```yaml
a:
  y0:
  # c22
# c1
  - 1
```

Both tools give `# c1` to the first item.
`TestCommentsDedentedBlockDivergence` pins graft's placement.

This is the only comment-placement difference left in our test corpus, and we have chosen not to close it. Fixing it means reproducing libyaml's rules for every kind of dedent, which risks moving shapes that already match. It affects 7 of the 1,500 generated files we sampled. spruce accepted 834 of those files, and the 7 are among them. The generator writes dedented comments far more often than real files do, so we expect the rate in real files to be much lower.

Where spruce panics, graft exits `2` with an error message instead of a
stack trace. That covers an SGR `38` sequence without valid arguments
inside a value and a tag of exactly `!`.

TOML local dates and times print in the machine's local zone, as they do
in spruce. spruce reads the machine's local zone when it starts, and graft
reads it when it loads the file, so the two agree unless the zone changes
in between.

graft reads a CRLF file exactly as it reads the same file with LF line
endings. spruce prints an extra blank line above a commented key in a CRLF
file, and graft does not.

The same difference shows up in a header comment. In a CRLF file, spruce keeps an extra blank line after a head comment on the `---` line. For `--- # h` followed by `b: 2`, diffed against `a: 1`, spruce prints a blank line between `# h` and `b: 2`, and graft prints none. When a second comment line follows the header, spruce prints a blank line after each of the two comments, and graft prints neither. We have not closed this one. The number of extra blank lines depends on what follows each comment, so a rule that guesses it would move graft away from spruce on other shapes.

The change list, the unified format, and the side-by-side format are graft's own, so spruce has no output for them to match. When the two files hold different numbers of documents, all of them exit `2` with `comparing YAMLs with a different number of documents is currently not supported`, as the default report does. The default report prints that text bare, as spruce does. The three graft-only formats put `Error comparing A and B:` in front of it, naming both files, and they do the same for any other error raised while they compare the documents. We keep the prefix on purpose, because it names the files and a script may already match on it. A parse error is not affected, and it prints the same `unable to parse data from <file>` line in every format.

## Exit Codes

| Code | Description |
|------|-------------|
| 0 | Files are semantically identical |
| 1 | Files differ |
| 2 | Error (file not found, parse error) |

`--quiet` suppresses all output but keeps these exit codes, for scripting:

```sh
if graft diff --quiet file1.yml file2.yml; then
  echo "No changes"
else
  echo "Files differ"
fi
```

## Flags Not Implemented

`--ignore-paths`/`--only-paths` (filtering the diff to exclude/include
specific paths) are not implemented. Combine `graft diff --changes` with
`grep`/`jq`-style post-filtering if you need this today.

## Library API

`pkg/graft` exports `DiffDocuments(a, b Document, opts *DiffOptions)
(DiffResult, error)` for comparing two documents from Go code. It also
exports a lower-level `Diff(a, b interface{}) (Diffable, error)` helper
(spruce-inherited), which has no non-test callers anywhere in graft.

Neither one is part of the `diff` command's own code path. `graft diff`
builds on graft's internal comparison and report packages, so treat
`Diff` as an implementation detail, not a supported API.

## See Also

- [diff Command](cli/diff.md) - CLI reference
- [History Tracking](history-tracking.md) - Merge history
- [merge Command](cli/merge.md) - Merge with change tracking

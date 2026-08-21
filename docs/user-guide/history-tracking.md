# History Tracking

Graft can track how each value in a merged document was derived: which
input file first set it, how it changed as later files overwrote it, and
what it resolved to after operator evaluation.

## Overview

History tracking answers the question: "Where did this value come from?"

For each path in the final document, graft can show:

- Which file(s) touched it, in order
- Its value at each step
- Whether operator evaluation changed it
- Its final value

Graft has no per-value line-number tracking (the position information that
does exist is scoped to a single `(( ... ))` expression's own tokens, not
to merged document values), so history entries identify their source by
**file, not file:line**.

## Enabling History

```sh
graft merge --history base.yml overlay.yml secrets.yml
```

When any of `--history`/`--trace-path`/`--show-changes`/`--changes-only`
are given, `merge` prints that report instead of the merged document. They
are mutually exclusive — combining more than one is a usage error.

The flags above are backed by `internal/history`/`internal/histdiff`, a
CLI-only snapshot-diff mechanism that re-runs the merge once per input
file to capture each one's contribution (see Performance Considerations
below). A separate library API also exists -
[`graft.History`/`MergeBuilder.TrackHistory()`](../developer-guide/library-api/history-api.md)
- backed by a different mechanism (`DocumentMemory`, wired directly into
the merge and evaluation paths) with different coverage: it records
map-key changes during merge and evaluation, but never a list-element
mutation, and a newly added nested subtree only at its top-level key, not
at every descendant path (see the [History API's gap list](../developer-guide/library-api/history-api.md#what-is-actually-recorded)
for the exact rule). It also only records when a caller explicitly
enables tracking. The two mechanisms do not share code or data; a library
caller cannot use one to produce the other's output.

## History Output

The examples below are real output, captured against this fixture:

```yaml
# base.yml
database:
  host: localhost
  port: 5432
  pool_size: 10
meta:
  version: "1.0"
```

```yaml
# env.yml
database:
  host: db.prod.example.com
  pool_size: 50
server:
  timeout: 60
```

```yaml
# secrets.yml
database:
  password: (( grab meta.version ))
```

### Full History

```sh
graft merge --history base.yml env.yml secrets.yml
```

**Output:**
```
Merge History:

database.host:
  [0] base.yml       → localhost
  [1] env.yml        → db.prod.example.com
  Final              → db.prod.example.com

database.password:
  [2] secrets.yml    → (( grab meta.version ))
  [3] <evaluated>    → "1.0"
  Final              → "1.0"

database.pool_size:
  [0] base.yml       → 10
  [1] env.yml        → 50
  Final              → 50

database.port:
  [0] base.yml       → 5432
  Final              → 5432  (unchanged)

meta.version:
  [0] base.yml       → "1.0"
  Final              → "1.0"  (unchanged)

server.timeout:
  [1] env.yml        → 60
  Final              → 60  (unchanged)
```

Every path in the final document gets an entry, including ones no later
file ever touched — those are marked `(unchanged)`. The `[N]` index is the
step's position across the whole merge (every input file in order, then
one synthetic evaluation step); it is not reset per path, so a path whose
history starts partway through (like `database.password`, first set by the
third file) starts at that file's own step index.

### Trace a Specific Path

```sh
graft merge --trace-path database.password base.yml env.yml secrets.yml
```

**Output:**
```
database.password:
  [2] secrets.yml    → (( grab meta.version ))
      Type: operator (grab)

  [3] <evaluated>    → "1.0"
      Type: value

  Final              → "1.0"
```

`Type` classifies the raw value at that step: `operator (<name>)` when it
is still an unevaluated `(( name ...` expression, `removed` for a
`<pruned>` entry, or `value` otherwise.

A path with no recorded history is an error, not an empty report:

```sh
$ graft merge --trace-path no.such.path base.yml env.yml
No history found for path no.such.path
```
(exit code 2)

### Show Changes

```sh
graft merge --show-changes base.yml env.yml secrets.yml
```

**Output:**
```
Merge Summary: 3 files → 6 keys (3 changed, 1 added, 0 removed)

database.host:
  ✗ base.yml         localhost
  ✓ env.yml          db.prod.example.com

database.password:
  ✗ secrets.yml      (( grab meta.version ))
  ✓ <evaluated>      "1.0"

database.pool_size:
  ✗ base.yml         10
  ✓ env.yml          50

server.timeout:
  + env.yml          60
```

Paths present in the first file and never touched again (`database.port`,
`meta.version` in this fixture) are omitted entirely — this report is
about what changed, not the whole document.

**Legend:**

- ✓ Final value used
- ✗ Value overwritten
- ○ Final value is still an unevaluated `(( ... ))` expression (only
  reachable with `--skip-eval`, or for a `(( defer ))`-style value that
  evaluation leaves alone)
- \+ Added (first appears after the first file)
- \- Removed (see `--prune`/`--cherry-pick` below)

Combined with `--prune`, a fourth line kind appears for removed paths:

```sh
graft merge --show-changes --prune meta base.yml env.yml secrets.yml
```

**Output** (same as above, plus):
```
meta.version:
  ✗ base.yml         "1.0"
  - <pruned>

server.timeout:
  + env.yml          60
```
(and the summary line's removed count becomes `1 removed`)

### Changes Only

```sh
graft merge --changes-only base.yml env.yml secrets.yml
```

**Output:**
```
Changed paths (4 paths of 6):
  database.host        localhost → db.prod.example.com
  database.password    <none> → "1.0"
  database.pool_size   10 → 50
  server.timeout       <none> → 60
```

`<none>` on the left means the path wasn't present in the first file
(added by a later file). The `of 6` denominator counts every path ever
recorded, including ones later pruned away — not strictly the final
document's key count.

## History Phases

Every history entry is tagged with the step that produced it:

| Phase | Description |
|-------|-------------|
| LOAD | The first input file |
| MERGE | A later input file overwriting or adding a value |
| EVAL | Operator evaluation, including an operator `(( prune ))` marker taking effect |
| POST | `--prune`/`--cherry-pick` removing a path |

The phase itself isn't printed directly in either `--history`'s or
`--trace-path`'s output; the `[N]` step index and the source column convey
it. A non-removed EVAL-phase entry's source is `<evaluated>`; a removed
entry's source is always `<pruned>`, whichever phase actually produced the
removal (EVAL for an operator `(( prune ))` marker, POST for a
`--prune`/`--cherry-pick` flag) — see [Pruned Paths](#pruned-paths) below.
`--trace-path`'s `Type:` line distinguishes an unevaluated operator
expression, a removal, or a plain value at each step.

## Pruned Paths

An operator `(( prune ))` marker is unconditional: the engine removes the
path during evaluation regardless of any CLI flag, so history already
shows it removed even without `--prune`/`--cherry-pick`:

```yaml
# base.yml
secret: (( prune ))
database:
  host: localhost
```

```yaml
# override.yml
database:
  host: db.prod.example.com
```

```sh
graft merge --history base.yml override.yml
```

**Output:**
```
Merge History:

database.host:
  [0] base.yml       → localhost
  [1] override.yml   → db.prod.example.com
  Final              → db.prod.example.com

secret:
  [0] base.yml       → (( prune ))
  [2] <pruned>       → <pruned>
  Final              → <pruned>
```

`[0]` shows the file the `(( prune ))` marker came from, still literal and
unevaluated (matching how any other operator expression appears before the
evaluation step resolves it); the removal itself is entry `[2]`, at the
evaluation step, labeled `<pruned>` rather than `<evaluated>` because this
path did not survive evaluation. `--show-changes`, `--changes-only`, and
`--trace-path` all classify this the same way a `--prune`/`--cherry-pick`
removal is classified — a removed path counts toward `--show-changes`'
"removed" total, not "changed".

A removed path's `Final` is never confused with a path whose value is
genuinely a YAML null (`~`): only an actual removal renders `<pruned>`, a
null value renders `~` like any other value.

Removal is also always reported at the exact path removed, never smeared
onto an unrelated sibling. Pruning one element out of a list, or one key
out of a map with other surviving keys, marks only that path `<pruned>`;
a parent that merely lost one child, but still exists with the rest of its
data, shows its real remaining value, not `<pruned>`.

## History Entry Details

Each history entry carries:

| Field | Description |
|-------|-------------|
| Index | The step's position in the overall merge (files, then evaluation, then optional post-processing) |
| Source | The file path, or `<evaluated>`/`<pruned>` for the synthetic steps (see [Pruned Paths](#pruned-paths) for when a removal recorded during evaluation is labeled `<pruned>` rather than `<evaluated>`) |
| Phase | LOAD / MERGE / EVAL / POST |
| Removed | Whether this path was pruned/cherry-picked away here, by an operator `(( prune ))` marker or a `--prune`/`--cherry-pick` flag alike |
| Value | The value at that step; nil both for a removal (Removed is true) and for a path whose value is genuinely a YAML null (Removed is false) — Removed is what tells the two apart |

There is no `Line` field: graft does not track which line of a source file
contributed a merged value.

## Literal Dotted Keys

A path segment is a literal map key, not necessarily a nested traversal.
A document can legally have a top-level key that itself contains a dot
(`a.b: 1`), which is a different thing from a nested `a: {b: 1}`. History
paths disambiguate the two using graft's existing quoted-segment path
syntax (the same `"literal.key"` form `pkg/graft/utils.go`'s path parser
already accepts): a segment containing a `.` or `[` is quoted.

```sh
# h1.yml
a.b: 1

# h2.yml
a:
  b: 2
```

```sh
graft merge --history h1.yml h2.yml
```

**Output:**
```
Merge History:

"a.b":
  [0] h1.yml         → 1
  Final              → 1  (unchanged)

a.b:
  [1] h2.yml         → 2
  Final              → 2  (unchanged)
```

`"a.b"` (quoted) is the literal top-level key from `h1.yml`; `a.b`
(unquoted) is the nested `a.b` path added by `h2.yml`. Without quoting,
both would flatten to the identical string `a.b`, and history would
misreport one as having overwritten the other. This only matters for keys
that actually contain a `.` or `[` — the overwhelming majority of paths
render exactly as before (`database.host`, not `"database"."host"`).

## Practical Examples

### Debugging Merge Issues

```sh
# Why is my value wrong?
graft merge --trace-path database.connection_string \
    base.yml env.yml secrets.yml

# See all changes
graft merge --history base.yml env.yml secrets.yml | \
    grep "database\."
```

### Audit Trail

```sh
# Generate audit report
echo "# Configuration Audit"
echo "Generated: $(date)"
echo
echo "## Source Files"
echo "- base.yml"
echo "- production.yml"
echo "- secrets.yml"
echo
echo "## Change History"
graft merge --history base.yml production.yml secrets.yml
```

### CI/CD Verification

```sh
#!/bin/bash
# Verify expected values and their sources

OUTPUT=$(graft merge --changes-only base.yml env.yml)

# Check database.host is in the changed-paths list
if ! echo "$OUTPUT" | grep -q "database.host"; then
    echo "ERROR: database.host should have changed"
    exit 1
fi

echo "Configuration sources verified"
```

### Interactive Debugging

```sh
graft debug base.yml env.yml secrets.yml

graft> history database.password
database.password:
  [2] secrets.yml    → (( grab meta.version ))
  [3] <evaluated>    → "1.0"
  Final              → "1.0"

graft> inspect database.password
"1.0"
```

## Performance Considerations

`--history`/`--trace-path`/`--show-changes`/`--changes-only` re-run the
real merge engine once per input file (to capture each file's individual
contribution), plus one more evaluation pass — O(n) merge calls for n
files, rather than the single merge/evaluate pass a plain `graft merge`
does. That trade favors guaranteed-correct history (it's always what an
equivalent plain merge would actually produce) over speed, which is the
right call for a diagnostic report over the typically small number of
files a merge invocation takes. For a large file set where this matters,
prefer `--trace-path` (one path, still the same number of merge calls but
much less output) over `--history` (every path).

## Secret Redaction

History reports render whatever value is in the document at each step —
including a resolved Vault/AWS secret's real value, unredacted. There is
no automatic secret-pattern redaction in `--history`/`--trace-path`/
`--show-changes`/`--changes-only` output; treat it the same as `graft
merge`'s own stdout when handling secrets (e.g. avoid piping it somewhere
that gets logged). Setting the `REDACT` environment variable (see
[Vault Integration](secrets/vault.md)) redacts secret values the same way
it does for a plain merge, since history tracking observes the same
document the merge itself produces.

## See Also

- [Diff & Comparison](diffing.md) - Comparing documents
- [debug Command](cli/debug.md) - Interactive debugging
- [merge Command](cli/merge.md) - Merge with history

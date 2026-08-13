# History Interface

`History` reports changes `pkg/graft` actually recorded to a merged
document's `DocumentMemory`: which map keys a later overlay overwrote, and
what operator evaluation resolved them to. It is a thin veneer over
`DocumentMemory` (`document_memory.go`) - not a second tracking engine, and
not the same mechanism as the CLI's `graft merge --history`/`--trace-path`/
`--show-changes`/`--changes-only` flags (see
[History Tracking](../../user-guide/history-tracking.md)), which are backed
by `internal/history`/`internal/histdiff`, a separate, CLI-only
snapshot-diff mechanism sharing no code or data with this API.

History tracking is off by default and costs nothing when off: no engine
enables it unless told to.

## Enabling History Tracking

```go
// Per merge chain - lazily activates tracking on the engine if it is not
// already active (only possible when the Engine is *DefaultEngine; a no-op
// for any other Engine implementation).
result, err := engine.Merge(ctx, base, overlay).
    TrackHistory().
    Execute()

// Or at the engine level - every merge on this engine records, whether or
// not that merge's own chain calls TrackHistory().
engine, err := graft.NewEngine(graft.WithHistoryTracking(true))

// Or with a smaller config surface than WithMemoryConfig's full
// MemoryConfig (document.md and options.md cover WithMemoryConfig itself).
// MaxEntriesPerPath is the only HistoryConfig field with an observable
// effect in this release - see the HistoryConfig field table below for
// CompressValues and RetentionPeriod, which are not:
engine, err := graft.NewEngine(graft.WithHistoryConfig(graft.HistoryConfig{
    MaxEntriesPerPath: 20,
}))
```

Access the result:

```go
history := result.History()
for _, entry := range history.Timeline() {
    fmt.Printf("[%s] %s: %v -> %v\n", entry.Phase, entry.Path, entry.OldValue, entry.NewValue)
}
```

`Document.History()` never returns a nil interface. A `Document` produced
without tracking active - including every `goPatchDocument` - returns an
empty, valid `History` whose methods return empty results rather than
requiring a nil check first.

## Engine-Wide Scope

`DocumentMemory` belongs to the `*DefaultEngine` that created it, not to
one merge. If tracking stays active across more than one `Execute()` call
on the same engine, every tracked merge's `Document.History()` reflects
every path any of those merges (or evaluations run on that engine) has
ever recorded - not just its own. Build a fresh engine per merge to
isolate history between them.

This scope is also an unbounded-memory concern, not only a correctness
one: every tracked merge on a long-lived engine keeps adding to one
timeline that `HistoryConfig` cannot bound - see [HistoryConfig](#historyconfig)
below. Use `WithMemoryConfig` directly, with `MaxTotalVersions`,
`MaxMemoryMB`, and `CleanupInterval` all set deliberately, for a real
bound.

## What Is Actually Recorded

Two recording sites feed `DocumentMemory`, both using the canonical
dotted, no-`"$"`-prefix path form (`pkg/graft/tree.Cursor.String()`):

- Merge-phase map-key writes (`merge_builder_impl.go`, `merger/merge.go`):
  a later document overwriting, adding, or deleting a map key.
- Evaluation-phase operator results (`evaluator.go`): an operator
  resolving `op.where` to a concrete value.

Everything else is a documented gap, not an oversight:

- **List-element mutations are never recorded.** `merger` computes
  list-element paths but only ever calls `RecordMergeChange` for map keys.
  `AllPaths`/`ChangedPaths`/`Timeline`/`Query`/`ForPath` will never contain
  a list-index path such as `"servers.0.name"`.
- **A newly added nested subtree is recorded only at its top-level key,
  not at every descendant path.** Merging `{"added": {"nested": {"leaf":
  v}}}` onto a document that lacks `added` records exactly one entry, at
  path `"added"`, carrying the whole subtree as `NewValue`;
  `ForPath("added.nested.leaf")` returns nil. This differs from
  overwriting an *existing* deep leaf, which does record at every level
  (`"a"`, `"a.b"`, `"a.b.c"`) - only a value with a corresponding key
  already present in the base document recurses far enough to record
  per-key entries; a wholly new key is recorded once, as a whole-value
  add, at the point it first appears.
- **Only `PhaseMerge`/`OpMerge` and `PhaseEval`/`OpTransform` are ever
  recorded.** `PhaseLoad`, `PhaseManual`, `PhasePostProcess`, `OpSet`,
  `OpDelete`, `OpPrune`, and `OpReplace` have no producer: pruning,
  cherry-picking, and `WithPostProcessors` post-processors run after
  evaluation but record nothing.
- **`HistoryEntry.Source` and `.Line` are always zero-valued.** Nothing in
  the merge or evaluation path threads an input file identity down to
  `DocumentMemory.RecordChange`, and graft's only line/column tracking
  (`pkg/graft/interfaces/position.go`) is scoped to tokens inside a single
  `(( ... ))` expression, never to merged values. `HistoryEntry.Operator`
  carries the string `DocumentMemory` actually records at the recording
  site instead - the literal merge verb (`"merge"`, `"add"`, `"delete"`)
  for a merge-phase entry, or the operator name (e.g. `"grab"`, `"vault"`)
  for an eval-phase entry.

## Interface Definition

```go
type History interface {
    AllPaths() []string
    ChangedPaths() []string
    ForPath(path string) []HistoryEntry
    Query(filter HistoryFilter) []HistoryEntry
    Timeline() []HistoryEntry
    TimelineAfter(t time.Time) []HistoryEntry
    TimelineBefore(t time.Time) []HistoryEntry
    ToJSON() ([]byte, error)
    ToYAML() ([]byte, error)
}
```

| Method | Returns |
|--------|---------|
| `AllPaths()` | Every path with at least one recorded entry, sorted. Not the same as `Document.Paths()`: a path can appear here without existing in the final document (if later pruned), and a path touched exactly once is included. |
| `ChangedPaths()` | Paths with more than one recorded entry (touched by, for example, both an overlay overwrite and operator evaluation), sorted. |
| `ForPath(path)` | Every recorded entry for `path`, oldest first. Nil if `path` has no recorded history. |
| `Query(filter)` | Entries matching `filter` (see `HistoryFilter` below). |
| `Timeline()` | Every recorded entry across every path, in recording order. |
| `TimelineAfter(t)` / `TimelineBefore(t)` | Entries recorded strictly after/before `t`. |
| `ToJSON()` / `ToYAML()` | `Timeline()` plus a summary block, serialized. |

## Types

### HistoryEntry

```go
type HistoryEntry struct {
    Index     int
    Path      string
    Version   int
    Timestamp time.Time
    Phase     HistoryPhase
    Operation HistoryOperation
    OldValue  interface{}
    NewValue  interface{}
    Source    string
    Line      int
    Operator  string
    Evaluated bool
    Metadata  map[string]interface{}
}
```

| Field | Description |
|-------|-------------|
| `Index` | Position within the slice this entry was returned in (0-based) - the global order for `Timeline()`/`Query()`, or the per-path order for `ForPath()`. Not a single counter shared across methods. |
| `Path` | Canonical dotted path (no `"$"` prefix) that changed. |
| `Version` | `DocumentMemory`'s per-path version number, starting at 1. |
| `Timestamp` | When `DocumentMemory` recorded the change. |
| `Phase` | `PhaseMerge` or `PhaseEval` in this release; see "What Is Actually Recorded" above. |
| `Operation` | `OpMerge` or `OpTransform` in this release. |
| `OldValue` | The value immediately before this change. `Timeline()`/`Query()` populate this from the recording call site's own prior-value argument, accurate even on a path's first entry. `ForPath()` instead reconstructs it from the *previous recorded version* for the same path (matching `DocumentMemory.Compare`), which is `nil` on a path's first recorded entry even when `Timeline()`/`Query()` report a real prior value for that same change. Prefer `Timeline()`/`Query()` when the true prior value matters. |
| `NewValue` | The value this change produced. |
| `Source` | Always `""` in this release; reserved for input-file provenance, which nothing currently threads down to `DocumentMemory.RecordChange`. |
| `Line` | Always `0` in this release; same reservation as `Source`. |
| `Operator` | The raw string `DocumentMemory` recorded: the merge verb for a `PhaseMerge` entry, or the operator name for a `PhaseEval` entry - not exclusively an operator name despite the field name. |
| `Evaluated` | `true` when `Phase` is `PhaseEval`. |
| `Metadata` | Populated only for `ForPath()` results (from `NodeVersion`); always `nil` for `Timeline()`/`Query()` results (`ChangeEvent` carries no metadata). |

### HistoryPhase / HistoryOperation

Aliases onto the existing `ChangePhase`/`ChangeOperation` types
(`document_memory.go`) - no new vocabulary.

```go
type HistoryPhase = ChangePhase
type HistoryOperation = ChangeOperation

const (
    PhaseLoad                    = PhaseInitial // alias; no producer in this release
    PhasePostProcess ChangePhase = PhaseManual + 1 // no producer in this release
)

const (
    HistorySet       = OpSet       // no producer in this release
    HistoryMerge     = OpMerge
    HistoryOverwrite = OpReplace   // no producer in this release
    HistoryDelete    = OpDelete    // no producer in this release
    HistoryTransform = OpTransform
    HistoryPrune     = OpPrune     // no producer in this release
)
```

Only `HistoryMerge`/`PhaseMerge` and `HistoryTransform`/`PhaseEval` ever
appear in a recorded `HistoryEntry`.

### HistoryFilter

```go
type HistoryFilter struct {
    Path      string
    Phase     *HistoryPhase
    Operation *HistoryOperation
    Source    string
    After     *time.Time
    Before    *time.Time
    Limit     int
}
```

| Field | Description |
|-------|-------------|
| `Path` | Filter by path. A recorded path matches on exact equality, on the wildcard grammar `PathMatches` implements (`*` one segment, `**` zero or more, `[0]`, `[*]`, `[key=value]`), or on a segment-aware path prefix: `Path: "db"` matches `"db"` and `"db.host"` but not `"dbextra"`. Empty disables the filter. |
| `Phase` | Filter by phase (`nil` for any). |
| `Operation` | Filter by operation (`nil` for any). |
| `Source` | Filter by the raw string `DocumentMemory` recorded at the site (see `HistoryEntry.Operator`'s doc comment) - this field's name predates the `HistoryEntry.Operator`/`Source` split and still matches the underlying `ChangeEvent.Source`, not `HistoryEntry.Source`. |
| `After` / `Before` | Only entries after/before this time. |
| `Limit` | Maximum entries to return, `0` for unlimited. Applied last, keeping the earliest `Limit` matches in timeline order - not the most recent `Limit`. |

### HistoryConfig

A smaller, documented-field-only view onto `MemoryConfig`
(`document_memory.go`'s 11-field struct), for `WithHistoryConfig`.
`WithMemoryConfig` remains available directly for anything this does not
expose.

```go
type HistoryConfig struct {
    MaxEntriesPerPath int
    RetentionPeriod   time.Duration
    CompressValues    bool
}
```

| Field | Maps to | Description |
|-------|---------|-------------|
| `MaxEntriesPerPath` | `MemoryConfig.MaxVersionsPerNode` | Once a path has recorded this many versions, `DocumentMemory` drops its oldest to stay at the cap on every further record. `0` means unlimited. This caps **only** `NodeHistory.Versions`, the per-path storage `History.ForPath` reads. The timeline `History.Timeline`, `History.Query`, `History.AllPaths`, and `History.ChangedPaths` all read is never trimmed by this field (or by anything except `DocumentMemory.Clear`) - a path capped at `ForPath`'s count `1` can still be reported by `ChangedPaths` as having more than one entry, and `Timeline`/`Query` keep growing regardless of this setting. It does not bound engine-wide memory; see `WithMemoryConfig`'s `MaxTotalVersions`/`MaxMemoryMB`/`CleanupInterval` for a real bound. |
| `CompressValues` | `MemoryConfig.EnableCompression` | Enables gzip+gob compression of old versions during cleanup - but, like `RetentionPeriod` below, has **no observable effect** through `WithHistoryConfig` in this release: compression only runs inside `performCleanup`, and `performCleanup` only runs when `MaxTotalVersions`/`MaxMemoryMB` (cleanup-by-size) or `CleanupInterval` (cleanup-by-ticker) is set, none of which `HistoryConfig` exposes. Setting `CompressValues` through `WithHistoryConfig` alone compresses nothing. Use `WithMemoryConfig` directly, with one of those fields set deliberately, for compression that actually runs. |
| `RetentionPeriod` | `MemoryConfig.CompressAfter` only | The age threshold compression uses once cleanup runs. Deliberately does **not** set `MemoryConfig.CleanupInterval`: doing so would start a background goroutine ticker (`DocumentMemory.startBackgroundCleanup`) for the process lifetime from a single engine option, with no way to stop it short of a type assertion to `*DocumentMemory`. Without `CleanupInterval` (or a `MaxTotalVersions`/`MaxMemoryMB` limit, neither exposed by `HistoryConfig`), cleanup never runs on its own, so `RetentionPeriod` has **no observable effect** in this release. Use `WithMemoryConfig` directly, with `CleanupInterval` set deliberately, for real age-based cleanup. |

## Functions

```go
func WithHistoryTracking(enabled bool) Option
func WithHistoryConfig(config HistoryConfig) Option
```

`WithHistoryTracking` is a discoverable wrapper over
`EngineOptions.EnableMemoryTracking`. `enabled=true` enables tracking with
any `MemoryConfig` separately supplied via `WithMemoryConfig`/
`WithHistoryConfig`, or a zero-value one otherwise. `enabled=false` is a
genuine no-op, not a way to turn tracking back off - it never calls
`DisableMemoryTracking`. If a `WithMemoryConfig`/`WithHistoryConfig` call
elsewhere in the same `NewEngine(...)` call already enabled tracking,
`WithHistoryTracking(false)` does not undo it. Call
`engine.(*graft.DefaultEngine).DisableMemoryTracking()` after construction
to turn tracking off once it is on.

`WithHistoryConfig` enables tracking using `HistoryConfig`'s field
mapping above; equivalent to `WithMemoryConfig(cfg)` plus
`WithHistoryTracking(true)`.

```go
func (b MergeBuilder) TrackHistory() MergeBuilder
func (d Document) History() History
```

`TrackHistory()` activates tracking for one merge chain, lazily calling
`EnableMemoryTracking` on the engine before the merge itself runs (a
no-op if the `Engine` is not `*DefaultEngine`). `History()` returns the
resulting `Document`'s recorded change history, or an empty `History` if
tracking was never active for it.

## Examples

### Basic Timeline

```go
engine, err := graft.NewEngine(graft.WithHistoryTracking(true))
if err != nil {
    return err
}

result, err := engine.Merge(ctx, base, overlay).Execute()
if err != nil {
    return err
}

for _, entry := range result.History().Timeline() {
    fmt.Printf("[%s] %s: %v -> %v (%s)\n",
        entry.Phase, entry.Path, entry.OldValue, entry.NewValue, entry.Operator)
}
```

### Per-Path Debugging

```go
history := result.History()
for _, path := range history.ChangedPaths() {
    fmt.Printf("=== %s ===\n", path)
    for i, entry := range history.ForPath(path) {
        fmt.Printf("%d. [%s/%s] -> %v\n", i+1, entry.Phase, entry.Operation, entry.NewValue)
    }
}
```

### Filtered Query with a Limit

```go
evalPhase := graft.PhaseEval
entries := history.Query(graft.HistoryFilter{
    Phase: &evalPhase,
    Limit: 10,
})
for _, entry := range entries {
    fmt.Printf("%s evaluated to %v via %s\n", entry.Path, entry.NewValue, entry.Operator)
}
```

### Serializing for an Audit Log

```go
data, err := history.ToJSON()
if err != nil {
    return err
}
os.WriteFile("audit.json", data, 0o644)
```

`ToJSON`/`ToYAML` output shape. Overwriting `database.host` records two
entries, not one: the leaf key itself, and its parent map `database`
(which the merge also rewrote, carrying the whole subtree as
`old_value`/`new_value`) - the same recurse-when-the-key-already-exists
behavior described in [What Is Actually Recorded](#what-is-actually-recorded)
above:

```json
{
  "entries": [
    {
      "index": 0,
      "path": "database.host",
      "version": 1,
      "timestamp": "2024-01-15T10:30:00Z",
      "phase": "merge",
      "operation": "merge",
      "old_value": "localhost",
      "new_value": "db.prod.example.com",
      "operator": "merge",
      "evaluated": false
    },
    {
      "index": 1,
      "path": "database",
      "version": 1,
      "timestamp": "2024-01-15T10:30:00Z",
      "phase": "merge",
      "operation": "merge",
      "old_value": { "host": "localhost" },
      "new_value": { "host": "db.prod.example.com" },
      "operator": "merge",
      "evaluated": false
    }
  ],
  "summary": {
    "total_entries": 2,
    "changed_paths": 0,
    "by_phase": {
      "merge": 2
    }
  }
}
```

`summary.changed_paths` is a count (paths with more than one entry among
`entries`), not a path list - `History.ChangedPaths()` returns the list
form.

## Related Documentation

- [Engine Interface](engine.md) - Enabling history tracking

- [MergeBuilder API](merge-builder.md) - Per-merge history tracking

- [Document Interface](document.md) - Accessing history from documents

- [Diff Interface](diff-api.md) - Comparing documents

- [History Tracking](../../user-guide/history-tracking.md) - The
  CLI's separate `--history`/`--trace-path`/`--show-changes`/
  `--changes-only` flags

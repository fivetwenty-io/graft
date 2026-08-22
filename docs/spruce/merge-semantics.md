# Merge Semantics: Graft vs. Spruce

Spruce merges YAML documents left to right, applying array-merge markers
and two "ghost" behaviors around `(( prune ))` and `(( sort by <key> ))`
that survive being overwritten mid-merge. Graft aims to reproduce all of
this. This document lists the marker set both tools implement, the
default fallback chain applied when no marker is present, and where
graft's current implementation still diverges.

## Array merge markers

Both tools scan the leading string entries of a merged-in array for
operator markers before applying any other merge logic. Multiple markers
can chain in one array.

| Marker | Effect | Graft status |
|---|---|---|
| `(( append ))` | New entries are added to the end of the array. | Implemented, matches spruce. |
| `(( prepend ))` | New entries are added to the start of the array. | Implemented, matches spruce. |
| `(( insert after <idx> ))` / `(( insert before <idx> ))` | New entries are inserted at a numeric position. | Implemented, matches spruce. |
| `(( insert after "<name>" ))` / `(( insert before "<name>" ))` | New entries are inserted relative to a named entry. | Implemented, with two deliberate divergences: graft also anchors on the entry value in a list of scalars, where spruce fails the merge, and graft rejects a new entry that duplicates the first list entry, which spruce's duplicate check misses. See [known gaps](known-gaps.md#named-insert-works-on-scalar-lists). |
| `(( delete <idx> ))` | The entry at a numeric position is removed. | Implemented, matches spruce. |
| `(( delete "<name>" ))` | The entry matching a name is removed. | Implemented, matches spruce. |
| `(( merge ))` | Entries are merged by the default identifier key. | Implemented, matches spruce, for arrays that reach the marker-aware merge path (see below). |
| `(( merge on <key> ))` | Entries are merged by an explicit identifier key. | Same as above. |
| `(( inline ))` | Entries are merged pairwise by position. | Implemented, matches spruce. |
| `(( replace ))` | The original array is discarded; the new array is used as-is. | Implemented, matches spruce. |

Graft carries a direct port of spruce's marker-parsing engine
(`pkg/graft/merger`), including the same regular expressions for insert
and delete forms and the same `DEFAULT_ARRAY_MERGE_KEY` environment
variable override for the identifier key used by `(( merge ))` and
by-key default merging.

## Default merge fallback chain

When an array carries no marker, spruce tries, in order: merge entries
by key if every entry in both the original and new array is a map that
carries a hashable scalar value for the identifier key; otherwise merge
pairwise by position (`inline`); otherwise, if `--fallback-append` is
set, append the new entries onto the original array instead. A
`WarningError` is written to stderr when the fallback is triggered for a
non-trivial reason, such as an identifier key whose value is itself a
map or list.

Graft applies this same three-step chain through its marker-aware merge
path (`pkg/graft/merger`), which the CLI's merge builder routes into
whenever an overlay array contains map entries, an explicit array-merge
marker, or a prune/sort marker. Arrays of plain scalars with no marker
and no map entries take a separate, simpler path controlled by an array
merge strategy option, whose default (`inline`) merges pairwise by
position exactly as spruce's fallback does: overlay entries replace
base entries index by index, base entries beyond the overlay's length
are kept, and extra overlay entries are appended.

```
# base.yml: f: [a, b, c]      overlay.yml: f: [X]
spruce merge base.yml overlay.yml   ->  f: [X, b, c]
graft  merge base.yml overlay.yml   ->  f: [X, b, c]
```

`--fallback-append` behaves the same in both tools.

```mermaid
flowchart TD
    A[Overlay array reached during merge] --> B{Has a marker,\nor an entry is a map,\nor a prune/sort marker present?}
    B -- yes --> C[Marker-aware merge path\npkg/graft/merger]
    C --> D{Marker present?}
    D -- yes --> E[Apply the marker:\nappend/prepend/insert/delete/\nmerge/merge on key/inline/replace]
    D -- no --> F{Both arrays key-mergeable?}
    F -- yes --> G[Merge by identifier key]
    F -- no --> H{--fallback-append set?}
    H -- yes --> I[Append new entries]
    H -- no --> J[Merge pairwise by position - inline]
    B -- no --> K[Array merge strategy option\ndefault: merge pairwise by position - inline]
```

### Array-marker handling under `--skip-eval` matches spruce

In spruce, `--skip-eval` only gates the operator evaluator's three
phases (the pass that resolves scalar operator calls like
`(( grab ... ))` or `(( vault ... ))`). Array-merge markers are
consumed during the merge step itself, which runs unconditionally
before the evaluator is ever invoked, so `--skip-eval` has no effect on
them: `(( append ))`, `(( merge on <key> ))`, and every other array
marker are always resolved into the final array structure, skip-eval or
not.

Graft's merge builder (`pkg/graft/merge_builder_impl.go`) follows the
same rule. Its marker-detection logic (`needsLegacyMerger`) recognizes
every marker family — `append`, `prepend`, `replace`, `merge`,
`merge on <key>`, `insert`, and `delete` — and routes any array
carrying one through the real merger unconditionally, whether or not
`--skip-eval` is set. No marker is ever left as literal, unresolved
text in the output or silently dropped in favor of a plain inline
replace; `merge_marker_parity_test.go` pins this for both a lone
document and multi-document merges, with and without `--skip-eval`.

`(( sort by <key> ))` ghost-tracking follows the same rule too: once a
sort marker has been queued by an earlier document and the path is
later overwritten by a real array, the sort still applies to the final
output whether or not `--skip-eval` is set. The one place `--skip-eval`
still changes behavior is a lone, never-overwritten `(( prune ))` on a
single document: pruning that value is evaluator work in both tools
(there is no earlier document for the ghost-tracking below to apply
to), so it stays as literal, unresolved text in the output when
evaluation is skipped, matching spruce. Once a `(( prune ))` marker has
actually been overwritten by a later document, its ghost-tracking is
unconditional in both tools regardless of `--skip-eval` — see
[ghost prune/sort semantics](#ghost-prunesort-semantics) below.

## Ghost prune/sort semantics

`(( prune ))` and `(( sort by <key> ))` markers have a merge-order
quirk in spruce that is easy to miss in a straightforward
reimplementation: the marker's effect survives being overwritten by a
later document, in both directions.

- If an earlier document sets `(( prune ))` at a path and a later
  document overwrites that path with a real value, the path is still
  queued for pruning. The overwritten value is used for the rest of the
  merge and evaluation (so other operators can still reference it), but
  it is removed from the final output.

- If an earlier document sets a real value at a path and a later
  document overwrites it with `(( prune ))`, the path is queued for
  pruning and the earlier value stays where it is. Operators that
  reference the path read that value rather than the marker, and the
  path is removed from the final output afterwards. A `(( prune ))`
  that lands on a path no earlier document set has nothing to preserve
  and is left for the evaluator to resolve.

The same before/after tracking applies to `(( sort by <key> ))`
markers: whichever document last touched the path determines the sort
key that gets applied to the final array, even if that document's own
value at the path was itself overwritten again later.

```mermaid
sequenceDiagram
    participant D1 as Document 1
    participant D2 as Document 2
    participant D3 as Document 3
    participant Out as Final output

    D1->>D2: path set to (( prune ))
    D2->>D3: path overwritten with a real value
    Note over D3: prune marker is remembered\neven though the real value now sits at this path
    D3->>Out: value evaluated normally,\nthen removed by the prune pass
```

Graft's merge engine implements the same two-directional tracking
(`pkg/graft/merger`), including the same ordering guarantee that
pruning runs before sorting, which runs before cherry-pick filtering,
during post-processing. Graft additionally distinguishes array-index
paths from map-key paths when a prune marker is being replaced, and
preserves the original value at a path when it is a map or list (rather
than a scalar) so that other operators referencing that path during
evaluation still see it; spruce does not make this distinction.

## BOSH placeholder pass-through

Spruce only evaluates `(( <registered-operator> [args...] ))`. Any other
`(( ... ))` content passes through the merge byte-for-byte — bare
references (`((meta.name))`, `(( meta.name ))`), single names
(`((cf_admin_password))`), and text that is not parseable as an operator
expression at all (`((genesis-entombed/uaa_ssl--key--fe75a2d0))`,
`((/dns_healthcheck_tls.ca))`). These are BOSH/CredHub variable
placeholders, and a later tool in the pipeline interpolates them; BOSH's
placeholder regex allows no interior whitespace, so re-rendering
`((x))` as `(( x ))` corrupts a manifest.

Graft matches this (`ParseOpcallWithParserForEngine` in
`pkg/graft/parser.go`, pinned by
`pkg/graft/bosh_placeholder_passthrough_test.go`):

- A whitespace-free inner text matching BOSH's placeholder shape that is
  not a registered operator name passes through verbatim, even when a
  matching key exists in the document — spruce never implicitly grabs.

- An expression that fails to parse passes through verbatim unless it
  starts with a registered operator name, in which case the parse error
  is reported (a malformed `(( concat ... ))` is a real authoring
  mistake, not a placeholder).

- A top-level bare reference that parses cleanly still passes through
  rather than being wrapped in an implicit `grab`. Bare references in
  *operand* position (`(( env == "production" ))`) still evaluate.

This means graft's earlier top-level implicit-grab behavior is gone:
`x: (( meta.name ))` no longer resolves — write
`x: (( grab meta.name ))`. Graft's own bundled examples were updated
accordingly.

Reference lexing also accepts `+` inside a path segment when followed by
an identifier character (`meta.__vaultified.haproxy_ssl+certificate` is
one reference, matching how Genesis's vaultified manifests name keys),
while `+` before a digit remains arithmetic (`(( meta.count+1 ))`).

## Related documents

- [YAML formatting differences](yaml-formatting.md)

- [Known gaps](known-gaps.md)

- [Genesis compatibility contract](genesis-compat-contract.md)

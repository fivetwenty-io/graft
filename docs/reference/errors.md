# Errors

This page documents the rendered shape of specific graft errors, complementing
[Error Codes](error-codes.md)'s classification reference. It currently covers
one error: the operator data-flow cycle block.

## Cycle Detection

When a merge's operators form a cycle (for example, `a` grabs `b` and `b`
grabs `a`), graft cannot evaluate them and reports a `*graft.CycleError`
instead of a merged document. `CycleError.Error()` still begins with the
literal text `cycle detected in operator data-flow graph`, so
`ClassifyError` still resolves it to `CodeCircularReference` (`E203`); see
[Error Codes](error-codes.md#evaluation-e2xx) for that classification.

The example below is real output, captured against this fixture:

```yaml
# a.yml
meta:
  foo: (( grab meta.bar ))
```

```yaml
# b.yml
meta:
  bar: (( grab meta.foo ))
```

```sh
graft merge a.yml b.yml
```

**Output:**
```
1 error(s) detected:
 - cycle detected in operator data-flow graph
   inputs:
     [1] a.yml
     [2] b.yml
   cycle (2 nodes): meta.bar -> meta.foo -> meta.bar
     b.yml:2  meta.bar: (( grab meta.foo ))
     a.yml:2  meta.foo: (( grab meta.bar ))
```

### Reading the block

The `inputs:` list names every merge input, in merge order, numbered from
`[1]`. It is omitted entirely, along with its heading, when the error has no
input names to report; nothing prints an empty `inputs:` section.

The `cycle (N nodes): ...` line names every operator on the cycle, in
reference order (each node's expression references the next), then repeats
the first node at the end. That repeat is why the chain visually closes on
itself.

Below the chain, one detail line per node gives its file, line, path, and
expression: `file:line  path: expr`. The last two detail lines always name
the two ends of the edge that closes the loop:

- For a cycle of three or more nodes, the detail lines repeat the first
  node's line at the end, mirroring the chain line above them, so the final
  two lines are the last node and the repeated first node.

- For a two-node cycle, no repeat is needed: the two detail lines already
  are both ends of the (only) edge that closes the loop.

- For a one-node self-cycle, a single detail line prints, with no wrap
  duplicate, because that one line already names both ends of its own
  self-edge.

### Position degradation

Each node's file and line come from resolving the operator's expression
back into the input that wrote it. That resolution can only degrade, never
invent a position:

| Case | Rendering |
|---|---|
| Line resolved | `a.yml:3  path: expr` |
| Unresolved, single-input merge | `a.yml  path: expr` |
| Unresolved, multi-input merge | `<unknown>  path: expr` |
| Self-cycle, one node | Single line, no wrap duplicate |
| Node reached only via an alias | Unresolved |
| Control-flow input | Unresolved |
| `<<<:` inject block | Unresolved |
| Quoted path segment, for example `$.'a.b'.c` | Unresolved |
| JSON or go-patch input | Unresolved |
| No sources on the context | Block printed without the `inputs:` section |

### Go API

`errors.Is(err, graft.ErrDependencyCycle)` answers true for a cycle
surfaced by the merge path, the same as it already did for one surfaced by
`DependencyGraph.TopologicalSort`.

`errors.As(err, &ce)`, with `var ce *graft.CycleError`, gives access to the
same data the rendered block draws from: `Inputs []string`, the merge
inputs in merge order, and `Nodes []CycleNode`, the cycle's operators in
reference order, where `CycleNode` is `{Path, Expr string; Pos
interfaces.Position}`.

## See Also

- [Error Codes](error-codes.md) - Classification and troubleshooting

- [History Tracking](../user-guide/history-tracking.md) - Why merged values
  carry no file or line, and how this page's positions differ

# graft vs. spruce parity

graft is a Go reimplementation of [spruce](https://github.com/geofffranks/spruce)'s
YAML-merging and operator-evaluation model, built on a different YAML
library (`goccy/go-yaml` instead of spruce's `geofffranks/yaml` fork) and a
different concurrency model (a copy-on-write tree that supports parallel
operator evaluation, versus spruce's single-threaded evaluator). This
directory documents where the two tools agree, where they diverge, and what
a drop-in replacement scenario (most notably Genesis, which currently shells
out to a `spruce` binary on `$PATH`) needs to account for.

## How to read these pages

| Page | Covers |
|---|---|
| [CLI surface](cli-surface.md) | Every flag, subcommand, exit code, and environment variable, spruce vs. graft, in tables. |
| [Operator inventory](operators.md) | The full operator set: which of spruce's 25 operators and graft's 47 registered operator names line up, which exist only in graft, and which exist only in spruce. |
| [Merge semantics](merge-semantics.md) | Array-merge marker behavior (`append`, `replace`, `inject`, and so on) and the prune/sort "ghost" semantics where a marker queued at one merge step can survive being overwritten by a later document. |
| [YAML formatting](yaml-formatting.md) | Differences in null rendering, key ordering, and trailing-newline behavior that follow from using a different YAML library. |
| [Known gaps](known-gaps.md) | The open, tracked list of behavioral gaps between graft and spruce, in one place. |
| [Genesis compatibility contract](genesis-compat-contract.md) | The specific invocation patterns and stderr-format contracts that Genesis depends on when it shells out to spruce, and what graft needs to satisfy to be a safe drop-in. |

## Parity status, in short

**CLI surface.** graft's command structure (`merge`, `fan`, `json`, `diff`,
`vaultinfo`) matches spruce's one-for-one, with the same flags on each
subcommand and the same three-way exit-code scheme (0 success, 1 usage
error or `diff`-found-differences, 2 runtime error). graft adds one new
global flag (`--color`) and one new merge/fan flag (`--dataflow-order`) that
spruce does not have; neither changes default behavior. See
[CLI surface](cli-surface.md) for the full comparison.

**Operators.** Every spruce operator except one, `raw_env`, has a
same-named, same-purpose counterpart in graft. graft additionally ships
operators with no spruce equivalent: a fallback-chain vault
lookup (`vault-try`), a NATS key-value backend (`nats`), string splitting
(`split`), a ternary conditional (`?:`), first-class arithmetic operators
(`+ - * / %`), and first-class boolean and comparison operators
(`&& || ! == != < > <= >=`). See [Operator inventory](operators.md) for the
full breakdown, including which operator behaviors have been checked
directly against source for this comparison and which have not yet been
verified for exact parity.

**Merge and evaluation semantics.** The core merge algorithm (map merge,
array-marker parsing, key-based vs. inline array merging) and the
three-phase evaluation model (merge, then param, then eval, with param
failures short-circuiting eval) are structurally the same in both tools. See
[Merge semantics](merge-semantics.md) for the specifics, including which
array-merge and prune/sort edge cases have been confirmed equivalent.

**YAML formatting.** Because graft and spruce marshal YAML with different
libraries, byte-identical output is not guaranteed for every input. See
[YAML formatting](yaml-formatting.md) for the specific areas (null
rendering, key ordering, trailing newlines) where output shape can differ.

**Extras beyond spruce.** graft has capabilities spruce does not: response
and backend caching, metrics export (Prometheus, OpenTelemetry, JSON,
text), parallel/DAG-scheduled operator evaluation, memory pooling, and
backends beyond Vault (AWS Parameter Store and Secrets Manager, NATS). None
of these change graft's behavior when used as a spruce replacement; they are
additive. See [Extra capabilities](../features/extras.md) for details.

**Genesis compatibility.** Genesis's Perl code parses spruce's stderr output
with specific regular expressions (for example, `- $.<path>: <message>` per
error line, and the substring `secret <key> not found` for missing
secrets), checks `spruce -v` output for the literal word "version" followed
by a version token, and relies on `spruce json` emitting exactly one JSON
object per line for multi-document input. See
[Genesis compatibility contract](genesis-compat-contract.md) for the full
list of invocation patterns this drives.

## What "parity" means here

These pages describe **current, observed behavior** in both codebases as of
this comparison, not a promise that every difference listed will be closed.
Where an operator, flag, or output format in graft has not been directly
verified byte-for-byte against spruce's behavior, that is stated explicitly
rather than assumed. [Known gaps](known-gaps.md) is the single place that
tracks which of these differences count as open work, including a
[Resolved](known-gaps.md#resolved) section for items that have since been
closed. As of this writing, the open items are a missing `raw_env`
operator, a few internal (non-parity) loose ends, and one
still-unverified byte-level formatting question (trailing newline
count); null rendering,
map key ordering, the array-merge fallback warning, and parallel-evaluation
determinism have all been confirmed against the spruce binary and are
covered by dedicated tests.

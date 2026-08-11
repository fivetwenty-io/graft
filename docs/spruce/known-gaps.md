# Known Gaps

This page lists every open item between graft's current implementation
and full parity with spruce, plus a few internal graft loose ends that
fall outside parity scope but are worth recording. Each open entry states
current behavior, the behavior expected for parity (where spruce has an
equivalent), and the impact. A separate [Resolved](#resolved) section
keeps a record of items that were open at some point and have since been
closed, so links from other pages into this file keep working.

## Open gaps

### raw-env-operator-missing

**Current behavior:** graft has no operator or file registered under
`raw_env` or a similar name.

**Expected behavior:** spruce's `raw_env` operator resolves a single
`$ENVVAR`-style argument to its raw string value, bypassing the YAML
type coercion that both tools' normal `$VAR` substitution applies. A
value like `$PORT` normally gets parsed as a YAML number through
ordinary substitution; `raw_env` keeps it as a literal string instead.

**Impact:** a kit or deployment file using `(( raw_env $SOME_VAR ))` to
preserve an environment variable's raw string form has no direct graft
equivalent today. The impact on the Genesis drop-in use case is low:
`raw_env` has zero occurrences in the sampled Genesis kits and
deployments corpus.

### wildcard-path-matching-stub

**Current behavior:** wildcard path matching in the document-memory
tracker is an explicit stub with no implementation.

**Expected behavior:** N/A: internal graft feature, no spruce
equivalent.

**Impact:** any graft feature depending on wildcard path matching in
document memory does not work yet. Deferred, and out of scope for the
current parity effort.

### no-cache-annotation-stub

**Current behavior:** the "do not cache this result" annotation feature
for expressions is partially built; both the metadata field and the
logic that would act on it are marked incomplete.

**Expected behavior:** N/A: internal graft feature, no spruce
equivalent.

**Impact:** operators cannot yet mark a result as non-cacheable. This
is deferred and sits outside the current parity effort.

### convenience-api-pending

**Current behavior:** a set of planned convenience functions on the
public Go API are commented out, pending the engine implementation
being considered complete.

**Expected behavior:** N/A: library-only surface, no spruce
equivalent.

**Impact:** embedders of graft as a Go library have a smaller
convenience surface than intended. Also deferred, also outside the
current parity effort.

### sort-post-processing-silently-skips-two-error-cases

**Current behavior:** after a `(( sort by X ))` marker is queued and
the merge completes, graft's sort post-processing step
(`pkg/graft/engine.go`'s `evaluate` and the equivalent
`--skip-eval` path in `pkg/graft/merge_builder_impl.go`) resolves the
queued path against the merged document. Two of its outcomes are
silently skipped rather than reported: the path failing to resolve
at all (for example, because a prune removed it), and the path
resolving to something other than a list.

**Expected behavior:** spruce treats both of these as hard errors:
a `(( sort by X ))` marker that ends up pointing at a path that
doesn't resolve, or at a non-list value, fails the merge with a
non-zero exit code rather than passing the document through
unsorted.

**Impact:** a misconfigured or since-pruned sort marker produces a
merge that succeeds silently in graft where spruce would fail
loudly, masking the misconfiguration until something downstream
notices the list is in the wrong order. Not observed in the sampled
Genesis kits and deployments corpus. A related third case, a sort
key on a homogeneous list that fails a type or key check, already
fails the merge in both tools.

### mixed-key-type-map-encoding-order

**Current behavior:** graft's internal document tree is always
`map[string]interface{}`; any non-string YAML map key (an integer
key like `10:`) is coerced to its string form as soon as the
document is parsed (`pkg/graft/yaml_compat.go`'s `NormalizeMap`).
When such a map is marshaled back out, its keys sort alongside
ordinary string keys purely lexicographically, so `10` sorts before
`2` and both sort before `9`.

**Expected behavior:** spruce's YAML library keeps non-string keys
typed through encode and sorts them accordingly: numeric keys are
compared and ordered numerically among themselves, and placed before
string keys, rather than being interleaved lexicographically with
them.

**Impact:** a document with a map that mixes integer and string keys
at the same level (uncommon in Genesis kits and deployments, which
use string keys throughout for job names, network names, and
property paths) is marshaled with a different key order than
spruce produces for the same input. Ordinary string-only and
integer-only maps are unaffected; see [Map key
ordering](yaml-formatting.md#known-differences) for the byte-parity
guarantee that still holds for those cases.

## Resolved

Items that were tracked as open gaps and have since been closed. Kept
here, under their original heading, so links from other pages that
point at a specific gap keep resolving to the right place.

### quoted-boolean-strings-coerced-to-booleans

**Resolved.** graft's YAML 1.1 compatibility layer now inspects quoting
at the AST level: a bare `yes`, `no`, `on`, or `off` still coerces to a
boolean (matching spruce's YAML 1.1-flavored parsing), while a quoted
form such as `a: "yes"` stays the literal string through merge, json,
and output, byte-identical to spruce. Both `graft merge` and
`graft json` parse through the same compatibility pipeline, so the two
subcommands agree on value types. Pinned by tests in
`pkg/graft/yaml_spruce_parity_test.go` and `pkg/graft/json_test.go`,
plus binary-comparison fixtures in the operator parity corpus.

### trailing-newline-byte-parity-unverified

Resolved. A live byte-for-byte comparison of built spruce and graft
binaries confirmed identical output tails across single-doc merge,
multi-doc merge, json (single and multi-doc), `--skip-eval`, stdin
merge, and diff invocations. The same sweep exposed and fixed a crash
on blank, comment-only, and null (`---` alone) input files, which both
tools now handle by producing `{}` for that document.

### vault-target-extraction

**Resolved.** The vault operator's target-extraction code used to be an
explicit placeholder that always returned an empty string for a
non-empty `@target`, which risked a wrong-target cache-key collision for
multi-target lookups. That placeholder has been removed. The current,
accurate behavior, documented directly on `VaultOperator`, is that
`@target` syntax (`(( vault@production "path:key" ))`) is accepted by
the parser and recorded on the parsed expression, but graft's `Opcall`
type has no field to carry it through, so `Run` never observes a
non-empty target at all: no placeholder call is attempted, so there is
no collision risk. Multi-target Vault access remains available today
through per-target environment variables and
`internal/backends/vault.DefaultPool`, just not through `@target`
parsing on the operator call itself.

### aws-nats-target-extraction

**Resolved.** Same fix as [vault-target-extraction](#vault-target-extraction):
the AWS and NATS operators no longer carry a placeholder that silently
mis-resolved `@target`. Both operators now document, next to their
`Run` method, that `@target` is parsed but not wired through to
execution, and that multi-account AWS access and multi-cluster NATS
access remain available via per-target environment variables
(`AWS_<TARGET>_REGION` and equivalents) consumed directly by
`internal/backends/aws.ClientPool` and
`internal/backends/nats.ClientPool`.

### or-operator-unregistered

**Resolved.** No standalone `or` operator implementation, registered or
commented out, exists in the current source tree. Logical-or
short-circuit behavior is expressed with `||` (`OrElseOperator`), which
graft already supports and which spruce's own operator set relies on in
the same way, so there was never a functional gap here.

### env-feature-flags-not-wired-to-cli

**Resolved.** `--config <path>` on the root command, plus `GRAFT_*` and
`GRAFT_FEATURE_*` environment variables, are now resolved once per CLI
invocation, `--config` given or not: an explicit config file or
`config.DefaultConfig()` as the base, `internal/config.ApplyEnv` layered
on top, and `internal/features.DefaultFlags().LoadFromEnv()` for feature
flags. Both are wired into engine construction for `merge`, `fan`, and
`vaultinfo` (not `diff` or `json`), giving the intended
env-over-file-over-default precedence. Not every resolved setting
changes observable merge behavior yet — the Parallel section and the
`caching` feature flag do, most other config sections don't yet. See
[Configuration reference](../reference/config.md) for the complete,
field-by-field breakdown of what's wired and what isn't.

### array-marker-handling-diverges-under-skip-eval

**Resolved.** Array-merge markers (`append`, `prepend`, `replace`,
`merge`, `merge on <key>`, `insert`, `delete`) are now always resolved
during the merge step, regardless of `--skip-eval`, matching spruce:
`--skip-eval` only disables the scalar operator evaluator, never the
merger. The merge builder's marker-detection logic
(`needsLegacyMerger`) recognizes every marker family and routes any
array carrying one through the real merger unconditionally, so no
marker silently falls through to a plain inline replace anymore.

### null-rendering-parity-unverified

**Resolved.** Confirmed by running both `spruce merge` and `graft
merge` binaries over equivalent fixtures, and pinned by a test in
`pkg/graft/yaml_spruce_parity_test.go`: every null representation
(explicit `null`, `~`, or an empty scalar) marshals to the bare word
`null`, matching spruce byte-for-byte, and a string value that happens
to read `"null"` or `"~"` stays quoted rather than being rendered as an
unquoted null-like token.

### map-key-order-parity-unverified

**Resolved.** Confirmed by the same spruce-vs-graft binary comparison
and pinned by a test in `pkg/graft/yaml_spruce_parity_test.go`: graft's
encoder emits map keys in alphabetical order on marshal, matching
spruce's `yaml.v2`-family encode behavior, regardless of the native Go
map's undefined iteration order in memory.

### array-fallback-warning-parity-unverified

**Resolved.** Verified against the spruce binary and pinned by
`pkg/graft/merge_fallback_warning_test.go`: graft emits the same stderr
warning pair spruce does when the default array-merge fallback
(key-merge, then inline) triggers, for every document-merge step where
the fallback applies, including the first document merged into the
initial empty root. The warning text intentionally never matches the
`^ - \$\.` error-line pattern Genesis's stderr scraping depends on.

### parallel-eval-determinism-unverified

**Resolved.** `pkg/graft/parallel_determinism_test.go` runs a full
parallel merge 40 times over representative multi-document,
operator-heavy fixtures and asserts every run's marshaled YAML output is
byte-identical to the first. Output ordering (map keys and list
elements) is stable across repeated runs regardless of worker count, so
enabling or tuning parallel evaluation changes only how fast a merge
runs, never what it produces.

### default-concurrency-hardcoded

**Resolved.** The CLI no longer uses a fixed worker count for parallel
evaluation. `resolveConcurrency` derives the default from an explicit
`cfg.Parallel.MaxWorkers` (set via config file or
`GRAFT_PARALLEL_MAX_WORKERS`) when present, otherwise from
`runtime.NumCPU()` floored at `1`, so the worker pool now scales with
the host machine by default.

## Related documents

- [Merge semantics](merge-semantics.md)

- [YAML formatting differences](yaml-formatting.md)

- [Genesis compatibility contract](genesis-compat-contract.md)

# Known Gaps

This page lists every open item between graft's current implementation
and full parity with spruce, plus a few internal graft loose ends that
fall outside parity scope but are worth recording. Each open entry states
current behavior, the behavior expected for parity (where spruce has an
equivalent), and the impact. A separate [Resolved](#resolved) section
keeps a record of items that were open at some point and have since been
closed, so links from other pages into this file keep working.

## Open gaps

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

### scalar-array-default-merge-replaces-instead-of-inlining

**Resolved.** The scalar-array path's default strategy now merges
pairwise by position, matching spruce's `inline` fallback: with
`base.yml` holding `f: [a, b, c]` and `overlay.yml` holding `f: [X]`,
both tools produce `f: [X, b, c]`. An explicit strategy option can
still request replace, and `--fallback-append` is unchanged.

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
multi-target lookups. The target is now carried through to `Run` and
resolved against `internal/backends/vault.DefaultPool`, so
`(( vault@production "path:key" ))` reaches the client configured by
`VAULT_PRODUCTION_ADDR` and `VAULT_PRODUCTION_TOKEN`. A target with no
matching configuration errors at evaluation time and names the variables
it expected, rather than falling back to the default client.

The spruce spelling that puts the target on the path
(`(( vault production@"path:key" ))`) is rejected with a message
redirecting to the operator-name spelling.

### aws-nats-target-extraction

**Resolved.** Same fix as [vault-target-extraction](#vault-target-extraction):
`(( awsparam@<target> ... ))`, `(( awssecret@<target> ... ))`, and
`(( nats@<target> ... ))` resolve their target against
`internal/backends/aws.ClientPool` and
`internal/backends/nats.ClientPool`, configured through
`AWS_<TARGET>_REGION` (and the profile, role, and access-key variants) and
`NATS_<TARGET>_URL`.

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

### sort-post-processing-silently-skips-two-error-cases

**Resolved in 1.32.0.** A queued `(( sort ... ))` marker whose path
fails to resolve after the merge (for example, because a prune removed
it), or resolves to a non-list value, now fails the merge with exit
code 2 and spruce's exact error text inside the standard
`N error(s) detected:` framing, instead of passing the document through
unsorted. Sort application also moved from the evaluator into the merge
builder's post-processing, after all pruning (including `--prune`
flags) and before cherry-picking, matching spruce's
prune-sort-cherry-pick order; `--skip-eval` runs the identical code
path, which also closed a third, undocumented case where a bad sort key
was silently ignored under `--skip-eval`. Pinned by
`pkg/graft/sort_postprocess_parity_test.go` and six binary-comparison
fixtures in the operator parity corpus. Note the deliberate behavior
change: documents that previously merged successfully with a dangling
or mistyped sort marker now fail, exactly as they do under spruce.

### raw-env-operator-missing

**Resolved in 1.32.0.** `(( raw_env $NAME ))` is now registered, with
spruce's exact semantics: it resolves a single environment-variable
argument to its raw string value, bypassing the YAML type coercion the
normal `grab $NAME` substitution applies (`PORT=8080` stays the string
`"8080"`), treats a set-but-empty variable as a valid empty string, and
errors with `environment variable $NAME is not set` for an unset one.
`(( raw_env "A" || raw_env "B" ))` keeps the raw-string behavior on
either side, while a non-`raw_env` fallback such as a literal still
coerces normally. Pinned by `pkg/graft/operators/op_raw_env_test.go`
and seven binary-comparison fixtures in the operator parity corpus.
With this operator in place, no spruce operator is missing from graft.

### convenience-api-pending

**Resolved in 1.32.0.** The commented-out convenience functions are now
implemented: `graft.QuickMerge(yamls ...string)` and
`graft.QuickMergeFiles(paths ...string)` each build a default engine,
merge their inputs left to right with full operator evaluation, and
return the marshaled YAML bytes. Zero arguments yield `"{}\n"`.
Pinned by `pkg/graft/quick_merge_test.go` and runnable examples in
`pkg/graft/examples_doc_test.go`; documented in the
[engine API guide](../developer-guide/library-api/engine.md).

### wildcard-path-matching-stub

**Resolved in 1.32.0.** History path filters
(`graft.HistoryFilter.Path`) now match with the same wildcard grammar
the rest of graft uses instead of a literal string comparison: exact
paths, `*` (one segment), `**` (any depth), `[N]`/`[*]` index patterns
(matching the dotted-numeric form recorded paths use), and
`[key=value]` selectors, plus segment-aware prefix matching so a filter
of `db` still covers `db.host` without also matching `dbextra`.
Pinned by `pkg/graft/document_memory_path_filter_test.go`.

### no-cache-annotation-stub

**Resolved in 1.32.0.** The partially built annotation is now the
`:nocache` expression modifier, implemented end to end: the parser
accepts `(( name:nocache args ))` (unknown modifiers are parse
errors), the flag travels through `Opcall` and the evaluator to every
backend cache, and a modified vault/awsparam/awssecret/nats call
neither reads from nor writes to the shared per-run cache while plain
calls keep sharing it under unchanged keys. See
[Expression Modifiers](../reference/expression-modifiers.md) for the
grammar, semantics, and spruce-compatibility notes. Pinned by
`pkg/graft/nocache_test.go`, `pkg/graft/nocache_backend_test.go`, and
`pkg/graft/operators/operator_nocache_support_test.go`.

## Related documents

- [Merge semantics](merge-semantics.md)

- [YAML formatting differences](yaml-formatting.md)

- [Genesis compatibility contract](genesis-compat-contract.md)

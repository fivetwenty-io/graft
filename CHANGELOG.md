# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `graft debug` now runs its prompt through a real line editor when it is
  attached to a terminal. Up and Down recall earlier commands, Ctrl+R
  searches them, and Tab completes command names, document paths from the
  tree at the current step, set breakpoints for `unbreak`, and the known
  keys for `config`. Recall persists in `~/.graft/debug_history`, created
  readable only by its owner, and a line that sets a secret such as
  `config vault.token` is kept out of it. Piped and redirected input is
  read exactly as before.

### Fixed

- `graft debug`'s `history` command now applies the session's deferred
  paths, as `step` and `continue` already did. Previously an operator the
  session could not resolve, such as an unreachable Vault path or an
  unfilled `(( param ))`, aborted the recompute that history tracking
  depends on, so `history` reported that operator's error for every path
  asked about, including unrelated ones. Deferring the offending path had
  no effect because the command rebuilt the merge without consulting the
  deferred set.

### Documentation

- Added [Inspecting a Merge](docs/examples/inspecting-a-merge.md), a
  walkthrough that debugs a failing merge from its first symptom to a
  full answer, with runnable fixtures in `examples/inspecting-a-merge/`.

- Documented how `REDACT` lets a whole debug session tolerate unreachable
  Vault paths, and how `graft diff` decides whether to color its output.

## [1.33.0] - 2026-08-17

A performance release. Output is byte-for-byte identical to 1.32.2
across the full compatibility corpus — every change below was gated in
CI on producing the exact stdout, stderr, and exit code of the previous
release — but merges are much faster: a heavy Genesis-style merge (one
large manifest plus 45 overlays) drops from 1.77 s to 0.23 s, and to
about 30 ms on a repeat run with the new persistent cache enabled.
The same merge takes spruce about 1.25 s.

### Added

- Persistent merge cache (opt-in)

  Set `GRAFT_CACHE_L2_ENABLED=true` (or `cache.l2_enabled: true`) and
  graft caches work across invocations on disk, in two layers. The
  output layer replays a previous run's exact stdout and stderr when a
  merge is repeated with byte-identical inputs, identical flags, and
  the same graft version — only *pure* invocations are stored, so any
  operator that consults an external system (`vault`, `awsparam`,
  `awssecret`, `nats`), the filesystem (`file`, `load`), the
  environment (`raw_env`, `$VAR`), or randomness (`shuffle`), and any
  control-flow document, disqualifies a run from output caching. The
  parse layer stores each document's parsed tree keyed by its content
  hash, so runs that miss the output layer still skip re-parsing
  unchanged documents. Keys hash content, never paths or mtimes, so
  Genesis-style temp files hit across runs and any edit misses.

  A cached result is guaranteed byte-identical to an uncached run; a
  new CI gate (`scripts/cache-identity.sh`) proves this across the
  whole example corpus on every push. Debug and trace runs bypass the
  cache, and cache trouble (unwritable directory, corrupt entry) is
  never an error. Entries live under `GRAFT_CACHE_L2_PATH`, defaulting
  to the OS user cache directory (`~/.cache/graft` on Linux,
  `~/Library/Caches/graft` on macOS) — an empty `cache.l2_path` no
  longer fails validation. Entries expire after seven days as
  housekeeping. See [Caching](docs/features/extras.md#caching).

- `graft cache stats` and `graft cache clear` subcommands

  Report per-layer entry counts and sizes for the persistent cache,
  and drop all stored entries.

### Changed

- The merge pipeline is roughly 7.5× faster on operator- and
  document-heavy workloads, independent of any caching: merge-phase
  regexes are compiled once, the two post-parse compatibility walks are
  fused into one, string screening happens at the byte level before
  expensive quote and array probing, overlays merge into the base
  in-place instead of deep-copying it, parse fan-out is bounded to the
  available cores, and costly debug dumps are gated on the debug flag.
  With this release a plain uncached `graft merge` outruns spruce
  about 5× on heavy inputs.

- Parallel evaluation hardening: an explicit
  `GRAFT_FEATURE_PARALLEL=false` now disables parallel evaluation even
  though the config default is `true` (either kill switch set to
  `false` wins), warning suppression is race-free under concurrent
  workers, and the scheduler panics loudly if its single-threaded
  dependency computation is ever entered concurrently instead of
  silently corrupting dependency edges.

### Changed

- **Breaking:** top-level bare references are no longer implicitly
  grabbed. `x: (( meta.name ))` now passes through the merge verbatim
  as a BOSH/CredHub placeholder, matching spruce; write
  `x: (( grab meta.name ))` to resolve it. Bare references in operand
  position (`(( env == "production" ))`) still evaluate. Bundled
  examples were updated to use explicit `grab`.

### Fixed

- BOSH/CredHub variable placeholders now survive a merge byte-for-byte:
  tight placeholders (`((cf_admin_password))`) are no longer re-spaced,
  and unparseable placeholder text such as
  `((genesis-entombed/uaa_ssl--key--fe75a2d0))` and
  `((/dns_healthcheck_tls.ca))` passes through untouched instead of
  failing the merge. Expressions starting with a registered operator
  still report parse errors.
- The vault backend now detects KV v2 mounts via
  `sys/internal/ui/mounts` (with per-mount caching), inserting the
  `data/` path segment v2 reads require and unwrapping the v2 response
  envelope, matching spruce's vaultkv behavior. Reads against
  KV v2-mounted secrets engines previously failed with
  "Invalid path for a versioned K/V secrets engine".
- Strings that cannot be written as plain YAML scalars for syntax
  reasons (e.g. `*.uaa.((system_domain))`) are now emitted
  single-quoted like spruce, instead of double-quoted. Type-lookalike
  strings (`"1.0"`, `"yes"`, `"null"`) keep double quotes, also
  matching spruce.
- Reference paths accept `+` inside a segment when followed by an
  identifier character (`meta.__vaultified.haproxy_ssl+certificate`),
  as produced by Genesis's vaultified manifests; `+` before a digit
  remains arithmetic.

## [1.32.1] - 2026-08-14

### Fixed

- `(( file ... ))` now fails with spruce's exact error text when the
  file cannot be read (`tried to read file <path>: could not be read -
  <os error>`) and when the argument resolves to a map or list
  (`tried to read file <arg>, which is not a string scalar`), instead
  of surfacing the raw Go error or trying to open the stringified
  collection as a filename.

## [1.32.0] - 2026-08-13

Closes the last five tracked entries in the
[known-gaps register](docs/spruce/known-gaps.md) and the ordering
component of the sixth: map keys now encode in spruce's order. Three
open entries remain in the register — the narrowed
mixed-key-type-map-encoding-order entry (key typing, quoting, and
label differences only) and two newly recorded divergences
(y-n-boolean-values-not-coerced, stringify-block-scalar-style).

### Added

- `raw_env` operator

  `(( raw_env $NAME ))` resolves an environment variable to its raw
  string value, bypassing the YAML type coercion normal `$NAME`
  substitution applies: `PORT=8080` stays the string `"8080"`. A
  set-but-empty variable is a valid empty string; an unset one errors.
  Semantics and error strings match spruce byte for byte. This was the
  last spruce operator missing from graft.

- `:nocache` expression modifier

  `(( vault:nocache "secret/db:password" ))` makes that single call
  bypass the per-run backend cache in both directions — it never reads
  a cached value and never writes one — while plain calls keep sharing
  the cache under unchanged keys. Honored by `vault`/`vault-try`,
  `awsparam`, `awssecret`, `nats`, and registry-registered custom
  backends; inert on operators without a backend cache. Composes with
  targets as `(( vault:nocache@prod ... ))`. An unknown modifier is a
  parse error. See
  [Expression Modifiers](docs/reference/expression-modifiers.md).

- `graft.QuickMerge` and `graft.QuickMergeFiles` library functions

  One-call conveniences that merge YAML strings or files left to right
  with full operator evaluation and return the marshaled YAML output.

- Wildcard history path filters

  `HistoryFilter.Path` now matches with graft's wildcard grammar
  (`*`, `**`, `[N]`, `[*]`, `[key=value]`) and segment-aware prefix
  matching, instead of literal string comparison.

### Changed

- Map keys are ordered like spruce on every YAML emit

  Behavior change: graft used to sort map keys purely
  lexicographically on encode (`item10` before `item9`, `10` before
  `2`). It now uses a port of spruce's comparator
  (`pkg/graft/keysort.go`): numeric-looking keys sort first,
  numerically, followed by string keys in spruce's natural order —
  digit runs compare numerically, non-letters sort before letters,
  uppercase before lowercase. String-only key sets are byte-identical
  to spruce; bare-numeric key sets match position-for-position (graft
  keys stay quoted strings, spruce's stay bare and typed). Pinned by
  the byte-exact runner `tests/spruce-compat/run-key-order.sh`.
  Residual divergences are documented in
  [known gaps](docs/spruce/known-gaps.md#mixed-key-type-map-encoding-order).

- `Document.ToYAML` and `DefaultEngine.ToYAML` route through
  `MarshalYAML`

  Library change: both now produce the same bytes as the CLI for the
  same tree, gaining the spruce-compatible key ordering and the
  special-float quoting guard (a string value like `".nan"` used to
  leave the library surface unquoted and silently re-parse as a
  float). History YAML (`History().ToYAML`) intentionally keeps
  goccy's lexicographic ordering: it is a graft-only diagnostic
  surface with no spruce counterpart, and its DTO structs hold map
  fields a tree-walk cannot reach.

- `(( stringify ))` serializes through the shared marshal

  Stringified subtrees carry the same spruce-compatible key order as
  every other YAML emit. The outer scalar's presentation still
  differs from spruce (quoted flow scalar vs literal block; see
  [known gaps](docs/spruce/known-gaps.md#stringify-block-scalar-style)).

### Fixed

- Dangling or mistyped `(( sort ... ))` markers now fail the merge

  A queued sort whose path no longer resolves after the merge (for
  example, because a prune removed it) or resolves to a non-list value
  now fails with exit code 2 and spruce's exact error text, instead of
  silently passing the document through unsorted. Sorting also now runs
  after all pruning (including `--prune` flags) and before
  cherry-picking, matching spruce's post-processing order, and
  `--skip-eval` follows the identical path. Behavior change: documents
  that previously merged successfully with a dangling or mistyped sort
  marker now fail, exactly as they do under spruce.

## [1.31.1] - 2026-08-13

Release-engineering release: no code changes to the CLI or library beyond
the version string.

### Added

- Homebrew tap distribution

  `brew install --cask fivetwenty-io/tap/graft` installs the binary and
  shell completions. The cask in
  [fivetwenty-io/homebrew-tap](https://github.com/fivetwenty-io/homebrew-tap)
  is generated and pushed automatically on each release.

- Signed and notarized macOS binaries

  The darwin release binaries carry a FiveTwenty Inc. Developer ID
  signature and Apple notarization, so Gatekeeper accepts them on first
  launch (previously the quarantined ad-hoc-signed binaries were killed
  outright on Apple Silicon).

- Debian and RPM packages, FreeBSD builds (amd64/arm64), and bash, zsh,
  and fish completions in every archive.

### Changed

- Releases are built and published by GoReleaser. Archive names changed
  from `graft-<version>-<os>-<arch>` to `graft_<version>_<os>_<arch>`,
  and the checksum file from `graft-<version>-checksums.sha256` to
  `graft_<version>_SHA256SUMS`.

## [1.31.0] - 2026-08-12

The library API release: `pkg/graft` is now a first-class Go library. The
CLI surface and the genesis/spruce stderr contract are unchanged and
byte-identical to 1.30.0, except for the `-v` dispatch fix noted under
Fixed.

### Added

- Parsing and merging entry points

  `Engine.ParseFile`, `ParseReader`, `ParseMultiDocFile`, and
  `ParseGoPatch` (with `DetectArrayRoot`, `RootIsArrayError`,
  `NewRootIsArrayError`, `IsArrayError`) replace the former stubs.
  `MergeFiles` and `MergeReaders` return a builder that carries load
  errors to `Execute()` instead of a nil builder that panicked.
  `MergeBuilder.Base`, `Overlay`, and `OverlayFile` compose document
  sources onto a chain.

- Document conveniences and sentinel errors

  Checked getters `String`, `Int`, `Int64`, `Float64`, `Bool`, plus
  `Has`, `Paths`, `SortKeys`, and `ToJSONIndent` on `Document`.
  `ErrNotFound`, `ErrTypeMismatch`, and `ErrInvalidPath` sentinels work
  with `errors.Is` against getter, `Set`, and `Delete` failures
  (`NewValidationErrorWithCause` carries the chain; `Error()` strings
  are unchanged). `MultiError` gained `Unwrap() []error`, so `errors.Is`
  and `errors.As` see through aggregated evaluation errors.

- Diff API

  `DiffResult`, `Change`, `ChangeType`, `DiffOptions` (including
  `IgnoreArrayOrder` and `IgnoreWhitespace`), `DiffDocuments`, renderers
  `WriteSideBySide`, `WriteUnified`, `WriteChangeList`, `WriteMergeTree`,
  and `Engine.Diff`/`DiffWithOptions`.

- Engine options and runtime reconfiguration

  `Option` alias, `WithCacheSize`, `WithCacheTTL`, `WithCacheDisabled`,
  `WithOperators`, `WithTraceOutput`, `WithTraceLevel` (with
  `TraceLevel`), and `DefaultEngine.Configure` for applying an option
  delta to a live engine with validate-before-mutate semantics.
  `WithLogger`, `WithDebugLogging`, and `WithYAMLCompat` are now
  functional. `Engine.ToYAML`, `ToJSON`, and `ToJSONIndent` evaluate and
  serialize instead of returning a not-implemented error; they resolve
  the document in place (pass `doc.Clone()` to keep the original).

- Post-processors

  The open `PostProcessor` interface, `WithPostProcessors` (engine-wide
  or per builder), built-ins via `NewPruner`, `NewCherryPicker`,
  `NewKeySorter`, and `NewSecurityRedactor`; processors run at the tail
  of `Execute()` after evaluation, pruning, and cherry-picking.

- Merge history

  `History`, `HistoryEntry`, `HistoryConfig`, `MergeBuilder.TrackHistory`,
  `Document.History`, `WithHistoryTracking`, `WithHistoryConfig`, and
  `HistoryFilter.Limit`. History is engine-scoped, off by default, and
  near-free when off. List-element mutations and the interior of newly
  added nested subtrees are not recorded; the docs state every gap.

- Custom backends (behind a feature flag)

  `Backend` and `TargetedBackend` with per-engine registration
  (`RegisterBackend`, `GetBackend`, `ListBackends`, `UnregisterBackend`,
  `WithBackend`), retry/cache/audit wrapping (`RetryConfig`, `TLSConfig`,
  `BackendCache`, `AuditLogger`), `BackendError`, `ErrBackendNotFound`,
  and `SequentialGetBatch`. Gated by `GRAFT_FEATURE_BACKEND_REGISTRY`
  (default off; behavior with the flag off is byte-identical). The
  vault, vault-try, awsparam, awssecret, and nats operators consult the
  registry when enabled, falling back to the built-in backends.
  `WithVault`/`WithVaultTarget` and `WithAWS`/`WithAWSTarget` register
  real SDK-backed implementations from a config struct.

- Testing support

  `NewMockEngine` (seeded in-memory vault/awsparam/awssecret/nats
  lookups with call recording), `OperatorFunc`, and `NewTestEvaluator`.

- Dependency graph and expression traversal

  `DependencyGraph`, `OperatorRef`, `EvalWave`, `BuildEvalPlan`,
  `DefaultEngine.BuildDependencyGraph` and `EvaluateParallel` (with
  `ErrNoWorkerPool`, `ErrInvalidEvalPlan`, `ErrDependencyCycle`) as a
  read-only projection of the live evaluation orderings; `Walk`,
  `Visitor`, and `Accept` over `Expr` with a `VisitOther` catch-all for
  forward compatibility.

- `EngineOf` nil-safe accessor for evaluator-attached engines, and
  `WithBackendRegistry` to toggle the backend feature flag without
  importing internal packages.

### Changed

- `Document.Prune` is variadic: `Prune(keys ...string)` (was
  `Prune(key string)`). Single-argument call sites compile unchanged.
- `NewEngine()` and `CreateDefaultEngine()` share one default
  configuration: 10000-entry cache, 4 max concurrent workers,
  alphabetical dataflow order (previously 1000/10 on one path).
- Engine-local operator registration is real: `RegisterOperator` on an
  engine affects that engine's evaluation everywhere, including
  control-flow expansion and nested dependency analysis. The exported
  `ControlFlowExpander` hook now receives the engine.
- Merge history records changes under full dotted paths (`meta.key`),
  not bare immediate keys.
- A zero-document merge runs post-processors and history attachment;
  with `WithCherryPick` it now returns the same error a non-empty merge
  does instead of an empty document.
- `Document`, `Engine`, `MergeBuilder`, and `DiffResult` are documented
  as closed interfaces: methods may be added in minor releases;
  implement `PostProcessor`, `Backend`, and `Visitor` instead.

### Deprecated

- `WithMaxWorkers` — use `WithConcurrency` (functionally identical).
- `WithVaultClient`, `WithVaultConfig`, `WithVaultSkipTLS`, and the
  `VaultClient` interface — never had an effect; use `WithVault` or
  environment variables.
- `WithAWSConfig`, `WithAWSProfile`, `WithAWSRegion` — never had an
  effect; use `WithAWS` or environment variables.
- `WithMemoryPools` — sets a feature flag nothing reads.

### Removed

- The never-wired copy-on-write tree types and their helpers:
  `COWNode`, `COWTree`, `COWEvaluator`, `EnhancedMigrationHelper`,
  `COWTreeFactory`, `COWPerformanceMonitor`, `COWTreeComparator`, their
  constructors, and the `ThreadSafeTree`/`TreeTransaction` interfaces
  and `WorkerPool` type they were the only implementors of. Nothing in
  graft outside their own tests ever constructed them; they were not
  the mechanism behind parallel evaluation. These symbols predate
  `pkg/graft` being a documented library surface (this release is the
  first to declare one), which is why their removal ships in a minor
  version.

### Fixed

- Pre-verb version flag precedence

  A version flag placed before the verb now wins over the subcommand,
  matching spruce: `graft -v merge ...` prints the version and exits 0
  before dispatch (previously the flag was silently ignored and the
  subcommand ran). Placed after the verb, the flag is still ignored
  and the verb runs; spruce instead treats a post-verb `-v` as a
  filename and exits 2. A pre-verb `-v` also skips `--color` and
  `--config` validation, so it now exits 0 where a bad value
  previously exited 1. The version line has always echoed the invoked
  name (`os.Args[0]`), so a spruce-named symlink or copy reports
  itself as `spruce` to genesis's version gate; that behavior is now
  pinned by tests.

## [1.30.0] - 2026-08-11

### Added

- Spruce drop-in parity

  The CLI surface, flags, exit codes, and stderr contract match spruce closely
  enough for graft to replace a `spruce` binary on `$PATH`, including under
  Genesis. Parity is covered by the `spruce-compat` test harnesses: a
  golden-output suite, an operator matrix, and an end-to-end Genesis drop-in
  check. Remaining known divergences are tracked in
  [docs/spruce/known-gaps.md](docs/spruce/known-gaps.md).

- YAML 1.1 compatibility layer

  Normalizes the YAML 1.1 behaviors that spruce relied on, so documents
  written for spruce parse and render the same way under graft's YAML 1.2
  parser.

- Configuration via `GRAFT_*` environment variables and a config file

  A `--config` flag loads a YAML configuration file; `GRAFT_*` environment
  variables override its values. Covers engine, cache, parallelism, logging,
  and metrics settings.

### Changed

- Parallel operator evaluation is enabled by default

  Operators are scheduled in dependency waves; within a wave,
  order-sensitive operators (such as `static_ips`) run one at a time,
  the rest run their work (including Vault/AWS/NATS calls)
  concurrently, and results are applied to the document tree serially
  in a fixed order. Set `GRAFT_PARALLEL_ENABLED=false` to fall back to
  serial evaluation.

[1.33.0]: https://github.com/fivetwenty-io/graft/releases/tag/v1.33.0
[1.32.2]: https://github.com/fivetwenty-io/graft/releases/tag/v1.32.2
[1.32.1]: https://github.com/fivetwenty-io/graft/releases/tag/v1.32.1
[1.32.0]: https://github.com/fivetwenty-io/graft/releases/tag/v1.32.0
[1.31.1]: https://github.com/fivetwenty-io/graft/releases/tag/v1.31.1
[1.31.0]: https://github.com/fivetwenty-io/graft/releases/tag/v1.31.0
[1.30.0]: https://github.com/fivetwenty-io/graft/releases/tag/v1.30.0

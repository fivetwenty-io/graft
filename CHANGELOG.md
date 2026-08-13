# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

[1.31.1]: https://github.com/fivetwenty-io/graft/releases/tag/v1.31.1
[1.31.0]: https://github.com/fivetwenty-io/graft/releases/tag/v1.31.0
[1.30.0]: https://github.com/fivetwenty-io/graft/releases/tag/v1.30.0

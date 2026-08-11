# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

  Operator evaluation runs concurrently over a copy-on-write document tree.
  Set `GRAFT_PARALLEL_ENABLED=false` to fall back to serial evaluation.

[1.30.0]: https://github.com/fivetwenty-io/graft/releases/tag/v1.30.0

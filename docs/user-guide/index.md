# User Guide

This guide covers everything you need to use Graft effectively for configuration management.

## Overview

Graft is a YAML/JSON configuration merging tool with:

- Powerful operators for data manipulation
- Control flow for conditional configurations
- Secrets integration with Vault, AWS, and NATS
- Semantic diffing of two documents

## Sections

### [CLI Commands](cli/)

Complete reference for all command-line operations.

- [CLI Overview](cli/index.md) - Global flags and concepts
- [merge](cli/merge.md) - Merge YAML/JSON files
- [diff](cli/diff.md) - Compare files semantically
- [json](cli/json.md) - Convert between YAML and JSON
- [fan](cli/fan.md) - Cross-product merge
- [vaultinfo](cli/vaultinfo.md) - List Vault references
- [debug](cli/debug.md) - Interactive step-through merge REPL

### [Operators](operators/)

All 48 registered operators, plus the control-flow keywords.

- [Data Manipulation](operators/data-manipulation.md) - grab, concat, join, split, type, etc.
- [Control Flow](operators/control-flow.md) - if/else, for, while, case
- [Arithmetic](operators/arithmetic.md) - Math operations
- [Comparison & Logic](operators/comparison-logic.md) - Comparisons & booleans
- [Array Operations](operators/array-operations.md) - sort, shuffle, flatten, uniq
- [External Sources](operators/external-sources.md) - file, load

### [Array Merging](array-merging.md)

Strategies for merging arrays between documents.

- append, prepend
- replace, inline
- merge by key
- insert, delete

### [Secrets Management](secrets/)

Integrate with external secrets stores.

- [Vault / OpenBao](secrets/vault.md)
- [AWS Parameter Store](secrets/aws-ssm.md)
- [AWS Secrets Manager](secrets/aws-secrets-manager.md)
- [NATS JetStream](secrets/nats.md)

### [Diff & Comparison](diffing.md)

Compare two documents structurally.

- Added, removed, and changed paths
- Map entries reported side by side
- List entries reported as added and removed runs

### [History Tracking](history-tracking.md)

Track where each value in a merged document came from.

- `merge --history` - Every path's full derivation
- `merge --trace-path` - One path's full derivation
- `merge --show-changes` / `--changes-only` - What changed

### [Configuration](configuration.md)

Environment variables and settings.

- Backend configuration
- Output options
- Performance tuning

## Quick Reference

### Common Operations

| Task | Command |
|------|---------|
| Merge files | `graft merge base.yml overlay.yml` |
| Compare files | `graft diff before.yml after.yml` |
| Convert to JSON | `graft json config.yml` |
| Keep only some keys | `graft merge --cherry-pick database base.yml overlay.yml` |
| Drop scaffolding keys | `graft merge --prune meta base.yml overlay.yml` |
| Leave operators unevaluated | `graft merge --skip-eval config.yml` |

### Common Operators

| Operator | Example | Description |
|----------|---------|-------------|
| grab | `(( grab path.to.value ))` | Reference another value |
| grab with predicate | `(( grab servers.name=primary.host ))` | Select a list entry by field value |
| concat | `(( concat "a" "b" ))` | Concatenate strings |
| type | `(( type some.value ))` | Name a value's type as a string |
| vault | `(( vault "secret/db:pass" ))` | Fetch from Vault |
| if/else | `(( if cond )) ... (( fi ))` | Conditional content |
| append | `(( append ))` | Append to array |

## Next Steps

- [CLI Commands](cli/) - Master the command line
- [Operators](operators/) - Learn all operators
- [Examples](../examples/) - See practical patterns

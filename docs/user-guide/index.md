# User Guide

This guide covers everything you need to use Graft effectively for configuration management.

## Overview

Graft is a YAML/JSON configuration merging tool with:

- Powerful operators for data manipulation
- Control flow for conditional configurations
- Secrets integration with Vault, AWS, and NATS
- Rich diff and history tracking

## Sections

### [CLI Commands](cli/)

Complete reference for all command-line operations.

- [CLI Overview](cli/index.md) - Global flags and concepts
- [merge](cli/merge.md) - Merge YAML/JSON files
- [diff](cli/diff.md) - Compare files semantically
- [json](cli/json.md) - Convert between YAML and JSON
- [fan](cli/fan.md) - Cross-product merge
- [vaultinfo](cli/vaultinfo.md) - List Vault references
- [debug](cli/debug.md) - Interactive debugging REPL

### [Operators](operators/)

All 50+ operators for configuration manipulation.

- [Data Manipulation](operators/data-manipulation.md) - grab, concat, join, split, etc.
- [Control Flow](operators/control-flow.md) - if/else, for, while, case
- [Arithmetic](operators/arithmetic.md) - Math operations
- [Comparison & Logic](operators/comparison-logic.md) - Comparisons & booleans
- [Array Operations](operators/array-operations.md) - Array manipulation
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

Compare configurations with rich output.

- Side-by-side diff
- Unified diff (git-style)
- Change list

### [History Tracking](history-tracking.md)

Track where every value came from.

- Merge history
- Path tracing
- Change trees

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
| See merge history | `graft merge --history base.yml overlay.yml` |
| Debug interactively | `graft debug base.yml overlay.yml` |

### Common Operators

| Operator | Example | Description |
|----------|---------|-------------|
| grab | `(( grab path.to.value ))` | Reference another value |
| concat | `(( concat "a" "b" ))` | Concatenate strings |
| vault | `(( vault "secret/db:pass" ))` | Fetch from Vault |
| if/else | `(( if cond )) ... (( fi ))` | Conditional content |
| append | `(( append ))` | Append to array |

## Next Steps

- [CLI Commands](cli/) - Master the command line
- [Operators](operators/) - Learn all operators
- [Examples](../examples/) - See practical patterns

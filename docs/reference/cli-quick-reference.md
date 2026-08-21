# CLI Quick Reference

Complete reference of all Graft CLI commands and flags.

## Commands Overview

| Command | Description |
|---------|-------------|
| `graft merge` | Merge YAML/JSON files |
| `graft diff` | Compare YAML files semantically |
| `graft json` | Convert YAML to JSON (and reverse) |
| `graft fan` | Cross-product merge against multiple targets |
| `graft vaultinfo` | List all vault references in documents |
| `graft debug` | Interactive debugging REPL |

## Global Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--debug` | `-D` | Enable debug logging |
| `--trace` | `-T` | Enable trace logging (verbose) |
| `--version` | `-v` | Show version |
| `--help` | `-h` | Show help |
| `--color` | | Force colorized output on (default: `auto`) |
| `--no-color` | | Force colorized output off, overriding `--color` |
| `--config <path>` | | Path to a YAML configuration file |
| `--max-loop-iterations <n>` | | `(( while ))` iteration cap (default 1000) |

## graft merge

Merge YAML/JSON files with operator evaluation.

```bash
graft merge [flags] file1.yml [file2.yml ...]
```

### Flags

| Flag | Description |
|------|-------------|
| `--skip-eval` | Don't evaluate operators |
| `--prune <key>` | Remove key from output (repeatable) |
| `--cherry-pick <key>` | Output only specific keys (repeatable) |
| `--fallback-append` | Use append for array merges (default: inline) |
| `--go-patch` | Treat file as go-patch format |
| `--multi-doc` | Handle multi-document YAML |
| `--history` | Show merge history for all paths |
| `--trace-path <path>` | Show history for specific path |
| `--show-changes` | Show merge change tree |
| `--changes-only` | Show only changed paths |
| `--interactive` | Enter debugging REPL (equivalent to `graft debug`; no short form) |
| `--skip-vault` | Defer `(( vault ... ))`/`(( vault-try ... ))` calls (leave the expression intact) instead of contacting Vault or OpenBao; composable with `--skip-aws`/`--skip-nats`. `REDACT=1` overrides this and redacts instead. |
| `--skip-aws` | Same, for `(( awsparam ... ))`/`(( awssecret ... ))` |
| `--skip-nats` | Same, for `(( nats ... ))` |

### Examples

```bash
# Basic merge
graft merge base.yml overlay.yml

# Multiple overlays
graft merge base.yml env.yml secrets.yml

# Output to file
graft merge base.yml overlay.yml > result.yml

# Skip operator evaluation
graft merge --skip-eval base.yml overlay.yml

# Remove keys from output
graft merge --prune meta --prune internal base.yml overlay.yml

# Keep only specific keys
graft merge --cherry-pick database --cherry-pick server base.yml overlay.yml

# Show merge history
graft merge --history base.yml overlay.yml

# Trace specific path
graft merge --trace-path database.host base.yml overlay.yml

# Show all changes
graft merge --show-changes base.yml overlay.yml

# Interactive debugging
graft merge --interactive base.yml overlay.yml
```

## graft diff

Compare YAML files semantically.

```bash
graft diff [flags] file1.yml file2.yml
```

### Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--side-by-side` | `-y` | Side-by-side diff view |
| `--unified` | `-u` | Unified diff format (git-style), grouped by top-level key |
| `--changes` | | List all changes (original → new), grouped by kind |
| `--context <n>` | | Lines of context in unified diff (default 3) |
| `--width <n>` | | Total width for side-by-side view (default 80) |
| `--quiet` | `-q` | Exit with status only, no output |

At most one of `--side-by-side`/`--unified`/`--changes` may be given.
`--color`/`--no-color` (Global Flags above) work here too; they're not
`diff`-specific flags.

### Examples

```bash
# Default diff output
graft diff before.yml after.yml

# Side-by-side view
graft diff -y before.yml after.yml

# Git-style unified diff
graft diff -u before.yml after.yml

# List changes
graft diff --changes before.yml after.yml

# Custom width
graft diff -y --width 160 before.yml after.yml

# Check for differences without printing output
graft diff --quiet before.yml after.yml; echo $?
```

## graft json

Convert between YAML and JSON.

```bash
graft json [flags] file.yml
```

### Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--reverse` | `-r` | Convert JSON to YAML |
| `--strict` | | Error on non-string map keys |
| `--multi-doc` | | Handle multi-document files |

### Examples

```bash
# YAML to JSON
graft json config.yml

# JSON to YAML
graft json -r config.json

# From stdin
cat config.yml | graft json

# Strict mode
graft json --strict config.yml
```

## graft fan

Cross-product merge against multiple targets. The first positional
argument is always the source document; every remaining positional
argument is a target (no `--` separator is used).

```bash
graft fan [flags] source.yml [target1.yml target2.yml ...]
```

### Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--skip-eval` | | Don't evaluate operators |
| `--prune <key>` | | Remove key from output (repeatable) |
| `--cherry-pick <key>` | | Output only specific keys (repeatable) |
| `--output-dir <dir>` | `-o` | Write each result to `<dir>/<target-basename>` instead of stdout |

### Examples

```bash
# Fan out to multiple environments
graft fan base.yml env/dev.yml env/staging.yml env/prod.yml

# With output directory
graft fan --output-dir outputs/ base.yml targets/dev.yml targets/prod.yml

# A directory target argument expands to its .yml/.yaml/.json files, sorted
graft fan base.yml targets/ --output-dir outputs/
```

## graft vaultinfo

List all Vault references in documents.

```bash
graft vaultinfo [flags] file.yml [file2.yml ...]
```

### Flags

| Flag | Description |
|------|-------------|
| `--json` | Output as JSON |
| `--paths-only` | Show only paths, not locations |
| `--resolve` | Perform live Vault lookups instead of skipping them (requires a reachable Vault); reports concrete values for paths composed from other vault lookups |

### Examples

```bash
# List vault references
graft vaultinfo config.yml

# JSON output
graft vaultinfo --json config.yml

# Multiple files
graft vaultinfo base.yml overlay.yml secrets.yml

# Paths only
graft vaultinfo --paths-only config.yml

# Resolve composed paths against a reachable Vault
graft vaultinfo --resolve config.yml
```

## graft debug

Interactive debugging REPL.

```bash
graft debug [flags] file1.yml [file2.yml ...]
```

### Flags

| Flag | Description |
|------|-------------|
| `--go-patch` | Same meaning as `merge --go-patch`; applied to every merge the session performs |
| `--fallback-append` | Same meaning as `merge --fallback-append`; applied to every merge the session performs |

### REPL Commands

| Command | Description |
|---------|-------------|
| `load` | Load all documents |
| `step` | Execute next merge step |
| `continue` | Run to completion (or until a breakpoint) |
| `break <path>` | Set breakpoint on path |
| `unbreak <path>` | Remove breakpoint from path |
| `breaks` | List all breakpoints |
| `inspect <path>` | Show current value at path |
| `history <path>` | Show change history for path |
| `defer <path>` | Mark path for deferred evaluation |
| `eval <path>` | Force evaluate operator at path |
| `config [key] [value]` | View/set configuration |
| `output` | Show current document state |
| `diff` | Show changes from original |
| `export <file>` | Export current state to file |
| `help [command]` | Show help |
| `quit` / `exit` | Exit debugger |

### Examples

```bash
# Start debugging session
graft debug base.yml overlay.yml

# Example session
graft> load
graft> step
graft> inspect database
graft> history database.host
graft> config vault.addr https://vault.example.com
graft> continue
graft> output
graft> export result.yml
graft> quit
```

## Environment Variables

### Graft Configuration

| Variable | Description |
|----------|-------------|
| `DEBUG` | Same as `--debug`: enable debug logging (any value but empty, `"false"`, or `"0"`) |
| `TRACE` | Same as `--trace`: enable trace logging, also implies `DEBUG` (any value but empty, `"false"`, or `"0"`) |
| `REDACT` | Any non-empty value skips Vault/AWS/NATS resolution and returns `"REDACTED"` in place of real secret values. Wins over `merge --skip-vault`/`--skip-aws`/`--skip-nats` (see above), which defer instead of redact when REDACT isn't set. |
| `DEFAULT_ARRAY_MERGE_KEY` | Overrides the identifier key (`name` by default) used for by-key array merges |

There is no `GRAFT_COLOR` or `GRAFT_TRACE` — color is controlled by
`--color`/`--no-color` together with the standard `NO_COLOR`/`TERM`
variables (see [CLI Reference: Color flags](cli.md#color-flags)), and CLI
debug/trace logging only by the bare `DEBUG`/`TRACE` variables above or
their `-D`/`-T` flag equivalents. (`GRAFT_DEBUG` does
exist, but it is not a general logging switch: `pkg/graft/parser.go` reads
it to print operator-expression parser errors to stderr, in addition to
the normal error report. Leave it unset for machine-readable stderr.) See
[CLI Reference](cli.md#environment-variables) for the authoritative list,
including the full set of `GRAFT_*` variables `internal/config` reads
(cache size/TTL, parallel worker bounds, and so on — a different, larger
set from the three above).

### Vault Configuration

| Variable | Description |
|----------|-------------|
| `VAULT_ADDR` | Vault server address |
| `VAULT_TOKEN` | Authentication token |
| `VAULT_NAMESPACE` | Vault namespace (enterprise) |
| `VAULT_SKIP_VERIFY` | Skip TLS verification |
| `VAULT_{TARGET}_ADDR` | Per-target address |
| `VAULT_{TARGET}_TOKEN` | Per-target token |

### AWS Configuration

| Variable | Description |
|----------|-------------|
| `AWS_REGION` | AWS region |
| `AWS_PROFILE` | AWS profile name |
| `AWS_ACCESS_KEY_ID` | Access key |
| `AWS_SECRET_ACCESS_KEY` | Secret key |
| `AWS_{TARGET}_REGION` | Per-target region |
| `AWS_{TARGET}_PROFILE` | Per-target profile |

### NATS Configuration

| Variable | Description |
|----------|-------------|
| `NATS_URL` | NATS server URL |
| `NATS_TOKEN` | Authentication token |
| `NATS_TLS_CERT` | TLS certificate path |
| `NATS_TLS_KEY` | TLS key path |
| `NATS_{TARGET}_URL` | Per-target URL |

## Common Patterns

### Multi-Environment Deployment

```bash
# Development
graft merge base.yml env/development.yml

# Staging
graft merge base.yml env/staging.yml

# Production
graft merge base.yml env/production.yml
```

### Secrets Integration

```bash
# Set Vault credentials
export VAULT_ADDR=https://vault.example.com
export VAULT_TOKEN=s.xxxxx

# Merge with secret resolution
graft merge base.yml secrets.yml
```

### CI/CD Pipeline

```bash
# Validate configuration
graft merge --skip-eval base.yml env/${ENV}.yml > /dev/null

# Generate and deploy
graft merge base.yml env/${ENV}.yml > config.yml
kubectl apply -f config.yml
```

### Configuration Comparison

```bash
# Compare environments
graft merge base.yml env/staging.yml > staging.yml
graft merge base.yml env/prod.yml > prod.yml
graft diff -y staging.yml prod.yml
```

## Exit Codes

graft uses three exit codes across every command (see
[CLI Reference](cli.md#exit-codes) for the full per-command breakdown):

| Code | Description |
|------|-------------|
| 0 | Success (for `diff`, no differences found) |
| 1 | Usage error, or mutually exclusive flags combined, or (`diff` only) differences found |
| 2 | Runtime error: file read/parse failure, merge/evaluation failure, or similar |

## See Also

- [Operator Quick Reference](operator-quick-reference.md) - Operator syntax

- [Environment Variables](environment-variables.md) - Full variable list

- [Examples](../examples/index.md) - Usage examples

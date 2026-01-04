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
| `--color` | | Color output: `on`, `off`, `auto` |

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
| `--interactive`, `-i` | Enter debugging REPL |

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
graft merge -i base.yml overlay.yml
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
| `--unified` | `-u` | Unified diff format (git-style) |
| `--changes` | | List all changes (original → new) |
| `--context <n>` | | Lines of context in unified diff |
| `--no-color` | | Disable colorized output |
| `--width <n>` | | Width for side-by-side view |
| `--ignore-paths <paths>` | | Paths to ignore (comma-separated) |
| `--only-paths <paths>` | | Only compare these paths |

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

# Ignore specific paths
graft diff --ignore-paths meta,internal before.yml after.yml

# Only compare specific paths
graft diff --only-paths database,server before.yml after.yml
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

Cross-product merge against multiple targets.

```bash
graft fan [flags] base.yml [overlays...] -- target1.yml target2.yml
```

### Flags

| Flag | Description |
|------|-------------|
| `--skip-eval` | Don't evaluate operators |
| `--prune <key>` | Remove key from output |
| `--output-dir <dir>` | Output directory for results |

### Examples

```bash
# Fan out to multiple environments
graft fan base.yml -- env/dev.yml env/staging.yml env/prod.yml

# With output directory
graft fan --output-dir outputs/ base.yml -- targets/*.yml

# With additional overlay
graft fan base.yml common.yml -- env/dev.yml env/prod.yml
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
```

## graft debug

Interactive debugging REPL.

```bash
graft debug [flags] file1.yml [file2.yml ...]
```

### REPL Commands

| Command | Description |
|---------|-------------|
| `load` | Load all documents |
| `step` | Execute next merge step |
| `continue` | Run to completion |
| `break <path>` | Set breakpoint on path |
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
| `GRAFT_COLOR` | Color output: `on`, `off`, `auto` |
| `GRAFT_DEBUG` | Enable debug logging |
| `GRAFT_TRACE` | Enable trace logging |

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

| Code | Description |
|------|-------------|
| 0 | Success |
| 1 | General error |
| 2 | Parse error |
| 3 | Evaluation error |
| 4 | Backend error |
| 5 | Validation error |

## See Also

- [Operator Quick Reference](operator-quick-reference.md) - Operator syntax

- [Environment Variables](environment-variables.md) - Full variable list

- [Examples](../examples/index.md) - Usage examples

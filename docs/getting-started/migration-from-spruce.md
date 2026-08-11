# Migration from Spruce

Graft is a drop-in replacement for Spruce with additional features. This guide helps you migrate smoothly.

## Compatibility

Graft is **fully compatible** with Spruce:

- All Spruce operators work identically
- All CLI commands and flags are supported
- All merge semantics are preserved
- Your existing configurations will work without modification

## Quick Migration

### Step 1: Install Graft

```sh
go install github.com/fivetwenty-io/graft/cmd/graft@latest
```

### Step 2: Alias or Replace

Option A - Create an alias:

```sh
alias spruce=graft
```

Option B - Replace the binary:

```sh
sudo mv $(which spruce) $(which spruce).bak
sudo ln -s $(which graft) /usr/local/bin/spruce
```

### Step 3: Test

Run your existing scripts - they should work without modification:

```sh
spruce merge base.yml overlay.yml  # Actually runs graft
```

## What's New in Graft

While all Spruce features work, Graft adds significant capabilities:

### Control Flow Operators

```yaml
# Spruce: No native conditionals
# Graft: Full control flow support

(( if grab environment == "production" ))
replicas: 5
(( else ))
replicas: 1
(( fi ))

(( for service in grab services ))
- name: (( grab service.name ))
  port: (( grab service.port ))
(( done ))

(( case grab cloud_provider ))
(( when "aws" ))
storage_class: gp3
(( when "gcp" ))
storage_class: pd-ssd
(( default ))
storage_class: standard
(( esac ))
```

### Additional Secrets Backends

```yaml
# Spruce: Only Vault
password: (( vault "secret/db:password" ))

# Graft: AWS and NATS too
db_host: (( awsparam "/app/prod/db_host" ))
api_key: (( awssecret "prod/api-key" ))
config: (( nats "kv:config/settings" ))
```

### Rich Diff Output

```sh
# Side-by-side diff
graft diff --side-by-side base.yml overlay.yml

# Unified diff (git-style)
graft diff --unified base.yml overlay.yml

# Change list
graft diff --changes base.yml overlay.yml
```

### Merge History Tracking

```sh
# See where every value came from
graft merge --history base.yml env.yml secrets.yml

# Trace a specific path
graft merge --trace-path database.password base.yml secrets.yml
```

### Interactive Debugging REPL

```sh
graft debug base.yml overlay.yml secrets.yml

graft> load
graft> step
graft> inspect database
graft> break database.password
graft> continue
```

### Embeddable Go Library

```go
import "github.com/fivetwenty-io/graft/pkg/graft"

engine, _ := graft.NewEngine()
base, _ := engine.ParseFile("base.yml")
overlay, _ := engine.ParseFile("overlay.yml")

result, _ := engine.Merge(ctx, base, overlay).Execute()
yaml, _ := result.ToYAML()
```

## Feature Comparison

| Feature | Spruce | Graft |
|---------|--------|-------|
| Basic merge | Yes | Yes |
| grab, concat, join | Yes | Yes |
| vault operator | Yes | Yes |
| Array merge operators | Yes | Yes |
| Arithmetic operators | Yes | Yes |
| Boolean operators | Yes | Yes |
| Ternary operator | Yes | Yes |
| go-patch support | Yes | Yes |
| if/elif/else/fi | No | Yes |
| for/while loops | No | Yes |
| case/when/esac | No | Yes |
| awsparam operator | No | Yes |
| awssecret operator | No | Yes |
| nats operator | No | Yes |
| Rich diff output | No | Yes |
| Merge history | No | Yes |
| Interactive REPL | No | Yes |
| Embeddable library | No | Yes |
| Parallel evaluation | No | Yes |

## Command Comparison

All Spruce commands work in Graft:

| Command | Spruce | Graft | Notes |
|---------|--------|-------|-------|
| merge | `spruce merge` | `graft merge` | Identical |
| diff | `spruce diff` | `graft diff` | Graft adds more formats |
| json | `spruce json` | `graft json` | Identical |
| vaultinfo | `spruce vaultinfo` | `graft vaultinfo` | Identical |

Graft adds new commands:

| Command | Description |
|---------|-------------|
| `graft fan` | Cross-product merge |
| `graft debug` | Interactive REPL |

## Flag Comparison

All Spruce flags work in Graft:

| Flag | Spruce | Graft |
|------|--------|-------|
| `--skip-eval` | Yes | Yes |
| `--prune` | Yes | Yes |
| `--cherry-pick` | Yes | Yes |
| `--go-patch` | Yes | Yes |
| `--multi-doc` | Yes | Yes |
| `--fallback-append` | Yes | Yes |

Graft adds new flags:

| Flag | Description |
|------|-------------|
| `--history` | Show merge history |
| `--trace-path` | Trace specific path |
| `--show-changes` | Show merge tree |
| `--changes-only` | Show only changed paths |
| `--interactive` | Enter debug REPL |
| `--side-by-side` | Side-by-side diff |
| `--unified` | Unified diff format |
| `--changes` | Change list format |

## Migration Tips

### 1. Test Your Configurations

Run Graft on your existing configurations and compare output:

```sh
spruce merge base.yml overlay.yml > spruce-output.yml
graft merge base.yml overlay.yml > graft-output.yml
diff spruce-output.yml graft-output.yml
```

### 2. Update CI/CD Scripts

If you're using Spruce in CI/CD, you can:

- Keep using `spruce` command (with alias)
- Update to `graft` command (recommended)

### 3. Take Advantage of New Features

Once migrated, consider using:

- Control flow for cleaner configurations
- Merge history for debugging
- Interactive REPL for development
- Library embedding for custom tools

### 4. Update Documentation

If you have internal documentation referencing Spruce:

- All operator syntax remains the same
- All merge semantics remain the same
- Only new features need documentation updates

## Troubleshooting

### "Unexpected token" errors

Graft has stricter parsing in some edge cases. If you encounter parsing errors:

1. Check for unbalanced parentheses in operators
2. Ensure strings are properly quoted
3. Verify operator syntax

### Different output ordering

Graft may output keys in a different order than Spruce. This is semantically identical but may affect text-based diffs.

### Environment variables

Graft uses the same environment variables as Spruce:

- `VAULT_ADDR`, `VAULT_TOKEN`, etc.

Plus additional variables for new backends:

- `AWS_REGION`, `AWS_PROFILE`
- `NATS_URL`, `NATS_TOKEN`

## Getting Help

- [GitHub Issues](https://github.com/fivetwenty-io/graft/issues) - Report bugs or compatibility issues
- [Examples](../examples/) - See practical usage patterns
- [Operator Reference](../reference/operator-quick-reference.md) - All operators at a glance

# CLI Overview

Graft provides a comprehensive command-line interface for configuration management.

## Commands

| Command | Description |
|---------|-------------|
| [merge](merge.md) | Merge YAML/JSON files |
| [diff](diff.md) | Compare YAML files semantically |
| [json](json.md) | Convert between YAML and JSON |
| [fan](fan.md) | Cross-product merge against multiple targets |
| [vaultinfo](vaultinfo.md) | List all Vault references in documents |
| [debug](debug.md) | Interactive debugging REPL |

## Global Flags

These flags work with all commands:

| Flag | Short | Description |
|------|-------|-------------|
| `--debug` | `-D` | Enable debug logging |
| `--trace` | `-T` | Enable trace logging (verbose) |
| `--version` | `-v` | Show version |
| `--help` | `-h` | Show help |
| `--color` | | Force colorized output on |
| `--no-color` | | Force colorized output off, overriding `--color` |

## Color Output

Control colorized output with the global `--color`/`--no-color` flags
(full precedence rules in
[CLI Reference: Color flags](../../reference/cli.md#color-flags)):

```sh
# Force color on
graft merge --color file.yml

# Disable color
graft merge --no-color file.yml

# Auto-detect (default: neither flag given)
graft merge file.yml
```

`--color=on`/`--color=off`/`--color=auto` also still work, kept for
script compatibility with older graft versions.

**Auto-detection behavior** (neither flag given):

- Color enabled only when `NO_COLOR` is unset, `TERM` isn't `dumb`, and
  the relevant output stream is a terminal (TTY) - stderr for most
  commands' diagnostics, stdout for `diff`'s own colored output
  ([diff command](diff.md#color))
- Color disabled when that stream is piped to a file or another command

**Environment variable:**

There is no `GRAFT_COLOR` variable. Color is disabled by setting `NO_COLOR`
to any non-empty value, or `TERM=dumb`:

```sh
export NO_COLOR=1   # Never use color
```

## Exit Codes

| Code | Description |
|------|-------------|
| 0 | Success |
| 1 | General error |
| 2 | Parse error |
| 3 | Merge error |
| 4 | Evaluation error |
| 5 | Backend error (Vault, AWS, NATS) |
| 6 | File not found |
| 7 | Permission denied |

## Input Sources

### Files

Most common usage - specify file paths:

```sh
graft merge base.yml overlay.yml
```

### Standard Input

Read from stdin using `-`:

```sh
cat config.yml | graft merge - overlay.yml
echo '{"key": "value"}' | graft json --reverse
```

### Multiple Files

Merge any number of files (processed left to right):

```sh
graft merge base.yml env.yml secrets.yml overrides.yml
```

## Output

### Standard Output

By default, results go to stdout:

```sh
graft merge base.yml overlay.yml
```

### Redirect to File

Use shell redirection:

```sh
graft merge base.yml overlay.yml > result.yml
```

### Pipe to Other Commands

Chain with other tools:

```sh
graft merge base.yml overlay.yml | kubectl apply -f -
graft merge base.yml overlay.yml | graft json | jq .database
```

## Common Patterns

### Environment-Specific Builds

```sh
# Development
graft merge base.yml envs/dev.yml > config.yml

# Production
graft merge base.yml envs/prod.yml secrets/prod.yml > config.yml
```

### Conditional Includes

```sh
# Include debugging config only in dev
if [ "$ENV" = "dev" ]; then
  graft merge base.yml debug.yml > config.yml
else
  graft merge base.yml > config.yml
fi
```

### Validation Before Deploy

```sh
# Compare against current config
graft diff current.yml new.yml

# Only deploy if different
if graft diff --quiet current.yml new.yml; then
  echo "No changes"
else
  kubectl apply -f new.yml
fi
```

### CI/CD Integration

```sh
# Generate config, convert to JSON, apply
graft merge base.yml "$ENV.yml" | graft json | aws ecs update-service ...
```

## See Also

- [merge](merge.md) - Merge YAML/JSON files
- [diff](diff.md) - Compare files
- [json](json.md) - Format conversion
- [debug](debug.md) - Interactive debugging

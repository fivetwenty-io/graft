# vaultinfo Command

List all Vault references in documents without evaluating them.

## Usage

```sh
graft vaultinfo [flags] file1.yml file2.yml ...
```

## Flags

| Flag | Short | Description |
|------|-------|--------------|
| `--go-patch` | | Enable the use of go-patch when parsing files to be merged |
| `--json` | | Output as JSON instead of YAML |
| `--paths-only` | | Output only the Vault secret keys (one per line, or a JSON array with `--json`), not their referring locations |

## Overview

The `vaultinfo` command analyzes YAML/JSON files and extracts all Vault operator references. This is useful for:

- Auditing what secrets a configuration needs
- Determining Vault access requirements
- Pre-fetching secrets for offline use
- Security review

## Basic Usage

### Single File

```sh
graft vaultinfo config.yml
```

**config.yml:**
```yaml
database:
  password: (( vault "secret/db:password" ))
  username: (( vault "secret/db:username" ))
api:
  key: (( vault "secret/api:key" ))
```

**Output:**
```yaml
secrets:
- key: secret/api:key
  references:
  - api.key
- key: secret/db:password
  references:
  - database.password
- key: secret/db:username
  references:
  - database.username
```

Every distinct `(( vault "<key>" ))` reference becomes one `secrets` entry,
sorted by key, with its referring dotted paths sorted underneath it. A key
referenced from more than one path lists every one of them under
`references`.

### Multiple Files

```sh
graft vaultinfo base.yml env.yml secrets.yml
```

### With Merge

Analyze the result of a merge:

```sh
graft merge base.yml env.yml | graft vaultinfo -
```

## Output Formats

### Default (YAML)

```sh
graft vaultinfo config.yml
```

Produces the `secrets:`/`key:`/`references:` YAML shape shown above. This
exact shape (unaffected by `--json`/`--paths-only`, both new flags) is what
genesis scrapes from `graft vaultinfo`
(`docs/spruce/genesis-compat-contract.md`), so it never changes.

### JSON Output

```sh
graft vaultinfo --json config.yml
```

**Output:**
```json
{
  "secrets": [
    {
      "key": "secret/api:key",
      "references": [
        "api.key"
      ]
    },
    {
      "key": "secret/db:password",
      "references": [
        "database.password"
      ]
    },
    {
      "key": "secret/db:username",
      "references": [
        "database.username"
      ]
    }
  ]
}
```

`--json` mirrors the default YAML shape exactly (the same `secrets`/`key`/
`references` fields), just indented JSON instead of YAML - it does not
split each key into a separate "Vault path" and "field" the way an
individual secret engine might store them, since graft has no such split
in its own data model.

### Paths Only

```sh
graft vaultinfo --paths-only config.yml
```

**Output:**
```
secret/api:key
secret/db:password
secret/db:username
```

One secret key per line, sorted, with no referring-location information.
Combine with `--json` for a JSON array of the same keys instead:

```sh
graft vaultinfo --paths-only --json config.yml
```

```json
[
  "secret/api:key",
  "secret/db:password",
  "secret/db:username"
]
```

## Use Cases

### Access Audit

Determine what Vault paths a deployment needs access to:

```sh
graft vaultinfo production-config.yml
```

### Policy Generation

Generate Vault policy from config:

```sh
graft vaultinfo --paths-only production-config.yml | while read -r key; do
  path="${key%%:*}"
  echo "path \"$path\" {"
  echo "  capabilities = [\"read\"]"
  echo "}"
done
```

### Pre-Flight Check

Verify Vault paths exist before merge:

```sh
#!/bin/bash
# Check all required secrets are accessible

graft vaultinfo --paths-only config.yml | while read -r key; do
  path="${key%%:*}"
  if ! vault kv get "$path" > /dev/null 2>&1; then
    echo "ERROR: Cannot access $path"
    exit 1
  fi
done

echo "All secrets accessible"
```

### Documentation

Document required secrets for a service:

```sh
echo "# Required Vault Secrets"
echo
graft vaultinfo config.yml
```

### CI/CD Integration

```sh
#!/bin/bash
# Ensure all secrets are available before deploy

echo "Checking Vault access..."
KEYS=$(graft vaultinfo --paths-only config.yml)

for key in $KEYS; do
  path="${key%%:*}"
  if vault kv get "$path" > /dev/null 2>&1; then
    echo "✓ $key"
  else
    echo "✗ $key - ACCESS DENIED"
    exit 1
  fi
done

echo "All secrets accessible, proceeding with merge..."
graft merge base.yml config.yml
```

## Vault Path Formats

The command recognizes various Vault reference formats:

```yaml
# Simple path
password: (( vault "secret/db:password" ))

# With target
password: (( vault@production "secret/db:password" ))

# Multiple paths (fallback)
password: (( vault "secret/v2/db:password; secret/v1/db:password" ))

# With default value
password: (( vault "secret/db:password" || "default" ))

# Dynamic path
password: (( vault (concat "secret/" env "/db:password") ))
```

**Note:** Dynamic paths (using `concat` or `grab`) are shown as expressions, not resolved paths.

## Examples

### Full Audit Report

```sh
# Generate complete audit
echo "# Vault Access Audit"
echo "Date: $(date)"
echo

for env in dev staging prod; do
  echo "## $env environment"
  echo
  graft vaultinfo "configs/$env.yml"
  echo
done
```

### Security Review

```sh
# Find all configs using Vault
find . -name "*.yml" -exec sh -c '
  if graft vaultinfo "$1" 2>/dev/null | grep -q "secrets:"; then
    echo "=== $1 ==="
    graft vaultinfo "$1"
  fi
' _ {} \;
```

## See Also

- [Vault Integration](../secrets/vault.md) - Full Vault operator documentation
- [merge](merge.md) - Merge with Vault evaluation
- [Secrets Management](../secrets/) - All secrets backends

# vaultinfo Command

List all Vault references in documents without evaluating them.

## Usage

```sh
graft vaultinfo [flags] file1.yml file2.yml ...
```

## Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--json` | | Output as JSON |

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
```
Vault paths found in config.yml:

  secret/db
    - password (used at database.password)
    - username (used at database.username)

  secret/api
    - key (used at api.key)
```

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

### Default (Human-Readable)

```sh
graft vaultinfo config.yml
```

**Output:**
```
Vault paths found in config.yml:

  secret/data/production/db
    - password (used at database.password)
    - host (used at database.host)

  secret/data/production/api
    - key (used at api.key)

Summary: 3 Vault references across 2 paths
```

### JSON Output

```sh
graft vaultinfo --json config.yml
```

**Output:**
```json
{
  "paths": [
    {
      "path": "secret/data/production/db",
      "keys": [
        {"key": "password", "location": "database.password"},
        {"key": "host", "location": "database.host"}
      ]
    },
    {
      "path": "secret/data/production/api",
      "keys": [
        {"key": "key", "location": "api.key"}
      ]
    }
  ],
  "summary": {
    "total_references": 3,
    "unique_paths": 2
  }
}
```

## Use Cases

### Access Audit

Determine what Vault paths a deployment needs access to:

```sh
graft vaultinfo production-config.yml > required-paths.txt
```

### Policy Generation

Generate Vault policy from config:

```sh
graft vaultinfo --json config.yml | jq -r '.paths[].path' | while read path; do
  echo "path \"$path\" {"
  echo "  capabilities = [\"read\"]"
  echo "}"
done
```

**Output:**
```hcl
path "secret/data/production/db" {
  capabilities = ["read"]
}
path "secret/data/production/api" {
  capabilities = ["read"]
}
```

### Pre-Flight Check

Verify Vault paths exist before merge:

```sh
#!/bin/bash
# Check all required secrets are accessible

graft vaultinfo --json config.yml | jq -r '.paths[].path' | while read path; do
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
PATHS=$(graft vaultinfo --json config.yml | jq -r '.paths[].path')

for path in $PATHS; do
  if vault kv get "$path" > /dev/null 2>&1; then
    echo "✓ $path"
  else
    echo "✗ $path - ACCESS DENIED"
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
  if graft vaultinfo "$1" 2>/dev/null | grep -q "Vault paths"; then
    echo "=== $1 ==="
    graft vaultinfo "$1"
  fi
' _ {} \;
```

## See Also

- [Vault Integration](../secrets/vault.md) - Full Vault operator documentation
- [merge](merge.md) - Merge with Vault evaluation
- [Secrets Management](../secrets/) - All secrets backends

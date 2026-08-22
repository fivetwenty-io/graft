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
| `--resolve` | | Perform live Vault lookups instead of skipping them (requires a reachable Vault); reports concrete values for paths composed from other Vault lookups instead of a symbolic `<path/to/secret:key>` reference |

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

## Composed Paths and `--resolve`

`vaultinfo` runs offline by default: it never contacts Vault, so a path
segment built from another `(( vault ... ))` lookup elsewhere in the
document can't be resolved to its real value. Instead of silently
reporting the corrupted, literal path `secret/paths:REDACTED`, it renders
a symbolic reference back to the lookup it came from:

```yaml
meta:
  path: (( vault "secret/paths:root" ))

value: (( vault "secret/paths:" meta.path ))
```

```sh
graft vaultinfo config.yml
```

```yaml
secrets:
- key: secret/paths:<secret/paths:root>
  references:
  - value
- key: secret/paths:root
  references:
  - meta.path
```

`<secret/paths:root>` means "whatever `secret/paths:root` resolves to at
merge time" — it is not itself a Vault path to look up. Add `--resolve`
to perform real Vault lookups instead of skipping them, reporting the
concrete composed path when Vault is reachable:

```sh
graft vaultinfo --resolve config.yml
```

```yaml
secrets:
- key: secret/paths:child
  references:
  - value
- key: secret/paths:root
  references:
  - meta.path
```

`--resolve` is opt-in specifically so `vaultinfo` stays usable offline by
default (its main audit/pre-flight use cases below don't need it); reach
for it only when you need the concrete composed path and have Vault
access on hand. `graft merge` itself has always resolved composed paths
correctly at evaluation time, with or without `--resolve` — only
`vaultinfo`'s own offline reporting is affected either way.

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

**Note:** Dynamic paths are resolved before reporting. A path segment
that comes from another `(( vault ... ))` lookup — referenced directly
or routed through `(( grab ))` aliases — is rendered symbolically as
`<path/to/secret:key>`, since `vaultinfo` runs offline and cannot know
its real value (see `docs/user-guide/secrets/vault.md`). A segment
computed with `(( concat ... ))` from such a lookup is a known
limitation: it reports the literal `REDACTED` text in the composed key.

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

# Getting Started with Graft

Welcome to Graft! This section will help you get up and running quickly.

## What is Graft?

Graft is a powerful YAML/JSON configuration merging tool that helps you:

- Merge multiple configuration files into one
- Use operators to dynamically compute values
- Manage secrets from external sources
- Generate environment-specific configurations

## Choose Your Path

### New to Configuration Merging?

If you're new to YAML configuration management:

1. [Install Graft](installation.md)
2. [Follow the Quick Start tutorial](quick-start.md)
3. [Explore the User Guide](../user-guide/)

### Coming from Spruce?

If you're already using Spruce:

1. [Install Graft](installation.md)
2. [Review the Migration Guide](migration-from-spruce.md)
3. Graft runs Spruce's operators, flags, and merge semantics, so existing
   configurations work as they are; the remaining differences are listed in
   [Known Gaps](../spruce/known-gaps.md)

### Want to Embed Graft?

If you want to use Graft as a Go library:

1. [Install Graft](installation.md)
2. [Read the Developer Guide](../developer-guide/)
3. [See Embedding Examples](../examples/web-service-embedding.md)

## Quick Example

Here's what Graft can do:

**base.yml:**
```yaml
database:
  host: localhost
  port: 5432
  pool_size: 10
features:
  - auth
  - logging
```

**production.yml:**
```yaml
database:
  host: db.prod.example.com
  pool_size: (( calc * 5 ))
features:
  - (( prepend ))
  - rate-limiting
  - monitoring
```

`(( calc * 5 ))` is calc's leading-operator form: it multiplies whatever the
same path held in an earlier file of the merge, so `pool_size` becomes
`10 * 5`.

**Command:**
```sh
graft merge base.yml production.yml
```

**Result:**
```yaml
database:
  host: db.prod.example.com
  pool_size: 50
  port: 5432
features:
- rate-limiting
- monitoring
- auth
- logging
```

Graft prints the whole merged document with map keys sorted alphabetically
at every level, and emits list items at their parent key's indentation.

## Next Steps

- [Installation Guide](installation.md) - Get Graft on your system
- [Quick Start](quick-start.md) - 5-minute tutorial
- [CLI Commands](../user-guide/cli/) - Learn the command line interface
- [Operators](../user-guide/operators/) - Explore all 48 operators

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
3. Graft is a drop-in replacement - your existing configs will work!

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
  pool_size: (( calc base.pool_size * 5 ))
features:
  - (( prepend ))
  - rate-limiting
  - monitoring
```

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

## Next Steps

- [Installation Guide](installation.md) - Get Graft on your system
- [Quick Start](quick-start.md) - 5-minute tutorial
- [CLI Commands](../user-guide/cli/) - Learn the command line interface
- [Operators](../user-guide/operators/) - Explore 50+ operators

# Document Memory Examples

This directory contains examples demonstrating the document memory feature that tracks all changes to your YAML/JSON documents throughout the merge and evaluation process.

## Overview

Document memory provides complete visibility into how your configuration values change over time, including:
- What changed (old value → new value)
- When it changed (timestamp)
- Where it changed (merge phase or evaluation phase)
- Why it changed (which file or operator caused the change)

## Enabling Document Memory

Document memory can be enabled via:

1. **Environment Variable**:
   ```bash
   export GRAFT_MEMORY_ENABLED=true
   graft merge config.yaml overrides.yaml
   ```

2. **Command Line Flag**:
   ```bash
   graft merge --track-changes config.yaml overrides.yaml
   ```

3. **Programmatic API**:
   ```go
   engine := graft.NewDefaultEngine()
   engine.EnableMemoryTracking()
   ```

## Example Usage

### Basic History Tracking

```bash
# Enable memory tracking and merge files
GRAFT_MEMORY_ENABLED=true graft merge base.yaml overlay.yaml --output result.yaml

# View the change history
graft history --path "database.host"

# Output:
# Version 1: null → "localhost" (Phase: INITIAL, Source: base.yaml)
# Version 2: "localhost" → "prod-db.example.com" (Phase: MERGE, Source: overlay.yaml)
```

### Querying Changes

```bash
# Show all changes during merge phase
graft history --phase merge

# Show all changes from a specific file
graft history --source overlay.yaml

# Show changes to paths matching a pattern
graft history --path "database.*"

# Show changes within a time range
graft history --after "2024-01-01" --before "2024-01-31"
```

### Comparing Versions

```bash
# Compare two versions of a specific path
graft history compare --path "app.config" --from 1 --to 3

# Show diff between initial and final state
graft history diff base.yaml result.yaml
```

## Example Files

1. **`tracking_example.yaml`** - Shows how values change through merge and evaluation
2. **`memory_config.yaml`** - Configuration options for document memory
3. **`analysis_example.sh`** - Script demonstrating history analysis

## Use Cases

1. **Debugging Configuration Issues**
   - Track down where unexpected values come from
   - See the complete transformation history

2. **Audit Trail**
   - Maintain a complete record of all configuration changes
   - Track which sources contributed to final values

3. **Configuration Analysis**
   - Understand the impact of overlays and operators
   - Identify unused or overridden configurations

4. **Development and Testing**
   - Verify that operators work as expected
   - Test configuration precedence rules

## Performance Considerations

Document memory does add some overhead:
- Memory usage increases with the number of tracked changes
- Slight performance impact during merge/evaluation

For production use, consider:
- Setting memory limits via `GRAFT_MEMORY_MAX_MB`
- Enabling compression for old versions
- Using cleanup intervals to remove old data
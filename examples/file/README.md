# File Operator Examples

The `file` operator reads the contents of a file and inserts it as a string value. Perfect for including scripts, certificates, or configuration snippets.

## Examples in this directory:

1. **basic.yml** - Simple file inclusion
2. **dynamic-paths.yml** - Building file paths dynamically
3. **scripts/**, **configs/**, **certificates/** - sample files created by `setup.sh` for the examples above to read

## Setup:

First, create the sample files:
```bash
./setup.sh
```

## Running the examples:

```bash
# Basic file inclusion
graft merge basic.yml

# Dynamic file paths (setup.sh creates configs/dev and configs/prod)
ENV=prod graft merge dynamic-paths.yml
```

## Important Notes:

- File contents are included as-is (string value)
- Use `(( load ))` if you need to parse YAML/JSON files
- Paths passed to `(( file ... ))` are resolved relative to the current
  working directory; there is no `--file-base-path` flag
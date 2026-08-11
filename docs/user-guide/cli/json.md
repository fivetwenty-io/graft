# json Command

Convert between YAML and JSON formats.

## Usage

```sh
graft json [flags] [files...]
```

By default, converts YAML to JSON. Use `--reverse` to convert JSON to YAML.

## Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--reverse` | `-r` | Convert JSON to YAML |
| `--strict` | | Error on non-string map keys |
| `--multi-doc` | | Wrap the output documents into one pretty-printed JSON array instead of one compact object per line |

## YAML to JSON

### Basic Conversion

```sh
graft json config.yml
```

**config.yml:**
```yaml
database:
  host: localhost
  port: 5432
features:
  - auth
  - logging
```

**Output:**
```json
{
  "database": {
    "host": "localhost",
    "port": 5432
  },
  "features": [
    "auth",
    "logging"
  ]
}
```

### From Stdin

```sh
cat config.yml | graft json
```

### With Merge

```sh
graft merge base.yml overlay.yml | graft json
```

### Multiple Files

```sh
graft json file1.yml file2.yml file3.yml
```

Each file is output as a separate JSON object.

## JSON to YAML

### Basic Conversion

```sh
graft json --reverse config.json
```

**config.json:**
```json
{
  "database": {
    "host": "localhost",
    "port": 5432
  }
}
```

**Output:**
```yaml
database:
  host: localhost
  port: 5432
```

### From Stdin

```sh
echo '{"key": "value"}' | graft json --reverse
```

### From API Response

```sh
curl -s https://api.example.com/config | graft json --reverse
```

## Options

### Strict Mode

Fail if map keys are not strings (JSON requirement):

```sh
graft json --strict config.yml
```

```yaml
# This would fail in strict mode:
123: value  # Non-string key
```

### Multi-Document

A multi-document YAML file is always split into one JSON document per YAML
document; no flag is needed for that. The default prints them as one
compact JSON object per line:

**multi.yml:**
```yaml
---
doc: 1
---
doc: 2
```

```sh
graft json multi.yml
```

**Output:**
```json
{"doc":1}
{"doc":2}
```

`--multi-doc` changes only how those documents are joined: into a single
pretty-printed JSON array, which is what most JSON consumers expect from a
file containing more than one document.

```sh
graft json --multi-doc multi.yml
```

**Output:**
```json
[
  {
    "doc": 1
  },
  {
    "doc": 2
  }
]
```

## Use Cases

### API Consumption

```sh
# Convert YAML config to JSON for API
graft merge base.yml env.yml | graft json > config.json
curl -X POST -d @config.json https://api.example.com/config
```

### jq Processing

```sh
# Use jq on YAML data
graft merge base.yml prod.yml | graft json | jq '.database.host'
```

### Tool Integration

```sh
# Convert for tools expecting JSON
graft json config.yml | aws ssm put-parameter --value file:///dev/stdin ...
```

### Configuration Validation

```sh
# Validate JSON against schema
graft json config.yml | jsonschema -i /dev/stdin schema.json
```

### API Response to YAML

```sh
# Convert API response to YAML
curl -s https://api.example.com/settings | graft json -r > settings.yml
```

## Examples

### Full Pipeline

```sh
# Merge YAML, convert to JSON, extract value
graft merge base.yml prod.yml | graft json | jq -r '.database.host'
```

### Round-Trip

```sh
# YAML → JSON → YAML
graft json config.yml | graft json --reverse > config-roundtrip.yml
```

### Pretty JSON Output

The output is already pretty-printed. For compact JSON, pipe through jq:

```sh
graft json config.yml | jq -c .
```

### Kubernetes Integration

```sh
# Convert YAML to JSON for kubectl
graft merge base.yml | graft json | kubectl apply -f -
```

### Terraform Integration

```sh
# Convert YAML config to JSON for Terraform
graft json vars.yml > terraform.tfvars.json
```

## Notes

### JSON Limitations

JSON has limitations compared to YAML:

- No comments
- No multi-line strings (without escaping)
- No anchors/aliases
- Map keys must be strings

### Handling Special Types

| YAML | JSON |
|------|------|
| `null` | `null` |
| `true` / `false` | `true` / `false` |
| `123` | `123` |
| `1.5` | `1.5` |
| `"string"` | `"string"` |
| `~` (null) | `null` |
| `.inf` | Cannot represent |
| `.nan` | Cannot represent |

### Multi-Document Output

When converting multi-document YAML, output is a JSON array:

```yaml
---
a: 1
---
b: 2
```

Becomes:

```json
[{"a": 1}, {"b": 2}]
```

## See Also

- [merge](merge.md) - Merge configurations before converting
- [Configuration](../configuration.md) - Output options

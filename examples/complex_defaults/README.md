# Complex YAML Defaults Examples

This directory contains examples demonstrating the use of complex YAML structures as defaults in graft expressions.

## Basic Usage

Complex YAML defaults allow you to specify structured data (hashes and arrays) as fallback values when using the `||` operator:

```yaml
config:
  database: (( grab custom_db || { host: "localhost", port: 5432, name: "myapp" } ))
```

## Examples

### 1. `basic_defaults.yaml` - Simple hash and array defaults
Shows how to use basic YAML structures as defaults.

### 2. `nested_defaults.yaml` - Nested structures
Demonstrates deeply nested default structures.

### 3. `production_config.yaml` - Real-world configuration example
Shows how complex defaults can be used for production configurations with sensible defaults.

### 4. `service_discovery.yaml` - Service configuration with defaults
Demonstrates using complex defaults for service discovery and configuration.

## Running the Examples

```bash
# Basic example
graft examples/complex_defaults/basic_defaults.yaml

# Override with custom values
graft examples/complex_defaults/basic_defaults.yaml custom_values.yaml

# Production configuration
graft examples/complex_defaults/production_config.yaml production_overrides.yaml
```

## Benefits

1. **Reduced Configuration Files**: No need for separate default files
2. **Self-Documenting**: Default values are visible in the template
3. **Type Safety**: Defaults maintain their structure and types
4. **Flexibility**: Can combine with other operators like `grab`, `concat`, etc.
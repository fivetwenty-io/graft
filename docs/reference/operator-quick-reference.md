# Operator Quick Reference

Complete reference of all Graft operators with syntax and examples.

## Data Manipulation

### grab

Reference values from elsewhere in the document.

```yaml
value: (( grab path.to.value ))
nested: (( grab deep.nested.path ))
array_item: (( grab items[0].name ))
by_key: (( grab items[name=target].value ))
```

### concat

Concatenate strings and values.

```yaml
url: (( concat "https://" host ":" port "/api" ))
message: (( concat "Hello, " name "!" ))
```

### join

Join array elements with delimiter.

```yaml
csv: (( join "," array ))
path: (( join "/" segments ))
```

### split

Split string into array. Supports PCRE regex.

```yaml
parts: (( split "," csv_string ))
words: (( split "\\s+" text ))
```

### stringify

Convert any value to YAML string.

```yaml
config_str: (( stringify config_map ))
```

### keys

Extract keys from map as array.

```yaml
env_names: (( keys environments ))
```

### base64 / base64-decode

Base64 encoding and decoding.

```yaml
encoded: (( base64 "secret data" ))
decoded: (( base64-decode encoded_string ))
```

### empty

Check if value is empty; clears parent if true.

```yaml
optional: (( empty maybe_null ))
```

### negate

Boolean negation.

```yaml
disabled: (( negate enabled ))
```

### null

Null/nil value handling.

```yaml
cleared: (( null ))
```

### type

Get value type as string.

```yaml
value_type: (( type some_value ))
# Returns: "string", "int", "float", "bool", "map", "array", "nil"
```

## Data Flow & Control

### param

Mark required parameter.

```yaml
password: (( param "Database password is required" ))
```

### inject

Inject map contents at parent level.

```yaml
settings:
  (( inject common_settings ))
  custom: value
```

### defer

Defer evaluation (for template generation).

```yaml
template: (( defer grab runtime.value ))
```

### prune

Mark key for removal from output.

```yaml
internal:
  temp: (( prune ))
  debug: (( prune ))
```

## Arithmetic

### Basic Operators

```yaml
sum: (( 1 + 2 ))           # 3
diff: (( 10 - 3 ))         # 7
product: (( 4 * 5 ))       # 20
quotient: (( 20 / 4 ))     # 5
remainder: (( 17 % 5 ))    # 2
```

### calc

Complex math with functions.

```yaml
result: (( calc "base * rate + offset" ))
clamped: (( calc "max(0, min(100, value))" ))
rounded: (( calc "floor(price * 100) / 100" ))
```

**Functions:** `max()`, `min()`, `mod()`, `pow()`, `sqrt()`, `floor()`, `ceil()`

### Enhanced calc (Value Modification)

```yaml
base:
  timeout: 30

overlay:
  timeout: (( calc * 2 ))    # 60 (30 * 2)
```

## Comparison & Logic

### Equality

```yaml
is_prod: (( env == "production" ))
not_empty: (( value != "" ))
```

### Comparison

```yaml
too_small: (( count < 10 ))
in_range: (( count >= 5 && count <= 100 ))
```

### Boolean Logic

```yaml
allowed: (( is_admin || has_permission ))
ready: (( initialized && !maintenance ))
```

### Ternary Conditional

```yaml
size: (( large ? "8Gi" : "2Gi" ))
host: (( production ? "prod.example.com" : "dev.example.com" ))
```

### Default Values (||)

```yaml
port: (( grab config.port || 8080 ))
host: (( vault "secret/db:host" || "localhost" ))
```

## Control Flow

### if / elif / else / fi

```yaml
(( if grab features.debug ))
log_level: debug
verbose: true
(( elif grab env == "staging" ))
log_level: info
(( else ))
log_level: warn
(( fi ))
```

### for / done

```yaml
# Iterate array
services:
(( for svc in grab service_list ))
  - name: (( grab svc.name ))
    port: (( grab svc.port ))
(( done ))

# Iterate with index
(( for idx, item in grab items ))
  - index: (( grab idx ))
    value: (( grab item ))
(( done ))

# Range iteration
(( for i in range 1 5 ))
  - worker: (( concat "worker-" i ))
(( done ))
```

### while / done

```yaml
(( while grab counter < 10 ))
  - attempt: (( grab counter ))
(( done ))
```

### case / when / esac

```yaml
(( case grab cloud_provider ))
(( when "aws" ))
storage_class: gp3
(( when "gcp" ))
storage_class: pd-ssd
(( when "azure" | "azurerm" ))
storage_class: managed-premium
(( default ))
storage_class: standard
(( esac ))
```

## Array Operations

### append

Add elements to end of array.

```yaml
items:
  - (( append ))
  - new_item
```

### prepend

Add elements to beginning.

```yaml
items:
  - (( prepend ))
  - first_item
```

### replace

Replace entire array.

```yaml
items:
  - (( replace ))
  - only_item
```

### inline

Merge arrays by index position.

```yaml
items:
  - (( inline ))
  - modified_first
```

### merge

Merge arrays by identifier key.

```yaml
items:
  - (( merge ))
  - name: existing
    value: updated

# With explicit key
items:
  - (( merge on id ))
  - id: 123
    data: new
```

### insert

Insert at specific position.

```yaml
items:
  - (( insert before 2 ))
  - inserted_item

items:
  - (( insert after "name=target" ))
  - inserted_after
```

### delete

Remove elements.

```yaml
items:
  - (( delete 2 ))           # By index
  - (( delete "name=old" ))  # By key match
```

### flatten

Flatten nested arrays.

```yaml
flat: (( flatten nested_arrays ))
```

### uniq

Remove duplicate elements.

```yaml
unique: (( uniq array_with_dupes ))
```

### sort

Sort array elements.

```yaml
sorted: (( sort ))
sorted_by: (( sort by name ))
```

### shuffle

Randomize array order.

```yaml
shuffled: (( shuffle array ))
```

### cartesian-product

Generate cartesian product.

```yaml
combinations: (( cartesian-product sizes colors ))
```

## External Sources

### vault

HashiCorp Vault / OpenBao secrets.

```yaml
# Basic
password: (( vault "secret/db:password" ))

# With target
staging: (( vault staging@"secret/db:password" ))

# Multiple paths (fallback)
value: (( vault "secret/v2:key; secret/v1:key" ))

# With default
value: (( vault "secret/key" || "default" ))

# Dynamic path
value: (( vault (concat "secret/" env ":password") ))
```

### awsparam

AWS Parameter Store.

```yaml
# Basic
host: (( awsparam "/app/prod/db_host" ))

# JSON extraction
port: (( awsparam "/app/config?key=database.port" ))

# With target
value: (( awsparam staging@"/app/config" ))
```

### awssecret

AWS Secrets Manager.

```yaml
# Basic
key: (( awssecret "prod/api-key" ))

# JSON extraction
pass: (( awssecret "prod/db?key=password" ))

# With version
value: (( awssecret "prod/db?key=password&stage=AWSCURRENT" ))
```

### nats

NATS JetStream KV/Object store.

```yaml
# KV store
config: (( nats "kv:bucket/key" ))

# Object store
template: (( nats "obj:assets/template.yml" ))

# With target
value: (( nats prod@"kv:config/settings" ))
```

### file

Read file contents as string.

```yaml
cert: (( file "certs/server.pem" ))
```

### load

Load and parse YAML/JSON file.

```yaml
external: (( load "extra-config.yml" ))
```

## IP Operations

### ips

IP arithmetic from CIDR.

```yaml
gateway: (( ips "10.0.0.0/24" 1 ))     # 10.0.0.1
range: (( ips "10.0.0.0/24" 10 5 ))    # 10.0.0.10 - 10.0.0.14
```

### static_ips

BOSH static IP allocation.

```yaml
ips: (( static_ips 0 1 2 ))
```

## Nesting Examples

Operators can be nested arbitrarily:

```yaml
# Nested references
config: (( grab (concat "env." environment ".settings") ))

# Vault with dynamic path
secret: (( vault (concat "secret/" env ":password") ))

# Complex conditional
url: (( concat
    (production ? "https" : "http")
    "://"
    (grab config.host)
    ":"
    (grab config.port || 80)
))

# Arithmetic with references
total: (( (grab base) + (grab tax) * (grab quantity) ))
```

## See Also

- [Operators User Guide](../user-guide/operators/index.md) - Detailed documentation

- [Examples](../examples/index.md) - Real-world usage

- [CLI Quick Reference](cli-quick-reference.md) - Command reference

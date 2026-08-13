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

Get value type as string. Takes exactly one argument.

```yaml
value_type: (( type some_value ))
# Returns one of: string, int, float, bool, array, map, null
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
quotient: (( 20 / 4 ))     # 5.0 — division always yields a float
remainder: (( 17 % 5 ))    # 2
```

### calc

Complex math with functions.

```yaml
result: (( calc "base * rate + offset" ))
clamped: (( calc "max(0, min(100, value))" ))
rounded: (( calc "floor(price * 100) / 100" ))
unquoted: (( calc (a + b) * 2 ))
```

**Functions:** `max()`, `min()`, `mod()`, `pow()`, `sqrt()`, `floor()`, `ceil()`

Functions require the quoted form — `(( calc max(a, b) ))` does not parse.
`**` (exponentiation) and `^` (bitwise XOR, not power) also require the quoted
form; unquoted, neither tokenizes.

### Enhanced calc (Value Modification)

A leading operator modifies the value an **earlier file** in the same merge
gave the same path:

```yaml
# base.yml
timeout: 30
```

```yaml
# overlay.yml
timeout: (( calc * 2 ))
```

`graft merge base.yml overlay.yml` yields `timeout: 60`. Chaining a third
file that modifies the same key again yields `0`.

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
size: '(( large ? "8Gi" : "2Gi" ))'
host: '(( production ? "prod.example.com" : "dev.example.com" ))'
```

Quote the whole expression. A plain YAML scalar cannot contain `: `, so an
unquoted ternary is a YAML parse error before graft ever sees it.

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

graft has no assignment construct, so nothing in a loop body can change the
value its condition tests. A condition that is false emits nothing; one that is
true runs to the iteration cap (default 1000, set with
`GRAFT_MAX_LOOP_ITERATIONS` or `--max-loop-iterations`) and then errors. Use
`for ... in range` instead:

```yaml
attempts:
(( for i in range 0 3 ))
  - attempt: (( grab i ))
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
  - (( insert before 2 ))       # By index
  - inserted_item

items:
  - (( insert after "target" )) # By the entry's name value
  - inserted_after
```

### delete

Remove elements. The string form matches on the entry's `name` value, not on a
`field=value` predicate.

```yaml
items:
  - (( delete 2 ))       # By index
  - (( delete "old" ))   # By name value
```

### flatten

Flatten a list recursively — nested lists at every depth are spliced into one
flat list. Exactly one list argument; there is no depth argument.

```yaml
flat: (( flatten nested_arrays ))
```

### uniq

Remove duplicates, keeping the first occurrence and preserving input order.
It never sorts. Exactly one list argument.

```yaml
unique: (( uniq array_with_dupes ))
# [zebra, apple, zebra, mango, apple] -> [zebra, apple, mango]
```

### sort

A merge marker, not an expression: it replaces a list an earlier document
defined, and sorts the result.

```yaml
# overlay.yml
numbers: (( sort ))
servers: (( sort by name ))
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
staging: (( vault@staging "secret/db:password" ))

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
value: (( awsparam@staging "/app/config" ))
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
value: (( nats@prod "kv:config/settings" ))
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

### raw_env

Read an environment variable as a raw string, with no YAML type coercion.
A set-but-empty variable is a valid (empty string) value; an unset variable
is an error. Fallback branches after `||` are evaluated normally, coercion
included.

```yaml
port: (( raw_env $PORT ))            # "8080" stays a string
debug: (( raw_env $DEBUG || false )) # unset -> boolean false (coerced)
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

An operator call may appear in another operator's argument position:

```yaml
# Nested references
config: (( grab (concat "env." environment ".settings") ))

# Vault with dynamic path
secret: (( vault (concat "secret/" env ":password") ))

# File contents, base64-encoded
blob: (( base64 (file "certs/server.pem") ))

# Arithmetic with references
total: (( (grab base) + (grab tax) * (grab quantity) ))
```

Three shapes do not parse, so keep them out of your documents:

- **An expression split across YAML lines.** Keep the whole `(( ... ))` on one
  line, or quote the scalar.

- **A parenthesised call wrapping the entire expression** —
  `(( (join "," (grab a)) ))` errors with `expected ')' to close parenthesized
  expression`. Write `(( join "," (grab a) ))`.

- **A parenthesised infix or ternary group in *first* argument position** —
  `(( concat (production ? "https" : "http") "://" host ))` errors with
  `expected '))' at end of operator expression, got STRING`. Compute the group
  into its own key first, or move it out of first position. A parenthesised
  *operator call* in first position is fine: `(( concat (grab a) "://" ))`.

## See Also

- [Operators User Guide](../user-guide/operators/index.md) - Detailed documentation

- [Examples](../examples/index.md) - Real-world usage

- [CLI Quick Reference](cli-quick-reference.md) - Command reference

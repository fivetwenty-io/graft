# Operators Overview

Operators are the core of Graft's power. They allow you to dynamically compute values, reference other parts of your configuration, and integrate with external systems.

## Syntax

Operators are written inside double parentheses:

```yaml
key: (( operator arguments ))
```

Operators can be nested:

```yaml
key: (( concat "prefix-" (grab other.value) "-suffix" ))
```

## Operator Categories

### [Data Manipulation](data-manipulation.md)

Transform and reference data within your configuration.

| Operator | Description | Example |
|----------|-------------|---------|
| `grab` | Reference a value | `(( grab database.host ))` |
| `concat` | Concatenate values | `(( concat "a" "b" ))` |
| `join` | Join array with delimiter | `(( join "," items ))` |
| `split` | Split string into array | `(( split "," str ))` |
| `stringify` | Convert to YAML string | `(( stringify data ))` |
| `keys` | Get map keys | `(( keys mymap ))` |
| `base64` | Base64 encode | `(( base64 "text" ))` |
| `base64-decode` | Base64 decode | `(( base64-decode encoded ))` |
| `empty` | Check if empty | `(( empty value ))` |
| `type` | Get value type | `(( type value ))` |
| `null` | Null value | `(( null ))` |

### [Control Flow](control-flow.md)

Conditional logic and iteration.

| Construct | Description | Example |
|-----------|-------------|---------|
| `if/elif/else/fi` | Conditionals | `(( if cond )) ... (( fi ))` |
| `for/done` | Iteration | `(( for x in list )) ... (( done ))` |
| `while/done` | While loop | `(( while cond )) ... (( done ))` |
| `case/when/esac` | Pattern matching | `(( case val )) (( when "x" )) ... (( esac ))` |

### [Arithmetic](arithmetic.md)

Mathematical operations.

| Operator | Description | Example |
|----------|-------------|---------|
| `+` | Addition | `(( 1 + 2 ))` |
| `-` | Subtraction | `(( 5 - 3 ))` |
| `*` | Multiplication | `(( 2 * 3 ))` |
| `/` | Division | `(( 10 / 2 ))` |
| `%` | Modulo | `(( 10 % 3 ))` |
| `calc` | Complex math | `(( calc "sqrt(a^2 + b^2)" ))` |

### [Comparison & Logic](comparison-logic.md)

Boolean operations and comparisons.

| Operator | Description | Example |
|----------|-------------|---------|
| `==` | Equal | `(( a == b ))` |
| `!=` | Not equal | `(( a != b ))` |
| `<` | Less than | `(( a < b ))` |
| `>` | Greater than | `(( a > b ))` |
| `<=` | Less or equal | `(( a <= b ))` |
| `>=` | Greater or equal | `(( a >= b ))` |
| `&&` | Logical AND | `(( a && b ))` |
| `\|\|` | Logical OR | `(( a \|\| b ))` |
| `!` | Logical NOT | `(( ! a ))` |
| `? :` | Ternary | `(( cond ? a : b ))` |

### [Array Operations](array-operations.md)

Manipulate arrays during merge.

| Operator | Description | Example |
|----------|-------------|---------|
| `append` | Add to end | `(( append ))` |
| `prepend` | Add to beginning | `(( prepend ))` |
| `replace` | Replace array | `(( replace ))` |
| `inline` | Merge by index | `(( inline ))` |
| `merge` | Merge by key | `(( merge ))` |
| `insert` | Insert at position | `(( insert after 2 ))` |
| `delete` | Remove elements | `(( delete "name" ))` |
| `flatten` | Flatten nested | `(( flatten array ))` |
| `uniq` | Remove duplicates | `(( uniq array ))` |
| `sort` | Sort array | `(( sort ))` |
| `shuffle` | Randomize order | `(( shuffle array ))` |
| `cartesian-product` | Cross product | `(( cartesian-product a b ))` |

### [External Sources](external-sources.md)

Load data from files and external systems.

| Operator | Description | Example |
|----------|-------------|---------|
| `file` | Read file contents | `(( file "path/to/file" ))` |
| `load` | Load YAML/JSON file | `(( load "config.yml" ))` |

### Secrets (see [Secrets Guide](../secrets/))

| Operator | Description | Example |
|----------|-------------|---------|
| `vault` | HashiCorp Vault | `(( vault "secret/db:pass" ))` |
| `awsparam` | AWS Parameter Store | `(( awsparam "/app/key" ))` |
| `awssecret` | AWS Secrets Manager | `(( awssecret "db-creds" ))` |
| `nats` | NATS JetStream | `(( nats "kv:bucket/key" ))` |

### Special Operators

| Operator | Description | Example |
|----------|-------------|---------|
| `param` | Required parameter | `(( param "error message" ))` |
| `prune` | Remove from output | `(( prune ))` |
| `inject` | Inject map contents | `(( inject ref ))` |
| `defer` | Defer evaluation | `(( defer operator args ))` |

## Operator Precedence

When combining operators, precedence follows mathematical conventions:

1. Parentheses `()`
2. Unary operators: `!`, `-` (negation)
3. Multiplication/Division: `*`, `/`, `%`
4. Addition/Subtraction: `+`, `-`
5. Comparisons: `<`, `>`, `<=`, `>=`
6. Equality: `==`, `!=`
7. Logical AND: `&&`
8. Logical OR: `||`
9. Ternary: `? :`

**Example:**
```yaml
# Evaluated as: (1 + (2 * 3)) == 7
result: (( 1 + 2 * 3 == 7 ))  # true
```

## Default Values

Use `||` to provide defaults:

```yaml
# If grab fails, use default
host: (( grab config.host || "localhost" ))

# Works with any operator
password: (( vault "secret/db:pass" || "default-password" ))
```

## Nesting Operators

Operators can be nested to arbitrary depth:

```yaml
# Simple nesting
url: (( concat "https://" (grab host) ":" (grab port) ))

# Deep nesting
config: (( grab (concat "environments." (grab env) ".settings") ))

# Complex expressions
db_url: (( concat
    "postgres://"
    (grab db.user) ":"
    (vault (concat "secret/" (grab env) "/db:password"))
    "@" (grab db.host) ":" (grab db.port)
    "/" (grab db.name)
))
```

## Error Handling

Operators provide clear error messages with position information:

```
Error at config.yml:15:34
  database:
    password: (( vault "secret/db:pass" || ))
                                         ^^
Expected: expression after '||' operator
Found: '))'

Hint: The '||' operator requires a default value.
Example: (( vault "path:key" || "default" ))
```

## See Also

- [Data Manipulation](data-manipulation.md) - Reference and transform data
- [Control Flow](control-flow.md) - Conditionals and loops
- [Arithmetic](arithmetic.md) - Math operations
- [Comparison & Logic](comparison-logic.md) - Boolean expressions
- [Array Operations](array-operations.md) - Array manipulation
- [External Sources](external-sources.md) - File loading
- [Secrets Guide](../secrets/) - Secrets backends

# Operators Overview

Operators are the core of Graft's power. They allow you to dynamically compute values, reference other parts of your configuration, and integrate with external systems.

Graft registers 47 operators. The tables below group them by what they are for;
[the operator reference](../../reference/operators.md) gives the exact arity,
argument types, and error text for each one.

## Syntax

Operators are written inside double parentheses:

```yaml
key: (( operator arguments ))
```

Operators can be nested:

```yaml
key: (( concat "prefix-" (grab other.value) "-suffix" ))
```

An expression must stay on one YAML line. Splitting `(( ... ))` across lines is
a YAML parse error, not a graft error. An expression containing `: ` — which
every ternary does — has to be quoted, because a plain YAML scalar cannot hold
that sequence:

```yaml
size: '(( large ? "8Gi" : "2Gi" ))'
```

## Operator Categories

### [Data Manipulation](data-manipulation.md)

Transform and reference data within your configuration.

| Operator | Description | Example |
|----------|-------------|---------|
| `grab` | Reference a value | `(( grab database.host ))` |
| `concat` | Concatenate values, two arguments or more | `(( concat "a" "b" ))` |
| `join` | Join array with a literal delimiter | `(( join "," items ))` |
| `split` | Split string into array; a `/`-prefixed delimiter is a PCRE pattern | `(( split "," str ))` |
| `stringify` | Convert to YAML string | `(( stringify data ))` |
| `keys` | Get map keys, sorted | `(( keys mymap ))` |
| `base64` | Base64 encode | `(( base64 "text" ))` |
| `base64-decode` | Base64 decode | `(( base64-decode encoded ))` |
| `empty` | Report whether a value is empty | `(( empty value ))` |
| `type` | Type name: `string`, `int`, `float`, `bool`, `array`, `map`, or `null` | `(( type value ))` |
| `null` | Null value | `(( null ))` |
| `negate` | Boolean negation of one argument | `(( negate enabled ))` |

### [Control Flow](control-flow.md)

Conditional logic and iteration. These are not operators: they are line-oriented
markers expanded by a preprocessor before the file is parsed.

| Construct | Description | Example |
|-----------|-------------|---------|
| `if/elif/else/fi` | Conditionals | `(( if cond )) ... (( fi ))` |
| `for/done` | Iteration over a list, a map, or `range` | `(( for x in list )) ... (( done ))` |
| `while/done` | While loop; nothing in a body can change the condition, so a true condition always runs to the iteration cap and errors | `(( while cond )) ... (( done ))` |
| `case/when/esac` | Pattern matching on exact string equality | `(( case val )) (( when "x" )) ... (( esac ))` |

### [Arithmetic](arithmetic.md)

Mathematical operations.

| Operator | Description | Example |
|----------|-------------|---------|
| `+` | Addition | `(( 1 + 2 ))` |
| `-` | Subtraction | `(( 5 - 3 ))` |
| `*` | Multiplication | `(( 2 * 3 ))` |
| `/` | Division, always producing a float | `(( 10 / 2 ))` |
| `%` | Modulo | `(( 10 % 3 ))` |
| `calc` | Expression evaluation, with functions in the quoted form | `(( calc "sqrt(pow(a, 2) + pow(b, 2))" ))` |

`(( 10 / 2 ))` evaluates to `5.0`, not `5`. Inside a quoted `calc` expression,
`**` is exponentiation and `^` is bitwise XOR; neither tokenizes unquoted.

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
| `&&` | Logical AND, short-circuiting | `(( a && b ))` |
| `\|\|` | Fallback, **not** logical OR: the left value if it resolves, otherwise the right | `(( a \|\| "default" ))` |
| `!` | Logical NOT | `(( ! a ))` |
| `? :` | Ternary; quote the whole scalar | `size: '(( cond ? "a" : "b" ))'` |

### [Array Operations](array-operations.md)

Manipulate arrays during merge, and transform them during evaluation.

| Operator | Description | Example |
|----------|-------------|---------|
| `append` | Add to end of the earlier document's array | `(( append ))` |
| `prepend` | Add to beginning | `(( prepend ))` |
| `replace` | Replace array | `(( replace ))` |
| `inline` | Merge by index | `(( inline ))` |
| `merge` | Merge by key, default `name` | `(( merge on name ))` |
| `insert` | Insert at position | `(( insert after 2 ))` |
| `delete` | Remove elements | `(( delete "worker" ))` |
| `sort` | Sort a list an earlier document defined | `(( sort by priority ))` |
| `flatten` | Flatten one list argument, recursively at every depth | `(( flatten nested ))` |
| `uniq` | Drop duplicates from one list argument, keeping first occurrence and input order | `(( uniq with_dupes ))` |
| `shuffle` | Randomize order | `(( shuffle array ))` |
| `cartesian-product` | Every combination, concatenated into strings | `(( cartesian-product a b ))` |

`append` through `sort` are merge markers: they act on a list an earlier
document supplied, and do nothing useful in a single file. `flatten`, `uniq`,
`shuffle`, and `cartesian-product` are ordinary expressions.

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
| `vault-try` | Vault with fallback paths, superseded by `;`-separated paths in `vault` | `(( vault-try "a:key" "b:key" "default" ))` |
| `awsparam` | AWS Parameter Store | `(( awsparam "/app/key" ))` |
| `awssecret` | AWS Secrets Manager | `(( awssecret "db-creds" ))` |
| `nats` | NATS JetStream | `(( nats "kv:bucket/key" ))` |

A named backend is selected on the operator name, not on the path:
`(( vault@production "secret/db:password" ))`. Each target is configured
through per-target environment variables — `VAULT_<TARGET>_ADDR` and
`VAULT_<TARGET>_TOKEN`, `AWS_<TARGET>_REGION` and friends, `NATS_<TARGET>_URL`.

### Special Operators

| Operator | Description | Example |
|----------|-------------|---------|
| `param` | Required parameter | `(( param "error message" ))` |
| `prune` | Remove from output | `(( prune ))` |
| `inject` | Inject map contents | `(( inject ref ))` |
| `defer` | Defer evaluation | `(( defer operator args ))` |
| `ips` | Addresses from a CIDR block plus offsets | `(( ips "10.0.0.0/24" 1 ))` |
| `static_ips` | BOSH static IP allocation by offset | `(( static_ips 0 1 2 ))` |

## Operator Precedence

When combining operators, precedence follows mathematical conventions:

1. Parentheses `()`

2. Unary operators: `!`, `-` (negation)

3. Multiplication/Division: `*`, `/`, `%`

4. Addition/Subtraction: `+`, `-`

5. Comparisons: `<`, `>`, `<=`, `>=`

6. Equality: `==`, `!=`

7. Logical AND: `&&`

8. Fallback: `||`

9. Ternary: `? :`

**Example:**

```yaml
# precedence.yml
# evaluated as: (1 + (2 * 3)) == 7
result: (( 1 + 2 * 3 == 7 ))
```

**Output** (`graft merge precedence.yml`):

```yaml
result: true
```

## Default Values

`||` is a fallback, not a boolean OR. It returns the left operand whenever that
operand resolves — including when it resolves to `false`, `0`, or `""` — and
only evaluates the right operand when the left one fails:

```yaml
# config.yml
config: {}
host: (( grab config.host || "localhost" ))
```

**Output** (`graft merge config.yml`):

```yaml
config: {}
host: localhost
```

It works with any operator on the left:

```yaml
password: (( vault "secret/db:pass" || "default-password" ))
```

To ask whether either of two conditions holds, use a ternary or negate a
conjunction — `(( ! (! a && ! b) ))` — rather than `||`.

## Nesting Operators

A parenthesized operator call can appear anywhere an argument can:

```yaml
# nest.yml
host: api.example.com
port: 8080
env: prod
environments:
  prod:
    settings:
      replicas: 5

url: (( concat "https://" (grab host) ":" (grab port) ))
config: (( grab (concat "environments." (grab env) ".settings") ))
```

**Output** (`graft merge nest.yml --prune environments`):

```yaml
config:
  replicas: 5
env: prod
host: api.example.com
port: 8080
url: https://api.example.com:8080
```

Two shapes do not parse, and the workaround for both is to move the group out
of first position or into its own key:

- Parentheses wrapping the entire expression: `(( (join "," (grab a)) ))`

- A parenthesized infix or ternary group as the **first** argument:
  `(( concat (a + b) "://" ))` and `(( concat (p ? "x" : "y") "://" ))`. The
  same group in a later argument position is fine.

## Error Handling

Errors are collected and reported together, each one prefixed with the document
path that produced it:

```
1 error(s) detected:
 - $.missing: unable to resolve `does.not.exist`: `$.does` could not be found in the datastructure
```

Parse failures name the file, line, and column, and quote the offending source:

```
config.yml: parse_error: failed to parse YAML: [2:11] mapping value is not allowed in this context
   1 | is_production: true
>  2 | replicas: (( grab is_production ? 5 : 1 ))
                 ^
```

Both cases exit with status 2.

## See Also

- [Data Manipulation](data-manipulation.md) - Reference and transform data

- [Control Flow](control-flow.md) - Conditionals and loops

- [Arithmetic](arithmetic.md) - Math operations

- [Comparison & Logic](comparison-logic.md) - Boolean expressions

- [Array Operations](array-operations.md) - Array manipulation

- [External Sources](external-sources.md) - File loading

- [Secrets Guide](../secrets/) - Secrets backends

- [Operator reference](../../reference/operators.md) - Arity, types, and error text

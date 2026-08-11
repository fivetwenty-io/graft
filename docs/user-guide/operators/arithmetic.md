# Arithmetic Operators

Mathematical operations for computing values dynamically.

## Basic Operators

```yaml
# math.yml
a: 10
b: 5
c: 3

sum: (( a + b ))
difference: (( a - b ))
product: (( a * b ))
quotient: (( a / b ))
remainder: (( a % c ))
```

**Output:**

```yaml
a: 10
b: 5
c: 3
difference: 5
product: 50
quotient: 2.0
remainder: 1
sum: 15
```

Note `quotient`. Division always produces a float, even when the result is a
whole number: `(( 20 / 4 ))` is `5.0`, not `5`. The other four operators keep
integer operands integral. Division by zero is an error:

```
 - $.value: division by zero
```

## Using with References

Operators work with `grab`, and with bare references:

```yaml
# derived.yml
base:
  timeout: 30
  pool_size: 10

derived:
  timeout: (( grab base.timeout * 2 ))
  pool_size: (( base.pool_size + 5 ))
```

**Output:**

```yaml
base:
  pool_size: 10
  timeout: 30
derived:
  pool_size: 15
  timeout: 60
```

## Operator Precedence

Standard mathematical precedence applies:

1. Parentheses `()`

2. Unary minus `-`

3. Multiplication, division, modulo: `*`, `/`, `%`

4. Addition, subtraction: `+`, `-`

```yaml
result1: (( 2 + 3 * 4 ))                  # 14 (not 20)
result2: (( (2 + 3) * 4 ))                # 20
```

## calc Operator

`calc` evaluates a whole expression in one go, and is the only place graft
offers mathematical functions.

### The Three Forms

**Quoted** — the full form, and the only one that accepts function calls:

```yaml
result: (( calc "1 + 2 * 3" ))            # 7
```

**Unquoted** — infix arithmetic and parenthesised grouping, without the
quotes:

```yaml
a: 10
b: 5
result: (( calc a + b ))                  # 15
scaled: (( calc (a + b) * 2 ))            # 30
```

Function calls are not available unquoted. `(( calc max(a, b) ))` fails to
parse, because the parentheses are read as grouping rather than as an argument
list. Use the quoted form for anything with a function in it.

**Leading operator** — modifies a value an earlier file in the same merge
gave this key. See [Modifying a Prior Value](#modifying-a-prior-value).

### Mathematical Functions

Available in the quoted form only:

| Function | Description | Example |
|----------|-------------|---------|
| `max(a, b)` | Maximum | `max(5, 10)` → 10 |
| `min(a, b)` | Minimum | `min(5, 10)` → 5 |
| `mod(a, b)` | Modulo | `mod(10, 3)` → 1 |
| `pow(a, b)` | Power | `pow(2, 3)` → 8 |
| `sqrt(x)` | Square root | `sqrt(16)` → 4 |
| `floor(x)` | Round down | `floor(3.7)` → 3 |
| `ceil(x)` | Round up | `ceil(3.2)` → 4 |

That is the whole list. There is no `abs`, `round`, or `log`; an unknown name
errors with `Undefined function <name>`.

`**` is exponentiation, so `2 ** 3` is `8`. `^` is bitwise XOR, **not** a power
operator — `2 ^ 3` is `1`. Both are quoted-form only; unquoted, each fails to
tokenize (`unexpected token: * (*)` and `unexpected token: INVALID (^)`). There
is no `pi` constant; define a document key if you need one.

### Named Variables

A bare name in a `calc` expression is resolved against the document: first as a
sibling of the key being computed, then from the document root.

```yaml
# scope.yml
size: 100
outer:
  size: 7
  doubled: (( calc "size * 2" ))
root_doubled: (( calc "size * 2" ))
```

**Output:**

```yaml
outer:
  doubled: 14
  size: 7
root_doubled: 200
size: 100
```

Dotted paths work too: `(( calc "base.pool * services" ))`.

A name that resolves nowhere, to `nil`, or to a non-numeric value is an error
rather than a zero:

```
 - $.x: calc operator does not support named variables in expression: nope
 - $.x: path a references a nil value, which cannot be used in calculations
 - $.x: path a is of type string, which cannot be used in calculations
```

### Modifying a Prior Value

Starting a `calc` expression with an operator modifies the value the same path
held **in an earlier file of the same merge**:

```yaml
# base.yml
timeout: 30
memory: 512
```

```yaml
# overlay.yml
timeout: (( calc * 1.5 ))
memory: (( calc + 256 ))
```

**Output** (`graft merge base.yml overlay.yml`):

```yaml
memory: 768
timeout: 45
```

Supported leading operators: `*`, `+`, `-`, `/`, `%`.

Three limits are worth knowing before you rely on this:

- **It spans exactly one merge step.** A third file that modifies the same key
  again yields `0`, because the prior value recorded for it is the second
  file's unevaluated expression text rather than a number.

- **It is not a same-file reference.** Two keys in one document
  (`base.timeout` and `overlay.timeout`) are unrelated paths, so
  `(( calc * 1.5 ))` under `overlay:` sees no prior value.

- **With no prior value at all, the result is `0`.**

### calc Inside Loops

A loop variable is substituted where it appears as a reference, and quoted
string literals are deliberately left alone. So `calc`'s quoted form cannot see
a loop variable:

```yaml
(( for i in range 0 3 ))
  - delay: (( calc "pow(2, i)" ))
(( done ))
```

fails with `calc operator does not support named variables in expression: i`.
Use the unquoted form, which is rewritten like any other reference:

```yaml
# retries.yml
retry_configs:
(( for i in range 0 3 ))
  - attempt: (( grab i ))
    delay_seconds: (( calc (i + 1) * 5 ))
(( done ))
```

**Output:**

```yaml
retry_configs:
- attempt: 0
  delay_seconds: 5
- attempt: 1
  delay_seconds: 10
- attempt: 2
  delay_seconds: 15
- attempt: 3
  delay_seconds: 20
```

Since the unquoted form has no functions, an expression that genuinely needs
`pow` or `sqrt` has to be lifted out of the loop.

## Practical Examples

### Scaling Replicas

An expression containing `? :` must be quoted, because a plain YAML scalar
cannot contain `: `:

```yaml
# replicas.yml
base_replicas: 2
environment: production

replicas: '(( environment == "production" ? base_replicas * 5 : base_replicas ))'
```

**Output:**

```yaml
base_replicas: 2
environment: production
replicas: 10
```

### Resource Calculations

```yaml
# resources.yml
cpu_cores: 4

resources:
  cpu: (( stringify cpu_cores ))
  cpu_limit: (( calc cpu_cores * 2 ))
  memory: (( concat (calc "cpu_cores * 1024") "Mi" ))
```

**Output:**

```yaml
cpu_cores: 4
resources:
  cpu: "4"
  cpu_limit: 8
  memory: 4096Mi
```

`concat` needs at least two arguments, so use `stringify` when you only want to
render a single value as a string.

### Pool Size Calculation

```yaml
# pool.yml
base_pool: 10
num_services: 3

total_connections: (( calc "base_pool * num_services" ))
per_service: (( calc "floor(total_connections / num_services)" ))
```

**Output:**

```yaml
base_pool: 10
num_services: 3
per_service: 10
total_connections: 30
```

### Percentage Calculations

```yaml
# usage.yml
total: 1000
used: 750

usage_percent: (( calc "floor((used / total) * 100)" ))
```

**Output:**

```yaml
total: 1000
usage_percent: 75
used: 750
```

## Floating Point Numbers

graft handles both integers and floating-point numbers:

```yaml
# floats.yml
int_division: (( 10 / 2 ))
float_division: (( 10 / 3 ))
pi: 3.14159
radius: 2
circumference: (( calc "2 * pi * radius" ))
```

**Output:**

```yaml
circumference: 12.56636
float_division: 3.3333333333333335
int_division: 5.0
pi: 3.14159
radius: 2
```

## Error Handling

### Invalid Types

```yaml
str: "hello"
num: 5
result: (( str + num ))
```

```
 - $.result: left operand: cannot use string 'hello' in arithmetic operation
```

### Using Defaults

Guard a division with a ternary, quoted and on one line:

```yaml
# guard.yml
dividend: 100
divisor: 0

safe_ratio: '(( divisor != 0 ? dividend / divisor : 0 ))'
```

**Output:**

```yaml
dividend: 100
divisor: 0
safe_ratio: 0
```

## See Also

- [Operators Overview](index.md) - All operators

- [Comparison & Logic](comparison-logic.md) - Boolean operators

- [Data Manipulation](data-manipulation.md) - grab and concat

- [Control Flow](control-flow.md) - Loops and conditionals

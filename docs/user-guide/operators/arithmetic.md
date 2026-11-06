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

## Division Needs a Space Before the Slash

A `/` attached to the end of a reference is part of the path, not division. This is how BOSH names an instance, as in `postgresql/0`, and it is how spruce reads a path. Division needs a space before the `/`.

```yaml
jobs:
  count: 8
  count/2: half
  postgresql/0:
    ips: [10.0.0.5]

db_ip: (( grab jobs.postgresql/0.ips[0] ))   # 10.0.0.5
named: (( grab jobs.count/2 ))               # half
halved: (( jobs.count / 2 ))                 # 4.0
also_halved: (( jobs.count /2 ))             # 4.0
```

Once a `/` joins a path, the name after it belongs to the path too, and the path carries on through any `.`, `[`, or further `/` that follows. Here is how each placement of the `/` reads:

| Expression | Reads as |
|---|---|
| `jobs.count/2`, `meta.a/b/c` | One path. The last key is `count/2` or `a/b/c`. |
| `postgresql/0.ips[0]` | One path whose first key is `postgresql/0`. |
| `8/2` | The path `8/2`. A number followed directly by a `/` is a path, as it is in spruce. |
| `a / b` and `a /b` | `a` divided by `b`, because a space comes before the `/`. |
| `a/ b` | Two references, `a/` and `b`. The space ends the path `a/`. |
| `a/` | The path `a/`, so the trailing slash is part of the key. |
| `/b` | The key `/b`, wherever no left operand comes before the `/`, as in `(( grab /b ))`, `(( grab c \|\| /b ))`, or `(( concat /b "x" ))`. Spruce reads it the same way. |
| `grab:nocache /b`, `vault@prod /b` | The key `/b`, just as after a plain operator name. A `:modifier` or `@target` glued to the operator name is part of the call, so nothing before the `/` is a left operand. |
| `concat "v=" /b` | Division, because the string is a left operand. Spruce has no arithmetic and reads two arguments, so put the `/b` first or after a comma, as in `(( concat "v=", /b ))`. |
| `1e+5/2`, `1E5/2`, `1.5e3/2` | A path, just as `8/2` is, and spruce reads it that way too. `1.5e3/2` is the key `5e3/2` inside `1`. With a space, as in `1e+5 / 2`, it divides. |
| `3abc`, `3em`, `1e`, `2e5x`, `m.3x` | One key, because a letter or `_` glued after a number makes the whole run a name, as it does in spruce. That holds after a `.` too, so `m.3x` is the key `3x` inside `m`, and `m.3e5` is the key `3e5`. `3-1` still subtracts, because a `-` right after a number is the operator. |
| `grab 1e5`, `concat "a" -1e5 "b"` | The key `1e5`, or `-1e5`. A number in exponent form that stands as a whole operator argument, or as one side of an argument's `\|\|`, is a reference, as it is in spruce. `grab 1.5e3` reads the key `5e3` inside `1`. |
| `1e5 * 2`, `grab 1e5 + 1`, `concat "a" (1e5 * 2)` | A number, because the exponent form is an operand here rather than a whole argument. That holds at the top of an expression, inside `( )`, in a function-style call such as `grab(1e5)`, and wherever an infix operator follows the number. |
| `1e5.x`, `1.foo` | The key `x` inside `1e5`, or `foo` inside `1`. A name segment after a number makes the run a path. |
| `arr/[0]` | The key `arr/`, then index 0 of it. |
| `arr[0]/x` | Index 0 of `arr`, then the key `/x` inside it. |
| `m./x` | The key `/x` inside `m`. This is how `defer` writes `arr[0]/x` back out, as `arr.0./x`. |
| `m.x/b+c` | The key `x/b+c` inside `m`. A `+` followed by a name stays in the path, as it does after a `.`, while `m.x/b+1` adds 1 to the value at `x/b`. |
| `a/$X`, `a/$X+y` | The key `a/$X` or `a/$X+y`. The environment variable is not substituted, because the key doesn't start with `$`, and a `+` name after it stays in the key. |

A whole expression that is only a reference, such as `(( jobs.count/2 ))`, stays in the output as written, just as `(( jobs.count ))` does and as it does in spruce, because BOSH uses that shape for its own variables. Wrap it in `grab` to read the value. Inside `calc`, the quoted expression does its own arithmetic, so `(( calc "8/2" ))` is still 4.

If you are upgrading from 1.45.0 or earlier, an unspaced `/` used to divide, so search your templates for a `/` with no space between two operands and add the space. Two shapes change. A whole expression that divides with no space, such as `(( total/parts ))` or `(( 8/2 ))`, now reads as a reference, so graft leaves it in the output unevaluated and reports no error. An unspaced `/` in the middle of a larger expression, such as `(( a * c/b ))`, now reads `c/b` as a path, so the merge fails with a "could not be found" error unless the document has a key named `c/b`.

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

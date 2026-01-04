# Arithmetic Operators

Mathematical operations for computing values dynamically.

## Basic Operators

### Addition (+)

```yaml
a: 10
b: 5

sum: (( a + b ))                          # 15
```

### Subtraction (-)

```yaml
a: 10
b: 5

difference: (( a - b ))                   # 5
```

### Multiplication (*)

```yaml
a: 10
b: 5

product: (( a * b ))                      # 50
```

### Division (/)

```yaml
a: 10
b: 5

quotient: (( a / b ))                     # 2
```

**Note:** Division by zero returns an error.

### Modulo (%)

```yaml
a: 10
b: 3

remainder: (( a % b ))                    # 1
```

## Using with References

Operators work with `grab`:

```yaml
base:
  timeout: 30
  pool_size: 10

derived:
  timeout: (( grab base.timeout * 2 ))    # 60
  pool_size: (( grab base.pool_size + 5 )) # 15
```

## Operator Precedence

Standard mathematical precedence applies:

1. Parentheses `()`
2. Unary minus `-`
3. Multiplication, Division, Modulo: `*`, `/`, `%`
4. Addition, Subtraction: `+`, `-`

```yaml
# Without parentheses
result1: (( 2 + 3 * 4 ))                  # 14 (not 20)

# With parentheses
result2: (( (2 + 3) * 4 ))                # 20
```

## calc Operator

For complex expressions using the `calc` operator.

**Syntax:**
```yaml
value: (( calc "expression" ))
```

### Basic Expressions

```yaml
# Simple arithmetic
result: (( calc "1 + 2 * 3" ))            # 7

# Using variables from document
a: 10
b: 5
result: (( calc "a + b" ))                # 15
```

### Mathematical Functions

| Function | Description | Example |
|----------|-------------|---------|
| `max(a, b)` | Maximum | `max(5, 10)` → 10 |
| `min(a, b)` | Minimum | `min(5, 10)` → 5 |
| `mod(a, b)` | Modulo | `mod(10, 3)` → 1 |
| `pow(a, b)` | Power | `pow(2, 3)` → 8 |
| `sqrt(x)` | Square root | `sqrt(16)` → 4 |
| `floor(x)` | Round down | `floor(3.7)` → 3 |
| `ceil(x)` | Round up | `ceil(3.2)` → 4 |

**Examples:**
```yaml
# Maximum of values
max_timeout: (( calc "max(api.timeout, db.timeout)" ))

# Power calculation
exponential: (( calc "pow(2, retry_count)" ))

# Square root
diagonal: (( calc "sqrt(pow(width, 2) + pow(height, 2))" ))

# Rounding
memory_gb: (( calc "ceil(memory_mb / 1024)" ))
```

### Enhanced calc Syntax

Start with an operator to modify the current path's value:

```yaml
base:
  timeout: 30
  memory: 512

overlay:
  # These modify the existing values
  timeout: (( calc * 1.5 ))               # 45 (30 * 1.5)
  memory: (( calc + 256 ))                # 768 (512 + 256)
```

**Supported leading operators:**

- `* factor` - Multiply by factor
- `+ value` - Add value
- `- value` - Subtract value
- `/ divisor` - Divide by divisor
- `% divisor` - Modulo by divisor

If the path doesn't exist, `0` is used as the default.

## Practical Examples

### Scaling Replicas

```yaml
base_replicas: 2
environment: production

replicas: (( grab environment == "production"
    ? grab base_replicas * 5
    : grab base_replicas ))
# production: 10, others: 2
```

### Resource Calculations

```yaml
cpu_cores: 4

resources:
  cpu: (( concat (grab cpu_cores) ))
  cpu_limit: (( concat (calc "cpu_cores * 2") ))
  memory: (( concat (calc "cpu_cores * 1024") "Mi" ))
```

### Retry Backoff

```yaml
retry_count: 0

retry_configs:
(( for i in range 0 5 ))
  - attempt: (( grab i ))
    delay_seconds: (( calc "pow(2, i)" ))
    max_delay: (( calc "min(pow(2, i), 60)" ))
(( done ))
```

**Output:**
```yaml
retry_configs:
  - attempt: 0
    delay_seconds: 1
    max_delay: 1
  - attempt: 1
    delay_seconds: 2
    max_delay: 2
  - attempt: 2
    delay_seconds: 4
    max_delay: 4
  - attempt: 3
    delay_seconds: 8
    max_delay: 8
  - attempt: 4
    delay_seconds: 16
    max_delay: 16
  - attempt: 5
    delay_seconds: 32
    max_delay: 32
```

### Pool Size Calculation

```yaml
base_pool: 10
num_services: 3

total_connections: (( calc "base_pool * num_services" ))
per_service: (( calc "floor(total_connections / num_services)" ))
```

### Percentage Calculations

```yaml
total: 1000
used: 750

usage_percent: (( calc "floor((used / total) * 100)" ))
# 75
```

## Floating Point Numbers

Graft handles both integers and floating-point numbers:

```yaml
# Integer result
int_result: (( 10 / 2 ))                  # 5

# Float result
float_result: (( 10 / 3 ))                # 3.333...

# Explicit float
pi: 3.14159
circumference: (( calc "2 * pi * radius" ))
```

## Error Handling

### Division by Zero

```yaml
value: (( 10 / 0 ))
# Error: division by zero
```

### Invalid Types

```yaml
str: "hello"
num: 5
result: (( str + num ))
# Error: cannot add string and int
```

### Using Defaults

```yaml
# Provide default if calculation might fail
safe_ratio: ((
    (grab divisor != 0)
    ? (grab dividend / grab divisor)
    : 0
))
```

## See Also

- [Operators Overview](index.md) - All operators
- [Comparison & Logic](comparison-logic.md) - Boolean operators
- [Data Manipulation](data-manipulation.md) - grab and concat

# Comparison & Logic Operators

Boolean operations for conditional logic and comparisons.

## Comparison Operators

### Equality (==)

Check if values are equal.

```yaml
env: production

is_prod: (( grab env == "production" ))   # true
is_dev: (( grab env == "development" ))   # false
```

### Inequality (!=)

Check if values are not equal.

```yaml
env: production

not_prod: (( grab env != "production" ))  # false
not_dev: (( grab env != "development" ))  # true
```

### Less Than (<)

```yaml
count: 5

under_limit: (( grab count < 10 ))        # true
at_minimum: (( grab count < 5 ))          # false
```

### Greater Than (>)

```yaml
count: 5

over_threshold: (( grab count > 3 ))      # true
exceeds_max: (( grab count > 10 ))        # false
```

### Less Than or Equal (<=)

```yaml
count: 5

at_or_under: (( grab count <= 5 ))        # true
under_max: (( grab count <= 10 ))         # true
```

### Greater Than or Equal (>=)

```yaml
count: 5

at_or_over: (( grab count >= 5 ))         # true
meets_min: (( grab count >= 3 ))          # true
```

## Logical Operators

### Logical AND (&&)

Both conditions must be true.

```yaml
is_production: true
has_ssl: true

secure_prod: (( grab is_production && grab has_ssl ))  # true
```

**Short-circuit evaluation:** If the first operand is false, the second is not evaluated.

```yaml
# Safe to use - second part not evaluated if first is false
safe: (( grab config.enabled && grab config.value > 0 ))
```

### Logical OR (||)

At least one condition must be true.

```yaml
is_production: false
is_staging: true

needs_monitoring: (( grab is_production || grab is_staging ))  # true
```

**Short-circuit evaluation:** If the first operand is true, the second is not evaluated.

**Default values:** `||` is commonly used for defaults:

```yaml
# Use default if grab fails or returns falsy
host: (( grab config.host || "localhost" ))
port: (( grab config.port || 8080 ))
enabled: (( grab config.enabled || false ))
```

### Logical NOT (!)

Negate a boolean value.

```yaml
debug_enabled: true

debug_disabled: (( ! grab debug_enabled ))  # false
```

```yaml
is_production: true

(( if ! grab is_production ))
debug:
  enabled: true
(( fi ))
```

## Ternary Operator (? :)

Conditional expression that returns one of two values.

**Syntax:**
```yaml
value: (( condition ? value_if_true : value_if_false ))
```

**Examples:**
```yaml
is_production: true

replicas: (( grab is_production ? 5 : 1 ))
# production: 5, others: 1

log_level: (( grab is_production ? "warn" : "debug" ))
# production: "warn", others: "debug"
```

### Complex Conditions

```yaml
env: staging
traffic: high

replicas: ((
    grab env == "production" ? 10 :
    grab env == "staging" && grab traffic == "high" ? 5 :
    grab env == "staging" ? 2 :
    1
))
# For staging with high traffic: 5
```

### With Operators

```yaml
use_ssl: true
host: api.example.com
port: 8080

url: (( concat
    (grab use_ssl ? "https" : "http")
    "://"
    (grab host)
    ":"
    (grab port)
))
# "https://api.example.com:8080"
```

## Combining Operators

### Complex Boolean Expressions

```yaml
user:
  role: admin
  verified: true
  active: true

has_access: ((
    grab user.role == "admin" ||
    (grab user.verified && grab user.active)
))
```

### Chained Comparisons

```yaml
temperature: 72

status: ((
    grab temperature < 60 ? "cold" :
    grab temperature < 75 ? "comfortable" :
    grab temperature < 90 ? "warm" :
    "hot"
))
# "comfortable"
```

### Null Checks

```yaml
optional_config:
  # might be empty

# Check before using
setting: ((
    (type grab optional_config) != "null" &&
    (grab optional_config.enabled || false)
    ? grab optional_config.value
    : "default"
))
```

## Operator Precedence

From highest to lowest:

1. `!` (logical NOT)
2. `*`, `/`, `%` (multiplication, division, modulo)
3. `+`, `-` (addition, subtraction)
4. `<`, `>`, `<=`, `>=` (comparisons)
5. `==`, `!=` (equality)
6. `&&` (logical AND)
7. `||` (logical OR)
8. `? :` (ternary)

**Example:**
```yaml
# Evaluated as: ((a < b) && (c > d)) || e
result: (( a < b && c > d || e ))

# Use parentheses for clarity
clear: (( (a < b && c > d) || e ))
```

## Practical Examples

### Feature Flags

```yaml
features:
  new_ui: true
  beta_api: false

settings:
  ui_version: (( grab features.new_ui ? "v2" : "v1" ))
  api_endpoint: (( grab features.beta_api
      ? "/api/beta"
      : "/api/v1" ))
```

### Environment-Based Configuration

```yaml
env: production

database:
  pool_size: ((
      grab env == "production" ? 50 :
      grab env == "staging" ? 20 :
      5
  ))
  ssl: (( grab env == "production" || grab env == "staging" ))
```

### Validation

```yaml
config:
  port: 8080
  timeout: 30

validation:
  port_valid: (( grab config.port > 0 && grab config.port < 65536 ))
  timeout_valid: (( grab config.timeout >= 0 && grab config.timeout <= 300 ))
  config_valid: (( grab validation.port_valid && grab validation.timeout_valid ))
```

### Safe Defaults

```yaml
input:
  # might be missing values

output:
  host: (( grab input.host || "localhost" ))
  port: (( grab input.port || 8080 ))
  ssl: (( grab input.ssl || false ))
  timeout: (( grab input.timeout || 30 ))
```

### Type-Safe Operations

```yaml
value: "not a number"

# Check type before arithmetic
safe_calc: ((
    (type grab value) == "int" || (type grab value) == "float"
    ? (grab value * 2)
    : 0
))
```

## See Also

- [Operators Overview](index.md) - All operators
- [Control Flow](control-flow.md) - if/else, for, case
- [Arithmetic](arithmetic.md) - Math operators

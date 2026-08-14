# Comparison & Logic Operators

Boolean operations for conditional logic and comparisons.

Two things trip up almost every first attempt, so they are worth stating before
anything else:

- **A ternary has to be quoted.** `(( cond ? a : b ))` contains `: `, which a
  plain YAML scalar cannot hold. Single-quote the whole value:
  `size: '(( large ? "8Gi" : "2Gi" ))'`.

- **`||` is a fallback, not a boolean OR.** It returns the left operand
  whenever the left operand resolves, including when it resolves to `false`.

Every output block below is what `graft merge` actually prints: the whole
merged document, map keys in graft's stable sorted order. Where an example would
otherwise be buried in the values it reads from, the `--prune` flag that
removed them is shown with the command.

A bare reference works as an operand, so `(( env == "production" ))` and
`(( grab env == "production" ))` test the same thing.

## Comparison Operators

### Equality (==) and Inequality (!=)

```yaml
# eq.yml
env: production

is_prod: (( env == "production" ))
is_dev: (( env == "development" ))
not_prod: (( env != "production" ))
not_dev: (( env != "development" ))
```

**Output** (`graft merge eq.yml`):

```yaml
env: production
is_dev: false
is_prod: true
not_dev: true
not_prod: false
```

### Ordering (<, >, <=, >=)

```yaml
# cmp.yml
count: 5

under_limit: (( count < 10 ))
at_minimum: (( count < 5 ))
over_threshold: (( count > 3 ))
exceeds_max: (( count > 10 ))
at_or_under: (( count <= 5 ))
at_or_over: (( count >= 5 ))
```

**Output** (`graft merge cmp.yml`):

```yaml
at_minimum: false
at_or_over: true
at_or_under: true
count: 5
exceeds_max: false
over_threshold: true
under_limit: true
```

## Logical Operators

### Logical AND (&&)

Both conditions must be true.

```yaml
# and.yml
is_production: true
has_ssl: true

secure_prod: (( is_production && has_ssl ))
```

**Output** (`graft merge and.yml`):

```yaml
has_ssl: true
is_production: true
secure_prod: true
```

**Short-circuit evaluation:** if the left operand is false, the right is never
evaluated, so a reference that would not resolve is safe there:

```yaml
config:
  enabled: false
safe: (( config.enabled && config.value > 0 ))
```

That yields `safe: false`. Flip `enabled` to `true` and the missing reference
is reached:

```
1 error(s) detected:
 - $.safe: unable to resolve `config.value`: `$.config.value` could not be found in the datastructure
```

### Fallback (||)

`||` is graft's fallback operator. It evaluates the left operand; if that
succeeds, the left value is the answer, whatever it is. Only a left operand
that *fails to resolve* hands control to the right.

```yaml
# fallback.yml
config:
  enabled: false
  retries: 0
  label: ""

enabled: (( config.enabled || true ))
retries: (( config.retries || 3 ))
label: (( config.label || "unnamed" ))
timeout: (( config.timeout || 30 ))
```

**Output** (`graft merge fallback.yml --prune config`):

```yaml
enabled: false
label: ""
retries: 0
timeout: 30
```

Only `timeout` took its default, because only `config.timeout` was missing.
`false`, `0`, and `""` are all values, so they win.

This is what makes `||` useful for defaults:

```yaml
host: (( grab config.host || "localhost" ))
password: (( vault "secret/db:pass" || "default-password" ))
```

**It is not a boolean OR.** To ask whether either of two conditions holds,
negate a conjunction or use a ternary:

```yaml
# either.yml
is_production: false
is_staging: true

wrong: (( is_production || is_staging ))
right: (( ! (! is_production && ! is_staging) ))
also_right: '(( is_production ? true : is_staging ))'
```

**Output** (`graft merge either.yml --prune is_production --prune is_staging`):

```yaml
also_right: true
right: true
wrong: false
```

`wrong` is `false` because `is_production` resolved, so `||` returned it.

### Logical NOT (!)

Negate a boolean value. `nil`, `false`, `0`, `""`, and empty lists and maps are
all falsey; everything else is truthy.

```yaml
# not.yml
debug_enabled: true
debug_disabled: (( ! debug_enabled ))
```

**Output** (`graft merge not.yml`):

```yaml
debug_disabled: false
debug_enabled: true
```

`!` also works in a control-flow condition, where no quoting is needed because
the marker occupies the whole line:

```yaml
is_production: false

(( if ! is_production ))
debug:
  enabled: true
(( fi ))
```

**Output:**

```yaml
debug:
  enabled: true
is_production: false
```

`negate` is a named operator with the same truthiness rules:
`(( negate enabled ))`.

## Ternary Operator (? :)

Conditional expression that returns one of two values.

**Syntax:**

```yaml
value: '(( condition ? value_if_true : value_if_false ))'
```

The single quotes are required. YAML reads `? ` and `: ` inside a plain scalar
as structure, so an unquoted ternary never reaches graft:

```
tern.yml: parse_error: failed to parse YAML: [2:11] mapping value is not allowed in this context
   1 | is_production: true
>  2 | replicas: (( is_production ? 5 : 1 ))
                 ^
```

**Examples:**

```yaml
# tern.yml
is_production: true

replicas: '(( is_production ? 5 : 1 ))'
log_level: '(( is_production ? "warn" : "debug" ))'
```

**Output** (`graft merge tern.yml`):

```yaml
is_production: true
log_level: warn
replicas: 5
```

Only the selected branch is evaluated, so the branch not taken may reference
something that does not exist.

### Complex Conditions

Ternaries chain, and the whole chain still has to be one quoted scalar on one
line.

```yaml
# complex.yml
env: staging
traffic: high

replicas: '(( env == "production" ? 10 : env == "staging" && traffic == "high" ? 5 : env == "staging" ? 2 : 1 ))'
```

**Output** (`graft merge complex.yml`):

```yaml
env: staging
replicas: 5
traffic: high
```

### With Operators

A parenthesized ternary cannot be the **first** argument of an operator call.
`url: '(( concat (use_ssl ? "https" : "http") "://" host ))'` fails with:

```
1 error(s) detected:
 - expected '))' at end of operator expression, got STRING
```

Give the ternary its own key and reference that:

```yaml
# url.yml
use_ssl: true
host: api.example.com
port: 8080

scheme: '(( use_ssl ? "https" : "http" ))'
url: (( concat scheme "://" host ":" port ))
```

**Output** (`graft merge url.yml --prune scheme`):

```yaml
host: api.example.com
port: 8080
url: https://api.example.com:8080
use_ssl: true
```

## Combining Operators

### Complex Boolean Expressions

"Admin, or else verified and active" is an OR, so it needs a ternary rather
than `||`:

```yaml
# access.yml
user:
  role: viewer
  verified: true
  active: true

has_access: '(( user.role == "admin" ? true : user.verified && user.active ))'
```

**Output** (`graft merge access.yml --prune user`):

```yaml
has_access: true
```

Written as `(( user.role == "admin" || (user.verified && user.active) ))` this
returns `false` for a verified, active viewer: the left comparison resolved to
`false`, and `||` returned it.

### Chained Comparisons

```yaml
# temp.yml
temperature: 72

status: '(( temperature < 60 ? "cold" : temperature < 75 ? "comfortable" : temperature < 90 ? "warm" : "hot" ))'
```

**Output** (`graft merge temp.yml`):

```yaml
status: comfortable
temperature: 72
```

### Null Checks

`type` is the reliable guard, because it answers for any value including a
missing one.

```yaml
# nullcheck.yml
optional_config: ~

setting: '(( (type optional_config) == "map" ? optional_config.value : "default" ))'
```

**Output** (`graft merge nullcheck.yml --prune optional_config`):

```yaml
setting: default
```

Give `optional_config` a `value` and the same expression returns it:

```yaml
# nullcheck-present.yml
optional_config:
  value: custom

setting: '(( (type optional_config) == "map" ? optional_config.value : "default" ))'
```

**Output** (`graft merge nullcheck-present.yml --prune optional_config`):

```yaml
setting: custom
```

## Operator Precedence

From highest to lowest:

1. `!` (logical NOT)

2. `*`, `/`, `%` (multiplication, division, modulo)

3. `+`, `-` (addition, subtraction)

4. `<`, `>`, `<=`, `>=` (comparisons)

5. `==`, `!=` (equality)

6. `&&` (logical AND)

7. `||` (fallback)

8. `? :` (ternary)

**Example:**

```yaml
# prec.yml
a: 1
b: 2
c: 4
d: 3

result: (( a < b && c > d ))
```

**Output** (`graft merge prec.yml --prune a --prune b --prune c --prune d`):

```yaml
result: true
```

Parentheses group as usual, and writing them out costs nothing:
`(( (a < b && c > d) || e ))` says explicitly what the precedence table already
implies.

## Practical Examples

### Feature Flags

```yaml
# flags.yml
features:
  new_ui: true
  beta_api: false

settings:
  ui_version: '(( features.new_ui ? "v2" : "v1" ))'
  api_endpoint: '(( features.beta_api ? "/api/beta" : "/api/v1" ))'
```

**Output** (`graft merge flags.yml`):

```yaml
features:
  beta_api: false
  new_ui: true
settings:
  api_endpoint: /api/v1
  ui_version: v2
```

### Environment-Based Configuration

```yaml
# envcfg.yml
env: production

database:
  pool_size: '(( env == "production" ? 50 : env == "staging" ? 20 : 5 ))'
  ssl: '(( env == "production" ? true : env == "staging" ))'
```

**Output** (`graft merge envcfg.yml`):

```yaml
database:
  pool_size: 50
  ssl: true
env: production
```

Change only the environment and both values follow:

```yaml
# envcfg-dev.yml
env: development

database:
  pool_size: '(( env == "production" ? 50 : env == "staging" ? 20 : 5 ))'
  ssl: '(( env == "production" ? true : env == "staging" ))'
```

**Output** (`graft merge envcfg-dev.yml --prune env`):

```yaml
database:
  pool_size: 5
  ssl: false
```

### Validation

```yaml
# valid.yml
config:
  port: 8080
  timeout: 30

validation:
  port_valid: (( config.port > 0 && config.port < 65536 ))
  timeout_valid: (( config.timeout >= 0 && config.timeout <= 300 ))
  config_valid: (( validation.port_valid && validation.timeout_valid ))
```

**Output** (`graft merge valid.yml`):

```yaml
config:
  port: 8080
  timeout: 30
validation:
  config_valid: true
  port_valid: true
  timeout_valid: true
```

### Safe Defaults

```yaml
# defaults.yml
input:
  host: app.example.com

output:
  host: (( grab input.host || "localhost" ))
  port: (( grab input.port || 8080 ))
  ssl: (( grab input.ssl || false ))
  timeout: (( grab input.timeout || 30 ))
```

**Output** (`graft merge defaults.yml`):

```yaml
input:
  host: app.example.com
output:
  host: app.example.com
  port: 8080
  ssl: false
  timeout: 30
```

### Type-Safe Operations

Because the untaken branch is never evaluated, a `type` guard keeps arithmetic
away from values that would fail it.

```yaml
# typesafe.yml
value: "not a number"

doubled: '(( (type value) == "int" ? value * 2 : 0 ))'
```

**Output** (`graft merge typesafe.yml`):

```yaml
doubled: 0
value: not a number
```

With `value: 21` the same file gives `doubled: 42`.

## See Also

- [Operators Overview](index.md) - All operators

- [Control Flow](control-flow.md) - if/else, for, case

- [Arithmetic](arithmetic.md) - Math operators

- [Operator reference](../../reference/operators.md) - Arity, types, and error text

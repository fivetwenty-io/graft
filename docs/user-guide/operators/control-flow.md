# Control Flow

Control flow brings conditional logic, iteration, and pattern matching to YAML
documents.

Unlike every other construct in this guide, control flow is not an operator. A
marker such as `(( if ... ))` occupies a whole line rather than a value
position, its body is raw YAML rather than an expression, and two branches may
legally define the same key. None of that is representable in a parsed
document, so graft expands control flow as a **source-to-source preprocessor**:
each input file's text is rewritten into ordinary YAML before it is parsed, and
everything downstream — merging, operator evaluation, pruning — sees only the
expanded result.

Three consequences follow from that, and they explain most of the surprises:

- **A marker is one line.** A line whose trimmed content is exactly
  `(( <keyword> ... ))`, optionally followed by a YAML comment, is a marker.
  Every other line is body text, copied out verbatim. Markers inside block
  scalars (`|`, `>`) are body text too.

- **Marker indentation is discarded; body indentation is kept.** Where you
  indent `(( if ))` makes no difference. Where you indent the lines inside it
  decides where they land in the document.

- **Expansion happens per file, before any merge.** Conditions, iterables, and
  `case` subjects resolve against the file they appear in, never against a
  value another file contributes.

## Conditionals: if / elif / else / fi

```yaml
(( if condition ))
# content when true
(( elif other_condition ))
# content when the other condition is true
(( else ))
# content when all conditions are false
(( fi ))
```

The condition is an ordinary graft expression, so a bare reference and an
explicit `grab` both work: `(( if environment == "production" ))` and
`(( if grab environment == "production" ))` are the same test.

### Simple if/else

```yaml
# app.yml
environment: production

(( if grab environment == "production" ))
replicas: 5
resources:
  memory: 4Gi
  cpu: "2"
(( else ))
replicas: 1
resources:
  memory: 512Mi
  cpu: "0.5"
(( fi ))
```

**Output** (`graft merge app.yml`):

```yaml
environment: production
replicas: 5
resources:
  cpu: "2"
  memory: 4Gi
```

Keys come out alphabetically sorted, as in every graft merge; the branch you
wrote is spliced in, not reordered around.

### Multiple Branches

```yaml
# env.yml
environment: staging

(( if grab environment == "production" ))
replicas: 5
log_level: warn
(( elif grab environment == "staging" ))
replicas: 2
log_level: info
(( elif grab environment == "development" ))
replicas: 1
log_level: debug
(( else ))
replicas: 1
log_level: trace
(( fi ))
```

**Output:**

```yaml
environment: staging
log_level: info
replicas: 2
```

Only the selected branch is evaluated. An `else` body that references a missing
key costs nothing when the `if` matched.

### Nested Conditionals

Indent the body to place it inside an enclosing key. Here `auth:` is written at
the top level and the inner branch bodies are indented two spaces, so they
become its children:

```yaml
# auth.yml
features:
  auth_enabled: true
  auth_type: oauth
oauth:
  client_id: abc123

(( if grab features.auth_enabled ))
auth:
  (( if grab features.auth_type == "oauth" ))
  provider: oauth2
  client_id: (( grab oauth.client_id ))
  (( elif grab features.auth_type == "saml" ))
  provider: saml
  metadata_url: (( grab saml.metadata_url ))
  (( else ))
  provider: basic
  (( fi ))
(( fi ))
```

**Output:**

```yaml
auth:
  client_id: abc123
  provider: oauth2
features:
  auth_enabled: true
  auth_type: oauth
oauth:
  client_id: abc123
```

### Boolean Expressions

```yaml
is_production: true
needs_ssl: true

(( if grab is_production && grab needs_ssl ))
ssl:
  enabled: true
  cert_path: /etc/ssl/cert.pem
(( fi ))

(( if grab is_production || grab needs_ssl ))
security:
  headers:
    strict-transport-security: "max-age=31536000"
(( fi ))

(( if ! grab is_production ))
debug:
  enabled: true
(( fi ))
```

## Iteration: for / done

```yaml
(( for item in collection ))
# content using item
(( done ))

(( for key, value in map ))
# content using key and value
(( done ))
```

Over a list, the two-variable form binds index and element. Over a map, it
binds key and value, with keys visited in sorted order. The single-variable
form over a map binds the value.

### Iterate Over a List

```yaml
# services.yml
service_list:
  - name: api
    port: 8080
  - name: web
    port: 80
  - name: worker
    port: 9090

services:
(( for svc in grab service_list ))
  - name: (( grab svc.name ))
    port: (( grab svc.port ))
    url: (( concat "http://" (grab svc.name) ":" (grab svc.port) ))
(( done ))
```

The loop source stays in the output like any other key, so drop it with
`--prune` when it is only scaffolding.

**Output** (`graft merge --prune service_list services.yml`):

```yaml
services:
- name: api
  port: 8080
  url: http://api:8080
- name: web
  port: 80
  url: http://web:80
- name: worker
  port: 9090
  url: http://worker:9090
```

### Iterate With Index

```yaml
# indexed.yml
items:
  - alpha
  - beta
  - gamma

indexed:
(( for idx, item in grab items ))
  - index: (( grab idx ))
    value: (( grab item ))
    priority: (( calc idx * 10 ))
(( done ))
```

**Output** (`graft merge --prune items indexed.yml`):

```yaml
indexed:
- index: 0
  priority: 0
  value: alpha
- index: 1
  priority: 10
  value: beta
- index: 2
  priority: 20
  value: gamma
```

### Iterate Over a Map

```yaml
# envvars.yml
environment:
  DB_HOST: localhost
  DB_PORT: "5432"
  API_KEY: secret123

env_vars:
(( for key, value in grab environment ))
  - name: (( grab key ))
    value: (( grab value ))
(( done ))
```

**Output** (`graft merge --prune environment envvars.yml`):

```yaml
env_vars:
- name: API_KEY
  value: secret123
- name: DB_HOST
  value: localhost
- name: DB_PORT
  value: "5432"
```

Any expression that yields a list or a map may be the iterable, including an
operator call — `(( for k in keys database ))` iterates a map's key names.

An empty list or map is not an error; the loop emits nothing, which
leaves the surrounding key null.

### Using range

`range` generates a sequence over a **closed** interval — both bounds are
included.

```yaml
# workers.yml
workers:
(( for i in range 1 5 ))
  - name: (( concat "worker-" i ))
    id: (( grab i ))
(( done ))
```

**Output:**

```yaml
workers:
- id: 1
  name: worker-1
- id: 2
  name: worker-2
- id: 3
  name: worker-3
- id: 4
  name: worker-4
- id: 5
  name: worker-5
```

**Range with step:**

```yaml
# evens.yml
evens:
(( for i in range 0 10 2 ))
  - (( grab i ))
(( done ))
```

**Output:**

```yaml
evens:
- 0
- 2
- 4
- 6
- 8
- 10
```

Bounds and step may be any expression that evaluates to an integer. A step of
zero, or one pointing away from the end bound, is an error:

```
range step must be non-zero and must move start toward end
```

`range` yields one value per iteration, so the two-variable form
(`(( for k, v in range 1 3 ))`) is rejected with `range yields a single value
per iteration`.

## While Loops: while / done

```yaml
(( while condition ))
# content
(( done ))
```

**`while` is of limited use today, and most documents should reach for
`for ... in range` instead.** graft has no assignment, increment, or `set`
construct, so nothing inside a loop body can change the value its condition
tests. A condition that is false at the start emits nothing; a condition that
is true stays true and runs until the iteration cap, which is a hard error:

```
w.yml: parse_error: control flow expansion failed: $.controlflow.while.L3: while loop exceeded maximum iterations (1000)
```

The exit code is 2. The cap defaults to 1000 and can be changed two ways, the
flag winning when both are given:

```bash
GRAFT_MAX_LOOP_ITERATIONS=5 graft merge config.yml
graft merge --max-loop-iterations 7 config.yml
```

A retry-backoff table is the case `while` looks made for, and it is exactly the
case `for` handles better:

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

## Pattern Matching: case / when / default / esac

```yaml
(( case expression ))
(( when pattern1 ))
# content for pattern1
(( when pattern2 | pattern3 ))
# content for pattern2 or pattern3
(( default ))
# default content
(( esac ))
```

Matching is exact equality against the stringified subject — no globs, no
regular expressions. The first matching `when` wins and there is no
fallthrough. Patterns are literals: a quoted string, a number, or `true`/`false`.
A bare name is rejected rather than looked up:

```
(( when )) pattern must be a quoted string, a number, or true/false, got "p"
```

`default` is optional but must come last. A subject that matches nothing, with
no `default` present, emits nothing.

### Simple Matching

```yaml
# cloud.yml
cloud_provider: aws

(( case grab cloud_provider ))
(( when "aws" ))
storage_class: gp3
region_prefix: us-
(( when "gcp" ))
storage_class: pd-ssd
region_prefix: us-central-
(( when "azure" ))
storage_class: managed-premium
region_prefix: eastus-
(( default ))
storage_class: standard
region_prefix: local-
(( esac ))
```

**Output:**

```yaml
cloud_provider: aws
region_prefix: us-
storage_class: gp3
```

### Multiple Patterns

```yaml
# replicas.yml
environment: prod

(( case grab environment ))
(( when "prod" | "production" ))
replicas: 5
(( when "stg" | "staging" | "uat" ))
replicas: 2
(( default ))
replicas: 1
(( esac ))
```

**Output:**

```yaml
environment: prod
replicas: 5
```

### Nested Case

Keep nested bodies at the indentation you want them to land at. Indenting them
under the marker does not nest them under the outer `case` — it nests them
under whatever mapping is open in the surrounding document.

```yaml
# k8s.yml
deployment:
  type: kubernetes
  size: large

(( case grab deployment.type ))
(( when "kubernetes" ))
  (( case grab deployment.size ))
  (( when "small" ))
resources:
  cpu: 100m
  memory: 128Mi
  (( when "medium" ))
resources:
  cpu: 500m
  memory: 512Mi
  (( when "large" ))
resources:
  cpu: 1000m
  memory: 1Gi
  (( esac ))
(( when "docker" ))
resources:
  cpu_shares: 512
(( esac ))
```

**Output:**

```yaml
deployment:
  size: large
  type: kubernetes
resources:
  cpu: 1000m
  memory: 1Gi
```

## Combining Control Flow

Blocks nest inside one another and mix freely with operators:

```yaml
# active.yml
services:
  - name: api
    enabled: true
    replicas: 3
  - name: worker
    enabled: false
    replicas: 1
  - name: web
    enabled: true
    replicas: 2

active_services:
(( for svc in grab services ))
  (( if grab svc.enabled ))
  - name: (( grab svc.name ))
    replicas: (( grab svc.replicas ))
  (( fi ))
(( done ))
```

**Output** (`graft merge --prune services active.yml`):

```yaml
active_services:
- name: api
  replicas: 3
- name: web
  replicas: 2
```

## Keyword Aliases

Each closing keyword has a longer spelling, which helps when `for` and `while`
nest and both would otherwise end in `(( done ))`:

| Canonical | Alias |
|-----------|-------|
| `elif` | `elsif` |
| `fi` | `endif` |
| `done` | `endfor`, `endwhile` |
| `esac` | `endcase` |

## Rules and Limits

- **Blocks must be closed**

  `fi`, `done`, and `esac` are all mandatory. An unclosed block reports
  `unclosed block: expected (( elif )) or (( else )) or (( fi )), reached end
  of input`; a stray closer reports `(( done )) at line 2 has no matching block
  start`.

- **Clause order is enforced**

  `elif` after `else`, a second `else`, or a `when` after `default` each get
  a diagnostic naming the ordering rule rather than a generic parse failure.

- **Nesting is bounded at 64**

  Deeper nesting errors with `control flow block nesting too deep (max 64)`.

- **Loop variables shadow document keys**

  For the extent of a loop body, the bound name resolves to the current
  element, even if the document has a key of the same name. Inner loops shadow
  outer ones, and the binding does not escape the body.

- **Loop variables are not visible inside quoted strings**

  The binding works by rewriting the name where it appears as a reference, and
  quoted string literals are deliberately left alone so that
  `(( concat "https://name.example.com/" name ))` is not corrupted. The
  practical consequence is that `calc`'s quoted form cannot see a loop
  variable: `(( calc "pow(2, i)" ))` inside a `for i in ...` fails with `calc
  operator does not support named variables in expression: i`. Use calc's
  unquoted form — `(( calc (i + 1) * 5 ))` — which is rewritten like any other
  reference. Unquoted calc has no function calls, so an expression needing
  `pow` or `sqrt` has to be lifted out of the loop.

- **`__graft_loop` is a reserved top-level key**

  Loop bindings are materialised under it during expansion and pruned before
  output. A document that defines its own top-level `__graft_loop` loses it,
  and one that defines it alongside control flow fails with a
  duplicate-mapping-key parse error. Rename the key.

- **Loops cannot iterate another file's data**

  Expansion runs per file before any merge, so a loop whose iterable is defined
  only in a different merged file fails at expansion time rather than picking
  the value up later:

  ```
  loop.yml: parse_error: control flow expansion failed: $.controlflow.for.L2: unable to resolve `svcs`: `$.svcs` could not be found in the datastructure
  ```

  Move the loop into the file that defines the data, or define the data in the
  file that loops over it.

- **Errors are reported as parse errors**

  Control flow is expanded before YAML parsing, so its failures carry graft's
  `parse_error` prefix and a synthetic path naming the construct and its source
  line (`$.controlflow.<construct>.L<line>`). The exit code is 2.

- **`--skip-eval` preserves the bindings**

  Under `--skip-eval` the `__graft_loop` block survives into the intermediate
  document alongside the unevaluated `(( grab __graft_loop... ))` references,
  so the intermediate can be fed back through graft. A later evaluated pass
  resolves the references and prunes the bindings.

## See Also

- [Operators Overview](index.md) - All operators

- [Comparison & Logic](comparison-logic.md) - Boolean operators for conditions

- [Examples: Conditional Configs](../../examples/conditional-configs.md) - More examples

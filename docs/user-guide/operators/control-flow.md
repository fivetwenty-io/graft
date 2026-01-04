# Control Flow Operators

Control flow operators enable conditional logic, iteration, and pattern matching within YAML documents.

## Conditionals: if / elif / else / fi

Include content based on conditions.

**Syntax:**
```yaml
(( if condition ))
# content when true
(( elif other_condition ))
# content when other condition is true
(( else ))
# content when all conditions are false
(( fi ))
```

### Simple if/else

```yaml
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

**Output:**
```yaml
environment: production
replicas: 5
resources:
  memory: 4Gi
  cpu: "2"
```

### Multiple Branches

```yaml
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

### Nested Conditionals

```yaml
features:
  auth_enabled: true
  auth_type: oauth

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

Iterate over arrays or maps.

**Syntax:**
```yaml
(( for item in collection ))
# content using item
(( done ))

(( for key, value in map ))
# content using key and value
(( done ))
```

### Iterate Over Array

```yaml
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

**Output:**
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

**Output:**
```yaml
indexed:
  - index: 0
    value: alpha
    priority: 0
  - index: 1
    value: beta
    priority: 10
  - index: 2
    value: gamma
    priority: 20
```

### Iterate Over Map

```yaml
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

**Output:**
```yaml
env_vars:
  - name: API_KEY
    value: secret123
  - name: DB_HOST
    value: localhost
  - name: DB_PORT
    value: "5432"
```

### Using range

Generate sequences with `range`:

```yaml
workers:
(( for i in range 1 5 ))
  - name: (( concat "worker-" i ))
    id: (( grab i ))
(( done ))
```

**Output:**
```yaml
workers:
  - name: worker-1
    id: 1
  - name: worker-2
    id: 2
  - name: worker-3
    id: 3
  - name: worker-4
    id: 4
  - name: worker-5
    id: 5
```

**Range with step:**
```yaml
# range start end step
(( for i in range 0 10 2 ))
# i = 0, 2, 4, 6, 8, 10
(( done ))
```

## While Loop: while / done

Loop while a condition is true.

**Syntax:**
```yaml
(( while condition ))
# content
(( done ))
```

**Note:** While loops have a configurable maximum iteration limit to prevent infinite loops (default: 1000).

### Example

```yaml
retry_count: 0
max_retries: 3

retry_configs:
(( while grab retry_count < grab max_retries ))
  - attempt: (( grab retry_count ))
    delay: (( calc pow(2, retry_count) ))
(( done ))
```

## Pattern Matching: case / when / default / esac

Match values against patterns.

**Syntax:**
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

### Simple Matching

```yaml
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

### Multiple Patterns

```yaml
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

### Nested Case

```yaml
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

## Combining Control Flow

Control flow operators can be combined with other operators:

```yaml
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

**Output:**
```yaml
active_services:
  - name: api
    replicas: 3
  - name: web
    replicas: 2
```

## Control Flow Requirements

- **Block validation**

  Matching open/close constructs are validated (if/fi, for/done, case/esac)

- **Scope isolation**

  Variables defined within loops are scoped to that iteration

- **Short-circuit evaluation**

  `&&` and `||` use short-circuit logic

- **Loop safety**

  Maximum iteration limits prevent infinite loops (configurable)

- **Nesting support**

  Control flow blocks can be nested to arbitrary depth

## See Also

- [Operators Overview](index.md) - All operators
- [Comparison & Logic](comparison-logic.md) - Boolean operators for conditions
- [Examples: Conditional Configs](../../examples/conditional-configs.md) - More examples

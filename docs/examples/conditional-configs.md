# Conditional Configurations

This guide demonstrates Graft's control flow constructs for creating dynamic,
conditional configurations.

Control flow is a source-to-source preprocessor: each file's text is rewritten
into ordinary YAML before it is parsed. Two facts from that shape every example
below.

- A marker's own indentation is discarded, but the indentation of the lines
  inside a block is kept verbatim. Where you indent the body decides where it
  lands in the document.

- Expansion happens per file, before any merge, and the source data a loop
  iterates stays in the output like any other key. Drop it with `--prune` when
  it is only scaffolding.

Graft prints the whole merged document with keys sorted at every level
in a stable, spruce-compatible order (numeric-looking keys first, then
strings with digit runs compared numerically), and list items at the
same indentation as their parent key. Every output block below is
exactly what the shown command prints.

See [Control Flow Operators](../user-guide/operators/control-flow.md) for the
complete reference.

## If/Elif/Else Conditionals

Use conditional blocks to include or exclude configuration sections based on
conditions.

### Simple If/Else

**config.yml:**

```yaml
environment: production

(( if environment == "production" ))
database:
  host: db.prod.example.com
  replicas: 3
  ssl: true
(( else ))
database:
  host: localhost
  replicas: 1
  ssl: false
(( fi ))
```

```sh
graft merge config.yml
```

**Output:**

```yaml
database:
  host: db.prod.example.com
  replicas: 3
  ssl: true
environment: production
```

The condition is an ordinary graft expression, so a bare reference works
without `grab`. `(( if environment == "production" ))` and
`(( if grab environment == "production" ))` are the same test.

### Multi-Branch with Elif

**config.yml:**

```yaml
environment: staging

(( if environment == "production" ))
resources:
  cpu: "4"
  memory: 8Gi
  replicas: 5
(( elif environment == "staging" ))
resources:
  cpu: "2"
  memory: 4Gi
  replicas: 2
(( elif environment == "development" ))
resources:
  cpu: "1"
  memory: 2Gi
  replicas: 1
(( else ))
resources:
  cpu: "0.5"
  memory: 512Mi
  replicas: 1
(( fi ))
```

```sh
graft merge config.yml
```

**Output:**

```yaml
environment: staging
resources:
  cpu: "2"
  memory: 4Gi
  replicas: 2
```

Only the selected branch is evaluated, so an unselected branch may reference
keys that do not exist.

### Boolean Conditions

**config.yml:**

```yaml
features:
  debug: true
  ssl: true
  metrics: false

(( if features.debug ))
logging:
  level: debug
  verbose: true
(( else ))
logging:
  level: info
  verbose: false
(( fi ))

(( if features.ssl ))
server:
  protocol: https
  port: 443
(( else ))
server:
  protocol: http
  port: 80
(( fi ))

(( if features.metrics ))
metrics:
  enabled: true
  endpoint: /metrics
(( fi ))
```

```sh
graft merge config.yml
```

**Output:**

```yaml
features:
  debug: true
  metrics: false
  ssl: true
logging:
  level: debug
  verbose: true
server:
  port: 443
  protocol: https
```

Note that the `metrics` block is entirely absent since `features.metrics` is
false.

### Compound Conditions

**config.yml:**

```yaml
environment: production
region: us-east

(( if environment == "production" && region == "us-east" ))
database:
  host: db.us-east.prod.example.com
  primary: true
(( elif environment == "production" && region == "eu-west" ))
database:
  host: db.eu-west.prod.example.com
  primary: false
(( else ))
database:
  host: localhost
  primary: false
(( fi ))
```

```sh
graft merge config.yml
```

**Output:**

```yaml
database:
  host: db.us-east.prod.example.com
  primary: true
environment: production
region: us-east
```

### Negation and OR Conditions

**config.yml:**

```yaml
is_public: false
requires_auth: true

(( if !is_public || requires_auth ))
security:
  authentication:
    enabled: true
    provider: oauth2
  rate_limiting:
    enabled: true
    requests_per_minute: 100
(( fi ))
```

```sh
graft merge config.yml
```

**Output:**

```yaml
is_public: false
requires_auth: true
security:
  authentication:
    enabled: true
    provider: oauth2
  rate_limiting:
    enabled: true
    requests_per_minute: 100
```

## Nested Conditionals

Conditionals nest inside one another. Because the marker's indentation is
discarded and the body's is kept, the inner branch bodies below are indented two
spaces so they land under `provider:`.

**config.yml:**

```yaml
cloud: aws
tier: premium
region: us-east-1

(( if cloud == "aws" ))
provider:
  name: Amazon Web Services
  (( if tier == "premium" ))
  support:
    level: enterprise
    response_time: 1h
  (( else ))
  support:
    level: basic
    response_time: 24h
  (( fi ))
  regions:
    - us-east-1
    - us-west-2
(( elif cloud == "gcp" ))
provider:
  name: Google Cloud Platform
  regions:
    - us-central1
    - europe-west1
(( fi ))
```

```sh
graft merge config.yml
```

**Output:**

```yaml
cloud: aws
provider:
  name: Amazon Web Services
  regions:
  - us-east-1
  - us-west-2
  support:
    level: enterprise
    response_time: 1h
region: us-east-1
tier: premium
```

## For Loops

Iterate over lists or maps to generate repeated structures.

### Basic Array Iteration

**config.yml:**

```yaml
services:
  - api
  - worker
  - scheduler

deployments:
(( for svc in services ))
  - name: (( grab svc ))
    image: (( concat "myapp/" svc ":latest" ))
    replicas: 1
(( done ))
```

```sh
graft merge config.yml
```

**Output:**

```yaml
deployments:
- image: myapp/api:latest
  name: api
  replicas: 1
- image: myapp/worker:latest
  name: worker
  replicas: 1
- image: myapp/scheduler:latest
  name: scheduler
  replicas: 1
services:
- api
- worker
- scheduler
```

The `services` list is a normal document key, so it survives into the output.
Add `--prune services` when it is only there to drive the loop. The remaining
loop examples do exactly that.

### Iterating Over Objects

**config.yml:**

```yaml
service_configs:
  - name: api
    port: 8080
    replicas: 3
  - name: worker
    port: 8081
    replicas: 2
  - name: scheduler
    port: 8082
    replicas: 1

kubernetes:
  deployments:
  (( for svc in service_configs ))
    - apiVersion: apps/v1
      kind: Deployment
      metadata:
        name: (( grab svc.name ))
      spec:
        replicas: (( grab svc.replicas ))
        template:
          spec:
            containers:
              - name: (( grab svc.name ))
                port: (( grab svc.port ))
  (( done ))
```

```sh
graft merge --prune service_configs config.yml
```

**Output:**

```yaml
kubernetes:
  deployments:
  - apiVersion: apps/v1
    kind: Deployment
    metadata:
      name: api
    spec:
      replicas: 3
      template:
        spec:
          containers:
          - name: api
            port: 8080
  - apiVersion: apps/v1
    kind: Deployment
    metadata:
      name: worker
    spec:
      replicas: 2
      template:
        spec:
          containers:
          - name: worker
            port: 8081
  - apiVersion: apps/v1
    kind: Deployment
    metadata:
      name: scheduler
    spec:
      replicas: 1
      template:
        spec:
          containers:
          - name: scheduler
            port: 8082
```

### Iterating with Index

Over a list, the two-variable form binds the index and the element.

**config.yml:**

```yaml
zones:
  - us-east-1a
  - us-east-1b
  - us-east-1c

subnets:
(( for idx, zone in zones ))
  - name: (( concat "subnet-" idx ))
    availability_zone: (( grab zone ))
    cidr: (( concat "10.0." idx ".0/24" ))
(( done ))
```

```sh
graft merge --prune zones config.yml
```

**Output:**

```yaml
subnets:
- availability_zone: us-east-1a
  cidr: 10.0.0.0/24
  name: subnet-0
- availability_zone: us-east-1b
  cidr: 10.0.1.0/24
  name: subnet-1
- availability_zone: us-east-1c
  cidr: 10.0.2.0/24
  name: subnet-2
```

### Iterating Over Map Keys

Over a map, the two-variable form binds the key and the value, with keys
visited in sorted order.

**config.yml:**

```yaml
environment_vars:
  DATABASE_URL: postgres://localhost/myapp
  REDIS_URL: redis://localhost:6379
  LOG_LEVEL: info

container:
  env:
  (( for key, value in environment_vars ))
    - name: (( grab key ))
      value: (( grab value ))
  (( done ))
```

```sh
graft merge --prune environment_vars config.yml
```

**Output:**

```yaml
container:
  env:
  - name: DATABASE_URL
    value: postgres://localhost/myapp
  - name: LOG_LEVEL
    value: info
  - name: REDIS_URL
    value: redis://localhost:6379
```

### Using Range for Numeric Iteration

`range` generates a sequence over a closed interval, so both bounds are
included.

**config.yml:**

```yaml
workers:
(( for i in range 1 5 ))
  - name: (( concat "worker-" i ))
    id: (( grab i ))
(( done ))
```

```sh
graft merge config.yml
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

### Range with Step

**config.yml:**

```yaml
ports:
(( for port in range 8080 8090 2 ))
  - (( grab port ))
(( done ))
```

```sh
graft merge config.yml
```

**Output:**

```yaml
ports:
- 8080
- 8082
- 8084
- 8086
- 8088
- 8090
```

A step of zero, or one pointing away from the end bound, is an error:

```
range step must be non-zero and must move start toward end
```

## Retry Tables Instead of While Loops

`while` is spelled `(( while condition ))` ... `(( done ))`, but it is of
little practical use. Graft has no assignment, so nothing inside the body can
change the value the condition tests. A condition that is false at the start
emits nothing; a condition that is true stays true and runs until the iteration
cap, which is a hard error.

**spin.yml:**

```yaml
counter: 0
limit: 5

attempts:
(( while counter < limit ))
  - pending
(( done ))
```

```sh
graft merge spin.yml
```

**Output** (exit code 2):

```
spin.yml: parse_error: control flow expansion failed: $.controlflow.while.L5: while loop exceeded maximum iterations (1000)
```

The cap defaults to 1000 and can be raised or lowered two ways, the flag
winning when both are given:

```sh
GRAFT_MAX_LOOP_ITERATIONS=7 graft merge spin.yml
graft merge --max-loop-iterations 7 spin.yml
```

A retry-and-backoff table is the case `while` looks made for, and it is exactly
the case `for ... in range` handles better.

**backoff.yml:**

```yaml
max_retries: 4
base_delay: 5

retries:
(( for attempt in range 0 max_retries ))
  - attempt: (( grab attempt ))
    delay_seconds: (( calc base_delay * (attempt + 1) ))
(( done ))
```

```sh
graft merge --prune max_retries --prune base_delay backoff.yml
```

**Output:**

```yaml
retries:
- attempt: 0
  delay_seconds: 5
- attempt: 1
  delay_seconds: 10
- attempt: 2
  delay_seconds: 15
- attempt: 3
  delay_seconds: 20
- attempt: 4
  delay_seconds: 25
```

The delay uses `calc`'s unquoted form on purpose. A loop variable is bound by
rewriting the name where it appears as a reference, and quoted string literals
are left alone, so `calc`'s quoted form cannot see `attempt`:
`(( calc "pow(2, attempt)" ))` fails with `calc operator does not support named
variables in expression: attempt`. Unquoted `calc` handles infix arithmetic and
parentheses but no function calls, so an exponential table has to be written
out as a literal list.

## Case/When Pattern Matching

Match a value against multiple patterns. Matching is exact string equality, the
first matching `when` wins, and there is no fallthrough. Patterns must be
literals: a quoted string, a number, or `true`/`false`.

### Basic Case

**config.yml:**

```yaml
cloud_provider: aws

(( case cloud_provider ))
(( when "aws" ))
storage:
  type: s3
  class: STANDARD
(( when "gcp" ))
storage:
  type: gcs
  class: STANDARD
(( when "azure" ))
storage:
  type: blob
  class: Hot
(( default ))
storage:
  type: local
  class: filesystem
(( esac ))
```

```sh
graft merge config.yml
```

**Output:**

```yaml
cloud_provider: aws
storage:
  class: STANDARD
  type: s3
```

`default` is optional but must come last. A subject that matches nothing, with
no `default` present, emits nothing.

### Multiple Patterns per When

**config.yml:**

```yaml
environment: prod

(( case environment ))
(( when "prod" | "production" ))
settings:
  debug: false
  replicas: 5
  log_level: warn
(( when "stg" | "staging" | "uat" ))
settings:
  debug: true
  replicas: 2
  log_level: info
(( when "dev" | "development" | "local" ))
settings:
  debug: true
  replicas: 1
  log_level: debug
(( default ))
settings:
  debug: true
  replicas: 1
  log_level: trace
(( esac ))
```

```sh
graft merge config.yml
```

**Output:**

```yaml
environment: prod
settings:
  debug: false
  log_level: warn
  replicas: 5
```

### Nested Case Statements

Keep nested bodies at the indentation you want them to land at. Indenting a
body under the inner `(( case ))` marker does not nest it under the outer case;
it nests it under whatever mapping is open in the surrounding document. Below
the inner markers are indented for readability while their bodies stay at the
top level, so `resources:` is a sibling of `platform:`.

**config.yml:**

```yaml
deployment:
  type: kubernetes
  size: large

(( case deployment.type ))
(( when "kubernetes" ))
platform: k8s
  (( case deployment.size ))
  (( when "small" ))
resources:
  cpu: 100m
  memory: 128Mi
  replicas: 1
  (( when "medium" ))
resources:
  cpu: 500m
  memory: 512Mi
  replicas: 2
  (( when "large" ))
resources:
  cpu: 1000m
  memory: 1Gi
  replicas: 3
  (( esac ))
(( when "docker" ))
platform: docker-compose
resources:
  cpu_shares: 512
  memory_limit: 512m
(( when "vm" ))
platform: virtual-machine
resources:
  vcpus: 2
  memory_gb: 4
(( esac ))
```

```sh
graft merge config.yml
```

**Output:**

```yaml
deployment:
  size: large
  type: kubernetes
platform: k8s
resources:
  cpu: 1000m
  memory: 1Gi
  replicas: 3
```

## Combining Control Flow with Operators

Control flow works alongside every other Graft operator.

### Conditionals with Grab and Concat

**config.yml:**

```yaml
meta:
  app_name: my-service
  environment: production
  version: 2.0.0

(( if meta.environment == "production" ))
app:
  name: (( concat meta.app_name "-" meta.environment ))
  image: (( concat "registry.example.com/" meta.app_name ":" meta.version ))
  url: (( concat "https://" meta.app_name ".example.com" ))
(( else ))
app:
  name: (( concat meta.app_name "-" meta.environment ))
  image: (( concat "localhost/" meta.app_name ":dev" ))
  url: (( concat "http://localhost:8080" ))
(( fi ))
```

```sh
graft merge config.yml
```

**Output:**

```yaml
app:
  image: registry.example.com/my-service:2.0.0
  name: my-service-production
  url: https://my-service.example.com
meta:
  app_name: my-service
  environment: production
  version: 2.0.0
```

### Loops with Conditionals

**config.yml:**

```yaml
services:
  - name: api
    public: true
    port: 8080
  - name: worker
    public: false
    port: 8081
  - name: admin
    public: true
    port: 8082

ingress:
  rules:
  (( for svc in services ))
    (( if svc.public ))
    - host: (( concat svc.name ".example.com" ))
      port: (( grab svc.port ))
    (( fi ))
  (( done ))
```

```sh
graft merge --prune services config.yml
```

**Output:**

```yaml
ingress:
  rules:
  - host: api.example.com
    port: 8080
  - host: admin.example.com
    port: 8082
```

### Vault Secrets with Conditionals

**config.yml:**

```yaml
environment: production

database:
  host: db.example.com
  port: 5432
  name: myapp
  (( if environment == "production" ))
  password: (( vault "secret/prod/db:password" ))
  (( else ))
  password: (( vault "secret/dev/db:password" ))
  (( fi ))
```

With Vault configured and reachable, `graft merge config.yml` fills
`database.password` in from `secret/prod/db`. Without it, the merge fails:

```
1 error(s) detected:
 - $.database.password: Error during Vault client initialization: failed to determine Vault URL / token, and the $REDACT environment variable is not set
```

Set `REDACT=1` to check the structure without contacting Vault at all. Every
`vault` lookup then resolves to the literal `REDACTED`:

```sh
REDACT=1 graft merge config.yml
```

**Output:**

```yaml
database:
  host: db.example.com
  name: myapp
  password: REDACTED
  port: 5432
environment: production
```

## Real-World Example: Kubernetes Deployment Generator

**deployment-config.yml:**

```yaml
app:
  name: my-api
  version: 1.5.0

environments:
  - name: development
    namespace: dev
    replicas: 1
    resources:
      cpu: 100m
      memory: 256Mi
  - name: production
    namespace: prod
    replicas: 5
    resources:
      cpu: 500m
      memory: 1Gi

deployments:
(( for env in environments ))
  - apiVersion: apps/v1
    kind: Deployment
    metadata:
      name: (( concat app.name "-" env.name ))
      namespace: (( grab env.namespace ))
      labels:
        app: (( grab app.name ))
        version: (( grab app.version ))
        environment: (( grab env.name ))
    spec:
      replicas: (( grab env.replicas ))
      selector:
        matchLabels:
          app: (( grab app.name ))
      template:
        metadata:
          labels:
            app: (( grab app.name ))
            version: (( grab app.version ))
        spec:
          containers:
            - name: (( grab app.name ))
              image: (( concat "myregistry/" app.name ":" app.version ))
              resources:
                requests:
                  cpu: (( grab env.resources.cpu ))
                  memory: (( grab env.resources.memory ))
                limits:
                  cpu: (( grab env.resources.cpu ))
                  memory: (( grab env.resources.memory ))
(( done ))
```

```sh
graft merge deployment-config.yml --prune app --prune environments
```

**Output:**

```yaml
deployments:
- apiVersion: apps/v1
  kind: Deployment
  metadata:
    labels:
      app: my-api
      environment: development
      version: 1.5.0
    name: my-api-development
    namespace: dev
  spec:
    replicas: 1
    selector:
      matchLabels:
        app: my-api
    template:
      metadata:
        labels:
          app: my-api
          version: 1.5.0
      spec:
        containers:
        - image: myregistry/my-api:1.5.0
          name: my-api
          resources:
            limits:
              cpu: 100m
              memory: 256Mi
            requests:
              cpu: 100m
              memory: 256Mi
- apiVersion: apps/v1
  kind: Deployment
  metadata:
    labels:
      app: my-api
      environment: production
      version: 1.5.0
    name: my-api-production
    namespace: prod
  spec:
    replicas: 5
    selector:
      matchLabels:
        app: my-api
    template:
      metadata:
        labels:
          app: my-api
          version: 1.5.0
      spec:
        containers:
        - image: myregistry/my-api:1.5.0
          name: my-api
          resources:
            limits:
              cpu: 500m
              memory: 1Gi
            requests:
              cpu: 500m
              memory: 1Gi
```

Add an environment to the `environments` list and a full manifest appears for
it, with no template duplicated.

## Common Pitfalls

- **Loops cannot iterate another file's data**

  Expansion runs per file before any merge, so a loop whose iterable is defined
  only in a different merged file fails at expansion time. Move the loop into
  the file that defines the data.

- **Loop variables are invisible inside quoted strings**

  A binding is applied by rewriting the name where it appears as a reference,
  and string literals are deliberately left alone. This is why `concat "https://name.example.com/" name`
  is not corrupted, and why `calc`'s quoted form cannot see a loop variable.

- **`__graft_loop` is a reserved top-level key**

  Loop bindings live under it during expansion and are pruned before output. A
  document that defines its own top-level `__graft_loop` alongside control flow
  fails with a duplicate-mapping-key parse error. Rename the key.

- **Control flow errors are parse errors**

  Expansion precedes YAML parsing, so failures carry graft's `parse_error`
  prefix and a synthetic path naming the construct and its source line
  (`$.controlflow.<construct>.L<line>`). The exit code is 2.

## See Also

- [Basic Merging](basic-merging.md) - Fundamental merge operations

- [Multi-Environment Setups](multi-environment.md) - Environment management patterns

- [Control Flow Operators](../user-guide/operators/control-flow.md) - Complete reference

- [Operator Reference](../reference/operator-quick-reference.md) - All operators

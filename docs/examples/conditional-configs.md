# Conditional Configurations

This guide demonstrates Graft's control flow operators for creating dynamic, conditional configurations.

## If/Elif/Else Conditionals

Use conditional blocks to include or exclude configuration sections based on conditions.

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

Note that the `metrics` block is entirely absent since `features.metrics` is false.

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

Conditionals can be nested for complex logic.

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

Iterate over arrays or maps to generate repeated structures.

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
graft merge config.yml
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
```

### Iterating with Index

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
graft merge config.yml
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
zones:
  - us-east-1a
  - us-east-1b
  - us-east-1c
```

### Iterating Over Map Keys

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
graft merge config.yml
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
environment_vars:
  DATABASE_URL: postgres://localhost/myapp
  LOG_LEVEL: info
  REDIS_URL: redis://localhost:6379
```

### Using Range for Numeric Iteration

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

## While Loops

Execute blocks while a condition is true.

**config.yml:**

```yaml
retry_attempts: 5

retry_configs:
  attempts: []

# Note: while loops have safety limits to prevent infinite loops
(( while retry_count < retry_attempts ))
retry_settings:
(( for i in range 1 retry_attempts ))
  - attempt: (( grab i ))
    delay_seconds: (( calc i * 2 ))
    max_delay: (( calc i * i ))
(( done ))
(( done ))
```

A more practical example using while:

**backoff.yml:**

```yaml
max_retries: 4
base_delay: 1

retries:
(( for attempt in range 0 max_retries ))
  - attempt: (( grab attempt ))
    delay: (( calc base_delay * (2 ** attempt) ))
(( done ))
```

```sh
graft merge backoff.yml
```

**Output:**

```yaml
base_delay: 1
max_retries: 4
retries:
  - attempt: 0
    delay: 1
  - attempt: 1
    delay: 2
  - attempt: 2
    delay: 4
  - attempt: 3
    delay: 8
  - attempt: 4
    delay: 16
```

## Case/When Pattern Matching

Match values against multiple patterns.

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

Control flow works seamlessly with other Graft operators.

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
graft merge config.yml
```

**Output:**

```yaml
ingress:
  rules:
    - host: api.example.com
      port: 8080
    - host: admin.example.com
      port: 8082
services:
  - name: api
    port: 8080
    public: true
  - name: worker
    port: 8081
    public: false
  - name: admin
    port: 8082
    public: true
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
  password: (( vault "secret/dev/db:password" || "dev-password" ))
  (( fi ))
```

When Vault is configured and accessible:

```sh
graft merge config.yml
```

**Output (with Vault):**

```yaml
database:
  host: db.example.com
  name: myapp
  password: <value-from-vault>
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
  - name: staging
    namespace: staging
    replicas: 2
    resources:
      cpu: 250m
      memory: 512Mi
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

This generates complete Kubernetes Deployment manifests for all environments.

## See Also

- [Basic Merging](basic-merging.md) - Fundamental merge operations
- [Multi-Environment Setups](multi-environment.md) - Environment management patterns
- [Control Flow Operators](../user-guide/operators/control-flow.md) - Complete reference
- [Operator Reference](../reference/operator-quick-reference.md) - All operators

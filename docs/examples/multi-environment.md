# Multi-Environment Configuration

This guide demonstrates patterns for managing configurations across development, staging, and production environments using Graft.

## Directory Structure

A typical multi-environment setup organizes files by concern:

```
config/
  base.yml              # Shared base configuration
  environments/
    development.yml     # Dev-specific overrides
    staging.yml         # Staging-specific overrides
    production.yml      # Production-specific overrides
  secrets/
    development.yml     # Dev secrets (may use defaults)
    staging.yml         # Staging secrets
    production.yml      # Production secrets (Vault references)
  features/
    defaults.yml        # Default feature flags
    experiments.yml     # A/B test configurations
```

## Base Configuration

The base configuration contains all shared settings with sensible defaults.

**config/base.yml:**

```yaml
meta:
  app_name: my-service
  version: 1.0.0
  team: platform

server:
  host: 0.0.0.0
  port: 8080
  read_timeout: 30
  write_timeout: 30
  graceful_shutdown: 15

database:
  driver: postgres
  port: 5432
  name: (( concat meta.app_name "_db" ))
  pool:
    min_connections: 5
    max_connections: 25
    idle_timeout: 300

cache:
  driver: redis
  port: 6379
  ttl: 3600
  prefix: (( concat meta.app_name ":" ))

logging:
  level: info
  format: json
  output: stdout

features:
  rate_limiting: true
  caching: true
  metrics: true
  tracing: false

health:
  path: /health
  interval: 10
```

## Environment-Specific Overrides

Each environment overrides only what differs from the base.

### Development

**config/environments/development.yml:**

```yaml
meta:
  environment: development

server:
  port: 3000

database:
  host: localhost
  pool:
    min_connections: 2
    max_connections: 10

cache:
  host: localhost

logging:
  level: debug

features:
  tracing: true

development:
  hot_reload: true
  mock_external_services: true
```

### Staging

**config/environments/staging.yml:**

```yaml
meta:
  environment: staging

server:
  port: 8080

database:
  host: db.staging.internal
  pool:
    min_connections: 5
    max_connections: 25

cache:
  host: redis.staging.internal

logging:
  level: info

features:
  tracing: true
```

### Production

**config/environments/production.yml:**

```yaml
meta:
  environment: production

server:
  port: 443
  read_timeout: 60
  write_timeout: 60

database:
  host: db.prod.internal
  pool:
    min_connections: 10
    max_connections: 100
    idle_timeout: 600

cache:
  host: redis.prod.internal
  ttl: 7200

logging:
  level: warn

features:
  rate_limiting: true
  caching: true
  metrics: true
  tracing: true

production:
  replicas: 5
  autoscaling:
    min: 3
    max: 10
    target_cpu: 70
```

## Secrets Configuration

Secrets files contain references to secret backends or development defaults.

### Development Secrets

**config/secrets/development.yml:**

```yaml
database:
  user: dev_user
  password: dev_password

cache:
  password: ""

api_keys:
  stripe: sk_test_development_key
  sendgrid: SG.development_key

jwt:
  secret: development-jwt-secret-not-for-production
```

### Staging Secrets

**config/secrets/staging.yml:**

```yaml
database:
  user: staging_user
  password: (( vault "secret/staging/database:password" ))

cache:
  password: (( vault "secret/staging/redis:password" ))

api_keys:
  stripe: (( vault "secret/staging/stripe:api_key" ))
  sendgrid: (( vault "secret/staging/sendgrid:api_key" ))

jwt:
  secret: (( vault "secret/staging/jwt:secret" ))
```

### Production Secrets

**config/secrets/production.yml:**

```yaml
database:
  user: prod_user
  password: (( vault "secret/production/database:password" ))

cache:
  password: (( vault "secret/production/redis:password" ))

api_keys:
  stripe: (( vault "secret/production/stripe:api_key" ))
  sendgrid: (( vault "secret/production/sendgrid:api_key" ))

jwt:
  secret: (( vault "secret/production/jwt:secret" ))

certificates:
  tls_cert: (( vault "secret/production/tls:certificate" ))
  tls_key: (( vault "secret/production/tls:private_key" ))
```

## Building Environment Configurations

### Simple Merge Script

**build-config.sh:**

```bash
#!/bin/bash
set -e

ENV=${1:-development}

graft merge \
  config/base.yml \
  config/environments/${ENV}.yml \
  config/secrets/${ENV}.yml \
  > generated/${ENV}-config.yml

echo "Generated ${ENV} configuration"
```

### Usage

```sh
# Build development config
./build-config.sh development

# Build staging config
./build-config.sh staging

# Build production config
./build-config.sh production
```

### Makefile Integration

**Makefile:**

```makefile
ENVIRONMENTS := development staging production

.PHONY: config
config: $(addprefix config-,$(ENVIRONMENTS))

config-%:
	@mkdir -p generated
	graft merge \
		config/base.yml \
		config/environments/$*.yml \
		config/secrets/$*.yml \
		> generated/$*-config.yml
	@echo "Generated $* configuration"

.PHONY: validate
validate: config
	@for env in $(ENVIRONMENTS); do \
		echo "Validating $$env..."; \
		graft merge generated/$$env-config.yml > /dev/null; \
	done
	@echo "All configurations valid"

.PHONY: diff
diff:
	graft diff generated/staging-config.yml generated/production-config.yml

.PHONY: clean
clean:
	rm -rf generated/
```

## Environment Detection Patterns

### Using Environment Variables

**config/base.yml:**

```yaml
meta:
  environment: (( grab $ENVIRONMENT || "development" ))

database:
  host: (( grab $DATABASE_HOST || "localhost" ))
  port: (( grab $DATABASE_PORT || 5432 ))
```

```sh
ENVIRONMENT=production DATABASE_HOST=db.prod.internal graft merge config/base.yml
```

### Dynamic Environment Selection

**config/environments.yml:**

```yaml
_environments:
  development:
    database_host: localhost
    cache_host: localhost
    log_level: debug
  staging:
    database_host: db.staging.internal
    cache_host: redis.staging.internal
    log_level: info
  production:
    database_host: db.prod.internal
    cache_host: redis.prod.internal
    log_level: warn

current_env: (( grab $ENVIRONMENT || "development" ))

# Dynamic selection
selected: (( grab (concat "_environments." current_env) ))

database:
  host: (( grab selected.database_host ))

cache:
  host: (( grab selected.cache_host ))

logging:
  level: (( grab selected.log_level ))
```

## Resource Scaling by Environment

**config/resources.yml:**

```yaml
environment: (( grab $ENVIRONMENT || "development" ))

(( case environment ))
(( when "production" ))
resources:
  replicas: 5
  cpu:
    request: 500m
    limit: 1000m
  memory:
    request: 512Mi
    limit: 1Gi
  autoscaling:
    enabled: true
    min_replicas: 3
    max_replicas: 20
    target_cpu_percent: 70
(( when "staging" ))
resources:
  replicas: 2
  cpu:
    request: 250m
    limit: 500m
  memory:
    request: 256Mi
    limit: 512Mi
  autoscaling:
    enabled: true
    min_replicas: 1
    max_replicas: 5
    target_cpu_percent: 80
(( default ))
resources:
  replicas: 1
  cpu:
    request: 100m
    limit: 200m
  memory:
    request: 128Mi
    limit: 256Mi
  autoscaling:
    enabled: false
(( esac ))
```

## Feature Flags per Environment

**config/features/defaults.yml:**

```yaml
features:
  authentication:
    enabled: true
    provider: oauth2
  rate_limiting:
    enabled: true
    requests_per_minute: 100
  caching:
    enabled: true
    strategy: redis
  metrics:
    enabled: true
    provider: prometheus
  tracing:
    enabled: false
    provider: jaeger
  new_checkout_flow:
    enabled: false
    percentage: 0
```

**config/features/development.yml:**

```yaml
features:
  rate_limiting:
    enabled: false
  tracing:
    enabled: true
    sample_rate: 1.0
  new_checkout_flow:
    enabled: true
    percentage: 100
```

**config/features/staging.yml:**

```yaml
features:
  tracing:
    enabled: true
    sample_rate: 0.5
  new_checkout_flow:
    enabled: true
    percentage: 50
```

**config/features/production.yml:**

```yaml
features:
  rate_limiting:
    requests_per_minute: 1000
  tracing:
    enabled: true
    sample_rate: 0.1
  new_checkout_flow:
    enabled: true
    percentage: 10
```

### Building with Feature Flags

```sh
graft merge \
  config/base.yml \
  config/features/defaults.yml \
  config/features/production.yml \
  config/environments/production.yml \
  config/secrets/production.yml
```

## Multi-Region Configuration

**config/regions.yml:**

```yaml
regions:
  us-east-1:
    name: US East (N. Virginia)
    database_endpoint: db.us-east-1.prod.internal
    cache_endpoint: redis.us-east-1.prod.internal
    cdn_endpoint: cdn-us-east.example.com
  eu-west-1:
    name: EU West (Ireland)
    database_endpoint: db.eu-west-1.prod.internal
    cache_endpoint: redis.eu-west-1.prod.internal
    cdn_endpoint: cdn-eu-west.example.com
  ap-southeast-1:
    name: Asia Pacific (Singapore)
    database_endpoint: db.ap-southeast-1.prod.internal
    cache_endpoint: redis.ap-southeast-1.prod.internal
    cdn_endpoint: cdn-ap-southeast.example.com

current_region: (( grab $AWS_REGION || "us-east-1" ))

regional_config: (( grab (concat "regions." current_region) ))

database:
  host: (( grab regional_config.database_endpoint ))

cache:
  host: (( grab regional_config.cache_endpoint ))

cdn:
  endpoint: (( grab regional_config.cdn_endpoint ))
```

### Generate All Regional Configurations

```sh
for region in us-east-1 eu-west-1 ap-southeast-1; do
  AWS_REGION=$region graft merge \
    config/base.yml \
    config/regions.yml \
    config/environments/production.yml \
    config/secrets/production.yml \
    > generated/production-${region}-config.yml
done
```

## Kubernetes ConfigMap Generation

**k8s/configmap-template.yml:**

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: (( concat meta.app_name "-config" ))
  namespace: (( grab meta.environment ))
  labels:
    app: (( grab meta.app_name ))
    environment: (( grab meta.environment ))
data:
  config.yml: |
    (( stringify (prune _internal) ))
```

### Generate ConfigMap

```sh
# Merge configuration
graft merge config/base.yml config/environments/production.yml > /tmp/config.yml

# Generate ConfigMap
graft merge k8s/configmap-template.yml /tmp/config.yml > k8s/production-configmap.yml
```

## Comparing Environments

Use `graft diff` to compare configurations:

### Compare Dev vs Production

```sh
graft merge config/base.yml config/environments/development.yml > /tmp/dev.yml
graft merge config/base.yml config/environments/production.yml > /tmp/prod.yml

graft diff --side-by-side /tmp/dev.yml /tmp/prod.yml
```

### View Specific Differences

```sh
graft diff --changes /tmp/dev.yml /tmp/prod.yml
```

**Output:**

```
Changes (8 modified, 2 added, 0 removed):

  MODIFIED  database.host
            - localhost
            + db.prod.internal

  MODIFIED  database.pool.max_connections
            - 10
            + 100

  MODIFIED  logging.level
            - debug
            + warn

  ADDED     production.replicas
            + 5

  ADDED     production.autoscaling
            + max: 10
            + min: 3
            + target_cpu: 70
```

(Illustrative counts/paths; each changed value is rendered as YAML, not
JSON — a multi-key added value like `autoscaling` prints one `+` line per
key, not a single compact object line.)

## Validation Pattern

Create a validation configuration to ensure required values:

**config/validation.yml:**

```yaml
database:
  host: (( param "database.host is required" ))
  port: (( param "database.port is required" ))
  user: (( param "database.user is required" ))
  password: (( param "database.password is required" ))

cache:
  host: (( param "cache.host is required" ))
```

### Validate Before Deploy

```sh
# This will fail if any required param is missing
graft merge \
  config/validation.yml \
  config/base.yml \
  config/environments/production.yml \
  config/secrets/production.yml \
  > /dev/null && echo "Validation passed"
```

## Complete Example: Full Stack Configuration

**config/full-stack.yml:**

```yaml
meta:
  app_name: my-fullstack-app
  version: (( grab $APP_VERSION || "1.0.0" ))
  environment: (( grab $ENVIRONMENT || "development" ))

# Backend services
backend:
  api:
    host: (( concat meta.app_name "-api" ))
    port: 8080
    replicas: '(( meta.environment == "production" ? 3 : 1 ))'

  worker:
    host: (( concat meta.app_name "-worker" ))
    port: 8081
    replicas: '(( meta.environment == "production" ? 5 : 1 ))'
    queues:
      - default
      - high
      - low

# Data stores
datastores:
  postgres:
    host: (( grab database.host ))
    port: 5432
    database: (( concat meta.app_name "_" meta.environment ))

  redis:
    host: (( grab cache.host ))
    port: 6379
    db: 0

  elasticsearch:
    host: (( grab $ES_HOST || "localhost" ))
    port: 9200

# External services
external:
  stripe:
    api_key: (( grab api_keys.stripe ))
    webhook_secret: (( vault (concat "secret/" meta.environment "/stripe:webhook_secret") || "" ))

  sendgrid:
    api_key: (( grab api_keys.sendgrid ))
    from_email: (( concat "noreply@" meta.app_name ".com" ))

# Monitoring
monitoring:
  prometheus:
    enabled: (( grab features.metrics ))
    port: 9090
    path: /metrics

  jaeger:
    enabled: (( grab features.tracing ))
    endpoint: (( grab $JAEGER_ENDPOINT || "http://localhost:14268/api/traces" ))

# Security
security:
  jwt:
    secret: (( grab jwt.secret ))
    expiry: 3600
    refresh_expiry: 86400

  cors:
    allowed_origins:
      (( if meta.environment == "production" ))
      - https://app.example.com
      - https://www.example.com
      (( else ))
      - http://localhost:3000
      - http://localhost:8080
      (( fi ))
```

### Build Full Stack Config

```sh
ENVIRONMENT=production APP_VERSION=2.5.0 graft merge \
  config/base.yml \
  config/full-stack.yml \
  config/environments/production.yml \
  config/secrets/production.yml \
  --prune meta \
  > generated/full-stack-production.yml
```

## See Also

- [Basic Merging](basic-merging.md) - Merge fundamentals
- [Conditional Configurations](conditional-configs.md) - Control flow operators
- [Secrets Management](secrets-management.md) - External secrets integration
- [CI/CD Integration](ci-cd-integration.md) - Pipeline integration

# Array Merging

Graft provides powerful control over how arrays are merged between documents.

## Default Behavior

By default, arrays are merged using **inline** semantics (by index):

```yaml
# base.yml
items:
  - name: first
    value: 1
  - name: second
    value: 2

# overlay.yml
items:
  - value: 10  # Merges with index 0

# Result
items:
  - name: first
    value: 10   # value overwritten
  - name: second
    value: 2    # unchanged
```

## Array Merge Operators

Use these operators as the **first element** of an array to control merge behavior:

| Operator | Description |
|----------|-------------|
| `(( append ))` | Add elements to end |
| `(( prepend ))` | Add elements to beginning |
| `(( replace ))` | Replace entire array |
| `(( inline ))` | Merge by index (default) |
| `(( merge ))` | Merge by key field |
| `(( insert ))` | Insert at position |
| `(( delete ))` | Remove elements |

## append

Add new elements to the end of an existing array.

```yaml
# base.yml
packages:
  - nginx
  - postgresql

# overlay.yml
packages:
  - (( append ))
  - redis
  - memcached

# Result
packages:
  - nginx
  - postgresql
  - redis
  - memcached
```

### Append with References

```yaml
common_packages:
  - curl
  - wget

server_packages:
  - (( append ))
  - nginx

all_packages:
  - (( append with common_packages ))
  - (( append with server_packages ))
  - custom-tool
```

## prepend

Add elements to the beginning of an existing array.

```yaml
# base.yml
steps:
  - build
  - test
  - deploy

# overlay.yml
steps:
  - (( prepend ))
  - setup
  - validate

# Result
steps:
  - setup
  - validate
  - build
  - test
  - deploy
```

## replace

Replace the entire array.

```yaml
# base.yml
environments:
  - dev
  - staging
  - production

# overlay.yml
environments:
  - (( replace ))
  - prod-us
  - prod-eu

# Result
environments:
  - prod-us
  - prod-eu
```

## inline

Merge arrays by index position (default behavior).

```yaml
# base.yml
servers:
  - name: web1
    port: 80
    ssl: false
  - name: web2
    port: 80
    ssl: false

# overlay.yml
servers:
  - (( inline ))
  - ssl: true    # Merges with index 0
  - ssl: true    # Merges with index 1

# Result
servers:
  - name: web1
    port: 80
    ssl: true
  - name: web2
    port: 80
    ssl: true
```

## merge

Merge arrays by matching a key field. This is powerful for managing lists of objects.

### Basic Merge

```yaml
# base.yml
services:
  - name: api
    port: 8080
    replicas: 1
  - name: web
    port: 80
    replicas: 1
  - name: worker
    port: 9090
    replicas: 1

# overlay.yml
services:
  - (( merge on name ))
  - name: api
    replicas: 5    # Update existing
  - name: cache    # Add new
    port: 6379
    replicas: 3

# Result
services:
  - name: api
    port: 8080
    replicas: 5      # Updated
  - name: web
    port: 80
    replicas: 1      # Unchanged
  - name: worker
    port: 9090
    replicas: 1      # Unchanged
  - name: cache      # Added
    port: 6379
    replicas: 3
```

### Merge on Different Keys

```yaml
# Merge on 'id' instead of 'name'
users:
  - (( merge on id ))
  - id: 1
    role: admin    # Updates user with id: 1
```

### Merge with Nested Objects

```yaml
# base.yml
deployments:
  - name: api
    containers:
      - name: app
        image: api:v1
      - name: sidecar
        image: proxy:v1

# overlay.yml
deployments:
  - (( merge on name ))
  - name: api
    containers:
      - (( merge on name ))
      - name: app
        image: api:v2  # Update container image
```

## insert

Insert elements at a specific position.

### Insert After

```yaml
# base.yml
pipeline:
  - checkout
  - build
  - deploy

# overlay.yml
pipeline:
  - (( insert after 1 ))
  - test
  - lint

# Result (inserted after index 1 = "build")
pipeline:
  - checkout
  - build
  - test      # Inserted
  - lint      # Inserted
  - deploy
```

### Insert Before

```yaml
# base.yml
steps:
  - build
  - test
  - deploy

# overlay.yml
steps:
  - (( insert before 2 ))
  - validate

# Result (inserted before index 2 = "deploy")
steps:
  - build
  - test
  - validate  # Inserted
  - deploy
```

## delete

Remove elements from an array.

### Delete by Index

```yaml
# base.yml
items:
  - keep1
  - remove
  - keep2
  - keep3

# overlay.yml
items:
  - (( delete 1 ))  # Remove index 1

# Result
items:
  - keep1
  - keep2
  - keep3
```

### Delete by Name

The string form names the value of the entry's `name` key, not a `key=value`
pair. Every entry in the list must have a `name` key.

```yaml
# base.yml
services:
  - name: api
    port: 8080
  - name: deprecated
    port: 9999
  - name: web
    port: 80

# overlay.yml
services:
  - (( delete "deprecated" ))

# Result
services:
  - name: api
    port: 8080
  - name: web
    port: 80
```

Writing `(( delete "name=deprecated" ))` looks for an entry whose `name` is the
literal string `name=deprecated`, and fails:

```
 - $.services: unable to find specified modification point with 'name: name=deprecated'
```

A list whose entries have no `name` key cannot be deleted from by string:

```
 - $.items.0: original object does not contain the key 'name' - cannot merge by key
```

## Combining Operators

### Append and Merge

```yaml
# base.yml
rules:
  - name: allow-health
    path: /health
    auth: false
  - name: allow-metrics
    path: /metrics
    auth: false

# overlay.yml
rules:
  - (( merge on name ))
  - (( append ))
  - name: allow-health
    rate_limit: 1000  # Update existing
  - name: api-gateway  # Add new
    path: /api/*
    auth: true
```

### Prepend with Merge

```yaml
# Ensure critical rules come first
rules:
  - (( merge on name ))
  - (( prepend ))
  - name: security-check
    priority: 0
```

## Practical Examples

### Kubernetes Containers

```yaml
# base.yml
spec:
  containers:
    - name: app
      image: myapp:latest
      ports:
        - containerPort: 8080

# Add sidecar containers
# overlay.yml
spec:
  containers:
    - (( append ))
    - name: istio-proxy
      image: istio/proxy:latest
    - name: log-collector
      image: fluentd:latest
```

### Environment Variables

```yaml
# base.yml
env:
  - name: APP_NAME
    value: my-app
  - name: LOG_LEVEL
    value: info

# overlay.yml
env:
  - (( merge on name ))
  - name: LOG_LEVEL
    value: debug  # Override
  - name: DEBUG
    value: "true"  # Add
```

### Pipeline Steps

```yaml
# base.yml
pipeline:
  - name: build
    script: make build
  - name: deploy
    script: make deploy

# Insert testing between build and deploy
# overlay.yml
pipeline:
  - (( insert after 0 ))
  - name: unit-test
    script: make test
  - name: integration-test
    script: make integration
```

### Remove Deprecated Items

```yaml
# base.yml
features:
  - name: feature-a
    enabled: true
  - name: deprecated-feature
    enabled: true
  - name: feature-b
    enabled: true

# overlay.yml
features:
  - (( delete "deprecated-feature" ))
```

## Fallback Append Mode

Use `--fallback-append` to change the default behavior:

```sh
graft merge --fallback-append base.yml overlay.yml
```

With this flag, arrays without explicit operators use append instead of inline.

## Summary Table

| Operator | Syntax | Effect |
|----------|--------|--------|
| append | `(( append ))` | Add to end |
| prepend | `(( prepend ))` | Add to beginning |
| replace | `(( replace ))` | Replace entire array |
| inline | `(( inline ))` | Merge by index (default) |
| merge | `(( merge on key ))` | Merge by key field |
| insert after | `(( insert after N ))` | Insert after index N |
| insert before | `(( insert before N ))` | Insert before index N |
| delete | `(( delete N ))` | Remove index N |
| delete | `(( delete "name-value" ))` | Remove the entry with that `name` |

## See Also

- [Array Operations](operators/array-operations.md) - Array transformation operators
- [merge Command](cli/merge.md) - Merge command options
- [Examples: Multi-Environment](../examples/multi-environment.md) - Practical patterns

# Array Operations

Operators for manipulating arrays during merge and evaluation.

## Array Merge Operators

These operators control how arrays are merged between documents.

### append

Add elements to the end of an existing array.

```yaml
# base.yml
packages:
  - git
  - vim

# overlay.yml
packages:
  - (( append ))
  - nginx
  - postgresql
```

**Result:**
```yaml
packages:
  - git
  - vim
  - nginx
  - postgresql
```

### prepend

Add elements to the beginning of an existing array.

```yaml
# base.yml
packages:
  - git
  - vim

# overlay.yml
packages:
  - (( prepend ))
  - required-first
  - also-first
```

**Result:**
```yaml
packages:
  - required-first
  - also-first
  - git
  - vim
```

### replace

Replace the entire array.

```yaml
# base.yml
packages:
  - git
  - vim
  - emacs

# overlay.yml
packages:
  - (( replace ))
  - only-this
  - and-this
```

**Result:**
```yaml
packages:
  - only-this
  - and-this
```

### inline

Merge arrays by index position (default behavior).

```yaml
# base.yml
servers:
  - name: server1
    port: 8080
  - name: server2
    port: 8081

# overlay.yml
servers:
  - (( inline ))
  - port: 9080
  - port: 9081
```

**Result:**
```yaml
servers:
  - name: server1
    port: 9080
  - name: server2
    port: 9081
```

### merge

Merge arrays by matching a key field.

```yaml
# base.yml
services:
  - name: api
    port: 8080
    replicas: 1
  - name: web
    port: 80
    replicas: 1

# overlay.yml
services:
  - (( merge on name ))
  - name: api
    replicas: 3
  - name: worker
    port: 9090
    replicas: 2
```

**Result:**
```yaml
services:
  - name: api
    port: 8080
    replicas: 3      # merged
  - name: web
    port: 80
    replicas: 1      # unchanged
  - name: worker     # added
    port: 9090
    replicas: 2
```

### insert

Insert elements at a specific position.

```yaml
# base.yml
steps:
  - name: step1
  - name: step2
  - name: step3

# overlay.yml - insert after index 1
steps:
  - (( insert after 1 ))
  - name: step1.5
```

**Result:**
```yaml
steps:
  - name: step1
  - name: step2
  - name: step1.5    # inserted
  - name: step3
```

**Syntax variations:**

- `(( insert after N ))` - Insert after index N
- `(( insert before N ))` - Insert before index N

### delete

Remove elements from an array.

```yaml
# base.yml
packages:
  - git
  - vim
  - emacs
  - nano

# overlay.yml - delete by index
packages:
  - (( delete 2 ))   # removes "emacs"

# Or delete by key match
services:
  - (( delete "name=worker" ))
```

## Array Transformation Operators

### flatten

Flatten nested arrays into a single array.

```yaml
nested:
  - [1, 2]
  - [3, 4]
  - [[5, 6], 7]

flat: (( flatten nested ))
```

**Result:**
```yaml
flat:
  - 1
  - 2
  - 3
  - 4
  - 5
  - 6
  - 7
```

### uniq

Remove duplicate elements from an array.

```yaml
with_dupes:
  - apple
  - banana
  - apple
  - cherry
  - banana

unique: (( uniq with_dupes ))
```

**Result:**
```yaml
unique:
  - apple
  - banana
  - cherry
```

### sort

Sort array elements.

**Simple sort:**
```yaml
numbers:
  - 3
  - 1
  - 4
  - 1
  - 5

sorted: (( sort numbers ))
```

**Result:**
```yaml
sorted:
  - 1
  - 1
  - 3
  - 4
  - 5
```

**Sort by key:**
```yaml
users:
  - name: Charlie
    age: 30
  - name: Alice
    age: 25
  - name: Bob
    age: 35

by_name: (( sort users by name ))
by_age: (( sort users by age ))
```

**Result:**
```yaml
by_name:
  - name: Alice
    age: 25
  - name: Bob
    age: 35
  - name: Charlie
    age: 30

by_age:
  - name: Alice
    age: 25
  - name: Charlie
    age: 30
  - name: Bob
    age: 35
```

### shuffle

Randomize array order.

```yaml
items:
  - a
  - b
  - c
  - d

randomized: (( shuffle items ))
```

**Result:** (random order)
```yaml
randomized:
  - c
  - a
  - d
  - b
```

### cartesian-product

Generate all combinations of elements from multiple arrays.

```yaml
colors:
  - red
  - blue
sizes:
  - small
  - large

combinations: (( cartesian-product colors sizes ))
```

**Result:**
```yaml
combinations:
  - [red, small]
  - [red, large]
  - [blue, small]
  - [blue, large]
```

## Combining with Control Flow

### Filter with for/if

```yaml
services:
  - name: api
    enabled: true
  - name: worker
    enabled: false
  - name: web
    enabled: true

enabled_services:
(( for svc in grab services ))
  (( if grab svc.enabled ))
  - (( grab svc.name ))
  (( fi ))
(( done ))
```

**Result:**
```yaml
enabled_services:
  - api
  - web
```

### Transform with for

```yaml
ports:
  - 8080
  - 8081
  - 8082

services:
(( for port in grab ports ))
  - name: (( concat "service-" port ))
    port: (( grab port ))
(( done ))
```

## Practical Examples

### Merge Package Lists

```yaml
# common.yml
packages:
  - curl
  - wget
  - git

# web-server.yml
packages:
  - (( append ))
  - nginx
  - certbot
```

### Environment Overrides

```yaml
# base.yml
env_vars:
  - name: APP_NAME
    value: my-app
  - name: LOG_LEVEL
    value: info

# production.yml
env_vars:
  - (( merge on name ))
  - name: LOG_LEVEL
    value: warn
  - name: METRICS_ENABLED
    value: "true"
```

### Kubernetes Containers

```yaml
# base.yml
containers:
  - name: app
    image: myapp:latest
    ports:
      - containerPort: 8080

# with-sidecar.yml
containers:
  - (( append ))
  - name: sidecar
    image: proxy:latest
    ports:
      - containerPort: 9090
```

### Unique Values

```yaml
all_hosts:
  - server1.example.com
  - server2.example.com
  - server1.example.com  # duplicate

hosts: (( uniq all_hosts ))
# Result: [server1.example.com, server2.example.com]
```

### Sorted Configuration

```yaml
routes:
  - path: /api
    priority: 10
  - path: /health
    priority: 100
  - path: /
    priority: 1

ordered_routes: (( sort routes by priority ))
# Sorted by priority ascending
```

## Array Operator Summary

| Operator | Position | Description |
|----------|----------|-------------|
| `append` | First element | Add to end |
| `prepend` | First element | Add to beginning |
| `replace` | First element | Replace entire array |
| `inline` | First element | Merge by index |
| `merge` | First element | Merge by key |
| `insert` | First element | Insert at position |
| `delete` | First element | Remove elements |
| `flatten` | Expression | Flatten nested |
| `uniq` | Expression | Remove duplicates |
| `sort` | Expression | Sort elements |
| `shuffle` | Expression | Randomize order |
| `cartesian-product` | Expression | Cross product |

## See Also

- [Array Merging Guide](../array-merging.md) - Deep dive into array merge strategies
- [Operators Overview](index.md) - All operators
- [Control Flow](control-flow.md) - for loops for array processing

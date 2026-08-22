# Array Operations

Operators for manipulating arrays during merge and evaluation.

Two different things live in this page, and they are not interchangeable:

- **Array merge markers** — `append`, `prepend`, `replace`, `inline`, `merge`,
  `insert`, `delete`, and `sort`. These are handled by the merger, so they only
  do anything when a *later* document overlays an *earlier* one. Writing them
  in a single file that has nothing to overlay is an error.

- **Array expressions** — `flatten`, `uniq`, `shuffle`, and
  `cartesian-product`. These are ordinary operators: they take arguments,
  return a value, and work inside a single file.

Every output block below is what `graft merge` actually prints: the whole
merged document, map keys in graft's stable sorted order, and list items at the
same indentation as their parent key. Where an example would otherwise be buried
in scaffolding, the `--prune` flag that removed it is shown with the command.

## Array Merge Operators

These operators control how arrays are merged between documents.

### append

Add elements to the end of an existing array.

```yaml
# base.yml
packages:
  - git
  - vim
```

```yaml
# overlay.yml
packages:
  - (( append ))
  - nginx
  - postgresql
```

**Output** (`graft merge base.yml overlay.yml`):

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
```

```yaml
# overlay.yml
packages:
  - (( prepend ))
  - required-first
  - also-first
```

**Output** (`graft merge base.yml overlay.yml`):

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
```

```yaml
# overlay.yml
packages:
  - (( replace ))
  - only-this
  - and-this
```

**Output** (`graft merge base.yml overlay.yml`):

```yaml
packages:
- only-this
- and-this
```

### inline

Merge arrays by index position.

```yaml
# base.yml
servers:
  - name: server1
    port: 8080
  - name: server2
    port: 8081
```

```yaml
# overlay.yml
servers:
  - (( inline ))
  - port: 9080
  - port: 9081
```

**Output** (`graft merge base.yml overlay.yml`):

```yaml
servers:
- name: server1
  port: 9080
- name: server2
  port: 9081
```

### merge

Merge arrays by matching a key field. `(( merge ))` matches on `name`;
`(( merge on <key> ))` matches on the key you name.

```yaml
# base.yml
services:
  - name: api
    port: 8080
    replicas: 1
  - name: web
    port: 80
    replicas: 1
```

```yaml
# overlay.yml
services:
  - (( merge on name ))
  - name: api
    replicas: 3
  - name: worker
    port: 9090
    replicas: 2
```

**Output** (`graft merge base.yml overlay.yml`):

```yaml
services:
- name: api
  port: 8080
  replicas: 3
- name: web
  port: 80
  replicas: 1
- name: worker
  port: 9090
  replicas: 2
```

`api` picks up the new `replicas` and keeps its `port`, `web` is untouched
because the overlay never mentions it, and `worker` is appended because no
entry matched.

### insert

Insert elements at a specific position.

```yaml
# base.yml
steps:
  - name: step1
  - name: step2
  - name: step3
```

```yaml
# overlay.yml
steps:
  - (( insert after 1 ))
  - name: step1.5
```

**Output** (`graft merge base.yml overlay.yml`):

```yaml
steps:
- name: step1
- name: step2
- name: step1.5
- name: step3
```

**Syntax variations:**

- `(( insert after N ))` / `(( insert before N ))` — position by index

- `(( insert after "<value>" ))` / `(( insert before "<value>" ))` — position
  next to the entry whose `name` is `<value>` (list of maps), or next to the
  entry equal to `<value>` (list of scalars). Matching is by string
  comparison, so non-string scalars such as numbers are never matched; the
  first match wins when the value appears more than once; a missing anchor
  fails the merge

- `(( insert after <key> "<value>" ))` — position next to the entry whose
  `<key>` is `<value>`; only valid on a list of maps

### delete

Remove elements from an array.

```yaml
# base.yml
packages:
  - git
  - vim
  - emacs
  - nano
services:
  - name: api
  - name: worker
```

```yaml
# overlay.yml
packages:
  - (( delete 2 ))
services:
  - (( delete "worker" ))
```

**Output** (`graft merge base.yml overlay.yml`):

```yaml
packages:
- git
- vim
- nano
services:
- name: api
```

**Syntax variations:**

- `(( delete N ))` — remove index N

- `(( delete "<value>" ))` — remove the entry whose `name` is `<value>`

- `(( delete <key> "<value>" ))` — remove the entry whose `<key>` is `<value>`

Nothing may follow a delete-by-name marker in the same overlay array except
another array operator:

```
1 error(s) detected:
 - $.services: item in array directly after (( delete name "worker" )) must be one of the array operators 'append', 'prepend', 'delete', or 'insert'
```

### sort

Sort a list that an earlier document already defined. `(( sort ))` replaces the
list in the overlay; the merger keeps the earlier list and queues it for
sorting.

```yaml
# base.yml
numbers:
  - 3
  - 1
  - 4
  - 1
  - 5
```

```yaml
# sorted.yml
numbers: (( sort ))
```

**Output** (`graft merge base.yml sorted.yml`):

```yaml
numbers:
- 1
- 1
- 3
- 4
- 5
```

For a list of maps, the sort key defaults to `name`; `sort by <key>` chooses a
different one.

```yaml
# users.yml
users:
  - name: Charlie
    age: 30
  - name: Alice
    age: 25
  - name: Bob
    age: 35
```

```yaml
# by-age.yml
users: (( sort by age ))
```

**Output** (`graft merge users.yml by-age.yml`):

```yaml
users:
- age: 25
  name: Alice
- age: 30
  name: Charlie
- age: 35
  name: Bob
```

`sort` needs a list to attach to. Written where no earlier document supplies
one — including in the file that first defines the list — it fails:

```
1 error(s) detected:
 - $.numbers: orphaned (( sort )) operator at $.numbers, no list exists at that path
```

The list must also be homogeneous. Mixed element types, or entries that are
themselves lists, are rejected; so is a list of maps where some entry lacks the
sort key.

## Array Transformation Operators

### flatten

Flatten a nested list into a single flat list.

`flatten` takes exactly one argument, and that argument must be a list. It
recurses to every depth — there is no depth argument and no way to flatten only
one level.

```yaml
# flat.yml
nested:
  - [1, 2]
  - [3, 4]
  - [[5, 6], 7]

flat: (( flatten nested ))
```

**Output** (`graft merge flat.yml --prune nested`):

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

The `[[5, 6], 7]` entry is flattened along with the rest: `5` and `6` come out
at the same level as `1` and `7`.

**Errors:**

- `flatten operator requires exactly one argument, got <n>`

- `flatten operator requires a list argument, got <type>`

### uniq

Remove duplicate elements from a list.

`uniq` takes exactly one argument, and that argument must be a list. It keeps
the **first** occurrence of each value and preserves the input order. It never
sorts.

```yaml
# uniq.yml
with_dupes:
  - zebra
  - apple
  - zebra
  - mango
  - apple

unique: (( uniq with_dupes ))
```

**Output** (`graft merge uniq.yml --prune with_dupes`):

```yaml
unique:
- zebra
- apple
- mango
```

`zebra` stays first because it appeared first. If you want alphabetical order,
sort separately.

Comparison is by value and type, so `1` and `"1"` are two different elements
and both survive.

**Errors:**

- `uniq operator requires exactly one argument, got <n>`

- `uniq operator requires a list argument, got <type>`

### shuffle

Randomize list order.

```yaml
# shuffle.yml
items:
  - a
  - b
  - c
  - d

randomized: (( shuffle items ))
```

**Output** (`graft merge shuffle.yml --prune items`), one run of many:

```yaml
randomized:
- b
- d
- c
- a
```

The order is drawn from `crypto/rand`, so consecutive runs differ. Arguments
must resolve to lists or scalars; a map argument errors with `shuffle only
accepts arrays and scalar values`.

### cartesian-product

Combine every element of each list with every element of the others. Each
combination is emitted as a single **concatenated string**, not as a tuple.

```yaml
# cart.yml
colors:
  - red
  - blue
sizes:
  - small
  - large

combinations: (( cartesian-product colors sizes ))
```

**Output** (`graft merge cart.yml --prune colors --prune sizes`):

```yaml
combinations:
- redsmall
- redlarge
- bluesmall
- bluelarge
```

Every list must contain only scalars — a nested list or a map errors with
`cartesian-product operator can only operate on lists of scalar values`. A
scalar argument is treated as a one-element list. `cartesian` is a registered
alias for the same operator.

## Combining with Control Flow

### Filter with for/if

```yaml
# filter.yml
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

**Output** (`graft merge filter.yml --prune services`):

```yaml
enabled_services:
- api
- web
```

### Transform with for

```yaml
# transform.yml
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

**Output** (`graft merge transform.yml`):

```yaml
ports:
- 8080
- 8081
- 8082
services:
- name: service-8080
  port: 8080
- name: service-8081
  port: 8081
- name: service-8082
  port: 8082
```

## Practical Examples

### Merge Package Lists

```yaml
# common.yml
packages:
  - curl
  - wget
  - git
```

```yaml
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
```

```yaml
# production.yml
env_vars:
  - (( merge on name ))
  - name: LOG_LEVEL
    value: warn
  - name: METRICS_ENABLED
    value: "true"
```

**Output** (`graft merge base.yml production.yml`):

```yaml
env_vars:
- name: APP_NAME
  value: my-app
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
```

```yaml
# with-sidecar.yml
containers:
  - (( append ))
  - name: sidecar
    image: proxy:latest
    ports:
      - containerPort: 9090
```

**Output** (`graft merge base.yml with-sidecar.yml`):

```yaml
containers:
- image: myapp:latest
  name: app
  ports:
  - containerPort: 8080
- image: proxy:latest
  name: sidecar
  ports:
  - containerPort: 9090
```

### Unique Values

```yaml
# hosts.yml
all_hosts:
  - server1.example.com
  - server2.example.com
  - server1.example.com

hosts: (( uniq all_hosts ))
```

**Output** (`graft merge hosts.yml --prune all_hosts`):

```yaml
hosts:
- server1.example.com
- server2.example.com
```

### Sorted Configuration

```yaml
# routes.yml
routes:
  - path: /api
    priority: 10
  - path: /health
    priority: 100
  - path: /
    priority: 1
```

```yaml
# order.yml
routes: (( sort by priority ))
```

**Output** (`graft merge routes.yml order.yml`):

```yaml
routes:
- path: /
  priority: 1
- path: /api
  priority: 10
- path: /health
  priority: 100
```

## Array Operator Summary

| Operator | Position | Description |
|----------|----------|-------------|
| `append` | First element of an overlay array | Add to end |
| `prepend` | First element of an overlay array | Add to beginning |
| `replace` | First element of an overlay array | Replace entire array |
| `inline` | First element of an overlay array | Merge by index |
| `merge` | First element of an overlay array | Merge by key, default `name` |
| `insert` | First element of an overlay array | Insert at index or next to a named entry |
| `delete` | First element of an overlay array | Remove by index or by key match |
| `sort` | Overlay value replacing a list | Sort the earlier document's list |
| `flatten` | Expression | Flatten one list, recursively, at every depth |
| `uniq` | Expression | Drop duplicates from one list, keeping first occurrence and input order |
| `shuffle` | Expression | Randomize order |
| `cartesian-product` | Expression | Every combination, concatenated into strings |

## See Also

- [Array Merging Guide](../array-merging.md) - Deep dive into array merge strategies

- [Operators Overview](index.md) - All operators

- [Control Flow](control-flow.md) - for loops for array processing

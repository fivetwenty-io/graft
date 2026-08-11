# Glossary

Terms and definitions used throughout the Graft documentation.

## A

### AST (Abstract Syntax Tree)

A tree representation of parsed expressions. Graft parses operator expressions like `(( concat "a" "b" ))` into an AST for evaluation.

### Append

An array merge strategy that adds elements to the end of an existing array rather than replacing it.

```yaml
items:
  - (( append ))
  - new_item
```

## B

### Backend

An external service that provides secrets or configuration data. Graft supports Vault, AWS Parameter Store, AWS Secrets Manager, and NATS.

### Base Document

The first document in a merge operation. Subsequent documents (overlays) are merged on top of the base.

### Batch

Not a term graft's own architecture uses today: graft does not aggregate requests for different backend paths into fewer calls. What it does do is deduplicate - coalesce concurrent references to the identical path (same target, same secret/parameter/KV path) into one request instead of one per reference - for Vault, AWS Parameter Store, AWS Secrets Manager, and NATS lookups. See [Parallel Execution Model](../architecture/parallelism.md#level-3-backend-request-dedup).

## C

### Cherry-Pick

A merge option that includes only specified keys in the output.

```bash
graft merge --cherry-pick database --cherry-pick server base.yml overlay.yml
```

### Control Flow

The whole-line keywords `if/elif/else/fi`, `for/done`, `while/done`, and `case/when/default/esac`. They are not operators: each occupies a line of its own rather than a value position, and its body is raw YAML rather than an expression. Graft expands them into plain YAML before the document is parsed. Blocks may nest up to 64 levels deep.

```yaml
(( if environment == "production" ))
replicas: 5
(( else ))
replicas: 1
(( fi ))
```

### Cursor

An internal path reference to a location in the document tree, used during evaluation.

## D

### Defer

An operator that delays evaluation, useful for template generation.

```yaml
template: (( defer grab runtime.value ))
```

### Diff

A comparison between two documents showing additions, removals, and modifications.

### Document

A parsed YAML/JSON data structure. The `Document` interface provides type-safe accessors and mutation methods.

## E

### Engine

The main entry point for all Graft operations. The `Engine` interface provides parsing, merging, diffing, and evaluation methods.

### Evaluator

The component that executes operators in dependency order, resolving references and calling external backends.

### Expansion

The source-to-source rewrite that turns control-flow blocks into plain YAML. It runs on each input file's raw text, before YAML parsing and before any merge, which is why a loop cannot iterate over data that only another merged file defines.

### Expression

A parsed operator or value within `(( ... ))` markers. Expressions nest and combine: an operator call may appear as an argument to another, and infix arithmetic, comparison, boolean, and ternary forms are all evaluated directly.

```yaml
url: (( concat "https://" (grab host) ":" (grab port) ))
total: (( base + tax * quantity ))
```

## F

### Fallback

A default value used when an operator fails or returns empty.

```yaml
host: (( vault "secret/db:host" || "localhost" ))
```

### Flatten

An operator that splices nested lists into a single flat list, at every depth. It takes exactly one list argument; there is no depth argument.

```yaml
flat: (( flatten nested ))
```

### Functional Options

A Go pattern used for configuration. Options are functions that modify internal configuration.

```go
engine, _ := graft.NewEngine(
    graft.WithCache(true, 1000),
    graft.WithConcurrency(4),
)
```

## G

### Grab

The primary reference operator. `grab` retrieves values from elsewhere in the document. A bare reference in operand position resolves the same way, so `grab` is optional inside an expression.

```yaml
url: (( grab database.host ))
is_prod: (( environment == "production" ))
```

### GraftError

The base error type in Graft. All errors include code, message, position, path, and cause.

## I

### Inject

An operator that merges map contents at the parent level.

```yaml
settings:
  <<<: (( inject common_settings ))
  custom: value
```

The `<<<:` key holds the call and is removed from the output once the merge
runs. A bare `(( inject ... ))` with no key is a YAML parse error.

### Inline

An array merge strategy that merges by index position.

## M

### Marker

A line whose trimmed content is exactly `(( <keyword> ... ))`, optionally followed by a YAML comment. Markers delimit control-flow blocks. Their own indentation is discarded; the indentation of the lines between them is kept verbatim, and decides where the body lands in the document.

### Merge

The process of combining multiple documents. Later documents override earlier ones for scalar values; maps are deeply merged.

### MergeBuilder

A fluent API for configuring merge operations, with options for prune, cherry-pick, array merge strategy, skipping evaluation, go-patch parsing, and fallback-append.

### Multi-Target

Support for multiple named backend configurations. Targets are selected by writing `@<target>` on the operator name.

```yaml
prod: (( vault@prod "secret/db:password" ))
staging: (( vault@staging "secret/db:password" ))
```

## O

### Opcall

Internal representation of an operator invocation with operator name, arguments, and position information.

### Operator

A function that transforms values during evaluation. Graft includes built-in operators and supports custom operators.

### Overlay

A document merged on top of a base document. Values in overlays override corresponding values in the base.

## P

### Param

An operator that marks a required parameter. Evaluation fails if the parameter is not provided.

```yaml
password: (( param "Password is required" ))
```

### Phase

When an operator runs relative to the rest of the document. **MergePhase** operators run while documents are combined; **ParamPhase** runs next, and an unresolved `param` aborts the run before evaluation starts; **EvalPhase** is the main pass, where the large majority of operators, including every external-backend lookup, execute.

### Pipeline

The processing stages for documents: control-flow expansion, YAML parsing, merging, operator evaluation, and post-processing.

### Position

Line and column location in source file, with a byte offset. Used for error messages.

### Post-Processor

A component that runs after merge and evaluation. Post-processors validate, transform, or analyze results.

### Prepend

An array merge strategy that adds elements to the beginning of an array.

### Predicate

A `field=value` selector used in place of a path segment, matching the entry of a list whose named field holds that value. Expressions accept the dotted and the bracketed spelling; `--cherry-pick` and `--prune` accept the dotted spelling only.

```yaml
primary_host: (( grab servers.name=primary.host ))
replica_host: (( grab servers[name=replica].host ))
```

```bash
graft merge --cherry-pick 'servers.name=primary' config.yml
```

### Pre-Scanner

A scanner in `pkg/graft/interfaces` that extracts `(( ... ))` locations and their raw contents from source text, handling nested parentheses and quoted strings. It is a standalone utility for tooling; the merge pipeline itself parses each expression when the evaluator reaches it.

### Prune

An operator or merge option that removes keys from output.

```yaml
internal: (( prune ))
```

```bash
graft merge --prune internal base.yml overlay.yml
```

## R

### Range

A generator used as a `for` loop's iterable: `range <start> <end> [step]`. The interval is **closed**, so `range 1 5` yields 1, 2, 3, 4, and 5. A step of zero, or one pointing away from the end bound, is an error.

```yaml
workers:
(( for i in range 1 5 ))
  - name: (( concat "worker-" i ))
(( done ))
```

### Reference

A path expression that retrieves a value from the document. References use dot notation — `path.to.value` — with numeric indexes or `field=value` predicates for list entries.

### Registry

The operator registry maintains all available operators and their metadata.

### Replace

An array merge strategy that completely replaces an array rather than merging elements.

## S

### Static IPs

A BOSH-specific operator for allocating static IP addresses from network pools.

### Stringify

An operator that converts any value to its YAML string representation.

## T

### Target

A named backend configuration, allowing connections to several instances of the same backend type. A target is written on the operator name with `@`, and configured through per-target environment variables — `VAULT_<TARGET>_ADDR` and `VAULT_<TARGET>_TOKEN`, `AWS_<TARGET>_REGION` and friends, `NATS_<TARGET>_URL`.

```yaml
password: (( vault@production "secret/db:password" ))
```

### Ternary

A conditional expression: `condition ? true_value : false_value`. Only the selected branch is evaluated. In YAML the whole expression must be quoted, because a plain scalar cannot contain the `: ` that separates the branches.

```yaml
size: '(( production ? "8Gi" : "2Gi" ))'
```

### Trace

Detailed logging of the merge process showing all operations, timing, and values.

### TTL (Time To Live)

Duration before cached values expire. Configurable for document cache and backend responses.

### Type

The `type` operator names its argument's type as a string — exactly one of `string`, `int`, `float`, `bool`, `array`, `map`, or `null`. It takes exactly one argument.

```yaml
kind: (( type database.port ))    # int
```

## U

### Uniq

An operator that removes duplicate elements from a list, keeping the first occurrence of each and preserving the input order. It never sorts, and compares by value and type, so `1` and `"1"` are distinct.

## V

### Vault

HashiCorp Vault or OpenBao secrets backend. The `vault` operator retrieves secrets.

```yaml
password: (( vault "secret/db:password" ))
```

## W

### Wave

A group of independent operators evaluated in parallel. Wave-based evaluation maximizes concurrency while respecting dependencies.

## See Also

- [Architecture Overview](../architecture/index.md) - System design

- [Operator Reference](operator-quick-reference.md) - Operator syntax

- [API Reference](../developer-guide/library-api/index.md) - Library interfaces

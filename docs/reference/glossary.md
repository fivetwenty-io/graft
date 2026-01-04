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

A group of external requests processed together. Graft batches multiple Vault or AWS calls to improve performance.

## C

### Cherry-Pick

A merge option that includes only specified keys in the output.

```bash
graft merge --cherry-pick database --cherry-pick server base.yml overlay.yml
```

### Control Flow

Operators that control execution flow: `if/elif/else/fi`, `for/done`, `while/done`, `case/when/esac`.

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

### Expression

A parsed operator or value within `(( ... ))` markers. Expressions can be nested and combined.

## F

### Fallback

A default value used when an operator fails or returns empty.

```yaml
host: (( vault "secret/db:host" || "localhost" ))
```

### Functional Options

A Go pattern used for configuration. Options are functions that modify internal configuration.

```go
engine, _ := graft.NewEngine(
    graft.WithCacheSize(1000),
    graft.WithHistoryTracking(true),
)
```

## G

### Grab

The primary reference operator. `grab` retrieves values from elsewhere in the document.

```yaml
url: (( grab database.host ))
```

### GraftError

The base error type in Graft. All errors include code, message, position, path, and cause.

## H

### History

A record of all changes during merge and evaluation. History tracks source files, line numbers, and intermediate values.

### History Entry

A single change record containing index, path, source, line, phase, operation, old value, new value, and timestamp.

### History Phase

The stage when a change occurred: Load, Merge, Eval, or PostProcess.

## I

### Inject

An operator that merges map contents at the parent level.

```yaml
settings:
  (( inject common_settings ))
  custom: value
```

### Inline

An array merge strategy that merges by index position.

## M

### Merge

The process of combining multiple documents. Later documents override earlier ones for scalar values; maps are deeply merged.

### MergeBuilder

A fluent API for configuring merge operations with options like prune, cherry-pick, and history tracking.

### Multi-Target

Support for multiple named backend configurations. Targets are selected with `target@path` syntax.

```yaml
prod: (( vault prod@"secret/db:password" ))
staging: (( vault staging@"secret/db:password" ))
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

An execution stage in the processing pipeline: Prescan, Parse, Merge, Evaluate, or PostProcess.

### Pipeline

The processing stages for documents: pre-scanning, YAML parsing, AST building, merging, evaluation, and post-processing.

### Position

Line and column location in source file. Used for error messages and history tracking.

### Post-Processor

A component that runs after merge and evaluation. Post-processors validate, transform, or analyze results.

### Prepend

An array merge strategy that adds elements to the beginning of an array.

### Pre-Scanner

The first pipeline stage that extracts `(( ... ))` operator locations before YAML parsing.

### Prune

An operator or merge option that removes keys from output.

```yaml
internal: (( prune ))
```

```bash
graft merge --prune internal base.yml overlay.yml
```

## R

### Reference

A path expression that retrieves a value from the document. References use dot notation: `path.to.value`.

### Registry

The operator registry maintains all available operators and their metadata.

### REPL

Read-Eval-Print Loop. Graft's interactive debugging interface accessed with `graft debug`.

### Replace

An array merge strategy that completely replaces an array rather than merging elements.

## S

### Static IPs

A BOSH-specific operator for allocating static IP addresses from network pools.

### Stringify

An operator that converts any value to its YAML string representation.

## T

### Target

A named backend configuration. Targets allow connecting to multiple instances of the same backend type.

### Ternary

A conditional expression: `condition ? true_value : false_value`.

```yaml
size: (( production ? "8Gi" : "2Gi" ))
```

### Trace

Detailed logging of the merge process showing all operations, timing, and values.

### TTL (Time To Live)

Duration before cached values expire. Configurable for document cache and backend responses.

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

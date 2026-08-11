# Reference Documentation

This section provides quick reference guides and comprehensive listings for Graft features.

## Quick Reference Guides

| Reference | Description |
|-----------|-------------|
| [Operator Quick Reference](operator-quick-reference.md) | All operators with syntax and examples |
| [CLI Quick Reference](cli-quick-reference.md) | All commands and flags |
| [Environment Variables](environment-variables.md) | Configuration environment variables |
| [Error Codes](error-codes.md) | Error types and troubleshooting |
| [Glossary](glossary.md) | Terms and definitions |

## Operators by Category

Graft registers 47 operators. The array-merge markers and the control-flow
keywords listed further down are handled elsewhere in the pipeline and are
not part of that count.

### Data Manipulation

| Operator | Description |
|----------|-------------|
| `grab` | Reference values from document |
| `concat` | Concatenate strings and values |
| `join` | Join array elements |
| `split` | Split string into array |
| `stringify` | Convert to YAML string |
| `base64` / `base64-decode` | Encode and decode base64 |
| `keys` | Extract and sort map keys |
| `type` | Name a value's type: `string`, `int`, `float`, `bool`, `array`, `map`, or `null` |
| `empty` | Construct an empty map/list, or test a value for emptiness |
| `null` | Return `nil`, or test whether a value is `nil` |
| `negate` | Boolean-negate a value |

### Arithmetic

| Operator | Description |
|----------|-------------|
| `+` `-` `*` `/` `%` | Basic arithmetic; `/` yields a float |
| `calc` | Expression evaluation, quoted or unquoted |

### Comparison & Logic

| Operator | Description |
|----------|-------------|
| `==` `!=` | Equality |
| `<` `>` `<=` `>=` | Comparison |
| `&&` `!` | Boolean logic |
| `\|\|` | Coalesce: the left value if it resolves non-`nil`, else the right |
| `? :` | Ternary conditional; quote the whole expression in YAML |

### Arrays

| Operator | Description |
|----------|-------------|
| `sort` | Sort elements, optionally `sort by <key>` |
| `shuffle` | Randomly reorder elements |
| `flatten` | Flatten nested arrays at every depth |
| `uniq` | Remove duplicates, keeping first occurrence and input order |
| `cartesian-product` / `cartesian` | Cross-product of several lists |

### Merge Structure

| Operator | Description |
|----------|-------------|
| `inject` | Deep-merge a map into the parent structure |
| `prune` | Mark the current path for removal from output |
| `param` | Mark a key as a required parameter |
| `defer` | Keep an expression as literal text for a later pass |

### External Sources

| Operator | Description |
|----------|-------------|
| `vault` / `vault-try` | HashiCorp Vault / OpenBao |
| `awsparam` | AWS Parameter Store |
| `awssecret` | AWS Secrets Manager |
| `nats` | NATS JetStream KV and Object stores |
| `file` | Read file contents |
| `load` | Load and parse YAML/JSON |

Any of these five backend operators accepts a named target written on the
operator name: `(( vault@production "secret/db:password" ))`.

### IP Arithmetic

| Operator | Description |
|----------|-------------|
| `ips` | Compute addresses from a CIDR block plus offsets |
| `static_ips` | BOSH-style static IP allocation inside a job block |

### Array-Merge Markers

Applied by the merger while documents are combined, not by an operator:

| Marker | Description |
|--------|-------------|
| `append` | Add to end of array |
| `prepend` | Add to beginning |
| `replace` | Replace entire array |
| `inline` | Merge by index |
| `merge` / `merge on <key>` | Merge by key |
| `delete` | Remove a matching entry |

### Control Flow

Whole-line keywords, expanded into plain YAML before parsing:

| Keyword | Description |
|---------|-------------|
| `if`/`elif`/`else`/`fi` | Conditional blocks |
| `for`/`done` | Iteration over collections, and over `range` |
| `while`/`done` | Conditional loops, bounded by an iteration cap |
| `case`/`when`/`default`/`esac` | Pattern matching on exact string equality |

## CLI Commands

| Command | Description |
|---------|-------------|
| `graft merge` | Merge YAML/JSON files |
| `graft diff` | Compare documents |
| `graft json` | Convert YAML to JSON |
| `graft fan` | Cross-product merge |
| `graft vaultinfo` | List vault references |

## Common Patterns

### Basic Merge

```bash
graft merge base.yml overlay.yml > result.yml
```

### With Secrets

```bash
export VAULT_ADDR=https://vault.example.com
export VAULT_TOKEN=s.xxxxx
graft merge base.yml secrets.yml
```

### Compare Documents

```bash
graft diff before.yml after.yml
```

### Multi-Environment

```bash
graft merge base.yml env/${ENVIRONMENT}.yml
```

## Library Quick Start

```go
import "github.com/fivetwenty-io/graft/pkg/graft"

// Create engine
engine, _ := graft.NewEngine()

// Parse documents
base, _ := engine.ParseFile("base.yml")
overlay, _ := engine.ParseFile("overlay.yml")

// Merge
result, _ := engine.Merge(ctx, base, overlay).Execute()

// Output
yaml, _ := result.ToYAML()
fmt.Println(string(yaml))
```

## See Also

- [User Guide](../user-guide/index.md) - Detailed feature documentation

- [Developer Guide](../developer-guide/index.md) - Library API and extension

- [Examples](../examples/index.md) - Real-world usage examples

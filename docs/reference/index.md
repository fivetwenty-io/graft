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

### Data Manipulation

| Operator | Description |
|----------|-------------|
| `grab` | Reference values from document |
| `concat` | Concatenate strings and values |
| `join` | Join array elements |
| `split` | Split string into array |
| `stringify` | Convert to YAML string |
| `keys` | Extract map keys |
| `type` | Get value type |

### Control Flow

| Operator | Description |
|----------|-------------|
| `if/elif/else/fi` | Conditional blocks |
| `for/done` | Iteration over collections |
| `while/done` | Conditional loops |
| `case/when/esac` | Pattern matching |

### Arithmetic

| Operator | Description |
|----------|-------------|
| `+` `-` `*` `/` `%` | Basic arithmetic |
| `calc` | Complex math expressions |

### Comparison & Logic

| Operator | Description |
|----------|-------------|
| `==` `!=` | Equality |
| `<` `>` `<=` `>=` | Comparison |
| `&&` `\|\|` `!` | Boolean logic |
| `? :` | Ternary conditional |

### Arrays

| Operator | Description |
|----------|-------------|
| `append` | Add to end of array |
| `prepend` | Add to beginning |
| `replace` | Replace entire array |
| `inline` | Merge by index |
| `merge` | Merge by key |
| `flatten` | Flatten nested arrays |
| `uniq` | Remove duplicates |
| `sort` | Sort elements |

### External Sources

| Operator | Description |
|----------|-------------|
| `vault` | HashiCorp Vault / OpenBao |
| `awsparam` | AWS Parameter Store |
| `awssecret` | AWS Secrets Manager |
| `nats` | NATS JetStream |
| `file` | Read file contents |
| `load` | Load and parse YAML/JSON |

## CLI Commands

| Command | Description |
|---------|-------------|
| `graft merge` | Merge YAML/JSON files |
| `graft diff` | Compare documents |
| `graft json` | Convert formats |
| `graft fan` | Cross-product merge |
| `graft vaultinfo` | List vault references |
| `graft debug` | Interactive REPL |

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
graft diff --side-by-side before.yml after.yml
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

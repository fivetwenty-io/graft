# Graft Architecture Overview

Graft is a YAML/JSON templating engine designed as an embeddable Go library with a CLI interface. The architecture follows a layered pipeline model with clear separation of concerns, supporting parallel processing at multiple levels.

## Design Principles

- **Library-First**

  All functionality is accessible via the Go API before the CLI. The `pkg/graft` package is the primary interface.

- **Pipeline Architecture**

  Clear stages with well-defined inputs and outputs enable composability and testability.

- **Parallel by Default**

  Wave-based evaluation, pooled connections, and batched requests maximize throughput.

- **Fail-Fast with Context**

  Rich error messages include source positions, hints, and suggestions for resolution.

- **Extensible**

  Custom operators, backends, and post-processors can be registered without modifying core code.

## System Architecture

```mermaid
graph TB
    subgraph CLI["CLI Layer"]
        CMD[Commands<br/>merge, diff, json, fan, debug]
        FLAGS[Flags & Options]
        REPL[Debug REPL]
    end

    subgraph Engine["Engine Layer"]
        ENG[Engine]
        PIPE[Pipeline Orchestrator]
        CACHE[Cache System]
    end

    subgraph Pipeline["Processing Pipeline"]
        PRESCAN[Pre-Scanner]
        YAMLPARSE[YAML Parser]
        ASTBUILD[AST Builder]
        MERGE[Merger]
        EVAL[Evaluator]
        POSTPROC[Post-Processor]
    end

    subgraph Parser["Parser Layer"]
        UP[Unified Parser]
        TOK[Tokenizer]
        AST[AST Nodes]
    end

    subgraph Operators["Operator Layer"]
        REG[Operator Registry]
        OPS[Operators]
        TYPE[Type Handlers]
    end

    subgraph Backends["Backend Layer"]
        VAULT[Vault/OpenBao Pool]
        AWS[AWS Session Pool]
        NATS[NATS Connection Pool]
        BATCH[Request Batcher]
    end

    subgraph Tracking["Tracking Layer"]
        HIST[History Tree]
        MEM[Document Memory]
        METRICS[Metrics]
    end

    CMD --> ENG
    REPL --> ENG
    ENG --> PIPE
    PIPE --> PRESCAN
    PRESCAN --> YAMLPARSE
    YAMLPARSE --> ASTBUILD
    ASTBUILD --> UP
    UP --> TOK
    UP --> AST
    ASTBUILD --> MERGE
    MERGE --> EVAL
    EVAL --> REG
    REG --> OPS
    OPS --> TYPE
    OPS --> BATCH
    BATCH --> VAULT
    BATCH --> AWS
    BATCH --> NATS
    EVAL --> POSTPROC
    ENG --> CACHE
    PIPE --> HIST
    MERGE --> MEM
    EVAL --> METRICS
```

## Component Inventory

### Engine Layer

| Component | Responsibility |
|-----------|----------------|
| `Engine` | Main entry point; manages configuration, operators, and state |
| `EngineConfig` | Configuration for backends, parallelism, and caching |
| `MergeBuilder` | Fluent API for merge operations |
| `Cache` | LRU cache for external lookups |

### Pipeline Layer

| Component | Responsibility |
|-----------|----------------|
| `PreScanner` | Extract operator locations before YAML parsing |
| `YAMLParser` | Parse YAML using yaml.v3 Node API |
| `ASTBuilder` | Build Graft AST from YAML nodes |
| `Merger` | Deep merge documents with array operators |
| `Evaluator` | Execute operators in dependency order |
| `PostProcessor` | Prune, cherry-pick, sort, and validate |

### Parser Layer

| Component | Responsibility |
|-----------|----------------|
| `UnifiedParser` | Recursive descent parser for expressions |
| `Tokenizer` | Lexical analysis with position tracking |
| `AST Nodes` | Expression tree with visitor pattern |
| `OperatorRegistry` | Metadata for precedence, arguments, and phase |

### Operator Layer

| Component | Responsibility |
|-----------|----------------|
| `UnifiedOperatorRegistry` | Combined metadata and implementations |
| `Operator` interface | Contract: Setup, Run, Dependencies, Phase |
| `TypeHandlers` | Polymorphic type operations |

### Backend Layer

| Component | Responsibility |
|-----------|----------------|
| `VaultClientPool` | Connection pooling per target |
| `AWSSessionPool` | AWS session management per region |
| `NATSConnectionPool` | NATS connection management |
| `RequestBatcher` | Aggregate requests for batch execution |

### Tracking Layer

| Component | Responsibility |
|-----------|----------------|
| `DocumentMemory` | Change history per path |
| `History` | Public API for history queries |
| `Metrics` | Performance counters |

## Data Flow Overview

```mermaid
flowchart LR
    subgraph Input
        YAML[YAML Bytes]
        JSON[JSON Bytes]
        FILE[File Path]
    end

    subgraph Parse
        NODE[yaml.Node]
        DOC[Document]
    end

    subgraph Transform
        MERGE[Merged Doc]
        EVAL[Evaluated Doc]
        POST[Final Doc]
    end

    subgraph Output
        YAML_OUT[YAML]
        JSON_OUT[JSON]
    end

    YAML --> NODE
    JSON --> NODE
    FILE --> NODE
    NODE --> DOC
    DOC --> MERGE
    MERGE --> EVAL
    EVAL --> POST
    POST --> YAML_OUT
    POST --> JSON_OUT
```

## Extension Points

Graft provides several extension points for customization:

### Custom Operators

```go
engine.RegisterOperator("myop", &MyOperator{})

type MyOperator struct{}

func (o *MyOperator) Setup() error { return nil }
func (o *MyOperator) Phase() OperatorPhase { return EvalPhase }
func (o *MyOperator) Run(ev *Evaluator, args []*Expr) (*Response, error) {
    return &Response{Type: Replace, Value: result}, nil
}
func (o *MyOperator) Dependencies(...) []*tree.Cursor {
    return auto
}
```

### Custom Backends

```go
engine := graft.NewEngine(
    graft.WithVaultClient(&MyVaultClient{}),
    graft.WithAWSConfig(&graft.AWSConfig{
        Endpoint: "http://localhost:4566", // LocalStack
    }),
)
```

### Custom Post-Processors

```go
type PostProcessor interface {
    Name() string
    Phase() PostProcessPhase
    Process(ctx context.Context, doc *Document, meta *Metadata) error
}

engine := graft.NewEngine(
    graft.WithPostProcessors(
        &SchemaValidator{schema: mySchema},
        &SecretDetector{patterns: patterns},
    ),
)
```

## Error Handling

Graft provides a rich error hierarchy with contextual information:

```mermaid
classDiagram
    class GraftError {
        +Code ErrorCode
        +Message string
        +Position Position
        +Path string
        +Cause error
    }

    class ParseError {
        +Source string
        +Line int
        +Column int
        +Hint string
    }

    class EvaluationError {
        +Operator string
        +Arguments []interface{}
    }

    class MergeError {
        +SourceA string
        +SourceB string
    }

    class BackendError {
        +Backend string
        +Target string
    }

    GraftError <|-- ParseError
    GraftError <|-- EvaluationError
    GraftError <|-- MergeError
    GraftError <|-- BackendError
```

### Example Error Output

```
Error at config.yml:15:34
  database:
    password: (( vault "secret/db:pass" || ))
                                         ^^
Expected: expression after '||' operator
Found: '))'

Hint: The '||' operator requires a default value.
Example: (( vault "path:key" || "default" ))
```

## Performance Expectations

| Scenario | Sequential | Parallel | Speedup |
|----------|------------|----------|---------|
| 10 files, 100 keys each, no external | 200ms | 50ms | 4x |
| 10 files, 50 vault calls | 5s | 300ms | 16x |
| 1 file, 10,000 keys | 800ms | 250ms | 3x |
| 20 files, 300 external calls | 30s | 1.5s | 20x |

## Related Documentation

- [Processing Pipeline](pipeline.md)

  Detailed walkthrough of the six-stage pipeline

- [Parser Design](parser.md)

  Unified recursive descent parser, tokenizer, and AST

- [Operator Evaluation](evaluation.md)

  Dependency analysis and wave-based execution

- [Parallel Execution Model](parallelism.md)

  File processing, evaluation waves, and batching

- [Backend Architecture](backends.md)

  Connection pooling, request batching, and caching

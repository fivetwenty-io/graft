# Creating Custom Post-Processors

Post-processors run after merge and evaluation to validate, transform, or analyze the resulting document. This guide covers implementing custom post-processors for specialized workflows.

## Post-Processor Interface

Post-processors implement the `PostProcessor` interface:

```go
type PostProcessor interface {
    // Name returns the processor identifier
    Name() string

    // Phase returns when this processor runs
    Phase() PostProcessPhase

    // Process executes the post-processor
    Process(ctx context.Context, doc *Document, meta *ProcessMetadata) error
}

type PostProcessPhase int

const (
    // Parallel phase - run concurrently with other parallel processors
    PhaseParallel PostProcessPhase = iota

    // Sequential phase - run in order after parallel phase
    PhaseSequential
)

type ProcessMetadata struct {
    // Source files that were merged
    Sources []SourceInfo

    // History if tracking enabled
    History History

    // Timing information
    ParseDuration time.Duration
    MergeDuration time.Duration
    EvalDuration  time.Duration

    // Custom metadata from operators
    Custom map[string]interface{}
}

type SourceInfo struct {
    Path  string
    Size  int64
    Keys  int
    Lines int
}
```

## Creating a Post-Processor

### Simple Validator

```go
package processors

import (
    "context"
    "fmt"

    "github.com/fivetwenty-io/graft/pkg/graft"
)

// RequiredFieldsValidator ensures specified fields exist
type RequiredFieldsValidator struct {
    fields []string
}

func NewRequiredFieldsValidator(fields ...string) *RequiredFieldsValidator {
    return &RequiredFieldsValidator{fields: fields}
}

func (p *RequiredFieldsValidator) Name() string {
    return "required-fields"
}

func (p *RequiredFieldsValidator) Phase() graft.PostProcessPhase {
    return graft.PhaseParallel // Can run concurrently
}

func (p *RequiredFieldsValidator) Process(
    ctx context.Context,
    doc *graft.Document,
    meta *graft.ProcessMetadata,
) error {
    var missing []string

    for _, field := range p.fields {
        if !doc.Has(field) {
            missing = append(missing, field)
        }
    }

    if len(missing) > 0 {
        return &graft.ValidationError{
            Processor: p.Name(),
            Message:   fmt.Sprintf("missing required fields: %v", missing),
            Fields:    missing,
        }
    }

    return nil
}
```

### Schema Validator

```go
type SchemaValidator struct {
    schema *jsonschema.Schema
}

func NewSchemaValidator(schemaPath string) (*SchemaValidator, error) {
    schema, err := jsonschema.Compile(schemaPath)
    if err != nil {
        return nil, err
    }
    return &SchemaValidator{schema: schema}, nil
}

func (p *SchemaValidator) Name() string {
    return "schema-validator"
}

func (p *SchemaValidator) Phase() graft.PostProcessPhase {
    return graft.PhaseParallel
}

func (p *SchemaValidator) Process(
    ctx context.Context,
    doc *graft.Document,
    meta *graft.ProcessMetadata,
) error {
    // Convert document to interface{} for validation
    data := doc.RawData()

    if err := p.schema.Validate(data); err != nil {
        var validationErr *jsonschema.ValidationError
        if errors.As(err, &validationErr) {
            return &graft.ValidationError{
                Processor: p.Name(),
                Message:   "schema validation failed",
                Details:   formatValidationErrors(validationErr),
            }
        }
        return err
    }

    return nil
}
```

## Processing Phases

### Parallel Phase

Parallel processors run concurrently and should not modify the document:

```go
func (p *AnalyticsCollector) Phase() graft.PostProcessPhase {
    return graft.PhaseParallel // Safe for concurrent execution
}

func (p *AnalyticsCollector) Process(
    ctx context.Context,
    doc *graft.Document,
    meta *graft.ProcessMetadata,
) error {
    // Read-only operations
    paths := doc.Paths()
    keyCount := len(paths)

    // Collect statistics
    p.metrics.Record("key_count", keyCount)
    p.metrics.Record("merge_time_ms", meta.MergeDuration.Milliseconds())

    return nil
}
```

### Sequential Phase

Sequential processors run in registration order and can modify the document:

```go
type KeySorter struct{}

func (p *KeySorter) Name() string {
    return "key-sorter"
}

func (p *KeySorter) Phase() graft.PostProcessPhase {
    return graft.PhaseSequential // Must run in order
}

func (p *KeySorter) Process(
    ctx context.Context,
    doc *graft.Document,
    meta *graft.ProcessMetadata,
) error {
    // Modify document - sort all map keys alphabetically
    return doc.SortKeys()
}
```

## Registering Post-Processors

### At Engine Creation

```go
engine, _ := graft.NewEngine(
    graft.WithPostProcessors(
        // Parallel processors (run concurrently)
        NewRequiredFieldsValidator("database.host", "database.port"),
        NewSchemaValidator("schema.json"),
        &AnalyticsCollector{},

        // Sequential processors (run in order)
        &Pruner{Keys: []string{"internal", "meta"}},
        &KeySorter{},
    ),
)
```

### Conditional Registration

```go
var processors []graft.PostProcessor

// Always validate
processors = append(processors, NewRequiredFieldsValidator("app.name"))

// Production-only validation
if env == "production" {
    processors = append(processors,
        NewSchemaValidator("strict-schema.json"),
        &SecretDetector{},
    )
}

engine, _ := graft.NewEngine(
    graft.WithPostProcessors(processors...),
)
```

## Built-in Post-Processors

Graft provides several built-in post-processors:

### Pruner

Removes specified keys from the output:

```go
graft.WithPostProcessors(
    &graft.Pruner{Keys: []string{"meta", "internal", "debug"}},
)
```

### CherryPicker

Includes only specified keys:

```go
graft.WithPostProcessors(
    &graft.CherryPicker{Keys: []string{"database", "server", "features"}},
)
```

### SecurityRedactor

Masks sensitive values in output:

```go
graft.WithPostProcessors(
    &graft.SecurityRedactor{
        Patterns: []string{"password", "secret", "api_key", "token"},
        Mask:     "***REDACTED***",
    },
)
```

## Advanced Patterns

### Conditional Processing

```go
type ConditionalValidator struct {
    condition func(*graft.Document) bool
    validator graft.PostProcessor
}

func (p *ConditionalValidator) Process(
    ctx context.Context,
    doc *graft.Document,
    meta *graft.ProcessMetadata,
) error {
    if !p.condition(doc) {
        return nil // Skip validation
    }
    return p.validator.Process(ctx, doc, meta)
}

// Usage
engine, _ := graft.NewEngine(
    graft.WithPostProcessors(
        &ConditionalValidator{
            condition: func(doc *graft.Document) bool {
                return doc.String("environment") == "production"
            },
            validator: NewStrictSchemaValidator(),
        },
    ),
)
```

### Chained Processors

```go
type ProcessorChain struct {
    processors []graft.PostProcessor
}

func (p *ProcessorChain) Name() string {
    return "chain"
}

func (p *ProcessorChain) Phase() graft.PostProcessPhase {
    return graft.PhaseSequential
}

func (p *ProcessorChain) Process(
    ctx context.Context,
    doc *graft.Document,
    meta *graft.ProcessMetadata,
) error {
    for _, proc := range p.processors {
        if err := proc.Process(ctx, doc, meta); err != nil {
            return fmt.Errorf("%s: %w", proc.Name(), err)
        }
    }
    return nil
}
```

### Document Transformation

```go
type EnvironmentExpander struct{}

func (p *EnvironmentExpander) Name() string {
    return "env-expander"
}

func (p *EnvironmentExpander) Phase() graft.PostProcessPhase {
    return graft.PhaseSequential
}

func (p *EnvironmentExpander) Process(
    ctx context.Context,
    doc *graft.Document,
    meta *graft.ProcessMetadata,
) error {
    // Walk all string values and expand environment variables
    for _, path := range doc.Paths() {
        val, err := doc.Get(path)
        if err != nil {
            continue
        }

        if str, ok := val.(string); ok {
            expanded := os.ExpandEnv(str)
            if expanded != str {
                doc.Set(path, expanded)
            }
        }
    }

    return nil
}
```

### Report Generator

```go
type MergeReportGenerator struct {
    output io.Writer
}

func (p *MergeReportGenerator) Name() string {
    return "merge-report"
}

func (p *MergeReportGenerator) Phase() graft.PostProcessPhase {
    return graft.PhaseParallel // Read-only, can run in parallel
}

func (p *MergeReportGenerator) Process(
    ctx context.Context,
    doc *graft.Document,
    meta *graft.ProcessMetadata,
) error {
    report := struct {
        Sources    []string      `json:"sources"`
        TotalKeys  int           `json:"total_keys"`
        Duration   time.Duration `json:"duration"`
        ChangedPaths []string    `json:"changed_paths,omitempty"`
    }{
        TotalKeys: len(doc.Paths()),
        Duration:  meta.ParseDuration + meta.MergeDuration + meta.EvalDuration,
    }

    for _, src := range meta.Sources {
        report.Sources = append(report.Sources, src.Path)
    }

    if meta.History != nil {
        report.ChangedPaths = meta.History.ChangedPaths()
    }

    return json.NewEncoder(p.output).Encode(report)
}
```

## Error Handling

### Validation Errors

```go
&graft.ValidationError{
    Processor: "schema-validator",
    Message:   "validation failed",
    Path:      "database.port",
    Expected:  "integer",
    Actual:    "string",
    Details: []string{
        "database.port: expected integer, got string",
        "database.host: required field missing",
    },
}
```

### Collecting Multiple Errors

```go
type MultiValidator struct {
    validators []graft.PostProcessor
}

func (p *MultiValidator) Process(
    ctx context.Context,
    doc *graft.Document,
    meta *graft.ProcessMetadata,
) error {
    var errors []error

    for _, v := range p.validators {
        if err := v.Process(ctx, doc, meta); err != nil {
            errors = append(errors, err)
        }
    }

    if len(errors) > 0 {
        return &graft.ValidationError{
            Processor: p.Name(),
            Message:   fmt.Sprintf("%d validation errors", len(errors)),
            Errors:    errors,
        }
    }

    return nil
}
```

## Testing Post-Processors

### Unit Testing

```go
func TestRequiredFieldsValidator(t *testing.T) {
    validator := NewRequiredFieldsValidator("database.host", "database.port")

    tests := []struct {
        name    string
        yaml    string
        wantErr bool
    }{
        {
            name: "all fields present",
            yaml: `
database:
  host: localhost
  port: 5432
`,
            wantErr: false,
        },
        {
            name: "missing field",
            yaml: `
database:
  host: localhost
`,
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            engine, _ := graft.NewEngine()
            doc, _ := engine.ParseYAML([]byte(tt.yaml))

            err := validator.Process(context.Background(), doc, &graft.ProcessMetadata{})

            if tt.wantErr {
                assert.Error(t, err)
            } else {
                assert.NoError(t, err)
            }
        })
    }
}
```

### Integration Testing

```go
func TestPostProcessorIntegration(t *testing.T) {
    engine, _ := graft.NewEngine(
        graft.WithPostProcessors(
            NewRequiredFieldsValidator("app.name"),
            &graft.Pruner{Keys: []string{"internal"}},
        ),
    )

    doc, _ := engine.ParseYAML([]byte(`
app:
  name: myapp
  version: 1.0
internal:
  debug: true
`))

    result, err := engine.Merge(context.Background(), doc).Execute()

    assert.NoError(t, err)
    assert.True(t, result.Has("app.name"))
    assert.False(t, result.Has("internal"))
}
```

## Best Practices

### Do

- Use `PhaseParallel` for read-only processors

- Return structured `ValidationError` with context

- Check context cancellation in long-running processors

- Document what the processor validates or transforms

- Test edge cases (empty documents, nil values)

### Don't

- Modify documents in `PhaseParallel` processors

- Assume document structure without checking

- Block indefinitely without respecting context

- Swallow errors silently

- Create side effects without documentation

## Related Documentation

- [Configuration Options](library-api/options.md) - Post-processor configuration

- [Custom Operators](custom-operators.md) - Creating operators

- [History API](library-api/history-api.md) - Accessing merge history

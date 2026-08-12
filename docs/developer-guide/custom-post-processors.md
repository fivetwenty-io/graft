# Creating Custom Post-Processors

Post-processors run after a merge's evaluation, pruning, and cherry-picking, as the last step before `Execute()` returns. This guide covers implementing custom post-processors and using graft's built-in ones.

## Post-Processor Interface

Post-processors implement the `graft.PostProcessor` interface:

```go
type PostProcessor interface {
    // Name identifies the processor; it appears in the error Execute()
    // returns if Process fails.
    Name() string

    // Phase reports when this processor runs relative to others.
    Phase() PostProcessPhase

    // Process transforms doc and returns the result. A non-nil error
    // aborts Execute(): no later processor runs.
    Process(ctx context.Context, doc graft.Document, meta *graft.ProcessMetadata) (graft.Document, error)
}

// PostProcessPhase is an alias for postprocess.Phase, not a distinct
// type of its own - graft.PhaseEarly and postprocess.PhaseEarly are the
// same value and interchangeable.
type PostProcessPhase = postprocess.Phase

const (
    PhaseEarly  = postprocess.PhaseEarly  // runs first
    PhaseNormal = postprocess.PhaseNormal // default phase
    PhaseLate   = postprocess.PhaseLate   // runs last, immediately before Execute() returns
)

type ProcessMetadata struct {
    Sources       []string // always empty in this release
    MergeCount    int
    EvalCount     int      // always 0 in this release
    StartTime     time.Time
    Duration      time.Duration
    ParseDuration time.Duration // always zero in this release
    MergeDuration time.Duration
    EvalDuration  time.Duration
    Custom        map[string]interface{} // always nil in this release
}
```

Processors run in `Phase` order, then by priority within a phase (see [Ordering](#ordering) below). A processor that returns an error aborts `Execute()` immediately: `Execute()` returns `fmt.Errorf("post-processor %q failed: %w", proc.Name(), err)`, and no later processor runs.

`ProcessMetadata.Sources`, `EvalCount`, `ParseDuration`, and `Custom` are always their zero value in this release - graft does not yet track file paths on the merge builder, does not count evaluated operators anywhere `Execute` can read it back, and parsing happens before a `MergeBuilder` exists at all (`ParseFile`/`ParseYAML`/`OverlayFile` all run ahead of `Execute`). `MergeCount`, `StartTime`, `Duration`, `MergeDuration`, and `EvalDuration` are accurate.

## Creating a Post-Processor

### Simple Field Checker

```go
package processors

import (
    "context"
    "fmt"

    "github.com/fivetwenty-io/graft/pkg/graft"
)

// requiredFieldsError is this package's own error type - graft does not
// export a ValidationError type, so a custom processor defines whatever
// error shape fits its own callers.
type requiredFieldsError struct {
    Processor string
    Missing   []string
}

func (e *requiredFieldsError) Error() string {
    return fmt.Sprintf("%s: missing required fields: %v", e.Processor, e.Missing)
}

// RequiredFieldsChecker fails the merge if any of Fields is missing from
// the final document.
type RequiredFieldsChecker struct {
    Fields []string
}

func NewRequiredFieldsChecker(fields ...string) *RequiredFieldsChecker {
    return &RequiredFieldsChecker{Fields: fields}
}

func (p *RequiredFieldsChecker) Name() string { return "required-fields" }

func (p *RequiredFieldsChecker) Phase() graft.PostProcessPhase {
    return graft.PhaseEarly
}

func (p *RequiredFieldsChecker) Process(
    ctx context.Context,
    doc graft.Document,
    meta *graft.ProcessMetadata,
) (graft.Document, error) {
    var missing []string
    for _, field := range p.Fields {
        if !doc.Has(field) {
            missing = append(missing, field)
        }
    }
    if len(missing) > 0 {
        return nil, &requiredFieldsError{Processor: p.Name(), Missing: missing}
    }
    return doc, nil
}
```

### Document Transformation

```go
type EnvironmentExpander struct{}

func (p *EnvironmentExpander) Name() string { return "env-expander" }

func (p *EnvironmentExpander) Phase() graft.PostProcessPhase {
    return graft.PhaseNormal
}

func (p *EnvironmentExpander) Process(
    ctx context.Context,
    doc graft.Document,
    meta *graft.ProcessMetadata,
) (graft.Document, error) {
    for _, path := range doc.Paths() {
        val, err := doc.Get(path)
        if err != nil {
            continue
        }
        if str, ok := val.(string); ok {
            if expanded := os.ExpandEnv(str); expanded != str {
                if err := doc.Set(path, expanded); err != nil {
                    return nil, fmt.Errorf("%s: %w", p.Name(), err)
                }
            }
        }
    }
    return doc, nil
}
```

## Ordering

Within a `Phase`, a processor that implements `PriorityPostProcessor` (adding `Priority() int`) controls its order relative to other processors in the same phase - lower runs first. A processor that only implements `PostProcessor` gets a default priority based on its phase alone (0 for `PhaseEarly`, 50 for `PhaseNormal`, 100 for `PhaseLate`), matching the built-ins' own defaults.

```go
type PriorityPostProcessor interface {
    PostProcessor
    Priority() int
}

func (p *RequiredFieldsChecker) Priority() int { return 10 }
```

There is no parallel execution phase. Every processor runs sequentially, in phase-then-priority order; graft checks `ctx.Done()` between processors and stops (returning `ctx.Err()`) if the context is cancelled mid-pipeline.

## Registering Post-Processors

### At Engine Creation

Processors registered via `graft.WithPostProcessors` at engine construction run on every merge that engine executes:

```go
engine, _ := graft.NewEngine(
    graft.WithPostProcessors(
        NewRequiredFieldsChecker("database.host", "database.port"),
        &EnvironmentExpander{},
    ),
)
```

### Per Merge

`MergeBuilder.WithPostProcessors` adds processors for one merge chain only. Processors registered this way combine with (not replace) any engine-level ones - both sets are ordered together by phase-then-priority:

```go
result, err := engine.Merge(ctx, base, overlay).
    WithPostProcessors(&EnvironmentExpander{}).
    Execute()
```

### Conditional Registration

```go
var processors []graft.PostProcessor

processors = append(processors, NewRequiredFieldsChecker("app.name"))

if env == "production" {
    processors = append(processors,
        graft.NewSecurityRedactor([]string{"password", "secret", "api_key"}, ""),
    )
}

engine, _ := graft.NewEngine(
    graft.WithPostProcessors(processors...),
)
```

## Built-in Post-Processors

Graft provides four built-in post-processor constructors, all returning `graft.PostProcessor`:

### `NewPruner`

Removes the given dot-separated paths from the output:

```go
graft.WithPostProcessors(
    graft.NewPruner("meta", "internal", "debug"),
)
```

Unlike `MergeBuilder.WithPrune` (which runs before any `WithPostProcessors` processor - see [Ordering](#ordering)), a processor built with `NewPruner` runs at its declared phase/priority position (`PhaseLate`), interleaved with other post-processors registered at that phase.

### `NewCherryPicker`

Keeps only the given dot-separated paths, discarding everything else:

```go
graft.WithPostProcessors(
    graft.NewCherryPicker("database", "server", "features"),
)
```

Same phase/priority caveat as `NewPruner` relative to `MergeBuilder.WithCherryPick`.

### `NewKeySorter`

Sorts the document's map keys recursively when `enabled` is `true` (a no-op otherwise, so it can be left registered and toggled by the argument alone):

```go
graft.WithPostProcessors(
    graft.NewKeySorter(true),
)
```

### `NewSecurityRedactor`

Replaces the value of any map entry whose key matches one of `patterns` with `mask`. Patterns are matched against key names, case-insensitively, as regular expressions (a pattern that fails to compile as a regular expression is matched literally instead, so a caller-supplied pattern never causes a panic). An empty `mask` defaults to `"***REDACTED***"`:

```go
graft.WithPostProcessors(
    graft.NewSecurityRedactor(
        []string{"password", "secret", "api_key", "token"},
        "***REDACTED***",
    ),
)
```

`NewSecurityRedactor` replaces the entire value at a matching key - scalar, map, or slice - rather than attempting to redact part of a nested structure.

### Not provided

Earlier drafts of this guide referenced `MetaKeyPruner`, `NullPruner`, and `SchemaValidator`. None of the three exists in `pkg/graft`, and none is planned:

- **`MetaKeyPruner`/`NullPruner`** - graft's real pruning model is path-based (`(( prune ))` in a document, `--prune`/`WithPrune` on the CLI/library, and `NewPruner` above), not keyed on a specific meta-key convention or on null values. Use `NewPruner` with the paths you want removed.
- **`SchemaValidator`** - graft has no defined validation semantic anywhere in the library (see the [Configuration Options](library-api/options.md) page's own note on `WithValidation`). Write a custom `PostProcessor` like `RequiredFieldsChecker` above, or validate the document after `Execute()` returns using a schema library of your choice.

## Error Handling

A `PostProcessor`'s `Process` method returns `(graft.Document, error)`. `Execute()` wraps any non-nil error as `fmt.Errorf("post-processor %q failed: %w", proc.Name(), err)`, so callers can use `errors.Is`/`errors.As` against the original error through that wrapper. A processor is free to define its own error type - as `requiredFieldsError` does above - graft does not require or export a specific validation-error shape.

### Collecting Multiple Errors

```go
type multiCheckError struct {
    Processor string
    Errors    []error
}

func (e *multiCheckError) Error() string {
    return fmt.Sprintf("%s: %d error(s)", e.Processor, len(e.Errors))
}

type MultiChecker struct {
    checks []graft.PostProcessor
}

func (p *MultiChecker) Name() string { return "multi-check" }

func (p *MultiChecker) Phase() graft.PostProcessPhase { return graft.PhaseNormal }

func (p *MultiChecker) Process(
    ctx context.Context,
    doc graft.Document,
    meta *graft.ProcessMetadata,
) (graft.Document, error) {
    var errs []error
    for _, c := range p.checks {
        if _, err := c.Process(ctx, doc, meta); err != nil {
            errs = append(errs, err)
        }
    }
    if len(errs) > 0 {
        return nil, &multiCheckError{Processor: p.Name(), Errors: errs}
    }
    return doc, nil
}
```

## Testing Post-Processors

### Unit Testing

```go
func TestRequiredFieldsChecker(t *testing.T) {
    checker := NewRequiredFieldsChecker("database.host", "database.port")

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

    engine, _ := graft.NewEngine()
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            doc, _ := engine.ParseYAML([]byte(tt.yaml))
            _, err := checker.Process(context.Background(), doc, &graft.ProcessMetadata{})

            if tt.wantErr && err == nil {
                t.Error("expected error, got nil")
            }
            if !tt.wantErr && err != nil {
                t.Errorf("unexpected error: %v", err)
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
            NewRequiredFieldsChecker("app.name"),
            graft.NewPruner("internal"),
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

    if err != nil {
        t.Fatalf("Execute() error = %v", err)
    }
    if !result.Has("app.name") {
        t.Error("expected app.name to survive")
    }
    if result.Has("internal") {
        t.Error("expected internal to be pruned")
    }
}
```

## Best Practices

### Do

- Return a wrapped, distinguishable error type so callers can use `errors.As` against it through `Execute()`'s `post-processor %q failed: %w` wrapper

- Check `ctx.Err()` in a long-running processor and return promptly if it is non-nil

- Document what the processor validates or transforms, and which phase it expects to run in

- Test edge cases (empty documents, missing paths, nil values)

### Don't

- Assume document structure without checking (`doc.Has`, `doc.Get`, checked getters)

- Retain `doc` or the `Document` `Process` returns past the call - both are backed by data `Execute` may reuse or discard once `Process` returns

- Block indefinitely without checking `ctx.Done()`

- Swallow errors silently - return them, or wrap them with context if you must transform them

## Related Documentation

- [Configuration Options](library-api/options.md) - Engine-level `WithPostProcessors`

- [Custom Operators](custom-operators.md) - Creating operators

- [MergeBuilder API](library-api/merge-builder.md) - `WithPostProcessors`, `WithPrune`, `WithCherryPick`

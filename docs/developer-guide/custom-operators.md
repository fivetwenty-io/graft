# Creating Custom Operators

Graft's operator system is extensible, allowing you to create custom operators for domain-specific functionality. This guide covers the operator interface, registration, and best practices.

## Before You Start: Registering Built-In Operators

`pkg/graft` on its own does not register any operators, custom or built-in. `grab`, `concat`, `vault`, and every other built-in operator live in `pkg/graft/operators`, which registers itself via `init()` the moment it is imported. A binary that imports `pkg/graft` but not `pkg/graft/operators` gets a parser that has never heard of `grab`. Blank-import both operator packages once, wherever your `main` package (or test binary) lives:

```go
import (
    _ "github.com/fivetwenty-io/graft/pkg/graft/controlflow" // (( if ))/(( for ))/(( case ))
    _ "github.com/fivetwenty-io/graft/pkg/graft/operators"   // grab, concat, vault, ...
)
```

`cmd/graft/main.go` does exactly this. Custom operators registered with `RegisterOperator`/`WithCustomOperator` (below) do not need their own blank import — you already have a reference to the type — but the built-ins still need `pkg/graft/operators` imported for anything, including a document containing only your custom operator, to parse correctly.

## Operator Interface

All operators implement the `Operator` interface:

```go
type Operator interface {
    // Setup performs one-time initialization before any Run call.
    Setup() error

    // Run evaluates the operator against its (unevaluated) call
    // arguments. args is the raw expression tree for each argument, not
    // resolved values - call EvaluateOperatorArgs(ev, args) first if the
    // operator wants plain Go values instead.
    Run(ev *Evaluator, args []*Expr) (*Response, error)

    // Dependencies reports which paths this operator's call depends on,
    // for dataflow ordering. Most operators return auto unchanged.
    Dependencies(ev *Evaluator, args []*Expr, locs []*tree.Cursor, auto []*tree.Cursor) []*tree.Cursor

    // Phase reports when this operator runs: MergePhase, EvalPhase, or
    // ParamPhase.
    Phase() OperatorPhase
}

// Response is what Run returns: Type is Replace (substitute Value at the
// call site) or Inject (merge Value into the parent structure).
type Response struct {
    Type  Action
    Value interface{}
}
```

`ev *Evaluator` gives the operator access to the document being evaluated (`ev.Tree`, a `map[string]interface{}`) and the current path (`ev.Here`, a `*tree.Cursor`). `Run` receives no `context.Context`; an operator that makes an outbound call builds its own (see [Outbound Calls and Deadlines](#outbound-calls-and-deadlines) below).

## Creating a Simple Operator

### Using OperatorFunc

For simple operators with no custom `Setup` or `Dependencies` logic, use the `OperatorFunc` helper, which adapts a plain function into the `Operator` interface:

```go
// Operator that returns the current timestamp.
timestampOp := &graft.OperatorFunc{
    OpPhase: graft.EvalPhase,
    Fn: func(ev *graft.Evaluator, args []*graft.Expr) (*graft.Response, error) {
        return &graft.Response{Type: graft.Replace, Value: time.Now().Unix()}, nil
    },
}

engine.RegisterOperator("timestamp", timestampOp)
```

**Usage:**

```yaml
created_at: (( timestamp ))
```

### Full Operator Implementation

For an operator with its own type, argument validation, and a default value:

```go
type EnvOperator struct{}

func (o *EnvOperator) Setup() error { return nil }

func (o *EnvOperator) Phase() graft.OperatorPhase { return graft.EvalPhase }

func (o *EnvOperator) Dependencies(ev *graft.Evaluator, args []*graft.Expr, locs, auto []*tree.Cursor) []*tree.Cursor {
    return auto
}

func (o *EnvOperator) Run(ev *graft.Evaluator, args []*graft.Expr) (*graft.Response, error) {
    resolved, err := graft.EvaluateOperatorArgs(ev, args)
    if err != nil {
        return nil, err
    }
    if len(resolved) < 1 || len(resolved) > 2 {
        return nil, fmt.Errorf("env requires 1 or 2 arguments, got %d", len(resolved))
    }

    name, ok := resolved[0].(string)
    if !ok {
        return nil, fmt.Errorf("env argument must be string, got %T", resolved[0])
    }

    value := os.Getenv(name)
    if value == "" && len(resolved) == 2 {
        // Second argument, if present, is the default.
        return &graft.Response{Type: graft.Replace, Value: resolved[1]}, nil
    }
    return &graft.Response{Type: graft.Replace, Value: value}, nil
}
```

`Operator` has no `Info()`/metadata method and no `graft.OperatorInfo` type reachable through `pkg/graft`. Name, description, and examples live only in the registration call site and this documentation — there is nowhere in the API to attach them to the operator value itself.

## Registering Operators

### At Engine Creation

```go
engine, err := graft.NewEngine(
    graft.WithCustomOperator("env", &EnvOperator{}),
    graft.WithCustomOperator("timestamp", timestampOp),
)
```

### At Runtime

```go
engine, err := graft.NewEngine()

// Register single operator.
err = engine.RegisterOperator("env", &EnvOperator{})

// Check if an operator exists before registering, since RegisterOperator
// returns an error when a name was already explicitly registered on this
// engine (registering "env" a second time is an error; shadowing a
// built-in inherited from DefaultRegistry, e.g. "grab", is not).
if _, exists := engine.GetOperator("env"); !exists {
    engine.RegisterOperator("env", &EnvOperator{})
}

// List all registered operator names.
for _, name := range engine.ListOperators() {
    fmt.Println(name)
}

// Unregister an operator.
err = engine.UnregisterOperator("env")
```

`GetOperator` returns `(graft.Operator, bool)` — the operator value and whether it exists, not a metadata struct. `ListOperators` returns `[]string` — names only.

## Argument Handling

### Evaluated vs. Raw Arguments

`args []*Expr` is the unevaluated call-site expression tree, exactly as the parser built it — a `(( grab other.path ))` argument arrives as a `*graft.Expr` with `Type: graft.Reference`, not as the string or number it will eventually resolve to. Most operators want plain Go values instead; `graft.EvaluateOperatorArgs(ev, args)` resolves the whole slice at once:

```go
func (o *MyOperator) Run(ev *graft.Evaluator, args []*graft.Expr) (*graft.Response, error) {
    resolved, err := graft.EvaluateOperatorArgs(ev, args)
    if err != nil {
        return nil, err
    }

    // resolved[i] is a plain Go value: string, int, int64, float64,
    // bool, []interface{}, map[string]interface{}, or nil - the same
    // set YAML/JSON decoding produces.
    if str, ok := resolved[0].(string); ok {
        _ = str // handle string argument
    }

    switch v := resolved[0].(type) {
    case int:
        _ = v
    case int64:
        _ = v
    case float64:
        _ = v
    }

    return &graft.Response{Type: graft.Replace, Value: resolved[0]}, nil
}
```

### Variadic Arguments

`args` is a plain slice; an operator with no fixed arity ranges over it directly:

```go
type ConcatOperator struct{}

func (o *ConcatOperator) Setup() error { return nil }

func (o *ConcatOperator) Phase() graft.OperatorPhase { return graft.EvalPhase }

func (o *ConcatOperator) Dependencies(ev *graft.Evaluator, args []*graft.Expr, locs, auto []*tree.Cursor) []*tree.Cursor {
    return auto
}

func (o *ConcatOperator) Run(ev *graft.Evaluator, args []*graft.Expr) (*graft.Response, error) {
    resolved, err := graft.EvaluateOperatorArgs(ev, args)
    if err != nil {
        return nil, err
    }

    var result strings.Builder
    for _, arg := range resolved {
        result.WriteString(fmt.Sprintf("%v", arg))
    }
    return &graft.Response{Type: graft.Replace, Value: result.String()}, nil
}
```

Graft's real `concat` operator (`pkg/graft/operators/op_concat.go`) follows the same shape.

## Resolving References and Nested Calls

### Resolving an Arbitrary Path

An operator that needs a document value at a path not passed in as an argument builds a `*tree.Cursor` and resolves it against `ev.Tree`, the same mechanism `grab` uses internally:

```go
func (o *MyOperator) Run(ev *graft.Evaluator, args []*graft.Expr) (*graft.Response, error) {
    cur, err := tree.ParseCursor("database.host")
    if err != nil {
        return nil, err
    }
    value, err := cur.Resolve(ev.Tree)
    if err != nil {
        return nil, err
    }
    return &graft.Response{Type: graft.Replace, Value: value}, nil
}
```

`ev.Here` (also a `*tree.Cursor`) is the path being evaluated right now; call `ev.Here.String()` where the earlier `EvaluationError` examples used `ctx.Path()`.

### Evaluating a Sub-expression Conditionally

An operator that only wants to evaluate one of several argument expressions — a ternary, effectively — calls `graft.EvaluateExpr` on the branch it selects, not on every argument up front:

```go
type ConditionalOperator struct{}

func (o *ConditionalOperator) Setup() error { return nil }

func (o *ConditionalOperator) Phase() graft.OperatorPhase { return graft.EvalPhase }

func (o *ConditionalOperator) Dependencies(ev *graft.Evaluator, args []*graft.Expr, locs, auto []*tree.Cursor) []*tree.Cursor {
    return auto
}

func (o *ConditionalOperator) Run(ev *graft.Evaluator, args []*graft.Expr) (*graft.Response, error) {
    if len(args) != 3 {
        return nil, fmt.Errorf("cond requires exactly 3 arguments, got %d", len(args))
    }

    condResp, err := graft.EvaluateExpr(args[0], ev)
    if err != nil {
        return nil, err
    }

    truthy, _ := condResp.Value.(bool)
    branch := args[2] // else
    if truthy {
        branch = args[1] // then
    }

    branchResp, err := graft.EvaluateExpr(branch, ev)
    if err != nil {
        return nil, err
    }
    return &graft.Response{Type: graft.Replace, Value: branchResp.Value}, nil
}
```

`graft.EvaluateExpr(e *Expr, ev *Evaluator) (*Response, error)` handles every expression type, including nested operator calls, and is what `EvaluateOperatorArgs` calls internally per argument.

## Custom Operators and Bare Identifiers

A bare identifier is an operator call only at the call-opening position of a `(( ... ))` expression — the first token right after `((`. In *every* nested position — an argument to another operator, a `||` operand, an `(( if ))` condition, a `(( for/case ))` subject — a bare name is a document reference instead, for built-in operators and custom ones alike. Registering a custom operator does not change this; it only changes what a call-opening bare identifier resolves to.

```yaml
# Works: "probeflag" is the call-opening token of its own (( ... )).
a: (( probeflag ))

# Fails: "probeflag" is an argument to concat, a nested position - it
# resolves as a document reference ("probeflag" not found), not a call.
a: (( concat "x" probeflag ))

# Fails the same way: "probeflag" is the || fallback operand.
a: (( grab missing || probeflag ))

# Fails: plain parentheses around a bare identifier do not open a call.
a: (( concat "x" (probeflag) ))

# Works: explicit call syntax opens the operator call at any position,
# nested or not.
a: (( concat "x" probeflag() ))
a: (( grab missing || probeflag() ))
```

Two control-flow positions follow the same rule and are worth calling out separately, since the failure mode there is a parse error rather than a resolution error:

```yaml
# Fails to parse: "probeflag" is not the condition's call-opening token
# (the condition itself, and "probeflag == true"/"probeflag && true", are
# all nested positions relative to it) - it resolves as a reference.
(( if probeflag ))
foo: 1
(( fi ))

(( if probeflag == true ))
foo: 1
(( fi ))

# Works: explicit call syntax.
(( if probeflag() ))
foo: 1
(( fi ))
```

A bare `(( for x in <iterable> ))` iterable or `(( case <subject> ))` subject is unconditionally rewritten to `grab <name>` before evaluation (a fixed parser rule, independent of what is registered) — `probeflag()` reaches the operator there too, the same as `(( if ))`.

There is no form of nesting, nor any amount of parenthesizing a bare identifier on its own, that opens a call outside the call-opening position. Explicit-call syntax — `name()`, with or without arguments — is the only way to invoke an operator, built-in or custom, from a nested position.

## Error Handling

Operators can return any `error`; graft does not require a specific type. `Opcall.Run` wraps whatever `Run` returns in a `*graft.PathError` carrying the call's location, so a plain `fmt.Errorf` already reports which path failed. For an error that also carries graft's `ErrorType` classification (visible to callers doing `errors.As(err, &graftErr)` against `*graft.GraftError`), use the `New*Error` constructors:

```go
func (o *MyOperator) Run(ev *graft.Evaluator, args []*graft.Expr) (*graft.Response, error) {
    if len(args) < 2 {
        return nil, graft.NewEvaluationError(
            ev.Here.String(),
            fmt.Sprintf("myop requires 2 arguments, got %d", len(args)),
            nil, // no underlying cause
        )
    }

    resolved, err := graft.EvaluateOperatorArgs(ev, args)
    if err != nil {
        return nil, err
    }

    str, ok := resolved[0].(string)
    if !ok {
        return nil, graft.NewEvaluationError(
            ev.Here.String(),
            fmt.Sprintf("first argument must be string, got %T", resolved[0]),
            nil,
        )
    }

    return &graft.Response{Type: graft.Replace, Value: str}, nil
}
```

`graft.NewEvaluationError(path, message string, cause error) *GraftError` and `graft.NewOperatorError(operator, message string, cause error) *GraftError` are the two constructors relevant to operator code; both set `Type` (`EvaluationError`/`OperatorError`) and, when `cause` is non-nil, an `Unwrap()`-visible chain. There is no `graft.EvaluationError` struct with `Operator`/`Hint`/`Arguments` fields, and no `graft.NotFoundError`/`graft.TypeMismatchError` constructible from `pkg/graft` — `graft.ErrNotFound`/`graft.ErrTypeMismatch` are sentinel values for `errors.Is` comparisons against errors `tree.Cursor.Resolve` and `Document`'s getters already return; an operator does not construct them.

## Advanced Patterns

### Stateful Operators

For operators that maintain state (guard it — `Run` may be called concurrently across an evaluation wave):

```go
type CounterOperator struct {
    mu    sync.Mutex
    count int
}

func (o *CounterOperator) Setup() error { return nil }

func (o *CounterOperator) Phase() graft.OperatorPhase { return graft.EvalPhase }

func (o *CounterOperator) Dependencies(ev *graft.Evaluator, args []*graft.Expr, locs, auto []*tree.Cursor) []*tree.Cursor {
    return auto
}

func (o *CounterOperator) Run(ev *graft.Evaluator, args []*graft.Expr) (*graft.Response, error) {
    o.mu.Lock()
    defer o.mu.Unlock()

    o.count++
    return &graft.Response{Type: graft.Replace, Value: o.count}, nil
}
```

### Operators with Extra Dependencies

`Dependencies` receives `auto` — the dependency set graft already inferred from the call's own arguments — and returns the set the scheduler should use. Most operators return `auto` unchanged (every example above does). An operator that reads a *fixed* path not passed as an argument adds it explicitly, so the dataflow scheduler evaluates that path first:

```go
type ConfiguredOperator struct {
    dependsOn string // e.g. "meta.owner"
}

func (o *ConfiguredOperator) Setup() error { return nil }

func (o *ConfiguredOperator) Phase() graft.OperatorPhase { return graft.EvalPhase }

func (o *ConfiguredOperator) Dependencies(ev *graft.Evaluator, args []*graft.Expr, locs, auto []*tree.Cursor) []*tree.Cursor {
    cur, err := tree.ParseCursor(o.dependsOn)
    if err != nil {
        return auto
    }
    return append(auto, cur)
}

func (o *ConfiguredOperator) Run(ev *graft.Evaluator, args []*graft.Expr) (*graft.Response, error) {
    cur, err := tree.ParseCursor(o.dependsOn)
    if err != nil {
        return nil, err
    }
    value, err := cur.Resolve(ev.Tree)
    if err != nil {
        return nil, err
    }
    return &graft.Response{Type: graft.Replace, Value: value}, nil
}
```

### Outbound Calls and Deadlines

`Run` receives no `context.Context`. An operator that calls a network service builds its own — the same pattern `pkg/graft/operators`' own `file`/backend-resolution code uses — rather than relying on one threaded in from evaluation:

```go
type HTTPOperator struct {
    client  *http.Client
    timeout time.Duration
}

func NewHTTPOperator(timeout time.Duration) *HTTPOperator {
    return &HTTPOperator{client: &http.Client{Timeout: timeout}, timeout: timeout}
}

func (o *HTTPOperator) Setup() error { return nil }

func (o *HTTPOperator) Phase() graft.OperatorPhase { return graft.EvalPhase }

func (o *HTTPOperator) Dependencies(ev *graft.Evaluator, args []*graft.Expr, locs, auto []*tree.Cursor) []*tree.Cursor {
    return auto
}

func (o *HTTPOperator) Run(ev *graft.Evaluator, args []*graft.Expr) (*graft.Response, error) {
    resolved, err := graft.EvaluateOperatorArgs(ev, args)
    if err != nil {
        return nil, err
    }
    if len(resolved) != 1 {
        return nil, fmt.Errorf("http requires exactly 1 argument, got %d", len(resolved))
    }
    url, ok := resolved[0].(string)
    if !ok {
        return nil, fmt.Errorf("http argument must be a string URL, got %T", resolved[0])
    }

    ctx, cancel := context.WithTimeout(context.Background(), o.timeout)
    defer cancel()

    req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
    if err != nil {
        return nil, err
    }
    resp, err := o.client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return nil, err
    }
    return &graft.Response{Type: graft.Replace, Value: string(body)}, nil
}

// Registration.
httpOp := NewHTTPOperator(30 * time.Second)
engine.RegisterOperator("http", httpOp)
```

### Caching Operators

Combine the stateful and outbound-call patterns above: guard the cache with a mutex or `sync.Map`, and build a fresh timeout context per uncached lookup.

```go
type cacheEntry struct {
    value     interface{}
    expiresAt time.Time
}

type CachedOperator struct {
    fetch func(ctx context.Context, key string) (interface{}, error)
    ttl   time.Duration
    cache sync.Map
}

func (o *CachedOperator) Setup() error { return nil }

func (o *CachedOperator) Phase() graft.OperatorPhase { return graft.EvalPhase }

func (o *CachedOperator) Dependencies(ev *graft.Evaluator, args []*graft.Expr, locs, auto []*tree.Cursor) []*tree.Cursor {
    return auto
}

func (o *CachedOperator) Run(ev *graft.Evaluator, args []*graft.Expr) (*graft.Response, error) {
    resolved, err := graft.EvaluateOperatorArgs(ev, args)
    if err != nil {
        return nil, err
    }
    key, ok := resolved[0].(string)
    if !ok {
        return nil, fmt.Errorf("cached operator's argument must be a string key, got %T", resolved[0])
    }

    if entry, ok := o.cache.Load(key); ok {
        ce := entry.(*cacheEntry)
        if time.Now().Before(ce.expiresAt) {
            return &graft.Response{Type: graft.Replace, Value: ce.value}, nil
        }
    }

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    value, err := o.fetch(ctx, key)
    if err != nil {
        return nil, err
    }

    o.cache.Store(key, &cacheEntry{value: value, expiresAt: time.Now().Add(o.ttl)})
    return &graft.Response{Type: graft.Replace, Value: value}, nil
}
```

## Testing Operators

### Unit Testing

`graft.NewTestEvaluator(t, yaml)` builds a real `*graft.Evaluator` over a fixture document, so an operator's `Run` method can be called directly without going through a full `Engine.Evaluate`:

```go
func TestTimestampOperator(t *testing.T) {
    ev := graft.NewTestEvaluator(t, `created_at: (( timestamp ))`)

    resp, err := timestampOp.Run(ev, nil)

    assert.NoError(t, err)
    assert.Equal(t, graft.Replace, resp.Type)
    assert.NotNil(t, resp.Value)
}
```

### Integration Testing

```go
func TestCustomOperatorIntegration(t *testing.T) {
    engine, err := graft.NewEngine(
        graft.WithCustomOperator("env", &EnvOperator{}),
    )
    if err != nil {
        t.Fatalf("NewEngine: %v", err)
    }

    t.Setenv("DB_HOST", "localhost")

    doc, err := engine.ParseYAML([]byte(`
database:
  host: (( env "DB_HOST" ))
`))
    if err != nil {
        t.Fatalf("ParseYAML: %v", err)
    }

    result, err := engine.Evaluate(context.Background(), doc)
    if err != nil {
        t.Fatalf("Evaluate: %v", err)
    }

    host, err := result.GetString("database.host")
    if err != nil || host != "localhost" {
        t.Errorf("database.host = %q, %v; want %q, nil", host, err, "localhost")
    }
}
```

See [Testing Guide](testing.md) for `MockOperator`/`TestHelper.TestWithMockOperator` (stubbing an operator you don't own) and the mock-engine patterns for testing documents that call built-in operators like `vault`/`awsparam` without live backends.

## Best Practices

### Do

- Validate argument count and type right after `EvaluateOperatorArgs`, before using any value

- Return errors from `graft.NewEvaluationError`/`graft.NewOperatorError` (or a plain `error`) — the path is added automatically by `Opcall.Run`'s `*graft.PathError` wrapping

- Give any outbound call its own `context.WithTimeout`, since `Run` receives no context

- Use `sync.Mutex`/`sync.Map` for stateful operators; a wave of independent operators can run concurrently

- Test with `graft.NewTestEvaluator` for unit tests and a real `Engine.Evaluate` for integration tests

- Test edge cases: nil `args`, wrong argument types, empty values

### Don't

- Modify `ev.Tree` directly during evaluation — return the value via `*graft.Response` instead

- Assume argument types without a type assertion or `switch`

- Block on a network call with no deadline

- Reference a `graft.EvalContext`, `graft.OperatorInfo`, `ctx.Resolve`/`ctx.Evaluate`/`ctx.Context()`, or any other `Evaluate(ctx, args)`-shaped signature — `Operator.Run(ev *Evaluator, args []*Expr)` is the only entry point

## Example: Complete Custom Operator

```go
package operators

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "time"

    "github.com/fivetwenty-io/graft/pkg/graft"
    "github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// JSONAPIOperator fetches JSON from an API and optionally extracts a value.
type JSONAPIOperator struct {
    client *http.Client
}

func NewJSONAPIOperator(timeout time.Duration) *JSONAPIOperator {
    return &JSONAPIOperator{client: &http.Client{Timeout: timeout}}
}

func (o *JSONAPIOperator) Setup() error { return nil }

func (o *JSONAPIOperator) Phase() graft.OperatorPhase { return graft.EvalPhase }

func (o *JSONAPIOperator) Dependencies(ev *graft.Evaluator, args []*graft.Expr, locs, auto []*tree.Cursor) []*tree.Cursor {
    return auto
}

func (o *JSONAPIOperator) Run(ev *graft.Evaluator, args []*graft.Expr) (*graft.Response, error) {
    resolved, err := graft.EvaluateOperatorArgs(ev, args)
    if err != nil {
        return nil, err
    }
    if len(resolved) < 1 || len(resolved) > 2 {
        return nil, graft.NewEvaluationError(ev.Here.String(),
            fmt.Sprintf("jsonapi requires 1-2 arguments, got %d", len(resolved)), nil)
    }

    url, ok := resolved[0].(string)
    if !ok {
        return nil, graft.NewEvaluationError(ev.Here.String(),
            fmt.Sprintf("jsonapi URL must be string, got %T", resolved[0]), nil)
    }

    ctx, cancel := context.WithTimeout(context.Background(), o.client.Timeout)
    defer cancel()

    req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
    if err != nil {
        return nil, err
    }

    resp, err := o.client.Do(req)
    if err != nil {
        return nil, graft.NewOperatorError("jsonapi", fmt.Sprintf("HTTP request failed: %v", err), err)
    }
    defer resp.Body.Close()

    var data interface{}
    if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
        return nil, err
    }

    if len(resolved) == 2 {
        jsonPath, _ := resolved[1].(string)
        cur, err := tree.ParseCursor(jsonPath)
        if err != nil {
            return nil, err
        }
        extracted, err := cur.Resolve(data)
        if err != nil {
            return nil, err
        }
        return &graft.Response{Type: graft.Replace, Value: extracted}, nil
    }

    return &graft.Response{Type: graft.Replace, Value: data}, nil
}

// Registration.
//
//  jsonOp := NewJSONAPIOperator(30 * time.Second)
//  engine.RegisterOperator("jsonapi", jsonOp)
//
// Usage:
//
//  (( jsonapi "https://api.example.com/config" ))
//  (( jsonapi "https://api.example.com/data" "items[0].name" ))
```

## Related Documentation

- [Operator Reference](../user-guide/operators/index.md) - Built-in operators

- [Configuration Options](library-api/options.md) - Engine configuration

- [Engine Interface](library-api/engine.md) - `RegisterOperator`/`GetOperator`/`ListOperators`/`UnregisterOperator` reference

- [Testing Guide](testing.md) - Testing with mock engine

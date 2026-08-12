# Testing with Graft

Graft provides comprehensive testing support for applications that embed the library. This guide covers mock engines, testing patterns, and best practices.

## Mock Engine

The mock engine allows testing without real external services:

```go
import (
    "testing"

    "github.com/fivetwenty-io/graft/pkg/graft"
)

func TestConfigMerge(t *testing.T) {
    // Create mock engine
    engine := graft.NewMockEngine()

    // Pre-populate mock responses
    engine.MockVault("secret/db:password", "test-password")
    engine.MockVault("secret/api:key", "test-api-key")

    engine.MockAWSParam("/app/config", `{"host": "localhost"}`)
    engine.MockAWSSecret("prod/db", map[string]string{
        "username": "testuser",
        "password": "testpass",
    })

    engine.MockNATS("kv:config/settings", map[string]interface{}{
        "debug": true,
        "level": "info",
    })

    // Test your code
    doc, _ := engine.ParseYAML([]byte(`
database:
  password: (( vault "secret/db:password" ))
  host: (( awsparam "/app/config" ))
`))

    result, err := engine.Evaluate(context.Background(), doc)
    if err != nil {
        t.Fatalf("evaluation failed: %v", err)
    }

    // Assertions
    if result.String("database.password") != "test-password" {
        t.Errorf("expected test-password, got %s", result.String("database.password"))
    }
}
```

## Mock Engine API

### Setting Mock Values

```go
engine := graft.NewMockEngine()

// Vault mocks. MockVault takes the full "path:key" string as it appears
// in the operator call; MockVaultPath is the same seed for callers who
// have path and key as separate strings.
engine.MockVault("secret/path:key1", "value1")
engine.MockVaultPath("secret/path", "key2", "value2")

// AWS Parameter Store mocks. MockAWSParamJSON JSON-encodes value and
// seeds the resulting string, matching how Parameter Store only ever
// stores strings.
engine.MockAWSParam("/path/to/param", "value")
engine.MockAWSParamJSON("/path/to/json", map[string]interface{}{"key": "value"})

// AWS Secrets Manager mocks. Unlike MockAWSParamJSON, the value is
// returned as-is, since Secrets Manager secrets are not restricted to
// strings.
engine.MockAWSSecret("secret-name", "string-value")
engine.MockAWSSecret("secret-name", map[string]string{
    "username": "user",
    "password": "pass",
})

// NATS mocks.
engine.MockNATS("kv:bucket/key", "value")
engine.MockNATS("obj:bucket/file", []byte("file contents"))
```

### Mock Errors

An unseeded path already reports "not found" without any error injection;
`MockVaultError`/`MockAWSParamError` are for simulating other failures
(there is no `MockAWSSecretError` or `MockNATSError` - an unseeded
`awssecret`/`nats` lookup always reports "not found"):

```go
engine := graft.NewMockEngine()

// Mock an unreachable backend.
engine.MockVaultError("secret/down:pass", errors.New("vault unreachable"))

// Mock a timeout.
engine.MockVaultError("secret/slow:pass", context.DeadlineExceeded)

// Mock a custom error.
engine.MockAWSParamError("/path", fmt.Errorf("access denied"))
```

### Verifying Calls

```go
engine := graft.NewMockEngine()

// ... run test ...

// Verify vault was called
calls := engine.VaultCalls()
if len(calls) != 2 {
    t.Errorf("expected 2 vault calls, got %d", len(calls))
}

// Verify specific path was called
if !engine.WasVaultCalled("secret/db:password") {
    t.Error("expected vault call for secret/db:password")
}

// Get call details
for _, call := range calls {
    fmt.Printf("Path: %s, Target: %s\n", call.Path, call.Target)
}
```

## Testing Patterns

### Table-Driven Tests

```go
func TestOperatorEvaluation(t *testing.T) {
    tests := []struct {
        name     string
        yaml     string
        mocks    map[string]interface{}
        expected map[string]interface{}
        wantErr  bool
    }{
        {
            name: "simple vault reference",
            yaml: `password: (( vault "secret/db:pass" ))`,
            mocks: map[string]interface{}{
                "secret/db:pass": "secret123",
            },
            expected: map[string]interface{}{
                "password": "secret123",
            },
        },
        {
            name: "vault with default",
            yaml: `password: (( vault "secret/missing:pass" || "default" ))`,
            mocks: map[string]interface{}{},
            expected: map[string]interface{}{
                "password": "default",
            },
        },
        {
            name: "missing required vault",
            yaml: `password: (( vault "secret/required:pass" ))`,
            mocks: map[string]interface{}{},
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            engine := graft.NewMockEngine()

            for path, value := range tt.mocks {
                engine.MockVault(path, value)
            }

            doc, _ := engine.ParseYAML([]byte(tt.yaml))
            result, err := engine.Evaluate(context.Background(), doc)

            if tt.wantErr {
                if err == nil {
                    t.Error("expected error, got nil")
                }
                return
            }

            if err != nil {
                t.Fatalf("unexpected error: %v", err)
            }

            for path, expected := range tt.expected {
                actual, _ := result.Get(path)
                if actual != expected {
                    t.Errorf("path %s: expected %v, got %v", path, expected, actual)
                }
            }
        })
    }
}
```

### Testing Custom Operators

```go
func TestCustomEnvOperator(t *testing.T) {
    // Set test environment
    os.Setenv("TEST_HOST", "localhost")
    os.Setenv("TEST_PORT", "8080")
    defer func() {
        os.Unsetenv("TEST_HOST")
        os.Unsetenv("TEST_PORT")
    }()

    engine, _ := graft.NewEngine(
        graft.WithCustomOperator("env", &EnvOperator{}),
    )

    doc, _ := engine.ParseYAML([]byte(`
server:
  host: (( env "TEST_HOST" ))
  port: (( env "TEST_PORT" ))
`))

    result, err := engine.Evaluate(context.Background(), doc)
    if err != nil {
        t.Fatalf("evaluation failed: %v", err)
    }

    if result.String("server.host") != "localhost" {
        t.Errorf("expected localhost, got %s", result.String("server.host"))
    }
}
```

### Testing Post-Processors

`NewRequiredFieldsChecker` here is the example custom `graft.PostProcessor` built in [Custom Post-Processors](custom-post-processors.md#simple-field-checker), not part of `pkg/graft` itself:

```go
func TestRequiredFieldsChecker(t *testing.T) {
    engine, _ := graft.NewEngine(
        graft.WithPostProcessors(
            NewRequiredFieldsChecker("app.name", "app.version"),
        ),
    )

    tests := []struct {
        name    string
        yaml    string
        wantErr bool
    }{
        {
            name: "all required fields present",
            yaml: `
app:
  name: myapp
  version: 1.0.0
`,
            wantErr: false,
        },
        {
            name: "missing required field",
            yaml: `
app:
  name: myapp
`,
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            doc, _ := engine.ParseYAML([]byte(tt.yaml))
            _, err := engine.Merge(context.Background(), doc).Execute()

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

### Testing Merge Operations

```go
func TestMergeOverlay(t *testing.T) {
    engine, _ := graft.NewEngine()

    base, _ := engine.ParseYAML([]byte(`
database:
  host: localhost
  port: 5432
  timeout: 30
`))

    overlay, _ := engine.ParseYAML([]byte(`
database:
  host: db.production.com
  ssl: true
`))

    result, err := engine.Merge(context.Background(), base, overlay).Execute()
    if err != nil {
        t.Fatalf("merge failed: %v", err)
    }

    // Verify overlay values applied
    if result.String("database.host") != "db.production.com" {
        t.Error("host not overridden")
    }

    // Verify base values preserved
    if result.Int("database.port") != 5432 {
        t.Error("port not preserved")
    }

    // Verify new values added
    if !result.Bool("database.ssl") {
        t.Error("ssl not added")
    }
}
```

### Testing with History

A merge that overwrites an existing key records exactly one entry for
that path (`NewValue` "production.com"), not one entry per input
document. Reading the prior value back needs `Timeline()`/`Query()`, not
`ForPath()`: `ForPath()`'s `OldValue` is nil on a path's first recorded
entry, while `Timeline()`/`Query()` carry the real prior value ("localhost")
for that same change - see
[History Interface](library-api/history-api.md#historyentry) for the full
`OldValue` asymmetry.

```go
func TestMergeHistory(t *testing.T) {
    engine, _ := graft.NewEngine()

    base, _ := engine.ParseYAML([]byte(`host: localhost`))
    overlay, _ := engine.ParseYAML([]byte(`host: production.com`))

    result, _ := engine.Merge(context.Background(), base, overlay).
        TrackHistory().
        Execute()

    history := result.History()

    entries := history.ForPath("host")
    if len(entries) != 1 {
        t.Fatalf("expected 1 history entry, got %d", len(entries))
    }

    if entries[0].NewValue != "production.com" {
        t.Errorf("expected NewValue \"production.com\", got %v", entries[0].NewValue)
    }
    if entries[0].Phase != graft.PhaseMerge {
        t.Errorf("expected Phase PhaseMerge, got %v", entries[0].Phase)
    }

    // Timeline()/Query() carry the accurate OldValue even on a path's
    // first recorded entry; ForPath() does not (see history-api.md).
    if got := history.Timeline()[0].OldValue; got != "localhost" {
        t.Errorf("expected OldValue \"localhost\", got %v", got)
    }
}
```

## Integration Testing

### With Real Backends (Optional)

```go
// +build integration

package integration

import (
    "os"
    "testing"

    "github.com/fivetwenty-io/graft/pkg/graft"
)

func TestVaultIntegration(t *testing.T) {
    vaultAddr := os.Getenv("VAULT_ADDR")
    vaultToken := os.Getenv("VAULT_TOKEN")

    if vaultAddr == "" || vaultToken == "" {
        t.Skip("VAULT_ADDR and VAULT_TOKEN required for integration tests")
    }

    engine, _ := graft.NewEngine(
        graft.WithBackendRegistry(true),
        graft.WithVault(graft.VaultConfig{
            Address: vaultAddr,
            Token:   vaultToken,
        }),
    )

    // Test actual vault access
    doc, _ := engine.ParseYAML([]byte(`
secret: (( vault "secret/test:value" ))
`))

    result, err := engine.Evaluate(context.Background(), doc)
    if err != nil {
        t.Fatalf("vault access failed: %v", err)
    }

    if result.String("secret") == "" {
        t.Error("expected non-empty secret")
    }
}
```

### Testing HTTP Service

```go
func TestConfigServiceHTTP(t *testing.T) {
    // Create mock engine for service
    mockEngine := graft.NewMockEngine()
    mockEngine.MockVault("secret/db:password", "test-pass")

    service := NewConfigService(mockEngine)
    handler := NewConfigHandler(service)

    // Create test server
    server := httptest.NewServer(http.HandlerFunc(handler.GetConfig))
    defer server.Close()

    // Make request
    resp, err := http.Get(server.URL + "?env=test")
    if err != nil {
        t.Fatalf("request failed: %v", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        t.Errorf("expected 200, got %d", resp.StatusCode)
    }

    // Verify response
    var config map[string]interface{}
    json.NewDecoder(resp.Body).Decode(&config)

    // Assert config values...
}
```

## Benchmark Testing

```go
func BenchmarkMerge(b *testing.B) {
    engine, _ := graft.NewEngine()

    base, _ := engine.ParseFile("testdata/large-base.yml")
    overlay, _ := engine.ParseFile("testdata/large-overlay.yml")

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        engine.Merge(context.Background(), base, overlay).Execute()
    }
}

func BenchmarkMergeWithHistory(b *testing.B) {
    engine, _ := graft.NewEngine()

    base, _ := engine.ParseFile("testdata/large-base.yml")
    overlay, _ := engine.ParseFile("testdata/large-overlay.yml")

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        engine.Merge(context.Background(), base, overlay).
            TrackHistory().
            Execute()
    }
}

func BenchmarkParallel(b *testing.B) {
    engine, _ := graft.NewEngine(
        graft.WithPipeline(graft.PipelineHighThroughput),
    )

    base, _ := engine.ParseFile("testdata/large-base.yml")

    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            engine.Merge(context.Background(), base).Execute()
        }
    })
}
```

## Test Fixtures

### Using testdata Directory

```
testdata/
├── base.yml
├── overlay.yml
├── expected/
│   ├── simple-merge.yml
│   └── complex-merge.yml
└── fixtures/
    ├── vault-responses.json
    └── aws-responses.json
```

### Loading Fixtures

```go
func loadFixture(t *testing.T, name string) []byte {
    t.Helper()
    data, err := os.ReadFile(filepath.Join("testdata", name))
    if err != nil {
        t.Fatalf("failed to load fixture %s: %v", name, err)
    }
    return data
}

func TestWithFixtures(t *testing.T) {
    engine, _ := graft.NewEngine()

    base, _ := engine.ParseYAML(loadFixture(t, "base.yml"))
    overlay, _ := engine.ParseYAML(loadFixture(t, "overlay.yml"))

    result, _ := engine.Merge(context.Background(), base, overlay).Execute()

    expected, _ := engine.ParseYAML(loadFixture(t, "expected/simple-merge.yml"))

    // Compare documents
    diff := engine.Diff(result, expected)
    if diff.HasChanges() {
        t.Errorf("result differs from expected:\n%v", diff.Changes())
    }
}
```

## Test Helpers

### Assert Package

```go
package testutil

import (
    "testing"

    "github.com/fivetwenty-io/graft/pkg/graft"
)

func AssertPath(t *testing.T, doc graft.Document, path string, expected interface{}) {
    t.Helper()
    actual, err := doc.Get(path)
    if err != nil {
        t.Errorf("path %s not found: %v", path, err)
        return
    }
    if actual != expected {
        t.Errorf("path %s: expected %v, got %v", path, expected, actual)
    }
}

func AssertHasPath(t *testing.T, doc graft.Document, path string) {
    t.Helper()
    if !doc.Has(path) {
        t.Errorf("expected path %s to exist", path)
    }
}

func AssertNoPath(t *testing.T, doc graft.Document, path string) {
    t.Helper()
    if doc.Has(path) {
        t.Errorf("expected path %s to not exist", path)
    }
}

func AssertDocumentsEqual(t *testing.T, engine graft.Engine, a, b graft.Document) {
    t.Helper()
    diff := engine.Diff(a, b)
    if diff.HasChanges() {
        t.Errorf("documents differ:\n")
        for _, change := range diff.Changes() {
            t.Errorf("  %s: %v -> %v", change.Path, change.OldValue, change.NewValue)
        }
    }
}
```

### Usage

```go
func TestConfig(t *testing.T) {
    engine, _ := graft.NewEngine()
    doc, _ := engine.ParseFile("config.yml")

    testutil.AssertPath(t, doc, "database.host", "localhost")
    testutil.AssertHasPath(t, doc, "database.port")
    testutil.AssertNoPath(t, doc, "internal.debug")
}
```

## Best Practices

### Do

- Use mock engine for unit tests

- Use table-driven tests for coverage

- Test error cases and edge conditions

- Benchmark performance-critical paths

- Use fixtures for complex test data

- Clean up environment variables in tests

### Don't

- Use real backends in unit tests

- Hardcode test values in assertions

- Skip error case testing

- Ignore context cancellation in tests

- Share mutable state between tests

## Related Documentation

- [Mock Engine API](library-api/index.md) - Complete mock API

- [Custom Operators](custom-operators.md) - Testing custom operators

- [Custom Backends](custom-backends.md) - Testing custom backends

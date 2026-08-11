# Creating Custom Backends

Graft's backend system enables integration with external secret stores and data sources. This guide covers creating custom backends for your infrastructure needs.

## Backend Interface

Backends implement the `Backend` interface:

```go
type Backend interface {
    // Name returns the backend identifier
    Name() string

    // Get retrieves a value from the backend
    Get(ctx context.Context, path string) (interface{}, error)

    // GetBatch retrieves multiple values efficiently
    GetBatch(ctx context.Context, paths []string) (map[string]interface{}, error)

    // Health checks backend connectivity
    Health(ctx context.Context) error

    // Close releases backend resources
    Close() error
}
```

## Creating a Simple Backend

### Basic Implementation

```go
package backends

import (
    "context"
    "fmt"
    "sync"

    "github.com/fivetwenty-io/graft/pkg/graft"
)

// RedisBackend implements graft.Backend for Redis
type RedisBackend struct {
    client *redis.Client
    prefix string
}

func NewRedisBackend(addr, prefix string) (*RedisBackend, error) {
    client := redis.NewClient(&redis.Options{
        Addr: addr,
    })

    return &RedisBackend{
        client: client,
        prefix: prefix,
    }, nil
}

func (b *RedisBackend) Name() string {
    return "redis"
}

func (b *RedisBackend) Get(ctx context.Context, path string) (interface{}, error) {
    key := b.prefix + path

    val, err := b.client.Get(ctx, key).Result()
    if err == redis.Nil {
        return nil, &graft.NotFoundError{Path: path}
    }
    if err != nil {
        return nil, &graft.BackendError{
            Backend: "redis",
            Message: fmt.Sprintf("failed to get key %s: %v", key, err),
            Cause:   err,
        }
    }

    return val, nil
}

func (b *RedisBackend) GetBatch(ctx context.Context, paths []string) (map[string]interface{}, error) {
    results := make(map[string]interface{})

    // Build keys
    keys := make([]string, len(paths))
    for i, path := range paths {
        keys[i] = b.prefix + path
    }

    // MGET for batch retrieval
    vals, err := b.client.MGet(ctx, keys...).Result()
    if err != nil {
        return nil, err
    }

    for i, val := range vals {
        if val != nil {
            results[paths[i]] = val
        }
    }

    return results, nil
}

func (b *RedisBackend) Health(ctx context.Context) error {
    return b.client.Ping(ctx).Err()
}

func (b *RedisBackend) Close() error {
    return b.client.Close()
}
```

## Registering Backends

### At Engine Creation

```go
redisBackend, _ := NewRedisBackend("localhost:6379", "config:")

engine, _ := graft.NewEngine(
    graft.WithBackend(redisBackend),
)
```

### At Runtime

```go
engine, _ := graft.NewEngine()

// Register backend
err := engine.RegisterBackend(redisBackend)

// Get backend
backend, exists := engine.GetBackend("redis")

// List backends
backends := engine.ListBackends()

// Unregister
err = engine.UnregisterBackend("redis")
```

## Creating an Operator for the Backend

Backends typically need a corresponding operator:

```go
type RedisOperator struct {
    backend *RedisBackend
}

func NewRedisOperator(backend *RedisBackend) *RedisOperator {
    return &RedisOperator{backend: backend}
}

func (o *RedisOperator) Evaluate(ctx graft.EvalContext, args []interface{}) (interface{}, error) {
    if len(args) < 1 {
        return nil, &graft.EvaluationError{
            Operator: "redis",
            Message:  "requires at least 1 argument (key)",
        }
    }

    key, ok := args[0].(string)
    if !ok {
        return nil, &graft.EvaluationError{
            Operator: "redis",
            Message:  fmt.Sprintf("key must be string, got %T", args[0]),
        }
    }

    return o.backend.Get(ctx.Context(), key)
}

func (o *RedisOperator) Info() graft.OperatorInfo {
    return graft.OperatorInfo{
        Name:        "redis",
        Description: "Retrieve value from Redis",
        MinArgs:     1,
        MaxArgs:     1,
        ArgTypes:    []string{"string (key)"},
        Returns:     "string",
        Examples:    []string{`(( redis "config/database/host" ))`},
        Category:    "backend",
    }
}

// Registration
backend, _ := NewRedisBackend("localhost:6379", "app:")
engine, _ := graft.NewEngine(
    graft.WithBackend(backend),
    graft.WithCustomOperator("redis", NewRedisOperator(backend)),
)
```

**Usage:**

```yaml
database:
  host: (( redis "database/host" ))
  port: (( redis "database/port" ))
```

## Connection Pooling

For performance, implement connection pooling:

```go
type PooledBackend struct {
    pool    chan *connection
    factory func() (*connection, error)
    timeout time.Duration
}

type connection struct {
    client  interface{}
    created time.Time
}

func NewPooledBackend(size int, factory func() (*connection, error)) *PooledBackend {
    b := &PooledBackend{
        pool:    make(chan *connection, size),
        factory: factory,
        timeout: 30 * time.Second,
    }

    // Pre-populate pool
    for i := 0; i < size; i++ {
        conn, err := factory()
        if err == nil {
            b.pool <- conn
        }
    }

    return b
}

func (b *PooledBackend) acquire(ctx context.Context) (*connection, error) {
    select {
    case conn := <-b.pool:
        return conn, nil
    case <-ctx.Done():
        return nil, ctx.Err()
    case <-time.After(b.timeout):
        // Create new connection if pool exhausted
        return b.factory()
    }
}

func (b *PooledBackend) release(conn *connection) {
    select {
    case b.pool <- conn:
        // Returned to pool
    default:
        // Pool full, discard connection
    }
}
```

## Request Batching

Implement batching for backends that support batch operations:

```go
type BatchingBackend struct {
    backend  Backend
    batcher  *requestBatcher
}

type requestBatcher struct {
    requests  chan batchRequest
    batchSize int
    timeout   time.Duration
}

type batchRequest struct {
    path     string
    response chan batchResponse
}

type batchResponse struct {
    value interface{}
    err   error
}

func NewBatchingBackend(backend Backend, batchSize int, timeout time.Duration) *BatchingBackend {
    b := &BatchingBackend{
        backend: backend,
        batcher: &requestBatcher{
            requests:  make(chan batchRequest, 1000),
            batchSize: batchSize,
            timeout:   timeout,
        },
    }

    go b.batcher.run(backend)

    return b
}

func (r *requestBatcher) run(backend Backend) {
    for {
        batch := make([]batchRequest, 0, r.batchSize)
        timer := time.NewTimer(r.timeout)

        // Collect batch
    collecting:
        for len(batch) < r.batchSize {
            select {
            case req := <-r.requests:
                batch = append(batch, req)
            case <-timer.C:
                break collecting
            }
        }
        timer.Stop()

        if len(batch) == 0 {
            continue
        }

        // Execute batch
        paths := make([]string, len(batch))
        for i, req := range batch {
            paths[i] = req.path
        }

        results, err := backend.GetBatch(context.Background(), paths)

        // Distribute results
        for _, req := range batch {
            if err != nil {
                req.response <- batchResponse{err: err}
            } else if val, ok := results[req.path]; ok {
                req.response <- batchResponse{value: val}
            } else {
                req.response <- batchResponse{err: &graft.NotFoundError{Path: req.path}}
            }
            close(req.response)
        }
    }
}

func (b *BatchingBackend) Get(ctx context.Context, path string) (interface{}, error) {
    response := make(chan batchResponse, 1)

    select {
    case b.batcher.requests <- batchRequest{path: path, response: response}:
    case <-ctx.Done():
        return nil, ctx.Err()
    }

    select {
    case resp := <-response:
        return resp.value, resp.err
    case <-ctx.Done():
        return nil, ctx.Err()
    }
}
```

## Multi-Target Support

Support named targets for different environments:

```go
type MultiTargetBackend struct {
    defaultTarget string
    targets       map[string]Backend
    mu            sync.RWMutex
}

func NewMultiTargetBackend(defaultBackend Backend) *MultiTargetBackend {
    return &MultiTargetBackend{
        defaultTarget: "default",
        targets: map[string]Backend{
            "default": defaultBackend,
        },
    }
}

func (b *MultiTargetBackend) AddTarget(name string, backend Backend) {
    b.mu.Lock()
    defer b.mu.Unlock()
    b.targets[name] = backend
}

func (b *MultiTargetBackend) Get(ctx context.Context, path string) (interface{}, error) {
    target, actualPath := b.parsePath(path)

    b.mu.RLock()
    backend, exists := b.targets[target]
    b.mu.RUnlock()

    if !exists {
        return nil, &graft.BackendError{
            Backend: b.Name(),
            Target:  target,
            Message: fmt.Sprintf("unknown target: %s", target),
        }
    }

    return backend.Get(ctx, actualPath)
}

func (b *MultiTargetBackend) parsePath(path string) (target, actualPath string) {
    // Parse "target@path" syntax
    if idx := strings.Index(path, "@"); idx > 0 {
        return path[:idx], path[idx+1:]
    }
    return b.defaultTarget, path
}
```

**Usage:**

```yaml
prod_secret: (( mysecret "secret/db:password" ))
staging_secret: (( mysecret staging@"secret/db:password" ))
```

## Error Handling

### Backend Errors

```go
// Create descriptive backend errors
&graft.BackendError{
    Backend:    "redis",
    Target:     "production",
    Message:    "connection failed",
    Cause:      originalErr,
    RetryCount: 3,
}

// Handle specific error types
func (b *MyBackend) Get(ctx context.Context, path string) (interface{}, error) {
    val, err := b.client.Get(ctx, path)

    switch {
    case err == nil:
        return val, nil
    case errors.Is(err, ErrNotFound):
        return nil, &graft.NotFoundError{Path: path}
    case errors.Is(err, ErrTimeout):
        return nil, &graft.BackendError{
            Backend: b.Name(),
            Message: "request timeout",
            Cause:   err,
        }
    default:
        return nil, &graft.BackendError{
            Backend: b.Name(),
            Message: err.Error(),
            Cause:   err,
        }
    }
}
```

### Retry Logic

```go
type RetryingBackend struct {
    backend    Backend
    maxRetries int
    backoff    time.Duration
}

func (b *RetryingBackend) Get(ctx context.Context, path string) (interface{}, error) {
    var lastErr error

    for attempt := 0; attempt <= b.maxRetries; attempt++ {
        if attempt > 0 {
            select {
            case <-time.After(b.backoff * time.Duration(attempt)):
            case <-ctx.Done():
                return nil, ctx.Err()
            }
        }

        val, err := b.backend.Get(ctx, path)
        if err == nil {
            return val, nil
        }

        lastErr = err

        // Don't retry NotFoundErrors
        var notFound *graft.NotFoundError
        if errors.As(err, &notFound) {
            return nil, err
        }
    }

    return nil, &graft.BackendError{
        Backend:    b.backend.Name(),
        Message:    "max retries exceeded",
        Cause:      lastErr,
        RetryCount: b.maxRetries,
    }
}
```

## Health Checks

Implement meaningful health checks:

```go
func (b *RedisBackend) Health(ctx context.Context) error {
    // Simple ping
    if err := b.client.Ping(ctx).Err(); err != nil {
        return &graft.BackendError{
            Backend: "redis",
            Message: "health check failed: " + err.Error(),
            Cause:   err,
        }
    }

    return nil
}

// Comprehensive health check
func (b *VaultBackend) Health(ctx context.Context) error {
    // Check connection
    health, err := b.client.Sys().Health()
    if err != nil {
        return err
    }

    // Check seal status
    if health.Sealed {
        return &graft.BackendError{
            Backend: "vault",
            Message: "vault is sealed",
        }
    }

    // Check if initialized
    if !health.Initialized {
        return &graft.BackendError{
            Backend: "vault",
            Message: "vault is not initialized",
        }
    }

    return nil
}
```

## Testing Backends

### Unit Testing

```go
func TestRedisBackend_Get(t *testing.T) {
    // Use miniredis for testing
    mr, _ := miniredis.Run()
    defer mr.Close()

    mr.Set("config:database/host", "localhost")

    backend, _ := NewRedisBackend(mr.Addr(), "config:")

    val, err := backend.Get(context.Background(), "database/host")

    assert.NoError(t, err)
    assert.Equal(t, "localhost", val)
}
```

### Mock Backend

```go
type MockBackend struct {
    data map[string]interface{}
}

func NewMockBackend() *MockBackend {
    return &MockBackend{
        data: make(map[string]interface{}),
    }
}

func (b *MockBackend) Set(path string, value interface{}) {
    b.data[path] = value
}

func (b *MockBackend) Get(ctx context.Context, path string) (interface{}, error) {
    if val, ok := b.data[path]; ok {
        return val, nil
    }
    return nil, &graft.NotFoundError{Path: path}
}

// Use in tests
func TestWithMockBackend(t *testing.T) {
    mock := NewMockBackend()
    mock.Set("secret/db:password", "test-password")

    engine, _ := graft.NewEngine(
        graft.WithBackend(mock),
    )

    // Test code...
}
```

## Best Practices

### Do

- Implement connection pooling for performance

- Support batch operations when the underlying system allows

- Implement health checks for monitoring

- Use context for cancellation and timeouts

- Return structured errors with context

- Support multi-target for different environments

- Test with integration tests against real backends

### Don't

- Hold connections indefinitely without health checks

- Ignore context cancellation

- Return raw error messages (wrap with BackendError)

- Block without timeouts

- Assume network reliability

## Related Documentation

- [Custom Operators](custom-operators.md) - Creating operator for backends

- [Configuration Options](library-api/options.md) - Backend configuration

- [Secrets Management](../user-guide/secrets/index.md) - Using secret backends

# Web Service Embedding

This guide demonstrates how to embed Graft as a Go library in web services, enabling dynamic configuration management through APIs.

## Basic Library Usage

### Installation

```sh
go get github.com/fivetwenty-io/graft
```

### Simple Example

```go
package main

import (
    "fmt"
    "log"

    "github.com/fivetwenty-io/graft/pkg/graft"
)

func main() {
    // Create a new engine
    engine := graft.NewEngine()

    // Parse YAML
    doc, err := engine.ParseYAML([]byte(`
database:
  host: localhost
  port: 5432
  url: (( concat "postgres://" database.host ":" database.port ))
`))
    if err != nil {
        log.Fatal(err)
    }

    // Evaluate operators
    result, err := engine.Evaluate(doc)
    if err != nil {
        log.Fatal(err)
    }

    // Access values
    url, _ := result.GetString("database.url")
    fmt.Println("Database URL:", url)
    // Output: Database URL: postgres://localhost:5432

    // Output as YAML
    yaml, _ := result.ToYAML()
    fmt.Println(string(yaml))
}
```

### Parsing Files

```go
package main

import (
    "log"

    "github.com/fivetwenty-io/graft/pkg/graft"
)

func main() {
    engine := graft.NewEngine()

    // Parse a file
    doc, err := engine.ParseFile("config/base.yml")
    if err != nil {
        log.Fatal(err)
    }

    // Parse multiple file types
    yamlDoc, _ := engine.ParseFile("config.yml")
    jsonDoc, _ := engine.ParseFile("config.json")

    // Parse from bytes
    jsonBytes := []byte(`{"key": "value"}`)
    doc, _ = engine.ParseJSON(jsonBytes)
}
```

## Building a Configuration API

### Complete HTTP Service

```go
package main

import (
    "encoding/json"
    "fmt"
    "log"
    "net/http"
    "os"
    "sync"
    "time"

    "github.com/fivetwenty-io/graft/pkg/graft"
)

// ConfigService manages configuration for multiple environments
type ConfigService struct {
    engine   graft.Engine
    base     graft.Document
    envCache map[string]graft.Document
    cacheMu  sync.RWMutex
    cacheTTL time.Duration
}

// NewConfigService creates a new configuration service
func NewConfigService(basePath string) (*ConfigService, error) {
    engine := graft.NewEngine(
        graft.WithCacheSize(1000),
        graft.WithCacheTTL(5 * time.Minute),
    )

    base, err := engine.ParseFile(basePath)
    if err != nil {
        return nil, fmt.Errorf("failed to parse base config: %w", err)
    }

    return &ConfigService{
        engine:   engine,
        base:     base,
        envCache: make(map[string]graft.Document),
        cacheTTL: 5 * time.Minute,
    }, nil
}

// GetConfig returns merged configuration for an environment
func (s *ConfigService) GetConfig(env string) (graft.Document, error) {
    // Check cache first
    s.cacheMu.RLock()
    if cached, ok := s.envCache[env]; ok {
        s.cacheMu.RUnlock()
        return cached, nil
    }
    s.cacheMu.RUnlock()

    // Load environment overlay
    envPath := fmt.Sprintf("config/environments/%s.yml", env)
    overlay, err := s.engine.ParseFile(envPath)
    if err != nil {
        return nil, fmt.Errorf("failed to parse %s config: %w", env, err)
    }

    // Merge base with environment overlay
    result, err := s.engine.Merge().
        Base(s.base).
        Overlay(overlay).
        Execute()
    if err != nil {
        return nil, fmt.Errorf("failed to merge config: %w", err)
    }

    // Cache the result
    s.cacheMu.Lock()
    s.envCache[env] = result
    s.cacheMu.Unlock()

    return result, nil
}

// InvalidateCache clears the configuration cache
func (s *ConfigService) InvalidateCache() {
    s.cacheMu.Lock()
    s.envCache = make(map[string]graft.Document)
    s.cacheMu.Unlock()
}

// HTTP Handlers

func (s *ConfigService) handleGetConfig(w http.ResponseWriter, r *http.Request) {
    env := r.URL.Query().Get("env")
    if env == "" {
        env = "development"
    }

    format := r.URL.Query().Get("format")
    if format == "" {
        format = "yaml"
    }

    config, err := s.GetConfig(env)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    switch format {
    case "json":
        w.Header().Set("Content-Type", "application/json")
        data, _ := config.ToJSON()
        w.Write(data)
    case "yaml":
        w.Header().Set("Content-Type", "application/x-yaml")
        data, _ := config.ToYAML()
        w.Write(data)
    default:
        http.Error(w, "unsupported format", http.StatusBadRequest)
    }
}

func (s *ConfigService) handleGetValue(w http.ResponseWriter, r *http.Request) {
    env := r.URL.Query().Get("env")
    if env == "" {
        env = "development"
    }

    path := r.URL.Query().Get("path")
    if path == "" {
        http.Error(w, "path parameter required", http.StatusBadRequest)
        return
    }

    config, err := s.GetConfig(env)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    value, err := config.Get(path)
    if err != nil {
        http.Error(w, fmt.Sprintf("path not found: %s", path), http.StatusNotFound)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]interface{}{
        "path":  path,
        "value": value,
    })
}

func (s *ConfigService) handleInvalidateCache(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        return
    }

    s.InvalidateCache()
    w.WriteHeader(http.StatusOK)
    w.Write([]byte("cache invalidated"))
}

func main() {
    service, err := NewConfigService("config/base.yml")
    if err != nil {
        log.Fatal(err)
    }

    http.HandleFunc("/config", service.handleGetConfig)
    http.HandleFunc("/config/value", service.handleGetValue)
    http.HandleFunc("/config/invalidate", service.handleInvalidateCache)

    log.Println("Starting config service on :8080")
    log.Fatal(http.ListenAndServe(":8080", nil))
}
```

### API Usage Examples

```sh
# Get full configuration (YAML)
curl "http://localhost:8080/config?env=production"

# Get full configuration (JSON)
curl "http://localhost:8080/config?env=production&format=json"

# Get specific value
curl "http://localhost:8080/config/value?env=production&path=database.host"

# Invalidate cache
curl -X POST "http://localhost:8080/config/invalidate"
```

## Caching Strategies

### Time-Based Cache

```go
package main

import (
    "sync"
    "time"

    "github.com/fivetwenty-io/graft/pkg/graft"
)

type CachedConfig struct {
    doc       graft.Document
    expiresAt time.Time
}

type ConfigCache struct {
    engine  graft.Engine
    cache   map[string]CachedConfig
    mu      sync.RWMutex
    ttl     time.Duration
}

func NewConfigCache(ttl time.Duration) *ConfigCache {
    return &ConfigCache{
        engine: graft.NewEngine(),
        cache:  make(map[string]CachedConfig),
        ttl:    ttl,
    }
}

func (c *ConfigCache) Get(key string, loader func() (graft.Document, error)) (graft.Document, error) {
    c.mu.RLock()
    cached, ok := c.cache[key]
    c.mu.RUnlock()

    if ok && time.Now().Before(cached.expiresAt) {
        return cached.doc, nil
    }

    // Cache miss or expired
    doc, err := loader()
    if err != nil {
        return nil, err
    }

    c.mu.Lock()
    c.cache[key] = CachedConfig{
        doc:       doc,
        expiresAt: time.Now().Add(c.ttl),
    }
    c.mu.Unlock()

    return doc, nil
}

func (c *ConfigCache) Invalidate(key string) {
    c.mu.Lock()
    delete(c.cache, key)
    c.mu.Unlock()
}

func (c *ConfigCache) InvalidateAll() {
    c.mu.Lock()
    c.cache = make(map[string]CachedConfig)
    c.mu.Unlock()
}
```

### LRU Cache with Size Limit

```go
package main

import (
    "container/list"
    "sync"

    "github.com/fivetwenty-io/graft/pkg/graft"
)

type LRUConfigCache struct {
    engine   graft.Engine
    cache    map[string]*list.Element
    lru      *list.List
    mu       sync.RWMutex
    maxSize  int
}

type cacheEntry struct {
    key string
    doc graft.Document
}

func NewLRUConfigCache(maxSize int) *LRUConfigCache {
    return &LRUConfigCache{
        engine:  graft.NewEngine(),
        cache:   make(map[string]*list.Element),
        lru:     list.New(),
        maxSize: maxSize,
    }
}

func (c *LRUConfigCache) Get(key string) (graft.Document, bool) {
    c.mu.Lock()
    defer c.mu.Unlock()

    if elem, ok := c.cache[key]; ok {
        c.lru.MoveToFront(elem)
        return elem.Value.(*cacheEntry).doc, true
    }
    return nil, false
}

func (c *LRUConfigCache) Set(key string, doc graft.Document) {
    c.mu.Lock()
    defer c.mu.Unlock()

    if elem, ok := c.cache[key]; ok {
        c.lru.MoveToFront(elem)
        elem.Value.(*cacheEntry).doc = doc
        return
    }

    // Evict oldest if at capacity
    if c.lru.Len() >= c.maxSize {
        oldest := c.lru.Back()
        if oldest != nil {
            c.lru.Remove(oldest)
            delete(c.cache, oldest.Value.(*cacheEntry).key)
        }
    }

    entry := &cacheEntry{key: key, doc: doc}
    elem := c.lru.PushFront(entry)
    c.cache[key] = elem
}
```

## Custom Operator Registration

### Simple Custom Operator

```go
package main

import (
    "fmt"
    "os"

    "github.com/fivetwenty-io/graft/pkg/graft"
)

func main() {
    engine := graft.NewEngine()

    // Register a custom "env" operator
    engine.RegisterOperator("env", graft.OperatorFunc(
        func(ctx graft.EvalContext, args []interface{}) (interface{}, error) {
            if len(args) < 1 {
                return nil, fmt.Errorf("env requires at least 1 argument")
            }

            name, ok := args[0].(string)
            if !ok {
                return nil, fmt.Errorf("env argument must be a string")
            }

            value := os.Getenv(name)

            // Optional default value
            if value == "" && len(args) > 1 {
                return args[1], nil
            }

            return value, nil
        },
    ))

    // Use the custom operator
    doc, _ := engine.ParseYAML([]byte(`
api_key: (( env "API_KEY" "default-key" ))
home_dir: (( env "HOME" ))
`))

    result, _ := engine.Evaluate(doc)
    yaml, _ := result.ToYAML()
    fmt.Println(string(yaml))
}
```

### HTTP Fetch Operator

```go
package main

import (
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "time"

    "github.com/fivetwenty-io/graft/pkg/graft"
)

func main() {
    engine := graft.NewEngine()

    // Register HTTP fetch operator
    engine.RegisterOperator("http", graft.OperatorFunc(
        func(ctx graft.EvalContext, args []interface{}) (interface{}, error) {
            if len(args) < 1 {
                return nil, fmt.Errorf("http requires a URL argument")
            }

            url, ok := args[0].(string)
            if !ok {
                return nil, fmt.Errorf("http URL must be a string")
            }

            client := &http.Client{Timeout: 10 * time.Second}
            resp, err := client.Get(url)
            if err != nil {
                return nil, fmt.Errorf("http request failed: %w", err)
            }
            defer resp.Body.Close()

            body, err := io.ReadAll(resp.Body)
            if err != nil {
                return nil, fmt.Errorf("failed to read response: %w", err)
            }

            // Try to parse as JSON
            var result interface{}
            if err := json.Unmarshal(body, &result); err != nil {
                // Return as string if not JSON
                return string(body), nil
            }

            return result, nil
        },
    ))

    // Use the custom operator
    doc, _ := engine.ParseYAML([]byte(`
user_info: (( http "https://api.github.com/users/octocat" ))
`))

    result, _ := engine.Evaluate(doc)
    yaml, _ := result.ToYAML()
    fmt.Println(string(yaml))
}
```

### Stateful Operator with External Service

```go
package main

import (
    "database/sql"
    "fmt"

    "github.com/fivetwenty-io/graft/pkg/graft"
    _ "github.com/lib/pq"
)

type DBLookupOperator struct {
    db *sql.DB
}

func (op *DBLookupOperator) Evaluate(ctx graft.EvalContext, args []interface{}) (interface{}, error) {
    if len(args) < 2 {
        return nil, fmt.Errorf("dblookup requires table and key arguments")
    }

    table, ok := args[0].(string)
    if !ok {
        return nil, fmt.Errorf("table must be a string")
    }

    key, ok := args[1].(string)
    if !ok {
        return nil, fmt.Errorf("key must be a string")
    }

    var value string
    err := op.db.QueryRow(
        fmt.Sprintf("SELECT value FROM %s WHERE key = $1", table),
        key,
    ).Scan(&value)

    if err == sql.ErrNoRows {
        // Return default if provided
        if len(args) > 2 {
            return args[2], nil
        }
        return nil, fmt.Errorf("key not found: %s", key)
    }
    if err != nil {
        return nil, fmt.Errorf("database error: %w", err)
    }

    return value, nil
}

func main() {
    db, _ := sql.Open("postgres", "postgres://localhost/config?sslmode=disable")
    defer db.Close()

    engine := graft.NewEngine()
    engine.RegisterOperator("dblookup", &DBLookupOperator{db: db})

    doc, _ := engine.ParseYAML([]byte(`
database:
  password: (( dblookup "secrets" "db_password" "default-password" ))
api:
  key: (( dblookup "secrets" "api_key" ))
`))

    result, _ := engine.Evaluate(doc)
    yaml, _ := result.ToYAML()
    fmt.Println(string(yaml))
}
```

## Testing with Mocks

### Mock Engine for Testing

```go
package main

import (
    "testing"

    "github.com/fivetwenty-io/graft/pkg/graft"
    "github.com/stretchr/testify/assert"
)

func TestConfigGeneration(t *testing.T) {
    // Create mock engine
    engine := graft.NewMockEngine()

    // Pre-configure Vault responses
    engine.MockVault("secret/database:password", "test-db-password")
    engine.MockVault("secret/api:key", "test-api-key")

    // Pre-configure AWS Parameter Store responses
    engine.MockAWSParam("/app/config/host", "test-host.example.com")

    // Parse and evaluate configuration
    doc, err := engine.ParseYAML([]byte(`
database:
  host: localhost
  password: (( vault "secret/database:password" ))
api:
  key: (( vault "secret/api:key" ))
  endpoint: (( awsparam "/app/config/host" ))
`))
    assert.NoError(t, err)

    result, err := engine.Evaluate(doc)
    assert.NoError(t, err)

    // Verify results
    password, _ := result.GetString("database.password")
    assert.Equal(t, "test-db-password", password)

    apiKey, _ := result.GetString("api.key")
    assert.Equal(t, "test-api-key", apiKey)

    endpoint, _ := result.GetString("api.endpoint")
    assert.Equal(t, "test-host.example.com", endpoint)
}

func TestMergeWithMocks(t *testing.T) {
    engine := graft.NewMockEngine()

    base, _ := engine.ParseYAML([]byte(`
database:
  host: localhost
  port: 5432
`))

    overlay, _ := engine.ParseYAML([]byte(`
database:
  host: (( vault "secret/db:host" || "fallback-host" ))
`))

    // Mock vault to fail (to test fallback)
    engine.MockVaultError("secret/db:host", fmt.Errorf("secret not found"))

    result, err := engine.Merge().
        Base(base).
        Overlay(overlay).
        Execute()

    assert.NoError(t, err)

    host, _ := result.GetString("database.host")
    assert.Equal(t, "fallback-host", host)
}
```

### Table-Driven Tests

```go
package main

import (
    "testing"

    "github.com/fivetwenty-io/graft/pkg/graft"
    "github.com/stretchr/testify/assert"
)

func TestOperatorEvaluation(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected map[string]interface{}
    }{
        {
            name: "concat operator",
            input: `
url: (( concat "https://" host ":" port ))
host: example.com
port: 443
`,
            expected: map[string]interface{}{
                "url": "https://example.com:443",
            },
        },
        {
            name: "grab with default",
            input: `
value: (( grab missing || "default" ))
`,
            expected: map[string]interface{}{
                "value": "default",
            },
        },
        {
            name: "arithmetic",
            input: `
base: 10
doubled: (( grab base * 2 ))
`,
            expected: map[string]interface{}{
                "doubled": 20,
            },
        },
    }

    engine := graft.NewEngine()

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            doc, err := engine.ParseYAML([]byte(tt.input))
            assert.NoError(t, err)

            result, err := engine.Evaluate(doc)
            assert.NoError(t, err)

            for path, expected := range tt.expected {
                actual, err := result.Get(path)
                assert.NoError(t, err)
                assert.Equal(t, expected, actual)
            }
        })
    }
}
```

## Complete Web Service Example

### Full-Featured Configuration Server

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "log"
    "net/http"
    "os"
    "os/signal"
    "sync"
    "syscall"
    "time"

    "github.com/fivetwenty-io/graft/pkg/graft"
)

// Server represents the configuration server
type Server struct {
    engine     graft.Engine
    baseConfig graft.Document
    cache      *ConfigCache
    server     *http.Server
}

// ConfigCache provides thread-safe caching
type ConfigCache struct {
    mu      sync.RWMutex
    entries map[string]*CacheEntry
    ttl     time.Duration
}

type CacheEntry struct {
    doc       graft.Document
    createdAt time.Time
}

func NewConfigCache(ttl time.Duration) *ConfigCache {
    return &ConfigCache{
        entries: make(map[string]*CacheEntry),
        ttl:     ttl,
    }
}

func (c *ConfigCache) Get(key string) (graft.Document, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()

    entry, ok := c.entries[key]
    if !ok {
        return nil, false
    }

    if time.Since(entry.createdAt) > c.ttl {
        return nil, false
    }

    return entry.doc, true
}

func (c *ConfigCache) Set(key string, doc graft.Document) {
    c.mu.Lock()
    defer c.mu.Unlock()

    c.entries[key] = &CacheEntry{
        doc:       doc,
        createdAt: time.Now(),
    }
}

func (c *ConfigCache) Clear() {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.entries = make(map[string]*CacheEntry)
}

// NewServer creates a new configuration server
func NewServer(baseConfigPath string) (*Server, error) {
    // Initialize engine with Vault support
    engine := graft.NewEngine(
        graft.WithVault(graft.VaultConfig{
            Address: os.Getenv("VAULT_ADDR"),
            Token:   os.Getenv("VAULT_TOKEN"),
        }),
        graft.WithCacheSize(500),
        graft.WithCacheTTL(5 * time.Minute),
    )

    // Load base configuration
    baseConfig, err := engine.ParseFile(baseConfigPath)
    if err != nil {
        return nil, fmt.Errorf("failed to load base config: %w", err)
    }

    return &Server{
        engine:     engine,
        baseConfig: baseConfig,
        cache:      NewConfigCache(5 * time.Minute),
    }, nil
}

// GetConfig returns merged configuration for an environment
func (s *Server) GetConfig(env string) (graft.Document, error) {
    // Check cache
    if cached, ok := s.cache.Get(env); ok {
        return cached, nil
    }

    // Load environment overlay
    envPath := fmt.Sprintf("config/environments/%s.yml", env)
    overlay, err := s.engine.ParseFile(envPath)
    if err != nil {
        return nil, fmt.Errorf("failed to load environment config: %w", err)
    }

    // Merge configurations
    result, err := s.engine.Merge().
        Base(s.baseConfig).
        Overlay(overlay).
        Execute()
    if err != nil {
        return nil, fmt.Errorf("failed to merge configs: %w", err)
    }

    // Cache result
    s.cache.Set(env, result)

    return result, nil
}

// HTTP Handlers

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
    json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
    env := r.URL.Query().Get("env")
    if env == "" {
        env = "development"
    }

    config, err := s.GetConfig(env)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    format := r.URL.Query().Get("format")
    switch format {
    case "json":
        w.Header().Set("Content-Type", "application/json")
        data, _ := config.ToJSON()
        w.Write(data)
    default:
        w.Header().Set("Content-Type", "application/x-yaml")
        data, _ := config.ToYAML()
        w.Write(data)
    }
}

func (s *Server) handleValue(w http.ResponseWriter, r *http.Request) {
    env := r.URL.Query().Get("env")
    if env == "" {
        env = "development"
    }

    path := r.URL.Query().Get("path")
    if path == "" {
        http.Error(w, "path parameter required", http.StatusBadRequest)
        return
    }

    config, err := s.GetConfig(env)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    value, err := config.Get(path)
    if err != nil {
        http.Error(w, fmt.Sprintf("path not found: %s", path), http.StatusNotFound)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]interface{}{
        "environment": env,
        "path":        path,
        "value":       value,
    })
}

func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
    env1 := r.URL.Query().Get("env1")
    env2 := r.URL.Query().Get("env2")

    if env1 == "" || env2 == "" {
        http.Error(w, "env1 and env2 parameters required", http.StatusBadRequest)
        return
    }

    config1, err := s.GetConfig(env1)
    if err != nil {
        http.Error(w, fmt.Sprintf("failed to get %s config: %v", env1, err), http.StatusInternalServerError)
        return
    }

    config2, err := s.GetConfig(env2)
    if err != nil {
        http.Error(w, fmt.Sprintf("failed to get %s config: %v", env2, err), http.StatusInternalServerError)
        return
    }

    diff := s.engine.Diff(config1, config2)

    w.Header().Set("Content-Type", "application/json")
    data, _ := diff.ToJSON()
    w.Write(data)
}

func (s *Server) handleInvalidate(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        return
    }

    s.cache.Clear()
    w.WriteHeader(http.StatusOK)
    json.NewEncoder(w).Encode(map[string]string{"status": "cache invalidated"})
}

// Start starts the HTTP server
func (s *Server) Start(addr string) error {
    mux := http.NewServeMux()
    mux.HandleFunc("/health", s.handleHealth)
    mux.HandleFunc("/config", s.handleConfig)
    mux.HandleFunc("/config/value", s.handleValue)
    mux.HandleFunc("/config/diff", s.handleDiff)
    mux.HandleFunc("/config/invalidate", s.handleInvalidate)

    s.server = &http.Server{
        Addr:         addr,
        Handler:      mux,
        ReadTimeout:  10 * time.Second,
        WriteTimeout: 10 * time.Second,
    }

    log.Printf("Starting configuration server on %s", addr)
    return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
    return s.server.Shutdown(ctx)
}

func main() {
    server, err := NewServer("config/base.yml")
    if err != nil {
        log.Fatalf("Failed to create server: %v", err)
    }

    // Handle graceful shutdown
    go func() {
        sigChan := make(chan os.Signal, 1)
        signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
        <-sigChan

        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()

        if err := server.Shutdown(ctx); err != nil {
            log.Printf("Shutdown error: %v", err)
        }
    }()

    if err := server.Start(":8080"); err != http.ErrServerClosed {
        log.Fatalf("Server error: %v", err)
    }
}
```

### API Endpoints

| Endpoint | Method | Parameters | Description |
|----------|--------|------------|-------------|
| `/health` | GET | - | Health check |
| `/config` | GET | `env`, `format` | Get full configuration |
| `/config/value` | GET | `env`, `path` | Get specific value |
| `/config/diff` | GET | `env1`, `env2` | Compare environments |
| `/config/invalidate` | POST | - | Clear cache |

### Usage Examples

```sh
# Get configuration
curl "http://localhost:8080/config?env=production&format=json"

# Get specific value
curl "http://localhost:8080/config/value?env=production&path=database.host"

# Compare environments
curl "http://localhost:8080/config/diff?env1=staging&env2=production"

# Invalidate cache
curl -X POST "http://localhost:8080/config/invalidate"
```

## See Also

- [Graft Library API](../user-guide/library/) - Complete library documentation
- [Basic Merging](basic-merging.md) - Core merge concepts
- [Secrets Management](secrets-management.md) - Backend integration

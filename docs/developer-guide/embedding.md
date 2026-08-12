# Embedding Graft in Applications

Graft is designed as an embeddable Go library for configuration management. This guide covers patterns and best practices for integrating Graft into your applications.

## Basic Integration

### Simple Configuration Loader

```go
package config

import (
    "context"
    "os"

    "github.com/fivetwenty-io/graft/pkg/graft"
)

type ConfigLoader struct {
    engine graft.Engine
}

func NewConfigLoader() (*ConfigLoader, error) {
    engine, err := graft.NewEngine()
    if err != nil {
        return nil, err
    }

    return &ConfigLoader{engine: engine}, nil
}

func (l *ConfigLoader) Load(path string) (graft.Document, error) {
    return l.engine.ParseFile(path)
}

func (l *ConfigLoader) LoadAndMerge(paths ...string) (graft.Document, error) {
    if len(paths) == 0 {
        return nil, fmt.Errorf("no paths provided")
    }

    docs := make([]graft.Document, len(paths))
    for i, path := range paths {
        doc, err := l.engine.ParseFile(path)
        if err != nil {
            return nil, fmt.Errorf("failed to parse %s: %w", path, err)
        }
        docs[i] = doc
    }

    return l.engine.Merge(context.Background(), docs...).Execute()
}
```

## Web Service Integration

### Configuration Service

```go
package service

import (
    "context"
    "sync"
    "time"

    "github.com/fivetwenty-io/graft/pkg/graft"
)

type ConfigService struct {
    engine     graft.Engine
    baseConfig graft.Document
    cache      map[string]cachedConfig
    cacheMu    sync.RWMutex
    cacheTTL   time.Duration
}

type cachedConfig struct {
    doc       graft.Document
    expiresAt time.Time
}

func NewConfigService(basePath string) (*ConfigService, error) {
    engine, err := graft.NewEngine(
        graft.WithVault(graft.VaultConfig{
            Address: os.Getenv("VAULT_ADDR"),
            Token:   os.Getenv("VAULT_TOKEN"),
        }),
        graft.WithCacheSize(500),
        graft.WithCacheTTL(5 * time.Minute),
    )
    if err != nil {
        return nil, err
    }

    base, err := engine.ParseFile(basePath)
    if err != nil {
        return nil, err
    }

    return &ConfigService{
        engine:     engine,
        baseConfig: base,
        cache:      make(map[string]cachedConfig),
        cacheTTL:   5 * time.Minute,
    }, nil
}

func (s *ConfigService) GetConfig(ctx context.Context, env string) ([]byte, error) {
    // Check cache
    s.cacheMu.RLock()
    if cached, ok := s.cache[env]; ok && time.Now().Before(cached.expiresAt) {
        s.cacheMu.RUnlock()
        return cached.doc.ToJSON()
    }
    s.cacheMu.RUnlock()

    // Load environment overlay
    overlay, err := s.engine.ParseFile(fmt.Sprintf("config/env/%s.yml", env))
    if err != nil {
        return nil, err
    }

    // Merge
    result, err := s.engine.Merge(ctx, s.baseConfig, overlay).Execute()
    if err != nil {
        return nil, err
    }

    // Cache result
    s.cacheMu.Lock()
    s.cache[env] = cachedConfig{
        doc:       result,
        expiresAt: time.Now().Add(s.cacheTTL),
    }
    s.cacheMu.Unlock()

    return result.ToJSON()
}

func (s *ConfigService) InvalidateCache(env string) {
    s.cacheMu.Lock()
    delete(s.cache, env)
    s.cacheMu.Unlock()
}
```

### HTTP Handler

```go
package api

import (
    "encoding/json"
    "net/http"
)

type ConfigHandler struct {
    service *ConfigService
}

func (h *ConfigHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
    env := r.URL.Query().Get("env")
    if env == "" {
        env = "default"
    }

    config, err := h.service.GetConfig(r.Context(), env)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    w.Write(config)
}

func (h *ConfigHandler) GetValue(w http.ResponseWriter, r *http.Request) {
    env := r.URL.Query().Get("env")
    path := r.URL.Query().Get("path")

    configJSON, err := h.service.GetConfig(r.Context(), env)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    // Parse and extract value
    engine, _ := graft.NewEngine()
    doc, _ := engine.ParseJSON(configJSON)

    value, err := doc.Get(path)
    if err != nil {
        http.Error(w, "path not found", http.StatusNotFound)
        return
    }

    json.NewEncoder(w).Encode(value)
}
```

## CLI Tool Integration

### Building a Config Tool

```go
package main

import (
    "context"
    "flag"
    "fmt"
    "os"

    "github.com/fivetwenty-io/graft/pkg/graft"
)

func main() {
    var (
        output = flag.String("output", "yaml", "Output format (yaml|json)")
        env    = flag.String("env", "", "Environment overlay")
        prune  = flag.String("prune", "", "Keys to prune (comma-separated)")
    )
    flag.Parse()

    files := flag.Args()
    if len(files) == 0 {
        fmt.Fprintln(os.Stderr, "Usage: configtool [options] file1.yml [file2.yml ...]")
        os.Exit(1)
    }

    engine, _ := graft.NewEngine(
        graft.WithVault(graft.VaultConfig{
            Address: os.Getenv("VAULT_ADDR"),
            Token:   os.Getenv("VAULT_TOKEN"),
        }),
    )

    // Parse all files
    docs := make([]graft.Document, 0, len(files))
    for _, f := range files {
        doc, err := engine.ParseFile(f)
        if err != nil {
            fmt.Fprintf(os.Stderr, "Error parsing %s: %v\n", f, err)
            os.Exit(1)
        }
        docs = append(docs, doc)
    }

    // Add environment overlay
    if *env != "" {
        envDoc, err := engine.ParseFile(fmt.Sprintf("env/%s.yml", *env))
        if err != nil {
            fmt.Fprintf(os.Stderr, "Error loading env: %v\n", err)
            os.Exit(1)
        }
        docs = append(docs, envDoc)
    }

    // Build merge
    builder := engine.Merge(context.Background(), docs...)

    if *prune != "" {
        keys := strings.Split(*prune, ",")
        builder = builder.WithPrune(keys...)
    }

    // Execute
    result, err := builder.Execute()
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error: %v\n", err)
        os.Exit(1)
    }

    // Output
    var out []byte
    switch *output {
    case "json":
        out, _ = result.ToJSONIndent("", "  ")
    default:
        out, _ = result.ToYAML()
    }

    fmt.Print(string(out))
}
```

## Kubernetes Operator Integration

### Config Controller

```go
package controller

import (
    "context"

    "github.com/fivetwenty-io/graft/pkg/graft"
    "sigs.k8s.io/controller-runtime/pkg/client"
)

type ConfigController struct {
    client client.Client
    engine graft.Engine
}

func NewConfigController(client client.Client) (*ConfigController, error) {
    engine, err := graft.NewEngine(
        graft.WithVault(graft.VaultConfig{
            Address: os.Getenv("VAULT_ADDR"),
        }),
        graft.WithHistoryTracking(true),
    )
    if err != nil {
        return nil, err
    }

    return &ConfigController{
        client: client,
        engine: engine,
    }, nil
}

func (c *ConfigController) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
    // Get ConfigTemplate CR
    var template configv1.ConfigTemplate
    if err := c.client.Get(ctx, req.NamespacedName, &template); err != nil {
        return reconcile.Result{}, client.IgnoreNotFound(err)
    }

    // Load base config from ConfigMap
    base, err := c.loadConfigMap(ctx, template.Spec.BaseConfigMap)
    if err != nil {
        return reconcile.Result{}, err
    }

    // Load overlays
    overlays := make([]graft.Document, len(template.Spec.Overlays))
    for i, overlay := range template.Spec.Overlays {
        doc, err := c.loadConfigMap(ctx, overlay)
        if err != nil {
            return reconcile.Result{}, err
        }
        overlays[i] = doc
    }

    // Merge
    docs := append([]graft.Document{base}, overlays...)
    result, err := c.engine.Merge(ctx, docs...).
        WithPrune(template.Spec.Prune...).
        Execute()
    if err != nil {
        return reconcile.Result{}, err
    }

    // Create/Update output ConfigMap
    output := &corev1.ConfigMap{
        ObjectMeta: metav1.ObjectMeta{
            Name:      template.Spec.OutputConfigMap,
            Namespace: req.Namespace,
        },
        Data: map[string]string{
            "config.yaml": string(mustYAML(result)),
        },
    }

    if err := c.client.Patch(ctx, output, client.Apply); err != nil {
        return reconcile.Result{}, err
    }

    return reconcile.Result{}, nil
}

func (c *ConfigController) loadConfigMap(ctx context.Context, name string) (graft.Document, error) {
    var cm corev1.ConfigMap
    if err := c.client.Get(ctx, client.ObjectKey{Name: name}, &cm); err != nil {
        return nil, err
    }

    if data, ok := cm.Data["config.yaml"]; ok {
        return c.engine.ParseYAML([]byte(data))
    }

    return nil, fmt.Errorf("config.yaml not found in ConfigMap %s", name)
}
```

## Multi-tenant Configuration

### Tenant Config Manager

```go
package tenant

import (
    "context"
    "sync"

    "github.com/fivetwenty-io/graft/pkg/graft"
)

type TenantConfigManager struct {
    engine     graft.Engine
    baseConfig graft.Document
    tenants    map[string]graft.Document
    mu         sync.RWMutex
}

func NewTenantConfigManager(basePath string) (*TenantConfigManager, error) {
    engine, _ := graft.NewEngine(
        graft.WithHistoryTracking(true),
    )

    base, err := engine.ParseFile(basePath)
    if err != nil {
        return nil, err
    }

    return &TenantConfigManager{
        engine:     engine,
        baseConfig: base,
        tenants:    make(map[string]graft.Document),
    }, nil
}

func (m *TenantConfigManager) LoadTenant(ctx context.Context, tenantID string, overlayPath string) error {
    overlay, err := m.engine.ParseFile(overlayPath)
    if err != nil {
        return err
    }

    config, err := m.engine.Merge(ctx, m.baseConfig, overlay).Execute()
    if err != nil {
        return err
    }

    m.mu.Lock()
    m.tenants[tenantID] = config
    m.mu.Unlock()

    return nil
}

func (m *TenantConfigManager) GetTenantConfig(tenantID string) (graft.Document, error) {
    m.mu.RLock()
    defer m.mu.RUnlock()

    config, ok := m.tenants[tenantID]
    if !ok {
        return nil, fmt.Errorf("tenant %s not found", tenantID)
    }

    return config.Clone(), nil // Return clone for thread safety
}

func (m *TenantConfigManager) GetValue(tenantID, path string) (interface{}, error) {
    config, err := m.GetTenantConfig(tenantID)
    if err != nil {
        return nil, err
    }

    return config.Get(path)
}
```

## Configuration Validation

### Validation Pipeline

Post-processors run on the merge path (`Merge`/`MergeFiles`/`MergeReaders`'s `Execute()`), not on a bare `Evaluate()` call - calling `Evaluate` directly skips them. Graft has no built-in schema-validation post-processor (see [Custom Post-Processors](custom-post-processors.md#not-provided)); the example below checks required fields with a small custom `graft.PostProcessor` and redacts secrets with the built-in `graft.NewSecurityRedactor`:

```go
package validation

import (
    "context"
    "fmt"

    "github.com/fivetwenty-io/graft/pkg/graft"
)

type ConfigValidator struct {
    engine graft.Engine
}

func NewConfigValidator() *ConfigValidator {
    engine, _ := graft.NewEngine(
        graft.WithPostProcessors(
            &requiredFieldsChecker{fields: []string{
                "app.name",
                "app.version",
                "database.host",
            }},
            graft.NewSecurityRedactor([]string{"password", "secret", "key"}, ""),
        ),
    )

    return &ConfigValidator{engine: engine}
}

func (v *ConfigValidator) Validate(ctx context.Context, configPath string) error {
    doc, err := v.engine.ParseFile(configPath)
    if err != nil {
        return fmt.Errorf("parse error: %w", err)
    }

    if _, err := v.engine.Merge(ctx, doc).Execute(); err != nil {
        return fmt.Errorf("validation error: %w", err)
    }

    return nil
}

func (v *ConfigValidator) ValidateAndMerge(ctx context.Context, paths ...string) (graft.Document, error) {
    docs := make([]graft.Document, len(paths))
    for i, path := range paths {
        doc, err := v.engine.ParseFile(path)
        if err != nil {
            return nil, err
        }
        docs[i] = doc
    }

    return v.engine.Merge(ctx, docs...).Execute()
}

// requiredFieldsChecker is a minimal graft.PostProcessor. See
// [Custom Post-Processors](custom-post-processors.md) for the full
// pattern, including a distinguishable error type and priority ordering.
type requiredFieldsChecker struct {
    fields []string
}

func (c *requiredFieldsChecker) Name() string { return "required-fields" }

func (c *requiredFieldsChecker) Phase() graft.PostProcessPhase { return graft.PhaseEarly }

func (c *requiredFieldsChecker) Process(
    _ context.Context,
    doc graft.Document,
    _ *graft.ProcessMetadata,
) (graft.Document, error) {
    for _, field := range c.fields {
        if !doc.Has(field) {
            return nil, fmt.Errorf("missing required field: %s", field)
        }
    }
    return doc, nil
}
```

## Hot Reloading

### File Watcher Integration

```go
package config

import (
    "context"
    "sync"

    "github.com/fivetwenty-io/graft/pkg/graft"
    "github.com/fsnotify/fsnotify"
)

type ReloadableConfig struct {
    engine   graft.Engine
    paths    []string
    config   graft.Document
    mu       sync.RWMutex
    onChange func(graft.Document)
}

func NewReloadableConfig(paths ...string) (*ReloadableConfig, error) {
    engine, _ := graft.NewEngine()

    rc := &ReloadableConfig{
        engine: engine,
        paths:  paths,
    }

    if err := rc.reload(); err != nil {
        return nil, err
    }

    go rc.watch()

    return rc, nil
}

func (rc *ReloadableConfig) reload() error {
    docs := make([]graft.Document, len(rc.paths))
    for i, path := range rc.paths {
        doc, err := rc.engine.ParseFile(path)
        if err != nil {
            return err
        }
        docs[i] = doc
    }

    config, err := rc.engine.Merge(context.Background(), docs...).Execute()
    if err != nil {
        return err
    }

    rc.mu.Lock()
    rc.config = config
    rc.mu.Unlock()

    if rc.onChange != nil {
        rc.onChange(config)
    }

    return nil
}

func (rc *ReloadableConfig) watch() {
    watcher, _ := fsnotify.NewWatcher()
    defer watcher.Close()

    for _, path := range rc.paths {
        watcher.Add(path)
    }

    for {
        select {
        case event := <-watcher.Events:
            if event.Op&fsnotify.Write == fsnotify.Write {
                rc.reload()
            }
        case <-watcher.Errors:
            // Handle error
        }
    }
}

func (rc *ReloadableConfig) Get() graft.Document {
    rc.mu.RLock()
    defer rc.mu.RUnlock()
    return rc.config.Clone()
}

func (rc *ReloadableConfig) OnChange(fn func(graft.Document)) {
    rc.onChange = fn
}
```

## Thread Safety Patterns

### Safe Document Access

```go
// Always clone for concurrent use
func (s *Service) ProcessConfig(config graft.Document) {
    // Clone before passing to goroutines
    clone := config.Clone()

    go func() {
        // Safe to read/modify clone
        value := clone.String("some.path")
        clone.Set("processed", true)
    }()
}

// Use sync.Map for config cache
type ConfigCache struct {
    engine graft.Engine
    cache  sync.Map
}

func (c *ConfigCache) Get(key string) (graft.Document, error) {
    if val, ok := c.cache.Load(key); ok {
        // Return clone for safety
        return val.(graft.Document).Clone(), nil
    }
    return nil, ErrNotFound
}
```

## Error Handling Patterns

### Graceful Degradation

```go
func (s *ConfigService) GetConfigWithFallback(ctx context.Context, env string) graft.Document {
    // Try to load full config
    config, err := s.loadConfig(ctx, env)
    if err != nil {
        log.Printf("Failed to load config for %s: %v, using defaults", env, err)
        return s.defaultConfig.Clone()
    }

    return config
}

// Validate and fall back to previous version
func (s *ConfigService) ValidateAndUpdate(ctx context.Context, newConfigPath string) error {
    newConfig, err := s.engine.ParseFile(newConfigPath)
    if err != nil {
        return fmt.Errorf("parse error: %w", err)
    }

    // Validate
    if err := s.validator.Validate(ctx, newConfig); err != nil {
        return fmt.Errorf("validation error: %w", err)
    }

    // Atomically update
    s.mu.Lock()
    s.config = newConfig
    s.mu.Unlock()

    return nil
}
```

## Best Practices

### Do

- Create engine once, reuse for all operations

- Clone documents before passing to goroutines

- Use functional options for configuration

- Implement health checks for backends

- Handle errors gracefully with fallbacks

- Use context for cancellation

### Don't

- Create new engine for each request

- Share document instances across goroutines

- Ignore context cancellation

- Hardcode configuration paths

- Block indefinitely on backend calls

## Related Documentation

- [Library API Overview](library-api/index.md) - Core API reference

- [Configuration Options](library-api/options.md) - Engine configuration

- [Custom Backends](custom-backends.md) - Backend integration

- [Testing Guide](testing.md) - Testing embedded applications

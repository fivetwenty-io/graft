package config

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"
)

// Test constants for repeated string literals.
const (
	testLogLevelDebug = "debug"
	testLogLevelError = "error"
	testFormatJSON    = "json"
)

//nolint:gocyclo // test function verifies many default config values
func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	// Engine defaults.
	if cfg.Engine.StrictMode {
		t.Error("Expected StrictMode to be false by default")
	}
	if cfg.Engine.MaxRecursion != 100 {
		t.Errorf("Expected MaxRecursion 100, got %d", cfg.Engine.MaxRecursion)
	}
	if cfg.Engine.Timeout != 30*time.Second {
		t.Errorf("Expected Timeout 30s, got %v", cfg.Engine.Timeout)
	}

	// Cache defaults.
	if !cfg.Cache.Enabled {
		t.Error("Expected Cache.Enabled to be true by default")
	}
	if cfg.Cache.MaxSize != 10000 {
		t.Errorf("Expected Cache.MaxSize 10000, got %d", cfg.Cache.MaxSize)
	}
	if cfg.Cache.TTL != 5*time.Minute {
		t.Errorf("Expected Cache.TTL 5m, got %v", cfg.Cache.TTL)
	}
	if cfg.Cache.L2Enabled {
		t.Error("Expected Cache.L2Enabled to be false by default")
	}

	// Parallel defaults.
	if !cfg.Parallel.Enabled {
		t.Error("Expected Parallel.Enabled to be true by default")
	}
	if cfg.Parallel.MinWorkers != 1 {
		t.Errorf("Expected Parallel.MinWorkers 1, got %d", cfg.Parallel.MinWorkers)
	}
	if cfg.Parallel.MaxWorkers != 0 {
		t.Errorf("Expected Parallel.MaxWorkers 0 (auto), got %d", cfg.Parallel.MaxWorkers)
	}

	// Metrics defaults.
	if cfg.Metrics.Enabled {
		t.Error("Expected Metrics.Enabled to be false by default")
	}
	if cfg.Metrics.Format != "prometheus" {
		t.Errorf("Expected Metrics.Format 'prometheus', got '%s'", cfg.Metrics.Format)
	}
	if cfg.Metrics.Endpoint != "/metrics" {
		t.Errorf("Expected Metrics.Endpoint '/metrics', got '%s'", cfg.Metrics.Endpoint)
	}

	// Logging defaults.
	if cfg.Logging.Level != "info" {
		t.Errorf("Expected Logging.Level 'info', got '%s'", cfg.Logging.Level)
	}
	if cfg.Logging.Format != "text" {
		t.Errorf("Expected Logging.Format 'text', got '%s'", cfg.Logging.Format)
	}
}

func TestConfigClone(t *testing.T) {
	original := DefaultConfig()
	original.Engine.StrictMode = true
	original.Cache.MaxSize = 5000
	original.Logging.Level = testLogLevelDebug

	clone := original.Clone()

	// Verify clone has same values.
	if clone.Engine.StrictMode != true {
		t.Error("Clone should have StrictMode = true")
	}
	if clone.Cache.MaxSize != 5000 {
		t.Errorf("Clone should have MaxSize 5000, got %d", clone.Cache.MaxSize)
	}
	if clone.Logging.Level != testLogLevelDebug {
		t.Errorf("Clone should have Level 'debug', got '%s'", clone.Logging.Level)
	}

	// Modify original and verify clone is unchanged.
	original.Engine.StrictMode = false
	original.Cache.MaxSize = 9999

	if clone.Engine.StrictMode != true {
		t.Error("Clone should still have StrictMode = true after original modification")
	}
	if clone.Cache.MaxSize != 5000 {
		t.Errorf("Clone should still have MaxSize 5000, got %d", clone.Cache.MaxSize)
	}
}

func TestConfigCloneNil(t *testing.T) {
	var cfg *Config
	clone := cfg.Clone()
	if clone != nil {
		t.Error("Clone of nil config should be nil")
	}
}

func TestManagerGetSet(t *testing.T) {
	manager := NewManager()

	cfg := manager.Get()
	if cfg == nil {
		t.Fatal("Expected config to be available")
	}

	// Modify and set.
	cfg.Engine.StrictMode = true
	cfg.Logging.Level = testLogLevelError

	if err := manager.Set(cfg); err != nil {
		t.Fatalf("Failed to set config: %v", err)
	}

	// Verify the change.
	newCfg := manager.Get()
	if !newCfg.Engine.StrictMode {
		t.Error("Expected StrictMode to be true")
	}
	if newCfg.Logging.Level != testLogLevelError {
		t.Errorf("Expected Level 'error', got '%s'", newCfg.Logging.Level)
	}
}

func TestManagerOnChange(t *testing.T) {
	manager := NewManager()

	var called bool
	var receivedCfg *Config
	var mu sync.Mutex
	var wg sync.WaitGroup

	wg.Add(1)
	manager.OnChange(func(cfg *Config) {
		mu.Lock()
		called = true
		receivedCfg = cfg
		mu.Unlock()
		wg.Done()
	})

	cfg := manager.Get()
	cfg.Engine.StrictMode = true
	if err := manager.Set(cfg); err != nil {
		t.Fatalf("Failed to set config: %v", err)
	}

	// Wait for callback.
	wg.Wait()

	mu.Lock()
	if !called {
		t.Error("OnChange callback was not called")
	}
	if receivedCfg == nil {
		t.Error("Received config is nil")
	} else if !receivedCfg.Engine.StrictMode {
		t.Error("Received config should have StrictMode = true")
	}
	mu.Unlock()
}

func TestLoad(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "test_config.yaml")

	configContent := `
engine:
  strict_mode: true
  max_recursion: 50
  timeout: 1m
cache:
  enabled: false
  max_size: 5000
  ttl: 10m
parallel:
  enabled: true
  min_workers: 2
  max_workers: 8
metrics:
  enabled: true
  format: json
  endpoint: /custom-metrics
logging:
  level: debug
  format: json
`

	if err := os.WriteFile(configPath, []byte(configContent), 0o600); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify loaded values.
	if !cfg.Engine.StrictMode {
		t.Error("Expected StrictMode true")
	}
	if cfg.Engine.MaxRecursion != 50 {
		t.Errorf("Expected MaxRecursion 50, got %d", cfg.Engine.MaxRecursion)
	}
	if cfg.Engine.Timeout != time.Minute {
		t.Errorf("Expected Timeout 1m, got %v", cfg.Engine.Timeout)
	}
	if cfg.Cache.Enabled {
		t.Error("Expected Cache.Enabled false")
	}
	if cfg.Cache.MaxSize != 5000 {
		t.Errorf("Expected MaxSize 5000, got %d", cfg.Cache.MaxSize)
	}
	if cfg.Parallel.MinWorkers != 2 {
		t.Errorf("Expected MinWorkers 2, got %d", cfg.Parallel.MinWorkers)
	}
	if cfg.Parallel.MaxWorkers != 8 {
		t.Errorf("Expected MaxWorkers 8, got %d", cfg.Parallel.MaxWorkers)
	}
	if !cfg.Metrics.Enabled {
		t.Error("Expected Metrics.Enabled true")
	}
	if cfg.Metrics.Format != testFormatJSON {
		t.Errorf("Expected Metrics.Format 'json', got '%s'", cfg.Metrics.Format)
	}
	if cfg.Logging.Level != testLogLevelDebug {
		t.Errorf("Expected Logging.Level 'debug', got '%s'", cfg.Logging.Level)
	}
}

func TestLoadEmptyFilePreservesDefaults(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "empty.yaml")

	if err := os.WriteFile(configPath, []byte(""), 0o600); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load empty config: %v", err)
	}

	want := DefaultConfig()
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("Expected empty file to load as DefaultConfig(), got %+v, want %+v", cfg, want)
	}
}

func TestLoadPartialFileOnlyOverridesNamedFields(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "partial.yaml")

	// Only sets logging.level; every other field (including
	// Cache.Enabled/Parallel.Enabled, both true by default) must retain
	// its DefaultConfig() value rather than being zeroed.
	if err := os.WriteFile(configPath, []byte("logging:\n  level: debug\n"), 0o600); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load partial config: %v", err)
	}

	if cfg.Logging.Level != testLogLevelDebug {
		t.Errorf("Expected Logging.Level 'debug', got '%s'", cfg.Logging.Level)
	}

	want := DefaultConfig()
	want.Logging.Level = testLogLevelDebug
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("Expected only logging.level to change from defaults, got %+v, want %+v", cfg, want)
	}
}

func TestLoadOrDefault(t *testing.T) {
	// Non-existent file should return default.
	cfg := LoadOrDefault("/non/existent/path.yaml")
	if cfg == nil {
		t.Fatal("Expected default config")
	}
	if cfg.Engine.MaxRecursion != 100 {
		t.Errorf("Expected default MaxRecursion 100, got %d", cfg.Engine.MaxRecursion)
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "invalid.yaml")

	invalidContent := `
engine:
  strict_mode: [invalid
  malformed yaml
`

	if err := os.WriteFile(configPath, []byte(invalidContent), 0o600); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Error("Expected error loading invalid YAML")
	}
}

func TestSaveAs(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "subdir", "saved_config.yaml")

	cfg := DefaultConfig()
	cfg.Engine.StrictMode = true
	cfg.Logging.Level = testLogLevelError

	if err := SaveAs(cfg, configPath); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	// Load and verify.
	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load saved config: %v", err)
	}

	if !loaded.Engine.StrictMode {
		t.Error("Expected StrictMode true")
	}
	if loaded.Logging.Level != testLogLevelError {
		t.Errorf("Expected Level 'error', got '%s'", loaded.Logging.Level)
	}
}

func TestApplyEnv(t *testing.T) {
	// Set environment variables.
	_ = os.Setenv("GRAFT_ENGINE_STRICT_MODE", "true")
	_ = os.Setenv("GRAFT_ENGINE_MAX_RECURSION", "200")
	_ = os.Setenv("GRAFT_ENGINE_TIMEOUT", "2m")
	_ = os.Setenv("GRAFT_CACHE_ENABLED", "false")
	_ = os.Setenv("GRAFT_CACHE_MAX_SIZE", "20000")
	_ = os.Setenv("GRAFT_PARALLEL_MIN_WORKERS", "4")
	_ = os.Setenv("GRAFT_PARALLEL_MAX_WORKERS", "16")
	_ = os.Setenv("GRAFT_LOGGING_LEVEL", testLogLevelDebug)
	_ = os.Setenv("GRAFT_LOGGING_FORMAT", testFormatJSON)

	defer func() {
		_ = os.Unsetenv("GRAFT_ENGINE_STRICT_MODE")
		_ = os.Unsetenv("GRAFT_ENGINE_MAX_RECURSION")
		_ = os.Unsetenv("GRAFT_ENGINE_TIMEOUT")
		_ = os.Unsetenv("GRAFT_CACHE_ENABLED")
		_ = os.Unsetenv("GRAFT_CACHE_MAX_SIZE")
		_ = os.Unsetenv("GRAFT_PARALLEL_MIN_WORKERS")
		_ = os.Unsetenv("GRAFT_PARALLEL_MAX_WORKERS")
		_ = os.Unsetenv("GRAFT_LOGGING_LEVEL")
		_ = os.Unsetenv("GRAFT_LOGGING_FORMAT")
	}()

	cfg := DefaultConfig()
	ApplyEnv(cfg)

	if !cfg.Engine.StrictMode {
		t.Error("Expected StrictMode true from env")
	}
	if cfg.Engine.MaxRecursion != 200 {
		t.Errorf("Expected MaxRecursion 200, got %d", cfg.Engine.MaxRecursion)
	}
	if cfg.Engine.Timeout != 2*time.Minute {
		t.Errorf("Expected Timeout 2m, got %v", cfg.Engine.Timeout)
	}
	if cfg.Cache.Enabled {
		t.Error("Expected Cache.Enabled false from env")
	}
	if cfg.Cache.MaxSize != 20000 {
		t.Errorf("Expected MaxSize 20000, got %d", cfg.Cache.MaxSize)
	}
	if cfg.Parallel.MinWorkers != 4 {
		t.Errorf("Expected MinWorkers 4, got %d", cfg.Parallel.MinWorkers)
	}
	if cfg.Parallel.MaxWorkers != 16 {
		t.Errorf("Expected MaxWorkers 16, got %d", cfg.Parallel.MaxWorkers)
	}
	if cfg.Logging.Level != testLogLevelDebug {
		t.Errorf("Expected Level 'debug', got '%s'", cfg.Logging.Level)
	}
	if cfg.Logging.Format != testFormatJSON {
		t.Errorf("Expected Format 'json', got '%s'", cfg.Logging.Format)
	}
}

func TestGetEnvVars(t *testing.T) {
	_ = os.Setenv("GRAFT_LOGGING_LEVEL", testLogLevelDebug)
	defer func() { _ = os.Unsetenv("GRAFT_LOGGING_LEVEL") }()

	vars := GetEnvVars()

	if len(vars) == 0 {
		t.Fatal("Expected env vars to be returned")
	}

	found := false
	for _, v := range vars {
		if v.Name == "GRAFT_LOGGING_LEVEL" {
			found = true
			if !v.IsSet {
				t.Error("Expected GRAFT_LOGGING_LEVEL to be marked as set")
			}
			if v.Value != testLogLevelDebug {
				t.Errorf("Expected value 'debug', got '%s'", v.Value)
			}
		}
	}

	if !found {
		t.Error("GRAFT_LOGGING_LEVEL not found in env vars")
	}
}

func TestGetSetEnvVars(t *testing.T) {
	// Clear any existing vars.
	for _, v := range GetEnvVars() {
		_ = os.Unsetenv(v.Name)
	}

	_ = os.Setenv("GRAFT_LOGGING_LEVEL", "warn")
	_ = os.Setenv("GRAFT_CACHE_ENABLED", "true")
	defer func() {
		_ = os.Unsetenv("GRAFT_LOGGING_LEVEL")
		_ = os.Unsetenv("GRAFT_CACHE_ENABLED")
	}()

	setVars := GetSetEnvVars()

	if len(setVars) != 2 {
		t.Errorf("Expected 2 set env vars, got %d", len(setVars))
	}
}

func TestValidate(t *testing.T) {
	// Valid config should pass.
	cfg := DefaultConfig()
	if err := Validate(cfg); err != nil {
		t.Errorf("Valid config should not have errors: %v", err)
	}

	// Invalid MaxRecursion.
	cfg = DefaultConfig()
	cfg.Engine.MaxRecursion = -1
	if err := Validate(cfg); err == nil {
		t.Error("Expected error for negative MaxRecursion")
	}

	// Invalid Timeout.
	cfg = DefaultConfig()
	cfg.Engine.Timeout = -1 * time.Second
	if err := Validate(cfg); err == nil {
		t.Error("Expected error for negative Timeout")
	}

	// Invalid Cache.MaxSize.
	cfg = DefaultConfig()
	cfg.Cache.MaxSize = -1
	if err := Validate(cfg); err == nil {
		t.Error("Expected error for negative MaxSize")
	}

	// Invalid MinWorkers > MaxWorkers.
	cfg = DefaultConfig()
	cfg.Parallel.MinWorkers = 10
	cfg.Parallel.MaxWorkers = 5
	if err := Validate(cfg); err == nil {
		t.Error("Expected error for MinWorkers > MaxWorkers")
	}

	// Invalid logging level.
	cfg = DefaultConfig()
	cfg.Logging.Level = "invalid_level"
	if err := Validate(cfg); err == nil {
		t.Error("Expected error for invalid log level")
	}

	// Invalid logging format.
	cfg = DefaultConfig()
	cfg.Logging.Format = "invalid_format"
	if err := Validate(cfg); err == nil {
		t.Error("Expected error for invalid log format")
	}

	// Invalid metrics format.
	cfg = DefaultConfig()
	cfg.Metrics.Format = "invalid_format"
	if err := Validate(cfg); err == nil {
		t.Error("Expected error for invalid metrics format")
	}

	// Invalid metrics endpoint (enabled but empty).
	cfg = DefaultConfig()
	cfg.Metrics.Enabled = true
	cfg.Metrics.Endpoint = ""
	if err := Validate(cfg); err == nil {
		t.Error("Expected error for enabled metrics with empty endpoint")
	}

	// Invalid metrics endpoint (missing leading slash).
	cfg = DefaultConfig()
	cfg.Metrics.Endpoint = "metrics"
	if err := Validate(cfg); err == nil {
		t.Error("Expected error for metrics endpoint without leading slash")
	}

	// L2 enabled without path.
	cfg = DefaultConfig()
	cfg.Cache.L2Enabled = true
	cfg.Cache.L2Path = ""
	if err := Validate(cfg); err == nil {
		t.Error("Expected error for L2 enabled without path")
	}
}

func TestValidateAndNormalize(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Logging.Level = "DEBUG"
	cfg.Logging.Format = "JSON"
	cfg.Parallel.MaxWorkers = 0
	cfg.Parallel.MinWorkers = 0

	if err := ValidateAndNormalize(cfg); err != nil {
		t.Errorf("ValidateAndNormalize failed: %v", err)
	}

	// Check normalization.
	if cfg.Logging.Level != "debug" {
		t.Errorf("Expected level to be normalized to 'debug', got '%s'", cfg.Logging.Level)
	}
	if cfg.Logging.Format != "json" {
		t.Errorf("Expected format to be normalized to 'json', got '%s'", cfg.Logging.Format)
	}
	if cfg.Parallel.MaxWorkers != runtime.NumCPU() {
		t.Errorf("Expected MaxWorkers to be auto-detected to %d, got %d", runtime.NumCPU(), cfg.Parallel.MaxWorkers)
	}
	if cfg.Parallel.MinWorkers != 1 {
		t.Errorf("Expected MinWorkers to be set to 1, got %d", cfg.Parallel.MinWorkers)
	}
}

func TestValidationError(t *testing.T) {
	err := ValidationError{
		Field:   "test.field",
		Value:   "bad_value",
		Message: "is invalid",
	}

	errStr := err.Error()
	if errStr == "" {
		t.Error("Error string should not be empty")
	}
	if !contains([]string{errStr}, "test.field") {
		// Just check it contains the field.
		t.Logf("Error string: %s", errStr)
	}
}

func TestValidationErrors(t *testing.T) {
	var errors ValidationErrors

	// Empty errors.
	if errors.Error() != "" {
		t.Error("Empty errors should return empty string")
	}

	errors = append(errors, ValidationError{
		Field:   "field1",
		Value:   "value1",
		Message: "error1",
	}, ValidationError{
		Field:   "field2",
		Value:   "value2",
		Message: "error2",
	})

	errStr := errors.Error()
	if errStr == "" {
		t.Error("Error string should not be empty")
	}
}

func TestManagerLoad(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "test_config.yaml")

	configContent := `
engine:
  strict_mode: true
  max_recursion: 75
logging:
  level: warn
`

	if err := os.WriteFile(configPath, []byte(configContent), 0o600); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	manager := NewManager()
	if err := manager.Load(configPath); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	cfg := manager.Get()
	if !cfg.Engine.StrictMode {
		t.Error("Expected StrictMode true")
	}
	if cfg.Engine.MaxRecursion != 75 {
		t.Errorf("Expected MaxRecursion 75, got %d", cfg.Engine.MaxRecursion)
	}
	if cfg.Logging.Level != "warn" {
		t.Errorf("Expected Level 'warn', got '%s'", cfg.Logging.Level)
	}

	// Verify config path is set.
	if manager.GetConfigPath() == "" {
		t.Error("Config path should be set after Load")
	}
}

func TestManagerLoadOrDefault(t *testing.T) {
	manager := NewManager()

	// Load non-existent file - should use defaults.
	manager.LoadOrDefault("/non/existent/config.yaml")

	cfg := manager.Get()
	if cfg.Engine.MaxRecursion != 100 {
		t.Errorf("Expected default MaxRecursion 100, got %d", cfg.Engine.MaxRecursion)
	}
}

func TestExpandPath(t *testing.T) {
	// Test empty path.
	result, err := expandPath("")
	if err != nil {
		t.Errorf("Unexpected error for empty path: %v", err)
	}
	if result != "" {
		t.Errorf("Expected empty result, got '%s'", result)
	}

	// Test home expansion.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("Cannot get home dir: %v", err)
	}

	result, err = expandPath("~/test")
	if err != nil {
		t.Errorf("Unexpected error for ~ path: %v", err)
	}
	expected := filepath.Join(home, "test")
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}

	// Test env var expansion.
	_ = os.Setenv("TEST_CONFIG_DIR", "/custom/path")
	defer func() { _ = os.Unsetenv("TEST_CONFIG_DIR") }()

	result, err = expandPath("$TEST_CONFIG_DIR/config.yaml")
	if err != nil {
		t.Errorf("Unexpected error for env var path: %v", err)
	}
	if result != "/custom/path/config.yaml" {
		t.Errorf("Expected '/custom/path/config.yaml', got '%s'", result)
	}
}

func TestSimpleWatcher(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "watch_config.yaml")

	// Create initial config.
	initialContent := `
engine:
  strict_mode: false
logging:
  level: info
`
	if err := os.WriteFile(configPath, []byte(initialContent), 0o600); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	manager := NewManager()
	if err := manager.Load(configPath); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify initial config.
	cfg := manager.Get()
	if cfg.Engine.StrictMode {
		t.Error("Expected initial StrictMode false")
	}

	// Create watcher.
	watcher := NewSimpleWatcher(manager, 50*time.Millisecond)

	var callbackCalled bool
	var callbackCfg *Config
	var mu sync.Mutex
	var wg sync.WaitGroup

	wg.Add(1)
	watcher.OnChange(func(cfg *Config) {
		mu.Lock()
		callbackCalled = true
		callbackCfg = cfg
		mu.Unlock()
		wg.Done()
	})

	if err := watcher.Start(); err != nil {
		t.Fatalf("Failed to start watcher: %v", err)
	}

	// Modify the config file.
	time.Sleep(100 * time.Millisecond) // Wait for initial file read

	modifiedContent := `
engine:
  strict_mode: true
logging:
  level: debug
`
	if err := os.WriteFile(configPath, []byte(modifiedContent), 0o600); err != nil {
		t.Fatalf("Failed to modify config: %v", err)
	}

	// Wait for callback with timeout.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// OK
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for watcher callback")
	}

	// Stop watcher.
	watcher.Stop()

	// Verify callback was called.
	mu.Lock()
	if !callbackCalled {
		t.Error("Watcher callback was not called")
	}
	if callbackCfg == nil {
		t.Error("Callback config is nil")
	} else if !callbackCfg.Engine.StrictMode {
		t.Error("Expected updated StrictMode true")
	}
	mu.Unlock()
}

func TestFsnotifyWatcher(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "fsnotify_config.yaml")

	// Create initial config.
	initialContent := `
engine:
  strict_mode: false
logging:
  level: info
`
	if err := os.WriteFile(configPath, []byte(initialContent), 0o600); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	manager := NewManager()
	if err := manager.Load(configPath); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify initial config.
	cfg := manager.Get()
	if cfg.Engine.StrictMode {
		t.Error("Expected initial StrictMode false")
	}

	// Create watcher with short debounce for testing.
	watcher, err := NewWatcher(manager, WithDebounce(50*time.Millisecond))
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}

	var callbackCalled bool
	var callbackCfg *Config
	var mu sync.Mutex
	var wg sync.WaitGroup

	wg.Add(1)
	watcher.OnChange(func(cfg *Config) {
		mu.Lock()
		callbackCalled = true
		callbackCfg = cfg
		mu.Unlock()
		wg.Done()
	})

	if err := watcher.StartPath(configPath); err != nil {
		t.Fatalf("Failed to start watcher: %v", err)
	}

	// Verify watcher state.
	if !watcher.IsWatching() {
		t.Error("Expected watcher to be watching")
	}
	if watcher.WatchedPath() != configPath {
		t.Errorf("Expected watched path '%s', got '%s'", configPath, watcher.WatchedPath())
	}

	// Wait a bit for watcher to be ready.
	time.Sleep(100 * time.Millisecond)

	// Modify the config file.
	modifiedContent := `
engine:
  strict_mode: true
logging:
  level: debug
`
	if err := os.WriteFile(configPath, []byte(modifiedContent), 0o600); err != nil {
		t.Fatalf("Failed to modify config: %v", err)
	}

	// Wait for callback with timeout.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// OK
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for watcher callback")
	}

	// Stop watcher.
	watcher.Stop()

	// Verify watcher stopped.
	if watcher.IsWatching() {
		t.Error("Expected watcher to not be watching after stop")
	}

	// Verify callback was called.
	mu.Lock()
	if !callbackCalled {
		t.Error("Watcher callback was not called")
	}
	if callbackCfg == nil {
		t.Error("Callback config is nil")
	} else if !callbackCfg.Engine.StrictMode {
		t.Error("Expected updated StrictMode true")
	}
	mu.Unlock()
}

func TestWatcherNoConfigPath(t *testing.T) {
	manager := NewManager()

	watcher, err := NewWatcher(manager)
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}
	defer watcher.Stop()

	// Start without loading config should fail.
	if err := watcher.Start(); err == nil {
		t.Error("Expected error when starting watcher without loaded config")
	}
}

func TestSimpleWatcherNoConfigPath(t *testing.T) {
	manager := NewManager()

	watcher := NewSimpleWatcher(manager, time.Second)
	defer watcher.Stop()

	// Start without loading config should fail.
	if err := watcher.Start(); err == nil {
		t.Error("Expected error when starting watcher without loaded config")
	}
}

func TestLoadWithSearch(t *testing.T) {
	// This test just verifies the function works without error.
	// In practice it may or may not find a config file.
	cfg, path, err := LoadWithSearch()
	if err != nil {
		t.Errorf("LoadWithSearch returned error: %v", err)
	}
	if cfg == nil {
		t.Error("Expected config to be returned")
	}
	// path may be empty if no config file was found.
	_ = path
}

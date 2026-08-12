package features

import (
	"os"
	"sync"
	"testing"
)

func TestNew(t *testing.T) {
	ff := New()
	if ff == nil {
		t.Fatal("New() returned nil")
	}
	if ff.flags == nil {
		t.Fatal("New() did not initialize flags map")
	}

	// New instance should have no flags enabled
	for _, flag := range AllFeatures() {
		if ff.IsEnabled(flag) {
			t.Errorf("New() should not have %s enabled by default", flag)
		}
	}
}

func TestDefaultFlags(t *testing.T) {
	ff := DefaultFlags()
	if ff == nil {
		t.Fatal("DefaultFlags() returned nil")
	}

	// Check enabled defaults
	enabledByDefault := []string{FeatureCaching, FeatureMemoryPools}
	for _, flag := range enabledByDefault {
		if !ff.IsEnabled(flag) {
			t.Errorf("DefaultFlags() should have %s enabled", flag)
		}
	}

	// Check disabled defaults
	disabledByDefault := []string{
		FeatureParallelEvaluation,
		FeatureMetrics,
		FeatureDebugLogging,
		FeatureStrictTypeChecking,
	}
	for _, flag := range disabledByDefault {
		if ff.IsEnabled(flag) {
			t.Errorf("DefaultFlags() should have %s disabled", flag)
		}
	}
}

func TestIsEnabled(t *testing.T) {
	ff := New()

	// Test unknown flag returns false
	if ff.IsEnabled("unknown_flag") {
		t.Error("IsEnabled should return false for unknown flags")
	}

	// Test after enabling
	ff.Enable(FeatureCaching)
	if !ff.IsEnabled(FeatureCaching) {
		t.Error("IsEnabled should return true after Enable")
	}
}

func TestEnable(t *testing.T) {
	ff := New()

	ff.Enable(FeatureMetrics)
	if !ff.IsEnabled(FeatureMetrics) {
		t.Error("Enable did not enable the flag")
	}

	// Enable again should be idempotent
	ff.Enable(FeatureMetrics)
	if !ff.IsEnabled(FeatureMetrics) {
		t.Error("Enable should be idempotent")
	}
}

func TestDisable(t *testing.T) {
	ff := DefaultFlags()

	// Disable an enabled flag
	ff.Disable(FeatureCaching)
	if ff.IsEnabled(FeatureCaching) {
		t.Error("Disable did not disable the flag")
	}

	// Disable again should be idempotent
	ff.Disable(FeatureCaching)
	if ff.IsEnabled(FeatureCaching) {
		t.Error("Disable should be idempotent")
	}
}

func TestSet(t *testing.T) {
	ff := New()

	ff.Set(FeatureDebugLogging, true)
	if !ff.IsEnabled(FeatureDebugLogging) {
		t.Error("Set(true) did not enable the flag")
	}

	ff.Set(FeatureDebugLogging, false)
	if ff.IsEnabled(FeatureDebugLogging) {
		t.Error("Set(false) did not disable the flag")
	}
}

func TestSetAll(t *testing.T) {
	ff := New()

	flags := map[string]bool{
		FeatureParallelEvaluation: true,
		FeatureCaching:            true,
		FeatureMetrics:            false,
	}

	ff.SetAll(flags)

	if !ff.IsEnabled(FeatureParallelEvaluation) {
		t.Error("SetAll did not enable FeatureParallelEvaluation")
	}
	if !ff.IsEnabled(FeatureCaching) {
		t.Error("SetAll did not enable FeatureCaching")
	}
	if ff.IsEnabled(FeatureMetrics) {
		t.Error("SetAll should have disabled FeatureMetrics")
	}
}

func TestGetAll(t *testing.T) {
	ff := DefaultFlags()

	all := ff.GetAll()
	if all == nil {
		t.Fatal("GetAll returned nil")
	}

	// Verify it's a copy, not a reference
	all[FeatureCaching] = false
	if !ff.IsEnabled(FeatureCaching) {
		t.Error("GetAll should return a copy, not a reference")
	}
}

func TestReset(t *testing.T) {
	ff := DefaultFlags()

	// Modify some flags
	ff.Enable(FeatureParallelEvaluation)
	ff.Disable(FeatureCaching)

	// Reset without defaults
	ff.Reset(false)
	if ff.IsEnabled(FeatureParallelEvaluation) {
		t.Error("Reset(false) should clear all flags")
	}
	if ff.IsEnabled(FeatureCaching) {
		t.Error("Reset(false) should clear all flags")
	}

	// Reset with defaults
	ff.Reset(true)
	if !ff.IsEnabled(FeatureCaching) {
		t.Error("Reset(true) should restore default enabled flags")
	}
	if ff.IsEnabled(FeatureParallelEvaluation) {
		t.Error("Reset(true) should restore default disabled flags")
	}
}

func TestClone(t *testing.T) {
	ff := DefaultFlags()
	ff.Enable(FeatureDebugLogging)

	clone := ff.Clone()

	// Verify clone has same values
	if !clone.IsEnabled(FeatureCaching) {
		t.Error("Clone should copy enabled flags")
	}
	if !clone.IsEnabled(FeatureDebugLogging) {
		t.Error("Clone should copy enabled flags")
	}

	// Verify clone is independent
	clone.Disable(FeatureCaching)
	if !ff.IsEnabled(FeatureCaching) {
		t.Error("Clone should be independent of original")
	}
}

func TestAllFeatures(t *testing.T) {
	features := AllFeatures()

	expectedFeatures := map[string]bool{
		FeatureParallelEvaluation: true,
		FeatureCaching:            true,
		FeatureMetrics:            true,
		FeatureDebugLogging:       true,
		FeatureStrictTypeChecking: true,
		FeatureMemoryPools:        true,
		FeatureBackendRegistry:    true,
	}

	if len(features) != len(expectedFeatures) {
		t.Errorf("AllFeatures returned %d features, expected %d", len(features), len(expectedFeatures))
	}

	for _, f := range features {
		if !expectedFeatures[f] {
			t.Errorf("Unexpected feature in AllFeatures: %s", f)
		}
	}
}

func TestThreadSafety(t *testing.T) {
	ff := New()
	var wg sync.WaitGroup

	// Concurrent enables
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ff.Enable(FeatureCaching)
		}()
	}

	// Concurrent disables
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ff.Disable(FeatureMetrics)
		}()
	}

	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = ff.IsEnabled(FeatureCaching)
		}()
	}

	// Concurrent GetAll
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = ff.GetAll()
		}()
	}

	wg.Wait()
}

func TestLoadFromEnv(t *testing.T) {
	// Save and clear environment
	originalEnv := make(map[string]string)
	for env := range envMapping {
		originalEnv[env] = os.Getenv(env)
		_ = os.Unsetenv(env)
	}
	defer func() {
		// Restore environment
		for env, value := range originalEnv {
			if value != "" {
				_ = os.Setenv(env, value)
			} else {
				_ = os.Unsetenv(env)
			}
		}
	}()

	// Set test environment variables
	_ = os.Setenv(EnvFeatureParallel, "true")
	_ = os.Setenv(EnvFeatureCache, "false")
	_ = os.Setenv(EnvFeatureMetrics, "1")
	_ = os.Setenv(EnvFeatureDebug, "yes")
	_ = os.Setenv(EnvFeatureStrictTypes, "on")
	_ = os.Setenv(EnvFeaturePools, "0")

	ff := New()
	ff.LoadFromEnv()

	tests := []struct {
		flag     string
		expected bool
	}{
		{FeatureParallelEvaluation, true},
		{FeatureCaching, false},
		{FeatureMetrics, true},
		{FeatureDebugLogging, true},
		{FeatureStrictTypeChecking, true},
		{FeatureMemoryPools, false},
	}

	for _, tt := range tests {
		if ff.IsEnabled(tt.flag) != tt.expected {
			t.Errorf("LoadFromEnv: %s should be %v", tt.flag, tt.expected)
		}
	}
}

func TestLoadFromEnvPreservesUnsetFlags(t *testing.T) {
	// Save and clear environment
	originalEnv := make(map[string]string)
	for env := range envMapping {
		originalEnv[env] = os.Getenv(env)
		_ = os.Unsetenv(env)
	}
	defer func() {
		for env, value := range originalEnv {
			if value != "" {
				_ = os.Setenv(env, value)
			} else {
				_ = os.Unsetenv(env)
			}
		}
	}()

	// Only set some environment variables
	_ = os.Setenv(EnvFeatureParallel, "true")

	ff := DefaultFlags()
	// Cache is enabled by default
	if !ff.IsEnabled(FeatureCaching) {
		t.Fatal("Precondition failed: caching should be enabled by default")
	}

	ff.LoadFromEnv()

	// Parallel should be enabled from env
	if !ff.IsEnabled(FeatureParallelEvaluation) {
		t.Error("LoadFromEnv should have enabled parallel from env")
	}

	// Cache should still be enabled (not changed by env since unset)
	if !ff.IsEnabled(FeatureCaching) {
		t.Error("LoadFromEnv should preserve unset flag values")
	}
}

func TestLoadFromEnvWithPrefix(t *testing.T) {
	testPrefix := "TEST_GRAFT_"

	// Save and clear test environment
	testEnvVars := []string{
		testPrefix + "FEATURE_PARALLEL",
		testPrefix + "FEATURE_CACHE",
	}
	originalEnv := make(map[string]string)
	for _, env := range testEnvVars {
		originalEnv[env] = os.Getenv(env)
		_ = os.Unsetenv(env)
	}
	defer func() {
		for env, value := range originalEnv {
			if value != "" {
				_ = os.Setenv(env, value)
			} else {
				_ = os.Unsetenv(env)
			}
		}
	}()

	_ = os.Setenv(testPrefix+"FEATURE_PARALLEL", "true")
	_ = os.Setenv(testPrefix+"FEATURE_CACHE", "false")

	ff := New()
	ff.LoadFromEnvWithPrefix(testPrefix)

	if !ff.IsEnabled(FeatureParallelEvaluation) {
		t.Error("LoadFromEnvWithPrefix should have enabled parallel")
	}
	if ff.IsEnabled(FeatureCaching) {
		t.Error("LoadFromEnvWithPrefix should have disabled caching")
	}
}

func TestParseBool(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
		ok       bool
	}{
		{"true", true, true},
		{"True", true, true},
		{"TRUE", true, true},
		{"false", false, true},
		{"False", false, true},
		{"FALSE", false, true},
		{"1", true, true},
		{"0", false, true},
		{"yes", true, true},
		{"YES", true, true},
		{"no", false, true},
		{"NO", false, true},
		{"on", true, true},
		{"ON", true, true},
		{"off", false, true},
		{"OFF", false, true},
		{"enabled", true, true},
		{"disabled", false, true},
		{"  true  ", true, true},
		{"invalid", false, false},
		{"", false, false},
		{"maybe", false, false},
	}

	for _, tt := range tests {
		got, ok := parseBool(tt.input)
		if ok != tt.ok {
			t.Errorf("parseBool(%q) ok = %v, want %v", tt.input, ok, tt.ok)
		}
		if ok && got != tt.expected {
			t.Errorf("parseBool(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestGetEnvMapping(t *testing.T) {
	mapping := GetEnvMapping()

	// Verify it returns expected mappings
	if mapping[EnvFeatureParallel] != FeatureParallelEvaluation {
		t.Error("GetEnvMapping missing parallel mapping")
	}
	if mapping[EnvFeatureCache] != FeatureCaching {
		t.Error("GetEnvMapping missing cache mapping")
	}

	// Verify it's a copy
	mapping[EnvFeatureParallel] = "modified"
	newMapping := GetEnvMapping()
	if newMapping[EnvFeatureParallel] != FeatureParallelEvaluation {
		t.Error("GetEnvMapping should return a copy")
	}
}

func TestFlagToEnvVar(t *testing.T) {
	tests := []struct {
		flag     string
		expected string
	}{
		{FeatureParallelEvaluation, EnvFeatureParallel},
		{FeatureCaching, EnvFeatureCache},
		{FeatureMetrics, EnvFeatureMetrics},
		{FeatureDebugLogging, EnvFeatureDebug},
		{FeatureStrictTypeChecking, EnvFeatureStrictTypes},
		{FeatureMemoryPools, EnvFeaturePools},
		{"unknown_flag", ""},
	}

	for _, tt := range tests {
		got := FlagToEnvVar(tt.flag)
		if got != tt.expected {
			t.Errorf("FlagToEnvVar(%q) = %q, want %q", tt.flag, got, tt.expected)
		}
	}
}

func TestGlobal(t *testing.T) {
	// Reset global for testing
	ResetGlobal()
	defer ResetGlobal()

	// Clear environment to ensure predictable defaults
	originalEnv := make(map[string]string)
	for env := range envMapping {
		originalEnv[env] = os.Getenv(env)
		_ = os.Unsetenv(env)
	}
	defer func() {
		for env, value := range originalEnv {
			if value != "" {
				_ = os.Setenv(env, value)
			} else {
				_ = os.Unsetenv(env)
			}
		}
	}()

	g1 := Global()
	g2 := Global()

	// Should return same instance
	if g1 != g2 {
		t.Error("Global() should return the same instance")
	}

	// Should have defaults
	if !g1.IsEnabled(FeatureCaching) {
		t.Error("Global() should have default flags")
	}
}

func TestResetGlobal(t *testing.T) {
	// Get initial global
	g1 := Global()
	g1.Enable(FeatureDebugLogging)

	// Reset and get new global
	ResetGlobal()
	g2 := Global()

	// Should be different instances
	if g1 == g2 {
		t.Error("ResetGlobal should create new instance")
	}
}

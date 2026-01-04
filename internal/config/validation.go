package config

import (
	"fmt"
	"runtime"
	"strings"
	"time"
)

// ValidationError represents a configuration validation error.
type ValidationError struct {
	Field   string
	Value   interface{}
	Message string
}

// Error implements the error interface.
func (e ValidationError) Error() string {
	return fmt.Sprintf("config validation error: field '%s' with value '%v': %s", e.Field, e.Value, e.Message)
}

// ValidationErrors represents multiple validation errors.
type ValidationErrors []ValidationError

// Error implements the error interface.
func (e ValidationErrors) Error() string {
	if len(e) == 0 {
		return ""
	}

	messages := make([]string, 0, len(e))
	for _, err := range e {
		messages = append(messages, err.Error())
	}
	return strings.Join(messages, "; ")
}

// Validate validates the entire configuration and returns any validation errors.
func Validate(cfg *Config) error {
	var errors ValidationErrors

	// Validate engine configuration.
	if errs := validateEngine(&cfg.Engine); len(errs) > 0 {
		errors = append(errors, errs...)
	}

	// Validate cache configuration.
	if errs := validateCache(&cfg.Cache); len(errs) > 0 {
		errors = append(errors, errs...)
	}

	// Validate parallel configuration.
	if errs := validateParallel(&cfg.Parallel); len(errs) > 0 {
		errors = append(errors, errs...)
	}

	// Validate metrics configuration.
	if errs := validateMetrics(&cfg.Metrics); len(errs) > 0 {
		errors = append(errors, errs...)
	}

	// Validate logging configuration.
	if errs := validateLogging(&cfg.Logging); len(errs) > 0 {
		errors = append(errors, errs...)
	}

	if len(errors) > 0 {
		return errors
	}
	return nil
}

// validateEngine validates engine configuration.
func validateEngine(cfg *EngineConfig) ValidationErrors {
	var errors ValidationErrors

	// Validate MaxRecursion.
	if cfg.MaxRecursion < 0 {
		errors = append(errors, ValidationError{
			Field:   "engine.max_recursion",
			Value:   cfg.MaxRecursion,
			Message: "cannot be negative",
		})
	}

	if cfg.MaxRecursion > 10000 {
		errors = append(errors, ValidationError{
			Field:   "engine.max_recursion",
			Value:   cfg.MaxRecursion,
			Message: "exceeds maximum allowed value of 10000",
		})
	}

	// Validate Timeout.
	if cfg.Timeout < 0 {
		errors = append(errors, ValidationError{
			Field:   "engine.timeout",
			Value:   cfg.Timeout,
			Message: "cannot be negative",
		})
	}

	if cfg.Timeout > 24*time.Hour {
		errors = append(errors, ValidationError{
			Field:   "engine.timeout",
			Value:   cfg.Timeout,
			Message: "exceeds maximum allowed value of 24 hours",
		})
	}

	return errors
}

// validateCache validates cache configuration.
func validateCache(cfg *CacheConfig) ValidationErrors {
	var errors ValidationErrors

	// Validate MaxSize.
	if cfg.MaxSize < 0 {
		errors = append(errors, ValidationError{
			Field:   "cache.max_size",
			Value:   cfg.MaxSize,
			Message: "cannot be negative",
		})
	}

	// Validate TTL.
	if cfg.TTL < 0 {
		errors = append(errors, ValidationError{
			Field:   "cache.ttl",
			Value:   cfg.TTL,
			Message: "cannot be negative",
		})
	}

	// Validate L2Path if L2 is enabled.
	if cfg.L2Enabled && cfg.L2Path == "" {
		errors = append(errors, ValidationError{
			Field:   "cache.l2_path",
			Value:   cfg.L2Path,
			Message: "l2_path is required when l2_enabled is true",
		})
	}

	return errors
}

// validateParallel validates parallel configuration.
func validateParallel(cfg *ParallelConfig) ValidationErrors {
	var errors ValidationErrors

	// Validate MinWorkers.
	if cfg.MinWorkers < 0 {
		errors = append(errors, ValidationError{
			Field:   "parallel.min_workers",
			Value:   cfg.MinWorkers,
			Message: "cannot be negative",
		})
	}

	// Validate MaxWorkers.
	if cfg.MaxWorkers < 0 {
		errors = append(errors, ValidationError{
			Field:   "parallel.max_workers",
			Value:   cfg.MaxWorkers,
			Message: "cannot be negative",
		})
	}

	// Validate MaxWorkers >= MinWorkers (when MaxWorkers is set).
	if cfg.MaxWorkers > 0 && cfg.MinWorkers > cfg.MaxWorkers {
		errors = append(errors, ValidationError{
			Field:   "parallel.min_workers",
			Value:   cfg.MinWorkers,
			Message: fmt.Sprintf("min_workers (%d) cannot be greater than max_workers (%d)", cfg.MinWorkers, cfg.MaxWorkers),
		})
	}

	// Warn if MaxWorkers is very high.
	numCPU := runtime.NumCPU()
	if cfg.MaxWorkers > numCPU*4 {
		errors = append(errors, ValidationError{
			Field:   "parallel.max_workers",
			Value:   cfg.MaxWorkers,
			Message: fmt.Sprintf("warning: very high worker count (%d) for %d CPUs may cause resource contention", cfg.MaxWorkers, numCPU),
		})
	}

	return errors
}

// validateMetrics validates metrics configuration.
func validateMetrics(cfg *MetricsConfig) ValidationErrors {
	var errors ValidationErrors

	// Validate Format.
	validFormats := []string{"prometheus", "json", "text"}
	if cfg.Format != "" && !contains(validFormats, strings.ToLower(cfg.Format)) {
		errors = append(errors, ValidationError{
			Field:   "metrics.format",
			Value:   cfg.Format,
			Message: fmt.Sprintf("must be one of: %v", validFormats),
		})
	}

	// Validate Endpoint.
	if cfg.Enabled && cfg.Endpoint == "" {
		errors = append(errors, ValidationError{
			Field:   "metrics.endpoint",
			Value:   cfg.Endpoint,
			Message: "endpoint is required when metrics are enabled",
		})
	}

	if cfg.Endpoint != "" && !strings.HasPrefix(cfg.Endpoint, "/") {
		errors = append(errors, ValidationError{
			Field:   "metrics.endpoint",
			Value:   cfg.Endpoint,
			Message: "endpoint must start with '/'",
		})
	}

	return errors
}

// validateLogging validates logging configuration.
func validateLogging(cfg *LoggingConfig) ValidationErrors {
	var errors ValidationErrors

	// Validate Level.
	validLevels := []string{"debug", "info", "warn", "error"}
	if cfg.Level != "" && !contains(validLevels, strings.ToLower(cfg.Level)) {
		errors = append(errors, ValidationError{
			Field:   "logging.level",
			Value:   cfg.Level,
			Message: fmt.Sprintf("must be one of: %v", validLevels),
		})
	}

	// Validate Format.
	validFormats := []string{"json", "text"}
	if cfg.Format != "" && !contains(validFormats, strings.ToLower(cfg.Format)) {
		errors = append(errors, ValidationError{
			Field:   "logging.format",
			Value:   cfg.Format,
			Message: fmt.Sprintf("must be one of: %v", validFormats),
		})
	}

	return errors
}

// contains checks if a string slice contains a specific string.
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// ValidateAndNormalize validates and normalizes the configuration.
// It applies sensible defaults where values are missing or zero.
func ValidateAndNormalize(cfg *Config) error {
	// Normalize logging level to lowercase.
	cfg.Logging.Level = strings.ToLower(cfg.Logging.Level)
	cfg.Logging.Format = strings.ToLower(cfg.Logging.Format)

	// Normalize metrics format to lowercase.
	cfg.Metrics.Format = strings.ToLower(cfg.Metrics.Format)

	// Auto-detect MaxWorkers if set to 0.
	if cfg.Parallel.MaxWorkers == 0 {
		cfg.Parallel.MaxWorkers = runtime.NumCPU()
	}

	// Ensure MinWorkers has a sensible default.
	if cfg.Parallel.MinWorkers == 0 {
		cfg.Parallel.MinWorkers = 1
	}

	// Validate after normalization.
	return Validate(cfg)
}

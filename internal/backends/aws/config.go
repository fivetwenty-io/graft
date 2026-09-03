// Package aws provides AWS client management and configuration
// for the AWS operator backend.
package aws

import (
	"os"
	"strconv"
	"time"
)

// Target represents an AWS target configuration.
type Target struct {
	Region             string        `yaml:"region"`
	Profile            string        `yaml:"profile"`
	Role               string        `yaml:"role"`
	AccessKeyID        string        `yaml:"access_key_id"`
	SecretAccessKey    string        `yaml:"secret_access_key"`
	SessionToken       string        `yaml:"session_token"`
	Endpoint           string        `yaml:"endpoint"`
	DisableSSL         bool          `yaml:"disable_ssl"`
	MaxRetries         int           `yaml:"max_retries"`
	HTTPTimeout        time.Duration `yaml:"http_timeout"`
	CacheTTL           time.Duration `yaml:"cache_ttl"`
	AssumeRoleDuration time.Duration `yaml:"assume_role_duration"`
	ExternalID         string        `yaml:"external_id"`
	SessionName        string        `yaml:"session_name"`
	MfaSerial          string        `yaml:"mfa_serial"`
	AuditLogging       bool          `yaml:"audit_logging"`
}

// GetEnvOrDefault returns environment variable value or default.
func GetEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// ParseDurationOrDefault parses duration string or returns default.
func ParseDurationOrDefault(value string, defaultValue time.Duration) time.Duration {
	if d, err := time.ParseDuration(value); err == nil {
		return d
	}
	return defaultValue
}

// ParseIntOrDefault parses integer string or returns default.
func ParseIntOrDefault(value string, defaultValue int) int {
	if i, err := strconv.Atoi(value); err == nil {
		return i
	}
	return defaultValue
}

// ParseBoolOrDefault parses boolean string or returns false as default.
func ParseBoolOrDefault(value string) bool {
	if b, err := strconv.ParseBool(value); err == nil {
		return b
	}
	return false
}

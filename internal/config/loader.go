package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Load loads configuration from a YAML file at the specified path.
// It returns an error if the file cannot be read or parsed.
func Load(path string) (*Config, error) {
	expandedPath, err := expandPath(path)
	if err != nil {
		return nil, fmt.Errorf("expanding config path: %w", err)
	}

	cleanPath := filepath.Clean(expandedPath)
	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	// Start with defaults and overlay the file contents.
	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	// Validate the loaded configuration.
	if err := Validate(cfg); err != nil {
		return nil, fmt.Errorf("validating configuration: %w", err)
	}

	return cfg, nil
}

// LoadOrDefault attempts to load configuration from the specified path.
// If the file does not exist or cannot be loaded, it returns the default configuration.
func LoadOrDefault(path string) *Config {
	cfg, err := Load(path)
	if err != nil {
		return DefaultConfig()
	}
	return cfg
}

// LoadWithSearch searches for configuration files in standard locations
// and loads the first one found. Search order:
//
// 1. ./graft.yaml (current directory)
//
// 2. $HOME/.graft/config.yaml (user config directory)
//
// 3. /etc/graft/config.yaml (system config)
//
// If no configuration file is found, it returns the default configuration.
func LoadWithSearch() (*Config, string, error) {
	searchPaths := getSearchPaths()

	for _, path := range searchPaths {
		expandedPath, err := expandPath(path)
		if err != nil {
			continue
		}

		if _, err := os.Stat(expandedPath); err == nil {
			cfg, err := Load(expandedPath)
			if err != nil {
				return nil, "", fmt.Errorf("loading config from %s: %w", expandedPath, err)
			}
			return cfg, expandedPath, nil
		}
	}

	return DefaultConfig(), "", nil
}

// SaveAs saves the configuration to a YAML file at the specified path.
func SaveAs(cfg *Config, path string) error {
	expandedPath, err := expandPath(path)
	if err != nil {
		return fmt.Errorf("expanding config path: %w", err)
	}

	// Ensure the parent directory exists.
	dir := filepath.Dir(expandedPath)
	if mkdirErr := os.MkdirAll(dir, 0o700); mkdirErr != nil {
		return fmt.Errorf("creating config directory: %w", mkdirErr)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling configuration: %w", err)
	}

	if err := os.WriteFile(expandedPath, data, 0o600); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}

	return nil
}

// LoadAndApplyEnv loads configuration from a file and applies environment variable overrides.
func LoadAndApplyEnv(path string) (*Config, error) {
	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}

	ApplyEnv(cfg)

	// Re-validate after applying environment overrides.
	if err := Validate(cfg); err != nil {
		return nil, fmt.Errorf("validating configuration after env overrides: %w", err)
	}

	return cfg, nil
}

// Load loads configuration into a Manager from the specified path.
func (m *Manager) Load(path string) error {
	cfg, err := LoadAndApplyEnv(path)
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.config = cfg
	m.configPath = path
	hooks := make([]func(*Config), len(m.changeHooks))
	copy(hooks, m.changeHooks)
	m.mu.Unlock()

	// Notify change hooks.
	for _, hook := range hooks {
		go hook(cfg.Clone())
	}

	return nil
}

// LoadOrDefault loads configuration from path or uses defaults.
func (m *Manager) LoadOrDefault(path string) {
	if err := m.Load(path); err != nil {
		m.mu.Lock()
		m.config = DefaultConfig()
		m.configPath = ""
		m.mu.Unlock()
	}
}

// getSearchPaths returns the list of paths to search for configuration files.
func getSearchPaths() []string {
	paths := []string{
		"./graft.yaml",
		"~/.graft/config.yaml",
	}

	// Add system config path on Unix-like systems.
	if _, err := os.Stat("/etc"); err == nil {
		paths = append(paths, "/etc/graft/config.yaml")
	}

	return paths
}

// expandPath expands ~ and environment variables in paths.
func expandPath(path string) (string, error) {
	if path == "" {
		return "", nil
	}

	// Expand ~ to home directory.
	if path != "" && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("getting home directory: %w", err)
		}
		path = filepath.Join(home, path[1:])
	}

	// Expand environment variables.
	path = os.ExpandEnv(path)

	return path, nil
}

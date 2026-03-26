package vault

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/cloudfoundry-community/vaultkv"
	"github.com/geofffranks/yaml"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
	"github.com/fivetwenty-io/graft/pkg/graft"
)

// GlobalKV is the module-level vault client, initialized lazily from environment.
var GlobalKV *vaultkv.KV

// ClientPool manages vault clients for different targets.
type ClientPool struct {
	clients map[string]*vaultkv.KV
	configs map[string]*Target
}

// DefaultPool is the global client pool for target-aware vault clients.
var DefaultPool = &ClientPool{
	clients: make(map[string]*vaultkv.KV),
	configs: make(map[string]*Target),
}

// GetClient returns a vault client for the specified target.
func (vcp *ClientPool) GetClient(targetName string, engine graft.Engine) (*vaultkv.KV, error) {
	// Return existing client if available
	if client, exists := vcp.clients[targetName]; exists {
		return client, nil
	}

	// Get target configuration
	config, err := vcp.getTargetConfig(targetName, engine)
	if err != nil {
		return nil, fmt.Errorf("vault target '%s' not found: %w", targetName, err)
	}

	// Create new client
	client, err := CreateClientFromConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create vault client for target '%s': %w", targetName, err)
	}

	// Store client for reuse
	vcp.clients[targetName] = client
	vcp.configs[targetName] = config

	return client, nil
}

// getTargetConfig retrieves target configuration from the engine or environment.
//
//nolint:unparam // engine reserved for future configuration retrieval from engine
func (vcp *ClientPool) getTargetConfig(targetName string, engine graft.Engine) (*Target, error) {
	// Check if we have cached config
	if config, exists := vcp.configs[targetName]; exists {
		return config, nil
	}

	// For now, try environment variables with target suffix
	// In a full implementation, this would query the engine's configuration
	envPrefix := fmt.Sprintf("VAULT_%s_", strings.ToUpper(targetName))

	config := &Target{
		URL:       os.Getenv(envPrefix + "ADDR"),
		Token:     os.Getenv(envPrefix + "TOKEN"),
		Namespace: os.Getenv(envPrefix + "NAMESPACE"),
	}

	// Check for skip verify
	if skipStr := os.Getenv(envPrefix + "SKIP_VERIFY"); skipStr == "true" || skipStr == "1" {
		config.SkipVerify = true
	}

	// If no environment variables found, return error
	if config.URL == "" || config.Token == "" {
		return nil, fmt.Errorf("vault target '%s' configuration not found (expected %sADDR and %sTOKEN environment variables)",
			targetName, envPrefix, envPrefix)
	}

	return config, nil
}

// CreateClientFromConfig creates a vault client from target configuration.
func CreateClientFromConfig(config *Target) (*vaultkv.KV, error) {
	// Expand environment variables in configuration
	addr := os.ExpandEnv(config.URL)
	token := os.ExpandEnv(config.Token)
	namespace := os.ExpandEnv(config.Namespace)

	parsedURL, err := url.Parse(addr)
	if err != nil {
		return nil, fmt.Errorf("could not parse Vault URL `%s': %w", addr, err)
	}

	// Port handling
	if parsedURL.Port() == "" {
		if parsedURL.Scheme == "http" {
			parsedURL.Host += ":80"
		} else {
			parsedURL.Host += ":443"
		}
	}

	// TLS configuration
	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve system root certificate authorities: %w", err)
	}

	client := &vaultkv.Client{
		AuthToken: token,
		VaultURL:  parsedURL,
		Namespace: namespace,
		Client: &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				TLSClientConfig: &tls.Config{
					RootCAs:            roots,
					InsecureSkipVerify: config.SkipVerify, // #nosec G402 - controlled by user configuration
				},
			},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) > 10 {
					return fmt.Errorf("stopped after 10 redirects")
				}
				req.Header.Add("X-Vault-Token", token)
				req.Header.Add("X-Vault-Namespace", namespace)
				return nil
			},
		},
	}

	// Enable tracing if debug is on
	if graft.DebugOn() {
		client.Trace = os.Stderr
	}

	return client.NewKV(), nil
}

// InitializeClient initializes the global vault client from environment variables.
//
//nolint:gocyclo // Vault client init handles multiple auth sources and TLS config
func InitializeClient() error {
	addr := os.Getenv("VAULT_ADDR")
	token := os.Getenv("VAULT_TOKEN")
	namespace := os.Getenv("VAULT_NAMESPACE")
	skip := false

	if addr == "" || token == "" {
		svtoken := struct {
			Vault      string `yaml:"vault"`
			Token      string `yaml:"token"`
			Namespace  string `yaml:"namespace"`
			SkipVerify bool   `yaml:"skip_verify"`
		}{}
		b, err := os.ReadFile(os.ExpandEnv("${HOME}/.svtoken"))
		if err == nil {
			err = yaml.Unmarshal(b, &svtoken)
			if err == nil {
				addr = svtoken.Vault
				token = svtoken.Token
				namespace = svtoken.Namespace
				skip = svtoken.SkipVerify
			}
		}
	}

	if SkipVerify(os.Getenv("VAULT_SKIP_VERIFY")) {
		skip = true
	}

	if token == "" {
		b, err := os.ReadFile(fmt.Sprintf("%s/.vault-token", os.Getenv("HOME")))
		if err == nil {
			token = strings.TrimSuffix(string(b), "\n")
		}
	}

	if addr == "" || token == "" {
		return fmt.Errorf("failed to determine Vault URL / token, and the $REDACT environment variable is not set")
	}

	roots, err := x509.SystemCertPool()
	if err != nil {
		return fmt.Errorf("unable to retrieve system root certificate authorities: %w", err)
	}

	parsedURL, err := url.Parse(addr)
	if err != nil {
		return fmt.Errorf("could not parse Vault URL `%s': %w", addr, err)
	}

	if parsedURL.Port() == "" {
		if parsedURL.Scheme == "http" {
			parsedURL.Host += ":80"
		} else {
			parsedURL.Host += ":443"
		}
	}

	client := &vaultkv.Client{
		AuthToken: token,
		VaultURL:  parsedURL,
		Namespace: namespace,
		Client: &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				TLSClientConfig: &tls.Config{
					RootCAs:            roots,
					InsecureSkipVerify: skip, // #nosec G402 - skip is controlled by user configuration
				},
			},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) > 10 {
					return fmt.Errorf("stopped after 10 redirects")
				}
				req.Header.Add("X-Vault-Token", token)
				req.Header.Add("X-Vault-Namespace", token)
				return nil
			},
		},
	}
	if graft.DebugOn() {
		client.Trace = os.Stderr
	}

	GlobalKV = client.NewKV()

	return nil
}

// GetSecretWithClient retrieves a secret using the provided client.
func GetSecretWithClient(kvClient *vaultkv.KV, secret string) (map[string]interface{}, error) {
	ret := map[string]interface{}{}

	graft.DEBUG("Fetching Vault secret at `%s'", secret)
	_, err := kvClient.Get(secret, &ret, nil)
	if err != nil {
		graft.DEBUG(" failure.")
		return nil, err
	}

	graft.DEBUG("  success.")
	return ret, nil
}

// ExtractSubkey extracts a named subkey from a vault secret map and returns it as a string.
func ExtractSubkey(secretMap map[string]interface{}, secret, subkey string) (string, error) {
	graft.DEBUG("  extracting the [%s] subkey from the secret", subkey)

	secretSubkeyPath := fmt.Sprintf("%s:%s", secret, subkey)
	v, ok := secretMap[subkey]
	if !ok {
		graft.DEBUG("    !! %s not found!\n", secretSubkeyPath)
		return "", ansi.Errorf("@R{secret} @c{%s} @R{not found}", secretSubkeyPath)
	}
	vStr, ok := v.(string)
	if !ok {
		graft.DEBUG("    !! %s is not a string!\n", secretSubkeyPath)
		return "", ansi.Errorf("@R{secret} @c{%s} @R{is not a string}", secretSubkeyPath)
	}
	graft.DEBUG(" success.")
	return vStr, nil
}

// ParsePath splits a vault path into the secret path and the key name.
// The key is the portion after the last colon.
func ParsePath(path string) (secret, key string) {
	secret = path
	if idx := strings.LastIndex(path, ":"); idx >= 0 {
		secret = path[:idx]
		key = path[idx+1:]
	}
	return
}

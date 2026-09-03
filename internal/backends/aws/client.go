package aws

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// ClientPool manages AWS configs and clients for different targets.
type ClientPool struct {
	mu                    sync.RWMutex
	awsConfigs            map[string]aws.Config
	secretsManagerClients map[string]SecretsManagerClient
	parameterStoreClients map[string]SSMClient
	configs               map[string]*Target
	secretsCache          map[string]map[string]string // target -> secret -> value
	paramsCache           map[string]map[string]string // target -> param -> value
}

// DefaultPool is the global client pool for target-aware AWS connections.
var DefaultPool = &ClientPool{
	awsConfigs:            make(map[string]aws.Config),
	secretsManagerClients: make(map[string]SecretsManagerClient),
	parameterStoreClients: make(map[string]SSMClient),
	configs:               make(map[string]*Target),
	secretsCache:          make(map[string]map[string]string),
	paramsCache:           make(map[string]map[string]string),
}

// GetConfig returns an aws.Config for the specified target, building and
// caching it on first use via BuildConfig.
func (acp *ClientPool) GetConfig(ctx context.Context, targetName string) (aws.Config, error) {
	acp.mu.RLock()
	if cfg, exists := acp.awsConfigs[targetName]; exists {
		acp.mu.RUnlock()
		return cfg, nil
	}
	acp.mu.RUnlock()

	// Get target configuration
	targetConfig, err := acp.GetTargetConfig(targetName)
	if err != nil {
		return aws.Config{}, fmt.Errorf("AWS target '%s' not found: %w", targetName, err)
	}

	// Build an aws.Config from target config
	cfg, err := acp.BuildConfig(ctx, targetConfig)
	if err != nil {
		return aws.Config{}, fmt.Errorf("failed to build AWS config for target '%s': %w", targetName, err)
	}

	// Store config for reuse
	acp.mu.Lock()
	acp.awsConfigs[targetName] = cfg
	acp.configs[targetName] = targetConfig
	acp.mu.Unlock()

	return cfg, nil
}

// GetSecretsManagerClient returns a Secrets Manager client for the specified target.
func (acp *ClientPool) GetSecretsManagerClient(ctx context.Context, targetName string) (SecretsManagerClient, error) {
	acp.mu.RLock()
	if client, exists := acp.secretsManagerClients[targetName]; exists {
		acp.mu.RUnlock()
		return client, nil
	}
	acp.mu.RUnlock()

	// Get config for this target
	cfg, err := acp.GetConfig(ctx, targetName)
	if err != nil {
		return nil, err
	}

	// Create Secrets Manager client
	client := NewSecretsManagerClient(cfg)

	// Store client for reuse
	acp.mu.Lock()
	acp.secretsManagerClients[targetName] = client
	acp.mu.Unlock()

	return client, nil
}

// GetParameterStoreClient returns a Parameter Store client for the specified target.
func (acp *ClientPool) GetParameterStoreClient(ctx context.Context, targetName string) (SSMClient, error) {
	acp.mu.RLock()
	if client, exists := acp.parameterStoreClients[targetName]; exists {
		acp.mu.RUnlock()
		return client, nil
	}
	acp.mu.RUnlock()

	// Get config for this target
	cfg, err := acp.GetConfig(ctx, targetName)
	if err != nil {
		return nil, err
	}

	// Create Parameter Store client
	client := NewSSMClient(cfg)

	// Store client for reuse
	acp.mu.Lock()
	acp.parameterStoreClients[targetName] = client
	acp.mu.Unlock()

	return client, nil
}

// GetTargetConfig retrieves target configuration from environment variables.
func (acp *ClientPool) GetTargetConfig(targetName string) (*Target, error) {
	// Check if we have cached config
	acp.mu.RLock()
	if targetConfig, exists := acp.configs[targetName]; exists {
		acp.mu.RUnlock()
		return targetConfig, nil
	}
	acp.mu.RUnlock()

	// Use environment variables with target suffix
	envPrefix := fmt.Sprintf("AWS_%s_", strings.ToUpper(targetName))

	// Check if any AWS target-specific environment variables are set
	region := os.Getenv(envPrefix + "REGION")
	profile := os.Getenv(envPrefix + "PROFILE")
	role := os.Getenv(envPrefix + "ROLE")
	accessKeyID := os.Getenv(envPrefix + "ACCESS_KEY_ID")

	// Require at least one target-specific configuration
	if region == "" && profile == "" && role == "" && accessKeyID == "" {
		return nil, fmt.Errorf("AWS target '%s' configuration incomplete (expected %sREGION, %sPROFILE, %sROLE, or %sACCESS_KEY_ID environment variable)",
			targetName, envPrefix, envPrefix, envPrefix, envPrefix)
	}

	targetConfig := &Target{
		Region:             GetEnvOrDefault(envPrefix+"REGION", ""),
		Profile:            GetEnvOrDefault(envPrefix+"PROFILE", ""),
		Role:               GetEnvOrDefault(envPrefix+"ROLE", ""),
		AccessKeyID:        GetEnvOrDefault(envPrefix+"ACCESS_KEY_ID", ""),
		SecretAccessKey:    GetEnvOrDefault(envPrefix+"SECRET_ACCESS_KEY", ""),
		SessionToken:       GetEnvOrDefault(envPrefix+"SESSION_TOKEN", ""),
		Endpoint:           GetEnvOrDefault(envPrefix+"ENDPOINT", ""),
		DisableSSL:         ParseBoolOrDefault(GetEnvOrDefault(envPrefix+"DISABLE_SSL", "false")),
		MaxRetries:         ParseIntOrDefault(GetEnvOrDefault(envPrefix+"MAX_RETRIES", "3"), 3),
		HTTPTimeout:        ParseDurationOrDefault(GetEnvOrDefault(envPrefix+"HTTP_TIMEOUT", "30s"), 30*time.Second),
		CacheTTL:           ParseDurationOrDefault(GetEnvOrDefault(envPrefix+"CACHE_TTL", "5m"), 5*time.Minute),
		AssumeRoleDuration: ParseDurationOrDefault(GetEnvOrDefault(envPrefix+"ASSUME_ROLE_DURATION", "1h"), 1*time.Hour),
		ExternalID:         GetEnvOrDefault(envPrefix+"EXTERNAL_ID", ""),
		SessionName:        GetEnvOrDefault(envPrefix+"SESSION_NAME", "graft-"+targetName),
		MfaSerial:          GetEnvOrDefault(envPrefix+"MFA_SERIAL", ""),
		AuditLogging:       ParseBoolOrDefault(GetEnvOrDefault(envPrefix+"AUDIT_LOGGING", "false")),
	}

	return targetConfig, nil
}

// endpointFor returns the endpoint to configure on aws.Config for t. A
// DisableSSL target rewrites an explicit "https://" Endpoint's scheme to
// "http://" (the narrowed v2 meaning of the v1 DisableSSL flag - v2 has no
// direct equivalent, and AWS proper does not serve plaintext, so
// DisableSSL alone with no Endpoint set is a documented no-op). Any other
// Endpoint (including one already using "http://", or one using "https://"
// with DisableSSL unset) is returned verbatim. An empty Endpoint returns
// "", telling BuildConfig not to call config.WithBaseEndpoint at all.
func endpointFor(t *Target) string {
	if t.Endpoint == "" {
		return ""
	}
	if t.DisableSSL && strings.HasPrefix(t.Endpoint, "https://") {
		return "http://" + strings.TrimPrefix(t.Endpoint, "https://")
	}
	return t.Endpoint
}

// BuildConfig builds an aws.Config from target configuration, composing
// config.LoadDefaultConfig with the target's region, profile, endpoint,
// retry, and credential settings, and wrapping the result in a role
// assumption provider (backed by aws.NewCredentialsCache, so repeated
// calls do not each re-run sts:AssumeRole) when target.Role is set.
func (acp *ClientPool) BuildConfig(ctx context.Context, target *Target) (aws.Config, error) {
	var opts []func(*config.LoadOptions) error

	// Configure region
	if target.Region != "" {
		opts = append(opts, config.WithRegion(target.Region))
	}

	// Configure profile
	if target.Profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(target.Profile))
	}

	// Configure endpoint (for testing or custom endpoints)
	if ep := endpointFor(target); ep != "" {
		opts = append(opts, config.WithBaseEndpoint(ep))
	}

	// Configure retries: MaxRetries preserves its documented "number of
	// retries" meaning by mapping to RetryMaxAttempts = MaxRetries+1 (v2's
	// "total attempts" semantics). A non-positive value leaves the SDK
	// default (3 attempts) in effect, matching v1's own "if > 0" guard.
	if target.MaxRetries > 0 {
		opts = append(opts, config.WithRetryMaxAttempts(target.MaxRetries+1))
	}

	// Configure the HTTP client's request timeout. A non-positive value
	// (Target's zero value, or an explicit 0/negative override) leaves
	// cfg.HTTPClient unset, so the SDK's own default HTTP client - which
	// has no client-side deadline of its own - stays in effect.
	if target.HTTPTimeout > 0 {
		opts = append(opts, config.WithHTTPClient(awshttp.NewBuildableClient().WithTimeout(target.HTTPTimeout)))
	}

	// Configure credentials if provided
	if target.AccessKeyID != "" && target.SecretAccessKey != "" {
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(target.AccessKeyID, target.SecretAccessKey, target.SessionToken),
		))
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return aws.Config{}, err
	}

	// Configure role assumption if provided
	if target.Role != "" {
		stsClient := sts.NewFromConfig(cfg) // inherits BaseEndpoint, matching v1's global Endpoint reaching STS
		provider := stscreds.NewAssumeRoleProvider(stsClient, target.Role, func(o *stscreds.AssumeRoleOptions) {
			if target.AssumeRoleDuration > 0 {
				o.Duration = target.AssumeRoleDuration
			}
			if target.ExternalID != "" {
				o.ExternalID = aws.String(target.ExternalID)
			}
			if target.SessionName != "" {
				o.RoleSessionName = target.SessionName
			}
			if target.MfaSerial != "" {
				o.SerialNumber = aws.String(target.MfaSerial)
				// Note: MFA token input is not yet wired here - a serial
				// set with no TokenProvider makes credential retrieval
				// fail with a clear error from stscreds, matching v1's
				// own gap (see CreateSessionFromConfig's identical
				// comment in the pre-port history of this file).
			}
		})
		// REQUIRED: without this cache, every SSM/Secrets Manager call
		// through cfg would re-run sts:AssumeRole.
		cfg.Credentials = aws.NewCredentialsCache(provider)
	}

	return cfg, nil
}

// InitializeConfig builds an aws.Config honoring shared config (e.g.
// ~/.aws/credentials), an optional profile, an optional region, and an
// optional STS AssumeRole, matching the plain-environment ("AWS_PROFILE"/
// "AWS_REGION"/"AWS_ROLE", not the "AWS_<TARGET>_*" family) resolution
// path op_aws.go falls back to when no pooled "default" target config is
// available.
func InitializeConfig(ctx context.Context, profile string, region string, role string) (aws.Config, error) {
	var opts []func(*config.LoadOptions) error

	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}

	if profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(profile))
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return aws.Config{}, err
	}

	if role != "" {
		stsClient := sts.NewFromConfig(cfg)
		provider := stscreds.NewAssumeRoleProvider(stsClient, role)
		cfg.Credentials = aws.NewCredentialsCache(provider)
	}

	return cfg, nil
}

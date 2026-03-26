package aws

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/secretsmanager"
	"github.com/aws/aws-sdk-go/service/secretsmanager/secretsmanageriface"
	"github.com/aws/aws-sdk-go/service/ssm"
	"github.com/aws/aws-sdk-go/service/ssm/ssmiface"
)

// ClientPool manages AWS sessions and clients for different targets.
type ClientPool struct {
	mu                    sync.RWMutex
	sessions              map[string]*session.Session
	secretsManagerClients map[string]secretsmanageriface.SecretsManagerAPI
	parameterStoreClients map[string]ssmiface.SSMAPI
	configs               map[string]*Target
	secretsCache          map[string]map[string]string // target -> secret -> value
	paramsCache           map[string]map[string]string // target -> param -> value
}

// DefaultPool is the global client pool for target-aware AWS connections.
var DefaultPool = &ClientPool{
	sessions:              make(map[string]*session.Session),
	secretsManagerClients: make(map[string]secretsmanageriface.SecretsManagerAPI),
	parameterStoreClients: make(map[string]ssmiface.SSMAPI),
	configs:               make(map[string]*Target),
	secretsCache:          make(map[string]map[string]string),
	paramsCache:           make(map[string]map[string]string),
}

// GetSession returns an AWS session for the specified target.
func (acp *ClientPool) GetSession(targetName string) (*session.Session, error) {
	acp.mu.RLock()
	if sess, exists := acp.sessions[targetName]; exists {
		acp.mu.RUnlock()
		return sess, nil
	}
	acp.mu.RUnlock()

	// Get target configuration
	config, err := acp.GetTargetConfig(targetName)
	if err != nil {
		return nil, fmt.Errorf("AWS target '%s' not found: %w", targetName, err)
	}

	// Create AWS session from target config
	sess, err := acp.CreateSessionFromConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS session for target '%s': %w", targetName, err)
	}

	// Store session for reuse
	acp.mu.Lock()
	acp.sessions[targetName] = sess
	acp.configs[targetName] = config
	acp.mu.Unlock()

	return sess, nil
}

// GetSecretsManagerClient returns a Secrets Manager client for the specified target.
func (acp *ClientPool) GetSecretsManagerClient(targetName string) (secretsmanageriface.SecretsManagerAPI, error) {
	acp.mu.RLock()
	if client, exists := acp.secretsManagerClients[targetName]; exists {
		acp.mu.RUnlock()
		return client, nil
	}
	acp.mu.RUnlock()

	// Get session for this target
	sess, err := acp.GetSession(targetName)
	if err != nil {
		return nil, err
	}

	// Create Secrets Manager client
	client := secretsmanager.New(sess)

	// Store client for reuse
	acp.mu.Lock()
	acp.secretsManagerClients[targetName] = client
	acp.mu.Unlock()

	return client, nil
}

// GetParameterStoreClient returns a Parameter Store client for the specified target.
func (acp *ClientPool) GetParameterStoreClient(targetName string) (ssmiface.SSMAPI, error) {
	acp.mu.RLock()
	if client, exists := acp.parameterStoreClients[targetName]; exists {
		acp.mu.RUnlock()
		return client, nil
	}
	acp.mu.RUnlock()

	// Get session for this target
	sess, err := acp.GetSession(targetName)
	if err != nil {
		return nil, err
	}

	// Create Parameter Store client
	client := ssm.New(sess)

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
	if config, exists := acp.configs[targetName]; exists {
		acp.mu.RUnlock()
		return config, nil
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

	config := &Target{
		Region:             GetEnvOrDefault(envPrefix+"REGION", ""),
		Profile:            GetEnvOrDefault(envPrefix+"PROFILE", ""),
		Role:               GetEnvOrDefault(envPrefix+"ROLE", ""),
		AccessKeyID:        GetEnvOrDefault(envPrefix+"ACCESS_KEY_ID", ""),
		SecretAccessKey:    GetEnvOrDefault(envPrefix+"SECRET_ACCESS_KEY", ""),
		SessionToken:       GetEnvOrDefault(envPrefix+"SESSION_TOKEN", ""),
		Endpoint:           GetEnvOrDefault(envPrefix+"ENDPOINT", ""),
		S3ForcePathStyle:   ParseBoolOrDefault(GetEnvOrDefault(envPrefix+"S3_FORCE_PATH_STYLE", "false")),
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

	return config, nil
}

// CreateSessionFromConfig creates an AWS session from target configuration.
//
//nolint:gocyclo // AWS session configuration requires handling many options
func (acp *ClientPool) CreateSessionFromConfig(config *Target) (*session.Session, error) {
	options := session.Options{
		Config:            aws.Config{},
		SharedConfigState: session.SharedConfigEnable,
	}

	// Configure region
	if config.Region != "" {
		options.Config.Region = aws.String(config.Region)
	}

	// Configure profile
	if config.Profile != "" {
		options.Profile = config.Profile
	}

	// Configure endpoint (for testing or custom endpoints)
	if config.Endpoint != "" {
		options.Config.Endpoint = aws.String(config.Endpoint)
	}

	// Configure S3 path style
	if config.S3ForcePathStyle {
		options.Config.S3ForcePathStyle = aws.Bool(true)
	}

	// Configure SSL
	if config.DisableSSL {
		options.Config.DisableSSL = aws.Bool(true)
	}

	// Configure retries
	if config.MaxRetries > 0 {
		options.Config.MaxRetries = aws.Int(config.MaxRetries)
	}

	// Configure HTTP timeout (this would require additional configuration in practice)
	// HTTPTimeout is not directly available in aws.Config but would be handled by custom transport

	// Configure credentials if provided
	if config.AccessKeyID != "" && config.SecretAccessKey != "" {
		options.Config.Credentials = credentials.NewStaticCredentials(
			config.AccessKeyID,
			config.SecretAccessKey,
			config.SessionToken,
		)
	}

	// Create base session
	sess, err := session.NewSessionWithOptions(options)
	if err != nil {
		return nil, err
	}

	// Configure role assumption if provided
	if config.Role != "" {
		assumeRoleFunc := func(p *stscreds.AssumeRoleProvider) {
			if config.AssumeRoleDuration > 0 {
				p.Duration = config.AssumeRoleDuration
			}
			if config.ExternalID != "" {
				p.ExternalID = aws.String(config.ExternalID)
			}
			if config.SessionName != "" {
				p.RoleSessionName = config.SessionName
			}
			if config.MfaSerial != "" {
				p.SerialNumber = aws.String(config.MfaSerial)
				// Note: MFA token input would need to be handled separately
			}
		}

		creds := stscreds.NewCredentials(sess, config.Role, assumeRoleFunc)
		roleConfig := aws.Config{Credentials: creds}
		if config.Region != "" {
			roleConfig.Region = aws.String(config.Region)
		}
		sess, err = session.NewSession(&roleConfig)
		if err != nil {
			return nil, err
		}
	}

	return sess, nil
}

// InitializeSession configures an AWS session with profile, region, and role
// assume including loading shared config (e.g. ~/.aws/credentials).
func InitializeSession(profile string, region string, role string) (s *session.Session, err error) {
	options := session.Options{
		Config:            aws.Config{},
		SharedConfigState: session.SharedConfigEnable,
	}

	if region != "" {
		options.Config.Region = aws.String(region)
	}

	if profile != "" {
		options.Profile = profile
	}

	s, err = session.NewSessionWithOptions(options)
	if err != nil {
		return nil, err
	}

	if role != "" {
		options.Config.Credentials = stscreds.NewCredentials(s, role, func(p *stscreds.AssumeRoleProvider) {})
		s, err = session.NewSession(&options.Config)
	}

	return s, err
}

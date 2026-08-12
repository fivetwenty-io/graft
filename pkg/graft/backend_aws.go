package graft

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	awssdk "github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/secretsmanager"
	"github.com/aws/aws-sdk-go/service/ssm"
	"github.com/aws/aws-sdk-go/service/sts"
)

// AWSConfig.AccessKeyID/SecretAccessKey/SessionToken/PoolSize are added
// here (rather than in the AWSConfig struct's own doc comment above, in
// api.go, where AWSConfig is declared) to keep the WithAWS/WithAWSTarget
// machinery that gives them an effect next to their documentation. Adding
// fields to an already-shipped exported struct is non-breaking as long as
// no caller uses an unkeyed composite literal; the only two literals in
// this repo are both keyed - examples.go:221 and engine_test.go:997.

// awsOptionBackendKinds enumerates the two Backend names WithAWS/
// WithAWSTarget register - one per AWS operator variant
// (pkg/graft/operators/op_aws.go's AwsOperator.variant). Both share one
// underlying *awsConfigStore (one WithAWS call configures both), but are
// registered as two independent Backend entries because the vault/AWS/NATS
// operators look a custom backend up by their own operator name (see
// Backend.Name's doc comment).
var awsOptionBackendKinds = [2]string{"awsparam", "awssecret"}

// awsConfigStore holds the AWSConfig and lazily-built *session.Session per
// target ("" is WithAWS's own default; other keys are WithAWSTarget
// names), shared by both of a NewEngine call's "awsparam" and "awssecret"
// awsOptionBackend instances so a single WithAWS/WithAWSTarget call
// configures both operators' backends at once.
type awsConfigStore struct {
	mu       sync.RWMutex
	configs  map[string]AWSConfig
	sessions map[string]*session.Session
}

func newAWSConfigStore() *awsConfigStore {
	return &awsConfigStore{
		configs:  make(map[string]AWSConfig),
		sessions: make(map[string]*session.Session),
	}
}

func (s *awsConfigStore) setConfig(target string, cfg AWSConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configs[target] = cfg
	delete(s.sessions, target)
}

func (s *awsConfigStore) sessionFor(target string) (*session.Session, error) {
	s.mu.RLock()
	if sess, ok := s.sessions[target]; ok {
		s.mu.RUnlock()
		return sess, nil
	}
	cfg, configured := s.configs[target]
	s.mu.RUnlock()

	if !configured {
		if target == "" {
			return nil, fmt.Errorf("aws: no configuration set; call WithAWS")
		}
		return nil, fmt.Errorf("aws: target %q not configured; call WithAWSTarget(%q, ...)", target, target)
	}

	sess, err := buildAWSSession(cfg)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	// Re-check: a concurrent caller may have built (and stored) a session
	// for the same target while this one was under construction; keep
	// whichever was stored first so all callers converge on one session.
	if existing, ok := s.sessions[target]; ok {
		s.mu.Unlock()
		return existing, nil
	}
	s.sessions[target] = sess
	s.mu.Unlock()

	return sess, nil
}

// buildAWSSession constructs an AWS session from cfg. Region is left unset
// (deferring to the SDK's own environment/shared-config resolution) when
// cfg.Region is "" - the AWS SDK errors out per-call with "MissingRegion"
// if nothing supplies one, which is the same failure the built-in
// internal/backends/aws path has when neither a target env var nor
// AWS_REGION is set.
//
// Not carried over from internal/backends/aws.Target (see
// awsOptionBackend's doc comment for why this is a from-scratch builder,
// not an import of that package): S3ForcePathStyle, MaxRetries,
// HTTPTimeout, AssumeRoleDuration/ExternalID/SessionName/MfaSerial (MFA'd
// role assumption), CacheTTL, AuditLogging. Role assumption itself IS
// carried over (a bare sts:AssumeRole, no MFA/session-name/external-ID
// options) since it is one call and a real, commonly-needed capability;
// the rest are cut because they would be silent no-ops on AWSConfig's
// current field set without adding fields the plan never asked for -
// flagged in the WithAWS doc comment rather than added speculatively.
func buildAWSSession(cfg AWSConfig) (*session.Session, error) {
	options := session.Options{
		Config:            awssdk.Config{},
		SharedConfigState: session.SharedConfigEnable,
	}

	if cfg.Region != "" {
		options.Config.Region = awssdk.String(cfg.Region)
	}
	if cfg.Profile != "" {
		options.Profile = cfg.Profile
	}
	if cfg.Endpoint != "" {
		options.Config.Endpoint = awssdk.String(cfg.Endpoint)
		if strings.HasPrefix(cfg.Endpoint, "http://") {
			options.Config.DisableSSL = awssdk.Bool(true)
		}
	}
	if cfg.PoolSize > 0 {
		options.Config.HTTPClient = &http.Client{
			Transport: &http.Transport{
				MaxIdleConnsPerHost: cfg.PoolSize,
				MaxIdleConns:        cfg.PoolSize,
			},
		}
	}

	switch {
	case cfg.SkipAuth:
		options.Config.Credentials = credentials.AnonymousCredentials
	case cfg.AccessKeyID != "" && cfg.SecretAccessKey != "":
		options.Config.Credentials = credentials.NewStaticCredentials(cfg.AccessKeyID, cfg.SecretAccessKey, cfg.SessionToken)
	}

	sess, err := session.NewSessionWithOptions(options)
	if err != nil {
		return nil, fmt.Errorf("aws: failed to create session: %w", err)
	}

	if cfg.Role != "" {
		roleCfg := awssdk.Config{Credentials: stscreds.NewCredentials(sess, cfg.Role)}
		if cfg.Region != "" {
			roleCfg.Region = awssdk.String(cfg.Region)
		}
		sess, err = session.NewSession(&roleCfg)
		if err != nil {
			return nil, fmt.Errorf("aws: failed to assume role %q: %w", cfg.Role, err)
		}
	}

	return sess, nil
}

// awsOptionBackend is the graft.Backend registered under "awsparam" or
// "awssecret" by WithAWS/WithAWSTarget - see awsConfigStore's doc comment
// for why one WithAWS call produces two Backend registrations sharing one
// store, and buildAWSSession's doc comment for the internal/backends/aws
// import-cycle reason this is a from-scratch client builder rather than a
// reuse of that package (the same reason vaultOptionBackend gives - see
// backend_vault.go).
//
// A key reaches Get/GetWithTarget already stripped of its "?stage=...&
// key=..." query suffix by AwsOperator.Run (see op_aws.go's
// resolveCustomBackend call site) - stage/version secret-selection and the
// "?key=..." post-fetch subkey extraction are handled uniformly by the
// operator for both the built-in path and any custom backend, so
// awsOptionBackend never sees them and always fetches the latest/default
// version.
type awsOptionBackend struct {
	store *awsConfigStore
	kind  string // "awsparam" or "awssecret" - see awsOptionBackendKinds.
}

// Name implements Backend.
func (b *awsOptionBackend) Name() string { return b.kind }

// Get implements Backend using the "" (WithAWS default) configuration.
func (b *awsOptionBackend) Get(ctx context.Context, key string) (interface{}, error) {
	return b.fetch(ctx, "", key)
}

// GetWithTarget implements TargetedBackend using the WithAWSTarget
// configuration registered under target. An empty target falls back to
// Get, matching vaultOptionBackend.GetWithTarget's same defensive
// behavior for direct (non-operator) callers.
func (b *awsOptionBackend) GetWithTarget(ctx context.Context, target, key string) (interface{}, error) {
	return b.fetch(ctx, target, key)
}

func (b *awsOptionBackend) fetch(ctx context.Context, target, key string) (interface{}, error) {
	sess, err := b.store.sessionFor(target)
	if err != nil {
		return nil, err
	}

	switch b.kind {
	case "awsparam":
		return getAWSOptionParam(ctx, sess, key)
	case "awssecret":
		return getAWSOptionSecret(ctx, sess, key)
	default:
		// Unreachable outside this file: only newAWSOptionBackends
		// constructs awsOptionBackend, and only with a kind from
		// awsOptionBackendKinds.
		return nil, fmt.Errorf("aws: backend has unknown kind %q", b.kind)
	}
}

// GetBatch implements Backend using SequentialGetBatch - see
// Backend.GetBatch's doc comment for why (no call site to design real
// batching against).
func (b *awsOptionBackend) GetBatch(ctx context.Context, keys []string) (map[string]interface{}, error) {
	return SequentialGetBatch(ctx, keys, b.Get)
}

// Health implements Backend by calling sts:GetCallerIdentity against the
// "" (WithAWS default) target's session - the AWS equivalent of "can this
// credential authenticate at all", since neither SSM nor Secrets Manager
// has a service-level health/ping call. A store configured only via
// WithAWSTarget (no WithAWS) has no default to check, matching
// vaultOptionBackend.Health's same behavior.
func (b *awsOptionBackend) Health(ctx context.Context) error {
	sess, err := b.store.sessionFor("")
	if err != nil {
		return err
	}
	_, err = sts.New(sess).GetCallerIdentityWithContext(ctx, &sts.GetCallerIdentityInput{})
	return err
}

// Close implements Backend. There is no persistent connection or
// background goroutine to release: aws-sdk-go v1 sessions are thin
// wrappers over *http.Client, whose idle connections close themselves on
// Go's normal transport idle timeout.
func (b *awsOptionBackend) Close() error { return nil }

// getAWSOptionParam fetches an SSM Parameter Store value (with
// decryption, matching internal/backends/aws's own WithDecryption:true
// default).
func getAWSOptionParam(ctx context.Context, sess *session.Session, name string) (string, error) {
	out, err := ssm.New(sess).GetParameterWithContext(ctx, &ssm.GetParameterInput{
		Name:           awssdk.String(name),
		WithDecryption: awssdk.Bool(true),
	})
	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok && (awsErr.Code() == ssm.ErrCodeParameterNotFound || awsErr.Code() == ssm.ErrCodeResourceNotFoundException) {
			return "", fmt.Errorf("%w: %s", ErrBackendNotFound, name)
		}
		return "", fmt.Errorf("aws: GetParameter %q: %w", name, err)
	}
	if out.Parameter == nil || out.Parameter.Value == nil {
		return "", fmt.Errorf("%w: %s", ErrBackendNotFound, name)
	}
	return *out.Parameter.Value, nil
}

// getAWSOptionSecret fetches a Secrets Manager value. A binary secret
// (SecretBinary, no SecretString) is returned as its raw bytes converted
// to a string, matching how a caller reading a binary secret through the
// built-in awssecret path would receive it (getAwsSecret in op_aws.go
// applies the same SecretString-else-SecretBinary precedence).
func getAWSOptionSecret(ctx context.Context, sess *session.Session, id string) (string, error) {
	out, err := secretsmanager.New(sess).GetSecretValueWithContext(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: awssdk.String(id),
	})
	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok && awsErr.Code() == secretsmanager.ErrCodeResourceNotFoundException {
			return "", fmt.Errorf("%w: %s", ErrBackendNotFound, id)
		}
		return "", fmt.Errorf("aws: GetSecretValue %q: %w", id, err)
	}
	if out.SecretString != nil {
		return *out.SecretString, nil
	}
	if out.SecretBinary != nil {
		return string(out.SecretBinary), nil
	}
	return "", fmt.Errorf("%w: %s", ErrBackendNotFound, id)
}

// awsConfigStoreFor returns the *awsConfigStore shared by opts.Backends'
// "awsparam"/"awssecret" entries. Behavior depends on what is already
// registered under those two names:
//   - If either name already holds an *awsOptionBackend, its store is
//     returned as-is - neither name is touched, so an earlier
//     WithBackend(myCustomAWSBackend) call under the *other* name (e.g. an
//     "awsparam" *awsOptionBackend alongside a hand-registered "awssecret"
//     Backend) is left exactly as the caller registered it, not replaced.
//     This is the one case that looks like "both replaced" is what should
//     happen (WithBackend/RegisterBackend's "last registration wins" rule)
//     but is not: this function only ever creates or leaves entries alone,
//     it never overwrites one that is already an *awsOptionBackend, and it
//     never touches a name it did not itself just create.
//   - Otherwise (neither name is already an *awsOptionBackend - including
//     when one or both hold an unrelated Backend from an earlier
//     WithBackend call), both names are (re)created backed by one new
//     store - this is the only branch that replaces an existing
//     non-*awsOptionBackend entry, and it always replaces both names
//     together, never just one.
func awsConfigStoreFor(opts *EngineOptions) *awsConfigStore {
	if opts.Backends == nil {
		opts.Backends = make(map[string]Backend)
	}
	for _, kind := range awsOptionBackendKinds {
		if existing, ok := opts.Backends[kind]; ok {
			if ob, ok := existing.(*awsOptionBackend); ok {
				return ob.store
			}
		}
	}

	store := newAWSConfigStore()
	for _, kind := range awsOptionBackendKinds {
		opts.Backends[kind] = &awsOptionBackend{store: store, kind: kind}
	}
	return store
}

// WithAWS registers Backends named "awsparam" and "awssecret", both built
// from config, that the awsparam/awssecret operators consult when
// features.FeatureBackendRegistry is enabled (see WithBackendRegistry) -
// see awsOptionBackend's and buildAWSSession's doc comments for exactly
// what they do and do not carry over from internal/backends/aws's
// environment-configured path. Calling WithAWS more than once, or
// combining it with WithAWSTarget, applies to the same underlying store:
// the last call for a given target ("" for WithAWS, a name for
// WithAWSTarget) wins.
//
// WithAWS alone does not enable FeatureBackendRegistry: pair it with
// WithBackendRegistry(true) (or a supplied *features.FeatureFlags that
// already enables it) or the registered backends are built but never
// consulted.
//
// Also works with DefaultEngine.Configure - it registers through
// WithBackend under the hood, so it is subject to the same Configure
// wiring WithBackend's doc comment describes.
func WithAWS(config AWSConfig) EngineOption {
	return func(opts *EngineOptions) {
		awsConfigStoreFor(opts).setConfig("", config)
	}
}

// WithAWSTarget registers per-"@target" AWS configuration (e.g.
// `(( awsparam@name "/path" ))`) on the same "awsparam"/"awssecret"
// backends WithAWS registers, reachable via TargetedBackend.GetWithTarget.
// name must be non-empty ("" is WithAWS's own default target and is reset
// by calling WithAWS, not this); an empty name is a no-op. WithAWSTarget
// may be called before WithAWS; the backends are created on first use by
// either.
func WithAWSTarget(name string, config AWSConfig) EngineOption {
	return func(opts *EngineOptions) {
		if name == "" {
			return
		}
		awsConfigStoreFor(opts).setConfig(name, config)
	}
}

package graft

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"

	"github.com/fivetwenty-io/graft/internal/backends/aws/awsfakes"
)

// TestBuildAWSConfig_RegionProfileEndpoint proves buildAWSConfig threads
// Region/Endpoint into the resulting aws.Config.
func TestBuildAWSConfig_RegionProfileEndpoint(t *testing.T) {
	cfg, err := buildAWSConfig(context.Background(), AWSConfig{Region: "us-west-2", Endpoint: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("buildAWSConfig failed: %v", err)
	}
	if cfg.Region != "us-west-2" {
		t.Fatalf("expected Region to reach aws.Config, got %v", cfg.Region)
	}
	if got := aws.ToString(cfg.BaseEndpoint); got != "http://127.0.0.1:1" {
		t.Fatalf("expected Endpoint to reach aws.Config, got %v", got)
	}
}

// TestBuildAWSConfig_EndpointSchemeIsPreserved proves an "https://"
// Endpoint reaches aws.Config verbatim - AWSConfig has no DisableSSL field
// to rewrite the scheme from, unlike internal/backends/aws.Target.
func TestBuildAWSConfig_EndpointSchemeIsPreserved(t *testing.T) {
	cfg, err := buildAWSConfig(context.Background(), AWSConfig{Region: "us-east-1", Endpoint: "https://example.com"})
	if err != nil {
		t.Fatalf("buildAWSConfig failed: %v", err)
	}
	if got := aws.ToString(cfg.BaseEndpoint); got != "https://example.com" {
		t.Fatalf("expected an https:// Endpoint to reach aws.Config verbatim, got %q", got)
	}
}

// TestBuildAWSConfig_StaticCredentials proves AccessKeyID/SecretAccessKey/
// SessionToken reach the config's Credentials as a static provider.
func TestBuildAWSConfig_StaticCredentials(t *testing.T) {
	cfg, err := buildAWSConfig(context.Background(), AWSConfig{
		Region:          "us-east-1",
		AccessKeyID:     "AKIAEXAMPLE",
		SecretAccessKey: "secret",
		SessionToken:    "token",
	})
	if err != nil {
		t.Fatalf("buildAWSConfig failed: %v", err)
	}
	v, err := cfg.Credentials.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("Credentials.Retrieve failed: %v", err)
	}
	if v.AccessKeyID != "AKIAEXAMPLE" || v.SecretAccessKey != "secret" || v.SessionToken != "token" {
		t.Fatalf("expected the configured static credentials, got %+v", v)
	}
}

// TestBuildAWSConfig_SkipAuthUsesAnonymousCredentials proves SkipAuth
// wins over AccessKeyID/SecretAccessKey when both are set, matching the
// field's name. config.LoadDefaultConfig always wraps the resolved
// provider in *aws.CredentialsCache (see wrapWithCredentialsCache), so
// cfg.Credentials cannot be type-asserted to aws.AnonymousCredentials
// directly; instead this proves the wrapped provider IS
// aws.AnonymousCredentials by observing its distinctive behavior:
// Retrieve deliberately errors with a message naming
// "AnonymousCredentials" (by design - see the type's doc comment), which
// the static credentials configured alongside SkipAuth would not do if
// they had been selected instead.
func TestBuildAWSConfig_SkipAuthUsesAnonymousCredentials(t *testing.T) {
	cfg, err := buildAWSConfig(context.Background(), AWSConfig{
		Region:          "us-east-1",
		SkipAuth:        true,
		AccessKeyID:     "AKIAEXAMPLE",
		SecretAccessKey: "secret",
	})
	if err != nil {
		t.Fatalf("buildAWSConfig failed: %v", err)
	}
	_, err = cfg.Credentials.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected AnonymousCredentials' sentinel Retrieve error, got nil (the static credentials may have been selected instead)")
	}
	if !strings.Contains(err.Error(), "AnonymousCredentials") {
		t.Fatalf("expected the AnonymousCredentials sentinel error, got: %v", err)
	}
}

// TestBuildAWSConfig_PoolSize proves PoolSize's entire documented effect
// on the resulting config's HTTPClient transport.
func TestBuildAWSConfig_PoolSize(t *testing.T) {
	cfg, err := buildAWSConfig(context.Background(), AWSConfig{Region: "us-east-1", PoolSize: 9})
	if err != nil {
		t.Fatalf("buildAWSConfig failed: %v", err)
	}
	bc, ok := cfg.HTTPClient.(*awshttp.BuildableClient)
	if !ok {
		t.Fatalf("expected an *awshttp.BuildableClient, got %T", cfg.HTTPClient)
	}
	transport := bc.GetTransport()
	if transport.MaxIdleConnsPerHost != 9 || transport.MaxIdleConns != 9 {
		t.Fatalf("expected PoolSize=9 to set both pool fields to 9, got MaxIdleConnsPerHost=%d MaxIdleConns=%d", transport.MaxIdleConnsPerHost, transport.MaxIdleConns)
	}
}

// TestBuildAWSConfig_RoleWrapsProviderInCache proves role assumption
// wraps its provider in *aws.CredentialsCache - without it, every
// SSM/Secrets Manager call would re-run sts:AssumeRole.
func TestBuildAWSConfig_RoleWrapsProviderInCache(t *testing.T) {
	cfg, err := buildAWSConfig(context.Background(), AWSConfig{
		Region:          "us-east-1",
		AccessKeyID:     "AKIAEXAMPLE",
		SecretAccessKey: "secret",
		Role:            "arn:aws:iam::123456789012:role/test-role",
	})
	if err != nil {
		t.Fatalf("buildAWSConfig failed: %v", err)
	}
	if _, ok := cfg.Credentials.(*aws.CredentialsCache); !ok {
		t.Fatalf("expected role assumption to wrap the provider in *aws.CredentialsCache, got %T", cfg.Credentials)
	}
}

// awsJSONServer returns an httptest server that always serves body for any
// request, standing in for either SSM or Secrets Manager's JSON RPC
// endpoint (the two are never both hit by a single test, so one handler
// suffices).
func awsJSONServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestGetAWSOptionParam_Found proves a successful SSM GetParameter
// response is unmarshaled into the returned string.
func TestGetAWSOptionParam_Found(t *testing.T) {
	srv := awsJSONServer(t, http.StatusOK, `{"Parameter":{"Name":"/x","Value":"param-value","Type":"String"}}`)
	cfg, err := buildAWSConfig(context.Background(), AWSConfig{Region: "us-east-1", Endpoint: srv.URL, SkipAuth: true})
	if err != nil {
		t.Fatalf("buildAWSConfig failed: %v", err)
	}
	val, err := getAWSOptionParam(context.Background(), cfg, "/x")
	if err != nil {
		t.Fatalf("getAWSOptionParam failed: %v", err)
	}
	if val != "param-value" {
		t.Fatalf("expected \"param-value\", got %q", val)
	}
}

// TestGetAWSOptionParam_NotFound proves an SSM ParameterNotFound response
// maps to ErrBackendNotFound - this is a wire-level test proving the v2
// SDK's JSON deserializer still maps the body's "__type" field to
// *ssmtypes.ParameterNotFound.
func TestGetAWSOptionParam_NotFound(t *testing.T) {
	srv := awsJSONServer(t, http.StatusBadRequest, `{"__type":"ParameterNotFound","message":"not found"}`)
	cfg, err := buildAWSConfig(context.Background(), AWSConfig{Region: "us-east-1", Endpoint: srv.URL, SkipAuth: true})
	if err != nil {
		t.Fatalf("buildAWSConfig failed: %v", err)
	}
	_, err = getAWSOptionParam(context.Background(), cfg, "/missing")
	if err == nil {
		t.Fatal("expected an error for a missing parameter, got nil")
	}
	if !errors.Is(err, ErrBackendNotFound) {
		t.Fatalf("expected ErrBackendNotFound, got: %v", err)
	}
}

// TestGetAWSOptionParam_NotFoundViaFake proves the errors.As mapping
// covers *ssmtypes.ResourceNotFoundException too (not just
// *ssmtypes.ParameterNotFound, which TestGetAWSOptionParam_NotFound
// already proves at the wire level) - a fake makes it trivial to return
// the SDK's typed error value directly, rather than round-tripping it
// through an httptest JSON body.
func TestGetAWSOptionParam_NotFoundViaFake(t *testing.T) {
	original := newAWSSSMClient
	fake := &awsfakes.FakeSSMClient{
		GetParameterFn: func(context.Context, *ssm.GetParameterInput, ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
			return nil, &ssmtypes.ResourceNotFoundException{Message: aws.String("not found")}
		},
	}
	newAWSSSMClient = func(aws.Config) awsSSMAPI { return fake }
	t.Cleanup(func() { newAWSSSMClient = original })

	_, err := getAWSOptionParam(context.Background(), aws.Config{Region: "us-east-1"}, "/missing")
	if !errors.Is(err, ErrBackendNotFound) {
		t.Fatalf("expected ErrBackendNotFound, got: %v", err)
	}
}

// TestGetAWSOptionSecret_Found proves a successful Secrets Manager
// GetSecretValue response is unmarshaled into the returned string.
func TestGetAWSOptionSecret_Found(t *testing.T) {
	srv := awsJSONServer(t, http.StatusOK, `{"Name":"db","SecretString":"secret-value"}`)
	cfg, err := buildAWSConfig(context.Background(), AWSConfig{Region: "us-east-1", Endpoint: srv.URL, SkipAuth: true})
	if err != nil {
		t.Fatalf("buildAWSConfig failed: %v", err)
	}
	val, err := getAWSOptionSecret(context.Background(), cfg, "db")
	if err != nil {
		t.Fatalf("getAWSOptionSecret failed: %v", err)
	}
	if val != "secret-value" {
		t.Fatalf("expected \"secret-value\", got %q", val)
	}
}

// TestGetAWSOptionSecret_NotFound proves a Secrets Manager
// ResourceNotFoundException maps to ErrBackendNotFound.
func TestGetAWSOptionSecret_NotFound(t *testing.T) {
	srv := awsJSONServer(t, http.StatusBadRequest, `{"__type":"ResourceNotFoundException","message":"not found"}`)
	cfg, err := buildAWSConfig(context.Background(), AWSConfig{Region: "us-east-1", Endpoint: srv.URL, SkipAuth: true})
	if err != nil {
		t.Fatalf("buildAWSConfig failed: %v", err)
	}
	_, err = getAWSOptionSecret(context.Background(), cfg, "missing")
	if err == nil {
		t.Fatal("expected an error for a missing secret, got nil")
	}
	if !errors.Is(err, ErrBackendNotFound) {
		t.Fatalf("expected ErrBackendNotFound, got: %v", err)
	}
}

// TestGetAWSOptionSecret_NotFoundViaFake mirrors
// TestGetAWSOptionParam_NotFoundViaFake for Secrets Manager.
func TestGetAWSOptionSecret_NotFoundViaFake(t *testing.T) {
	original := newAWSSecretsClient
	fake := &awsfakes.FakeSecretsManagerClient{
		GetSecretValueFn: func(context.Context, *secretsmanager.GetSecretValueInput, ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
			return nil, &smtypes.ResourceNotFoundException{Message: aws.String("not found")}
		},
	}
	newAWSSecretsClient = func(aws.Config) awsSecretsAPI { return fake }
	t.Cleanup(func() { newAWSSecretsClient = original })

	_, err := getAWSOptionSecret(context.Background(), aws.Config{Region: "us-east-1"}, "missing")
	if !errors.Is(err, ErrBackendNotFound) {
		t.Fatalf("expected ErrBackendNotFound, got: %v", err)
	}
}

// TestAWSConfigStoreFor_SharesOneStoreAcrossBothNames proves one WithAWS
// call configures both "awsparam" and "awssecret" registry entries from a
// single underlying store, rather than requiring two separate calls.
func TestAWSConfigStoreFor_SharesOneStoreAcrossBothNames(t *testing.T) {
	opts := &EngineOptions{}
	WithAWS(AWSConfig{Region: "us-east-1"})(opts)

	param, ok := opts.Backends["awsparam"].(*awsOptionBackend)
	if !ok {
		t.Fatalf("expected an *awsOptionBackend under \"awsparam\", got %T", opts.Backends["awsparam"])
	}
	secret, ok := opts.Backends["awssecret"].(*awsOptionBackend)
	if !ok {
		t.Fatalf("expected an *awsOptionBackend under \"awssecret\", got %T", opts.Backends["awssecret"])
	}
	if param.store != secret.store {
		t.Fatal("expected \"awsparam\" and \"awssecret\" to share one *awsConfigStore")
	}
	if param.kind != "awsparam" || secret.kind != "awssecret" {
		t.Fatalf("expected kinds to match their registry names, got param.kind=%q secret.kind=%q", param.kind, secret.kind)
	}
}

// TestWithAWSTarget_EmptyNameIsNoOp mirrors
// TestWithVaultTarget_EmptyNameIsNoOp for the AWS backends.
func TestWithAWSTarget_EmptyNameIsNoOp(t *testing.T) {
	opts := &EngineOptions{}
	WithAWSTarget("", AWSConfig{Region: "us-east-1"})(opts)
	if opts.Backends != nil {
		t.Fatalf("expected WithAWSTarget(\"\", ...) to register nothing, got: %#v", opts.Backends)
	}
}

// TestAWSOptionBackend_UnconfiguredTargetIsAConfigurationError mirrors
// TestWithVaultTarget_UnconfiguredTargetIsAConfigurationError.
func TestAWSOptionBackend_UnconfiguredTargetIsAConfigurationError(t *testing.T) {
	store := newAWSConfigStore()
	store.setConfig("", AWSConfig{Region: "us-east-1"})
	b := &awsOptionBackend{store: store, kind: "awsparam"}

	_, err := b.GetWithTarget(context.Background(), "staging", "/x")
	if err == nil {
		t.Fatal("expected an error for an unconfigured target, got nil")
	}
	if errors.Is(err, ErrBackendNotFound) {
		t.Fatalf("expected a configuration error, not ErrBackendNotFound: %v", err)
	}
}

// TestAWSOptionBackend_GetBatch mirrors TestVaultOptionBackend_GetBatch.
func TestAWSOptionBackend_GetBatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		_, _ = w.Write([]byte(`{"Parameter":{"Name":"/a","Value":"va"}}`))
	}))
	defer srv.Close()

	store := newAWSConfigStore()
	store.setConfig("", AWSConfig{Region: "us-east-1", Endpoint: srv.URL, SkipAuth: true})
	b := &awsOptionBackend{store: store, kind: "awsparam"}

	got, err := b.GetBatch(context.Background(), []string{"/a"})
	if err != nil {
		t.Fatalf("GetBatch failed: %v", err)
	}
	if got["/a"] != "va" {
		t.Fatalf("expected {\"/a\": \"va\"}, got %#v", got)
	}
}

// TestAWSOptionBackend_HealthUsesDefaultTarget mirrors
// TestVaultOptionBackend_HealthUsesDefaultTarget.
func TestAWSOptionBackend_HealthUsesDefaultTarget(t *testing.T) {
	store := newAWSConfigStore()
	store.setConfig("staging", AWSConfig{Region: "us-east-1"})
	b := &awsOptionBackend{store: store, kind: "awsparam"}

	if err := b.Health(context.Background()); err == nil {
		t.Fatal("expected Health to fail with no default (\"\") target configured, got nil")
	}
}

// TestAWSOptionBackend_HealthCallsSTS proves Health calls
// sts:GetCallerIdentity through the newAWSSTSClient factory (rather than
// hardcoding sts.NewFromConfig), so a fake swapped in for that factory
// observes exactly one call.
func TestAWSOptionBackend_HealthCallsSTS(t *testing.T) {
	original := newAWSSTSClient
	fake := &awsfakes.FakeSTSClient{}
	newAWSSTSClient = func(aws.Config) awsSTSAPI { return fake }
	t.Cleanup(func() { newAWSSTSClient = original })

	store := newAWSConfigStore()
	store.setConfig("", AWSConfig{Region: "us-east-1"})
	b := &awsOptionBackend{store: store, kind: "awsparam"}

	if err := b.Health(context.Background()); err != nil {
		t.Fatalf("Health failed: %v", err)
	}
	if fake.CallCount() != 1 {
		t.Fatalf("expected exactly one GetCallerIdentity call, got %d", fake.CallCount())
	}
}

// TestAWSOptionBackend_Close proves Close is a benign no-op.
func TestAWSOptionBackend_Close(t *testing.T) {
	b := &awsOptionBackend{store: newAWSConfigStore(), kind: "awsparam"}
	if err := b.Close(); err != nil {
		t.Fatalf("expected Close to always succeed, got: %v", err)
	}
}

// TestAWSConfigStoreFor_ReplacesNonAWSBackend mirrors
// TestVaultOptionBackendFor_ReplacesNonVaultBackend.
type fakeAWSParamBackend struct{}

func (fakeAWSParamBackend) Name() string { return "awsparam" }
func (fakeAWSParamBackend) Get(context.Context, string) (interface{}, error) {
	return nil, ErrBackendNotFound
}
func (fakeAWSParamBackend) GetBatch(context.Context, []string) (map[string]interface{}, error) {
	return nil, nil
}
func (fakeAWSParamBackend) Health(context.Context) error { return nil }
func (fakeAWSParamBackend) Close() error                 { return nil }

func TestAWSConfigStoreFor_ReplacesNonAWSBackend(t *testing.T) {
	opts := &EngineOptions{}
	WithBackend(fakeAWSParamBackend{})(opts)
	WithAWS(AWSConfig{Region: "us-east-1"})(opts)

	if _, ok := opts.Backends["awsparam"].(*awsOptionBackend); !ok {
		t.Fatalf("expected WithAWS to replace the earlier WithBackend registration, got %T", opts.Backends["awsparam"])
	}
	if _, ok := opts.Backends["awssecret"].(*awsOptionBackend); !ok {
		t.Fatalf("expected WithAWS to also register \"awssecret\", got %T", opts.Backends["awssecret"])
	}
}

// TestAWSConfigStoreFor_AsymmetricNonAWSBackendIsNotReplaced pins M6
// (.agents/work/20260812-wave-c/phase3-review.md): when exactly one of
// "awsparam"/"awssecret" is already an *awsOptionBackend,
// awsConfigStoreFor returns that shared store as-is and does not touch
// either name - in particular it does not replace the other name's
// explicit WithBackend registration, contrary to what an earlier version
// of this function's doc comment claimed.
func TestAWSConfigStoreFor_AsymmetricNonAWSBackendIsNotReplaced(t *testing.T) {
	opts := &EngineOptions{}
	WithAWS(AWSConfig{Region: "us-east-1"})(opts)
	WithBackend(fakeAWSParamBackend{})(opts) // replaces only "awsparam"
	WithAWSTarget("prod", AWSConfig{Region: "us-west-2"})(opts)

	awsparam := opts.Backends["awsparam"]
	if _, ok := awsparam.(fakeAWSParamBackend); !ok {
		t.Fatalf("expected \"awsparam\" to remain the explicit WithBackend registration, got %T", awsparam)
	}

	awssecret, ok := opts.Backends["awssecret"].(*awsOptionBackend)
	if !ok {
		t.Fatalf("expected \"awssecret\" to remain an *awsOptionBackend, got %T", opts.Backends["awssecret"])
	}
	if _, ok := awssecret.store.configs["prod"]; !ok {
		t.Fatal("expected WithAWSTarget(\"prod\", ...) to configure the original store reached through \"awssecret\"")
	}
}

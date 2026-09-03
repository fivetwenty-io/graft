package aws_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/service/ssm"

	awsbackend "github.com/fivetwenty-io/graft/internal/backends/aws"
)

// hermeticizeAWSEnv neutralizes every AWS credential/config source
// config.LoadDefaultConfig consults, and disables EC2 IMDS probing, so a
// test exercising BuildConfig/InitializeConfig is deterministic and fast
// regardless of the machine or user account it runs under - mirrors
// pkg/graft/operators/op_aws_backend_test.go's hermeticizeAWSEnv.
func hermeticizeAWSEnv(t *testing.T) {
	t.Helper()
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("HOME", t.TempDir())
}

// targetCounter hands out unique-ish target names so tests sharing the
// package-level DefaultPool (which caches by target name) never collide
// with cache entries left by other tests or a concurrently running test
// binary that also touches DefaultPool.
var targetCounter int64

func uniqueTarget(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, atomic.AddInt64(&targetCounter, 1))
}

func TestGetTargetConfig_ReadsPrefixedEnv(t *testing.T) {
	hermeticizeAWSEnv(t)
	target := uniqueTarget("read-prefixed-env")
	envPrefix := "AWS_" + strings.ToUpper(target) + "_"

	t.Setenv(envPrefix+"REGION", "us-west-2")
	t.Setenv(envPrefix+"PROFILE", "myprofile")
	t.Setenv(envPrefix+"ROLE", "arn:aws:iam::123456789012:role/myrole")
	t.Setenv(envPrefix+"ACCESS_KEY_ID", "AKIAEXAMPLE")
	t.Setenv(envPrefix+"SECRET_ACCESS_KEY", "secret")
	t.Setenv(envPrefix+"SESSION_TOKEN", "token")
	t.Setenv(envPrefix+"ENDPOINT", "http://127.0.0.1:1")
	t.Setenv(envPrefix+"DISABLE_SSL", "true")
	t.Setenv(envPrefix+"MAX_RETRIES", "5")
	t.Setenv(envPrefix+"EXTERNAL_ID", "ext-id")
	t.Setenv(envPrefix+"SESSION_NAME", "my-session")
	t.Setenv(envPrefix+"MFA_SERIAL", "arn:aws:iam::123456789012:mfa/user")

	cfg, err := awsbackend.DefaultPool.GetTargetConfig(target)
	if err != nil {
		t.Fatalf("GetTargetConfig failed: %v", err)
	}
	if cfg.Region != "us-west-2" {
		t.Errorf("Region = %q, want %q", cfg.Region, "us-west-2")
	}
	if cfg.Profile != "myprofile" {
		t.Errorf("Profile = %q, want %q", cfg.Profile, "myprofile")
	}
	if cfg.Role != "arn:aws:iam::123456789012:role/myrole" {
		t.Errorf("Role = %q, want the configured role ARN", cfg.Role)
	}
	if cfg.AccessKeyID != "AKIAEXAMPLE" || cfg.SecretAccessKey != "secret" || cfg.SessionToken != "token" {
		t.Errorf("static credentials = (%q, %q, %q), want (AKIAEXAMPLE, secret, token)", cfg.AccessKeyID, cfg.SecretAccessKey, cfg.SessionToken)
	}
	if cfg.Endpoint != "http://127.0.0.1:1" {
		t.Errorf("Endpoint = %q, want %q", cfg.Endpoint, "http://127.0.0.1:1")
	}
	if !cfg.DisableSSL {
		t.Error("expected DisableSSL to be true")
	}
	if cfg.MaxRetries != 5 {
		t.Errorf("MaxRetries = %d, want 5", cfg.MaxRetries)
	}
	if cfg.ExternalID != "ext-id" {
		t.Errorf("ExternalID = %q, want %q", cfg.ExternalID, "ext-id")
	}
	if cfg.SessionName != "my-session" {
		t.Errorf("SessionName = %q, want %q", cfg.SessionName, "my-session")
	}
	if cfg.MfaSerial != "arn:aws:iam::123456789012:mfa/user" {
		t.Errorf("MfaSerial = %q, want the configured MFA serial", cfg.MfaSerial)
	}
}

func TestGetTargetConfig_RequiresOneOfFour(t *testing.T) {
	hermeticizeAWSEnv(t)
	target := uniqueTarget("requires-one-of-four")

	_, err := awsbackend.DefaultPool.GetTargetConfig(target)
	if err == nil {
		t.Fatal("expected an error when none of REGION/PROFILE/ROLE/ACCESS_KEY_ID is set")
	}
}

func TestBuildConfig_RegionEndpointRetries(t *testing.T) {
	hermeticizeAWSEnv(t)
	ctx := context.Background()

	cfg, err := awsbackend.DefaultPool.BuildConfig(ctx, &awsbackend.Target{
		Region:      "us-west-2",
		Endpoint:    "http://127.0.0.1:1",
		MaxRetries:  4,
		AccessKeyID: "AKIAEXAMPLE", SecretAccessKey: "secret",
	})
	if err != nil {
		t.Fatalf("BuildConfig failed: %v", err)
	}
	if cfg.Region != "us-west-2" {
		t.Errorf("Region = %q, want %q", cfg.Region, "us-west-2")
	}
	if got := aws.ToString(cfg.BaseEndpoint); got != "http://127.0.0.1:1" {
		t.Errorf("BaseEndpoint = %q, want %q", got, "http://127.0.0.1:1")
	}
	// MaxRetries=4 (v1 "retries" semantics) maps to RetryMaxAttempts=5
	// (v2 "total attempts" semantics) - see BuildConfig's doc comment.
	if cfg.RetryMaxAttempts != 5 {
		t.Errorf("RetryMaxAttempts = %d, want 5 (MaxRetries+1)", cfg.RetryMaxAttempts)
	}
}

func TestBuildConfig_NonPositiveMaxRetriesLeavesSDKDefault(t *testing.T) {
	hermeticizeAWSEnv(t)
	ctx := context.Background()

	cfg, err := awsbackend.DefaultPool.BuildConfig(ctx, &awsbackend.Target{
		Region: "us-east-1", MaxRetries: 0,
		AccessKeyID: "AKIAEXAMPLE", SecretAccessKey: "secret",
	})
	if err != nil {
		t.Fatalf("BuildConfig failed: %v", err)
	}
	if cfg.RetryMaxAttempts != 0 {
		t.Errorf("RetryMaxAttempts = %d, want 0 (unset, SDK default takes over)", cfg.RetryMaxAttempts)
	}
}

// TestBuildConfig_HTTPTimeoutSetsClientTimeout proves a positive
// HTTPTimeout reaches the resulting config's HTTPClient as an
// *awshttp.BuildableClient whose configured timeout matches - the fast,
// deterministic half of proving HTTPTimeout is honored (see
// TestBuildConfig_HTTPTimeoutReachesClient below for the functional half:
// a slow server actually timing out).
func TestBuildConfig_HTTPTimeoutSetsClientTimeout(t *testing.T) {
	hermeticizeAWSEnv(t)
	ctx := context.Background()

	cfg, err := awsbackend.DefaultPool.BuildConfig(ctx, &awsbackend.Target{
		Region:      "us-east-1",
		HTTPTimeout: 50 * time.Millisecond,
		AccessKeyID: "AKIAEXAMPLE", SecretAccessKey: "secret",
	})
	if err != nil {
		t.Fatalf("BuildConfig failed: %v", err)
	}
	bc, ok := cfg.HTTPClient.(*awshttp.BuildableClient)
	if !ok {
		t.Fatalf("expected an *awshttp.BuildableClient, got %T", cfg.HTTPClient)
	}
	if got := bc.GetTimeout(); got != 50*time.Millisecond {
		t.Errorf("BuildableClient timeout = %v, want 50ms", got)
	}
}

// TestBuildConfig_ZeroHTTPTimeoutLeavesClientUnset proves a non-positive
// HTTPTimeout (Target's zero value) leaves cfg.HTTPClient unset, so the
// SDK's own default HTTP client - which has no client-side deadline -
// remains in effect, matching the documented "only apply when
// HTTPTimeout > 0" behavior.
func TestBuildConfig_ZeroHTTPTimeoutLeavesClientUnset(t *testing.T) {
	hermeticizeAWSEnv(t)
	ctx := context.Background()

	cfg, err := awsbackend.DefaultPool.BuildConfig(ctx, &awsbackend.Target{
		Region:      "us-east-1",
		HTTPTimeout: 0,
		AccessKeyID: "AKIAEXAMPLE", SecretAccessKey: "secret",
	})
	if err != nil {
		t.Fatalf("BuildConfig failed: %v", err)
	}
	if cfg.HTTPClient != nil {
		t.Errorf("expected HTTPClient to stay unset for HTTPTimeout=0, got %T", cfg.HTTPClient)
	}
}

// TestBuildConfig_HTTPTimeoutReachesClient is the functional proof that
// HTTPTimeout is not just recorded on the config but actually enforced:
// a GetParameter call against a server that sleeps well past the
// configured timeout must fail. MaxRetries is pinned to 1 (two total
// attempts) so a timing-flaky retry storm cannot turn this into a slow
// test.
func TestBuildConfig_HTTPTimeoutReachesClient(t *testing.T) {
	hermeticizeAWSEnv(t)
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"Parameter":{"Value":"too-late"}}`))
	}))
	t.Cleanup(srv.Close)

	cfg, err := awsbackend.DefaultPool.BuildConfig(ctx, &awsbackend.Target{
		Region:      "us-east-1",
		Endpoint:    srv.URL,
		HTTPTimeout: 50 * time.Millisecond,
		MaxRetries:  1,
		AccessKeyID: "AKIAEXAMPLE", SecretAccessKey: "secret",
	})
	if err != nil {
		t.Fatalf("BuildConfig failed: %v", err)
	}

	client := awsbackend.NewSSMClient(cfg)
	_, err = client.GetParameter(ctx, &ssm.GetParameterInput{Name: aws.String("/x")})
	if err == nil {
		t.Fatal("expected GetParameter to fail once HTTPTimeout elapses, got nil error")
	}
}

func TestBuildConfig_DisableSSLRewritesScheme(t *testing.T) {
	hermeticizeAWSEnv(t)
	ctx := context.Background()

	cfg, err := awsbackend.DefaultPool.BuildConfig(ctx, &awsbackend.Target{
		Region: "us-east-1", Endpoint: "https://localstack.example.com", DisableSSL: true,
		AccessKeyID: "AKIAEXAMPLE", SecretAccessKey: "secret",
	})
	if err != nil {
		t.Fatalf("BuildConfig failed: %v", err)
	}
	if got := aws.ToString(cfg.BaseEndpoint); got != "http://localstack.example.com" {
		t.Errorf("BaseEndpoint = %q, want the https:// scheme rewritten to http://", got)
	}

	cfgNoEndpoint, err := awsbackend.DefaultPool.BuildConfig(ctx, &awsbackend.Target{
		Region: "us-east-1", DisableSSL: true,
		AccessKeyID: "AKIAEXAMPLE", SecretAccessKey: "secret",
	})
	if err != nil {
		t.Fatalf("BuildConfig failed: %v", err)
	}
	if cfgNoEndpoint.BaseEndpoint != nil {
		t.Errorf("expected DisableSSL with no Endpoint to be a no-op, got BaseEndpoint=%q", aws.ToString(cfgNoEndpoint.BaseEndpoint))
	}
}

func TestBuildConfig_HTTPEndpointDisableSSLFalseIsUnchanged(t *testing.T) {
	hermeticizeAWSEnv(t)
	ctx := context.Background()

	cfg, err := awsbackend.DefaultPool.BuildConfig(ctx, &awsbackend.Target{
		Region: "us-east-1", Endpoint: "http://127.0.0.1:1", DisableSSL: false,
		AccessKeyID: "AKIAEXAMPLE", SecretAccessKey: "secret",
	})
	if err != nil {
		t.Fatalf("BuildConfig failed: %v", err)
	}
	if got := aws.ToString(cfg.BaseEndpoint); got != "http://127.0.0.1:1" {
		t.Errorf("BaseEndpoint = %q, want the endpoint passed through verbatim", got)
	}
}

func TestBuildConfig_StaticCredentials(t *testing.T) {
	hermeticizeAWSEnv(t)
	ctx := context.Background()

	cfg, err := awsbackend.DefaultPool.BuildConfig(ctx, &awsbackend.Target{
		Region:          "us-east-1",
		AccessKeyID:     "AKIAEXAMPLE",
		SecretAccessKey: "secret",
		SessionToken:    "token",
	})
	if err != nil {
		t.Fatalf("BuildConfig failed: %v", err)
	}
	creds, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		t.Fatalf("Credentials.Retrieve failed: %v", err)
	}
	if creds.AccessKeyID != "AKIAEXAMPLE" || creds.SecretAccessKey != "secret" || creds.SessionToken != "token" {
		t.Fatalf("expected the configured static credentials, got %+v", creds)
	}
}

// awsAssumeRoleServer returns an httptest server standing in for STS,
// recording every request's parsed form body and replying with a fixed,
// far-future-expiring AssumeRoleResponse so a *aws.CredentialsCache never
// needs to refresh mid-test.
func awsAssumeRoleServer(t *testing.T) (*httptest.Server, *int32, *[]url.Values) {
	t.Helper()
	var callCount int32
	var forms []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		forms = append(forms, form)

		w.Header().Set("Content-Type", "text/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<AssumeRoleResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/">
  <AssumeRoleResult>
    <Credentials>
      <AccessKeyId>AKIAASSUMEDROLE</AccessKeyId>
      <SecretAccessKey>assumed-secret</SecretAccessKey>
      <SessionToken>assumed-token</SessionToken>
      <Expiration>2099-01-01T00:00:00Z</Expiration>
    </Credentials>
    <AssumedRoleUser>
      <AssumedRoleId>AROAEXAMPLE:test-session</AssumedRoleId>
      <Arn>arn:aws:sts::123456789012:assumed-role/test-role/test-session</Arn>
    </AssumedRoleUser>
  </AssumeRoleResult>
  <ResponseMetadata>
    <RequestId>test-request-id</RequestId>
  </ResponseMetadata>
</AssumeRoleResponse>`))
	}))
	t.Cleanup(srv.Close)
	return srv, &callCount, &forms
}

func TestBuildConfig_RoleWrapsProviderInCache(t *testing.T) {
	hermeticizeAWSEnv(t)
	ctx := context.Background()
	srv, _, _ := awsAssumeRoleServer(t)

	cfg, err := awsbackend.DefaultPool.BuildConfig(ctx, &awsbackend.Target{
		Region: "us-east-1", Endpoint: srv.URL,
		AccessKeyID: "AKIAEXAMPLE", SecretAccessKey: "secret",
		Role: "arn:aws:iam::123456789012:role/test-role",
	})
	if err != nil {
		t.Fatalf("BuildConfig failed: %v", err)
	}
	if _, ok := cfg.Credentials.(*aws.CredentialsCache); !ok {
		t.Fatalf("expected role assumption to wrap the provider in *aws.CredentialsCache, got %T", cfg.Credentials)
	}
}

func TestBuildConfig_RoleAssumptionSendsOptionsAndCaches(t *testing.T) {
	hermeticizeAWSEnv(t)
	ctx := context.Background()
	srv, callCount, forms := awsAssumeRoleServer(t)

	cfg, err := awsbackend.DefaultPool.BuildConfig(ctx, &awsbackend.Target{
		Region: "us-east-1", Endpoint: srv.URL,
		AccessKeyID: "AKIAEXAMPLE", SecretAccessKey: "secret",
		Role:               "arn:aws:iam::123456789012:role/test-role",
		ExternalID:         "ext-id-123",
		SessionName:        "graft-test-session",
		AssumeRoleDuration: 30 * time.Minute,
	})
	if err != nil {
		t.Fatalf("BuildConfig failed: %v", err)
	}

	if _, err := cfg.Credentials.Retrieve(ctx); err != nil {
		t.Fatalf("first Credentials.Retrieve failed: %v", err)
	}
	if atomic.LoadInt32(callCount) != 1 {
		t.Fatalf("expected exactly one STS call after the first Retrieve, got %d", atomic.LoadInt32(callCount))
	}

	form := (*forms)[0]
	if got := form.Get("RoleArn"); got != "arn:aws:iam::123456789012:role/test-role" {
		t.Errorf("RoleArn = %q, want the configured role ARN", got)
	}
	if got := form.Get("ExternalId"); got != "ext-id-123" {
		t.Errorf("ExternalId = %q, want %q", got, "ext-id-123")
	}
	if got := form.Get("RoleSessionName"); got != "graft-test-session" {
		t.Errorf("RoleSessionName = %q, want %q", got, "graft-test-session")
	}
	if got := form.Get("DurationSeconds"); got != "1800" {
		t.Errorf("DurationSeconds = %q, want %q (30m)", got, "1800")
	}

	// A second Retrieve must be served from the CredentialsCache, not a
	// second STS call - this is the CredentialsCache omission risk the
	// port's doc comments call out.
	if _, err := cfg.Credentials.Retrieve(ctx); err != nil {
		t.Fatalf("second Credentials.Retrieve failed: %v", err)
	}
	if atomic.LoadInt32(callCount) != 1 {
		t.Fatalf("expected the second Retrieve to be served from cache (still 1 STS call), got %d calls", atomic.LoadInt32(callCount))
	}
}

func TestGetConfig_CachesPerTarget(t *testing.T) {
	hermeticizeAWSEnv(t)
	ctx := context.Background()
	target := uniqueTarget("get-config-caches")
	envPrefix := "AWS_" + strings.ToUpper(target) + "_"
	t.Setenv(envPrefix+"REGION", "us-east-1")

	cfg1, err := awsbackend.DefaultPool.GetConfig(ctx, target)
	if err != nil {
		t.Fatalf("first GetConfig failed: %v", err)
	}
	cfg2, err := awsbackend.DefaultPool.GetConfig(ctx, target)
	if err != nil {
		t.Fatalf("second GetConfig failed: %v", err)
	}
	if cfg1.Region != cfg2.Region {
		t.Fatalf("expected both calls to return the same cached config, got regions %q and %q", cfg1.Region, cfg2.Region)
	}
}

func TestGetSecretsManagerClient_UsesFactory(t *testing.T) {
	hermeticizeAWSEnv(t)
	ctx := context.Background()
	target := uniqueTarget("get-secretsmanager-client-factory")
	envPrefix := "AWS_" + strings.ToUpper(target) + "_"
	t.Setenv(envPrefix+"REGION", "us-east-1")

	client1, err := awsbackend.DefaultPool.GetSecretsManagerClient(ctx, target)
	if err != nil {
		t.Fatalf("first GetSecretsManagerClient failed: %v", err)
	}
	client2, err := awsbackend.DefaultPool.GetSecretsManagerClient(ctx, target)
	if err != nil {
		t.Fatalf("second GetSecretsManagerClient failed: %v", err)
	}
	if client1 != client2 {
		t.Fatal("expected GetSecretsManagerClient to cache and return the same client for the same target")
	}
}

func TestGetParameterStoreClient_UsesFactory(t *testing.T) {
	hermeticizeAWSEnv(t)
	ctx := context.Background()
	target := uniqueTarget("get-parameterstore-client-factory")
	envPrefix := "AWS_" + strings.ToUpper(target) + "_"
	t.Setenv(envPrefix+"REGION", "us-east-1")

	client1, err := awsbackend.DefaultPool.GetParameterStoreClient(ctx, target)
	if err != nil {
		t.Fatalf("first GetParameterStoreClient failed: %v", err)
	}
	client2, err := awsbackend.DefaultPool.GetParameterStoreClient(ctx, target)
	if err != nil {
		t.Fatalf("second GetParameterStoreClient failed: %v", err)
	}
	if client1 != client2 {
		t.Fatal("expected GetParameterStoreClient to cache and return the same client for the same target")
	}
}

func TestInitializeConfig_PlainRegionAndProfile(t *testing.T) {
	hermeticizeAWSEnv(t)
	ctx := context.Background()

	cfg, err := awsbackend.InitializeConfig(ctx, "", "us-east-1", "")
	if err != nil {
		t.Fatalf("InitializeConfig failed: %v", err)
	}
	if cfg.Region != "us-east-1" {
		t.Fatalf("Region = %q, want %q", cfg.Region, "us-east-1")
	}
}

func TestInitializeConfig_RoleWrapsProviderInCache(t *testing.T) {
	hermeticizeAWSEnv(t)
	ctx := context.Background()
	srv, _, _ := awsAssumeRoleServer(t)
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIAEXAMPLE")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secret")
	t.Setenv("AWS_ENDPOINT_URL_STS", srv.URL)

	cfg, err := awsbackend.InitializeConfig(ctx, "", "us-east-1", "arn:aws:iam::123456789012:role/test-role")
	if err != nil {
		t.Fatalf("InitializeConfig failed: %v", err)
	}
	if _, ok := cfg.Credentials.(*aws.CredentialsCache); !ok {
		t.Fatalf("expected role assumption to wrap the provider in *aws.CredentialsCache, got %T", cfg.Credentials)
	}
}

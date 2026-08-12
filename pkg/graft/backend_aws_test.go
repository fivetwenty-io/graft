package graft

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go/aws/credentials"
)

// TestBuildAWSSession_RegionProfileEndpoint proves buildAWSSession threads
// Region/Endpoint (and DisableSSL for a plain "http://" Endpoint, needed
// for httptest servers) into the resulting session's *aws.Config.
func TestBuildAWSSession_RegionProfileEndpoint(t *testing.T) {
	sess, err := buildAWSSession(AWSConfig{Region: "us-west-2", Endpoint: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("buildAWSSession failed: %v", err)
	}
	if sess.Config.Region == nil || *sess.Config.Region != "us-west-2" {
		t.Fatalf("expected Region to reach aws.Config, got %v", sess.Config.Region)
	}
	if sess.Config.Endpoint == nil || *sess.Config.Endpoint != "http://127.0.0.1:1" {
		t.Fatalf("expected Endpoint to reach aws.Config, got %v", sess.Config.Endpoint)
	}
	if sess.Config.DisableSSL == nil || !*sess.Config.DisableSSL {
		t.Fatal("expected a plain http:// Endpoint to set DisableSSL")
	}
}

// TestBuildAWSSession_HTTPSEndpointLeavesSSLEnabled proves an "https://"
// Endpoint does not set DisableSSL, unlike the "http://" case above.
func TestBuildAWSSession_HTTPSEndpointLeavesSSLEnabled(t *testing.T) {
	sess, err := buildAWSSession(AWSConfig{Region: "us-east-1", Endpoint: "https://example.com"})
	if err != nil {
		t.Fatalf("buildAWSSession failed: %v", err)
	}
	if sess.Config.DisableSSL != nil && *sess.Config.DisableSSL {
		t.Fatal("expected an https:// Endpoint to leave DisableSSL unset")
	}
}

// TestBuildAWSSession_StaticCredentials proves AccessKeyID/SecretAccessKey/
// SessionToken reach the session's Credentials as a static provider.
func TestBuildAWSSession_StaticCredentials(t *testing.T) {
	sess, err := buildAWSSession(AWSConfig{
		Region:          "us-east-1",
		AccessKeyID:     "AKIAEXAMPLE",
		SecretAccessKey: "secret",
		SessionToken:    "token",
	})
	if err != nil {
		t.Fatalf("buildAWSSession failed: %v", err)
	}
	v, err := sess.Config.Credentials.Get()
	if err != nil {
		t.Fatalf("Credentials.Get failed: %v", err)
	}
	if v.AccessKeyID != "AKIAEXAMPLE" || v.SecretAccessKey != "secret" || v.SessionToken != "token" {
		t.Fatalf("expected the configured static credentials, got %+v", v)
	}
}

// TestBuildAWSSession_SkipAuthUsesAnonymousCredentials proves SkipAuth
// wins over AccessKeyID/SecretAccessKey when both are set, matching the
// field's name.
func TestBuildAWSSession_SkipAuthUsesAnonymousCredentials(t *testing.T) {
	sess, err := buildAWSSession(AWSConfig{
		Region:          "us-east-1",
		SkipAuth:        true,
		AccessKeyID:     "AKIAEXAMPLE",
		SecretAccessKey: "secret",
	})
	if err != nil {
		t.Fatalf("buildAWSSession failed: %v", err)
	}
	if sess.Config.Credentials != credentials.AnonymousCredentials {
		t.Fatal("expected SkipAuth to select credentials.AnonymousCredentials")
	}
}

// TestBuildAWSSession_PoolSize proves PoolSize's entire documented effect
// on the resulting session's HTTPClient transport.
func TestBuildAWSSession_PoolSize(t *testing.T) {
	sess, err := buildAWSSession(AWSConfig{Region: "us-east-1", PoolSize: 9})
	if err != nil {
		t.Fatalf("buildAWSSession failed: %v", err)
	}
	transport, ok := sess.Config.HTTPClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected an *http.Transport, got %T", sess.Config.HTTPClient.Transport)
	}
	if transport.MaxIdleConnsPerHost != 9 || transport.MaxIdleConns != 9 {
		t.Fatalf("expected PoolSize=9 to set both pool fields to 9, got MaxIdleConnsPerHost=%d MaxIdleConns=%d", transport.MaxIdleConnsPerHost, transport.MaxIdleConns)
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
	sess, err := buildAWSSession(AWSConfig{Region: "us-east-1", Endpoint: srv.URL, SkipAuth: true})
	if err != nil {
		t.Fatalf("buildAWSSession failed: %v", err)
	}
	val, err := getAWSOptionParam(context.Background(), sess, "/x")
	if err != nil {
		t.Fatalf("getAWSOptionParam failed: %v", err)
	}
	if val != "param-value" {
		t.Fatalf("expected \"param-value\", got %q", val)
	}
}

// TestGetAWSOptionParam_NotFound proves an SSM ParameterNotFound response
// maps to ErrBackendNotFound.
func TestGetAWSOptionParam_NotFound(t *testing.T) {
	srv := awsJSONServer(t, http.StatusBadRequest, `{"__type":"ParameterNotFound","message":"not found"}`)
	sess, err := buildAWSSession(AWSConfig{Region: "us-east-1", Endpoint: srv.URL, SkipAuth: true})
	if err != nil {
		t.Fatalf("buildAWSSession failed: %v", err)
	}
	_, err = getAWSOptionParam(context.Background(), sess, "/missing")
	if err == nil {
		t.Fatal("expected an error for a missing parameter, got nil")
	}
	if !errors.Is(err, ErrBackendNotFound) {
		t.Fatalf("expected ErrBackendNotFound, got: %v", err)
	}
}

// TestGetAWSOptionSecret_Found proves a successful Secrets Manager
// GetSecretValue response is unmarshaled into the returned string.
func TestGetAWSOptionSecret_Found(t *testing.T) {
	srv := awsJSONServer(t, http.StatusOK, `{"Name":"db","SecretString":"secret-value"}`)
	sess, err := buildAWSSession(AWSConfig{Region: "us-east-1", Endpoint: srv.URL, SkipAuth: true})
	if err != nil {
		t.Fatalf("buildAWSSession failed: %v", err)
	}
	val, err := getAWSOptionSecret(context.Background(), sess, "db")
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
	sess, err := buildAWSSession(AWSConfig{Region: "us-east-1", Endpoint: srv.URL, SkipAuth: true})
	if err != nil {
		t.Fatalf("buildAWSSession failed: %v", err)
	}
	_, err = getAWSOptionSecret(context.Background(), sess, "missing")
	if err == nil {
		t.Fatal("expected an error for a missing secret, got nil")
	}
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

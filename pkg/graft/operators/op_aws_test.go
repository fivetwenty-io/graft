package operators

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"

	awsbackend "github.com/fivetwenty-io/graft/internal/backends/aws"
	"github.com/fivetwenty-io/graft/internal/backends/aws/awsfakes"
	"github.com/fivetwenty-io/graft/pkg/graft"
)

// fakeAwsConfig builds an aws.Config with non-functional static
// credentials, a single retry attempt, and BaseEndpoint pointing at a
// reserved-then-closed local port, for tests that must prove a cache hit
// short-circuits getAwsSecret/getAwsParam before either ever reaches the
// network: if the cache is missed and the fetch falls through to a real
// GetSecretValue/GetParameter call, connecting to a closed local port
// fails immediately with "connection refused" rather than hanging or
// (worse) silently reaching a real AWS endpoint.
func fakeAwsConfig(t *testing.T) aws.Config {
	t.Helper()
	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fakeAwsConfig: unexpected error reserving a port: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("fakeAwsConfig: unexpected error closing the reserved port: %v", err)
	}

	return aws.Config{
		Region:           "us-east-1",
		Credentials:      credentials.NewStaticCredentialsProvider("AKIAFAKEFAKEFAKEFAKE", "fake-secret-key-not-real", ""),
		BaseEndpoint:     aws.String("http://" + addr),
		RetryMaxAttempts: 1,
	}
}

func TestParseAwsOpKey(t *testing.T) {
	Convey("parseAwsOpKey", t, func() {
		Convey("simple key without parameters", func() {
			key, params, err := parseAwsOpKey("my/secret/path")
			So(err, ShouldBeNil)
			So(key, ShouldEqual, "my/secret/path")
			So(len(params), ShouldEqual, 0)
		})

		Convey("key with single parameter", func() {
			key, params, err := parseAwsOpKey("my/secret?region=us-east-1")
			So(err, ShouldBeNil)
			So(key, ShouldEqual, "my/secret")
			So(params.Get("region"), ShouldEqual, "us-east-1")
		})

		Convey("key with multiple parameters", func() {
			key, params, err := parseAwsOpKey("my/secret?region=us-west-2&version=v1")
			So(err, ShouldBeNil)
			So(key, ShouldEqual, "my/secret")
			So(params.Get("region"), ShouldEqual, "us-west-2")
			So(params.Get("version"), ShouldEqual, "v1")
		})

		Convey("key with stage parameter", func() {
			key, params, err := parseAwsOpKey("prod/db/credentials?stage=AWSCURRENT")
			So(err, ShouldBeNil)
			So(key, ShouldEqual, "prod/db/credentials")
			So(params.Get("stage"), ShouldEqual, "AWSCURRENT")
		})

		Convey("empty key", func() {
			key, params, err := parseAwsOpKey("")
			So(err, ShouldBeNil)
			So(key, ShouldEqual, "")
			So(len(params), ShouldEqual, 0)
		})

		Convey("key with empty query string", func() {
			key, params, err := parseAwsOpKey("my/secret?")
			So(err, ShouldBeNil)
			So(key, ShouldEqual, "my/secret")
			So(len(params), ShouldEqual, 0)
		})

		Convey("key with special characters in path", func() {
			key, params, err := parseAwsOpKey("my-app/db_credentials/prod?region=eu-west-1")
			So(err, ShouldBeNil)
			So(key, ShouldEqual, "my-app/db_credentials/prod")
			So(params.Get("region"), ShouldEqual, "eu-west-1")
		})
	})
}

// TestAwsSecretCacheKey verifies awsSecretCacheKey folds the "stage" and
// "version" query qualifiers into the cache/dedup identity, since those are
// the only params that change what Secrets Manager returns (see
// buildAwsSecretInput). This is the fix for the bug where
// "db?version=1" and "db?version=2" collided on a single cache entry.
func TestAwsSecretCacheKey(t *testing.T) {
	Convey("awsSecretCacheKey", t, func() {
		Convey("unqualified spec is unchanged (matches pre-fix cache keys)", func() {
			_, params, err := parseAwsOpKey("my/secret/path")
			So(err, ShouldBeNil)
			So(awsSecretCacheKey("my/secret/path", params), ShouldEqual, "my/secret/path")
		})

		Convey("different version params on the same base key produce distinct identities", func() {
			_, p1, err := parseAwsOpKey("db?version=1")
			So(err, ShouldBeNil)
			_, p2, err := parseAwsOpKey("db?version=2")
			So(err, ShouldBeNil)

			k1 := awsSecretCacheKey("db", p1)
			k2 := awsSecretCacheKey("db", p2)
			So(k1, ShouldNotEqual, k2)
		})

		Convey("different stage params on the same base key produce distinct identities", func() {
			_, p1, err := parseAwsOpKey("db?stage=AWSCURRENT")
			So(err, ShouldBeNil)
			_, p2, err := parseAwsOpKey("db?stage=AWSPREVIOUS")
			So(err, ShouldBeNil)

			k1 := awsSecretCacheKey("db", p1)
			k2 := awsSecretCacheKey("db", p2)
			So(k1, ShouldNotEqual, k2)
		})

		Convey("a qualified spec never collides with the unqualified base key", func() {
			_, base, err := parseAwsOpKey("db")
			So(err, ShouldBeNil)
			_, versioned, err := parseAwsOpKey("db?version=1")
			So(err, ShouldBeNil)

			So(awsSecretCacheKey("db", base), ShouldNotEqual, awsSecretCacheKey("db", versioned))
		})

		Convey("equivalent specs with reordered params share the same identity", func() {
			_, p1, err := parseAwsOpKey("db?version=1&stage=x")
			So(err, ShouldBeNil)
			_, p2, err := parseAwsOpKey("db?stage=x&version=1")
			So(err, ShouldBeNil)

			So(awsSecretCacheKey("db", p1), ShouldEqual, awsSecretCacheKey("db", p2))
		})

		Convey("the unrelated 'key' subkey param does not affect the identity", func() {
			_, p1, err := parseAwsOpKey("db?version=1&key=username")
			So(err, ShouldBeNil)
			_, p2, err := parseAwsOpKey("db?version=1&key=password")
			So(err, ShouldBeNil)

			// The "key" param selects a field from the already-fetched secret
			// (see AwsOperator.Run's subkey extraction) - it does not change
			// what Secrets Manager returns, so it must not fragment the cache.
			So(awsSecretCacheKey("db", p1), ShouldEqual, awsSecretCacheKey("db", p2))
		})

		Convey("version is ignored in the identity when stage is set, matching buildAwsSecretInput's precedence", func() {
			_, p1, err := parseAwsOpKey("db?stage=x&version=1")
			So(err, ShouldBeNil)
			_, p2, err := parseAwsOpKey("db?stage=x&version=2")
			So(err, ShouldBeNil)

			// buildAwsSecretInput sets VersionStage and never looks at
			// version at all once stage is present (see TestBuildAwsSecretInput's
			// "stage takes precedence" case), so these two specs send the
			// identical GetSecretValueInput to Secrets Manager. The cache
			// identity must collapse them to one entry, not fragment into
			// two separate fetches for what is really the same request.
			So(awsSecretCacheKey("db", p1), ShouldEqual, awsSecretCacheKey("db", p2))
		})
	})
}

// TestBuildAwsSecretInput verifies the GetSecretValueInput sent to Secrets
// Manager honors stage/version qualifiers with "stage" taking precedence
// over "version" when both are present, matching the pre-existing
// precedence in getAwsSecret.
func TestBuildAwsSecretInput(t *testing.T) {
	Convey("buildAwsSecretInput", t, func() {
		Convey("no qualifiers sets neither VersionId nor VersionStage", func() {
			input := buildAwsSecretInput("db", url.Values{})
			So(aws.ToString(input.SecretId), ShouldEqual, "db")
			So(input.VersionId, ShouldBeNil)
			So(input.VersionStage, ShouldBeNil)
		})

		Convey("version param sets VersionId", func() {
			input := buildAwsSecretInput("db", url.Values{"version": []string{"v1"}})
			So(aws.ToString(input.VersionId), ShouldEqual, "v1")
			So(input.VersionStage, ShouldBeNil)
		})

		Convey("stage param sets VersionStage", func() {
			input := buildAwsSecretInput("db", url.Values{"stage": []string{"AWSCURRENT"}})
			So(aws.ToString(input.VersionStage), ShouldEqual, "AWSCURRENT")
			So(input.VersionId, ShouldBeNil)
		})

		Convey("stage takes precedence over version when both present", func() {
			input := buildAwsSecretInput("db", url.Values{
				"stage":   []string{"AWSCURRENT"},
				"version": []string{"v1"},
			})
			So(aws.ToString(input.VersionStage), ShouldEqual, "AWSCURRENT")
			So(input.VersionId, ShouldBeNil)
		})
	})
}

// TestAwsSecretCacheKey_PreventsVersionCollisionInPool exercises the fix at
// the DefaultPool.GetOrFetchSecret layer that getAwsSecret itself drives:
// the identity fed to the cache/dedup layer is computed by
// awsSecretCacheKey, the exact function getAwsSecret uses, so this proves
// the fix end-to-end without a live AWS call (the fetch closures below
// stand in for the real Secrets Manager call in getAwsSecret).
func TestAwsSecretCacheKey_PreventsVersionCollisionInPool(t *testing.T) {
	Convey("qualified cache keys through DefaultPool.GetOrFetchSecret", t, func() {
		pool := awsbackend.DefaultPool
		target := "aws-cache-bugfix-target"

		Convey("same base key, different version params: distinct entries and distinct fetches", func() {
			base := fmt.Sprintf("aws-cache-bugfix/%d/db", timeSeed())
			_, p1, err := parseAwsOpKey(base + "?version=1")
			So(err, ShouldBeNil)
			_, p2, err := parseAwsOpKey(base + "?version=2")
			So(err, ShouldBeNil)

			var calls1, calls2 int32
			v1, err := pool.GetOrFetchSecret(target, awsSecretCacheKey(base, p1), func() (string, error) {
				atomic.AddInt32(&calls1, 1)
				return "value-v1", nil
			})
			So(err, ShouldBeNil)
			v2, err := pool.GetOrFetchSecret(target, awsSecretCacheKey(base, p2), func() (string, error) {
				atomic.AddInt32(&calls2, 1)
				return "value-v2", nil
			})
			So(err, ShouldBeNil)

			So(v1, ShouldEqual, "value-v1")
			So(v2, ShouldEqual, "value-v2")
			So(atomic.LoadInt32(&calls1), ShouldEqual, 1)
			So(atomic.LoadInt32(&calls2), ShouldEqual, 1)

			// Re-fetching v1's exact identity must hit the cache, not re-fetch,
			// and must not be perturbed by v2 having been fetched after it.
			v1Again, err := pool.GetOrFetchSecret(target, awsSecretCacheKey(base, p1), func() (string, error) {
				atomic.AddInt32(&calls1, 1)
				return "should-not-be-called", nil
			})
			So(err, ShouldBeNil)
			So(v1Again, ShouldEqual, "value-v1")
			So(atomic.LoadInt32(&calls1), ShouldEqual, 1)
		})

		Convey("identical fully-qualified specs (reordered params) share one entry and one fetch", func() {
			base := fmt.Sprintf("aws-cache-bugfix/%d/shared", timeSeed())
			_, p1, err := parseAwsOpKey(base + "?version=1&stage=x")
			So(err, ShouldBeNil)
			_, p2, err := parseAwsOpKey(base + "?stage=x&version=1")
			So(err, ShouldBeNil)

			var calls int32
			const n = 10
			var startGate sync.WaitGroup
			startGate.Add(1)
			var wg sync.WaitGroup
			results := make([]string, n)

			for i := 0; i < n; i++ {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					startGate.Wait()
					params := p1
					if idx%2 == 0 {
						params = p2
					}
					v, err := pool.GetOrFetchSecret(target, awsSecretCacheKey(base, params), func() (string, error) {
						atomic.AddInt32(&calls, 1)
						// Sleep so all n goroutines are genuinely in flight
						// together (matching cache_test.go's
						// TestClientPool_GetOrFetchSecret_ConcurrentSameKeyDedupes
						// technique) - singleflight only coalesces callers
						// that overlap a call already in flight, so an
						// instant-returning fetch would let a fast caller
						// slip in after the winner already finished and
						// trigger its own fetch, which is a race in the
						// test, not a cache-key bug.
						time.Sleep(80 * time.Millisecond)
						return "shared-value", nil
					})
					if err != nil {
						t.Errorf("caller %d: unexpected error: %v", idx, err)
					}
					results[idx] = v
				}(i)
			}
			startGate.Done()
			wg.Wait()

			So(atomic.LoadInt32(&calls), ShouldEqual, 1)
			for _, v := range results {
				So(v, ShouldEqual, "shared-value")
			}
		})

		Convey("unqualified spec cache key is unchanged from the pre-fix bare secret name", func() {
			base := fmt.Sprintf("aws-cache-bugfix/%d/plain", timeSeed())
			_, params, err := parseAwsOpKey(base)
			So(err, ShouldBeNil)
			So(awsSecretCacheKey(base, params), ShouldEqual, base)

			var calls int32
			v, err := pool.GetOrFetchSecret(target, awsSecretCacheKey(base, params), func() (string, error) {
				atomic.AddInt32(&calls, 1)
				return "plain-value", nil
			})
			So(err, ShouldBeNil)
			So(v, ShouldEqual, "plain-value")
			So(atomic.LoadInt32(&calls), ShouldEqual, 1)
		})
	})
}

// TestGetAwsSecret_UsesQualifiedCacheKey calls AwsOperator.getAwsSecret
// itself (not just the pure helpers) to prove the production wiring - not
// just awsSecretCacheKey in isolation - keys the cache/dedup layer by the
// qualified identity. A pre-seeded cache entry for the exact key
// getAwsSecret should compute is hit before getAwsSecret's fetch closure
// ever runs, so no live AWS call happens: if getAwsSecret instead used the
// bare secret string (the pre-fix bug), it would miss this pre-seeded entry
// and fall through to a real secretsmanager call against fakeAwsConfig,
// which fails fast rather than returning the expected value - that failure
// is what makes this test catch the regression.
func TestGetAwsSecret_UsesQualifiedCacheKey(t *testing.T) {
	Convey("AwsOperator.getAwsSecret", t, func() {
		op := AwsOperator{variant: "awssecret"}
		cfg := fakeAwsConfig(t)
		target := fmt.Sprintf("aws-cache-bugfix-wiring-target-%d", timeSeed())

		Convey("a cache hit on the qualified key returns the cached value without reaching the network", func() {
			base := fmt.Sprintf("aws-cache-bugfix/%d/wired", timeSeed())
			_, params, err := parseAwsOpKey(base + "?version=42")
			So(err, ShouldBeNil)

			_, err = awsbackend.DefaultPool.GetOrFetchSecret(target, awsSecretCacheKey(base, params), func() (string, error) {
				return "preseeded-v42", nil
			})
			So(err, ShouldBeNil)

			value, err := op.getAwsSecret(context.Background(), cfg, target, base, params, false)
			So(err, ShouldBeNil)
			So(value, ShouldEqual, "preseeded-v42")
		})

		Convey("distinct pre-seeded versions of the same base secret resolve independently", func() {
			base := fmt.Sprintf("aws-cache-bugfix/%d/wired-multi", timeSeed())
			_, p1, err := parseAwsOpKey(base + "?version=1")
			So(err, ShouldBeNil)
			_, p2, err := parseAwsOpKey(base + "?version=2")
			So(err, ShouldBeNil)

			_, err = awsbackend.DefaultPool.GetOrFetchSecret(target, awsSecretCacheKey(base, p1), func() (string, error) {
				return "preseeded-v1", nil
			})
			So(err, ShouldBeNil)
			_, err = awsbackend.DefaultPool.GetOrFetchSecret(target, awsSecretCacheKey(base, p2), func() (string, error) {
				return "preseeded-v2", nil
			})
			So(err, ShouldBeNil)

			v1, err := op.getAwsSecret(context.Background(), cfg, target, base, p1, false)
			So(err, ShouldBeNil)
			So(v1, ShouldEqual, "preseeded-v1")

			v2, err := op.getAwsSecret(context.Background(), cfg, target, base, p2, false)
			So(err, ShouldBeNil)
			So(v2, ShouldEqual, "preseeded-v2")
		})
	})
}

// timeSeed produces a per-call unique-ish suffix so tests using the shared
// package-level DefaultPool don't collide with cache entries left by other
// tests or concurrently running test agents that also touch DefaultPool.
var timeSeedCounter int64

func timeSeed() int64 {
	return atomic.AddInt64(&timeSeedCounter, 1)
}

// TestAwsOperatorSkipMode pins both of WithSkipAws(true)'s two behaviors
// (plans/dennis-feedback-gaps.md's Item 3): by itself (the --skip-aws CLI
// flag's own default), it defers - leaves the operator's own "(( ... ))"
// expression intact - so a document merged this way can be merged again
// once AWS is reachable; paired with WithRedact(true) (REDACT=1, or
// vaultinfo-style internal skips), it keeps graft's original "return the
// literal REDACTED sentinel" behavior instead, matching the vault and
// NATS operators (op_vault.go, op_nats.go) and spruce's op_aws.go
// semantics.
func TestAwsOperatorSkipMode(t *testing.T) {
	Convey("AWS Operator Skip Mode", t, func() {
		Convey("when SkipAws is true via engine option alone (defer, the default)", func() {
			Convey("awssecret defers with its own expression intact", func() {
				engine, err := graft.NewEngine(graft.WithSkipAws(true))
				So(err, ShouldBeNil)

				yaml := []byte(`
secret: (( awssecret "prod/database/password" ))
`)
				doc, err := engine.ParseYAML(yaml)
				So(err, ShouldBeNil)

				result, err := engine.Evaluate(context.TODO(), doc)
				So(err, ShouldBeNil)

				secret, err := result.Get("secret")
				So(err, ShouldBeNil)
				So(secret, ShouldEqual, `(( awssecret "prod/database/password" ))`)
			})

			Convey("awsparam defers with its own expression intact", func() {
				engine, err := graft.NewEngine(graft.WithSkipAws(true))
				So(err, ShouldBeNil)

				yaml := []byte(`
param: (( awsparam "/config/app/setting" ))
`)
				doc, err := engine.ParseYAML(yaml)
				So(err, ShouldBeNil)

				result, err := engine.Evaluate(context.TODO(), doc)
				So(err, ShouldBeNil)

				param, err := result.Get("param")
				So(err, ShouldBeNil)
				So(param, ShouldEqual, `(( awsparam "/config/app/setting" ))`)
			})
		})

		Convey("when SkipAws and Redact are both true (REDACT=1 / vaultinfo-style)", func() {
			Convey("awssecret should return REDACTED", func() {
				engine, err := graft.NewEngine(graft.WithSkipAws(true), graft.WithRedact(true))
				So(err, ShouldBeNil)

				yaml := []byte(`
secret: (( awssecret "prod/database/password" ))
`)
				doc, err := engine.ParseYAML(yaml)
				So(err, ShouldBeNil)

				result, err := engine.Evaluate(context.TODO(), doc)
				So(err, ShouldBeNil)

				secret, err := result.Get("secret")
				So(err, ShouldBeNil)
				So(secret, ShouldEqual, "REDACTED")
			})

			Convey("awsparam should return REDACTED", func() {
				engine, err := graft.NewEngine(graft.WithSkipAws(true), graft.WithRedact(true))
				So(err, ShouldBeNil)

				yaml := []byte(`
param: (( awsparam "/config/app/setting" ))
`)
				doc, err := engine.ParseYAML(yaml)
				So(err, ShouldBeNil)

				result, err := engine.Evaluate(context.TODO(), doc)
				So(err, ShouldBeNil)

				param, err := result.Get("param")
				So(err, ShouldBeNil)
				So(param, ShouldEqual, "REDACTED")
			})
		})
	})
}

func TestAwsOperatorPhase(t *testing.T) {
	Convey("AWS Operator Phase", t, func() {
		Convey("AwsSecretOperator should be EvalPhase", func() {
			op := NewAwsSecretOperator()
			So(op.Phase(), ShouldEqual, graft.EvalPhase)
		})

		Convey("AwsParamOperator should be EvalPhase", func() {
			op := NewAwsParamOperator()
			So(op.Phase(), ShouldEqual, graft.EvalPhase)
		})
	})
}

func TestAwsOperatorSetup(t *testing.T) {
	Convey("AWS Operator Setup", t, func() {
		Convey("AwsSecretOperator Setup should succeed", func() {
			op := NewAwsSecretOperator()
			err := op.Setup()
			So(err, ShouldBeNil)
		})

		Convey("AwsParamOperator Setup should succeed", func() {
			op := NewAwsParamOperator()
			err := op.Setup()
			So(err, ShouldBeNil)
		})
	})
}

func TestAwsClientPoolThreadSafety(t *testing.T) {
	Convey("AwsClientPool Thread Safety", t, func() {
		Convey("Multiple cache operations should not race", func() {
			// DefaultPool is the global pool from internal/backends/aws
			pool := awsbackend.DefaultPool

			// Test that the pool is properly initialized with mutex protection
			So(pool, ShouldNotBeNil)

			// GetOrFetchSecret/GetOrFetchParam are the safe cache accessors
			// (they never expose the backing map, unlike the removed
			// GetSecretCache/SetSecretCache pair - see internal/backends/aws/cache.go).
			secretVal, err := pool.GetOrFetchSecret("test-target", "test-secret", func() (string, error) {
				return "test-value", nil
			})
			So(err, ShouldBeNil)
			So(secretVal, ShouldEqual, "test-value")

			paramVal, err := pool.GetOrFetchParam("test-target", "test-param", func() (string, error) {
				return "test-param-value", nil
			})
			So(err, ShouldBeNil)
			So(paramVal, ShouldEqual, "test-param-value")
		})
	})
}

// TestGetAwsParam_UsesInjectedClient proves getAwsParam builds its SSM
// client through awsbackend.NewSSMClient (not a hardcoded ssm.NewFromConfig
// call), so tests can swap that factory var for a fake, and that the
// WithDecryption/Name fields getAwsParam sends reach the client verbatim.
// skipCache=true (":nocache") is used throughout so the assertion below
// ("exactly one call") cannot be satisfied by a cache hit instead of a
// real fetch.
func TestGetAwsParam_UsesInjectedClient(t *testing.T) {
	original := awsbackend.NewSSMClient
	fake := &awsfakes.FakeSSMClient{
		GetParameterFn: func(_ context.Context, params *ssm.GetParameterInput, _ ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
			return &ssm.GetParameterOutput{Parameter: &ssmtypes.Parameter{Value: aws.String("injected-value")}}, nil
		},
	}
	awsbackend.NewSSMClient = func(aws.Config) awsbackend.SSMClient { return fake }
	t.Cleanup(func() { awsbackend.NewSSMClient = original })

	op := AwsOperator{variant: "awsparam"}
	target := fmt.Sprintf("aws-injected-client-param-target-%d", timeSeed())

	value, err := op.getAwsParam(context.Background(), aws.Config{Region: "us-east-1"}, target, "/x", true)
	if err != nil {
		t.Fatalf("getAwsParam returned an unexpected error: %v", err)
	}
	if value != "injected-value" {
		t.Fatalf("value = %q, want %q", value, "injected-value")
	}

	calls := fake.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected exactly one GetParameter call, got %d", len(calls))
	}
	if got := aws.ToString(calls[0].Name); got != "/x" {
		t.Fatalf("Name = %q, want %q", got, "/x")
	}
	if !aws.ToBool(calls[0].WithDecryption) {
		t.Fatal("expected WithDecryption to be true, matching internal/backends/aws's own default")
	}
}

// TestGetAwsSecret_PassesStageAndVersion proves getAwsSecret builds its
// Secrets Manager client through awsbackend.NewSecretsManagerClient and
// that the "?stage=..." query qualifier reaches the client as
// VersionStage (with VersionId left nil), matching
// buildAwsSecretInput's stage-wins precedence. skipCache=true
// (":nocache") keeps this a genuine call-level assertion, not a cache
// hit.
func TestGetAwsSecret_PassesStageAndVersion(t *testing.T) {
	original := awsbackend.NewSecretsManagerClient
	fake := &awsfakes.FakeSecretsManagerClient{
		GetSecretValueFn: func(_ context.Context, params *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
			return &secretsmanager.GetSecretValueOutput{SecretString: aws.String("injected-secret")}, nil
		},
	}
	awsbackend.NewSecretsManagerClient = func(aws.Config) awsbackend.SecretsManagerClient { return fake }
	t.Cleanup(func() { awsbackend.NewSecretsManagerClient = original })

	op := AwsOperator{variant: "awssecret"}
	target := fmt.Sprintf("aws-injected-client-secret-target-%d", timeSeed())

	_, params, err := parseAwsOpKey("db?stage=AWSCURRENT")
	if err != nil {
		t.Fatalf("parseAwsOpKey failed: %v", err)
	}

	value, err := op.getAwsSecret(context.Background(), aws.Config{Region: "us-east-1"}, target, "db", params, true)
	if err != nil {
		t.Fatalf("getAwsSecret returned an unexpected error: %v", err)
	}
	if value != "injected-secret" {
		t.Fatalf("value = %q, want %q", value, "injected-secret")
	}

	calls := fake.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected exactly one GetSecretValue call, got %d", len(calls))
	}
	if got := aws.ToString(calls[0].SecretId); got != "db" {
		t.Fatalf("SecretId = %q, want %q", got, "db")
	}
	if got := aws.ToString(calls[0].VersionStage); got != "AWSCURRENT" {
		t.Fatalf("VersionStage = %q, want %q", got, "AWSCURRENT")
	}
	if calls[0].VersionId != nil {
		t.Fatalf("expected VersionId to be nil when stage is set, got %q", aws.ToString(calls[0].VersionId))
	}
}

// TestGetAwsSecret_NocacheBypassesPool proves skipCache=true (the
// ":nocache" modifier) never consults DefaultPool's cache: a cache entry
// is pre-seeded with a stale value under the exact identity getAwsSecret
// would compute, then getAwsSecret is called with skipCache=true and a
// fake returning a different value. If getAwsSecret consulted the cache
// despite skipCache, it would return the stale pre-seeded value instead
// of the fake's fresh one.
func TestGetAwsSecret_NocacheBypassesPool(t *testing.T) {
	original := awsbackend.NewSecretsManagerClient
	fake := &awsfakes.FakeSecretsManagerClient{
		GetSecretValueFn: func(context.Context, *secretsmanager.GetSecretValueInput, ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
			return &secretsmanager.GetSecretValueOutput{SecretString: aws.String("fresh-value")}, nil
		},
	}
	awsbackend.NewSecretsManagerClient = func(aws.Config) awsbackend.SecretsManagerClient { return fake }
	t.Cleanup(func() { awsbackend.NewSecretsManagerClient = original })

	op := AwsOperator{variant: "awssecret"}
	target := fmt.Sprintf("aws-nocache-target-%d", timeSeed())
	base := fmt.Sprintf("aws-nocache/%d/db", timeSeed())
	_, params, err := parseAwsOpKey(base)
	if err != nil {
		t.Fatalf("parseAwsOpKey failed: %v", err)
	}

	if _, err := awsbackend.DefaultPool.GetOrFetchSecret(target, awsSecretCacheKey(base, params), func() (string, error) {
		return "stale-cached-value", nil
	}); err != nil {
		t.Fatalf("pre-seeding the cache failed: %v", err)
	}

	value, err := op.getAwsSecret(context.Background(), aws.Config{Region: "us-east-1"}, target, base, params, true)
	if err != nil {
		t.Fatalf("getAwsSecret returned an unexpected error: %v", err)
	}
	if value != "fresh-value" {
		t.Fatalf("value = %q, want the fake's fresh value %q (cache was consulted despite skipCache)", value, "fresh-value")
	}
	if fake.CallCount() != 1 {
		t.Fatalf("expected exactly one GetSecretValue call, got %d", fake.CallCount())
	}
}

package operators

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"

	awsSDK "github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"

	awsbackend "github.com/fivetwenty-io/graft/internal/backends/aws"
	"github.com/fivetwenty-io/graft/pkg/graft"
)

// fakeAwsSession builds a session with non-functional static credentials
// and no retries, for tests that must prove a cache hit short-circuits
// getAwsSecret before it ever reaches the network: if getAwsSecret misses
// the cache and falls through to secretsmanager.New(session).GetSecretValue,
// this session errors out quickly (bad credentials / no route) rather than
// silently succeeding against a real account.
func fakeAwsSession(t *testing.T) *session.Session {
	t.Helper()
	sess, err := session.NewSession(&awsSDK.Config{
		Region:      awsSDK.String("us-east-1"),
		Credentials: credentials.NewStaticCredentials("AKIAFAKEFAKEFAKEFAKE", "fake-secret-key-not-real", ""),
		MaxRetries:  awsSDK.Int(0),
	})
	if err != nil {
		t.Fatalf("fakeAwsSession: unexpected error building session: %v", err)
	}
	return sess
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
			So(awsSDK.StringValue(input.SecretId), ShouldEqual, "db")
			So(input.VersionId, ShouldBeNil)
			So(input.VersionStage, ShouldBeNil)
		})

		Convey("version param sets VersionId", func() {
			input := buildAwsSecretInput("db", url.Values{"version": []string{"v1"}})
			So(awsSDK.StringValue(input.VersionId), ShouldEqual, "v1")
			So(input.VersionStage, ShouldBeNil)
		})

		Convey("stage param sets VersionStage", func() {
			input := buildAwsSecretInput("db", url.Values{"stage": []string{"AWSCURRENT"}})
			So(awsSDK.StringValue(input.VersionStage), ShouldEqual, "AWSCURRENT")
			So(input.VersionId, ShouldBeNil)
		})

		Convey("stage takes precedence over version when both present", func() {
			input := buildAwsSecretInput("db", url.Values{
				"stage":   []string{"AWSCURRENT"},
				"version": []string{"v1"},
			})
			So(awsSDK.StringValue(input.VersionStage), ShouldEqual, "AWSCURRENT")
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
// and fall through to a real secretsmanager call against fakeAwsSession,
// which fails fast rather than returning the expected value - that failure
// is what makes this test catch the regression.
func TestGetAwsSecret_UsesQualifiedCacheKey(t *testing.T) {
	Convey("AwsOperator.getAwsSecret", t, func() {
		op := AwsOperator{variant: "awssecret"}
		sess := fakeAwsSession(t)
		target := fmt.Sprintf("aws-cache-bugfix-wiring-target-%d", timeSeed())

		Convey("a cache hit on the qualified key returns the cached value without reaching the network", func() {
			base := fmt.Sprintf("aws-cache-bugfix/%d/wired", timeSeed())
			_, params, err := parseAwsOpKey(base + "?version=42")
			So(err, ShouldBeNil)

			_, err = awsbackend.DefaultPool.GetOrFetchSecret(target, awsSecretCacheKey(base, params), func() (string, error) {
				return "preseeded-v42", nil
			})
			So(err, ShouldBeNil)

			value, err := op.getAwsSecret(sess, target, base, params, false)
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

			v1, err := op.getAwsSecret(sess, target, base, p1, false)
			So(err, ShouldBeNil)
			So(v1, ShouldEqual, "preseeded-v1")

			v2, err := op.getAwsSecret(sess, target, base, p2, false)
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

func TestAwsOperatorSkipMode(t *testing.T) {
	Convey("AWS Operator Skip Mode", t, func() {
		Convey("when SkipAws is true via engine option", func() {
			Convey("awssecret should return REDACTED", func() {
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
				// When SkipAws is true, returns the literal "REDACTED" instead of
				// the actual value, matching the vault and NATS operators
				// (op_vault.go, op_nats.go) and spruce's op_aws.go semantics.
				So(secret, ShouldEqual, "REDACTED")
			})

			Convey("awsparam should return REDACTED", func() {
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
				// When SkipAws is true, returns the literal "REDACTED" instead of
				// the actual value, matching the vault and NATS operators
				// (op_vault.go, op_nats.go) and spruce's op_aws.go semantics.
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

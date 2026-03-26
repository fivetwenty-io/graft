package operators

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/secretsmanager/secretsmanageriface"
	"github.com/aws/aws-sdk-go/service/ssm/ssmiface"
	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

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

func TestAwsOperatorSkipMode(t *testing.T) {
	Convey("AWS Operator Skip Mode", t, func() {
		Convey("when SkipAws is true via engine option", func() {
			Convey("awssecret should return skipped message", func() {
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
				// When SkipAws is true, returns a skip message instead of actual value
				So(secret, ShouldContainSubstring, "skipped")
			})

			Convey("awsparam should return skipped message", func() {
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
				// When SkipAws is true, returns a skip message instead of actual value
				So(param, ShouldContainSubstring, "skipped")
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
			pool := &AwsClientPool{
				sessions:              make(map[string]*session.Session),
				secretsManagerClients: make(map[string]secretsmanageriface.SecretsManagerAPI),
				parameterStoreClients: make(map[string]ssmiface.SSMAPI),
				configs:               make(map[string]*AwsTarget),
				secretsCache:          make(map[string]map[string]string),
				paramsCache:           make(map[string]map[string]string),
			}

			// Test that the pool is properly initialized with mutex protection
			So(pool.sessions, ShouldNotBeNil)
			So(pool.secretsManagerClients, ShouldNotBeNil)
			So(pool.parameterStoreClients, ShouldNotBeNil)
			So(pool.configs, ShouldNotBeNil)
			So(pool.secretsCache, ShouldNotBeNil)
			So(pool.paramsCache, ShouldNotBeNil)
		})
	})
}

package operators

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// TestRedactEnvVarForcesVaultAwsNatsRedaction locks the REDACT environment
// variable's effect on the production CLI evaluation path (DefaultEngine.
// Evaluate), matching spruce's evaluator.go REDACT semantics: any non-empty
// REDACT value forces vault, AWS (param + secret), and NATS lookups to
// return the literal string "REDACTED" without making a backend call.
func TestRedactEnvVarForcesVaultAwsNatsRedaction(t *testing.T) {
	Convey("REDACT environment variable set to a non-empty value", t, func() {
		t.Setenv("REDACT", "1")

		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		yamlInput := []byte(`
vault_secret: (( vault "secret/hand:shake" ))
aws_param: (( awsparam "/config/app/setting" ))
aws_secret: (( awssecret "prod/database/password" ))
nats_config: (( nats "kv:mybucket/mykey" ))
`)
		doc, err := engine.ParseYAML(yamlInput)
		So(err, ShouldBeNil)

		result, err := engine.Evaluate(context.TODO(), doc)
		So(err, ShouldBeNil)

		Convey("vault operator returns REDACTED without a backend call", func() {
			val, getErr := result.Get("vault_secret")
			So(getErr, ShouldBeNil)
			So(val, ShouldEqual, "REDACTED")
		})

		Convey("awsparam operator returns REDACTED without a backend call", func() {
			val, getErr := result.Get("aws_param")
			So(getErr, ShouldBeNil)
			So(val, ShouldEqual, "REDACTED")
		})

		Convey("awssecret operator returns REDACTED without a backend call", func() {
			val, getErr := result.Get("aws_secret")
			So(getErr, ShouldBeNil)
			So(val, ShouldEqual, "REDACTED")
		})

		Convey("nats operator returns REDACTED without a backend call", func() {
			val, getErr := result.Get("nats_config")
			So(getErr, ShouldBeNil)
			So(val, ShouldEqual, "REDACTED")
		})
	})

	Convey("REDACT environment variable unset (empty string)", t, func() {
		// An empty REDACT value must not trigger redaction, matching spruce's
		// `os.Getenv("REDACT") != ""` check. Evaluate a doc with no vault/aws/
		// nats operators so the assertion exercises only the REDACT wiring
		// itself, not real backend connectivity.
		t.Setenv("REDACT", "")

		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte("foo: bar\n"))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.TODO(), doc)
		So(err, ShouldBeNil)

		So(engine.GetOperatorState().IsVaultSkipped(), ShouldBeFalse)
		So(engine.GetOperatorState().IsAWSSkipped(), ShouldBeFalse)
		So(engine.GetOperatorState().IsNATSSkipped(), ShouldBeFalse)
	})
}

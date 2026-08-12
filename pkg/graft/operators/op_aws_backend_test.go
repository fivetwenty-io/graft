package operators

import (
	"context"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// awsStubBackend is a minimal graft.Backend registered under name (either
// "awsparam" or "awssecret", matching AwsOperator.variant).
type awsStubBackend struct {
	name string
	data map[string]string
}

func (b *awsStubBackend) Name() string { return b.name }

func (b *awsStubBackend) Get(_ context.Context, path string) (interface{}, error) {
	v, ok := b.data[path]
	if !ok {
		return nil, graft.ErrBackendNotFound
	}
	return v, nil
}

func (b *awsStubBackend) GetBatch(ctx context.Context, paths []string) (map[string]interface{}, error) {
	return graft.SequentialGetBatch(ctx, paths, b.Get)
}
func (b *awsStubBackend) Health(context.Context) error { return nil }
func (b *awsStubBackend) Close() error                 { return nil }

// hermeticizeAWSEnv neutralizes every AWS credential/config source
// internal/backends/aws's session-building path consults, and disables
// EC2 IMDS probing, so a test exercising the built-in (unconfigured) AWS
// path fails fast and deterministically regardless of the machine it runs
// on (see phase3-review.md M9 - this used to take 5-6s per test on a
// machine with no AWS credentials, dominated by IMDS timeout probing, and
// would behave differently entirely on a machine that does have usable
// AWS credentials or an EC2 IMDS endpoint reachable).
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

func TestAwsParamOperatorFlagOffIgnoresCustomBackend(t *testing.T) {
	hermeticizeAWSEnv(t)

	backend := &awsStubBackend{name: "awsparam", data: map[string]string{"/config/app/setting": "custom-value"}}

	withCustom, err := graft.NewEngine(graft.WithBackend(backend))
	if err != nil {
		t.Fatalf("NewEngine (with custom backend): %v", err)
	}
	withoutCustom, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("NewEngine (no custom backend): %v", err)
	}

	yamlDoc := "value: (( awsparam \"/config/app/setting\" ))\n"

	_, errWith := evaluateYAML(t, withCustom, yamlDoc)
	_, errWithout := evaluateYAML(t, withoutCustom, yamlDoc)

	if errWith == nil {
		t.Fatalf("expected an error (no real AWS credentials in this test environment); the custom backend was consulted despite the flag being off")
	}
	if strings.Contains(errWith.Error(), "custom-value") {
		t.Fatalf("error unexpectedly references the custom backend's value: %v", errWith)
	}
	if errWithout == nil {
		t.Fatalf("expected the no-custom-backend baseline to also error")
	}
}

func TestAwsParamOperatorFlagOnNoCustomBackendFallsBack(t *testing.T) {
	hermeticizeAWSEnv(t)

	flagOn, err := graft.NewEngine(graft.WithFeatureFlags(backendRegistryFlagsEnabled()))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	_, err = evaluateYAML(t, flagOn, "value: (( awsparam \"/config/app/setting\" ))\n")
	if err == nil {
		t.Fatalf("expected an error (no real AWS credentials, and no custom backend registered)")
	}
}

func TestAwsParamOperatorFlagOnCustomBackendConsulted(t *testing.T) {
	backend := &awsStubBackend{name: "awsparam", data: map[string]string{"/config/app/setting": "custom-value"}}
	engine, err := graft.NewEngine(
		graft.WithFeatureFlags(backendRegistryFlagsEnabled()),
		graft.WithBackend(backend),
	)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	doc, err := evaluateYAML(t, engine, "value: (( awsparam \"/config/app/setting\" ))\n")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	got, err := doc.GetString("value")
	if err != nil {
		t.Fatalf("GetString(\"value\"): %v", err)
	}
	if got != "custom-value" {
		t.Fatalf("value = %q, want %q", got, "custom-value")
	}
}

func TestAwsSecretOperatorFlagOnCustomBackendConsulted(t *testing.T) {
	backend := &awsStubBackend{name: "awssecret", data: map[string]string{"prod/db/password": "s3cr3t"}}
	engine, err := graft.NewEngine(
		graft.WithFeatureFlags(backendRegistryFlagsEnabled()),
		graft.WithBackend(backend),
	)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	doc, err := evaluateYAML(t, engine, "value: (( awssecret \"prod/db/password\" ))\n")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	got, err := doc.GetString("value")
	if err != nil {
		t.Fatalf("GetString(\"value\"): %v", err)
	}
	if got != "s3cr3t" {
		t.Fatalf("value = %q, want %q", got, "s3cr3t")
	}
}

// TestAwsParamOperatorVariantIsolation proves a backend registered under
// "awssecret" is not consulted by the awsparam operator (and vice versa
// implicitly, since each operator only ever looks up its own o.variant
// name).
func TestAwsParamOperatorVariantIsolation(t *testing.T) {
	hermeticizeAWSEnv(t)

	backend := &awsStubBackend{name: "awssecret", data: map[string]string{"/config/app/setting": "wrong-variant-value"}}
	engine, err := graft.NewEngine(
		graft.WithFeatureFlags(backendRegistryFlagsEnabled()),
		graft.WithBackend(backend),
	)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	_, err = evaluateYAML(t, engine, "value: (( awsparam \"/config/app/setting\" ))\n")
	if err == nil {
		t.Fatalf("expected awsparam to miss an \"awssecret\"-registered backend and fall through to the (failing, uncredentialed) built-in path")
	}
	if strings.Contains(err.Error(), "wrong-variant-value") {
		t.Fatalf("awsparam unexpectedly consulted a backend registered under \"awssecret\": %v", err)
	}
}

func TestAwsParamOperatorTargetUnsupportedByCustomBackend(t *testing.T) {
	backend := &awsStubBackend{name: "awsparam", data: map[string]string{"/config/app/setting": "custom-value"}}
	engine, err := graft.NewEngine(
		graft.WithFeatureFlags(backendRegistryFlagsEnabled()),
		graft.WithBackend(backend),
	)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	_, evalErr := evaluateYAML(t, engine, "value: (( awsparam@prod \"/config/app/setting\" ))\n")
	if evalErr == nil {
		t.Fatalf("expected an error for @target against a non-TargetedBackend")
	}
	if !strings.Contains(evalErr.Error(), "does not support @target selection") {
		t.Fatalf("error = %q, want it to mention unsupported @target selection", evalErr.Error())
	}
}

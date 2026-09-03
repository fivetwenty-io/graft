package aws_test

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/ssm"

	awsbackend "github.com/fivetwenty-io/graft/internal/backends/aws"
	"github.com/fivetwenty-io/graft/internal/backends/aws/awsfakes"
)

// TestNewSecretsManagerClient_DefaultsToRealSDKClient proves the
// production factory var builds a real *secretsmanager.Client (not a nil
// interface, not a fake) from an aws.Config, which is what every
// production call site relies on when it has not been swapped by a test.
func TestNewSecretsManagerClient_DefaultsToRealSDKClient(t *testing.T) {
	client := awsbackend.NewSecretsManagerClient(aws.Config{Region: "us-east-1"})
	if client == nil {
		t.Fatal("expected a non-nil SecretsManagerClient")
	}
	if _, ok := client.(*secretsmanager.Client); !ok {
		t.Fatalf("expected the default factory to build *secretsmanager.Client, got %T", client)
	}
}

// TestNewSSMClient_DefaultsToRealSDKClient mirrors
// TestNewSecretsManagerClient_DefaultsToRealSDKClient for SSM.
func TestNewSSMClient_DefaultsToRealSDKClient(t *testing.T) {
	client := awsbackend.NewSSMClient(aws.Config{Region: "us-east-1"})
	if client == nil {
		t.Fatal("expected a non-nil SSMClient")
	}
	if _, ok := client.(*ssm.Client); !ok {
		t.Fatalf("expected the default factory to build *ssm.Client, got %T", client)
	}
}

// TestNewSecretsManagerClient_IsSwappableForTests proves the factory var
// is a genuine test seam: swapping it (and restoring via t.Cleanup, the
// pattern every consuming test must follow) makes the swapped-in fake
// reachable through the exact same call production code uses.
func TestNewSecretsManagerClient_IsSwappableForTests(t *testing.T) {
	original := awsbackend.NewSecretsManagerClient
	fake := &awsfakes.FakeSecretsManagerClient{}
	awsbackend.NewSecretsManagerClient = func(aws.Config) awsbackend.SecretsManagerClient { return fake }
	t.Cleanup(func() { awsbackend.NewSecretsManagerClient = original })

	got := awsbackend.NewSecretsManagerClient(aws.Config{})
	if got != awsbackend.SecretsManagerClient(fake) {
		t.Fatalf("expected the swapped-in fake to be returned, got %#v", got)
	}
}

// TestNewSSMClient_IsSwappableForTests mirrors
// TestNewSecretsManagerClient_IsSwappableForTests for SSM.
func TestNewSSMClient_IsSwappableForTests(t *testing.T) {
	original := awsbackend.NewSSMClient
	fake := &awsfakes.FakeSSMClient{}
	awsbackend.NewSSMClient = func(aws.Config) awsbackend.SSMClient { return fake }
	t.Cleanup(func() { awsbackend.NewSSMClient = original })

	got := awsbackend.NewSSMClient(aws.Config{})
	if got != awsbackend.SSMClient(fake) {
		t.Fatalf("expected the swapped-in fake to be returned, got %#v", got)
	}
}

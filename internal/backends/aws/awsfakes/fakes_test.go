package awsfakes_test

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	awsbackend "github.com/fivetwenty-io/graft/internal/backends/aws"
	"github.com/fivetwenty-io/graft/internal/backends/aws/awsfakes"
)

// Compile-time proof the fakes satisfy the real client interfaces they
// stand in for. This lives in package awsfakes_test (an external test
// package importing both awsbackend and awsfakes), which is cycle-free:
// awsfakes itself never imports awsbackend.
var (
	_ awsbackend.SecretsManagerClient = (*awsfakes.FakeSecretsManagerClient)(nil)
	_ awsbackend.SSMClient            = (*awsfakes.FakeSSMClient)(nil)
)

func TestFakeSecretsManagerClient_RecordsInputAndReturnsStub(t *testing.T) {
	fake := &awsfakes.FakeSecretsManagerClient{
		GetSecretValueFn: func(_ context.Context, params *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
			return &secretsmanager.GetSecretValueOutput{SecretString: aws.String("stubbed-" + aws.ToString(params.SecretId))}, nil
		},
	}

	out, err := fake.GetSecretValue(context.Background(), &secretsmanager.GetSecretValueInput{SecretId: aws.String("db")})
	if err != nil {
		t.Fatalf("GetSecretValue returned an unexpected error: %v", err)
	}
	if got := aws.ToString(out.SecretString); got != "stubbed-db" {
		t.Fatalf("SecretString = %q, want %q", got, "stubbed-db")
	}

	if fake.CallCount() != 1 {
		t.Fatalf("CallCount() = %d, want 1", fake.CallCount())
	}
	calls := fake.Calls()
	if len(calls) != 1 || aws.ToString(calls[0].SecretId) != "db" {
		t.Fatalf("Calls() = %#v, want a single call for SecretId \"db\"", calls)
	}
}

func TestFakeSecretsManagerClient_NoFnReturnsEmptyOutputNoError(t *testing.T) {
	fake := &awsfakes.FakeSecretsManagerClient{}
	out, err := fake.GetSecretValue(context.Background(), &secretsmanager.GetSecretValueInput{SecretId: aws.String("db")})
	if err != nil {
		t.Fatalf("expected nil error with no GetSecretValueFn set, got: %v", err)
	}
	if out == nil {
		t.Fatal("expected a non-nil output with no GetSecretValueFn set")
	}
}

func TestFakeSecretsManagerClient_ReturnsConfiguredError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := &awsfakes.FakeSecretsManagerClient{
		GetSecretValueFn: func(context.Context, *secretsmanager.GetSecretValueInput, ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
			return nil, wantErr
		},
	}
	_, err := fake.GetSecretValue(context.Background(), &secretsmanager.GetSecretValueInput{SecretId: aws.String("db")})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected the configured error, got: %v", err)
	}
}

func TestFakeSSMClient_RecordsInputAndReturnsStub(t *testing.T) {
	fake := &awsfakes.FakeSSMClient{
		GetParameterFn: func(_ context.Context, params *ssm.GetParameterInput, _ ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
			return &ssm.GetParameterOutput{Parameter: &ssmtypes.Parameter{Value: aws.String("stubbed-" + aws.ToString(params.Name))}}, nil
		},
	}

	out, err := fake.GetParameter(context.Background(), &ssm.GetParameterInput{Name: aws.String("/x"), WithDecryption: aws.Bool(true)})
	if err != nil {
		t.Fatalf("GetParameter returned an unexpected error: %v", err)
	}
	if got := aws.ToString(out.Parameter.Value); got != "stubbed-/x" {
		t.Fatalf("Parameter.Value = %q, want %q", got, "stubbed-/x")
	}

	if fake.CallCount() != 1 {
		t.Fatalf("CallCount() = %d, want 1", fake.CallCount())
	}
	calls := fake.Calls()
	if len(calls) != 1 || aws.ToString(calls[0].Name) != "/x" || !aws.ToBool(calls[0].WithDecryption) {
		t.Fatalf("Calls() = %#v, want a single call for Name \"/x\" with WithDecryption=true", calls)
	}
}

func TestFakeSSMClient_NoFnReturnsEmptyOutputNoError(t *testing.T) {
	fake := &awsfakes.FakeSSMClient{}
	out, err := fake.GetParameter(context.Background(), &ssm.GetParameterInput{Name: aws.String("/x")})
	if err != nil {
		t.Fatalf("expected nil error with no GetParameterFn set, got: %v", err)
	}
	if out == nil {
		t.Fatal("expected a non-nil output with no GetParameterFn set")
	}
}

func TestFakeSSMClient_ReturnsConfiguredError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := &awsfakes.FakeSSMClient{
		GetParameterFn: func(context.Context, *ssm.GetParameterInput, ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
			return nil, wantErr
		},
	}
	_, err := fake.GetParameter(context.Background(), &ssm.GetParameterInput{Name: aws.String("/x")})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected the configured error, got: %v", err)
	}
}

func TestFakeSTSClient_RecordsCallCountAndReturnsStub(t *testing.T) {
	fake := &awsfakes.FakeSTSClient{
		GetCallerIdentityFn: func(context.Context, *sts.GetCallerIdentityInput, ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
			return &sts.GetCallerIdentityOutput{Account: aws.String("123456789012")}, nil
		},
	}

	out, err := fake.GetCallerIdentity(context.Background(), &sts.GetCallerIdentityInput{})
	if err != nil {
		t.Fatalf("GetCallerIdentity returned an unexpected error: %v", err)
	}
	if got := aws.ToString(out.Account); got != "123456789012" {
		t.Fatalf("Account = %q, want %q", got, "123456789012")
	}

	if _, err := fake.GetCallerIdentity(context.Background(), &sts.GetCallerIdentityInput{}); err != nil {
		t.Fatalf("second GetCallerIdentity returned an unexpected error: %v", err)
	}
	if fake.CallCount() != 2 {
		t.Fatalf("CallCount() = %d, want 2", fake.CallCount())
	}
}

func TestFakeSTSClient_NoFnReturnsEmptyOutputNoError(t *testing.T) {
	fake := &awsfakes.FakeSTSClient{}
	out, err := fake.GetCallerIdentity(context.Background(), &sts.GetCallerIdentityInput{})
	if err != nil {
		t.Fatalf("expected nil error with no GetCallerIdentityFn set, got: %v", err)
	}
	if out == nil {
		t.Fatal("expected a non-nil output with no GetCallerIdentityFn set")
	}
}

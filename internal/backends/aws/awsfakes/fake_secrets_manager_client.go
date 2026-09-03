package awsfakes //nolint:dupl // mirrors fake_ssm_client.go by design - both are small hand-written test doubles for single-method SDK client interfaces that happen to share a shape

import (
	"context"
	"sync"

	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// FakeSecretsManagerClient is a test double for
// internal/backends/aws.SecretsManagerClient. If GetSecretValueFn is set,
// it is invoked for every call and its result returned verbatim; a nil
// GetSecretValueFn makes GetSecretValue return an empty, non-nil output
// and a nil error, which is enough for tests that only care about the
// call being recorded. All access is serialized under mu so the fake is
// safe under `go test -race` when the production code under test issues
// concurrent calls.
type FakeSecretsManagerClient struct {
	GetSecretValueFn func(ctx context.Context, params *secretsmanager.GetSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)

	mu    sync.Mutex
	calls []*secretsmanager.GetSecretValueInput
}

// GetSecretValue implements internal/backends/aws.SecretsManagerClient
// (and pkg/graft's structurally identical unexported mirror interface).
func (f *FakeSecretsManagerClient) GetSecretValue(ctx context.Context, params *secretsmanager.GetSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	f.mu.Lock()
	f.calls = append(f.calls, params)
	f.mu.Unlock()

	if f.GetSecretValueFn != nil {
		return f.GetSecretValueFn(ctx, params, optFns...)
	}
	return &secretsmanager.GetSecretValueOutput{}, nil
}

// Calls returns a copy of every GetSecretValueInput passed to
// GetSecretValue so far, in call order. A copy is returned (rather than
// the backing slice) so a caller ranging over the result cannot race with
// a concurrent GetSecretValue call appending to it.
func (f *FakeSecretsManagerClient) Calls() []*secretsmanager.GetSecretValueInput {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*secretsmanager.GetSecretValueInput, len(f.calls))
	copy(out, f.calls)
	return out
}

// CallCount returns the number of times GetSecretValue has been invoked.
func (f *FakeSecretsManagerClient) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

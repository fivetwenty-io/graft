package awsfakes //nolint:dupl // mirrors fake_secrets_manager_client.go by design - both are small hand-written test doubles for single-method SDK client interfaces that happen to share a shape

import (
	"context"
	"sync"

	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

// FakeSSMClient is a test double for internal/backends/aws.SSMClient. See
// FakeSecretsManagerClient's doc comment for the same GetParameterFn/nil
// behavior and the concurrency guarantee.
type FakeSSMClient struct {
	GetParameterFn func(ctx context.Context, params *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error)

	mu    sync.Mutex
	calls []*ssm.GetParameterInput
}

// GetParameter implements internal/backends/aws.SSMClient (and pkg/graft's
// structurally identical unexported mirror interface).
func (f *FakeSSMClient) GetParameter(ctx context.Context, params *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
	f.mu.Lock()
	f.calls = append(f.calls, params)
	f.mu.Unlock()

	if f.GetParameterFn != nil {
		return f.GetParameterFn(ctx, params, optFns...)
	}
	return &ssm.GetParameterOutput{}, nil
}

// Calls returns a copy of every GetParameterInput passed to GetParameter
// so far, in call order. See FakeSecretsManagerClient.Calls for why a copy
// is returned.
func (f *FakeSSMClient) Calls() []*ssm.GetParameterInput {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*ssm.GetParameterInput, len(f.calls))
	copy(out, f.calls)
	return out
}

// CallCount returns the number of times GetParameter has been invoked.
func (f *FakeSSMClient) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

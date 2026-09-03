package awsfakes

import (
	"context"
	"sync"

	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// FakeSTSClient is a test double for the sts client's GetCallerIdentity
// method, consumed through pkg/graft's unexported awsSTSAPI mirror
// interface (backend_aws.go's Health check). See
// FakeSecretsManagerClient's doc comment for the same *Fn/nil behavior and
// the concurrency guarantee.
type FakeSTSClient struct {
	GetCallerIdentityFn func(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error)

	mu    sync.Mutex
	calls int
}

// GetCallerIdentity implements pkg/graft's unexported awsSTSAPI mirror
// interface.
func (f *FakeSTSClient) GetCallerIdentity(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()

	if f.GetCallerIdentityFn != nil {
		return f.GetCallerIdentityFn(ctx, params, optFns...)
	}
	return &sts.GetCallerIdentityOutput{}, nil
}

// CallCount returns the number of times GetCallerIdentity has been
// invoked.
func (f *FakeSTSClient) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

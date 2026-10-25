package regex

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestContextDeadlineStopsMatching(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := MustCompile(`(a+)+$`).Find(ctx, strings.Repeat("a", 60)+"b", 0)
	wantErrIs(t, err, context.DeadlineExceeded)
}

func TestCancelledContextStopsBeforeMatching(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := MustCompile(`a`).Find(ctx, "a", 0)
	wantErrIs(t, err, context.Canceled)
}

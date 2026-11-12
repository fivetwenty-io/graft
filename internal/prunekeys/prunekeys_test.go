package prunekeys

import (
	"slices"
	"testing"
)

func TestForUsesTheInstalledLookup(t *testing.T) {
	saved := lookup
	t.Cleanup(func() { lookup = saved })

	lookup = nil
	if got := For(1); got != nil {
		t.Fatalf("For with no hook = %v, want nil", got)
	}
	SetLookup(func(ev any) []string {
		if ev == 1 {
			return []string{"m.b"}
		}
		return nil
	})
	if got := For(1); !slices.Equal(got, []string{"m.b"}) {
		t.Fatalf("For(1) = %v, want [m.b]", got)
	}
	if got := For(2); got != nil {
		t.Fatalf("For(2) = %v, want nil", got)
	}
}

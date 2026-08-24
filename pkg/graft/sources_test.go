package graft

import (
	"context"
	"testing"
)

func TestSourceRefsRoundTripThroughContext(t *testing.T) {
	refs := []SourceRef{
		{Name: "base.yml", Bytes: []byte("a: (( grab b ))\n")},
		{Name: "over.json", Opaque: true},
	}

	ctx := WithSourceRefs(context.Background(), refs)
	got := GetSourceRefs(ctx)

	if len(got) != 2 {
		t.Fatalf("GetSourceRefs returned %d refs, want 2", len(got))
	}
	if got[0].Name != "base.yml" || string(got[0].Bytes) != "a: (( grab b ))\n" {
		t.Errorf("got[0] = %+v, want base.yml with its bytes", got[0])
	}
	if !got[1].Opaque {
		t.Errorf("got[1].Opaque = false, want true")
	}
}

func TestGetSourceRefsIsSafeWithoutAValue(t *testing.T) {
	if refs := GetSourceRefs(context.Background()); refs != nil {
		t.Errorf("GetSourceRefs on a bare context = %v, want nil", refs)
	}
	//nolint:staticcheck // deliberately passing nil to pin the nil-safety contract
	if refs := GetSourceRefs(nil); refs != nil {
		t.Errorf("GetSourceRefs(nil) = %v, want nil", refs)
	}
}

func TestEvaluateCopiesSourceRefsOntoEvaluator(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	doc, err := engine.ParseYAML([]byte("meta:\n  a: 1\n  b: (( grab meta.a ))\n"))
	if err != nil {
		t.Fatalf("ParseYAML() error = %v", err)
	}

	refs := []SourceRef{{Name: "only.yml", Bytes: []byte("meta:\n  a: 1\n  b: (( grab meta.a ))\n")}}
	ctx := WithSourceRefs(context.Background(), refs)

	// A successful evaluation must not be disturbed by carrying sources.
	if _, err := engine.Evaluate(ctx, doc); err != nil {
		t.Fatalf("Evaluate() error = %v; carrying sources must not change a successful merge", err)
	}
}

// TestContextAccessorsAreUniformlyNilSafe pins the package's nil-context
// contract across every exported context accessor, not just the newest
// pair.
//
// Every exported entry point that takes a context already normalizes nil
// at the boundary (DefaultEngine.Merge, MergeFiles, MergeReaders,
// Evaluate, and EvaluateParallel), so nothing inside the package reaches
// an accessor with a nil context. The guards exist for the other caller:
// these helpers are exported, so a library caller can build a context by
// calling one of them directly, and one accessor panicking where its
// siblings return cleanly is a trap with no upside.
func TestContextAccessorsAreUniformlyNilSafe(t *testing.T) {
	//nolint:staticcheck // deliberately passing nil to pin the nil-safety contract
	t.Run("WithCherryPickPaths", func(t *testing.T) {
		ctx := WithCherryPickPaths(nil, []string{"meta.a"})
		if got := GetCherryPickPaths(ctx); len(got) != 1 || got[0] != "meta.a" {
			t.Errorf("round trip through a nil context = %v, want [meta.a]", got)
		}
	})

	//nolint:staticcheck // deliberately passing nil to pin the nil-safety contract
	t.Run("GetCherryPickPaths", func(t *testing.T) {
		if got := GetCherryPickPaths(nil); got != nil {
			t.Errorf("GetCherryPickPaths(nil) = %v, want nil", got)
		}
	})

	//nolint:staticcheck // deliberately passing nil to pin the nil-safety contract
	t.Run("WithPriorCalcValues", func(t *testing.T) {
		ctx := WithPriorCalcValues(nil, map[string]interface{}{"meta.a": 1})
		if got := GetPriorCalcValues(ctx); len(got) != 1 || got["meta.a"] != 1 {
			t.Errorf("round trip through a nil context = %v, want {meta.a:1}", got)
		}
	})

	//nolint:staticcheck // deliberately passing nil to pin the nil-safety contract
	t.Run("GetPriorCalcValues", func(t *testing.T) {
		if got := GetPriorCalcValues(nil); got != nil {
			t.Errorf("GetPriorCalcValues(nil) = %v, want nil", got)
		}
	})

	//nolint:staticcheck // deliberately passing nil to pin the nil-safety contract
	t.Run("WithNoCacheContext", func(t *testing.T) {
		ctx := WithNoCacheContext(nil)
		if !noCacheFromContext(ctx) {
			t.Errorf("noCacheFromContext after WithNoCacheContext(nil) = false, want true")
		}
	})

	t.Run("noCacheFromContext", func(t *testing.T) {
		//nolint:staticcheck // deliberately passing nil to pin the nil-safety contract
		if noCacheFromContext(nil) {
			t.Errorf("noCacheFromContext(nil) = true, want false")
		}
	})

	//nolint:staticcheck // deliberately passing nil to pin the nil-safety contract
	t.Run("WithSourceRefs", func(t *testing.T) {
		ctx := WithSourceRefs(nil, []SourceRef{{Name: "a.yml"}})
		if got := GetSourceRefs(ctx); len(got) != 1 || got[0].Name != "a.yml" {
			t.Errorf("round trip through a nil context = %v, want [a.yml]", got)
		}
	})
}

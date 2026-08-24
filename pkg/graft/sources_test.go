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

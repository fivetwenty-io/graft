package yq

import (
	"strings"
	"testing"
)

func TestDecoderLimitsDepth(t *testing.T) {
	_, err := NewDecoder(strings.NewReader("[[[1]]]"), DecodeOptions{DepthLimit: 2}).Next()
	if err == nil || !strings.Contains(err.Error(), "depth") {
		t.Fatalf("got %v, want a depth error", err)
	}
	if _, err := NewDecoder(strings.NewReader("[[[1]]]"), DecodeOptions{DepthLimit: 3}).Next(); err != nil {
		t.Fatalf("a document at the limit failed: %v", err)
	}
}

func TestWithDepthLimitRejectsNegative(t *testing.T) {
	_, err := Compile(`.`, WithDepthLimit(-1))
	if err == nil || !strings.Contains(err.Error(), "depth limit") {
		t.Fatalf("got %v, want a depth limit error", err)
	}
}

func TestWithDepthLimitBoundsEvaluation(t *testing.T) {
	doc := decodeOne(t, "[[1]]")
	_, err := collect(mustCompile(t, `map(map(.))`, WithDepthLimit(1)).Eval(t.Context(), doc))
	if err == nil || !strings.Contains(err.Error(), "exceeds depth limit of 1") {
		t.Fatalf("got %v, want a depth limit error", err)
	}
	if _, err := collect(mustCompile(t, `map(map(.))`).Eval(t.Context(), doc)); err != nil {
		t.Fatalf("default limit: %v", err)
	}
}

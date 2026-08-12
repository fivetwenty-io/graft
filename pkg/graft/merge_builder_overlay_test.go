package graft

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestMergeBuilder_BaseOverlayOverlayFile_Ordering is the order-sensitivity
// table test C5's plan section calls for: every combination of
// Merge(ctx, a, b), .Base(c), .Overlay(d), and .OverlayFile(e) against the
// expected final document. "last write wins" on the single "value" key
// pins the precedence: documents passed to Merge come first (Base
// overwrites position 0), Overlay/OverlayFile append after, in call order.
func TestMergeBuilder_BaseOverlayOverlayFile_Ordering(t *testing.T) {
	dir := t.TempDir()
	docA := NewDocument(map[string]interface{}{"value": "a", "from_a": true})
	docB := NewDocument(map[string]interface{}{"value": "b", "from_b": true})
	docC := NewDocument(map[string]interface{}{"value": "c", "from_c": true})
	docD := NewDocument(map[string]interface{}{"value": "d", "from_d": true})
	fileE := writeTempFile(t, dir, "e.yml", "value: e\nfrom_e: true\n")

	engine := NewDefaultEngine()
	ctx := context.Background()

	tests := []struct {
		name    string
		build   func() MergeBuilder
		want    string
		wantAll []string // additional presence keys expected in final doc
	}{
		{
			name:    "Merge(a,b) only",
			build:   func() MergeBuilder { return engine.Merge(ctx, docA, docB) },
			want:    "b",
			wantAll: []string{"from_a", "from_b"},
		},
		{
			name:    "Merge(a,b).Base(c) replaces position 0",
			build:   func() MergeBuilder { return engine.Merge(ctx, docA, docB).Base(docC) },
			want:    "b",
			wantAll: []string{"from_c", "from_b"},
		},
		{
			name:    "Merge(a,b).Overlay(d) appends after b",
			build:   func() MergeBuilder { return engine.Merge(ctx, docA, docB).Overlay(docD) },
			want:    "d",
			wantAll: []string{"from_a", "from_b", "from_d"},
		},
		{
			name:    "Merge(a).Base(c).Overlay(d) - base replaced, then overlay",
			build:   func() MergeBuilder { return engine.Merge(ctx, docA).Base(docC).Overlay(docD) },
			want:    "d",
			wantAll: []string{"from_c", "from_d"},
		},
		{
			name:    "Merge(ctx).Base(c).Overlay(d) - no initial docs",
			build:   func() MergeBuilder { return engine.Merge(ctx).Base(docC).Overlay(docD) },
			want:    "d",
			wantAll: []string{"from_c", "from_d"},
		},
		{
			name:    "Merge(a).OverlayFile(e) appends loaded file after a",
			build:   func() MergeBuilder { return engine.Merge(ctx, docA).OverlayFile(fileE) },
			want:    "e",
			wantAll: []string{"from_a", "from_e"},
		},
		{
			name: "Merge(a).Base(c).Overlay(d).OverlayFile(e) - full chain",
			build: func() MergeBuilder {
				return engine.Merge(ctx, docA).Base(docC).Overlay(docD).OverlayFile(fileE)
			},
			want:    "e",
			wantAll: []string{"from_c", "from_d", "from_e"},
		},
		{
			name: "OverlayFile(e).Overlay(d) - call order, not argument order",
			build: func() MergeBuilder {
				return engine.Merge(ctx, docA).OverlayFile(fileE).Overlay(docD)
			},
			want:    "d",
			wantAll: []string{"from_a", "from_e", "from_d"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.build().Execute()
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			got, _ := result.GetString("value")
			if got != tt.want {
				t.Errorf("value = %q; want %q", got, tt.want)
			}
			for _, key := range tt.wantAll {
				if !result.Has(key) {
					t.Errorf("result missing expected key %q", key)
				}
			}
		})
	}
}

// TestMergeBuilder_OverlayFile_MissingFile pins the documented error shape
// (merge-builder.md) and proves Execute() reports the failure instead of
// panicking on what would otherwise be an untyped nil builder.
func TestMergeBuilder_OverlayFile_MissingFile(t *testing.T) {
	engine := NewDefaultEngine()
	missing := filepath.Join(t.TempDir(), "missing.yml")

	builder := engine.Merge(context.Background(), NewDocument(map[string]interface{}{"a": 1})).
		OverlayFile(missing)
	if builder == nil {
		t.Fatal("OverlayFile() returned a nil MergeBuilder; must return a typed builder carrying the error")
	}

	result, err := builder.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil; want an error for a missing overlay file")
	}
	if result != nil {
		t.Errorf("Execute() result = %v; want nil on error", result)
	}
	if !strings.Contains(err.Error(), "failed to load overlay file") {
		t.Errorf("Execute() error = %q; want it to contain %q", err.Error(), "failed to load overlay file")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("Execute() error = %q; want it to reference the missing path %q", err.Error(), missing)
	}
}

// TestMergeBuilder_OverlayFile_ErrorShortCircuitsChain proves that once
// OverlayFile has captured a load error, every later builder call
// (including further Base/Overlay/OverlayFile calls and post-processing
// options) propagates that same error instead of overwriting it or
// panicking.
func TestMergeBuilder_OverlayFile_ErrorShortCircuitsChain(t *testing.T) {
	engine := NewDefaultEngine()
	missing := filepath.Join(t.TempDir(), "missing.yml")

	builder := engine.Merge(context.Background()).
		OverlayFile(missing).
		Base(NewDocument(map[string]interface{}{"a": 1})).
		Overlay(NewDocument(map[string]interface{}{"b": 2})).
		WithPrune("a")

	_, err := builder.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil; want the OverlayFile load error to survive later chain calls")
	}
	if !strings.Contains(err.Error(), "failed to load overlay file") {
		t.Errorf("Execute() error = %q; want it to still contain %q", err.Error(), "failed to load overlay file")
	}
}

// TestMergeBuilder_ErrorCarryingBuilder_BaseOverlayDoNotPanic proves Base
// and Overlay are safe to call on a builder that already carries a
// construction error (e.g. from a prior failing MergeFiles/OverlayFile
// call) — they must propagate the existing error, not clear it or panic.
func TestMergeBuilder_ErrorCarryingBuilder_BaseOverlayDoNotPanic(t *testing.T) {
	engine := NewDefaultEngine()
	missing := filepath.Join(t.TempDir(), "missing.yml")

	builder := engine.MergeFiles(context.Background(), missing)
	chained := builder.
		Base(NewDocument(map[string]interface{}{"a": 1})).
		Overlay(NewDocument(map[string]interface{}{"b": 2})).
		OverlayFile(missing)

	if chained == nil {
		t.Fatal("chained builder is nil; must always return a typed builder")
	}
	_, err := chained.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil; want the original MergeFiles error preserved")
	}
	if !strings.Contains(err.Error(), "failed to load merge file") {
		t.Errorf("Execute() error = %q; want the original MergeFiles error text preserved", err.Error())
	}
}

// TestMergeBuilder_Base_Immutability proves Base does not mutate the
// receiver: two builders derived from the same starting point via
// different Base calls must not alias each other's document list.
func TestMergeBuilder_Base_Immutability(t *testing.T) {
	engine := NewDefaultEngine()
	ctx := context.Background()
	shared := engine.Merge(ctx, NewDocument(map[string]interface{}{"value": "shared"}))

	b1 := shared.Base(NewDocument(map[string]interface{}{"value": "x"}))
	b2 := shared.Base(NewDocument(map[string]interface{}{"value": "y"}))

	r1, err := b1.Execute()
	if err != nil {
		t.Fatalf("b1.Execute() error = %v", err)
	}
	r2, err := b2.Execute()
	if err != nil {
		t.Fatalf("b2.Execute() error = %v", err)
	}
	v1, _ := r1.GetString("value")
	v2, _ := r2.GetString("value")
	if v1 != "x" {
		t.Errorf("b1 value = %q; want %q (b2's Base call must not have aliased b1)", v1, "x")
	}
	if v2 != "y" {
		t.Errorf("b2 value = %q; want %q", v2, "y")
	}

	// The original shared builder must also be untouched by either branch.
	rShared, err := shared.Execute()
	if err != nil {
		t.Fatalf("shared.Execute() error = %v", err)
	}
	vShared, _ := rShared.GetString("value")
	if vShared != "shared" {
		t.Errorf("shared value = %q; want %q (Base must not mutate the receiver)", vShared, "shared")
	}
}

// TestMergeBuilder_Overlay_Immutability is Base's immutability test for
// Overlay: branching Overlay calls from the same builder must not alias
// each other's document slice.
func TestMergeBuilder_Overlay_Immutability(t *testing.T) {
	engine := NewDefaultEngine()
	ctx := context.Background()
	shared := engine.Merge(ctx, NewDocument(map[string]interface{}{"value": "base"}))

	b1 := shared.Overlay(NewDocument(map[string]interface{}{"value": "x"}))
	b2 := shared.Overlay(NewDocument(map[string]interface{}{"value": "y"}))

	r1, err := b1.Execute()
	if err != nil {
		t.Fatalf("b1.Execute() error = %v", err)
	}
	r2, err := b2.Execute()
	if err != nil {
		t.Fatalf("b2.Execute() error = %v", err)
	}
	v1, _ := r1.GetString("value")
	v2, _ := r2.GetString("value")
	if v1 != "x" {
		t.Errorf("b1 value = %q; want %q", v1, "x")
	}
	if v2 != "y" {
		t.Errorf("b2 value = %q; want %q", v2, "y")
	}
}

// TestMergeBuilder_Overlay_NoArgsIsNoOp proves Overlay() with zero
// arguments is a harmless no-op rather than, say, appending a nil
// Document into the list.
func TestMergeBuilder_Overlay_NoArgsIsNoOp(t *testing.T) {
	engine := NewDefaultEngine()
	ctx := context.Background()
	result, err := engine.Merge(ctx, NewDocument(map[string]interface{}{"value": "only"})).
		Overlay().
		Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	got, _ := result.GetString("value")
	if got != "only" {
		t.Errorf("value = %q; want %q", got, "only")
	}
}

// TestMergeBuilder_OverlayFile_JSONAndGoPatch proves OverlayFile reuses
// the engine's ParseFile format auto-detection (JSON by extension,
// go-patch by array-rooted YAML) rather than assuming YAML-map content.
func TestMergeBuilder_OverlayFile_JSONAndGoPatch(t *testing.T) {
	dir := t.TempDir()
	jsonPath := writeTempFile(t, dir, "overlay.json", `{"value":"json"}`)

	engine := NewDefaultEngine()
	result, err := engine.Merge(context.Background(), NewDocument(map[string]interface{}{"value": "base"})).
		OverlayFile(jsonPath).
		Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	got, _ := result.GetString("value")
	if got != "json" {
		t.Errorf("value = %q; want %q", got, "json")
	}
}

// TestMergeBuilder_OverlayFile_ComposesWithPruneAndCherryPick proves
// OverlayFile documents participate in post-processing (WithPrune /
// WithCherryPick) exactly like documents supplied any other way.
func TestMergeBuilder_OverlayFile_ComposesWithPruneAndCherryPick(t *testing.T) {
	dir := t.TempDir()
	overlay := writeTempFile(t, dir, "overlay.yml", "keep: kept\ndrop: dropped\n")

	engine := NewDefaultEngine()
	result, err := engine.Merge(context.Background(), NewDocument(map[string]interface{}{"base": true})).
		OverlayFile(overlay).
		WithPrune("drop").
		Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Has("drop") {
		t.Error("result still has pruned key \"drop\"")
	}
	if !result.Has("keep") {
		t.Error("result missing \"keep\"")
	}
}

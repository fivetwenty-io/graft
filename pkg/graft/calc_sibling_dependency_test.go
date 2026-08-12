package graft_test

// (( calc "floor(width)" )) resolves the bare named variable "width" by
// trying, in order, a sibling of the calc call's own path (op_calc.go's
// resolveCalcVariable) then an absolute path from the document root.
// calcBareNameDependencies (op_calc.go) mirrors both candidate cursors
// into the operator's own Dependencies() result, specifically so the
// evaluator orders the sibling's own calc call ahead of this one when the
// sibling also needs evaluating. That mirroring depends on ev.Here being
// set to the CURRENT opcall's own path at the moment Dependencies() is
// called — dataFlowContext.buildDependencyGraph (evaluator.go) iterates
// every discovered opcall and calls a.Dependencies(ctx.ev, ctx.locs) for
// each one without ever setting ctx.ev.Here to that opcall's own path
// first (unlike Opcall.Run, which does set ev.Here = op.where before
// running). ev.Here is left wherever the preceding tree scan pass ended
// (the root), so the sibling candidate's Depth() check
// (resolveCalcVariable's own "ev.Here.Depth() >= 1" guard mirrors the
// same check in calcBareNameDependencies) is skipped entirely, no valid
// dependency edge is ever recorded, and the two calc calls run in
// whatever order Go's (randomized) map iteration over ctx.all happens to
// produce. When the sibling runs second, its own calc call resolves
// against the still-unevaluated marker text and errors with a
// type-mismatch instead of the numeric value.

import (
	"testing"

	_ "github.com/fivetwenty-io/graft/pkg/graft/operators"
)

// TestCalcBareSiblingVariable_Resolves is the exact repro from the bug
// report.
func TestCalcBareSiblingVariable_Resolves(t *testing.T) {
	doc, err := mergeYAML(t, "sizing:\n  width: (( calc \"10 * 2\" ))\n  area: (( calc \"floor(width)\" ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("sizing.area")
	if err != nil {
		t.Fatalf("failed to read sizing.area: %v", err)
	}
	if got != int64(20) {
		t.Fatalf("expected 20, got %v (%T)", got, got)
	}
}

// TestCalcBareSiblingVariable_Repeated runs the same document merge many
// times: map iteration order in Go is randomized per-run, so a flaky
// ordering bug would not necessarily reproduce on every single
// invocation. This guards against a fix (or a test) that only happens to
// pass by chance.
func TestCalcBareSiblingVariable_Repeated(t *testing.T) {
	for i := 0; i < 25; i++ {
		doc, err := mergeYAML(t, "sizing:\n  width: (( calc \"10 * 2\" ))\n  area: (( calc \"floor(width)\" ))\n")
		if err != nil {
			t.Fatalf("iteration %d: unexpected error: %v", i, err)
		}
		got, err := doc.Get("sizing.area")
		if err != nil {
			t.Fatalf("iteration %d: failed to read sizing.area: %v", i, err)
		}
		if got != int64(20) {
			t.Fatalf("iteration %d: expected 20, got %v (%T)", i, got, got)
		}
	}
}

// TestCalcFullyQualifiedSiblingPath_StillWorks pins the already-working
// workaround (the full qualified path instead of the bare sibling name)
// keeps working unchanged.
func TestCalcFullyQualifiedSiblingPath_StillWorks(t *testing.T) {
	doc, err := mergeYAML(t, "sizing:\n  width: (( calc \"10 * 2\" ))\n  area: (( calc \"floor(sizing.width)\" ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("sizing.area")
	if err != nil {
		t.Fatalf("failed to read sizing.area: %v", err)
	}
	if got != int64(20) {
		t.Fatalf("expected 20, got %v (%T)", got, got)
	}
}

// TestCalcBareSiblingVariable_TopLevel pins the same bare-sibling-name
// resolution at the top level of the document (ev.Here.Depth() == 1,
// the boundary condition resolveCalcVariable's own guard checks).
func TestCalcBareSiblingVariable_TopLevel(t *testing.T) {
	doc, err := mergeYAML(t, "width: (( calc \"10 * 2\" ))\narea: (( calc \"floor(width)\" ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("area")
	if err != nil {
		t.Fatalf("failed to read area: %v", err)
	}
	if got != int64(20) {
		t.Fatalf("expected 20, got %v (%T)", got, got)
	}
}

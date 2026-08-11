package graft

import (
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// targetAwareStubOperator is a minimal Operator that also implements
// TargetAware, for exercising Opcall.Run's target dispatch without
// depending on the operators package (which cannot be imported here — it
// imports this package).
type targetAwareStubOperator struct {
	supports bool
	ranWith  string // captures ev.Target as observed inside Run
}

func (s *targetAwareStubOperator) Setup() error { return nil }

func (s *targetAwareStubOperator) Run(ev *Evaluator, args []*Expr) (*Response, error) {
	s.ranWith = ev.Target
	return &Response{Type: Replace, Value: "ok"}, nil
}

func (s *targetAwareStubOperator) Dependencies(_ *Evaluator, _ []*Expr, _, auto []*tree.Cursor) []*tree.Cursor {
	return auto
}

func (s *targetAwareStubOperator) Phase() OperatorPhase { return EvalPhase }

func (s *targetAwareStubOperator) SupportsTarget() bool { return s.supports }

// plainStubOperator implements Operator but not TargetAware at all.
type plainStubOperator struct {
	ranWith string
}

func (s *plainStubOperator) Setup() error { return nil }

func (s *plainStubOperator) Run(ev *Evaluator, args []*Expr) (*Response, error) {
	s.ranWith = ev.Target
	return &Response{Type: Replace, Value: "ok"}, nil
}

func (s *plainStubOperator) Dependencies(_ *Evaluator, _ []*Expr, _, auto []*tree.Cursor) []*tree.Cursor {
	return auto
}

func (s *plainStubOperator) Phase() OperatorPhase { return EvalPhase }

func TestOpcallRun_TargetRejectedWhenOperatorDoesNotSupportIt(t *testing.T) {
	op := &plainStubOperator{}
	where, _ := tree.ParseCursor("x")
	opcall := &Opcall{
		op:     op,
		name:   "grab",
		target: "prod",
		where:  where,
	}

	ev := &Evaluator{Tree: map[string]interface{}{}}
	_, err := opcall.Run(ev)
	if err == nil {
		t.Fatalf("expected an error for an unsupported @target")
	}
	if !strings.Contains(err.Error(), "grab operator does not support an @target") {
		t.Fatalf("expected the rejection message, got: %v", err)
	}
	if !strings.Contains(err.Error(), "$.x:") {
		t.Fatalf("expected the standard '$.x:' prefix, got: %v", err)
	}
	if op.ranWith != "" {
		t.Fatalf("operator's Run must not be called when the target is rejected")
	}
}

func TestOpcallRun_TargetRejectedWhenSupportsTargetReturnsFalse(t *testing.T) {
	op := &targetAwareStubOperator{supports: false}
	opcall := &Opcall{op: op, name: "sort", target: "prod"}

	ev := &Evaluator{Tree: map[string]interface{}{}}
	_, err := opcall.Run(ev)
	if err == nil {
		t.Fatalf("expected an error")
	}
	if !strings.Contains(err.Error(), "sort operator does not support an @target") {
		t.Fatalf("expected the rejection message, got: %v", err)
	}
}

func TestOpcallRun_TargetAcceptedAndVisibleToOperator(t *testing.T) {
	op := &targetAwareStubOperator{supports: true}
	opcall := &Opcall{op: op, name: "vault", target: "prod"}

	ev := &Evaluator{Tree: map[string]interface{}{}}
	resp, err := opcall.Run(ev)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Value != "ok" {
		t.Fatalf("expected 'ok', got %v", resp.Value)
	}
	if op.ranWith != "prod" {
		t.Fatalf("expected the operator to observe ev.Target=='prod', got %q", op.ranWith)
	}
	if ev.Target != "" {
		t.Fatalf("expected ev.Target to be restored to '' after Run, got %q", ev.Target)
	}
}

func TestOpcallRun_NoTargetIsUnaffected(t *testing.T) {
	op := &plainStubOperator{}
	opcall := &Opcall{op: op, name: "grab"} // no target set

	ev := &Evaluator{Tree: map[string]interface{}{}}
	resp, err := opcall.Run(ev)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Value != "ok" {
		t.Fatalf("expected 'ok', got %v", resp.Value)
	}
}

func TestOpcall_TargetAccessor(t *testing.T) {
	opcall := &Opcall{target: "prod"}
	if opcall.Target() != "prod" {
		t.Fatalf("expected Target() to return 'prod', got %q", opcall.Target())
	}
}

// --- Parser-level: form (a) rejection message and form (b) parsing ---

func TestParseTarget_FormARejectedWithRedirectMessage(t *testing.T) {
	_, err := ParseOpcallWithParser(EvalPhase, `(( vault prod@"secret/foo:bar" ))`)
	if err == nil {
		t.Fatalf("expected form (a) to be rejected")
	}
	want := `vault target must be written as (( vault@<target> "path:key" )), not (( vault <target>@"path:key" ))`
	if err.Error() != want {
		t.Fatalf("expected %q, got %q", want, err.Error())
	}
}

func TestParseTarget_FormBParsesTargetOntoOpcall(t *testing.T) {
	opcall, err := ParseOpcallWithParser(EvalPhase, `(( vault@prod "secret/foo:bar" ))`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opcall.Target() != "prod" {
		t.Fatalf("expected target 'prod', got %q", opcall.Target())
	}
}

func TestParseTarget_NoTargetLeavesOpcallTargetEmpty(t *testing.T) {
	opcall, err := ParseOpcallWithParser(EvalPhase, `(( vault "secret/foo:bar" ))`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opcall.Target() != "" {
		t.Fatalf("expected no target, got %q", opcall.Target())
	}
}

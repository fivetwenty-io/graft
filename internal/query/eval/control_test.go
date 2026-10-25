package eval

import (
	"errors"
	"testing"

	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

func TestReduceForeachTryLabel(t *testing.T) {
	wantOutputs(t, `reduce .[] as $x (0; . + $x)`, `[1,2,3]`, `6`)
	wantOutputs(t, `[foreach .[] as $x (0; . + $x; [$x, .])]`, `[1,2]`, `[[1,1],[2,3]]`)
	wantOutputs(t, `try error("x") catch .`, `null`, `"x"`)
	wantOutputs(t, `.a.b?`, `{"a":1}`)
	wantOutputs(t, `[label $f | 1, 2, break $f, 3]`, `null`, `[1,2]`)
	wantOutputs(t, `try (1, error("x"), 3) catch "c"`, `null`, `1`, `"c"`)
}

func TestTryBoundaryAndErrorProvenance(t *testing.T) {
	wantError(t, `try 1 catch "caught" | error("outside")`, `null`, "outside")
	wantOutputs(t, `[.[] | try (if . == 2 then error("x") else . end) catch "c"]`, `[1,2,3]`, `[1,"c",3]`)
	wantOutputs(t, `label $f | try (1, break $f) catch "caught"`, `null`, `1`, `"caught"`) // pinned jq 1.8.2
	wantOutputs(t, `[first(range(10) | if . > 2 then error("too far") else . end)]`, `null`, `[0]`)
	wantOutputs(t, `[limit(2; 1, 2, error("x"))]`, `null`, `[1,2]`)
	wantOutputs(t, `reduce range(3) as $x (0; empty)`, `null`, `null`)
	wantOutputs(t, `reduce range(3) as $x (0; ., 10)`, `null`, `10`)
	wantOutputs(t, `[foreach range(3) as $x (0; (. + $x), 100)]`, `null`, `[0,100,101,100,102,100]`)
	wantOutputs(t, `[foreach range(3) as $x (0; empty)]`, `null`, `[]`)
}

func TestControlFlowEdges(t *testing.T) {
	wantOutputs(t, `label $f | try break $f catch .`, `null`, `{"__jq":0}`)
	wantOutputs(t, `[label $f | 1, (label $g | 2, break $f, 3), 4]`, `null`, `[1,2]`)
	wantOutputs(t, `label $f | 1 | (., break $f) | . + 1`, `null`, `2`)
	wantOutputs(t, `[foreach range(3) as $x (0; if $x == 1 then empty else . + 1 end)]`, `null`, `[1,1]`)
	wantOutputs(t, `[reduce (1, 2) as $x (0, 10; . + $x)]`, `null`, `[3,13]`)
	wantOutputs(t, `try error(null) catch .`, `null`, `null`)
	wantOutputs(t, `try error catch .`, `{"a":1}`, `{"a":1}`)
	wantOutputs(t, `[.[] | (1 / .)?]`, `[1,0,2]`, `[1,0.5]`)
	wantOutputs(t, `[limit(0; 1, 2)]`, `null`, `[]`)
	wantOutputs(t, `[limit(1; limit(2; 1, 2, 3))]`, `null`, `[1]`)
	wantError(t, `[limit(-1; 1, 2)]`, `null`, "limit doesn't support negative count")
	wantError(t, `break $f`, `null`, "$*label-f is not defined")
	wantError(t, `[error("x").a?]`, `null`, "x")
	wantError(t, `try error("x") catch error("y")`, `null`, "y")
	wantError(t, `(1, 2) as [$a] | $a`, `null`, "Cannot index number with number (0)")
}

func TestTryLetsStopAndDownstreamErrorsPass(t *testing.T) {
	// The stop from first crosses the try inside it untouched.
	wantOutputs(t, `[first(try (1, 2) catch "c")]`, `null`, `[1]`)
	wantOutputs(t, `[first((1, 2)?)]`, `null`, `[1]`)
	// An error raised by the consumer of a try's output is not the try's.
	var outs []*value.Value
	ctx := newCtx()
	sentinel := errors.New("consumer")
	err := Run(ctx, mustParse(t, `try (1, 2) catch "c"`), NewEnv(ctx.Dialect.Builtins()), []*value.Value{value.Null()}, func(v *value.Value, _ value.Path) error {
		outs = append(outs, v)
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("got %v, want the consumer's error", err)
	}
	wantEqual(t, "try", outs, []string{`1`})
	// A runtime error from the consumer is not caught either.
	outs = nil
	err = Run(ctx, mustParse(t, `try (1, 2) catch "c"`), NewEnv(ctx.Dialect.Builtins()), []*value.Value{value.Null()}, func(v *value.Value, _ value.Path) error {
		outs = append(outs, v)
		return &query.RuntimeError{Msg: "boom"}
	})
	var re *query.RuntimeError
	if !errors.As(err, &re) || re.Msg != "boom" {
		t.Fatalf("got %v, want boom", err)
	}
	var down *DownstreamError
	if errors.As(err, &down) {
		t.Fatal("Run should unwrap the downstream marker before returning")
	}
	wantEqual(t, "try", outs, []string{`1`})
}

func TestHaltErrorPassesTry(t *testing.T) {
	ctx := newCtx()
	ctx.Dialect.Builtins().Register("halt_error", 1, func(_ *runtime.Ctx, in *value.Value, args []runtime.Thunk, _ runtime.Emit) error {
		return args[0].Each(in, func(code *value.Value, _ value.Path) error {
			return &query.HaltError{Code: int(code.Float()), Message: in}
		})
	})
	_, err := mustEval(t, ctx, `try ("bye" | halt_error(3)) catch "c"`, []string{`null`})
	var he *query.HaltError
	if !errors.As(err, &he) || he.Code != 3 || he.Message.Str() != "bye" {
		t.Fatalf("got %v, want a halt with code 3", err)
	}
}

package runtime

import (
	"context"
	"errors"
	"go/build"
	"slices"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/query/ast"
	"github.com/fivetwenty-io/graft/pkg/decimal"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

func wantEqual[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func wantErrContains(t *testing.T, err error, sub string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want one containing %q", sub)
	}
	if !strings.Contains(err.Error(), sub) {
		t.Fatalf("error = %q, want it to contain %q", err.Error(), sub)
	}
}

func emitOne(v *value.Value) Native {
	return func(_ *Ctx, _ *value.Value, _ []Thunk, emit Emit) error {
		return emit(v, nil)
	}
}

func TestRegistryKeysByNameAndArity(t *testing.T) {
	var r Registry
	r.Register("f", 0, emitOne(value.Str("zero")))
	r.Register("f", 1, emitOne(value.Str("one")))

	for arity, want := range []string{"zero", "one"} {
		fn, ok := r.Lookup("f", arity)
		if !ok {
			t.Fatalf("f/%d is missing", arity)
		}
		var got *value.Value
		if err := fn(nil, value.Null(), nil, func(v *value.Value, _ value.Path) error { got = v; return nil }); err != nil {
			t.Fatal(err)
		}
		wantEqual(t, got.Str(), want)
	}
	if _, ok := r.Lookup("f", 2); ok {
		t.Fatal("f/2 should be missing")
	}
	if _, ok := r.Lookup("g", 0); ok {
		t.Fatal("g/0 should be missing")
	}
}

func TestRegistryLaterRegistrationWins(t *testing.T) {
	var r Registry
	r.Register("f", 0, emitOne(value.Str("first")))
	r.Register("f", 0, emitOne(value.Str("second")))
	fn, _ := r.Lookup("f", 0)
	var got *value.Value
	_ = fn(nil, value.Null(), nil, func(v *value.Value, _ value.Path) error { got = v; return nil })
	wantEqual(t, got.Str(), "second")
}

func TestRegistryRecordsSingleOutputNatives(t *testing.T) {
	var r Registry
	r.RegisterSingle("length", 0, emitOne(value.Null()))
	r.Register("range", 1, emitOne(value.Null()))
	if !r.Single("length", 0) {
		t.Fatal("length/0 should be single")
	}
	if _, ok := r.Lookup("length", 0); !ok {
		t.Fatal("length/0 is missing")
	}
	if r.Single("range", 1) || r.Single("length", 1) {
		t.Fatal("only length/0 should be single")
	}
	r.Register("length", 0, emitOne(value.Null()))
	if r.Single("length", 0) {
		t.Fatal("a later Register should clear single")
	}
	var nilReg *Registry
	if nilReg.Single("length", 0) {
		t.Fatal("a nil Registry has nothing single")
	}
}

func TestRegistryNamesIncludeThePrelude(t *testing.T) {
	var r Registry
	r.Register("length", 0, emitOne(value.Null()))
	r.Register("range", 2, emitOne(value.Null()))
	r.AddPrelude(&ast.FuncDef{Name: "map", Params: []string{"f"}, Body: &ast.Identity{},
		Rest: &ast.FuncDef{Name: "select", Params: []string{"f"}, Body: &ast.Identity{}}})
	r.AddPrelude(&ast.FuncDef{Name: "add", Body: &ast.Identity{}})

	wantEqual(t, strings.Join(r.Names(), " "), "add/0 length/0 map/1 range/2 select/1")
	wantEqual(t, len(r.Prelude()), 2)
}

func TestNewCtxPicksComparisonsFromTheNumberMode(t *testing.T) {
	big := value.NumLit(decimal.MustParse("100000000000000000000"), "100000000000000000000")
	next := value.NumLit(decimal.MustParse("100000000000000000001"), "100000000000000000001")

	exact := NewCtx(nil, NumbersExact)
	wantEqual(t, exact.Equal(big, next), false)
	wantEqual(t, exact.Compare(big, next), -1)

	float := NewCtx(nil, NumbersFloat)
	wantEqual(t, float.Equal(big, next), true)
	wantEqual(t, float.Compare(big, next), 0)
	wantEqual(t, float.Numbers, NumbersFloat)
}

type capsDialect struct{ Dialect }

func (capsDialect) Capabilities() Capabilities { return Capabilities{Env: true} }

func TestNewCtxDefaults(t *testing.T) {
	c := NewCtx(capsDialect{}, NumbersExact)
	wantEqual(t, c.Caps, Capabilities{Env: true})
	wantEqual(t, c.DepthLimit, DefaultDepthLimit)
	wantEqual(t, c.Deadline != nil, true)
	wantEqual(t, c.Stderr != nil, true)
	wantEqual(t, c.Env != nil, true)
}

func TestNumberReadsBoundedValues(t *testing.T) {
	c := NewCtx(nil, NumbersExact)
	d, err := c.Number(value.NumLit(decimal.MustParse("1.50"), "1.50"))
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, d.String(), "1.5")
}

func TestNumberRaisesTheBoundOnlyWhenTheValueIsNeeded(t *testing.T) {
	huge := value.NumText(strings.Repeat("9", 100001))
	vast := value.NumText("1e1000000000")

	c := NewCtx(nil, NumbersExact)
	_, err := c.Number(huge)
	wantErrContains(t, err, "number exceeds 100000 significant digits")
	var re *query.RuntimeError
	if !errors.As(err, &re) {
		t.Fatalf("error %T is not a *query.RuntimeError", err)
	}
	_, err = c.Number(vast)
	wantErrContains(t, err, "number exponent exceeds 999999999")

	f := NewCtx(nil, NumbersFloat)
	d, err := f.Number(huge)
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, d.FloatString(), "Infinity")
	d, err = f.Number(vast)
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, d.FloatString(), "Infinity")
}

func TestEnterCountsDepthAgainstTheLimit(t *testing.T) {
	c := NewCtx(nil, NumbersExact)
	c.DepthLimit = 3
	for range 3 {
		if err := c.Enter(); err != nil {
			t.Fatal(err)
		}
	}
	wantErrContains(t, c.Enter(), "exceeds depth limit of 3")
	c.Leave()
	if err := c.Enter(); err != nil {
		t.Fatalf("a level freed by Leave should be usable again: %v", err)
	}
}

func TestZeroDepthLimitMeansTheDefault(t *testing.T) {
	var c Ctx
	for range DefaultDepthLimit {
		if err := c.Enter(); err != nil {
			t.Fatal(err)
		}
	}
	wantErrContains(t, c.Enter(), "exceeds depth limit of 10000")
}

func TestTickChecksTheDeadlineEvery1024Calls(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	c := NewCtx(nil, NumbersExact)
	c.Deadline = canceled
	var err error
	n := 0
	for err == nil && n < 2048 {
		n++
		err = c.Tick()
	}
	wantEqual(t, n, 1024)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}

func TestAssignModes(t *testing.T) {
	wantEqual(t, AssignSet.Op(), "=")
	wantEqual(t, AssignUpdate.Op(), "|=")
	wantEqual(t, AssignSet.Arith(), false)
	add := AssignArith("+=", func(cur, rhs *value.Value) (*value.Value, error) {
		return value.NumDec(decimal.FromInt64(int64(cur.Float() + rhs.Float()))), nil
	})
	wantEqual(t, add.Op(), "+=")
	wantEqual(t, add.Arith(), true)
	v, err := add.Apply(value.Num(1), value.Num(2))
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, v.Float(), 3.0)
}

func TestRuntimeImportsNoEvaluatorOrBuiltins(t *testing.T) {
	p, err := build.Import("github.com/fivetwenty-io/graft/internal/query/runtime", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	allowed := []string{
		"github.com/fivetwenty-io/graft/internal/query/ast",
		"github.com/fivetwenty-io/graft/pkg/decimal",
		"github.com/fivetwenty-io/graft/pkg/query",
		"github.com/fivetwenty-io/graft/pkg/query/value",
	}
	for _, imp := range p.Imports {
		if strings.Contains(imp, ".") && !slices.Contains(allowed, imp) {
			t.Errorf("internal/query/runtime imports %s", imp)
		}
	}
}

package builtin_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/query/builtin"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// consts is an argument that emits each of its values in turn.
type consts []*value.Value

func (c consts) Each(_ *value.Value, emit runtime.Emit) error {
	for _, v := range c {
		if err := emit(v, nil); err != nil {
			return err
		}
	}
	return nil
}

func registry() *runtime.Registry {
	r := &runtime.Registry{}
	builtin.RegisterCore(r)
	return r
}

// call runs the native name on in with each argument's values and
// returns the outputs as JSON text.
func call(t *testing.T, ctx *runtime.Ctx, name string, in *value.Value, args ...consts) ([]string, error) {
	t.Helper()
	fn, ok := registry().Lookup(name, len(args))
	if !ok {
		t.Fatalf("%s/%d is not registered", name, len(args))
	}
	thunks := make([]runtime.Thunk, len(args))
	for i, a := range args {
		thunks[i] = a
	}
	var out []string
	err := fn(ctx, in, thunks, func(v *value.Value, _ value.Path) error {
		out = append(out, runtime.JSON(v))
		return nil
	})
	return out, err
}

func exact() *runtime.Ctx { return runtime.NewCtx(nil, runtime.NumbersExact) }

func n(f float64) *value.Value { return value.Num(f) }
func s(v string) *value.Value  { return value.Str(v) }

func wantCall(t *testing.T, name string, in *value.Value, args []consts, want string) {
	t.Helper()
	out, err := call(t, exact(), name, in, args...)
	if err != nil {
		t.Errorf("%s: %v", name, err)
		return
	}
	if got := strings.Join(out, " "); got != want {
		t.Errorf("%s on %s: got %s, want %s", name, runtime.JSON(in), got, want)
	}
}

func wantFail(t *testing.T, err error, msg string) {
	t.Helper()
	var re *query.RuntimeError
	if !errors.As(err, &re) || re.Msg != msg {
		t.Errorf("got %v, want runtime error %q", err, msg)
	}
}

func TestRegisterCoreMarksSingleNatives(t *testing.T) {
	r := registry()
	for _, na := range []struct {
		name  string
		arity int
	}{
		{"length", 0}, {"keys", 0}, {"has", 1}, {"_plus", 2}, {"_negate", 0}, {"tostring", 0},
		{"error", 1}, {"pow", 2},
	} {
		if !r.Single(na.name, na.arity) {
			t.Errorf("%s/%d is not single", na.name, na.arity)
		}
	}
}

func TestNativesCombineArgumentOutputs(t *testing.T) {
	wantCall(t, "_plus", value.Null(), []consts{{n(1), n(2)}, {n(10), n(20)}}, "11 12 21 22")
	wantCall(t, "has", value.Obj(value.Pair{Key: "a", Value: n(1)}), []consts{{s("a"), s("b")}}, "true false")
}

func TestNativesReturnEmitErrorsUnchanged(t *testing.T) {
	stop := errors.New("downstream")
	r := registry()
	for _, c := range []struct {
		name string
		args []runtime.Thunk
	}{
		{"length", nil},
		{"_plus", []runtime.Thunk{consts{n(1)}, consts{n(2)}}},
	} {
		fn, _ := r.Lookup(c.name, len(c.args))
		err := fn(exact(), value.Arr(n(1)), c.args, func(*value.Value, value.Path) error { return stop })
		if err != stop { //nolint:errorlint // the native must return the emit's error itself, not a wrapper
			t.Errorf("%s: got %v, want the emit's error itself", c.name, err)
		}
	}
}

func TestExactAndFloatArithmetic(t *testing.T) {
	out, err := call(t, exact(), "_divide", value.Null(), consts{n(1)}, consts{n(3)})
	if err != nil || out[0] != "0.3333333333333333333333333333333333" {
		t.Errorf("exact divide: got %v, %v", out, err)
	}
	float := runtime.NewCtx(nil, runtime.NumbersFloat)
	out, err = call(t, float, "_divide", value.Null(), consts{n(1)}, consts{n(3)})
	if err != nil || out[0] != "0.3333333333333333" {
		t.Errorf("float divide: got %v, %v", out, err)
	}
	_, err = call(t, exact(), "_mod", value.Null(), consts{n(1)}, consts{n(0)})
	wantFail(t, err, `number (1) and number (0) cannot be divided (remainder) because the divisor is zero`)
	_, err = call(t, exact(), "_multiply", value.Null(), consts{s("ab")}, consts{n(1 << 30)})
	wantFail(t, err, `Repeat string result too long`)
	huge := value.NumText(strings.Repeat("9", 100001))
	_, err = call(t, exact(), "_plus", value.Null(), consts{huge}, consts{n(1)})
	wantFail(t, err, `number exceeds 100000 significant digits`)
}

func TestRaiseKeepsTheValue(t *testing.T) {
	v := value.Obj(value.Pair{Key: "a", Value: n(1)})
	var re *query.RuntimeError
	if err := builtin.Raise(v); !errors.As(err, &re) || re.Value != v || re.Msg != `{"a":1} (not a string)` {
		t.Errorf("got %v", err)
	}
	if err := builtin.Raise(s("boom")); !errors.As(err, &re) || re.Msg != "boom" {
		t.Errorf("got %v", err)
	}
}

func TestEnvNeedsTheCapability(t *testing.T) {
	ctx := exact()
	ctx.Caps.Env = false
	_, err := call(t, ctx, "env", value.Null())
	if err == nil || err.Error() != "env operations are disabled" {
		t.Errorf("got %v", err)
	}
	ctx.Caps.Env = true
	ctx.Env = func() map[string]string { return map[string]string{"A": "1"} }
	out, err := call(t, ctx, "env", value.Null())
	if err != nil || out[0] != `{"A":"1"}` {
		t.Errorf("got %v, %v", out, err)
	}
}

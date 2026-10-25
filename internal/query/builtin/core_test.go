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
		{"length", 0}, {"keys", 0}, {"has", 1}, {"_plus", 2}, {"_negate", 0}, {"setpath", 2},
		{"getpath", 1}, {"delpaths", 1}, {"tostring", 0}, {"error", 1}, {"isempty", 1},
		{"sort", 0}, {"_sort_by", 1}, {"split", 1}, {"@csv", 0}, {"format", 1}, {"pow", 2},
	} {
		if !r.Single(na.name, na.arity) {
			t.Errorf("%s/%d is not single", na.name, na.arity)
		}
	}
	for _, na := range []struct {
		name  string
		arity int
	}{{"range", 2}, {"range", 3}, {"limit", 2}, {"first", 1}, {"tostream", 0}, {"fromstream", 1}} {
		if _, ok := r.Lookup(na.name, na.arity); !ok {
			t.Errorf("%s/%d is missing", na.name, na.arity)
		}
		if r.Single(na.name, na.arity) {
			t.Errorf("%s/%d emits more than once and must not be single", na.name, na.arity)
		}
	}
}

func TestNativesCombineArgumentOutputs(t *testing.T) {
	wantCall(t, "_plus", value.Null(), []consts{{n(1), n(2)}, {n(10), n(20)}}, "11 12 21 22")
	wantCall(t, "range", value.Null(), []consts{{n(0), n(1)}, {n(2)}}, "0 1 1")
	wantCall(t, "has", value.Obj(value.Pair{Key: "a", Value: n(1)}), []consts{{s("a"), s("b")}}, "true false")
	wantCall(t, "limit", value.Null(), []consts{{n(2)}, {n(1), n(2), n(3)}}, "1 2")
	wantCall(t, "first", value.Null(), []consts{{n(7), n(8)}}, "7")
	wantCall(t, "isempty", value.Null(), []consts{{}}, "true")
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
		{"first", []runtime.Thunk{consts{n(1), n(2)}}},
		{"limit", []runtime.Thunk{consts{n(2)}, consts{n(1), n(2)}}},
		{"range", []runtime.Thunk{consts{n(0)}, consts{n(3)}}},
		{"tostream", nil},
		{"@sh", nil},
	} {
		fn, _ := r.Lookup(c.name, len(c.args))
		err := fn(exact(), value.Arr(n(1)), c.args, func(*value.Value, value.Path) error { return stop })
		if err != stop { //nolint:errorlint // the native must return the emit's error itself, not a wrapper
			t.Errorf("%s: got %v, want the emit's error itself", c.name, err)
		}
	}
}

func TestStopSignalsStayPrivate(t *testing.T) {
	// limit inside limit: the inner stop must not end the outer generator
	// early, and the outer stop must not leak as an error.
	r := registry()
	limit, _ := r.Lookup("limit", 2)
	inner := thunkFunc(func(in *value.Value, emit runtime.Emit) error {
		return limit(exact(), in, []runtime.Thunk{consts{n(1)}, consts{n(1), n(2)}}, emit)
	})
	var got []string
	err := limit(exact(), value.Null(), []runtime.Thunk{consts{n(3)}, multi{inner, inner, inner, inner}}, func(v *value.Value, _ value.Path) error {
		got = append(got, runtime.JSON(v))
		return nil
	})
	if err != nil || strings.Join(got, " ") != "1 1 1" {
		t.Errorf("got %v, %v", got, err)
	}
}

type thunkFunc func(*value.Value, runtime.Emit) error

func (f thunkFunc) Each(in *value.Value, emit runtime.Emit) error { return f(in, emit) }

// multi runs each thunk in turn.
type multi []runtime.Thunk

func (m multi) Each(in *value.Value, emit runtime.Emit) error {
	for _, t := range m {
		if err := t.Each(in, emit); err != nil {
			return err
		}
	}
	return nil
}

func TestIndexAndSlice(t *testing.T) {
	arr := value.Arr(n(1), n(2), n(3))
	for _, c := range []struct {
		c, key *value.Value
		want   string
	}{
		{arr, n(-0.5), "1"},
		{arr, n(-1.5), "3"},
		{arr, n(5), "null"},
		{arr, value.Arr(n(2), n(3)), "[1]"},
		{value.Null(), s("a"), "null"},
		{value.Obj(value.Pair{Key: "a", Value: n(1)}), s("a"), "1"},
		{arr, value.Obj(value.Pair{Key: "start", Value: n(1)}, value.Pair{Key: "end", Value: value.Null()}), "[2,3]"},
	} {
		got, err := builtin.Index(c.c, c.key)
		if err != nil || runtime.JSON(got) != c.want {
			t.Errorf("Index(%s, %s): got %v, %v", runtime.JSON(c.c), runtime.JSON(c.key), got, err)
		}
	}
	_, err := builtin.Index(s("abc"), n(0))
	wantFail(t, err, `Cannot index string with number (0)`)
	_, err = builtin.Index(value.Obj(), n(0))
	wantFail(t, err, `Cannot index object with number (0)`)
	got, err := builtin.Slice(s("aéb"), n(1), value.Null())
	if err != nil || got.Str() != "éb" {
		t.Errorf("string slice: got %v, %v", got, err)
	}
	got, err = builtin.Slice(arr, n(1.2), n(2.5))
	if err != nil || runtime.JSON(got) != "[2,3]" {
		t.Errorf("fractional slice: got %v, %v", got, err)
	}
	_, err = builtin.Slice(n(1), n(0), n(1))
	wantFail(t, err, `Cannot index number with object ({"start":0,"end":1})`)
}

func TestDelPathsResolvesAgainstTheOriginal(t *testing.T) {
	arr := value.Arr(n(1), n(2), n(3))
	for _, c := range []struct {
		paths []value.Path
		want  string
	}{
		{[]value.Path{{n(-1)}, {n(-2)}}, "[1]"},
		{[]value.Path{{n(0)}, {n(0)}}, "[2,3]"},
		{[]value.Path{{n(0)}, {value.Obj(value.Pair{Key: "start", Value: n(1)}, value.Pair{Key: "end", Value: value.Null()})}}, "[]"},
		{[]value.Path{{n(9)}}, "[1,2,3]"},
		{nil, "[1,2,3]"},
	} {
		got, err := builtin.DelPaths(exact(), arr, c.paths)
		if err != nil || runtime.JSON(got) != c.want {
			t.Errorf("DelPaths: got %v, %v, want %s", got, err, c.want)
		}
	}
	obj := value.Obj(value.Pair{Key: "a", Value: value.Obj(value.Pair{Key: "b", Value: n(1)})}, value.Pair{Key: "c", Value: n(2)})
	got, err := builtin.DelPaths(exact(), obj, []value.Path{{s("a"), s("b")}, {s("c")}, {s("a"), s("x"), s("y")}})
	if err != nil || runtime.JSON(got) != `{"a":{}}` {
		t.Errorf("nested DelPaths: got %v, %v", got, err)
	}
	_, err = builtin.DelPaths(exact(), s("abc"), []value.Path{{n(0)}})
	wantFail(t, err, `Cannot delete fields from string`)
	_, err = builtin.DelPaths(exact(), obj, []value.Path{{n(0)}})
	wantFail(t, err, `Cannot delete number field of object`)
}

func TestEditorCopiesEachContainerOnce(t *testing.T) {
	inner := value.Arr(n(1), n(2))
	root := value.Obj(value.Pair{Key: "a", Value: inner}, value.Pair{Key: "b", Value: n(0)})
	ed := builtin.NewEditor(root)
	for i, v := range []float64{10, 20} {
		cur, err := ed.Get(value.Path{s("a"), n(float64(i))})
		if err != nil {
			t.Fatal(err)
		}
		if err := ed.Set(value.Path{s("a"), n(float64(i))}, n(cur.Float()+v)); err != nil {
			t.Fatal(err)
		}
	}
	if err := ed.Set(value.Path{s("c"), n(1)}, s("x")); err != nil {
		t.Fatal(err)
	}
	got := ed.Value()
	wantJSON := `{"a":[11,22],"b":0,"c":[null,"x"]}`
	if runtime.JSON(got) != wantJSON {
		t.Errorf("got %s, want %s", runtime.JSON(got), wantJSON)
	}
	if runtime.JSON(root) != `{"a":[1,2],"b":0}` || runtime.JSON(inner) != `[1,2]` {
		t.Errorf("editing changed the original: %s", runtime.JSON(root))
	}
	if err := ed.Set(value.Path{s("b"), s("x")}, n(1)); err == nil {
		t.Errorf("setting a key of a number did not fail")
	}
	got, err := builtin.SetPath(value.Arr(n(1)), value.Path{n(-1)}, n(5))
	if err != nil || runtime.JSON(got) != "[5]" {
		t.Errorf("SetPath: got %v, %v", got, err)
	}
	_, err = builtin.SetPath(value.Arr(), value.Path{n(1 << 30)}, n(5))
	wantFail(t, err, `Array index too large`)
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
	_, err = call(t, exact(), "sort", value.Arr(huge, n(1)))
	wantFail(t, err, `number exceeds 100000 significant digits`)
	out, err = call(t, exact(), "sort", value.Arr(value.Arr(huge), value.Arr(n(1))))
	if err != nil || len(out) != 1 || !strings.HasPrefix(out[0], "[[1],[9") {
		t.Errorf("containers compare totally: got %.20v, %v", out, err)
	}
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

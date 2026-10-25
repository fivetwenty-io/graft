package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/query/ast"
	"github.com/fivetwenty-io/graft/internal/query/parser"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/decimal"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// testDialect is a small copy of jq's semantics, enough to drive the
// evaluator before the real builtins exist.
type testDialect struct {
	reg  *runtime.Registry
	caps runtime.Capabilities
}

func newTestDialect() *testDialect {
	return &testDialect{reg: testRegistry(), caps: runtime.Capabilities{Env: true, Files: true}}
}

func (d *testDialect) Name() string                       { return "jq-test" }
func (d *testDialect) Builtins() *runtime.Registry        { return d.reg }
func (d *testDialect) Capabilities() runtime.Capabilities { return d.caps }
func (d *testDialect) Compare(a, b *value.Value) int      { return value.Compare(a, b) }
func (d *testDialect) FormatError(err error) string       { return err.Error() }

func (d *testDialect) Truthy(v *value.Value) bool {
	k := v.Kind()
	return k != value.KindNull && k != value.KindFalse
}

func (d *testDialect) Index(c, key *value.Value, _ runtime.IndexMode) (*value.Value, error) {
	return indexJQ(c, key)
}

func (d *testDialect) Iterate(v *value.Value, emit func(key, elem *value.Value) error) error {
	switch v.Kind() {
	case value.KindArray:
		for i, e := range v.Elems() {
			if err := emit(value.Num(float64(i)), e); err != nil {
				return err
			}
		}
		return nil
	case value.KindObject:
		for _, e := range v.Entries() {
			if err := emit(e.Key, e.Value); err != nil {
				return err
			}
		}
		return nil
	default:
		return failf("Cannot iterate over %s (%s)", kindName(v), dump(v))
	}
}

func (d *testDialect) Recurse(v *value.Value, _ bool, emit func(*value.Value, value.Path) error) error {
	var walk func(v *value.Value, p value.Path) error
	walk = func(v *value.Value, p value.Path) error {
		if err := emit(v, p); err != nil {
			return err
		}
		switch v.Kind() {
		case value.KindArray:
			for i, e := range v.Elems() {
				if err := walk(e, appendPath(p, value.Num(float64(i)))); err != nil {
					return err
				}
			}
		case value.KindObject:
			for _, e := range v.Entries() {
				if err := walk(e.Value, appendPath(p, e.Key)); err != nil {
					return err
				}
			}
		default:
		}
		return nil
	}
	return walk(v, value.Path{})
}

func (d *testDialect) Assign(root *value.Value, paths []value.Path, rhs func(cur *value.Value) iter.Seq2[*value.Value, error], mode runtime.AssignMode) iter.Seq2[*value.Value, error] {
	return func(yield func(*value.Value, error) bool) {
		if mode.Op() == runtime.AssignUpdate.Op() {
			out, err := updatePaths(root, paths, rhs)
			yield(out, err)
			return
		}
		for v, err := range rhs(root) {
			if err != nil {
				yield(nil, err)
				return
			}
			out := root
			for _, p := range paths {
				nv := v
				if mode.Arith() {
					cur, err := getPath(out, p)
					if err == nil {
						nv, err = mode.Apply(cur, v)
					}
					if err != nil {
						yield(nil, err)
						return
					}
				}
				if out, err = setPath(out, p, nv); err != nil {
					yield(nil, err)
					return
				}
			}
			if !yield(out, nil) {
				return
			}
		}
	}
}

// updatePaths is jq's |=. The first output replaces each path's value, and
// a path whose update is empty is deleted once every path has been seen.
func updatePaths(root *value.Value, paths []value.Path, rhs func(cur *value.Value) iter.Seq2[*value.Value, error]) (*value.Value, error) {
	out := root
	var dels []value.Path
	for _, p := range paths {
		cur, err := getPath(out, p)
		if err != nil {
			return nil, err
		}
		var first *value.Value
		for v, err := range rhs(cur) {
			if err != nil {
				return nil, err
			}
			first = v
			break
		}
		if first == nil {
			dels = append(dels, p)
			continue
		}
		if out, err = setPath(out, p, first); err != nil {
			return nil, err
		}
	}
	return delPaths(out, dels)
}

func failf(format string, args ...any) error {
	return &query.RuntimeError{Msg: fmt.Sprintf(format, args...)}
}

func kindName(v *value.Value) string {
	switch v.Kind() {
	case value.KindNull:
		return "null"
	case value.KindFalse, value.KindTrue:
		return "boolean"
	case value.KindNumber:
		return "number"
	case value.KindString:
		return "string"
	case value.KindArray:
		return "array"
	default:
		return "object"
	}
}

func appendPath(p value.Path, c *value.Value) value.Path {
	return append(slices.Clip(p), c)
}

func indexJQ(c, key *value.Value) (*value.Value, error) {
	if start, end, ok := value.IsSliceComponent(key); ok && c.Kind() != value.KindObject {
		return sliceJQ(c, start, end)
	}
	switch {
	case c.Kind() == value.KindNull && (key.Kind() == value.KindString || key.Kind() == value.KindNumber):
		return value.Null(), nil
	case c.Kind() == value.KindObject && key.Kind() == value.KindString:
		if v, ok := c.Get(key.Str()); ok {
			return v, nil
		}
		return value.Null(), nil
	case c.Kind() == value.KindArray && key.Kind() == value.KindNumber:
		if v, ok := c.Index(int(math.Floor(key.Float()))); ok {
			return v, nil
		}
		return value.Null(), nil
	}
	return nil, failf("Cannot index %s with %s (%s)", kindName(c), kindName(key), dump(key))
}

func sliceBounds(n int, start, end *value.Value) (int, int) {
	clamp := func(v *value.Value, def int, round func(float64) float64) int {
		if v.Kind() == value.KindNull {
			return def
		}
		i := int(round(v.Float()))
		if i < 0 {
			i += n
		}
		return min(max(i, 0), n)
	}
	s, e := clamp(start, 0, math.Floor), clamp(end, n, math.Ceil)
	return s, max(s, e)
}

func sliceJQ(c, start, end *value.Value) (*value.Value, error) {
	switch c.Kind() {
	case value.KindNull:
		return value.Null(), nil
	case value.KindArray:
		s, e := sliceBounds(c.Len(), start, end)
		return value.Arr(slices.Clone(c.Elems()[s:e])...), nil
	case value.KindString:
		runes := []rune(c.Str())
		s, e := sliceBounds(len(runes), start, end)
		return value.Str(string(runes[s:e])), nil
	default:
		return nil, failf("Cannot index %s with object", kindName(c))
	}
}

func getPath(v *value.Value, p value.Path) (*value.Value, error) {
	for _, c := range p {
		if v.Kind() == value.KindNull {
			return value.Null(), nil
		}
		var err error
		if v, err = indexJQ(v, c); err != nil {
			return nil, err
		}
	}
	return v, nil
}

func setPath(v *value.Value, p value.Path, nv *value.Value) (*value.Value, error) {
	if len(p) == 0 {
		return nv, nil
	}
	c := p[0]
	if start, end, ok := value.IsSliceComponent(c); ok {
		if v.Kind() == value.KindNull {
			v = value.Arr()
		}
		if v.Kind() != value.KindArray {
			return nil, failf("Cannot update field at object index of %s", kindName(v))
		}
		s, e := sliceBounds(v.Len(), start, end)
		cur := value.Arr(slices.Clone(v.Elems()[s:e])...)
		repl, err := setPath(cur, p[1:], nv)
		if err != nil {
			return nil, err
		}
		if repl.Kind() != value.KindArray {
			return nil, failf("A slice of an array can only be assigned another array")
		}
		elems := slices.Concat(v.Elems()[:s], repl.Elems(), v.Elems()[e:])
		return value.Arr(elems...), nil
	}
	switch {
	case c.Kind() == value.KindString && (v.Kind() == value.KindObject || v.Kind() == value.KindNull):
		if v.Kind() == value.KindNull {
			v = value.Obj()
		}
		cur, _ := v.Get(c.Str())
		if cur == nil {
			cur = value.Null()
		}
		child, err := setPath(cur, p[1:], nv)
		if err != nil {
			return nil, err
		}
		return v.Set(c.Str(), child), nil
	case c.Kind() == value.KindNumber && (v.Kind() == value.KindArray || v.Kind() == value.KindNull):
		if v.Kind() == value.KindNull {
			v = value.Arr()
		}
		i := int(math.Floor(c.Float()))
		if i < 0 && i+v.Len() < 0 {
			return nil, failf("Out of bounds negative array index")
		}
		cur, ok := v.Index(i)
		if !ok {
			cur = value.Null()
		}
		child, err := setPath(cur, p[1:], nv)
		if err != nil {
			return nil, err
		}
		return v.SetIndex(i, child), nil
	}
	return nil, failf("Cannot index %s with %s (%s)", kindName(v), kindName(c), dump(c))
}

func delPaths(v *value.Value, paths []value.Path) (*value.Value, error) {
	sorted := slices.Clone(paths)
	slices.SortFunc(sorted, func(a, b value.Path) int {
		return value.Compare(value.Arr(b...), value.Arr(a...))
	})
	for _, p := range sorted {
		var err error
		if v, err = delPath(v, p); err != nil {
			return nil, err
		}
	}
	return v, nil
}

func delPath(v *value.Value, p value.Path) (*value.Value, error) {
	if len(p) == 0 {
		return value.Null(), nil
	}
	if v.Kind() == value.KindNull {
		return v, nil
	}
	c := p[0]
	if len(p) > 1 {
		child, err := indexJQ(v, c)
		if err != nil {
			return nil, err
		}
		if child, err = delPath(child, p[1:]); err != nil {
			return nil, err
		}
		return setPath(v, value.Path{c}, child)
	}
	if start, end, ok := value.IsSliceComponent(c); ok && v.Kind() == value.KindArray {
		s, e := sliceBounds(v.Len(), start, end)
		return value.Arr(slices.Concat(v.Elems()[:s], v.Elems()[e:])...), nil
	}
	switch {
	case v.Kind() == value.KindObject && c.Kind() == value.KindString:
		return v.Delete(c.Str()), nil
	case v.Kind() == value.KindArray && c.Kind() == value.KindNumber:
		return v.DeleteIndex(int(math.Floor(c.Float()))), nil
	}
	return nil, failf("Cannot delete field at %s index of %s", kindName(c), kindName(v))
}

func testRegistry() *runtime.Registry {
	r := &runtime.Registry{}
	r.Register("empty", 0, func(*runtime.Ctx, *value.Value, []runtime.Thunk, runtime.Emit) error { return nil })
	r.Register("error", 0, func(_ *runtime.Ctx, in *value.Value, _ []runtime.Thunk, _ runtime.Emit) error {
		return raise(in)
	})
	r.Register("error", 1, func(_ *runtime.Ctx, in *value.Value, args []runtime.Thunk, _ runtime.Emit) error {
		return args[0].Each(in, func(v *value.Value, _ value.Path) error { return raise(v) })
	})
	r.Register("range", 1, func(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		return args[0].Each(in, func(n *value.Value, _ value.Path) error {
			for i := 0; float64(i) < n.Float(); i++ {
				if err := emit(value.Num(float64(i)), nil); err != nil {
					return err
				}
			}
			return nil
		})
	})
	r.Register("limit", 2, func(_ *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		return args[0].Each(in, func(n *value.Value, _ value.Path) error {
			if n.Float() < 0 {
				return failf("limit doesn't support negative count")
			}
			return firstN(in, args[1], int(n.Float()), emit)
		})
	})
	r.Register("first", 1, func(_ *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		return firstN(in, args[0], 1, emit)
	})
	r.Register("getpath", 1, func(_ *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		return args[0].Each(in, func(p *value.Value, _ value.Path) error {
			v, err := getPath(in, p.Elems())
			if err != nil {
				return err
			}
			return emit(v, nil)
		})
	})
	r.Register("setpath", 2, func(_ *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		return args[0].Each(in, func(p *value.Value, _ value.Path) error {
			return args[1].Each(in, func(nv *value.Value, _ value.Path) error {
				v, err := setPath(in, p.Elems(), nv)
				if err != nil {
					return err
				}
				return emit(v, nil)
			})
		})
	})
	r.Register("delpaths", 1, func(_ *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		return args[0].Each(in, func(ps *value.Value, _ value.Path) error {
			paths := make([]value.Path, 0, ps.Len())
			for _, p := range ps.Elems() {
				paths = append(paths, p.Elems())
			}
			v, err := delPaths(in, paths)
			if err != nil {
				return err
			}
			return emit(v, nil)
		})
	})
	r.Register("env", 0, func(ctx *runtime.Ctx, _ *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		if err := ctx.Caps.RequireEnv(); err != nil {
			return err
		}
		return emit(envObject(ctx.Env()), nil)
	})
	registerArith(r)
	return r
}

// firstN emits the first n outputs of f and then stops f. Only the stop it
// raised itself is swallowed, so an outer limit's stop still propagates.
func firstN(in *value.Value, f runtime.Thunk, n int, emit runtime.Emit) error {
	if n <= 0 {
		return nil
	}
	count, stopped := 0, false
	err := f.Each(in, func(v *value.Value, p value.Path) error {
		if err := emit(v, p); err != nil {
			return err
		}
		count++
		if count == n {
			stopped = true
			return &StopError{}
		}
		return nil
	})
	var stop *StopError
	if stopped && errors.As(err, &stop) {
		return nil
	}
	return err
}

func raise(v *value.Value) error {
	if v.Kind() == value.KindString {
		return &query.RuntimeError{Msg: v.Str(), Value: v}
	}
	return &query.RuntimeError{Msg: dump(v) + " (not a string)", Value: v}
}

func envObject(m map[string]string) *value.Value {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	pairs := make([]value.Pair, len(keys))
	for i, k := range keys {
		pairs[i] = value.Pair{Key: k, Value: value.Str(m[k])}
	}
	return value.Obj(pairs...)
}

func registerArith(r *runtime.Registry) {
	num := func(op func(a, b decimal.Decimal) (decimal.Decimal, error)) runtime.Native {
		return binaryNative(func(ctx *runtime.Ctx, a, b *value.Value) (*value.Value, error) {
			if a.Kind() != value.KindNumber || b.Kind() != value.KindNumber {
				return nil, failf("%s (%s) and %s (%s) cannot be combined", kindName(a), dump(a), kindName(b), dump(b))
			}
			x, err := ctx.Number(a)
			if err != nil {
				return nil, err
			}
			y, err := ctx.Number(b)
			if err != nil {
				return nil, err
			}
			d, err := op(x, y)
			if err != nil {
				return nil, failf("%s", err.Error())
			}
			return value.NumDec(d), nil
		})
	}
	plusNum := num(decimal.Decimal.Add)
	r.Register("_plus", 2, binaryNative(func(ctx *runtime.Ctx, a, b *value.Value) (*value.Value, error) {
		switch {
		case a.Kind() == value.KindNull:
			return b, nil
		case b.Kind() == value.KindNull:
			return a, nil
		case a.Kind() == value.KindString && b.Kind() == value.KindString:
			return value.Str(a.Str() + b.Str()), nil
		case a.Kind() == value.KindArray && b.Kind() == value.KindArray:
			return value.Arr(slices.Concat(a.Elems(), b.Elems())...), nil
		}
		var out *value.Value
		err := plusNum(ctx, value.Null(), []runtime.Thunk{constThunk{a}, constThunk{b}}, func(v *value.Value, _ value.Path) error {
			out = v
			return nil
		})
		return out, err
	}))
	r.Register("_minus", 2, num(decimal.Decimal.Sub))
	r.Register("_multiply", 2, num(decimal.Decimal.Mul))
	r.Register("_divide", 2, num(func(a, b decimal.Decimal) (decimal.Decimal, error) { return a.Quo(b, decimal.Context128) }))
	r.Register("_mod", 2, num(decimal.Decimal.Rem))
	r.Register("_negate", 0, func(ctx *runtime.Ctx, in *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		d, err := ctx.Number(in)
		if err != nil {
			return err
		}
		return emit(value.NumDec(d.Neg()), nil)
	})
}

// binaryNative runs op over the cartesian product of two arguments, the
// second one outermost as jq does.
func binaryNative(op func(ctx *runtime.Ctx, a, b *value.Value) (*value.Value, error)) runtime.Native {
	return func(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		return args[1].Each(in, func(b *value.Value, _ value.Path) error {
			return args[0].Each(in, func(a *value.Value, _ value.Path) error {
				v, err := op(ctx, a, b)
				if err != nil {
					return err
				}
				return emit(v, nil)
			})
		})
	}
}

// mustJSON parses JSON text into a value, keeping key order, repeated
// keys, and each number's literal text.
func mustJSON(t *testing.T, s string) *value.Value {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	v, err := readJSON(dec)
	if err != nil {
		t.Fatalf("parsing %q: %v", s, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		t.Fatalf("parsing %q: trailing data", s)
	}
	return v
}

func readJSON(dec *json.Decoder) (*value.Value, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch tok := tok.(type) {
	case nil:
		return value.Null(), nil
	case bool:
		return value.Bool(tok), nil
	case string:
		return value.Str(tok), nil
	case json.Number:
		if d, err := decimal.Parse(tok.String()); err == nil {
			return value.NumLit(d, tok.String()), nil
		}
		return value.NumText(tok.String()), nil
	case json.Delim:
		if tok == '[' {
			var elems []*value.Value
			for dec.More() {
				e, err := readJSON(dec)
				if err != nil {
					return nil, err
				}
				elems = append(elems, e)
			}
			_, err := dec.Token()
			return value.Arr(elems...), err
		}
		var entries []value.Entry
		for dec.More() {
			k, err := dec.Token()
			if err != nil {
				return nil, err
			}
			v, err := readJSON(dec)
			if err != nil {
				return nil, err
			}
			entries = append(entries, value.Entry{Key: value.Str(k.(string)), Value: v})
		}
		_, err := dec.Token()
		return value.ObjEntries(entries...), err
	}
	return nil, fmt.Errorf("unexpected token %v", tok)
}

func mustParse(t *testing.T, src string) ast.Node {
	t.Helper()
	prog, err := parser.Parse(src, parser.JQ())
	if err != nil {
		t.Fatalf("parsing %q: %v", src, err)
	}
	return prog
}

func newCtx() *runtime.Ctx {
	ctx := runtime.NewCtx(newTestDialect(), runtime.NumbersExact)
	ctx.Env = func() map[string]string { return map[string]string{} }
	return ctx
}

func ctxWithDepth(n int) *runtime.Ctx {
	ctx := newCtx()
	ctx.DepthLimit = n
	return ctx
}

func ctxCancelled() *runtime.Ctx {
	ctx := newCtx()
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	ctx.Deadline = canceled
	return ctx
}

func ctxWithEnv(env map[string]string) *runtime.Ctx {
	ctx := newCtx()
	ctx.Env = func() map[string]string { return env }
	return ctx
}

func ctxWithCaps(caps runtime.Capabilities) *runtime.Ctx {
	ctx := newCtx()
	ctx.Caps = caps
	ctx.Env = func() map[string]string { panic("the environment was read") }
	return ctx
}

// mustEval runs src against roots and returns every output.
func mustEval(t *testing.T, ctx *runtime.Ctx, src string, roots []string) ([]*value.Value, error) {
	t.Helper()
	prog := mustParse(t, src)
	vals := make([]*value.Value, len(roots))
	for i, r := range roots {
		vals[i] = mustJSON(t, r)
	}
	var outs []*value.Value
	err := Run(ctx, prog, NewEnv(ctx.Dialect.Builtins()), vals, func(v *value.Value, _ value.Path) error {
		outs = append(outs, v)
		return nil
	})
	return outs, err
}

func wantEqual(t *testing.T, src string, got []*value.Value, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %d outputs %s, want %d %v", src, len(got), dumpAll(got), len(want), want)
	}
	for i, w := range want {
		if !value.Equal(got[i], mustJSON(t, w)) {
			t.Fatalf("%s: output %d is %s, want %s (all: %s)", src, i, dump(got[i]), w, dumpAll(got))
		}
	}
}

func dumpAll(vs []*value.Value) string {
	parts := make([]string, len(vs))
	for i, v := range vs {
		parts[i] = dump(v)
	}
	return "[" + strings.Join(parts, " ") + "]"
}

func wantOutputsWithCtx(t *testing.T, ctx *runtime.Ctx, src, input string, want ...string) {
	t.Helper()
	got, err := mustEval(t, ctx, src, []string{input})
	if err != nil {
		t.Fatalf("%s: %v", src, err)
	}
	wantEqual(t, src, got, want)
}

func wantOutputs(t *testing.T, src, input string, want ...string) {
	t.Helper()
	wantOutputsWithCtx(t, newCtx(), src, input, want...)
}

func wantOutputsRoots(t *testing.T, src string, roots []string, want ...string) {
	t.Helper()
	got, err := mustEval(t, newCtx(), src, roots)
	if err != nil {
		t.Fatalf("%s: %v", src, err)
	}
	wantEqual(t, src, got, want)
}

func wantErrContains(t *testing.T, src string, err error, sub string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: error = nil, want one containing %q", src, sub)
	}
	if !strings.Contains(err.Error(), sub) {
		t.Fatalf("%s: error = %q, want it to contain %q", src, err.Error(), sub)
	}
}

func wantErrorWithCtx(t *testing.T, ctx *runtime.Ctx, src, input, sub string) {
	t.Helper()
	_, err := mustEval(t, ctx, src, []string{input})
	wantErrContains(t, src, err, sub)
}

func wantError(t *testing.T, src, input, sub string) {
	t.Helper()
	wantErrorWithCtx(t, newCtx(), src, input, sub)
}

// wantPaths runs src through RunPaths and compares the paths it emits.
func wantPaths(t *testing.T, src, input string, want ...string) {
	t.Helper()
	ctx := newCtx()
	var got []*value.Value
	err := RunPaths(ctx, mustParse(t, src), NewEnv(ctx.Dialect.Builtins()), []*value.Value{mustJSON(t, input)}, func(_ *value.Value, p value.Path) error {
		got = append(got, value.Arr(p...))
		return nil
	})
	if err != nil {
		t.Fatalf("%s: %v", src, err)
	}
	wantEqual(t, src, got, want)
}

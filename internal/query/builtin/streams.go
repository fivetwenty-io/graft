package builtin

import (
	"errors"

	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// stopSignal is the error a native returns from its argument's emit to
// stop the argument early. Each use makes its own, so a stop raised by an
// outer native passes through an inner one untouched.
type stopSignal struct{ _ byte }

func (*stopSignal) Error() string { return "stop" }

// take runs f on in and passes its outputs, with their paths, to emit
// while keep says to. It stops f at the first output keep refuses.
func take(in *value.Value, f runtime.Thunk, keep func(n int) (pass, more bool), emit runtime.Emit) error {
	stop := &stopSignal{}
	n := 0
	err := f.Each(in, func(v *value.Value, p value.Path) error {
		n++
		pass, more := keep(n)
		if pass {
			if err := emit(v, p); err != nil {
				return err
			}
		}
		if !more {
			return stop
		}
		return nil
	})
	if errors.Is(err, stop) {
		return nil
	}
	return err
}

func registerStreams(r *runtime.Registry) {
	counted := func(name string, run func(count float64, in *value.Value, f runtime.Thunk, emit runtime.Emit) error) {
		r.Register(name, 2, func(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
			return args[0].Each(in, func(n *value.Value, _ value.Path) error {
				if n.Kind() != value.KindNumber {
					return fail("Invalid %s count %s", name, describe(n))
				}
				if n.Float() < 0 {
					return fail("%s doesn't support negative count", name)
				}
				return run(n.Float(), in, args[1], emit)
			})
		})
	}
	counted("limit", func(count float64, in *value.Value, f runtime.Thunk, emit runtime.Emit) error {
		if count == 0 {
			return nil
		}
		return take(in, f, func(n int) (bool, bool) { return true, float64(n) < count }, emit)
	})
	counted("skip", func(count float64, in *value.Value, f runtime.Thunk, emit runtime.Emit) error {
		return take(in, f, func(n int) (bool, bool) { return float64(n) > count, true }, emit)
	})
	// first stops its argument after the emit returns, so it is not single.
	r.Register("first", 1, func(_ *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		return take(in, args[0], func(int) (bool, bool) { return true, false }, emit)
	})
	r.RegisterSingle("isempty", 1, func(_ *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		empty := true
		err := take(in, args[0], func(int) (bool, bool) {
			empty = false
			return false, false
		}, emit)
		if err != nil {
			return err
		}
		return emit(value.Bool(empty), nil)
	})
	r.Register("range", 2, func(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		return args[0].Each(in, func(from *value.Value, _ value.Path) error {
			return args[1].Each(in, func(upto *value.Value, _ value.Path) error {
				return count(ctx, from, upto, value.Num(1), emit)
			})
		})
	})
	r.Register("range", 3, func(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		return args[0].Each(in, func(from *value.Value, _ value.Path) error {
			return args[1].Each(in, func(upto *value.Value, _ value.Path) error {
				return args[2].Each(in, func(by *value.Value, _ value.Path) error {
					return count(ctx, from, upto, by, emit)
				})
			})
		})
	})
	r.Register("tostream", 0, func(_ *runtime.Ctx, in *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		return tostream(in, value.Path{}, emit)
	})
	r.Register("fromstream", 1, fromstream)
}

// count emits from, from+by, and so on while short of upto, counting down
// for a negative by and emitting nothing for a zero one. The sums are
// exact.
func count(ctx *runtime.Ctx, from, upto, by *value.Value, emit runtime.Emit) error {
	for _, v := range []*value.Value{from, upto, by} {
		if v.Kind() != value.KindNumber {
			return fail("Range bounds must be numeric")
		}
	}
	x, err := ctx.Number(from)
	if err != nil {
		return err
	}
	end, err := ctx.Number(upto)
	if err != nil {
		return err
	}
	step, err := ctx.Number(by)
	if err != nil {
		return err
	}
	if step.IsZero() || step.IsNaN() {
		return nil
	}
	dir := step.Sign()
	for x.Cmp(end)*dir < 0 {
		if err := ctx.Tick(); err != nil {
			return err
		}
		v := value.NumDec(x)
		if from.Literal() != "" && x.Equal(from.Dec()) {
			v = from
		}
		if err := emit(v, nil); err != nil {
			return err
		}
		next, err := x.Add(step)
		if err != nil {
			return fail("%s", err.Error())
		}
		if next.Equal(x) {
			return nil
		}
		x = next
	}
	return nil
}

// tostream emits v as stream events: a path and a leaf for every scalar
// and empty container, and after a non-empty container's last child, the
// path to that child alone, which closes the container.
func tostream(v *value.Value, p value.Path, emit runtime.Emit) error {
	var last *value.Value
	visit := func(k, c *value.Value) error {
		last = k
		return tostream(c, extendPath(p, k), emit)
	}
	switch {
	case v.Kind() == value.KindArray && v.Len() > 0:
		for i, e := range v.Elems() {
			if err := visit(value.Num(float64(i)), e); err != nil {
				return err
			}
		}
	case v.Kind() == value.KindObject && v.Len() > 0:
		for _, e := range v.Entries() {
			if err := visit(e.Key, e.Value); err != nil {
				return err
			}
		}
	default:
		return emit(value.Arr(value.Arr(p...), v), nil)
	}
	return emit(value.Arr(value.Arr(extendPath(p, last)...)), nil)
}

// extendPath returns p with c appended, never sharing p's spare capacity.
func extendPath(p value.Path, c *value.Value) value.Path {
	out := make(value.Path, len(p)+1)
	copy(out, p)
	out[len(p)] = c
	return out
}

// fromstream rebuilds values from the stream events f emits. An event
// with a value sets it at its path, a top-level scalar is complete at
// once, and a closing event one level deep completes the value.
func fromstream(_ *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
	var ed *Editor
	return args[0].Each(in, func(ev *value.Value, _ value.Path) error {
		pv, err := Index(ev, value.Num(0))
		if err != nil {
			return err
		}
		if pv.Kind() != value.KindArray {
			return fail("Invalid path component")
		}
		path := pv.Elems()
		if ev.Len() > 1 {
			leaf, err := Index(ev, value.Num(1))
			if err != nil {
				return err
			}
			if len(path) == 0 {
				ed = nil
				return emit(leaf, nil)
			}
			if ed == nil {
				ed = NewEditor(value.Null())
			}
			return ed.Set(path, leaf)
		}
		if len(path) == 1 && ed != nil {
			out := ed.Value()
			ed = nil
			return emit(out, nil)
		}
		return nil
	})
}

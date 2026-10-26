package value

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"unsafe"
)

// bigObj returns an object with n keys k0 to k(n-1), each holding its
// position.
func bigObj(n int) *Value {
	v := Obj()
	for i := range n {
		v = v.Set(fmt.Sprintf("k%d", i), Num(float64(i)))
	}
	return v
}

func TestGrownObjectsNeverSeeLaterGrowth(t *testing.T) {
	a := bigObj(20)
	b := a.Set("x", Num(1))
	c := b.Set("y", Num(2))
	d := b.Set("z", Num(3)) // b no longer ends its storage, so this copies
	e := c.Set("w", Num(4))
	wantEqual(t, a.Len(), 20)
	wantEqual(t, b.Len(), 21)
	wantEqual(t, c.Keys()[20:], []string{"x", "y"})
	wantEqual(t, d.Keys()[20:], []string{"x", "z"})
	wantEqual(t, e.Keys()[20:], []string{"x", "y", "w"})
	for _, k := range []string{"x", "y", "z", "w"} {
		if _, ok := a.Get(k); ok {
			t.Errorf("a holds %s", k)
		}
	}
	for v, absent := range map[*Value][]string{b: {"y", "z", "w"}, c: {"z", "w"}, d: {"y", "w"}, e: {"z"}} {
		for _, k := range absent {
			if _, ok := v.Get(k); ok {
				t.Errorf("%v holds %s", v.Keys()[20:], k)
			}
		}
	}
	if got, _ := d.Get("z"); got.Float() != 3 {
		t.Errorf("d.z is %v", got)
	}
	if got, _ := e.Get("k7"); got.Float() != 7 {
		t.Errorf("e.k7 is %v", got)
	}
	// Replacing a key copies, and every older value keeps its own.
	f := e.Set("k7", Num(70))
	if got, _ := e.Get("k7"); got.Float() != 7 {
		t.Errorf("e.k7 became %v", got)
	}
	if got, _ := f.Set("v", Num(5)).Get("k7"); got.Float() != 70 {
		t.Errorf("f.k7 is %v", got)
	}
}

func TestGrownArraysNeverSeeLaterGrowth(t *testing.T) {
	a := Arr()
	for i := range 20 {
		a = a.SetIndex(i, Num(float64(i)))
	}
	b := a.Append(Num(100))
	c := b.SetIndex(21, Num(101))
	d := b.Append(Num(200), Num(201)) // b no longer ends its storage
	e := c.SetIndex(24, Num(102))     // pads with nulls
	wantEqual(t, a.Len(), 20)
	wantEqual(t, b.Len(), 21)
	wantEqual(t, floats(c.Elems()[20:]), []float64{100, 101})
	wantEqual(t, floats(d.Elems()[20:]), []float64{100, 200, 201})
	wantEqual(t, e.Len(), 25)
	if got, _ := e.Index(22); got.Kind() != KindNull {
		t.Errorf("e[22] is %v", got)
	}
	if _, ok := b.Index(21); ok {
		t.Error("b grew")
	}
}

func floats(vs []*Value) []float64 {
	out := make([]float64, len(vs))
	for i, v := range vs {
		out[i] = v.Float()
	}
	return out
}

// A caller who appends to a view must never write into storage that a
// value grown later holds.
func TestViewsCannotReachSpareStorage(t *testing.T) {
	o := bigObj(20).Set("x", Num(1))
	grown := o.Set("y", Num(2))
	_ = append(o.Entries(), Entry{Str("evil"), Num(0)})
	wantEqual(t, grown.Keys()[20:], []string{"x", "y"})

	a := Arr(Num(0)).Append(Num(1), Num(2), Num(3), Num(4), Num(5), Num(6), Num(7), Num(8))
	longer := a.Append(Num(9))
	_ = append(a.Elems(), Str("evil"))
	if got, _ := longer.Index(9); got.Float() != 9 {
		t.Errorf("longer[9] is %v", got)
	}
}

func TestSetEntryReplacesTheKeyNode(t *testing.T) {
	k := Str("a").WithMeta(&Meta{LineComment: "new"})
	v := Obj(Pair{"a", Num(1)}, Pair{"b", Num(2)}).WithMeta(&Meta{HeadComment: "root"})
	out := v.SetEntry(k, Num(9))
	wantEqual(t, out.Keys(), []string{"a", "b"})
	if out.Entries()[0].Key != k || out.Meta() != v.Meta() {
		t.Error("SetEntry must take the new key node and keep the object's Meta")
	}
	out = out.SetEntry(Str("c"), Num(3))
	wantEqual(t, out.Keys(), []string{"a", "b", "c"})
	if got, _ := v.Get("a"); got.Float() != 1 {
		t.Errorf("v.a became %v", got)
	}
}

// Values grown from one shared value on many goroutines each see only
// their own growth, which go test -race checks for data races too.
func TestGrowingSharedValuesConcurrently(t *testing.T) {
	obj := bigObj(20).Set("base", Null())
	arr := Arr(Num(0)).Append(Num(1), Num(2), Num(3), Num(4), Num(5), Num(6), Num(7), Num(8))
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			o, a := obj, arr
			for i := range 200 {
				o = o.Set(fmt.Sprintf("g%d-%d", g, i), Num(float64(i)))
				a = a.Append(Num(float64(g)))
				if _, ok := o.Get("k3"); !ok {
					t.Errorf("goroutine %d lost k3", g)
				}
			}
			if o.Len() != 221 || a.Len() != 209 {
				t.Errorf("goroutine %d grew to %d and %d", g, o.Len(), a.Len())
			}
			for _, k := range o.Keys()[21:] {
				if !strings.HasPrefix(k, fmt.Sprintf("g%d-", g)) {
					t.Errorf("goroutine %d sees %s", g, k)
				}
			}
			for _, e := range a.Elems()[9:] {
				if e.Float() != float64(g) {
					t.Errorf("goroutine %d sees element %v", g, e)
				}
			}
		})
	}
	wg.Wait()
	wantEqual(t, obj.Len(), 21)
	wantEqual(t, arr.Len(), 9)
}

// Padding an array past its end writes the padding straight into the new
// array, rather than building it apart and copying it in.
func TestSetIndexPastTheEndAllocatesOneArray(t *testing.T) {
	const n = 1 << 20
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	v := Arr().SetIndex(n, Num(1))
	runtime.ReadMemStats(&after)
	wantEqual(t, v.Len(), n+1)
	if got, _ := v.Index(n - 1); got.Kind() != KindNull {
		t.Errorf("v[%d] is %v", n-1, got)
	}
	if got, _ := v.Index(n); got.Float() != 1 {
		t.Errorf("v[%d] is %v", n, got)
	}
	need := float64(n * int(unsafe.Sizeof(v)))
	if got := float64(after.TotalAlloc - before.TotalAlloc); got > 1.6*need {
		t.Errorf("padding to %d allocated %.1f times the array", n, got/need)
	}
}

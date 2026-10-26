package value

import (
	"fmt"
	"testing"
)

func TestOwnerNeverChangesAValueItWasGiven(t *testing.T) {
	var o Owner
	small := Obj(Pair{"a", Num(1)})
	big := bigObj(20)
	arr := Arr(Num(0), Num(1))
	held := o.Set(small, "a", Num(2))
	wantEqual(t, held != small, true)
	held = o.Set(big, "k3", Num(30))
	wantEqual(t, held != big, true)
	held = o.SetIndex(arr, 0, Num(5))
	wantEqual(t, held != arr, true)
	if got, _ := small.Get("a"); got.Float() != 1 {
		t.Errorf("small.a became %v", got)
	}
	if got, _ := big.Get("k3"); got.Float() != 3 {
		t.Errorf("big.k3 became %v", got)
	}
	wantEqual(t, floats(arr.Elems()), []float64{0, 1})
}

func TestOwnerChangesWhatItHoldsInPlace(t *testing.T) {
	var o Owner
	v := o.Set(bigObj(20), "k0", Num(-1))
	for i := range 100 {
		w := o.Set(v, fmt.Sprintf("n%d", i), Num(float64(i)))
		w = o.Set(w, "k1", Num(float64(i)))
		if w != v {
			t.Fatalf("step %d copied the object it holds", i)
		}
	}
	wantEqual(t, v.Len(), 120)
	for k, want := range map[string]float64{"k0": -1, "k1": 99, "k19": 19, "n0": 0, "n99": 99} {
		if got, ok := v.Get(k); !ok || got.Float() != want {
			t.Errorf("%s is %v, want %v", k, got, want)
		}
	}
	wantEqual(t, v.Keys()[118:], []string{"n98", "n99"})

	a := o.SetIndex(Arr(), 0, Num(0)) // taking a lets v go
	for i := 1; i < 100; i++ {
		if b := o.SetIndex(a, i, Num(float64(i))); b != a {
			t.Fatalf("append %d copied the array it holds", i)
		}
	}
	o.SetIndex(a, 50, Num(-50))
	wantEqual(t, a.Len(), 100)
	if got, _ := a.Index(50); got.Float() != -50 {
		t.Errorf("a[50] is %v", got)
	}
	if w := o.Set(v, "k2", Num(0)); w == v {
		t.Error("o still held v after it took another container")
	}
}

// Anything that reads a held container's storage, or grows it in shared
// storage, makes the Owner let it go, so what it read never changes.
func TestReadingStorageLetsTheContainerGo(t *testing.T) {
	reads := map[string]func(v *Value){
		"Entries":  func(v *Value) { _ = v.Entries() },
		"WithMeta": func(v *Value) { _ = v.WithMeta(&Meta{HeadComment: "x"}) },
		"Set":      func(v *Value) { _ = v.Set("grown", Null()) },
	}
	for name, read := range reads {
		var o Owner
		v := o.Set(bigObj(8), "k0", Num(0))
		read(v)
		if w := o.Set(v, "k0", Num(1)); w == v {
			t.Errorf("%s left the object held", name)
		}
		if got, _ := v.Get("k0"); got.Float() != 0 {
			t.Errorf("%s: v.k0 became %v", name, got)
		}
	}
	elemReads := map[string]func(v *Value){
		"Elems":    func(v *Value) { _ = v.Elems() },
		"WithMeta": func(v *Value) { _ = v.WithMeta(&Meta{HeadComment: "x"}) },
		"Append":   func(v *Value) { _ = v.Append(Null()) },
	}
	for name, read := range elemReads {
		var o Owner
		v := o.SetIndex(Arr(Num(0)), 0, Num(0))
		read(v)
		if w := o.SetIndex(v, 0, Num(1)); w == v {
			t.Errorf("%s left the array held", name)
		}
		if got, _ := v.Index(0); got.Float() != 0 {
			t.Errorf("%s: v[0] became %v", name, got)
		}
	}
}

// A container set into what the Owner holds is shared from then on, so
// putting the held container inside itself copies it first.
func TestSettingTheHeldContainerLetsItGo(t *testing.T) {
	var o Owner
	v := o.Set(Obj(), "a", Num(1))
	w := o.Set(v, "self", v)
	if w == v {
		t.Fatal("the held object was set inside itself")
	}
	if _, ok := v.Get("self"); ok {
		t.Error("v gained self")
	}
	if got, _ := w.Get("self"); got != v {
		t.Errorf("w.self is %v", got)
	}
	a := o.SetIndex(Arr(), 0, Num(1))
	b := o.SetIndex(a, 1, a)
	if b == a || a.Len() != 1 {
		t.Error("the held array was set inside itself")
	}
}

func TestReleaseLetsGo(t *testing.T) {
	var o Owner
	v := o.Set(Obj(), "a", Num(1))
	o.Release()
	if w := o.Set(v, "a", Num(2)); w == v {
		t.Error("Release left v held")
	}
	o.Disown(Num(1)) // not held, so nothing happens
	w := o.Set(v, "b", Num(2))
	o.Disown(w)
	if x := o.Set(w, "c", Num(3)); x == w {
		t.Error("Disown left w held")
	}
}

func TestOwnerSetIndexPanicsOutsideTheArray(t *testing.T) {
	for _, i := range []int{-1, 2} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("SetIndex(%d) did not panic", i)
				}
			}()
			var o Owner
			o.SetIndex(Arr(Num(0)), i, Null())
		}()
	}
}

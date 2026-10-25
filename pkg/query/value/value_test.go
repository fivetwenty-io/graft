package value

import (
	"math"
	"sync"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/decimal"
)

func TestObjectKeepsInsertionOrder(t *testing.T) {
	v := Obj(Pair{"b", Num(1)}, Pair{"a", Num(2)})
	wantEqual(t, v.Keys(), []string{"b", "a"})
	v = v.Set("c", Num(3)).Set("b", Num(9))
	wantEqual(t, v.Keys(), []string{"b", "a", "c"})
}

func TestCompareFollowsJQOrder(t *testing.T) {
	order := []*Value{Null(), Bool(false), Bool(true), Num(-1), Str(""), Arr(), Obj()}
	for i := 1; i < len(order); i++ {
		if Compare(order[i-1], order[i]) >= 0 {
			t.Fatalf("%v should sort before %v", order[i-1], order[i])
		}
	}
	wantEqual(t, Compare(Obj(Pair{"a", Num(1)}), Obj(Pair{"b", Num(0)})), -1) // keys first, then values
	wantEqual(t, Compare(Arr(Num(1), Num(2)), Arr(Num(1))), 1)
	wantEqual(t, Compare(Arr(Num(1), Num(2)), Arr(Num(2))), -1)
	wantEqual(t, Compare(Str("a"), Str("b")), -1)
	wantEqual(t, Compare(Obj(Pair{"a", Num(2)}, Pair{"b", Num(1)}), Obj(Pair{"b", Num(1)}, Pair{"a", Num(2)})), 0)
	wantEqual(t, Compare(Obj(Pair{"a", Num(1)}, Pair{"b", Num(9)}), Obj(Pair{"a", Num(2)}, Pair{"b", Num(0)})), -1)
}

func TestSetCopiesSpineAndKeepsMeta(t *testing.T) {
	m := &Meta{LineComment: "keep"}
	sib := Str("x").WithMeta(m)
	root := Obj(Pair{"sib", sib}, Pair{"t", Num(1)}).WithMeta(&Meta{HeadComment: "root"})
	out := root.Set("t", Num(2))
	got, _ := out.Get("sib")
	if got != sib || got.Meta() != m {
		t.Fatal("sibling must be shared, with its Meta")
	}
	if orig, _ := root.Get("t"); orig.Float() != 1 {
		t.Fatal("original mutated")
	}
	if out.Meta() == nil || out.Meta().HeadComment != "root" {
		t.Fatal("the copy must carry the container's Meta")
	}
}

func TestEntriesKeepDuplicateAndTypedKeys(t *testing.T) {
	v := ObjEntries(Entry{Num(1), Str("x")}, Entry{Str("1"), Str("y")}, Entry{Str("a"), Num(1)}, Entry{Str("a"), Num(2)})
	wantEqual(t, len(v.Entries()), 4)
	wantEqual(t, v.Keys(), []string{"1", "1", "a", "a"})
	got, _ := v.Get("a")
	wantEqual(t, got.Float(), 2.0) // last entry wins, as yq prints
	got, _ = v.Get("1")
	wantEqual(t, got.Str(), "y")
}

func TestLiteralComparisonIsExact(t *testing.T) {
	a := lit("100000000000000000000")
	b := lit("100000000000000000001")
	if Equal(a, b) {
		t.Fatal("distinct literals must compare unequal")
	}
	wantEqual(t, Compare(a, b), -1)
	if !Equal(Num(1e20), a) || Equal(Num(1e20), b) {
		t.Fatal("a computed number compares exactly against a literal")
	}
	wantEqual(t, Compare(NumDec(decimal.NaN()), Num(-1e308)), -1) // NaN sorts below every number
	wantEqual(t, Compare(NumDec(decimal.NaN()), NumDec(decimal.NaN())), -1)
	if Equal(NumDec(decimal.NaN()), NumDec(decimal.NaN())) {
		t.Fatal("nan == nan is false in jq")
	}
	if Equal(Obj(Pair{"a", nan()}), Obj(Pair{"a", nan()})) {
		t.Fatal("a NaN inside a container keeps == false")
	}
	if Equal(Arr(nan()), Arr(nan())) {
		t.Fatal("a NaN inside an array keeps == false")
	}
	if !EqualFloat(Num(1e20), b) || Equal(Num(1e20), b) {
		t.Fatal("float comparison rounds to doubles, exact comparison does not")
	}
	if !Equal(lit("-0"), Num(0)) {
		t.Fatal("-0 equals 0")
	}
	wantEqual(t, Compare(lit("-0"), Num(0)), 0)
	wantEqual(t, CanonicalLiteral("3.0e2"), "3.0E+2")
	wantEqual(t, CanonicalLiteral("1e1000"), "1E+1000")
	wantEqual(t, CanonicalLiteral("100000000000000000000"), "100000000000000000000")
	wantEqual(t, CanonicalLiteral("0.00000001"), "1E-8")
	wantEqual(t, CanonicalLiteral("not a number"), "not a number")
}

func TestFloatComparisonFollowsNaNRules(t *testing.T) {
	wantEqual(t, CompareFloat(nan(), Num(-1e308)), -1)
	wantEqual(t, CompareFloat(nan(), nan()), -1)
	wantEqual(t, CompareFloat(Num(1), nan()), 1)
	wantEqual(t, CompareFloat(Num(1), Num(2)), -1)
	if EqualFloat(nan(), nan()) || EqualFloat(Arr(nan()), Arr(nan())) {
		t.Fatal("NaN never equals under float comparison")
	}
	if !EqualFloat(Obj(Pair{"a", Num(1)}), Obj(Pair{"a", lit("1.0")})) {
		t.Fatal("containers compare through the same walk")
	}
	wantEqual(t, CompareFloat(Arr(lit("100000000000000000000")), Arr(lit("100000000000000000001"))), 0)
}

func TestBuildersCopyOnce(t *testing.T) {
	base := Arr(Num(1), Num(2), Num(3))
	b := NewArrBuilderFrom(base)
	for i := 0; i < 3; i++ {
		b.Set(i, Num(float64(i)*10))
	}
	out := b.Seal()
	wantEqual(t, out.Len(), 3)
	if e, _ := base.Index(0); e.Float() != 1 {
		t.Fatal("base mutated")
	}
	if e, _ := out.Index(2); e.Float() != 20 {
		t.Fatal("builder did not write")
	}
	allocs := testing.AllocsPerRun(10, func() {
		bb := NewArrBuilderFrom(base)
		for i := 0; i < 3; i++ {
			bb.Set(i, Num(0))
		}
		bb.Seal()
	})
	if allocs > 6 {
		t.Fatalf("builder allocates per element: %v", allocs)
	}
}

func TestArrBuilderAppendsAndDies(t *testing.T) {
	b := NewArrBuilder(2)
	b.Append(Num(1))
	b.Append(Num(2))
	b.Append(Num(3))
	out := b.Seal()
	wantEqual(t, out.Len(), 3)
	defer func() {
		if recover() == nil {
			t.Fatal("a sealed builder must refuse further use")
		}
	}()
	b.Append(Num(4))
}

func TestObjBuilder(t *testing.T) {
	base := Obj(Pair{"a", Num(1)}, Pair{"b", Num(2)}).WithMeta(&Meta{Anchor: "x"})
	b := NewObjBuilderFrom(base)
	b.Set("a", Num(10))
	b.Set("c", Num(3))
	b.Delete("b")
	b.SetEntry(Str("d"), Num(4))
	out := b.Seal()
	wantEqual(t, out.Keys(), []string{"a", "c", "d"})
	if v, _ := out.Get("a"); v.Float() != 10 {
		t.Fatal("set did not replace")
	}
	if v, _ := base.Get("a"); v.Float() != 1 || base.Len() != 2 {
		t.Fatal("base mutated")
	}
	if out.Meta() == nil || out.Meta().Anchor != "x" {
		t.Fatal("seal carries the source Meta")
	}
	fresh := NewObjBuilder(0)
	fresh.Set("k", Null())
	wantEqual(t, fresh.Seal().Keys(), []string{"k"})
}

func TestSlicePathComponent(t *testing.T) {
	c := Obj(Pair{"start", Num(1)}, Pair{"end", Num(3)})
	s, e, ok := IsSliceComponent(c)
	if !ok || s.Float() != 1 || e.Float() != 3 {
		t.Fatal("slice component not recognized")
	}
	_, _, ok = IsSliceComponent(Obj(Pair{"start", Num(1)}))
	if ok {
		t.Fatal("a slice component needs both keys, null allowed")
	}
	_, _, ok = IsSliceComponent(Obj(Pair{"start", Null()}, Pair{"end", Null()}))
	if !ok {
		t.Fatal("null bounds are allowed")
	}
	_, _, ok = IsSliceComponent(Str("start"))
	if ok {
		t.Fatal("a string is not a slice component")
	}
}

func TestNumberAccessors(t *testing.T) {
	v := Num(0.1)
	wantEqual(t, v.Dec().String(), "0.1")
	wantEqual(t, v.Literal(), "")
	wantEqual(t, v.Bounded(), true)
	l := lit("3.0e2")
	wantEqual(t, l.Literal(), "3.0e2")
	wantEqual(t, l.Float(), 300.0)
	wantEqual(t, NumDec(decimal.MustParse("1.50")).Kind(), KindNumber)
}

func TestNumTextKeepsOverBoundLiterals(t *testing.T) {
	huge := "1e1000000000"
	v := NumText(huge)
	wantEqual(t, v.Bounded(), false)
	wantEqual(t, v.Literal(), huge)
	wantEqual(t, v.Kind(), KindNumber)
	if !math.IsInf(v.Float(), 1) {
		t.Fatalf("exponent past float64 is an infinity, got %v", v.Float())
	}
	if f := NumText("-1e1000000000").Float(); !math.IsInf(f, -1) {
		t.Fatalf("got %v", f)
	}
	if f := NumText("1e-1000000000").Float(); f != 0 || math.Signbit(f) {
		t.Fatalf("got %v", f)
	}
	if f := NumText("-1e-1000000000").Float(); f != 0 || !math.Signbit(f) {
		t.Fatalf("got %v", f)
	}
	wantEqual(t, v.Dec().Err(), decimal.ErrExponentRange)
	wantEqual(t, Compare(NumText("1e1000000000"), NumText("1e1000000001")), -1)
}

func TestNumTextFloatIsNearestDoubleWhenInRange(t *testing.T) {
	// 100001 significant digits with an adjusted exponent of zero.
	digits := "1." + repeat("1", 100000)
	v := NumText(digits)
	wantEqual(t, v.Bounded(), false)
	wantEqual(t, v.Dec().Err(), decimal.ErrTooManyDigits)
	wantEqual(t, v.Float(), 1.1111111111111112)
	// A 40-digit integer is within the bounds, and still rounds to a double.
	forty := lit("1234567890123456789012345678901234567890")
	wantEqual(t, forty.Bounded(), true)
	wantEqual(t, forty.Float(), 1.2345678901234568e39)
}

func TestIndexAndNegativeIndex(t *testing.T) {
	a := Arr(Num(1), Num(2), Num(3))
	if v, ok := a.Index(-1); !ok || v.Float() != 3 {
		t.Fatal("negative index counts from the end")
	}
	if _, ok := a.Index(3); ok {
		t.Fatal("past the end")
	}
	if _, ok := a.Index(-4); ok {
		t.Fatal("before the start")
	}
}

func TestArrayUpdaters(t *testing.T) {
	m := &Meta{FootComment: "f"}
	a := Arr(Num(1), Num(2), Num(3)).WithMeta(m)
	s := a.SetIndex(1, Num(9))
	if v, _ := s.Index(1); v.Float() != 9 || s.Meta() != m {
		t.Fatal("SetIndex")
	}
	if v, _ := a.Index(1); v.Float() != 2 {
		t.Fatal("original mutated")
	}
	p := a.SetIndex(4, Num(5))
	wantEqual(t, p.Len(), 5)
	if v, _ := p.Index(3); v.Kind() != KindNull {
		t.Fatal("padding is null")
	}
	d := a.DeleteIndex(0)
	wantEqual(t, d.Len(), 2)
	if v, _ := d.Index(0); v.Float() != 2 || d.Meta() != m {
		t.Fatal("DeleteIndex")
	}
	wantEqual(t, a.DeleteIndex(10).Len(), 3)
	wantEqual(t, a.SetIndex(-1, Num(0)).Len(), 3)
}

func TestDeleteRemovesEveryEntryForTheKey(t *testing.T) {
	v := ObjEntries(Entry{Str("a"), Num(1)}, Entry{Str("b"), Num(2)}, Entry{Str("a"), Num(3)})
	wantEqual(t, v.Delete("a").Keys(), []string{"b"})
	wantEqual(t, v.Delete("zz").Keys(), []string{"a", "b", "a"})
	wantEqual(t, v.Len(), 3)
}

func TestKeyFormatting(t *testing.T) {
	v := ObjEntries(Entry{Bool(true), Num(1)}, Entry{Null(), Num(2)}, Entry{lit("1.50"), Num(3)}, Entry{Num(2.5), Num(4)})
	wantEqual(t, v.Keys(), []string{"true", "null", "1.50", "2.5"})
}

func TestLargeObjectLookupIsSafeAcrossGoroutines(t *testing.T) {
	pairs := make([]Pair, 100)
	for i := range pairs {
		pairs[i] = Pair{Key: string(rune('A'+i/26)) + string(rune('a'+i%26)), Value: Num(float64(i))}
	}
	v := Obj(pairs...)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i, p := range pairs {
				got, ok := v.Get(p.Key)
				if !ok || got.Float() != float64(i) {
					t.Errorf("lookup of %s", p.Key)
				}
			}
		}()
	}
	wg.Wait()
	if _, ok := v.Get("nope"); ok {
		t.Fatal("absent key found")
	}
}

func TestScalarAccessors(t *testing.T) {
	wantEqual(t, Str("hi").Str(), "hi")
	wantEqual(t, Bool(true).Kind(), KindTrue)
	wantEqual(t, Bool(false).Kind(), KindFalse)
	wantEqual(t, Null().Kind(), KindNull)
	wantEqual(t, Arr(Num(1)).Kind(), KindArray)
	wantEqual(t, Obj().Kind(), KindObject)
	wantEqual(t, Str("hi").Len(), 2)
	wantEqual(t, len(Arr(Num(1), Num(2)).Elems()), 2)
}

func TestStyleFlagsCombine(t *testing.T) {
	f := Tagged | DoubleQuoted
	if f&Tagged == 0 || f&DoubleQuoted == 0 || f&Flow != 0 {
		t.Fatal("flags are independent bits")
	}
}

func TestNumConvertsDoublesExactly(t *testing.T) {
	wantEqual(t, Num(1000).Dec().String(), "1000")
	wantEqual(t, Num(0.1).Dec().String(), "0.1")
	wantEqual(t, Num(-3).Dec().String(), "-3")
	wantEqual(t, Num(math.Copysign(0, -1)).Dec().IsNegative(), true)
	wantEqual(t, Num(0).Dec().IsNegative(), false)
	wantEqual(t, Num(1e20).Dec().String(), "100000000000000000000")
	wantEqual(t, Num(math.NaN()).Dec().IsNaN(), true)
	wantEqual(t, Num(9007199254740993).Float(), 9007199254740992.0)
}

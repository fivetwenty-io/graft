package value_test

import (
	"fmt"
	"math"

	"github.com/fivetwenty-io/graft/pkg/decimal"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

func ExampleValue() {
	v := value.Obj(
		value.Pair{Key: "name", Value: value.Str("web")},
		value.Pair{Key: "ports", Value: value.Arr(value.Num(80), value.Num(443))},
	)
	ports, _ := v.Get("ports")
	fmt.Println(v.Keys(), ports.Len())
	// Output: [name ports] 2
}

func ExampleKind() {
	for _, v := range []*value.Value{value.Null(), value.Bool(false), value.Num(1), value.Str("s"), value.Arr()} {
		fmt.Print(v.Kind(), " ")
	}
	fmt.Println(value.KindNull < value.KindObject)
	// Output: 0 1 3 4 5 true
}

func ExampleNull() {
	fmt.Println(value.Null().Kind() == value.KindNull)
	// Output: true
}

func ExampleBool() {
	fmt.Println(value.Bool(true).Kind() == value.KindTrue, value.Bool(false).Kind() == value.KindFalse)
	// Output: true true
}

func ExampleNum() {
	v := value.Num(0.1)
	fmt.Println(v.Dec(), v.Float())
	// Output: 0.1 0.1
}

func ExampleNumDec() {
	sum, _ := decimal.MustParse("0.1").Add(decimal.MustParse("0.2"))
	fmt.Println(value.NumDec(sum).Dec())
	// Output: 0.3
}

func ExampleNumLit() {
	v := value.NumLit(decimal.MustParse("1.50"), "1.50")
	fmt.Printf("%q %v\n", v.Literal(), v.Dec())
	// Output: "1.50" 1.5
}

func ExampleNumText() {
	v := value.NumText("1e9999999999")
	fmt.Println(v.Bounded(), v.Literal(), v.Dec().Err())
	// Output: false 1e9999999999 number exponent exceeds 999999999
}

func ExampleStr() {
	v := value.Str("héllo")
	fmt.Println(v.Str(), v.Len())
	// Output: héllo 6
}

func ExampleArr() {
	v := value.Arr(value.Num(1), value.Str("two"))
	fmt.Println(v.Len(), v.Elems()[1].Str())
	// Output: 2 two
}

func ExampleObj() {
	v := value.Obj(value.Pair{Key: "b", Value: value.Num(1)}, value.Pair{Key: "a", Value: value.Num(2)})
	fmt.Println(v.Keys())
	// Output: [b a]
}

func ExamplePair() {
	p := value.Pair{Key: "port", Value: value.Num(8080)}
	fmt.Println(value.Obj(p).Keys())
	// Output: [port]
}

func ExampleObjEntries() {
	v := value.ObjEntries(
		value.Entry{Key: value.Num(1), Value: value.Str("one")},
		value.Entry{Key: value.Bool(true), Value: value.Str("yes")},
	)
	fmt.Println(v.Keys())
	// Output: [1 true]
}

func ExampleEntry() {
	v := value.ObjEntries(value.Entry{Key: value.Str("a"), Value: value.Num(1)})
	for _, e := range v.Entries() {
		fmt.Println(e.Key.Str(), e.Value.Dec())
	}
	// Output: a 1
}

func ExampleValue_Kind() {
	fmt.Println(value.Str("x").Kind() == value.KindString)
	// Output: true
}

func ExampleValue_Dec() {
	fmt.Println(value.Num(2.5).Dec(), value.Str("x").Dec())
	// Output: 2.5 0
}

func ExampleValue_Bounded() {
	fmt.Println(value.Num(1).Bounded(), value.NumText("1e9999999999").Bounded())
	// Output: true false
}

func ExampleValue_Float() {
	fmt.Println(value.NumDec(decimal.MustParse("100000000000000000001")).Float())
	// Output: 1e+20
}

func ExampleValue_Literal() {
	v := value.NumLit(decimal.MustParse("1.0"), "1.0")
	fmt.Printf("%q %q\n", v.Literal(), value.Num(1).Literal())
	// Output: "1.0" ""
}

func ExampleValue_Str() {
	fmt.Printf("%q %q\n", value.Str("a").Str(), value.Num(1).Str())
	// Output: "a" ""
}

func ExampleValue_Elems() {
	for _, e := range value.Arr(value.Str("a"), value.Str("b")).Elems() {
		fmt.Println(e.Str())
	}
	// Output:
	// a
	// b
}

func ExampleValue_Len() {
	obj := value.Obj(value.Pair{Key: "a", Value: value.Null()})
	fmt.Println(value.Str("abc").Len(), value.Arr(value.Null()).Len(), obj.Len())
	// Output: 3 1 1
}

func ExampleValue_Keys() {
	v := value.Obj(value.Pair{Key: "z", Value: value.Null()}, value.Pair{Key: "a", Value: value.Null()})
	fmt.Println(v.Keys())
	// Output: [z a]
}

func ExampleValue_Entries() {
	v := value.Obj(value.Pair{Key: "a", Value: value.Num(1)}, value.Pair{Key: "b", Value: value.Num(2)})
	for _, e := range v.Entries() {
		fmt.Println(e.Key.Str(), e.Value.Dec())
	}
	// Output:
	// a 1
	// b 2
}

func ExampleValue_Get() {
	v := value.Obj(value.Pair{Key: "a", Value: value.Num(1)})
	got, ok := v.Get("a")
	fmt.Println(got.Dec(), ok)
	_, ok = v.Get("b")
	fmt.Println(ok)
	// Output:
	// 1 true
	// false
}

func ExampleValue_Index() {
	v := value.Arr(value.Str("a"), value.Str("b"), value.Str("c"))
	last, ok := v.Index(-1)
	fmt.Println(last.Str(), ok)
	_, ok = v.Index(3)
	fmt.Println(ok)
	// Output:
	// c true
	// false
}

func ExampleValue_Set() {
	v := value.Obj(value.Pair{Key: "a", Value: value.Num(1)})
	w := v.Set("b", value.Num(2))
	fmt.Println(v.Keys(), w.Keys())
	// Output: [a] [a b]
}

func ExampleValue_SetEntry() {
	v := value.Obj(value.Pair{Key: "a", Value: value.Num(1)})
	w := v.SetEntry(value.Str("a").WithMeta(&value.Meta{LineComment: "new"}), value.Num(2))
	fmt.Println(w.Entries()[0].Key.Meta().LineComment, v.Entries()[0].Key.Meta() == nil)
	// Output: new true
}

func ExampleValue_Delete() {
	v := value.Obj(value.Pair{Key: "a", Value: value.Num(1)}, value.Pair{Key: "b", Value: value.Num(2)})
	fmt.Println(v.Delete("a").Keys(), v.Keys())
	// Output: [b] [a b]
}

func ExampleValue_SetIndex() {
	v := value.Arr(value.Num(1))
	w := v.SetIndex(2, value.Num(3))
	for _, e := range w.Elems() {
		fmt.Print(e.Kind() == value.KindNull, " ")
	}
	fmt.Println(v.Len())
	// Output: false true false 1
}

func ExampleValue_Append() {
	v := value.Arr(value.Num(1))
	w := v.Append(value.Num(2), value.Num(3))
	fmt.Println(v.Len(), w.Len())
	// Output: 1 3
}

func ExampleValue_DeleteIndex() {
	v := value.Arr(value.Str("a"), value.Str("b"))
	w := v.DeleteIndex(0)
	fmt.Println(w.Len(), w.Elems()[0].Str(), v.Len())
	// Output: 1 b 2
}

func ExampleValue_Meta() {
	fmt.Println(value.Str("x").Meta() == nil)
	// Output: true
}

func ExampleValue_WithMeta() {
	v := value.Str("x").WithMeta(&value.Meta{LineComment: "# why"})
	fmt.Println(v.Meta().LineComment, v.Str())
	// Output: # why x
}

func ExampleMeta() {
	m := &value.Meta{Tag: "!!str", Style: value.Tagged | value.DoubleQuoted, Text: "0x10", Line: 3}
	v := value.Str("0x10").WithMeta(m)
	fmt.Println(v.Meta().Tag, v.Meta().Text, v.Meta().Line)
	// Output: !!str 0x10 3
}

func ExampleStyleFlags() {
	s := value.Tagged | value.Flow
	fmt.Println(s&value.Flow != 0, s&value.Literal != 0)
	// Output: true false
}

func ExamplePath() {
	p := value.Path{value.Str("items"), value.Num(0), value.Str("name")}
	for _, c := range p {
		fmt.Print(c.Kind() == value.KindString, " ")
	}
	fmt.Println(len(p))
	// Output: true false true 3
}

func ExampleIsSliceComponent() {
	c := value.Obj(value.Pair{Key: "start", Value: value.Num(1)}, value.Pair{Key: "end", Value: value.Null()})
	start, end, ok := value.IsSliceComponent(c)
	fmt.Println(start.Dec(), end.Kind() == value.KindNull, ok)
	_, _, ok = value.IsSliceComponent(value.Str("start"))
	fmt.Println(ok)
	// Output:
	// 1 true true
	// false
}

func ExampleArrBuilder() {
	b := value.NewArrBuilder(3)
	for i := range 3 {
		b.Append(value.Num(float64(i)))
	}
	fmt.Println(b.Seal().Len())
	// Output: 3
}

func ExampleNewArrBuilder() {
	b := value.NewArrBuilder(0)
	b.Append(value.Str("a"))
	fmt.Println(b.Seal().Elems()[0].Str())
	// Output: a
}

func ExampleNewArrBuilderFrom() {
	base := value.Arr(value.Num(1), value.Num(2))
	b := value.NewArrBuilderFrom(base)
	b.Append(value.Num(3))
	fmt.Println(b.Seal().Len(), base.Len())
	// Output: 3 2
}

func ExampleArrBuilder_Append() {
	b := value.NewArrBuilder(2)
	b.Append(value.Str("x"))
	b.Append(value.Str("y"))
	fmt.Println(b.Seal().Elems()[1].Str())
	// Output: y
}

func ExampleArrBuilder_Set() {
	b := value.NewArrBuilderFrom(value.Arr(value.Str("a"), value.Str("b")))
	b.Set(0, value.Str("z"))
	fmt.Println(b.Seal().Elems()[0].Str())
	// Output: z
}

func ExampleArrBuilder_Seal() {
	b := value.NewArrBuilder(0)
	v := b.Seal()
	fmt.Println(v.Kind() == value.KindArray, v.Len())
	// Output: true 0
}

func ExampleObjBuilder() {
	b := value.NewObjBuilder(2)
	b.Set("name", value.Str("web"))
	b.Set("port", value.Num(80))
	fmt.Println(b.Seal().Keys())
	// Output: [name port]
}

func ExampleNewObjBuilder() {
	b := value.NewObjBuilder(0)
	b.Set("a", value.Null())
	fmt.Println(b.Seal().Keys())
	// Output: [a]
}

func ExampleNewObjBuilderFrom() {
	base := value.Obj(value.Pair{Key: "a", Value: value.Num(1)})
	b := value.NewObjBuilderFrom(base)
	b.Set("b", value.Num(2))
	fmt.Println(b.Seal().Keys(), base.Keys())
	// Output: [a b] [a]
}

func ExampleObjBuilder_Set() {
	b := value.NewObjBuilder(0)
	b.Set("a", value.Num(1))
	b.Set("a", value.Num(2))
	v := b.Seal()
	a, _ := v.Get("a")
	fmt.Println(v.Len(), a.Dec())
	// Output: 1 2
}

func ExampleObjBuilder_SetEntry() {
	b := value.NewObjBuilder(0)
	b.SetEntry(value.Num(1), value.Str("one"))
	fmt.Println(b.Seal().Keys())
	// Output: [1]
}

func ExampleObjBuilder_Delete() {
	b := value.NewObjBuilderFrom(value.Obj(value.Pair{Key: "a", Value: value.Null()}, value.Pair{Key: "b", Value: value.Null()}))
	b.Delete("a")
	fmt.Println(b.Seal().Keys())
	// Output: [b]
}

func ExampleObjBuilder_Seal() {
	v := value.NewObjBuilder(0).Seal()
	fmt.Println(v.Kind() == value.KindObject, v.Len())
	// Output: true 0
}

func ExampleCompare() {
	fmt.Println(
		value.Compare(value.Null(), value.Bool(false)),
		value.Compare(value.Num(10), value.Str("1")),
		value.Compare(value.Arr(value.Num(1)), value.Arr(value.Num(1), value.Num(0))),
		value.Compare(value.Num(math.Copysign(0, -1)), value.Num(0)),
	)
	// Output: -1 -1 -1 0
}

func ExampleCompareFloat() {
	big := value.NumDec(decimal.MustParse("100000000000000000001"))
	fmt.Println(value.Compare(big, value.Num(1e20)), value.CompareFloat(big, value.Num(1e20)))
	// Output: 1 0
}

func ExampleEqual() {
	nan := value.NumDec(decimal.NaN())
	fmt.Println(value.Equal(value.Num(1), value.NumLit(decimal.MustParse("1.0"), "1.0")), value.Equal(nan, nan))
	// Output: true false
}

func ExampleEqualFloat() {
	big := value.NumDec(decimal.MustParse("100000000000000000001"))
	fmt.Println(value.Equal(big, value.Num(1e20)), value.EqualFloat(big, value.Num(1e20)))
	// Output: false true
}

func ExampleCanonicalLiteral() {
	for _, lit := range []string{"3.0e2", "1e1000", "1.50", "abc"} {
		fmt.Println(value.CanonicalLiteral(lit))
	}
	// Output:
	// 3.0E+2
	// 1E+1000
	// 1.50
	// abc
}

func ExampleFromGo() {
	v, _ := value.FromGo(map[string]interface{}{"b": 1, "a": []interface{}{"x"}})
	fmt.Println(v.Keys())
	// Output: [a b]
}

func ExampleToGo() {
	v := value.Obj(value.Pair{Key: "n", Value: value.Num(3)})
	m := value.ToGo(v, value.ToGoOptions{Numbers: value.ToGoInt64}).(map[string]interface{})
	fmt.Println(m["n"])
	// Output: 3
}

func ExampleToGoNumbers() {
	v := value.Num(7)
	fmt.Printf("%T %T\n", value.ToGo(v), value.ToGo(v, value.ToGoOptions{Numbers: value.ToGoFloat64}))
	// Output: decimal.Decimal float64
}

func ExampleToGoOptions() {
	opts := value.ToGoOptions{Numbers: value.ToGoInt64}
	fmt.Printf("%T\n", value.ToGo(value.Num(7), opts))
	// Output: int64
}

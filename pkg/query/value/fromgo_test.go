package value

import (
	"testing"

	"github.com/fivetwenty-io/graft/pkg/decimal"
)

func TestFromGoKeysInSpruceOrder(t *testing.T) {
	v, err := FromGo(map[string]interface{}{"b": 1, "a": []interface{}{"x"}, "10": true, "9": nil})
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, v.Keys(), []string{"9", "10", "a", "b"})
	a, _ := v.Get("a")
	wantEqual(t, a.Len(), 1)
}

func TestFromGoScalars(t *testing.T) {
	cases := map[string]interface{}{"7": 7, "-3": int64(-3), "18446744073709551615": uint64(1<<64 - 1), "2.5": 2.5}
	for want, in := range cases {
		v, err := FromGo(in)
		if err != nil {
			t.Fatal(err)
		}
		wantEqual(t, v.Dec().String(), want)
	}
}

func TestFromGoRejectsUnsupportedTypes(t *testing.T) {
	_, err := FromGo(map[string]interface{}{"a": []interface{}{make(chan int)}})
	wantErrContains(t, err, "chan int at .a[0]")
	_, err = FromGo(make(chan int))
	wantErrContains(t, err, "at the root")
}

func TestToGoInvertsFromGo(t *testing.T) {
	in := map[string]interface{}{"n": int64(5), "l": []interface{}{"x", true, nil}, "f": 0.5}
	v, err := FromGo(in)
	if err != nil {
		t.Fatal(err)
	}
	out := ToGo(v, ToGoOptions{Numbers: ToGoInt64}).(map[string]interface{})
	wantEqual(t, out["n"], interface{}(int64(5)))
	wantEqual(t, out["l"], in["l"])
	wantEqual(t, out["f"], interface{}(decimal.MustParse("0.5")))
	wantEqual(t, ToGo(Num(0.5), ToGoOptions{Numbers: ToGoFloat64}), interface{}(0.5))
	wantEqual(t, ToGo(Num(3)), interface{}(decimal.FromInt64(3)))
}

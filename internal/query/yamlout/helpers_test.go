package yamlout

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/decimal"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

func wantEqual[T any](t *testing.T, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func lit(s string) *value.Value { return value.NumLit(decimal.MustParse(s), s) }

func dec(s string) *value.Value { return value.NumDec(decimal.MustParse(s)) }

func pair(k string, v *value.Value) value.Pair { return value.Pair{Key: k, Value: v} }

func arr(vs ...*value.Value) *value.Value { return value.Arr(vs...) }

func obj(ps ...value.Pair) *value.Value { return value.Obj(ps...) }

// sample is {"b":1,"a":{"10":1,"2":2,"x":[1,{"y":null}]},"e":[],"o":{}}.
func sample() *value.Value {
	return obj(
		pair("b", lit("1")),
		pair("a", obj(
			pair("10", lit("1")),
			pair("2", lit("2")),
			pair("x", arr(lit("1"), obj(pair("y", value.Null())))),
		)),
		pair("e", arr()),
		pair("o", obj()),
	)
}

func emitAll(t *testing.T, vs []*value.Value, o Options) string {
	t.Helper()
	var buf bytes.Buffer
	e := New(&buf, o)
	for _, v := range vs {
		if err := e.Write(v); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.Flush(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func emitOne(t *testing.T, v *value.Value, o Options) string {
	t.Helper()
	return emitAll(t, []*value.Value{v}, o)
}

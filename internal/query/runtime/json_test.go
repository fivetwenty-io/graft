package runtime

import (
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/decimal"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

func TestJSONPrintsAsJQDoes(t *testing.T) {
	v := value.Obj(
		value.Pair{Key: "s", Value: value.Str("a\"\\\n\x01\x7f/é")},
		value.Pair{Key: "n", Value: value.Arr(value.NumText("1e1000000000"), value.NumDec(decimal.NaN()), value.NumDec(decimal.Inf(-1)), value.Num(0.25))},
		value.Pair{Key: "x", Value: value.Arr(value.Null(), value.Bool(true), value.Bool(false))},
	)
	wantEqual(t, JSON(v), `{"s":"a\"\\\n\u0001\u007f/é","n":[1E+1000000000,null,-1.7976931348623157e+308,0.25],"x":[null,true,false]}`)
}

func TestDumpCutsLongValuesAtARuneBoundary(t *testing.T) {
	wantEqual(t, Dump(value.Str(strings.Repeat("a", 27))), `"`+strings.Repeat("a", 27)+`"`)
	wantEqual(t, Dump(value.Str(strings.Repeat("a", 30))), `"`+strings.Repeat("a", 24)+`..."`)
	wantEqual(t, Dump(value.Str(strings.Repeat("a", 23)+"éééé")), `"`+strings.Repeat("a", 23)+`..."`)
}

func TestTypeNameNamesEveryKind(t *testing.T) {
	got := []string{}
	for _, v := range []*value.Value{value.Null(), value.Bool(false), value.Bool(true), value.Num(1), value.Str(""), value.Arr(), value.Obj()} {
		got = append(got, TypeName(v))
	}
	wantEqual(t, strings.Join(got, " "), "null boolean boolean number string array object")
}

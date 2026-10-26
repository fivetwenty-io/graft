package eval

import (
	"testing"

	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

func TestReplaceAt(t *testing.T) {
	one := value.Num(1)
	doc := value.Obj(value.Pair{Key: "a", Value: value.Arr(value.Num(0), value.Num(0))})
	for _, c := range []struct {
		name string
		root *value.Value
		path value.Path
		want *value.Value
	}{
		{"an element", doc, value.Path{value.Str("a"), value.Num(-1)},
			value.Obj(value.Pair{Key: "a", Value: value.Arr(value.Num(0), one)})},
		{"a missing key", doc, value.Path{value.Str("b"), value.Str("c")},
			value.Obj(value.Pair{Key: "a", Value: value.Arr(value.Num(0), value.Num(0))},
				value.Pair{Key: "b", Value: value.Obj(value.Pair{Key: "c", Value: one})})},
		{"the root", doc, value.Path{}, one},
		{"a key mark", doc, value.Path{runtime.KeyMark}, nil},
		{"past the end", doc, value.Path{value.Str("a"), value.Num(2)}, nil},
		{"a key on a scalar", one, value.Path{value.Str("a")}, nil},
		{"an index on a map", doc, value.Path{value.Num(0)}, nil},
	} {
		got, ok := replaceAt(c.root, c.path, one)
		if ok != (c.want != nil) || (ok && !value.Equal(got, c.want)) {
			t.Errorf("%s: got %v, %v", c.name, got, ok)
		}
	}
}

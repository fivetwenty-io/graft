package sortkeys

import (
	"reflect"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/query/value"
)

func wantEqual[T any](t *testing.T, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestLexicalIsCodepointOrder(t *testing.T) {
	less := Less(Lexical)
	for _, p := range [][2]string{{"B", "a"}, {"a", "aa"}, {"10", "2"}, {"z", "é"}} {
		if !less(p[0], p[1]) {
			t.Errorf("want %q < %q", p[0], p[1])
		}
	}
}

func TestNoneHasNoComparison(t *testing.T) {
	if Less(None) != nil {
		t.Error("None must have no comparison")
	}
}

func TestSpruceIsNumericFirstThenNatural(t *testing.T) {
	in := []string{"b", "10", "a10", "2", "a2"}
	wantEqual(t, Keys(in, Spruce), []string{"2", "10", "a2", "a10", "b"})
	wantEqual(t, Keys(in, Lexical), []string{"10", "2", "a10", "a2", "b"})
	wantEqual(t, Keys(in, None), in)
	wantEqual(t, in, []string{"b", "10", "a10", "2", "a2"})
}

func obj(pairs ...value.Pair) *value.Value { return value.Obj(pairs...) }

func TestSortValueIsRecursiveAndKeepsMeta(t *testing.T) {
	inner := obj(value.Pair{Key: "10", Value: value.Num(1)}, value.Pair{Key: "2", Value: value.Num(2)}).
		WithMeta(&value.Meta{LineComment: "# keep"})
	list := value.Arr(obj(value.Pair{Key: "d", Value: value.Num(1)}, value.Pair{Key: "c", Value: value.Num(2)}))
	v := obj(value.Pair{Key: "b", Value: list}, value.Pair{Key: "a", Value: inner})

	got := Sort(v, Lexical)
	wantEqual(t, got.Keys(), []string{"a", "b"})
	a, _ := got.Get("a")
	wantEqual(t, a.Keys(), []string{"10", "2"})
	b, _ := got.Get("b")
	b0, _ := b.Index(0)
	wantEqual(t, b0.Keys(), []string{"c", "d"})
	wantEqual(t, a.Meta().LineComment, "# keep")

	sa, _ := Sort(v, Spruce).Get("a")
	wantEqual(t, sa.Keys(), []string{"2", "10"})
	if Sort(v, None) != v {
		t.Error("None must return the same value")
	}
	wantEqual(t, v.Keys(), []string{"b", "a"})
}

func TestSortKeepsTypedAndDuplicateKeys(t *testing.T) {
	v := value.ObjEntries(
		value.Entry{Key: value.Num(1), Value: value.Str("x")},
		value.Entry{Key: value.Str("1"), Value: value.Str("y")},
		value.Entry{Key: value.Str("0"), Value: value.Str("z")},
		value.Entry{Key: value.Str("0"), Value: value.Str("w")},
	)
	s := Sort(v, Lexical)
	es := s.Entries()
	wantEqual(t, len(es), 4)
	wantEqual(t, es[0].Value.Str(), "z")
	wantEqual(t, es[1].Value.Str(), "w")
	wantEqual(t, es[2].Key.Kind(), value.KindNumber)
	wantEqual(t, es[3].Key.Kind(), value.KindString)
}

func TestParse(t *testing.T) {
	if _, err := Parse("alpha"); err == nil || err.Error() != `unknown sort mode "alpha" (none, lexical, or spruce)` {
		t.Fatal(err)
	}
	m, err := Parse("spruce")
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, m, Spruce)
}

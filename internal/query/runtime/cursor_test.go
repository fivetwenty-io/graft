package runtime

import (
	"testing"

	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// cursorInput is {"a": {"b": [1, 2]}, "1": "one"} with a number key 7 too.
func cursorInput() *value.Value {
	return value.ObjEntries(
		value.Entry{Key: value.Str("a"), Value: value.Obj(value.Pair{Key: "b", Value: value.Arr(value.Num(1), value.Num(2))})},
		value.Entry{Key: value.Num(7), Value: value.Str("seven")},
	)
}

func TestCursorAnswersParentKeyAndPath(t *testing.T) {
	root := cursorInput()
	c := Cursor{Root: root}.Child(value.Str("a")).Child(value.Str("b")).Child(value.Num(1))
	wantEqual(t, c.Node().Float(), 2.0)
	p, ok := c.Parent()
	wantEqual(t, ok, true)
	wantEqual(t, p.Kind(), value.KindArray)
	k, ok := c.Key()
	wantEqual(t, ok, true)
	wantEqual(t, k.Float(), 1.0)
	pk, _ := Cursor{Root: root}.Child(value.Str("a")).Child(value.Str("b")).Key()
	wantEqual(t, pk.Str(), "b")
	wantEqual(t, len(c.Path), 3)
	wantEqual(t, c.Path[0].Str()+c.Path[1].Str(), "ab")
	wantEqual(t, c.Path[2].Float(), 1.0)
	_, ok = Cursor{Root: root}.Parent()
	wantEqual(t, ok, false)
	_, ok = Cursor{Root: root}.Key()
	wantEqual(t, ok, false)
	wantEqual(t, Cursor{Root: root}.Node(), root)
}

func TestCursorChildDoesNotShareItsPath(t *testing.T) {
	a := Cursor{Root: cursorInput(), Doc: 2}.Child(value.Str("a"))
	b := a.Child(value.Str("b"))
	x := a.Child(value.Str("x"))
	wantEqual(t, b.Path[1].Str(), "b")
	wantEqual(t, x.Path[1].Str(), "x")
	wantEqual(t, len(a.Path), 1)
	wantEqual(t, b.Doc, 2)
}

func TestCursorNodeFollowsGetpathRules(t *testing.T) {
	root := cursorInput()
	at := func(path ...*value.Value) *value.Value {
		return Cursor{Root: root, Path: path}.Node()
	}
	wantEqual(t, at(value.Num(7)).Str(), "seven") // a non-string key, as yq loads it
	wantEqual(t, at(value.Str("a"), value.Str("b"), value.Num(-1)).Float(), 2.0)
	wantEqual(t, at(value.Str("a"), value.Str("b"), value.Num(1.5)).Float(), 2.0)
	wantEqual(t, at(value.Str("a"), value.Str("b"), value.Num(-1.5)).Float(), 2.0) // truncated, as getpath does
	wantEqual(t, at(value.Str("missing")).Kind(), value.KindNull)
	wantEqual(t, at(value.Str("missing"), value.Str("deeper")).Kind(), value.KindNull)
	wantEqual(t, at(value.Str("a"), value.Str("b"), value.Num(9)).Kind(), value.KindNull)
	wantEqual(t, at(value.Str("a"), value.Num(0)).Kind(), value.KindNull) // a number is a key yq could hold
	wantEqual(t, at(value.Str("a"), value.Obj()), (*value.Value)(nil))
	wantEqual(t, at(value.Str("a"), value.Str("b"), value.Str("c")), (*value.Value)(nil))
	wantEqual(t, Cursor{}.Node(), (*value.Value)(nil))
	p, ok := Cursor{Root: root, Path: value.Path{value.Str("missing"), value.Str("x")}}.Parent()
	wantEqual(t, ok, true)
	wantEqual(t, p.Kind(), value.KindNull)
}

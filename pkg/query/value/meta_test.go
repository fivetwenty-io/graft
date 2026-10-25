package value

import "testing"

func TestKeyMetaSurvivesUpdates(t *testing.T) {
	key := Str("a").WithMeta(&Meta{Style: SingleQuoted})
	tagged := Str("b").WithMeta(&Meta{Style: Tagged, Tag: "!!str"})
	obj := ObjEntries(Entry{Key: key, Value: Num(1)}, Entry{Key: tagged, Value: Num(2)})

	set := obj.Set("a", Num(3))
	wantEqual(t, set.Entries()[0].Key.Meta().Style, SingleQuoted)
	wantEqual(t, set.Entries()[1].Key.Meta().Tag, "!!str")

	b := NewObjBuilderFrom(obj)
	b.Set("c", Num(4))
	built := b.Seal()
	wantEqual(t, built.Entries()[0].Key.Meta().Style, SingleQuoted)

	del := obj.Delete("a")
	wantEqual(t, del.Entries()[0].Key.Meta().Tag, "!!str")
}

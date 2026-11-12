package graft

import (
	"context"
	"reflect"
	"testing"
	"time"
)

type label string

func TestPlainGoValue(t *testing.T) {
	in := map[string]interface{}{
		"s":    []string{"a", "b"},
		"n":    []int{1, 2},
		"arr":  [2]string{"x", "y"},
		"m":    map[string]string{"k": "v"},
		"lm":   map[label]int{"k": 1},
		"deep": []map[string][]int{{"a": {1}}},
		"b":    []byte("raw"),
		"ok":   []interface{}{"z"},
	}
	want := map[string]interface{}{
		"s":    []interface{}{"a", "b"},
		"n":    []interface{}{1, 2},
		"arr":  []interface{}{"x", "y"},
		"m":    map[string]interface{}{"k": "v"},
		"lm":   map[string]interface{}{"k": 1},
		"deep": []interface{}{map[string]interface{}{"a": []interface{}{1}}},
		"b":    []byte("raw"),
		"ok":   []interface{}{"z"},
	}
	if got := plainGoValue(in); !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v\nwant %#v", got, want)
	}
}

func TestPlainGoValueKeepsPlainAndUnkeyedValues(t *testing.T) {
	plain := map[string]interface{}{"l": []interface{}{"a", map[string]interface{}{"k": 1}}}
	got, ok := plainGoValue(plain).(map[string]interface{})
	if !ok || reflect.ValueOf(got).Pointer() != reflect.ValueOf(plain).Pointer() {
		t.Error("an already plain map was rebuilt")
	}
	nonString := map[int]string{1: "a"}
	if got := plainGoValue(nonString); !reflect.DeepEqual(got, nonString) {
		t.Errorf("non-string-keyed map changed: %#v", got)
	}
}

func TestDocumentsAcceptTypedValues(t *testing.T) {
	doc := NewDocument(map[string]interface{}{"l": []string{"a", "b"}})
	if s, err := doc.GetSlice("l"); err != nil || len(s) != 2 {
		t.Errorf("GetSlice(l) = %v, %v", s, err)
	}
	if err := doc.Set("m", map[string]int{"k": 1}); err != nil {
		t.Fatal(err)
	}
	if m, err := doc.GetMap("m"); err != nil || m["k"] != 1 {
		t.Errorf("GetMap(m) = %v, %v", m, err)
	}
	fromAny, err := NewDocumentFromInterface(map[string]interface{}{"n": []int{3}})
	if err != nil {
		t.Fatal(err)
	}
	if s, err := fromAny.GetSlice("n"); err != nil || len(s) != 1 {
		t.Errorf("GetSlice(n) = %v, %v", s, err)
	}
	typedRoot, err := NewDocumentFromInterface(map[string][]string{"t": {"q"}})
	if err != nil {
		t.Fatal(err)
	}
	if s, err := typedRoot.GetSlice("t"); err != nil || len(s) != 1 {
		t.Errorf("GetSlice(t) = %v, %v", s, err)
	}
}

func TestTypedValuesEvaluateTheSameInBothModes(t *testing.T) {
	var results [2]string
	for i, parallel := range []bool{false, true} {
		engine := newModeEngine(t, parallel)
		doc := NewDocument(map[string]interface{}{
			"l": []string{"a", "b"},
			"j": "(( join \",\" l ))",
		})
		out, err := engine.Evaluate(context.Background(), doc)
		if err != nil {
			t.Fatalf("parallel=%v: %v", parallel, err)
		}
		results[i], _ = out.GetString("j")
	}
	if results[0] != "a,b" || results[1] != results[0] {
		t.Errorf("sequential %q, parallel %q", results[0], results[1])
	}
}

func TestPlainGoValueAllocatesNothingForPlainTrees(t *testing.T) {
	tree := map[string]interface{}{
		"a": []interface{}{"x", 1, map[string]interface{}{"k": true}},
		"b": map[string]interface{}{"c": []byte("raw"), "d": nil},
	}
	if n := testing.AllocsPerRun(100, func() { plainGoValue(tree) }); n != 0 {
		t.Errorf("plainGoValue allocated %v times on a plain tree", n)
	}
	if n := testing.AllocsPerRun(100, func() { NewDocument(tree) }); n > 1 {
		t.Errorf("NewDocument allocated %v times on a plain tree", n)
	}
}

func TestPlainGoValueSharedSubstructureIsLinear(t *testing.T) {
	var shared interface{} = []string{"leaf"}
	for i := 0; i < 40; i++ {
		shared = []interface{}{shared, shared}
	}
	done := make(chan interface{}, 1)
	go func() { done <- plainGoValue(shared) }()
	select {
	case got := <-done:
		top, ok := got.([]interface{})
		if !ok || len(top) != 2 {
			t.Fatalf("unexpected result %T", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("conversion of a heavily shared tree did not finish")
	}
}

func TestPlainGoValueSharedContainerConvertsOnce(t *testing.T) {
	leaf := []string{"a"}
	got := plainGoValue(map[string]interface{}{"x": leaf, "y": leaf}).(map[string]interface{})
	x, y := got["x"].([]interface{}), got["y"].([]interface{})
	if len(x) != 1 || len(y) != 1 || &x[0] != &y[0] {
		t.Errorf("shared slice was not converted once: %#v", got)
	}
}

func TestPlainGoValueKeepsCyclesCyclic(t *testing.T) {
	m := map[string]interface{}{"typed": []string{"a"}}
	m["self"] = m
	got := plainGoValue(m).(map[string]interface{})
	if _, ok := got["typed"].([]interface{}); !ok {
		t.Errorf("typed slice next to a cycle not converted: %T", got["typed"])
	}
	if inner, ok := got["self"].(map[string]interface{}); !ok || inner["self"] == nil {
		t.Errorf("cycle was lost: %#v", got["self"])
	}

	s := make([]interface{}, 2)
	s[0] = s
	s[1] = []int{7}
	gs := plainGoValue(s).([]interface{})
	if _, ok := gs[1].([]interface{}); !ok {
		t.Errorf("typed element next to a slice cycle not converted: %T", gs[1])
	}

	// A plain cycle is too deep for the quick check and must still end.
	plain := map[string]interface{}{}
	plain["self"] = plain
	if got := plainGoValue(plain).(map[string]interface{}); got["self"] == nil {
		t.Error("plain cycle was lost")
	}
}

type namedBytes []byte

func TestPlainGoValueEdgeShapes(t *testing.T) {
	got := plainGoValue(map[string]interface{}{
		"nilSlice": []string(nil),
		"nilMap":   map[string]int(nil),
		"arrOfSl":  [2][]string{{"a"}, {"b", "c"}},
		"named":    namedBytes("xy"),
	}).(map[string]interface{})
	if v, ok := got["nilSlice"].([]interface{}); !ok || len(v) != 0 {
		t.Errorf("nil typed slice = %#v", got["nilSlice"])
	}
	if v, ok := got["nilMap"].(map[string]interface{}); !ok || len(v) != 0 {
		t.Errorf("nil typed map = %#v", got["nilMap"])
	}
	want := []interface{}{[]interface{}{"a"}, []interface{}{"b", "c"}}
	if !reflect.DeepEqual(got["arrOfSl"], want) {
		t.Errorf("array of slices = %#v", got["arrOfSl"])
	}
	if _, ok := got["named"].(namedBytes); !ok {
		t.Errorf("named byte slice changed to %T", got["named"])
	}
}

func TestSetTypedValueThenEvaluate(t *testing.T) {
	engine := newModeEngine(t, false)
	doc := NewDocument(map[string]interface{}{"j": "(( join \"-\" l ))"})
	if err := doc.Set("l", []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	out, err := engine.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := out.GetString("j"); v != "a-b" {
		t.Errorf("j = %q", v)
	}
}

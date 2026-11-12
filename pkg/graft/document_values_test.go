package graft

import (
	"context"
	"reflect"
	"testing"
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

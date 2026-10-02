package yamlnode

import (
	"fmt"
	"reflect"
	"testing"
)

func sortedNames(in ...interface{}) []string {
	keys := make([]reflect.Value, len(in))
	for i, k := range in {
		keys[i] = reflect.ValueOf(k)
	}
	SortKeys(keys)
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = fmt.Sprintf("%T:%v", k.Interface(), k.Interface())
	}
	return out
}

func TestSortKeysMatchesYAMLv3StringOrder(t *testing.T) {
	got := sortedNames("é", "z09", "x-10", "a1", "10k", "B", "_u", "a01", "x-2", "1k", "a", "z9", "a001", "x-1")
	want := []string{"string:_u", "string:1k", "string:10k", "string:B", "string:a", "string:a1", "string:a01",
		"string:a001", "string:x-1", "string:x-2", "string:x-10", "string:z9", "string:z09", "string:é"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order %v, want %v", got, want)
	}
}

func TestSortKeysMatchesYAMLv3MixedOrder(t *testing.T) {
	got := sortedNames(1, "b", true, 2.5, -3, "10", "a", false, uint(7), "a10", "a9", 0.5)
	want := []string{"int:-3", "bool:false", "float64:0.5", "bool:true", "int:1", "float64:2.5", "uint:7",
		"string:10", "string:a", "string:a9", "string:a10", "string:b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order %v, want %v", got, want)
	}
}

func TestKeyLessUnwrapsInterfaces(t *testing.T) {
	m := map[interface{}]interface{}{"b": 1, "a": 2}
	keys := reflect.ValueOf(m).MapKeys()
	a, b := keys[0], keys[1]
	if a.Elem().String() != "a" {
		a, b = b, a
	}
	if !KeyLess(a, b) || KeyLess(b, a) {
		t.Fatal("KeyLess must compare the values inside interface keys")
	}
}

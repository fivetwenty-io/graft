package graft

import (
	"context"
	"reflect"
	"testing"

	"github.com/fivetwenty-io/graft/internal/literal"
)

// TestLiteralCopiesTypedContainers wraps typed containers, changes the
// originals, and checks that the merge output keeps what was wrapped.
func TestLiteralCopiesTypedContainers(t *testing.T) {
	tests := []struct {
		name   string
		build  func() interface{}
		mutate func(v interface{})
		want   interface{}
	}{
		{
			name:   "string slice",
			build:  func() interface{} { return []string{"a", "b"} },
			mutate: func(v interface{}) { v.([]string)[0] = "MUT" },
			want:   []string{"a", "b"},
		},
		{
			name:   "int slice",
			build:  func() interface{} { return []int{1, 2} },
			mutate: func(v interface{}) { v.([]int)[1] = 99 },
			want:   []int{1, 2},
		},
		{
			name:   "string map",
			build:  func() interface{} { return map[string]string{"k": "v"} },
			mutate: func(v interface{}) { v.(map[string]string)["k"] = "MUT" },
			want:   map[string]string{"k": "v"},
		},
		{
			name:   "map of string slices",
			build:  func() interface{} { return map[string][]string{"k": {"x"}} },
			mutate: func(v interface{}) { v.(map[string][]string)["k"][0] = "MUT" },
			want:   map[string][]string{"k": {"x"}},
		},
		{
			name:   "slice of string slices",
			build:  func() interface{} { return [][]string{{"x", "y"}, {"z"}} },
			mutate: func(v interface{}) { v.([][]string)[0][1] = "MUT" },
			want:   [][]string{{"x", "y"}, {"z"}},
		},
		{
			name:   "map of int maps",
			build:  func() interface{} { return map[string]map[string]int{"a": {"b": 1}} },
			mutate: func(v interface{}) { v.(map[string]map[string]int)["a"]["b"] = 99 },
			want:   map[string]map[string]int{"a": {"b": 1}},
		},
		{
			name: "pointer to string slice",
			build: func() interface{} {
				s := []string{"a", "b"}
				return &s
			},
			mutate: func(v interface{}) {
				p := v.(*[]string)
				(*p)[0] = "MUT"
				*p = append(*p, "NEW")
			},
			want: &[]string{"a", "b"},
		},
		{
			name: "pointer to map of slices",
			build: func() interface{} {
				m := map[string][]int{"k": {1}}
				return &m
			},
			mutate: func(v interface{}) {
				(*v.(*map[string][]int))["k"][0] = 99
			},
			want: &map[string][]int{"k": {1}},
		},
		{
			name: "typed slice inside an untyped map",
			build: func() interface{} {
				return map[string]interface{}{"s": []string{"a"}, "m": map[string][]string{"k": {"x"}}}
			},
			mutate: func(v interface{}) {
				m := v.(map[string]interface{})
				m["s"].([]string)[0] = "MUT"
				m["m"].(map[string][]string)["k"][0] = "MUT"
			},
			want: map[string]interface{}{"s": []string{"a"}, "m": map[string][]string{"k": {"x"}}},
		},
		{
			name:   "array of slices",
			build:  func() interface{} { return [2][]string{{"a"}, {"b"}} },
			mutate: func(v interface{}) { v.([2][]string)[1][0] = "MUT" },
			want:   [2][]string{{"a"}, {"b"}},
		},
	}
	for _, mode := range literalEngineModes(t) {
		for _, tt := range tests {
			t.Run(mode.name+"/"+tt.name, func(t *testing.T) {
				orig := tt.build()
				plan := mustParse(t, mode.eng, "params: {}\n")
				user := userDoc(orig)
				tt.mutate(orig)
				got, err := mode.eng.Merge(context.Background(), plan, user).Execute()
				if err != nil {
					t.Fatal(err)
				}
				have, err := got.Get("params")
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(have, tt.want) {
					t.Fatalf("output changed after the original did: got %#v, want %#v", have, tt.want)
				}
			})
		}
	}
}

func TestLiteralTypedContainersKeepNilAndEmpty(t *testing.T) {
	var nilSlice []string
	var nilMap map[string]string
	var nilPtr *[]string
	got := NewLiteral(map[string]interface{}{
		"ns": nilSlice, "nm": nilMap, "np": nilPtr,
		"es": []string{}, "em": map[string]string{},
	})
	m := got.Value().(map[string]interface{})
	if s := m["ns"].([]string); s != nil {
		t.Fatal("a nil slice became non-nil")
	}
	if mm := m["nm"].(map[string]string); mm != nil {
		t.Fatal("a nil map became non-nil")
	}
	if p := m["np"].(*[]string); p != nil {
		t.Fatal("a nil pointer became non-nil")
	}
	if s := m["es"].([]string); s == nil || len(s) != 0 {
		t.Fatal("an empty slice did not stay empty and non-nil")
	}
	if mm := m["em"].(map[string]string); mm == nil || len(mm) != 0 {
		t.Fatal("an empty map did not stay empty and non-nil")
	}
}

func TestLiteralTypedContainerCycleIsRefused(t *testing.T) {
	defer func() {
		if r := recover(); r != literal.CycleMessage {
			t.Fatalf("a pointer cycle panicked with %v, want %q", r, literal.CycleMessage)
		}
	}()
	var p *[]interface{}
	s := []interface{}{nil}
	p = &s
	s[0] = p
	NewLiteral(p)
}

// TestCloneCopiesTypedContainersInALiteral checks that Clone copies the
// typed containers inside a literal, so a change to the original
// document's literal never shows in the clone.
func TestCloneCopiesTypedContainersInALiteral(t *testing.T) {
	doc := NewDocument(map[string]interface{}{"params": NewLiteral(map[string]interface{}{"list": []string{"a", "b"}})})
	clone := doc.Clone()

	orig := doc.(*document).data["params"].(LiteralValue).Value().(map[string]interface{})
	orig["list"].([]string)[0] = "changed"

	got, err := clone.Get("params.list")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("the clone shows the change to the original: %#v", got)
	}
}

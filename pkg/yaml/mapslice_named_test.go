package yaml_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

type orderedNamed yaml.MapSlice

func TestNamedMapSliceIsAnOrdinarySlice(t *testing.T) {
	out, err := yaml.Marshal(orderedNamed{{Key: "b", Value: 1}, {Key: "a", Value: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "- key: b\n  value: 1\n- key: a\n  value: 2\n" {
		t.Errorf("Marshal = %q", out)
	}
}

func TestMarshalRefusalText(t *testing.T) {
	self := map[string]any{}
	self["self"] = self
	cases := []struct {
		v      any
		prefix string
	}{
		{yaml.MapSlice{{Key: "a", Value: 1}, {Key: "a", Value: 2}}, "yaml: Marshal does not support a key repeated in a yaml.MapSlice, which "},
		// A key held in an any is named by the type it holds.
		{yaml.MapSlice{{Key: map[string]int{"a": 1}, Value: 1}}, "yaml: Marshal does not support a map key of Go type map[string]int, which "},
		{map[any]int{[2]int{1, 2}: 1}, "yaml: Marshal does not support a map key of Go type [2]int, which "},
		{self, "yaml: Marshal does not support a cycle, which "},
	}
	for _, c := range cases {
		_, err := yaml.Marshal(c.v)
		if err == nil || !strings.HasPrefix(err.Error(), c.prefix) {
			t.Errorf("Marshal(%T) = %v, want %q", c.v, err, c.prefix)
			continue
		}
		var de *yaml.DecodeError
		if errors.As(err, &de) {
			t.Errorf("Marshal(%T) returned a DecodeError", c.v)
		}
	}
}

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
		{yaml.MapSlice{{Key: "a", Value: 1}, {Key: "a", Value: 2}}, "Marshal does not support a key repeated in a yaml.MapSlice, which "},
		// A MapSlice key is an any, so the text names that static type.
		{yaml.MapSlice{{Key: map[string]int{"a": 1}, Value: 1}}, "Marshal does not support a map key of Go type interface {}, which "},
		{self, "Marshal does not support a cycle, which "},
	}
	for _, c := range cases {
		_, err := yaml.Marshal(c.v)
		if err == nil || !strings.Contains(err.Error(), c.prefix) {
			t.Errorf("Marshal(%T) = %v, want %q", c.v, err, c.prefix)
			continue
		}
		var de *yaml.DecodeError
		if errors.As(err, &de) {
			t.Errorf("Marshal(%T) returned a DecodeError", c.v)
		}
	}
}

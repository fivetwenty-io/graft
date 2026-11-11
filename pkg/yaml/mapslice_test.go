package yaml_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/sortkeys"
	"github.com/fivetwenty-io/graft/pkg/yaml"
)

type orderedValue struct{}

func (orderedValue) MarshalYAML() (any, error) {
	return yaml.MapSlice{{Key: "b", Value: 1}, {Key: "a", Value: 2}}, nil
}

func TestMarshalMapSliceKeepsItsOrder(t *testing.T) {
	t.Parallel()
	type service struct {
		Name string        `yaml:"name"`
		Env  yaml.MapSlice `yaml:"env"`
		Tags yaml.MapSlice `yaml:"tags,flow"`
		None yaml.MapSlice `yaml:"none,omitempty"`
	}
	v := map[string]any{
		"z": yaml.MapSlice{
			{Key: "zeta", Value: "1"},
			{Key: "alpha", Value: yaml.MapSlice{{Key: "c", Value: true}, {Key: "b", Value: nil}}},
		},
		"a": []any{yaml.MapSlice{{Key: "2", Value: "two"}, {Key: 10, Value: "ten"}, {Key: 9, Value: "nine"}}},
		"w": service{
			Name: "n",
			Env:  yaml.MapSlice{{Key: "Z", Value: "1"}, {Key: "A", Value: map[string]any{"q": 1, "c": 2}}},
			Tags: yaml.MapSlice{{Key: "b", Value: 1}, {Key: "a", Value: 2}},
		},
		"p": &yaml.MapSlice{{Key: "y2", Value: 1}, {Key: "x2", Value: 2}},
		"o": orderedValue{},
	}
	want := "a:\n- \"2\": two\n  10: ten\n  9: nine\n" +
		"o:\n  b: 1\n  a: 2\n" +
		"p:\n  y2: 1\n  x2: 2\n" +
		"w:\n  name: \"n\"\n  env:\n    Z: \"1\"\n    A:\n      c: 2\n      q: 1\n  tags: {b: 1, a: 2}\n" +
		"z:\n  zeta: \"1\"\n  alpha:\n    c: true\n    b: null\n"
	for _, order := range []sortkeys.Mode{"", sortkeys.Spruce, sortkeys.Lexical} {
		got, err := yaml.MarshalWith(v, yaml.EncodeOptions{KeyOrder: order})
		if err != nil {
			t.Fatalf("order %q: %v", order, err)
		}
		if string(got) != want {
			t.Errorf("order %q:\ngot  %q\nwant %q", order, got, want)
		}
	}
	empty, err := yaml.Marshal(yaml.MapSlice(nil))
	if err != nil || string(empty) != "{}\n" {
		t.Errorf("nil MapSlice: got %q, %v, want \"{}\\n\"", empty, err)
	}
}

func TestMarshalMapSliceRefusesARepeatedKey(t *testing.T) {
	t.Parallel()
	cases := []struct {
		v    any
		want string
	}{
		{yaml.MapSlice{{Key: "a", Value: 1}, {Key: "a", Value: 2}},
			"yaml: Marshal does not support a key repeated in a yaml.MapSlice, which yaml.MapSlice holds at item 1"},
		{map[string]any{"m": yaml.MapSlice{{Key: "x", Value: 0}, {Key: "1", Value: "s"}, {Key: 1, Value: "i"}}},
			"yaml: Marshal does not support a key repeated in a yaml.MapSlice, which map[string]interface {}{value} holds at item 2"},
	}
	for _, c := range cases {
		out, err := yaml.Marshal(c.v)
		if err == nil || out != nil || err.Error() != c.want {
			t.Errorf("got %q, %v, want %q", out, err, c.want)
		}
	}
}

func TestMarshalMapSliceRefusesWhatAMapRefuses(t *testing.T) {
	t.Parallel()
	if _, err := yaml.Marshal(yaml.MapSlice{{Key: []int{1}, Value: 1}}); err == nil {
		t.Error("a collection key: got no error")
	}
	self := &yaml.MapSlice{}
	*self = append(*self, yaml.MapItem{Key: "self", Value: self})
	if _, err := yaml.Marshal(self); err == nil {
		t.Error("a cycle: got no error")
	}
}

func TestUnmarshalRefusesMapSlice(t *testing.T) {
	t.Parallel()
	var ms yaml.MapSlice
	err := yaml.Unmarshal([]byte("a: 1\n"), &ms)
	var de *yaml.DecodeError
	if !errors.As(err, &de) || de.Kind != yaml.ErrorKindType {
		t.Fatalf("got %v, want an ErrorKindType DecodeError", err)
	}
	if want := "Unmarshal does not support yaml.MapSlice, which *yaml.MapSlice holds at yaml.MapSlice; decode into a yaml.Node to keep key order"; de.Msg != want {
		t.Errorf("got %q, want %q", de.Msg, want)
	}
	var held struct{ Env yaml.MapSlice }
	err = yaml.Unmarshal([]byte("env: {a: 1}\n"), &held)
	if !errors.As(err, &de) || !strings.HasPrefix(de.Msg, "Unmarshal does not support yaml.MapSlice, which ") {
		t.Errorf("in a struct: got %v", err)
	}
}

func TestMarshalMapSliceNestedKeepsOrderAtIndentFour(t *testing.T) {
	t.Parallel()
	type holder struct {
		Env yaml.MapSlice `yaml:"env"`
	}
	v := map[string]any{
		"m": map[string]any{"k": yaml.MapSlice{{Key: "b", Value: 1}, {Key: "a", Value: []any{1}}}},
		"s": holder{Env: yaml.MapSlice{{Key: "z", Value: 1}, {Key: "y", Value: yaml.MapSlice{{Key: "q", Value: 1}, {Key: "p", Value: 2}}}}},
		"l": []any{yaml.MapSlice{{Key: "d", Value: 1}, {Key: "c", Value: []any{"x"}}}},
	}
	const want = "l:\n    - d: 1\n      c:\n          - x\nm:\n    k:\n        b: 1\n        a:\n            - 1\ns:\n    env:\n        z: 1\n        \"y\":\n            q: 1\n            p: 2\n"
	got, err := yaml.MarshalWith(v, yaml.EncodeOptions{Indent: 4, IndentSequence: true})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

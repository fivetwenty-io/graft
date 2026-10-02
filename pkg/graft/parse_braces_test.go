package graft

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseYAMLReadsBracePlaceholdersAsStrings(t *testing.T) {
	doc, err := NewDefaultEngine().ParseYAML([]byte("a: {{x}}\nb: {{x}}-suffix\nl:\n- {{y}}\n- [{{z}}]\n"))
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}
	for path, want := range map[string]string{"a": "{{x}}", "b": "{{x}}-suffix"} {
		got, err := doc.GetString(path)
		if err != nil || got != want {
			t.Errorf("%s = %q (%v), want %q", path, got, err, want)
		}
	}
	list, err := doc.GetSlice("l")
	if err != nil || len(list) != 2 || list[0] != "{{y}}" || !reflect.DeepEqual(list[1], []interface{}{"{{z}}"}) {
		t.Fatalf("l = %#v (%v), want [{{y}} [{{z}}]]", list, err)
	}
}

func TestParseYAMLKeepsUnbalancedBracesAnError(t *testing.T) {
	_, err := NewDefaultEngine().ParseYAML([]byte("{{{{\n"))
	if err == nil || !strings.Contains(err.Error(), "could not find flow map content") {
		t.Fatalf("ParseYAML({{{{) error = %v, want goccy's flow map error", err)
	}
}

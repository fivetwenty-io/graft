package cursorpath

import (
	"errors"
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"a.b.0.c", []string{"a", "b", "0", "c"}},
		{"$.a.b", []string{"a", "b"}},
		{"$a.b", []string{"$a", "b"}},
		{"ips[0]", []string{"ips", "0"}},
		{"jobs.name=web.port", []string{"jobs", "name=web", "port"}},
		{"meta.[a.b].c", []string{"meta", "a.b", "c"}},
		{"a..b", []string{"a", "b"}},
		{"", nil},
	}
	for _, c := range cases {
		got, err := Parse(c.in)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("Parse(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
}

func TestParseSyntaxErrors(t *testing.T) {
	for in, want := range map[string]SyntaxError{
		"a[b[c]]": {Problem: "unexpected '['", Position: 3},
		"a]":      {Problem: "unexpected ']'", Position: 1},
	} {
		_, err := Parse(in)
		var se SyntaxError
		if !errors.As(err, &se) || se != want {
			t.Errorf("Parse(%q) error = %v, want %+v", in, err, want)
		}
	}
}

func TestPredicate(t *testing.T) {
	cases := []struct {
		seg, field, value string
		ok                bool
	}{
		{"name=web", "name", "web", true},
		{"a-b=", "a-b", "", true},
		{"id=x=y", "id", "x=y", true},
		{"0", "", "", false},
		{"=x", "", "", false},
		{"9a=x", "", "", false},
	}
	for _, c := range cases {
		f, v, ok := Predicate(c.seg)
		if f != c.field || v != c.value || ok != c.ok {
			t.Errorf("Predicate(%q) = %q, %q, %v", c.seg, f, v, ok)
		}
	}
}

func TestQuoteRoundTrips(t *testing.T) {
	for _, node := range []string{"plain", "a.b", "x.y.z"} {
		got, err := Parse(Quote(node))
		if err != nil || len(got) != 1 || got[0] != node {
			t.Errorf("Parse(Quote(%q)) = %q, %v", node, got, err)
		}
	}
	if Quote("a[b]") != "a[b]" {
		t.Error("a node holding a bracket is returned unchanged")
	}
}

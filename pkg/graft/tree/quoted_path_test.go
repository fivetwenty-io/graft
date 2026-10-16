package tree

import (
	"reflect"
	"strings"
	"testing"
)

func TestQuoteNode(t *testing.T) {
	tests := []struct {
		node string
		want string
	}{
		{node: "abc", want: "abc"},
		{node: "0", want: "0"},
		{node: "", want: ""},
		{node: "a.b.c", want: "[a.b.c]"},
		{node: "ocfp-lab-ocf.rabbitmq.net-rabbitmq", want: "[ocfp-lab-ocf.rabbitmq.net-rabbitmq]"},
		// a node holding a bracket has no spelling ParseCursor reads
		// back whole, so it is left exactly as it was
		{node: "a[b]", want: "a[b]"},
		{node: "a.b[c]", want: "a.b[c]"},
	}

	for _, tt := range tests {
		if got := QuoteNode(tt.node); got != tt.want {
			t.Errorf("QuoteNode(%q) = %q, want %q", tt.node, got, tt.want)
		}
	}
}

func TestCursorQuoted(t *testing.T) {
	tests := []struct {
		name  string
		nodes []string
		want  string
	}{
		{name: "no dotted nodes", nodes: []string{"networks", "abc", "static_ips"}, want: "networks.abc.static_ips"},
		{name: "dotted list entry name", nodes: []string{"networks", "a.b.c", "foo"}, want: "networks.[a.b.c].foo"},
		{name: "dotted first node", nodes: []string{"a.b", "c"}, want: "[a.b].c"},
		{name: "dotted last node", nodes: []string{"meta", "a.b"}, want: "meta.[a.b]"},
		{name: "empty cursor", nodes: nil, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Cursor{Nodes: tt.nodes}
			if got := c.Quoted(); got != tt.want {
				t.Fatalf("Quoted() = %q, want %q", got, tt.want)
			}

			if len(tt.nodes) == 0 {
				return
			}
			back, err := ParseCursor(c.Quoted())
			if err != nil {
				t.Fatalf("ParseCursor(%q): %v", c.Quoted(), err)
			}
			if !reflect.DeepEqual(back.Nodes, tt.nodes) {
				t.Fatalf("ParseCursor(%q) = %q, want %q", c.Quoted(), back.Nodes, tt.nodes)
			}
			if got := SplitPath(c.Quoted()); !reflect.DeepEqual(got, tt.nodes) {
				t.Fatalf("SplitPath(%q) = %q, want %q", c.Quoted(), got, tt.nodes)
			}
		})
	}
}

func TestSplitPath(t *testing.T) {
	tests := []struct {
		path string
		want []string
	}{
		{path: "networks.[a.b.c].foo", want: []string{"networks", "a.b.c", "foo"}},
		{path: "[a.b].c", want: []string{"a.b", "c"}},
		{path: "meta.[x.y]", want: []string{"meta", "x.y"}},
		{path: "groups.[a.b].list.[c.d]", want: []string{"groups", "a.b", "list", "c.d"}},
	}

	for _, tt := range tests {
		if got := SplitPath(tt.path); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("SplitPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

// TestSplitPathMatchesStringsSplit pins that a path with no bracketed
// dotted node splits exactly as strings.Split(path, ".") always did,
// including a bracket that holds no dot, so callers that switch to
// SplitPath keep their behavior for every path they handled before.
func TestSplitPathMatchesStringsSplit(t *testing.T) {
	for _, path := range []string{
		"",
		"a",
		"a.b.c",
		"a..b",
		"jobs.0.networks.1",
		"servers.name=primary",
		"servers[name=primary]",
		"meta.foo[0]",
		"$.a.b",
		"a[b.c",
		"a]b.c[",
	} {
		if got, want := SplitPath(path), strings.Split(path, "."); !reflect.DeepEqual(got, want) {
			t.Errorf("SplitPath(%q) = %q, want %q", path, got, want)
		}
	}
}

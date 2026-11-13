package yaml_test

import (
	"errors"
	"strconv"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

// checkChangePaths makes sure every change's Path either names, in both
// inputs, the node the change is about, or makes Document refuse it. It
// returns the changes.
func checkChangePaths(t *testing.T, a, b string) []yaml.Change {
	t.Helper()
	r, err := yaml.Diff([]byte(a), []byte(b), nil)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	docA, err := yaml.ParseDocument([]byte(a))
	if err != nil {
		t.Fatal(err)
	}
	docB, err := yaml.ParseDocument([]byte(b))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range r.Changes() {
		for side, doc := range map[string]*yaml.Document{"from": docA, "to": docB} {
			got, err := doc.Get(c.Path)
			if err != nil {
				if !errors.Is(err, yaml.ErrInvalidPath) &&
					!errors.Is(err, yaml.ErrPathNotFound) &&
					!errors.Is(err, yaml.ErrAmbiguousPath) {
					t.Errorf("%s %q on %s: unexpected error %v", c.Kind, c.Path, side, err)
				}
				continue
			}
			var want *yaml.Node
			switch {
			case c.Kind == yaml.ChangeModified && side == "from":
				want = c.From
			case c.Kind == yaml.ChangeModified:
				want = c.To
			case c.Kind == yaml.ChangeAdded && side == "to":
				want = c.To
			case c.Kind == yaml.ChangeRemoved && side == "from":
				want = c.From
			}
			if want == nil {
				continue
			}
			if c.Kind == yaml.ChangeModified {
				if got.Line != want.Line || got.Column != want.Column {
					t.Errorf("%s %q on %s names line %d col %d, want line %d col %d",
						c.Kind, c.Path, side, got.Line, got.Column, want.Line, want.Column)
				}
				continue
			}
			// An addition or a removal names the collection that
			// holds the entries.
			for _, entry := range want.Content {
				if !holdsAt(got, entry.Line, entry.Column) {
					t.Errorf("%s %q on %s names a collection without the entry at line %d col %d",
						c.Kind, c.Path, side, entry.Line, entry.Column)
				}
			}
		}
	}
	return r.Changes()
}

// holdsAt reports whether n holds a node at line and column.
func holdsAt(n *yaml.Node, line, column int) bool {
	for _, e := range n.Content {
		if e.Line == line && e.Column == column {
			return true
		}
	}
	return false
}

// A Diff path reads through Document as the node the change is about.
// A name the path syntax can't spell gives a path Document refuses as
// invalid, and never one that names a different node.
func TestDiffPathsRoundTripThroughDocument(t *testing.T) {
	cases := []struct {
		name    string
		a, b    string
		path    string
		invalid bool
	}{
		{
			name: "dotted list entry name",
			a:    "jobs:\n- name: a\n  b:\n    v: 1\n- name: a.b\n  v: 1\n",
			b:    "jobs:\n- name: a\n  b:\n    v: 1\n- name: a.b\n  v: 2\n",
			path: "jobs.[name=a.b].v",
		},
		{
			name:    "bracketed list entry name",
			a:       "jobs:\n- name: a[0]\n  v: 1\n- name: x\n  v: 1\n",
			b:       "jobs:\n- name: a[0]\n  v: 2\n- name: x\n  v: 1\n",
			invalid: true,
		},
		{
			name:    "empty map key",
			a:       "m:\n  \"\": {v: 1}\n  \"-1\": {v: 1}\n",
			b:       "m:\n  \"\": {v: 2}\n  \"-1\": {v: 1}\n",
			invalid: true,
		},
		{
			name:    "bracketed map key",
			a:       "m:\n  a[0]: 1\n  a: [5]\n",
			b:       "m:\n  a[0]: 2\n  a: [5]\n",
			invalid: true,
		},
		{
			name:    "dollar key at the top",
			a:       "$: {v: 1}\nv: 9\n",
			b:       "$: {v: 2}\nv: 9\n",
			invalid: true,
		},
		{
			name:    "alias key spelled like another key",
			a:       "a: &k z\nm: {*k : 1, k: 5}\n",
			b:       "a: &k z\nm: {*k : 2, k: 5}\n",
			invalid: true,
		},
		{
			name: "dollar key below the top",
			a:    "m:\n  $: 1\n",
			b:    "m:\n  $: 2\n",
			path: "m.$",
		},
		{
			name: "map key shaped like a predicate",
			a:    "m:\n  name=x: 1\n",
			b:    "m:\n  name=x: 2\n",
			path: "m.name=x",
		},
		{
			name: "map key of digits",
			a:    "m:\n  \"0\": 1\n",
			b:    "m:\n  \"0\": 2\n",
			path: "m.0",
		},
		{
			name: "dotted map key",
			a:    "m:\n  a.b: {c: 1}\n",
			b:    "m:\n  a.b: {c: 2}\n",
			path: "m.[a.b].c",
		},
		{
			name: "list entry with an empty name",
			a:    "jobs:\n- name: \"\"\n  v: 1\n- name: x\n  v: 1\n",
			b:    "jobs:\n- name: \"\"\n  v: 2\n- name: x\n  v: 1\n",
			path: "jobs.name=.v",
		},
		{
			name: "list entry name holding an equals sign",
			a:    "jobs:\n- name: a=b\n  v: 1\n- name: x\n  v: 1\n",
			b:    "jobs:\n- name: a=b\n  v: 2\n- name: x\n  v: 1\n",
			path: "jobs.name=a=b.v",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			changes := checkChangePaths(t, tc.a, tc.b)
			if len(changes) != 1 || changes[0].Kind != yaml.ChangeModified {
				t.Fatalf("changes = %+v", changes)
			}
			path := changes[0].Path
			doc, err := yaml.ParseDocument([]byte(tc.a))
			if err != nil {
				t.Fatal(err)
			}
			if tc.invalid {
				if _, err := doc.Get(path); !errors.Is(err, yaml.ErrInvalidPath) {
					t.Fatalf("Get(%q) error = %v, want ErrInvalidPath", path, err)
				}
				if err := doc.Set(path, 2); !errors.Is(err, yaml.ErrInvalidPath) {
					t.Fatalf("Set(%q) error = %v, want ErrInvalidPath", path, err)
				}
				return
			}
			if path != tc.path {
				t.Fatalf("Path = %q, want %q", path, tc.path)
			}
			if err := doc.Set(path, 2); err != nil {
				t.Fatalf("Set(%q): %v", path, err)
			}
			if got := string(doc.Bytes()); got != tc.b {
				t.Fatalf("after Set:\n%s\nwant:\n%s", got, tc.b)
			}
		})
	}
}

// Additions, removals, and nested changes in a document with awkward
// names all give paths that read back as the collection or value the
// change is about.
func TestDiffPathsRoundTripMixed(t *testing.T) {
	a := "jobs:\n- name: a\n  b: {v: 1, w: [1]}\n- name: a.b\n  v: 1\n" +
		"  l: [x]\nm:\n  \"\": {k: 1}\n  a.b: {c: 1}\n  \"-1\": 3\n"
	b := "jobs:\n- name: a\n  b: {v: 2, w: [1, 2]}\n- name: a.b\n  v: 3\n" +
		"  l: [x, y]\n  n: 1\nm:\n  \"\": {k: 2}\n  a.b: {c: 2, d: 1}\n  \"-1\": 4\n"
	if changes := checkChangePaths(t, a, b); len(changes) == 0 {
		t.Fatal("no changes")
	}
}

// A list of Kubernetes resources names its entries by a resource name
// that no field holds, so a change inside one gives a path Document
// refuses, even when an entry has a field that looks like the name.
func TestDiffPathsForKubernetesResourcesAreRefused(t *testing.T) {
	res := func(v int) string {
		return "items:\n" +
			"- apiVersion: v1\n  kind: Pod\n  metadata: {name: a}\n" +
			"  resource: v1/Pod/b\n  spec: {x: 1}\n" +
			"- apiVersion: v1\n  kind: Pod\n  metadata: {name: b}\n" +
			"  resource: v1/Pod/a\n  spec: {x: " + strconv.Itoa(v) + "}\n"
	}
	changes := checkChangePaths(t, res(1), res(2))
	if len(changes) != 1 {
		t.Fatalf("changes = %+v", changes)
	}
	doc, err := yaml.ParseDocument([]byte(res(1)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Get(changes[0].Path); !errors.Is(err, yaml.ErrInvalidPath) {
		t.Fatalf("Get(%q) error = %v, want ErrInvalidPath", changes[0].Path, err)
	}
}

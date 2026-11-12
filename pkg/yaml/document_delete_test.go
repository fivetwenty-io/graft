package yaml_test

import (
	"errors"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

func TestDocumentDelete(t *testing.T) {
	cases := []struct{ name, src, path, want string }{
		{"mapping key", "a: 1\nb: 2\nc: 3\n", "b", "a: 1\nc: 3\n"},
		{"nested block", "a:\n  x: 1\n  y:\n    z: 2\n  w: 3\n", "a.y", "a:\n  x: 1\n  w: 3\n"},
		{"last key", "a: 1\nb: 2\n", "b", "a: 1\n"},
		{"last key without newline", "a: 1\nb: 2", "b", "a: 1\n"},
		{"head comment goes with it", "a: 1\n# about b\nb: 2\nc: 3\n", "b", "a: 1\nc: 3\n"},
		{"blank line kept", "a: 1\nb: 2\n\nc: 3\n", "b", "a: 1\n\nc: 3\n"},
		{"next entry's comment kept", "a: 1\nb: 2\n# about c\nc: 3\n", "b", "a: 1\n# about c\nc: 3\n"},
		{"sequence item", "l:\n  - a\n  - b\n  - c\n", "l.1", "l:\n  - a\n  - c\n"},
		{"named item", "jobs:\n- name: api\n  port: 1\n- name: web\n  port: 2\n", "jobs.name=api", "jobs:\n- name: web\n  port: 2\n"},
		{"block scalar value", "a: |\n  one\n  two\nb: 1\n", "a", "b: 1\n"},
		{"line comments stay with their lines", "a: 1 # one\nb: 2 # two\n", "a", "b: 2 # two\n"},
		{"deeper comment goes", "a:\n  x: 1\n  # inner\n  y: 2\nb: 3\n", "a", "b: 3\n"},
	}
	for _, c := range cases {
		d := mustDoc(t, c.src)
		if err := d.Delete(c.path); err != nil {
			t.Errorf("%s: Delete: %v", c.name, err)
			continue
		}
		if got := string(d.Bytes()); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
		if _, err := d.Get(c.path); c.name != "sequence item" && !errors.Is(err, yaml.ErrPathNotFound) {
			t.Errorf("%s: Get after Delete = %v", c.name, err)
		}
	}
}

func TestDocumentDeleteRefusals(t *testing.T) {
	cases := map[string]struct {
		src, path string
		want      error
		reason    string
	}{
		"only entry":       {"a:\n  b: 1\n", "a.b", yaml.ErrUnsupportedEdit, "the entry is the only one in its collection"},
		"flow mapping":     {"a: {b: 1, c: 2}\n", "a.b", yaml.ErrUnsupportedEdit, "the entry is in a flow collection"},
		"flow sequence":    {"a: [1, 2]\n", "a.0", yaml.ErrUnsupportedEdit, "the entry is in a flow collection"},
		"key on dash line": {"l:\n- name: a\n  port: 1\n", "l.0.name", yaml.ErrUnsupportedEdit, "the key shares its line with a sequence dash"},
		"merge key":        {"base: &b {x: 1}\nm:\n  <<: *b\n  y: 2\n", "m.<<", yaml.ErrUnsupportedEdit, "the entry is a merge key"},
		"anchored value":   {"a: &x 1\nb: *x\nc: 2\n", "a", yaml.ErrUnsupportedEdit, "the value is anchored"},
		"tab in range":     {"a: 1\nb:\t2\nc: 3\n", "b", yaml.ErrUnsupportedEdit, "a tab sits on a line the edit touches"},
		"carriage return":  {"a: 1\r\nb: 2\r\n", "a", yaml.ErrUnsupportedEdit, "the input holds a carriage return"},
		"missing":          {"a: 1\nb: 2\n", "z", yaml.ErrPathNotFound, ""},
	}
	for name, c := range cases {
		d := mustDoc(t, c.src)
		err := d.Delete(c.path)
		var de *yaml.DocumentError
		if !errors.Is(err, c.want) || (c.reason != "" && (!errors.As(err, &de) || de.Reason != c.reason)) {
			t.Errorf("%s: got %v, want %v %q", name, err, c.want, c.reason)
		}
		if string(d.Bytes()) != c.src {
			t.Errorf("%s: bytes changed to %q", name, d.Bytes())
		}
	}
}

// TestDocumentDeleteShapes covers the last entry and item of a
// collection, collection and block scalar values, comments that belong
// to the entry, and anchors and aliases Delete can remove safely.
func TestDocumentDeleteShapes(t *testing.T) {
	cases := []struct{ name, src, path, want string }{
		{"last item", "l:\n  - a\n  - b\nm: 1\n", "l.1", "l:\n  - a\nm: 1\n"},
		{"last item at the end", "l:\n- a\n- b\n", "l.1", "l:\n- a\n"},
		{"last item without newline", "l:\n- a\n- b", "l.1", "l:\n- a\n"},
		{"first item", "l:\n- a\n- b\n", "l.0", "l:\n- b\n"},
		{"mapping value", "a: 1\nb:\n  x: 1\n  y: 2\nc: 3\n", "b", "a: 1\nc: 3\n"},
		{"sequence value", "a:\n  x: 1\nb:\n  - 1\n  - 2\nc: 3\n", "b", "a:\n  x: 1\nc: 3\n"},
		{"sequence at the key's indent", "a:\n- 1\n# between\n- 2\nb: 3\n", "a", "b: 3\n"},
		{"block scalar item", "l:\n- |\n  one\n  two\n- b\n", "l.0", "l:\n- b\n"},
		{"folded scalar last", "a: 1\nb: >-\n  one\n  two\n", "b", "a: 1\n"},
		{"nested sequence item", "l:\n- - a\n  - b\n- c\n", "l.0", "l:\n- c\n"},
		{"item on the line after its dash", "l:\n-\n  x: 1\n- b\n", "l.0", "l:\n- b\n"},
		{"named item last", "jobs:\n- name: api\n  port: 1\n- name: web\n  port: 2\n", "jobs.web", "jobs:\n- name: api\n  port: 1\n"},
		{"item's head comment", "l:\n  - a\n  # about b\n  - b\n  - c\n", "l.1", "l:\n  - a\n  - c\n"},
		{"multi-line flow value", "a: [1,\n  2]\nb: 3\n", "a", "b: 3\n"},
		{"first key under a document comment", "# top\na: 1\nb: 2\n", "a", "b: 2\n"},
		{"document comment apart", "# top\n\na: 1\nb: 2\n", "a", "# top\n\nb: 2\n"},
		{"foot comment at the end", "a: 1\nb: 2\n# end\n", "b", "a: 1\n"},
		{"foot comment before a blank line", "a: 1\nb: 2\n# f1\n# f2\n\nc: 3\n", "b", "a: 1\n\nc: 3\n"},
		{"item's foot comment", "l:\n- a\n- b\n# foot\n\nm: 1\n", "l.1", "l:\n- a\n\nm: 1\n"},
		{"nested foot comment", "a:\n  x: 1\n  y: 2\n  # foot\nb: 2\n", "a.y", "a:\n  x: 1\nb: 2\n"},
		{"unused anchor", "a: &x 1\nb: 2\n", "a", "b: 2\n"},
		{"anchor used only inside", "a:\n  x: &v 1\n  y: *v\nb: 2\n", "a", "b: 2\n"},
		{"alias to elsewhere inside", "base: &b 1\nuse:\n  v: *b\nc: 2\n", "use", "base: &b 1\nc: 2\n"},
		{"unused anchor above", "base: &b\n  x: 1\n  y: 2\n", "base.x", "base: &b\n  y: 2\n"},
		{"quoted << is a string", "\"<<\": 1\nb: 2\n", "<<", "b: 2\n"},
		{"comment after a blank line stays", "a: 1\nb: 2\n\n# apart\n", "b", "a: 1\n\n# apart\n"},
		{"null value", "a:\nb: 2\nc: 3\n", "a", "b: 2\nc: 3\n"},
		{"after a null value", "a:\nb: 2\nc: 3\n", "b", "a:\nc: 3\n"},
		{"anchored item", "l:\n- &a x\n- b\n", "l.0", "l:\n- b\n"},
		{"null item", "l:\n-\n- b\n", "l.0", "l:\n- b\n"},
		{"multi-line plain value", "a: x\n  y\nb: 1\n", "a", "b: 1\n"},
		{"document markers stay", "---\na:\n  b: 1\n  c: 2\n...\n", "a.c", "---\na:\n  b: 1\n...\n"},
		{"root sequence item", "- a\n-   a\n", "0", "-   a\n"},
		{"comment after the dash", "l:\n- # c\n  x: 1\n- b\n", "l.0", "l:\n- b\n"},
		{"inner item under a dash comment", "l:\n- # c\n  - a\n  - b\n- z\n", "l.0.1", "l:\n- # c\n  - a\n- z\n"},
		{"tagged item", "l:\n- !!map\n  x: 1\n- b\n", "l.0", "l:\n- b\n"},
		{"tab outside the range", "a:\t1\nb: 2\nc: 3\n", "b", "a:\t1\nc: 3\n"},
	}
	for _, c := range cases {
		d := mustDoc(t, c.src)
		if err := d.Delete(c.path); err != nil {
			t.Errorf("%s: Delete: %v", c.name, err)
			continue
		}
		if got := string(d.Bytes()); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
		if _, err := yaml.ParseDocument(d.Bytes()); err != nil {
			t.Errorf("%s: the result does not parse: %v", c.name, err)
		}
	}
}

// TestDocumentDeleteRefusesWhatItCannotCut covers the root, aliases and
// anchors, entries that share a line, and a cut whose reparse differs.
func TestDocumentDeleteRefusesWhatItCannotCut(t *testing.T) {
	const (
		root      = "the path names the document's root"
		only      = "the entry is the only one in its collection"
		alias     = "the value is an alias"
		through   = "the path passes through an alias"
		anchored  = "the value is anchored"
		shared    = "the entry does not start its own line"
		tab       = "a tab sits on a line the edit touches"
		bom       = "the input starts with a byte order mark"
		otherPart = "the edit would change another part of the document"
	)
	cases := []struct{ name, src, path, reason string }{
		{"root", "a: 1\nb: 2\n", "", root},
		{"root by $", "a: 1\nb: 2\n", "$", root},
		{"scalar root", "a\n", "", root},
		{"only item", "l:\n- a\nm: 1\n", "l.0", only},
		{"alias value", "a: &x 1\nb: *x\nc: 2\n", "b", alias},
		{"alias item", "a: &x 1\nl:\n- *x\n- b\n", "l.0", alias},
		{"through an alias", "base: &b\n  x: 1\n  y: 2\nm: *b\n", "m.x", through},
		{"anchor used below the value", "a:\n  x: &v 1\n  y: 2\nb: *v\n", "a", anchored},
		{"anchored key used", "&k a: 1\nb: 2\nc: *k\n", "a", anchored},
		{"anchor above used", "base: &b\n  x: 1\n  y: 2\nm: *b\n", "base.x", anchored},
		{"anchor above used by a merge", "base: &b\n  x: 1\n  y: 2\nm:\n  <<: *b\n  z: 3\n", "base.x", anchored},
		{"item on another item's dash line", "l:\n- - a\n  - b\n- c\n", "l.0.0", shared},
		{"explicit key", "? a\n: 1\nb: 2\n", "a", shared},
		{"tab on a deeper line", "a:\n  x: 1\n  y:\t2\nb: 3\n", "a", tab},
		{"byte order mark", "\ufeffa: 1\nb: 2\n", "a", bom},
		{"keep scalar takes the blank lines", "a: |+\n  x\nb: |+\n  y\n\nc: 1\n", "b", otherPart},
		{"flow bracket at the key's indent", "a: [\n  1\n]\nb: 2\n", "a", otherPart},
		{"head comment apart from its key", "a: 1\n\n# about b\n\nb: 2\nc: 3\n", "b", otherPart},
	}
	for _, c := range cases {
		d := mustDoc(t, c.src)
		err := d.Delete(c.path)
		var de *yaml.DocumentError
		if !errors.Is(err, yaml.ErrUnsupportedEdit) || !errors.As(err, &de) || de.Reason != c.reason || de.Op != "delete" {
			t.Errorf("%s: got %v, want %q", c.name, err, c.reason)
		}
		if string(d.Bytes()) != c.src {
			t.Errorf("%s: bytes changed to %q", c.name, d.Bytes())
		}
	}
}

// TestDocumentDeleteThenEdit checks that the tree and line index follow
// the cut, so a later edit lands where it should.
func TestDocumentDeleteThenEdit(t *testing.T) {
	d := mustDoc(t, "a: 1\n# about b\nb:\n  x: 1\nc: 3 # c\nd: 4\n")
	if err := d.Delete("b"); err != nil {
		t.Fatal(err)
	}
	if err := d.Set("d", 5); err != nil {
		t.Fatal(err)
	}
	if err := d.Delete("a"); err != nil {
		t.Fatal(err)
	}
	if got, want := string(d.Bytes()), "c: 3 # c\nd: 5\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	n, err := d.Get("c")
	if err != nil || n.Line != 1 {
		t.Errorf("Get(c) = %v, %v", n, err)
	}
}

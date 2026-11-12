package yaml_test

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

func mustDoc(t *testing.T, src string) *yaml.Document {
	t.Helper()
	d, err := yaml.ParseDocument([]byte(src))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	return d
}

func TestParseDocumentBytesAreVerbatim(t *testing.T) {
	for _, src := range []string{
		"a: 1\n",
		"# head\na:   1   # trailing\n\n\nb: [x,  y]\n",
		"a: 1",
		"---\na: 1\n...\n",
		"a: 1\n---\nb: 2\n",
		"",
		"# only a comment\n",
		"a:\r\n  b: 1\r\n",
		"\ufeffa: 1\n",
		"a:\t1\n",
	} {
		d := mustDoc(t, src)
		if got := string(d.Bytes()); got != src {
			t.Errorf("Bytes() = %q, want %q", got, src)
		}
	}
}

func TestDocumentBytesIsACopy(t *testing.T) {
	d := mustDoc(t, "a: 1\n")
	b := d.Bytes()
	b[0] = 'z'
	if string(d.Bytes()) != "a: 1\n" {
		t.Error("changing the returned slice changed the Document")
	}
}

func TestParseDocumentKeepsItsOwnCopyOfTheInput(t *testing.T) {
	in := []byte("a: 1\n")
	d, err := yaml.ParseDocument(in)
	if err != nil {
		t.Fatal(err)
	}
	in[0] = 'z'
	if string(d.Bytes()) != "a: 1\n" {
		t.Error("changing the caller's slice changed the Document")
	}
}

func TestParseDocumentRefusesBadYAML(t *testing.T) {
	_, err := yaml.ParseDocument([]byte("a: [1\n"))
	var de *yaml.DecodeError
	if !errors.As(err, &de) || de.Kind != yaml.ErrorKindSyntax {
		t.Fatalf("got %v, want an ErrorKindSyntax DecodeError", err)
	}
}

func TestParseDocumentRefusesAnUndefinedAlias(t *testing.T) {
	if _, err := yaml.ParseDocument([]byte("a: *nope\n")); err == nil {
		t.Fatal("ParseDocument accepted an alias with no anchor")
	}
}

const getSrc = `name: web
jobs:
- name: api
  ips: [10.0.0.1, 10.0.0.2]
- name: worker
  port: 8080
meta:
  owner: &o team-a
  copy: *o
  a.b: dotted
`

func TestDocumentGet(t *testing.T) {
	d := mustDoc(t, getSrc)
	cases := []struct {
		path, want string
		line       int
	}{
		{"name", "web", 1},
		{"jobs.0.name", "api", 3},
		{"jobs.name=worker.port", "8080", 6},
		{"jobs.worker.port", "8080", 6},
		{"jobs.api.ips[1]", "10.0.0.2", 4},
		{"$.meta.owner", "team-a", 8},
		{"meta.copy", "team-a", 8},
		{"meta.[a.b]", "dotted", 10},
	}
	for _, c := range cases {
		n, err := d.Get(c.path)
		if err != nil {
			t.Errorf("Get(%q): %v", c.path, err)
			continue
		}
		if n.Kind != yaml.ScalarNode || n.Value != c.want || n.Line != c.line {
			t.Errorf("Get(%q) = %v %q line %d, want %q line %d", c.path, n.Kind, n.Value, n.Line, c.want, c.line)
		}
	}
	root, err := d.Get("")
	if err != nil || root.Kind != yaml.MappingNode || len(root.Content) != 6 {
		t.Errorf("Get(\"\") = %+v, %v; want the root mapping", root, err)
	}
}

// TestDocumentGetMatchesListItemsAsTreeDoes pins the sequence rules to
// pkg/graft/tree's. A name field is tried in NameFields order, the first
// item that matches wins, and a segment that looks like an index or a
// predicate is read only as one.
func TestDocumentGetMatchesListItemsAsTreeDoes(t *testing.T) {
	d := mustDoc(t, `l:
- id: a
  v: by-id
- key: a
  v: by-key
- name: dup
  v: first
- name: dup
  v: second
- name: "7"
  v: named-seven
- name: x=y
  v: named-pred
`)
	for path, want := range map[string]string{
		"l.a.v":      "by-key",
		"l.dup.v":    "first",
		"l.name=dup": "",
		"l.id=a.v":   "by-id",
		"l.4.v":      "named-seven",
	} {
		n, err := d.Get(path)
		if want == "" {
			if err != nil || n.Kind != yaml.MappingNode || n.Line != 6 {
				t.Errorf("Get(%q) = %+v, %v; want the mapping on line 6", path, n, err)
			}
			continue
		}
		if err != nil || n.Value != want {
			t.Errorf("Get(%q) = %+v, %v; want %q", path, n, err, want)
		}
	}
	for _, path := range []string{"l.7.v", "l.x=y.v"} {
		if _, err := d.Get(path); !errors.Is(err, yaml.ErrPathNotFound) {
			t.Errorf("Get(%q) = %v, want ErrPathNotFound", path, err)
		}
	}
}

func TestDocumentGetFollowsAliasesIntoCollections(t *testing.T) {
	d := mustDoc(t, "base: &b\n  port: 1\nuse: *b\nitems: &i [x, y]\nmore: *i\n")
	for path, want := range map[string]string{"use.port": "1", "more.1": "y"} {
		n, err := d.Get(path)
		if err != nil || n.Value != want {
			t.Errorf("Get(%q) = %+v, %v; want %q", path, n, err, want)
		}
	}
}

func TestDocumentGetErrors(t *testing.T) {
	d := mustDoc(t, "a: {b: 1}\nl: [x]\nm:\n  <<: {c: 1}\n")
	for path, want := range map[string]error{
		"a.c":   yaml.ErrPathNotFound,
		"l.3":   yaml.ErrPathNotFound,
		"a.b.c": yaml.ErrPathNotFound,
		"m.c":   yaml.ErrPathNotFound,
		"a[":    yaml.ErrInvalidPath,
		"a]":    yaml.ErrInvalidPath,
	} {
		_, err := d.Get(path)
		var de *yaml.DocumentError
		if !errors.Is(err, want) || !errors.As(err, &de) || de.Op != "get" || de.Path != path {
			t.Errorf("Get(%q) = %v, want %v in a DocumentError", path, err, want)
		}
	}
	for src, want := range map[string]error{
		"a: 1\n---\na: 2\n":  yaml.ErrAmbiguousPath,
		"a: 1\na: 2\n":       yaml.ErrAmbiguousPath,
		"":                   yaml.ErrPathNotFound,
		"# only a comment\n": yaml.ErrPathNotFound,
	} {
		if _, err := mustDoc(t, src).Get("a"); !errors.Is(err, want) {
			t.Errorf("%q: Get(a) = %v, want %v", src, err, want)
		}
	}
	// The two keys differ in tag but both read as the text 1.
	if _, err := mustDoc(t, "1: a\n'1': b\n").Get("1"); !errors.Is(err, yaml.ErrAmbiguousPath) {
		t.Errorf("Get(1) over an int and a string key = %v, want ErrAmbiguousPath", err)
	}
}

func TestDocumentErrorText(t *testing.T) {
	d := mustDoc(t, "a:\n  b: 1\n")
	_, err := d.Get("a.c")
	if got, want := err.Error(), "yaml: get a.c: the key is not in the mapping at line 2"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	_, err = mustDoc(t, "a: 1\n---\nb: 2\n").Get("a")
	if got, want := err.Error(), "yaml: get a: the input holds more than one document"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestDocumentErrorNeverQuotesInput(t *testing.T) {
	d := mustDoc(t, "secret: hunter2\nlist: [hunter3]\n")
	for _, path := range []string{"secret.x", "list.5", "list.name=x"} {
		_, err := d.Get(path)
		if err == nil || strings.Contains(err.Error(), "hunter") {
			t.Errorf("Get(%q) error %v quotes the input", path, err)
		}
	}
}

// TestDocumentGetRootMatchesUnmarshal checks that the tree Get hands back
// is the one Unmarshal into a Node gives for the same text, in its
// positions and in what it decodes to.
func TestDocumentGetRootMatchesUnmarshal(t *testing.T) {
	for _, src := range []string{
		getSrc,
		"a:\t1\nb: [x,\ty]\n",
		"a: 1\rb:\t2\rc: [x,\ty]\r",
		"a:\r\n  b: 1\r\n  c: |\r\n    text\r\n",
		"\ufeffa: 1\n",
		"%YAML 1.1\n---\na: yes\nb: !!str 5\n",
		"---\nlit: |2-\n    kept\nfold: >\n  y\n...\n",
		"a: 1",
	} {
		var want yaml.Node
		if err := yaml.Unmarshal([]byte(src), &want); err != nil {
			t.Fatalf("Unmarshal(%q): %v", src, err)
		}
		got, err := mustDoc(t, src).Get("")
		if err != nil {
			t.Fatalf("%q: Get(\"\"): %v", src, err)
		}
		if diff := nodeDiff(got, want.Content[0], "$"); diff != "" {
			t.Errorf("%q: %s", src, diff)
		}
		var gotV, wantV any
		if err := got.Decode(&gotV); err != nil {
			t.Fatalf("%q: Decode: %v", src, err)
		}
		if err := want.Content[0].Decode(&wantV); err != nil {
			t.Fatalf("%q: Decode: %v", src, err)
		}
		if !reflect.DeepEqual(gotV, wantV) {
			t.Errorf("%q: Get decodes to %#v, Unmarshal to %#v", src, gotV, wantV)
		}
	}
}

// nodeDiff describes the first place a and b differ in Kind, Tag, Value,
// Line, Column, or shape, or returns "".
func nodeDiff(a, b *yaml.Node, at string) string {
	switch {
	case a.Kind != b.Kind || a.Tag != b.Tag || a.Value != b.Value:
		return at + ": the nodes differ"
	case a.Line != b.Line || a.Column != b.Column:
		return at + ": the positions differ"
	case len(a.Content) != len(b.Content):
		return at + ": the child counts differ"
	}
	for i := range a.Content {
		if d := nodeDiff(a.Content[i], b.Content[i], at+"."+strconv.Itoa(i)); d != "" {
			return d
		}
	}
	return ""
}

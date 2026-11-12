package yaml_test

import (
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

func changeList(t *testing.T, a, b string, opts *yaml.DiffOptions) []string {
	t.Helper()
	r, err := yaml.Diff([]byte(a), []byte(b), opts)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	var out []string
	for _, c := range r.Changes() {
		out = append(out, c.Kind.String()+" "+c.Path)
	}
	sort.Strings(out)
	return out
}

func TestDiffChanges(t *testing.T) {
	got := changeList(t,
		"a: 1\nb: {x: 1}\nl: [1, 2]\njobs:\n- name: api\n  port: 1\nmeta:\n  a.b: 1\n",
		"a: 2\nb: {x: 1, y: 2}\nl: [1]\njobs:\n- name: api\n  port: 2\nmeta:\n  a.b: 2\nc: 3\n",
		nil)
	want := []string{"added ", "added b", "modified a", "modified jobs.name=api.port", "modified meta.[a.b]", "removed l"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("changes:\n got %q\nwant %q", got, want)
	}
}

func TestDiffChangeNodes(t *testing.T) {
	r, err := yaml.Diff([]byte("a: 1\n"), []byte("a: two\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	cs := r.Changes()
	if len(cs) != 1 || cs[0].Kind != yaml.ChangeModified || cs[0].Document != 0 {
		t.Fatalf("changes = %+v", cs)
	}
	if cs[0].From.Value != "1" || cs[0].To.Value != "two" || cs[0].To.Line != 1 {
		t.Errorf("nodes = %+v, %+v", cs[0].From, cs[0].To)
	}
	cs[0].Path = "changed"
	if r.Changes()[0].Path != "a" {
		t.Error("Changes returned the report's own slice")
	}
}

func TestDiffEmpty(t *testing.T) {
	r, err := yaml.Diff([]byte("a: 1\nb: [1, 2]\n"), []byte("b: [1, 2]\na: 1\n"), nil)
	if err != nil || !r.Empty() || len(r.Changes()) != 0 {
		t.Errorf("Empty = %v, changes %d, %v", r.Empty(), len(r.Changes()), err)
	}
}

func TestDiffIgnoreOrderChanges(t *testing.T) {
	a := "l:\n- name: x\n- name: y\n"
	b := "l:\n- name: y\n- name: x\n"
	if got := changeList(t, a, b, nil); len(got) != 1 || got[0] != "order l" {
		t.Errorf("without the option: %q", got)
	}
	r, err := yaml.Diff([]byte(a), []byte(b), &yaml.DiffOptions{IgnoreOrderChanges: true})
	if err != nil || !r.Empty() {
		t.Errorf("with the option: Empty = %v, %v", r.Empty(), err)
	}
}

func TestDiffStreams(t *testing.T) {
	r, err := yaml.Diff([]byte("a: 1\n---\nb: 1\n"), []byte("a: 1\n---\nb: 2\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	cs := r.Changes()
	if len(cs) != 1 || cs[0].Document != 1 || cs[0].Path != "b" {
		t.Errorf("changes = %+v", cs)
	}
	if r, err := yaml.Diff([]byte(`{"a": 1}`), []byte("a: 1\n"), nil); err != nil || !r.Empty() {
		t.Errorf("JSON against YAML: %v, %v", r, err)
	}
}

func TestDiffErrors(t *testing.T) {
	_, err := yaml.Diff([]byte("a: [1\n"), []byte("a: 1\n"), nil)
	var de *yaml.DecodeError
	if !errors.As(err, &de) || de.Kind != yaml.ErrorKindSyntax {
		t.Errorf("broken input: %v", err)
	}
	_, err = yaml.Diff([]byte("secret: hunter2\n"), []byte("secret: hunter2\n---\nb: 2\n"), nil)
	var diffErr *yaml.DiffError
	if !errors.As(err, &diffErr) || diffErr.Reason != "the inputs hold different numbers of documents" {
		t.Errorf("document count: %v", err)
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Errorf("the error quotes the input: %v", err)
	}
}

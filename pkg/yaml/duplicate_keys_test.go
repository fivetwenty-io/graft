package yaml_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

// collect returns options whose callback appends each report to got.
func collect(got *[]yaml.DuplicateKey) yaml.DecodeOptions {
	return yaml.DecodeOptions{OnDuplicateKey: func(d yaml.DuplicateKey) {
		*got = append(*got, d)
	}}
}

// brokerManifest repeats a top-level features key, as three forge
// manifests do.
const brokerManifest = `name: redis
features:
  backup: true
plans:
  standalone:
    vm_type: small
features:
  tls: false
`

func TestOnDuplicateKeyForgeShape(t *testing.T) {
	var got []yaml.DuplicateKey
	var m map[string]any
	if err := yaml.UnmarshalWith([]byte(brokerManifest), &m, collect(&got)); err != nil {
		t.Fatalf("UnmarshalWith: %v", err)
	}
	want := []yaml.DuplicateKey{{Path: "features", Line: 7, Column: 1, FirstLine: 2, FirstColumn: 1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reports = %+v, want %+v", got, want)
	}
	if f, _ := m["features"].(map[string]any); !reflect.DeepEqual(f, map[string]any{"tls": false}) {
		t.Fatalf("features = %#v, want the last value", m["features"])
	}
}

func TestOnDuplicateKeyNestedAndList(t *testing.T) {
	doc := "plans:\n  standalone:\n    features: 1\n    features: 2\nlist:\n  - a: 1\n  - b: 1\n  - name: x\n    name: y\n"
	var got []yaml.DuplicateKey
	var m map[string]any
	if err := yaml.UnmarshalWith([]byte(doc), &m, collect(&got)); err != nil {
		t.Fatalf("UnmarshalWith: %v", err)
	}
	want := []yaml.DuplicateKey{
		{Path: "plans.standalone.features", Line: 4, Column: 5, FirstLine: 3, FirstColumn: 5},
		{Path: "list[2].name", Line: 9, Column: 5, FirstLine: 8, FirstColumn: 5},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reports = %+v, want %+v", got, want)
	}
}

func TestOnDuplicateKeyTriple(t *testing.T) {
	var got []yaml.DuplicateKey
	var m map[string]any
	if err := yaml.UnmarshalWith([]byte("a: 1\na: 2\nb: 0\na: 3\n"), &m, collect(&got)); err != nil {
		t.Fatalf("UnmarshalWith: %v", err)
	}
	want := []yaml.DuplicateKey{
		{Path: "a", Line: 2, Column: 1, FirstLine: 1, FirstColumn: 1},
		{Path: "a", Line: 4, Column: 1, FirstLine: 1, FirstColumn: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reports = %+v, want %+v", got, want)
	}
	if m["a"] != 3 {
		t.Fatalf("a = %v, want 3", m["a"])
	}
}

func TestOnDuplicateKeyNilStaysStrict(t *testing.T) {
	var m map[string]any
	err := yaml.UnmarshalWith([]byte(brokerManifest), &m, yaml.DecodeOptions{})
	var de *yaml.DecodeError
	if !errors.As(err, &de) || de.Msg != "mapping key already defined at [2:1]" || de.Line != 7 {
		t.Fatalf("err = %v, want the strict refusal", err)
	}
	if err := yaml.Unmarshal([]byte(brokerManifest), &m); err == nil {
		t.Fatal("Unmarshal accepted a duplicate key")
	}
}

func TestOnDuplicateKeyTargets(t *testing.T) {
	doc := []byte("a: 1\nb: 2\na: 3\n")
	var got []yaml.DuplicateKey
	opts := collect(&got)

	var s struct {
		A int `yaml:"a"`
		B int `yaml:"b"`
	}
	if err := yaml.UnmarshalWith(doc, &s, opts); err != nil || s.A != 3 || s.B != 2 {
		t.Fatalf("struct = %+v, %v", s, err)
	}
	var m map[string]int
	if err := yaml.UnmarshalWith(doc, &m, opts); err != nil || m["a"] != 3 {
		t.Fatalf("map = %v, %v", m, err)
	}
	var a any
	if err := yaml.UnmarshalWith(doc, &a, opts); err != nil || !reflect.DeepEqual(a, map[string]any{"a": 3, "b": 2}) {
		t.Fatalf("any = %v, %v", a, err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d reports, want 3", len(got))
	}

	var n yaml.Node
	got = nil
	if err := yaml.UnmarshalWith(doc, &n, opts); err != nil {
		t.Fatalf("node: %v", err)
	}
	if len(got) != 1 || got[0] != (yaml.DuplicateKey{Path: "a", Line: 3, Column: 1, FirstLine: 1, FirstColumn: 1}) {
		t.Fatalf("node reports = %+v", got)
	}
	root := n.Content[0]
	if len(root.Content) != 4 || root.Content[0].Value != "a" || root.Content[1].Value != "3" ||
		root.Content[2].Value != "b" || root.Content[3].Value != "2" {
		t.Fatalf("node content = %d pairs, want a:3 then b:2", len(root.Content)/2)
	}
	var back map[string]int
	if err := n.Decode(&back); err != nil || back["a"] != 3 {
		t.Fatalf("Decode of collapsed node = %v, %v", back, err)
	}
}

func TestOnDuplicateKeyNodeNested(t *testing.T) {
	var got []yaml.DuplicateKey
	var n yaml.Node
	doc := "list:\n  - x: 1\n    x: 2\n"
	if err := yaml.UnmarshalWith([]byte(doc), &n, collect(&got)); err != nil {
		t.Fatal(err)
	}
	want := []yaml.DuplicateKey{{Path: "list[0].x", Line: 3, Column: 5, FirstLine: 2, FirstColumn: 5}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reports = %+v, want %+v", got, want)
	}
}

func TestOnDuplicateKeyNodeNilKeepsTree(t *testing.T) {
	var n yaml.Node
	if err := yaml.Unmarshal([]byte("a: 1\na: 2\n"), &n); err != nil {
		t.Fatal(err)
	}
	if len(n.Content[0].Content) != 4 {
		t.Fatalf("a Node read without a callback must keep both pairs, has %d", len(n.Content[0].Content)/2)
	}
}

func TestOnDuplicateKeyMergeKeyNeverReported(t *testing.T) {
	doc := "base: &b\n  x: 1\n  y: 2\nitem:\n  <<: *b\n  x: 9\n"
	for _, target := range []any{new(map[string]any), new(yaml.Node)} {
		var got []yaml.DuplicateKey
		if err := yaml.UnmarshalWith([]byte(doc), target, collect(&got)); err != nil {
			t.Fatalf("%T: %v", target, err)
		}
		if len(got) != 0 {
			t.Fatalf("%T: merge key reported %+v", target, got)
		}
	}
	var m map[string]any
	if err := yaml.UnmarshalWith([]byte(doc), &m, collect(new([]yaml.DuplicateKey))); err != nil {
		t.Fatal(err)
	}
	item, _ := m["item"].(map[string]any)
	if item["x"] != 9 || item["y"] != 2 {
		t.Fatalf("item = %v, want x 9 and y 2", item)
	}
	// Two merge keys in one mapping are not duplicates either.
	var got []yaml.DuplicateKey
	two := "a: &a {p: 1}\nb: &b {q: 2}\nc:\n  <<: *a\n  <<: *b\n"
	var m2 map[string]any
	if err := yaml.UnmarshalWith([]byte(two), &m2, collect(&got)); len(got) != 0 {
		t.Fatalf("two merge keys reported %+v (err %v)", got, err)
	}
}

func TestOnDuplicateKeyFlowMapping(t *testing.T) {
	var got []yaml.DuplicateKey
	var m map[string]any
	if err := yaml.UnmarshalWith([]byte("o: {a: 1, b: 2, a: 3}\n"), &m, collect(&got)); err != nil {
		t.Fatal(err)
	}
	want := []yaml.DuplicateKey{{Path: "o.a", Line: 1, Column: 17, FirstLine: 1, FirstColumn: 5}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reports = %+v, want %+v", got, want)
	}
	if o, _ := m["o"].(map[string]any); o["a"] != 3 {
		t.Fatalf("o = %v", m["o"])
	}
}

func TestOnDuplicateKeyRootSequenceAndDottedKey(t *testing.T) {
	var got []yaml.DuplicateKey
	var a any
	doc := "- {k: 1, k: 2}\n- [{a.b: 1, a.b: 2}]\n"
	if err := yaml.UnmarshalWith([]byte(doc), &a, collect(&got)); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Path != "[0].k" || got[1].Path != "[1][0].a.b" {
		t.Fatalf("reports = %+v", got)
	}
}

func TestOnDuplicateKeyStillCalledWhenDecodeFails(t *testing.T) {
	var got []yaml.DuplicateKey
	var s struct {
		N int `yaml:"n"`
	}
	err := yaml.UnmarshalWith([]byte("n: 1\nn: 2\nn: [x]\n"), &s, collect(&got))
	if err == nil {
		t.Fatal("expected a type error")
	}
	if len(got) != 2 {
		t.Fatalf("got %d reports, want 2 before the failure", len(got))
	}
}

func TestOnDuplicateKeyCallbackPanicPropagates(t *testing.T) {
	defer func() {
		if r := recover(); r != "boom" {
			t.Fatalf("recovered %v, want the callback's panic", r)
		}
	}()
	var m map[string]any
	_ = yaml.UnmarshalWith([]byte("a: 1\na: 2\n"), &m, yaml.DecodeOptions{OnDuplicateKey: func(yaml.DuplicateKey) { panic("boom") }})
	t.Fatal("no panic")
}

func TestOnDuplicateKeyErrorsNeverQuoteKeys(t *testing.T) {
	var m map[string]any
	err := yaml.UnmarshalWith([]byte("secretkey: 1\nsecretkey: 2\nbad: [\n"), &m, collect(new([]yaml.DuplicateKey)))
	if err == nil {
		t.Fatal("expected a syntax error")
	}
	if strings.Contains(err.Error(), "secretkey") {
		t.Fatalf("error quotes a key: %v", err)
	}
	err = yaml.Unmarshal([]byte("secretkey: 1\nsecretkey: 2\n"), &m)
	if err == nil || strings.Contains(err.Error(), "secretkey") {
		t.Fatalf("strict error = %v", err)
	}
}

// nodeReports decodes doc into a Node and returns what the callback saw.
func nodeReports(t *testing.T, doc string) []yaml.DuplicateKey {
	t.Helper()
	var got []yaml.DuplicateKey
	var n yaml.Node
	if err := yaml.UnmarshalWith([]byte(doc), &n, collect(&got)); err != nil {
		t.Fatalf("Node UnmarshalWith: %v", err)
	}
	return got
}

// textReports decodes doc into a map and returns what the callback saw.
func textReports(t *testing.T, doc string) []yaml.DuplicateKey {
	t.Helper()
	var got []yaml.DuplicateKey
	var m any
	if err := yaml.UnmarshalWith([]byte(doc), &m, collect(&got)); err != nil {
		t.Fatalf("text UnmarshalWith: %v", err)
	}
	return got
}

func TestOnDuplicateKeyAnchoredMappingAndAlias(t *testing.T) {
	doc := "base: &b\n  x: 1\n  x: 2\nuse1: *b\nuse2: *b\nplain:\n  <<: *b\n  y: 1\n  y: 2\n"
	want := []yaml.DuplicateKey{
		{Path: "base.x", Line: 3, Column: 3, FirstLine: 2, FirstColumn: 3},
		{Path: "plain.y", Line: 9, Column: 3, FirstLine: 8, FirstColumn: 3},
	}
	if got := textReports(t, doc); !reflect.DeepEqual(got, want) {
		t.Fatalf("text reports = %+v, want %+v", got, want)
	}
	if got := nodeReports(t, doc); !reflect.DeepEqual(got, want) {
		t.Fatalf("node reports = %+v, want %+v", got, want)
	}
	var m map[string]map[string]int
	if err := yaml.UnmarshalWith([]byte(doc), &m, yaml.DecodeOptions{OnDuplicateKey: func(yaml.DuplicateKey) {}}); err != nil {
		t.Fatal(err)
	}
	if m["use1"]["x"] != 2 || m["use2"]["x"] != 2 || m["plain"]["x"] != 2 || m["plain"]["y"] != 2 {
		t.Fatalf("decoded = %v, want last values through the alias", m)
	}
}

// A Node counts a tab as one column, as yaml.v3 does, and the text route
// reads the parser's own count, which skips it, so the two columns differ
// by one on a continuation line of a multi-line flow mapping that holds a
// tab. Line, path, count, and the first key's position still agree.
func TestOnDuplicateKeyTabInFlowContinuation(t *testing.T) {
	cases := []struct {
		doc        string
		text, node yaml.DuplicateKey
	}{
		{
			"x: {a: 1,\n\ta: 2}\n",
			yaml.DuplicateKey{Path: "x.a", Line: 2, Column: 2, FirstLine: 1, FirstColumn: 5},
			yaml.DuplicateKey{Path: "x.a", Line: 2, Column: 3, FirstLine: 1, FirstColumn: 5},
		},
		{
			"x: {a: 1,\n \t a: 2}\n",
			yaml.DuplicateKey{Path: "x.a", Line: 2, Column: 4, FirstLine: 1, FirstColumn: 5},
			yaml.DuplicateKey{Path: "x.a", Line: 2, Column: 5, FirstLine: 1, FirstColumn: 5},
		},
	}
	for _, c := range cases {
		text, node := textReports(t, c.doc), nodeReports(t, c.doc)
		if !reflect.DeepEqual(text, []yaml.DuplicateKey{c.text}) {
			t.Errorf("doc %q: text = %+v, want %+v", c.doc, text, c.text)
		}
		if !reflect.DeepEqual(node, []yaml.DuplicateKey{c.node}) {
			t.Errorf("doc %q: node = %+v, want %+v", c.doc, node, c.node)
		}
	}
}

func TestOnDuplicateKeyRoutesAgreeOnPositions(t *testing.T) {
	docs := []string{
		"a: 1\na: 2\n",
		"\"a\": 1\n'a': 2\n",
		"&k a: 1\n&j a: 2\n",
		"!!str a: 1\n!!str a: 2\n",
		"l:\n  - {a: 1, a: 2}\n  - a: 1\n    a: 2\n",
		"x: {\n  a: 1,\n  a: 2}\n",
		"é: 1\né: 2\n",
		"o: {é: 1, é: 2}\n",
		"? a\n: 1\na: 2\n",
		"? &k a\n: 1\na: 2\n",
		"? !!str a\n: 1\na: 2\n",
		"a: 1\n? a\n: 2\n",
	}
	for _, doc := range docs {
		text, node := textReports(t, doc), nodeReports(t, doc)
		if len(text) == 0 || !reflect.DeepEqual(text, node) {
			t.Errorf("doc %q: text = %+v, node = %+v", doc, text, node)
		}
	}
}

// TestOnDuplicateKeyExplicitKeyPlace pins the place of a key written
// after "?", which is the key's own place and not the indicator's.
func TestOnDuplicateKeyExplicitKeyPlace(t *testing.T) {
	want := []yaml.DuplicateKey{{Path: "a", Line: 3, Column: 1, FirstLine: 1, FirstColumn: 3}}
	if got := textReports(t, "? a\n: 1\na: 2\n"); !reflect.DeepEqual(got, want) {
		t.Fatalf("reports = %+v, want %+v", got, want)
	}
}

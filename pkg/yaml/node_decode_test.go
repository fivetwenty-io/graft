package yaml_test

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

func TestNodeDecodeScalarYesIntoBool(t *testing.T) {
	root := parseNode(t, "plain: yes\nquoted: \"yes\"\noff: Off\n").Content[0]
	var b bool
	if err := lookup(root, "plain").Decode(&b); err != nil || !b {
		t.Errorf("plain yes = %v, %v; want true", b, err)
	}
	b = true
	if err := lookup(root, "off").Decode(&b); err != nil || b {
		t.Errorf("plain Off = %v, %v; want false", b, err)
	}
	var v any
	if err := lookup(root, "plain").Decode(&v); err != nil || v != true {
		t.Errorf("plain yes into any = %#v, %v; want true", v, err)
	}
	if err := lookup(root, "quoted").Decode(&v); err != nil || v != "yes" {
		t.Errorf("quoted yes into any = %#v, %v; want \"yes\"", v, err)
	}
	// A quoted yes is refused for a bool, as Unmarshal refuses it, at the
	// scalar's line.
	wantDecodeError(t, lookup(root, "quoted").Decode(&b), yaml.ErrorKindType, 2)
}

func TestNodeDecodeKeepsTheTreesDecodeOptions(t *testing.T) {
	var n yaml.Node
	if err := yaml.UnmarshalWith([]byte("a: yes\n"), &n, yaml.DecodeOptions{YAML12: true}); err != nil {
		t.Fatal(err)
	}
	var v any
	if err := n.Content[0].Content[1].Decode(&v); err != nil || v != "yes" {
		t.Errorf("YAML 1.2 tree: yes = %#v, %v; want the string", v, err)
	}
	var b bool
	wantDecodeError(t, n.Content[0].Content[1].Decode(&b), yaml.ErrorKindType, 1)
}

type decodedJob struct {
	Name       string         `yaml:"name"`
	Instances  int            `yaml:"instances"`
	Enabled    *bool          `yaml:"enabled"`
	Properties map[string]any `yaml:"properties"`
	Skipped    string         `yaml:"-"`
}

func TestNodeDecodeMappingIntoTaggedStruct(t *testing.T) {
	root := parseNode(t, "group:\n  name: redis\n  instances: 2\n  enabled: on\n  properties:\n    port: 6379\n    tls: no\n  -: ignored\n").Content[0]
	var job decodedJob
	if err := lookup(root, "group").Decode(&job); err != nil {
		t.Fatal(err)
	}
	if job.Name != "redis" || job.Instances != 2 || job.Enabled == nil || !*job.Enabled {
		t.Errorf("job = %+v", job)
	}
	wantDeepEqual(t, job.Properties, map[string]any{"port": 6379, "tls": false})
}

func TestNodeDecodeThroughAliasNode(t *testing.T) {
	root := parseNode(t, certManifest).Content[0]
	alias := lookup(lookup(root, "instance_groups").Content[1], "instances")
	if alias.Kind != yaml.AliasNode {
		t.Fatalf("instances = %+v, want an alias", alias)
	}
	var count int
	if err := alias.Decode(&count); err != nil || count != 3 {
		t.Errorf("alias decode = %d, %v; want 3", count, err)
	}
	ca := lookup(lookup(root, "variables").Content[1], "options").Content[1]
	var opts map[string]string
	if err := ca.Decode(&opts); err != nil {
		t.Fatal(err)
	}
	wantDeepEqual(t, opts, map[string]string{"ca": "redis_ca"})
}

func TestNodeDecodeRefusesInterfaceKeyedMaps(t *testing.T) {
	root := parseNode(t, "a: {b: 1}\n").Content[0]
	var m map[interface{}]interface{}
	de := wantDecodeError(t, lookup(root, "a").Decode(&m), yaml.ErrorKindType, 0)
	if !strings.Contains(de.Msg, "map[string]any") {
		t.Errorf("Msg = %q, want it to suggest map[string]any", de.Msg)
	}
	var nested struct{ M map[interface{}]interface{} }
	wantDecodeError(t, root.Decode(&nested), yaml.ErrorKindType, 0)
	wantDecodeError(t, root.Decode(nil), yaml.ErrorKindType, 0)
	wantDecodeError(t, root.Decode(m), yaml.ErrorKindType, 0)
}

func TestNodeDecodeAliasAnchoredOutsideSubtree(t *testing.T) {
	root := parseNode(t, certManifest).Content[0]
	props := lookup(lookup(lookup(root, "instance_groups").Content[0], "jobs").Content[0], "properties")
	var got map[string]any
	if err := props.Decode(&got); err != nil {
		t.Fatal(err)
	}
	wantDeepEqual(t, got, map[string]any{
		"enabled": true,
		"port":    6379,
		"tls":     map[string]any{"cert": "((redis_tls.certificate))"},
		"quoted":  "yes",
		"list":    []any{"a", 1, "on", nil, 1.5},
	})

	var typed struct {
		Enabled bool `yaml:"enabled"`
		Port    int  `yaml:"port"`
	}
	if err := props.Decode(&typed); err != nil || !typed.Enabled || typed.Port != 6379 {
		t.Errorf("typed = %+v, %v", typed, err)
	}

	// Two aliases to one anchor outside the subtree, one of them a key.
	root = parseNode(t, "k: &k name\nv: &v {x: 1}\nsub:\n  *k : *v\n  other: *v\n").Content[0]
	got = nil
	if err := lookup(root, "sub").Decode(&got); err != nil {
		t.Fatal(err)
	}
	wantDeepEqual(t, got, map[string]any{"name": map[string]any{"x": 1}, "other": map[string]any{"x": 1}})

	// The parser never takes an alias key for a duplicate, so an alias
	// key and the text it names may share a mapping, as in Unmarshal.
	root = parseNode(t, "k: &k name\nsub:\n  name: 2\n  *k : 1\n").Content[0]
	var whole struct {
		Sub map[string]any `yaml:"sub"`
	}
	if err := yaml.Unmarshal([]byte("k: &k name\nsub:\n  name: 2\n  *k : 1\n"), &whole); err != nil {
		t.Fatal(err)
	}
	got = nil
	if err := lookup(root, "sub").Decode(&got); err != nil {
		t.Fatal(err)
	}
	wantDeepEqual(t, got, whole.Sub)
}

// errorFields is what a test compares between two DecodeErrors.
type errorFields struct {
	Line, Column int
	Kind         yaml.ErrorKind
}

func fieldsOf(t *testing.T, err error) errorFields {
	t.Helper()
	var de *yaml.DecodeError
	if !errors.As(err, &de) {
		t.Fatalf("got %T (%v), want *yaml.DecodeError", err, err)
	}
	return errorFields{de.Line, de.Column, de.Kind}
}

func TestNodeDecodeErrorLinesPointAtInput(t *testing.T) {
	doc := "name: x\nbad: &bad\n  port: notaport\njobs:\n- name: web\n  port: alsonotaport\n- <<: *bad\n  name: db\n"
	root := parseNode(t, doc).Content[0]
	type job struct {
		Name string `yaml:"name"`
		Port int    `yaml:"port"`
	}
	jobs := lookup(root, "jobs")

	var j job
	got := fieldsOf(t, jobs.Content[0].Decode(&j))
	if want := (errorFields{6, 9, yaml.ErrorKindType}); got != want {
		t.Errorf("subtree error at %+v, want %+v", got, want)
	}
	wantNoLeak(t, jobs.Content[0].Decode(&j), "alsonotaport")

	// The failing scalar sits in an anchor outside the subtree, so the
	// error points at the anchor's own lines.
	got = fieldsOf(t, jobs.Content[1].Decode(&j))
	if want := (errorFields{3, 9, yaml.ErrorKindType}); got != want {
		t.Errorf("error through an outside alias at %+v, want %+v", got, want)
	}

	// Decoding the document gives the error Unmarshal gives.
	var whole struct {
		Jobs []job `yaml:"jobs"`
	}
	direct := fieldsOf(t, yaml.Unmarshal([]byte(doc), &whole))
	viaNode := fieldsOf(t, parseNode(t, doc).Decode(&whole))
	if direct != viaNode {
		t.Errorf("Decode error %+v, Unmarshal error %+v", viaNode, direct)
	}
}

func TestNodeDecodeHandBuiltNodes(t *testing.T) {
	scalar := func(tag, value string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value} }

	var b bool
	if err := scalar("", "yes").Decode(&b); err != nil || !b {
		t.Errorf("hand-built yes = %v, %v", b, err)
	}
	for _, c := range []struct {
		n    *yaml.Node
		want any
	}{
		{scalar("", "123"), 123},
		{scalar("!!str", "123"), "123"},
		// An explicit !!str keeps a YAML 1.1 boolean word a string, in
		// Unmarshal and in a hand-built node alike.
		{scalar("!!str", "yes"), "yes"},
		{scalar("!!bool", "on"), true},
		{scalar("!!int", "0x1F"), 31},
		{scalar("", "yes"), true},
		{scalar("", ""), nil},
		{scalar("!!str", ""), ""},
		{scalar("", "line one\nline: two #\t\"q\" \\ end "), "line one\nline: two #\t\"q\" \\ end "},
		{scalar("", "- not a list"), "- not a list"},
		{scalar("", "---"), "---"},
		{scalar("", "&anchor"), "&anchor"},
		{scalar("", "\x01bell"), "\x01bell"},
		{&yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{scalar("", "a"), scalar("", "1"), scalar("", "b"), {Kind: yaml.SequenceNode}}},
			map[string]any{"a": 1, "b": []any{}}},
		{&yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}, scalar("!!float", "2.5")}},
			[]any{map[string]any{}, 2.5}},
	} {
		var v any
		if err := c.n.Decode(&v); err != nil {
			t.Errorf("%+v: %v", c.n, err)
			continue
		}
		if !reflect.DeepEqual(v, c.want) {
			t.Errorf("%+v decoded to %#v, want %#v", c.n, v, c.want)
		}
	}

	// A node shared by two places decodes in both.
	shared := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{scalar("", "x"), scalar("", "1")}}
	twice := &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{shared, shared, {Kind: yaml.AliasNode, Alias: shared}}}
	var v any
	if err := twice.Decode(&v); err != nil {
		t.Fatal(err)
	}
	wantDeepEqual(t, v, []any{map[string]any{"x": 1}, map[string]any{"x": 1}, map[string]any{"x": 1}})

	// A malformed tree is a DecodeError, never a panic.
	cycle := &yaml.Node{Kind: yaml.SequenceNode, Line: 7}
	cycle.Content = []*yaml.Node{cycle}
	aliasCycle := &yaml.Node{Kind: yaml.SequenceNode}
	aliasCycle.Content = []*yaml.Node{{Kind: yaml.AliasNode, Alias: aliasCycle, Line: 9}}
	selfAlias := &yaml.Node{Kind: yaml.AliasNode}
	selfAlias.Alias = selfAlias
	for name, n := range map[string]*yaml.Node{
		"nil receiver":       nil,
		"unknown kind":       {Kind: 3},
		"odd mapping":        {Kind: yaml.MappingNode, Content: []*yaml.Node{scalar("", "a")}},
		"nil child":          {Kind: yaml.SequenceNode, Content: []*yaml.Node{nil}},
		"nil key":            {Kind: yaml.MappingNode, Content: []*yaml.Node{nil, scalar("", "1")}},
		"alias with no node": {Kind: yaml.AliasNode, Value: "x"},
		"content cycle":      cycle,
		"alias cycle":        aliasCycle,
		"alias to itself":    selfAlias,
		"invalid UTF-8":      scalar("", "\xff"),
		"collection key":     {Kind: yaml.MappingNode, Content: []*yaml.Node{{Kind: yaml.SequenceNode}, scalar("", "1")}},
		"two document roots": {Kind: yaml.DocumentNode, Content: []*yaml.Node{scalar("", "1"), scalar("", "2")}},
		"nested document":    {Kind: yaml.SequenceNode, Content: []*yaml.Node{{Kind: yaml.DocumentNode}}},
		"tag with a space":   scalar("!a b", "x"),
	} {
		var v any
		var de *yaml.DecodeError
		if err := n.Decode(&v); !errors.As(err, &de) {
			t.Errorf("%s: got %v, want a *yaml.DecodeError", name, err)
		}
	}
	if err := cycle.Decode(&v); fieldsOf(t, err).Line != 7 {
		t.Errorf("content cycle error %v, want line 7", err)
	}
	if err := aliasCycle.Decode(&v); fieldsOf(t, err).Line != 9 {
		t.Errorf("alias cycle error %v, want line 9", err)
	}
}

func TestNodeDecodeHandBuiltDeepTreeIsALimit(t *testing.T) {
	root := &yaml.Node{Kind: yaml.SequenceNode}
	cur := root
	for range 20000 {
		next := &yaml.Node{Kind: yaml.SequenceNode}
		cur.Content = []*yaml.Node{next}
		cur = next
	}
	var v any
	wantDecodeError(t, root.Decode(&v), yaml.ErrorKindLimit, 0)
}

func TestNodeDecodeEmptyDocumentLeavesTargetUnchanged(t *testing.T) {
	v := any("keep")
	if err := (&yaml.Node{Kind: yaml.DocumentNode}).Decode(&v); err != nil || v != "keep" {
		t.Errorf("empty document: %#v, %v", v, err)
	}
	null := parseNode(t, "~\n")
	if err := null.Decode(&v); err != nil || v != "keep" {
		t.Errorf("null document: %#v, %v", v, err)
	}
}

func TestNodeDecodeSeesTheTreeAsModified(t *testing.T) {
	doc := parseNode(t, "a: 1\nb: [x]\nc: &c {k: v}\nd: *c\n")
	root := doc.Content[0]
	lookup(root, "a").Value = "2"
	b := lookup(root, "b")
	b.Content = append(b.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "yes"})
	lookup(root, "c").Content[1].Value = "changed"
	root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "e"}, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "3"})
	var got map[string]any
	if err := doc.Decode(&got); err != nil {
		t.Fatal(err)
	}
	wantDeepEqual(t, got, map[string]any{
		"a": 2,
		"b": []any{"x", true},
		"c": map[string]any{"k": "changed"},
		"d": map[string]any{"k": "changed"},
		"e": "3",
	})
}

func TestNodeDecodeIntoNode(t *testing.T) {
	root := parseNode(t, certManifest).Content[0]
	var copied yaml.Node
	if err := root.Decode(&copied); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(&copied, root) || &copied.Content[0] != &root.Content[0] {
		t.Errorf("Decode into a Node did not copy the node")
	}
	wantDecodeError(t, root.Decode((*yaml.Node)(nil)), yaml.ErrorKindType, 0)
}

func TestNodeDecodeBillionLaughsIsALimit(t *testing.T) {
	doc := parseNode(t, billionLaughs)
	start := time.Now()
	var v any
	wantDecodeError(t, doc.Decode(&v), yaml.ErrorKindLimit, 0)
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("Decode took %s", d)
	}
}

func TestNodeDecodeDeepParsedTree(t *testing.T) {
	const depth = 5000
	doc := parseNode(t, strings.Repeat("[", depth)+"x"+strings.Repeat("]", depth))
	var v any
	if err := doc.Decode(&v); err != nil {
		t.Fatal(err)
	}
	got := 0
	for {
		s, ok := v.([]any)
		if !ok {
			break
		}
		got++
		v = s[0]
	}
	if got != depth || v != "x" {
		t.Errorf("decoded %d levels around %#v, want %d around x", got, v, depth)
	}
}

func TestNodeDecodeConcurrently(t *testing.T) {
	root := parseNode(t, certManifest).Content[0]
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 10 {
				var v map[string]any
				if err := root.Decode(&v); err != nil || v["name"] != "redis-x" {
					t.Errorf("concurrent Decode = %v, %v", v["name"], err)
				}
			}
		})
	}
	wg.Wait()
}

// laughsAAndB is the first two lines of billionLaughs, whose aliases
// expand to 90 nodes.
var laughsAAndB, _, _ = strings.Cut(billionLaughs, "c:")

// decodeCorpus holds documents whose Decode through a Node must equal
// Unmarshal of the same text.
var decodeCorpus = []string{
	certManifest,
	laughsAAndB,
	"words: [yes, No, ON, off, y, n, \"yes\", 'off', true, False]\n",
	"numbers: [1, -2, 1.10, 0x1F, 012, 0o12, 1e3, .5, +.inf, -.Inf, .nan, 99999999999999999999]\n",
	"nulls: [~, null, Null, '', \"\"]\nempty:\nlast: ~\n",
	"times: [2026-01-02, 2026-01-02T03:04:05Z, '2026-01-02']\n",
	"literal: |\n  one\n  two\nstrip: |-\n  x\nkeep: |+\n  y\n\nfolded: >\n  a\n  b\n\n  c\nindented: |2\n    lead\n  base\nyes_block: |-\n  yes\n",
	"multi: plain text\n  that folds\n\n  over lines\nsq: 'it''s'\ndq: \"tab\\there\\nnew\\u00e9\"\n",
	"base: &b {x: 1, y: 2}\nother: &o\n  z: 3\nm:\n  <<: [*b, *o]\n  x: override\nn:\n  <<: *b\n",
	"&root\na: 1\n",
	"k: &key name\nm:\n  *key : value\n",
	"tpl: {{ placeholder }}\nops: (( grab meta.x ))\nlist:\n- a\n-\n- c\n",
	"\"1\": one\n2: two\ntrue: yes\n~: null key\n",
	"unicode: héllo wörld ✓\n\"ключ\": значение\n",
	"emptyc: {}\nemptys: []\nnested: [[], {}, [{a: [1, {b: c}]}]]\n",
	"tagged: !!str 12\nint: !!int \"7\"\ncustom: !thing x\nbin: !!binary aGVsbG8=\nseq: !!seq [1]\n",
	"url: http://example.com:8080/path?q=1#frag\ncolon: a:b\nhash: a#b\ndash: -x\n",
	"- top\n- level\n- [seq]\n",
	"just a scalar\n",
	"\"quoted root\"\n",
	"|\n  block root\n",
	"long_key_with_value_on_next_line:\n  value\n",
	"? " + strings.Repeat("k", 1500) + "\n: v\nshort: [{" + strings.Repeat("j", 1500) + ": w}]\n",
	deepBlock(40),
	"a: !foo\n",
	"b: !!str\n",
	"c: !foo ''\n",
	"{0,0}\n",
	"x: &x [1]\nd: {*x}\n",
	"0: &b\n<<:\t*b\n",
	"...:\t \"\"\n",
	"!000\n",
	"0: ! \n",
	"0: ---\n",
	"a: |-\n 0  \nb: 1\n",
	">\n 0 \n",
	"|+1\n",
	"x: !!str\n",
	"n\to\n",
	"c:\t---\n",
	" ---\n ...\n",
	"? |\n? |1\n",
	"? |\n\n#\n",
	"? !!0000 00 ",
	"!<tag:yaml.org,2002:str>\n",
	"a: !<tag:yaml.org,2002:str>\nb: !<tag:yaml.org,2002:int> '5'\n",
	"s: !<tag:yaml.org,2002:str> [1, 2]\nm: !<tag:yaml.org,2002:str> {x: 1}\nq: !<tag:yaml.org,2002:str>\n- 1\n",
	"? !0 0\n",
	"? !0 0",
	"! \t0\n",
	"a: !!str \tyes\n",
	" !00\n ---\n",
	"? !0 >\n",
	"m:\n ? ~\n : 1\nn:\n ? !!null ~\n : x\n",
	"? &k ~\n: 1\n",
	"{0\t0,0\t0}\n",
}

// deepBlock returns a block mapping nested depth levels deep, deeper than
// Decode writes in block style, around a few scalars.
func deepBlock(depth int) string {
	var b strings.Builder
	for i := range depth {
		b.WriteString(strings.Repeat("  ", i) + "a:\n")
	}
	pad := strings.Repeat("  ", depth)
	b.WriteString(pad + "yes: \"on\"\n" + pad + "list:\n" + pad + "- off\n" + pad + "- \"a, b: [c]\"\n" + pad + "- ~\n")
	return b.String()
}

type corpusStruct struct {
	Name      string            `yaml:"name"`
	Words     []bool            `yaml:"words"`
	Numbers   []string          `yaml:"numbers"`
	Literal   string            `yaml:"literal"`
	Keep      string            `yaml:"keep"`
	Folded    string            `yaml:"folded"`
	Indented  string            `yaml:"indented"`
	YesBlock  string            `yaml:"yes_block"`
	Multi     string            `yaml:"multi"`
	DQ        string            `yaml:"dq"`
	M         map[string]any    `yaml:"m"`
	N         map[string]string `yaml:"n"`
	Tpl       any               `yaml:"tpl"`
	Ops       string            `yaml:"ops"`
	List      []*string         `yaml:"list"`
	Unicode   string            `yaml:"unicode"`
	Nested    any               `yaml:"nested"`
	Tagged    string            `yaml:"tagged"`
	URL       string            `yaml:"url"`
	Long      string            `yaml:"long_key_with_value_on_next_line"`
	Variables []struct {
		Name    string         `yaml:"name"`
		Options map[string]any `yaml:"options"`
	} `yaml:"variables"`
}

// withoutNaN replaces each NaN inside v, a pointer to a generic value,
// with a string, since DeepEqual never finds NaN equal to itself.
func withoutNaN(v any) any {
	var walk func(any) any
	walk = func(x any) any {
		switch y := x.(type) {
		case float64:
			if math.IsNaN(y) {
				return "NaN"
			}
		case map[string]any:
			out := make(map[string]any, len(y))
			for k, item := range y {
				out[k] = walk(item)
			}
			return out
		case []any:
			out := make([]any, len(y))
			for i, item := range y {
				out[i] = walk(item)
			}
			return out
		}
		return x
	}
	switch p := v.(type) {
	case *any:
		return walk(*p)
	case *map[string]any:
		return walk(*p)
	case *[]any:
		return walk(*p)
	}
	return v
}

func TestNodeDecodeMatchesUnmarshal(t *testing.T) {
	targets := map[string]func() any{
		"any":            func() any { return new(any) },
		"map[string]any": func() any { return new(map[string]any) },
		"[]any":          func() any { return new([]any) },
		"struct":         func() any { return new(corpusStruct) },
		"string":         func() any { return new(string) },
		"bool":           func() any { return new(bool) },
		"map[string]bool": func() any {
			return new(map[string]bool)
		},
		"map[string]string": func() any {
			return new(map[string]string)
		},
	}
	for _, opts := range []yaml.DecodeOptions{{}, {YAML12: true}} {
		for i, doc := range decodeCorpus {
			var n yaml.Node
			if err := yaml.UnmarshalWith([]byte(doc), &n, opts); err != nil {
				t.Errorf("doc %d: Unmarshal into a Node: %v", i, err)
				continue
			}
			for name, fresh := range targets {
				direct, viaNode := fresh(), fresh()
				errDirect := yaml.UnmarshalWith([]byte(doc), direct, opts)
				errNode := n.Decode(viaNode)
				if (errDirect == nil) != (errNode == nil) {
					t.Errorf("doc %d into %s (YAML12 %v): Unmarshal error %v, Decode error %v", i, name, opts.YAML12, errDirect, errNode)
					continue
				}
				if errDirect != nil {
					if a, b := fieldsOf(t, errDirect), fieldsOf(t, errNode); a.Kind != b.Kind || a.Line != b.Line {
						t.Errorf("doc %d into %s (YAML12 %v): Unmarshal error %+v (%v), Decode error %+v (%v)", i, name, opts.YAML12, a, errDirect, b, errNode)
					}
					continue
				}
				if !reflect.DeepEqual(withoutNaN(direct), withoutNaN(viaNode)) {
					t.Errorf("doc %d into %s (YAML12 %v):\nUnmarshal %s\n   Decode %s", i, name, opts.YAML12, describe(reflect.ValueOf(direct).Elem().Interface()), describe(reflect.ValueOf(viaNode).Elem().Interface()))
				}
			}
		}
	}
}

func TestNodeDecodeTaggedEmptyScalarIsNull(t *testing.T) {
	for doc, want := range map[string]any{
		"a: !foo\n":  map[string]any{"a": nil},
		"b: !!str\n": map[string]any{"b": ""},
	} {
		var direct, viaNode any
		if err := yaml.Unmarshal([]byte(doc), &direct); err != nil {
			t.Fatal(err)
		}
		if err := parseNode(t, doc).Decode(&viaNode); err != nil {
			t.Fatal(err)
		}
		wantDeepEqual(t, viaNode, want)
		wantDeepEqual(t, viaNode, direct)
	}

	v := any("keep")
	if err := (&yaml.Node{Kind: yaml.ScalarNode, Tag: "!foo"}).Decode(&v); err != nil || v != "keep" {
		t.Errorf("hand-built tagged empty scalar = %#v, %v; want v unchanged", v, err)
	}
}

func TestNodeDecodeChangedScalarReadsAsItsText(t *testing.T) {
	for _, c := range []struct {
		doc, value, tag string
		opts            yaml.DecodeOptions
		want            any
	}{
		{"x: foo\n", "true", "", yaml.DecodeOptions{}, true},
		{"x: foo\n", "123", "", yaml.DecodeOptions{}, 123},
		{"x: foo\n", "yes", "", yaml.DecodeOptions{}, true},
		{"x: foo\n", "yes", "", yaml.DecodeOptions{YAML12: true}, "yes"},
		{"x: foo\n", "", "", yaml.DecodeOptions{}, nil},
		{"x: 'foo'\n", "true", "", yaml.DecodeOptions{}, "true"},
		{"x: 'foo'\n", "yes", "", yaml.DecodeOptions{}, "yes"},
		{"x: 5\n", "5", "!!str", yaml.DecodeOptions{}, "5"},
		{"x: true\n", "false", "", yaml.DecodeOptions{}, false},
		{"x: |\n  foo\n", "123", "", yaml.DecodeOptions{}, "123"},
	} {
		var n yaml.Node
		if err := yaml.UnmarshalWith([]byte(c.doc), &n, c.opts); err != nil {
			t.Fatal(err)
		}
		x := n.Content[0].Content[1]
		x.Value = c.value
		if c.tag != "" {
			x.Tag = c.tag
		}
		var got any
		if err := n.Content[0].Decode(&got); err != nil {
			t.Errorf("%q with Value %q and Tag %q: %v", c.doc, c.value, c.tag, err)
			continue
		}
		if want := map[string]any{"x": c.want}; !reflect.DeepEqual(got, want) {
			t.Errorf("%q with Value %q and Tag %q (YAML12 %v) = %#v, want %#v", c.doc, c.value, c.tag, c.opts.YAML12, got, want)
		}
	}
}

func TestNodeDecodeZeroNodeIsNull(t *testing.T) {
	// yaml.v3 v3.0.1 decodes a zero Node as null, leaving v unchanged.
	v := any("keep")
	if err := (&yaml.Node{}).Decode(&v); err != nil || v != "keep" {
		t.Errorf("zero Node = %#v, %v; want v unchanged", v, err)
	}
	var n yaml.Node
	if err := yaml.Unmarshal([]byte(""), &n); err != nil {
		t.Fatal(err)
	}
	m := map[string]int{"keep": 1}
	if err := n.Decode(&m); err != nil || !reflect.DeepEqual(m, map[string]int{"keep": 1}) {
		t.Errorf("Unmarshal(\"\") tree = %#v, %v; want m unchanged", m, err)
	}
}

func TestNodeDecodeClampsNegativeColumn(t *testing.T) {
	n := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "a"},
		{Kind: yaml.ScalarNode, Tag: "!!int", Value: "x", Line: 2, Column: -1},
	}}
	var v struct {
		A bool `yaml:"a"`
	}
	de := wantDecodeError(t, n.Decode(&v), yaml.ErrorKindSyntax, 2)
	if de.Column != 0 {
		t.Errorf("Column %d, want 0", de.Column)
	}
}

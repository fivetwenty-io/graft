package yaml_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

// certManifest is a BOSH-style certManifest with anchors, aliases, and merge
// keys, shaped like the deployed manifests the broker's certificate
// inventory walks.
const certManifest = `name: redis-x
count: &three 3
defaults: &defaults
  enabled: yes
  port: 6379
ca_opts: &ca
  ca: redis_ca
variables:
- name: redis_ca
  type: certificate
  options:
    is_ca: true
- name: redis_tls
  type: certificate
  options:
    <<: *ca
    common_name: redis.example
instance_groups:
- name: redis
  instances: 3
  jobs:
  - name: redis
    properties:
      <<: *defaults
      tls:
        cert: ((redis_tls.certificate))
      quoted: "yes"
      list: [a, 1, "on", ~, 1.5]
- name: sentinel
  instances: *three
`

// manifestTreeV3 is the tree gopkg.in/yaml.v3 v3.0.1 builds for
// certManifest, printed by dumpNode. We gathered it on 2026-11-02 with
// yaml.Unmarshal into a yaml.Node in a throwaway module, since this
// package and its tests may not import yaml.v3.
const manifestTreeV3 = `{Document "" "" L1 C1}
  {Mapping "!!map" "" L1 C1}
    {Scalar "!!str" "name" L1 C1}
    {Scalar "!!str" "redis-x" L1 C7}
    {Scalar "!!str" "count" L2 C1}
    {Scalar "!!int" "3" L2 C8}
    {Scalar "!!str" "defaults" L3 C1}
    {Mapping "!!map" "" L3 C11}
      {Scalar "!!str" "enabled" L4 C3}
      {Scalar "!!str" "yes" L4 C12}
      {Scalar "!!str" "port" L5 C3}
      {Scalar "!!int" "6379" L5 C9}
    {Scalar "!!str" "ca_opts" L6 C1}
    {Mapping "!!map" "" L6 C10}
      {Scalar "!!str" "ca" L7 C3}
      {Scalar "!!str" "redis_ca" L7 C7}
    {Scalar "!!str" "variables" L8 C1}
    {Sequence "!!seq" "" L9 C1}
      {Mapping "!!map" "" L9 C3}
        {Scalar "!!str" "name" L9 C3}
        {Scalar "!!str" "redis_ca" L9 C9}
        {Scalar "!!str" "type" L10 C3}
        {Scalar "!!str" "certificate" L10 C9}
        {Scalar "!!str" "options" L11 C3}
        {Mapping "!!map" "" L12 C5}
          {Scalar "!!str" "is_ca" L12 C5}
          {Scalar "!!bool" "true" L12 C12}
      {Mapping "!!map" "" L13 C3}
        {Scalar "!!str" "name" L13 C3}
        {Scalar "!!str" "redis_tls" L13 C9}
        {Scalar "!!str" "type" L14 C3}
        {Scalar "!!str" "certificate" L14 C9}
        {Scalar "!!str" "options" L15 C3}
        {Mapping "!!map" "" L16 C5}
          {Scalar "!!merge" "<<" L16 C5}
          {Alias "" "ca" L16 C9} -> alias@6:10
          {Scalar "!!str" "common_name" L17 C5}
          {Scalar "!!str" "redis.example" L17 C18}
    {Scalar "!!str" "instance_groups" L18 C1}
    {Sequence "!!seq" "" L19 C1}
      {Mapping "!!map" "" L19 C3}
        {Scalar "!!str" "name" L19 C3}
        {Scalar "!!str" "redis" L19 C9}
        {Scalar "!!str" "instances" L20 C3}
        {Scalar "!!int" "3" L20 C14}
        {Scalar "!!str" "jobs" L21 C3}
        {Sequence "!!seq" "" L22 C3}
          {Mapping "!!map" "" L22 C5}
            {Scalar "!!str" "name" L22 C5}
            {Scalar "!!str" "redis" L22 C11}
            {Scalar "!!str" "properties" L23 C5}
            {Mapping "!!map" "" L24 C7}
              {Scalar "!!merge" "<<" L24 C7}
              {Alias "" "defaults" L24 C11} -> alias@3:11
              {Scalar "!!str" "tls" L25 C7}
              {Mapping "!!map" "" L26 C9}
                {Scalar "!!str" "cert" L26 C9}
                {Scalar "!!str" "((redis_tls.certificate))" L26 C15}
              {Scalar "!!str" "quoted" L27 C7}
              {Scalar "!!str" "yes" L27 C15}
              {Scalar "!!str" "list" L28 C7}
              {Sequence "!!seq" "" L28 C13}
                {Scalar "!!str" "a" L28 C14}
                {Scalar "!!int" "1" L28 C17}
                {Scalar "!!str" "on" L28 C20}
                {Scalar "!!null" "~" L28 C26}
                {Scalar "!!float" "1.5" L28 C29}
      {Mapping "!!map" "" L29 C3}
        {Scalar "!!str" "name" L29 C3}
        {Scalar "!!str" "sentinel" L29 C9}
        {Scalar "!!str" "instances" L30 C3}
        {Alias "" "three" L30 C14} -> alias@2:8
`

var kindNames = map[yaml.Kind]string{
	yaml.DocumentNode: "Document",
	yaml.SequenceNode: "Sequence",
	yaml.MappingNode:  "Mapping",
	yaml.ScalarNode:   "Scalar",
	yaml.AliasNode:    "Alias",
}

// dumpNode prints n and everything in its Content, one node per line,
// the way the oracle printed yaml.v3's tree.
func dumpNode(b *strings.Builder, n *yaml.Node, depth int) {
	alias := ""
	if n.Alias != nil {
		alias = fmt.Sprintf(" -> alias@%d:%d", n.Alias.Line, n.Alias.Column)
	}
	fmt.Fprintf(b, "%s{%s %q %q L%d C%d}%s\n", strings.Repeat("  ", depth), kindNames[n.Kind], n.Tag, n.Value, n.Line, n.Column, alias)
	for _, c := range n.Content {
		dumpNode(b, c, depth+1)
	}
}

func parseNode(t *testing.T, doc string) *yaml.Node {
	t.Helper()
	var n yaml.Node
	if err := yaml.Unmarshal([]byte(doc), &n); err != nil {
		t.Fatalf("Unmarshal into a Node: %v", err)
	}
	return &n
}

// lookup returns the value of key in the mapping m, or nil.
func lookup(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func TestKindValuesMatchYAMLv3(t *testing.T) {
	// yaml.v3 declares type Kind uint32 with DocumentNode Kind = 1 << iota.
	if got := reflect.TypeFor[yaml.Kind]().Kind(); got != reflect.Uint32 {
		t.Errorf("Kind's underlying type is %s, want uint32", got)
	}
	for kind, want := range map[yaml.Kind]uint32{
		yaml.DocumentNode: 1,
		yaml.SequenceNode: 2,
		yaml.MappingNode:  4,
		yaml.ScalarNode:   8,
		yaml.AliasNode:    16,
	} {
		if uint32(kind) != want {
			t.Errorf("%s = %d, want %d", kindNames[kind], kind, want)
		}
	}
}

func TestUnmarshalNodeMatchesYAMLv3Tree(t *testing.T) {
	var b strings.Builder
	dumpNode(&b, parseNode(t, certManifest), 0)
	if got := b.String(); got != manifestTreeV3 {
		t.Errorf("tree differs from yaml.v3's\n got:\n%s\nwant:\n%s", got, manifestTreeV3)
	}
}

func TestUnmarshalNodeTags(t *testing.T) {
	doc := parseNode(t, `base: &b {x: 1}
m:
  <<: *b
  plain_yes: yes
  quoted_int: "12"
  int: 12
  float: 1.5
  bool: true
  null: ~
  empty:
  custom: !thing value
  explicit: !!str 12
  seq: [1]
  ts: 2026-01-02
`)
	m := lookup(doc.Content[0], "m")
	if m == nil || m.Kind != yaml.MappingNode || m.Tag != "!!map" {
		t.Fatalf("m = %+v, want a !!map mapping", m)
	}
	if k := m.Content[0]; k.Kind != yaml.ScalarNode || k.Tag != "!!merge" || k.Value != "<<" {
		t.Errorf("merge key = %+v, want a !!merge scalar <<", k)
	}
	for key, want := range map[string]string{
		"plain_yes":  "!!str",
		"quoted_int": "!!str",
		"int":        "!!int",
		"float":      "!!float",
		"bool":       "!!bool",
		"null":       "!!null",
		"empty":      "!!null",
		"custom":     "!thing",
		"explicit":   "!!str",
		"seq":        "!!seq",
		"ts":         "!!timestamp",
	} {
		if got := lookup(m, key); got == nil || got.Tag != want {
			t.Errorf("%s: tag = %+v, want %s", key, got, want)
		}
	}
	// Tags never depend on the decode mode.
	var n12 yaml.Node
	if err := yaml.UnmarshalWith([]byte("a: yes\n"), &n12, yaml.DecodeOptions{YAML12: true}); err != nil {
		t.Fatal(err)
	}
	if got := n12.Content[0].Content[1].Tag; got != "!!str" {
		t.Errorf("YAML12 plain yes tag = %s, want !!str", got)
	}
}

func TestUnmarshalNodeAliasIdentity(t *testing.T) {
	root := parseNode(t, certManifest).Content[0]
	defaults := lookup(root, "defaults")
	props := lookup(lookup(lookup(root, "instance_groups").Content[0], "jobs").Content[0], "properties")
	alias := props.Content[1]
	if alias.Kind != yaml.AliasNode || alias.Value != "defaults" {
		t.Fatalf("merge value = %+v, want an alias named defaults", alias)
	}
	if alias.Alias != defaults {
		t.Errorf("alias.Alias = %p, want the anchored node %p", alias.Alias, defaults)
	}
	if alias.Tag != "" || len(alias.Content) != 0 {
		t.Errorf("alias carries Tag %q and %d children, want neither", alias.Tag, len(alias.Content))
	}
	three := lookup(root, "count")
	sentinel := lookup(root, "instance_groups").Content[1]
	if got := lookup(sentinel, "instances"); got.Alias != three {
		t.Errorf("scalar alias target = %p, want %p", got.Alias, three)
	}
}

func TestUnmarshalNodeLineAndColumn(t *testing.T) {
	root := parseNode(t, "top:\n  inner: [x, \"y\"]\n  block: |\n    text\nlast: v\n").Content[0]
	inner := lookup(lookup(root, "top"), "inner")
	block := lookup(lookup(root, "top"), "block")
	// Positions from yaml.v3 v3.0.1 for the same document.
	for _, c := range []struct {
		name         string
		n            *yaml.Node
		line, column int
	}{
		{"root", root, 1, 1},
		{"top's value", lookup(root, "top"), 2, 3},
		{"inner", inner, 2, 10},
		{"x", inner.Content[0], 2, 11},
		{"y", inner.Content[1], 2, 14},
		{"block", block, 3, 10},
		{"last key", root.Content[2], 5, 1},
		{"last value", root.Content[3], 5, 7},
	} {
		if c.n.Line != c.line || c.n.Column != c.column {
			t.Errorf("%s at %d:%d, want %d:%d", c.name, c.n.Line, c.n.Column, c.line, c.column)
		}
	}
}

func TestUnmarshalNodeRefusesSelfContainingAnchor(t *testing.T) {
	var n yaml.Node
	err := yaml.Unmarshal([]byte("a: 1\nloop: &selfref\n  - x\n  - *selfref\n"), &n)
	de := wantDecodeError(t, err, yaml.ErrorKindSyntax, 4)
	wantNoLeak(t, de, "selfref")
	if n.Kind != 0 {
		t.Errorf("node filled on failure: %+v", n)
	}
}

func TestUnmarshalNodeErrors(t *testing.T) {
	var n yaml.Node
	err := yaml.Unmarshal([]byte("a: 1\nb: *nosuchanchor\n"), &n)
	wantNoLeak(t, wantDecodeError(t, err, yaml.ErrorKindSyntax, 2), "nosuchanchor")

	err = yaml.Unmarshal([]byte("a: [1\nb: supersecret\n"), &n)
	var de *yaml.DecodeError
	wantNoLeak(t, err, "supersecret")
	if !errors.As(err, &de) || de.Kind != yaml.ErrorKindSyntax {
		t.Errorf("unclosed flow sequence: %v, want an ErrorKindSyntax DecodeError", err)
	}

	err = yaml.Unmarshal([]byte("a: 1"), (*yaml.Node)(nil))
	wantDecodeError(t, err, yaml.ErrorKindType, 0)
}

func TestUnmarshalNodeEmptyLeavesNodeUnchanged(t *testing.T) {
	for _, doc := range []string{"", "# only a comment\n"} {
		n := yaml.Node{Kind: yaml.ScalarNode, Value: "keep"}
		if err := yaml.Unmarshal([]byte(doc), &n); err != nil {
			t.Fatalf("%q: %v", doc, err)
		}
		if n.Kind != yaml.ScalarNode || n.Value != "keep" {
			t.Errorf("%q changed the node to %+v", doc, n)
		}
	}
}

func TestUnmarshalNodeReadsFirstDocumentOnly(t *testing.T) {
	n := parseNode(t, "a: 1\n---\nb: [\n")
	if got := lookup(n.Content[0], "a"); got == nil || got.Value != "1" {
		t.Errorf("first document = %+v", n.Content[0])
	}
}

func TestUnmarshalNodeConverterTerminatesOnDeepInput(t *testing.T) {
	const depth = 5000
	doc := strings.Repeat("[", depth) + "x" + strings.Repeat("]", depth)
	n := parseNode(t, doc)
	got := 0
	for cur := n.Content[0]; cur.Kind == yaml.SequenceNode; cur = cur.Content[0] {
		got++
	}
	if got != depth {
		t.Errorf("nesting = %d, want %d", got, depth)
	}
}

// The tests below model the broker's certificate inventory walk, in
// certinventory/manifest.go, which reads a deployed manifest as a node
// tree. It follows aliases with deref, expands << merge keys with
// explicit keys winning, guards against cycles with pointer sets, and
// decodes scalars with Node.Decode.

type inventoryFacts struct {
	Name          string
	CertVariables []string
	SigningCAs    map[string]string
	Groups        map[string]int
	Properties    map[string]any
	CertRefs      map[string]string
}

type inventoryWalker struct {
	facts   inventoryFacts
	active  map[*yaml.Node]bool
	merging map[*yaml.Node]bool
}

type nodePair struct{ key, value *yaml.Node }

func (w *inventoryWalker) deref(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	return n
}

func (w *inventoryWalker) pairs(n *yaml.Node) []nodePair {
	if w.merging[n] {
		return nil
	}
	w.merging[n] = true
	defer delete(w.merging, n)
	var out, merged []nodePair
	seen := map[string]bool{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind == yaml.ScalarNode && k.Tag == "!!merge" {
			src := w.deref(v)
			switch {
			case src != nil && src.Kind == yaml.MappingNode:
				merged = append(merged, w.pairs(src)...)
			case src != nil && src.Kind == yaml.SequenceNode:
				for _, item := range src.Content {
					if m := w.deref(item); m != nil && m.Kind == yaml.MappingNode {
						merged = append(merged, w.pairs(m)...)
					}
				}
			}
			continue
		}
		seen[k.Value] = true
		out = append(out, nodePair{k, v})
	}
	for _, p := range merged {
		if !seen[p.key.Value] {
			seen[p.key.Value] = true
			out = append(out, p)
		}
	}
	return out
}

func scalarText(n *yaml.Node) string {
	if n != nil && n.Kind == yaml.ScalarNode {
		return n.Value
	}
	return ""
}

func (w *inventoryWalker) top(root *yaml.Node) {
	for _, p := range w.pairs(root) {
		val := w.deref(p.value)
		switch p.key.Value {
		case "name":
			w.facts.Name = scalarText(val)
		case "variables":
			w.variables(val)
		case "instance_groups":
			w.instanceGroups(val)
		}
	}
}

func (w *inventoryWalker) variables(n *yaml.Node) {
	for _, item := range n.Content {
		m := w.deref(item)
		if m == nil || m.Kind != yaml.MappingNode {
			continue
		}
		var name, typ, ca string
		for _, p := range w.pairs(m) {
			v := w.deref(p.value)
			switch p.key.Value {
			case "name":
				name = scalarText(v)
			case "type":
				typ = scalarText(v)
			case "options":
				for _, o := range w.pairs(v) {
					if o.key.Value == "ca" {
						ca = scalarText(w.deref(o.value))
					}
				}
			}
		}
		if typ == "certificate" && name != "" {
			w.facts.CertVariables = append(w.facts.CertVariables, name)
			w.facts.SigningCAs[name] = ca
		}
	}
}

func (w *inventoryWalker) instanceGroups(n *yaml.Node) {
	for _, item := range n.Content {
		m := w.deref(item)
		group := ""
		for _, p := range w.pairs(m) {
			if p.key.Value == "name" {
				group = scalarText(w.deref(p.value))
			}
		}
		for _, p := range w.pairs(m) {
			v := w.deref(p.value)
			switch p.key.Value {
			case "instances":
				var count int
				if err := v.Decode(&count); err == nil {
					w.facts.Groups[group] = count
				}
			case "jobs":
				for _, job := range v.Content {
					for _, jp := range w.pairs(w.deref(job)) {
						if jp.key.Value == "properties" {
							w.facts.Properties, _ = w.value(jp.value).(map[string]any)
							w.refs(jp.value, "")
						}
					}
				}
			}
		}
	}
}

func (w *inventoryWalker) value(n *yaml.Node) any {
	n = w.deref(n)
	if n == nil || w.active[n] {
		return nil
	}
	switch n.Kind {
	case yaml.MappingNode:
		w.active[n] = true
		defer delete(w.active, n)
		out := map[string]any{}
		for _, p := range w.pairs(n) {
			out[p.key.Value] = w.value(p.value)
		}
		return out
	case yaml.SequenceNode:
		w.active[n] = true
		defer delete(w.active, n)
		out := make([]any, 0, len(n.Content))
		for _, item := range n.Content {
			out = append(out, w.value(item))
		}
		return out
	case yaml.ScalarNode:
		var v any
		if err := n.Decode(&v); err != nil {
			return n.Value
		}
		return v
	default:
		return nil
	}
}

func (w *inventoryWalker) refs(n *yaml.Node, path string) {
	n = w.deref(n)
	if n == nil || w.active[n] {
		return
	}
	switch n.Kind {
	case yaml.ScalarNode:
		if strings.HasPrefix(n.Value, "((") && strings.HasSuffix(n.Value, ".certificate))") {
			w.facts.CertRefs[path] = strings.TrimSuffix(strings.TrimPrefix(n.Value, "(("), ".certificate))")
		}
	case yaml.MappingNode:
		w.active[n] = true
		for _, p := range w.pairs(n) {
			key := p.key.Value
			if path != "" {
				key = path + "." + key
			}
			w.refs(p.value, key)
		}
		delete(w.active, n)
	default:
	}
}

func TestNodeWalkModeledOnCertInventory(t *testing.T) {
	doc := parseNode(t, certManifest)
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 {
		t.Fatalf("document = %+v", doc)
	}
	w := &inventoryWalker{
		facts:   inventoryFacts{SigningCAs: map[string]string{}, Groups: map[string]int{}, CertRefs: map[string]string{}},
		active:  map[*yaml.Node]bool{},
		merging: map[*yaml.Node]bool{},
	}
	root := w.deref(doc.Content[0])
	if root.Kind != yaml.MappingNode {
		t.Fatalf("root kind = %d", root.Kind)
	}
	w.top(root)

	want := inventoryFacts{
		Name:          "redis-x",
		CertVariables: []string{"redis_ca", "redis_tls"},
		SigningCAs:    map[string]string{"redis_ca": "", "redis_tls": "redis_ca"},
		Groups:        map[string]int{"redis": 3, "sentinel": 3},
		Properties: map[string]any{
			// The merged defaults decode through Node.Decode, so a plain
			// yes is the YAML 1.1 boolean and 6379 is an int.
			"enabled": true,
			"port":    6379,
			"tls":     map[string]any{"cert": "((redis_tls.certificate))"},
			"quoted":  "yes",
			"list":    []any{"a", 1, "on", nil, 1.5},
		},
		CertRefs: map[string]string{"tls.cert": "redis_tls"},
	}
	if !reflect.DeepEqual(w.facts, want) {
		t.Errorf("facts =\n%#v\nwant\n%#v", w.facts, want)
	}
}

func TestUnmarshalNodeDocumentAndTabPositions(t *testing.T) {
	// Positions from yaml.v3 v3.0.1 for the same documents. yaml.v3 puts
	// a document at its "---" or its first directive when it has one, and
	// counts a tab as one column.
	for _, c := range []struct {
		doc          string
		path         []int
		line, column int
	}{
		{"---\na: b\n", nil, 1, 1},
		{"# c\n\n---\na: b\n", nil, 3, 1},
		{"%YAML 1.1\n---\na: 1\n", nil, 1, 1},
		{"--- # c\n\n  a: b\n", nil, 1, 1},
		{"# head\n\na: b\n", nil, 3, 1},
		{"b:\ttabbed\n", []int{0, 1}, 1, 4},
		{"b: \ttabbed\n", []int{0, 1}, 1, 5},
		{"b:\t\ttabbed\n", []int{0, 1}, 1, 5},
		{"a:\n  b:\tc\n", []int{0, 1, 1}, 2, 6},
		{"{a:\tb}\n", []int{0, 1}, 1, 5},
		{"- [a,\t{b:\tc}]\n", []int{0, 0, 1}, 1, 7},
		{"- [a,\t{b:\tc}]\n", []int{0, 0, 1, 0}, 1, 8},
		{"- [a,\t{b:\tc}]\n", []int{0, 0, 1, 1}, 1, 11},
		{"k:\t|\n  x\ne:\tf\n", []int{0, 1}, 1, 4},
		{"k:\t|\n  x\ne:\tf\n", []int{0, 3}, 3, 4},
		{"c: ['\t',\td]\n", []int{0, 1, 0}, 1, 5},
		{"c: ['\t',\td]\n", []int{0, 1, 1}, 1, 10},
	} {
		n := parseNode(t, c.doc)
		for _, i := range c.path {
			n = n.Content[i]
		}
		if n.Line != c.line || n.Column != c.column {
			t.Errorf("%q at %v: %d:%d, want %d:%d", c.doc, c.path, n.Line, n.Column, c.line, c.column)
		}
	}
}

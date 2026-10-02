package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/gonvenience/bunt"
	"github.com/gonvenience/ytbx"
	"github.com/homeport/dyff/pkg/dyff"
	"github.com/mitchellh/hashstructure/v2"
	yaml "go.yaml.in/yaml/v3"
)

func engineVectors(root string, out outputs) error {
	bunt.SetColorSettings(bunt.OFF, bunt.OFF)
	out.json("internal/yamlnode/testdata/golden/tags.json", tagVectors())
	if err := nodeVectors(root, out); err != nil {
		return err
	}
	out.json("internal/yamlnode/testdata/golden/scalars.json", scalarShapeVectors())
	out.json("internal/yamlnode/testdata/golden/values.json", valueVectors())
	out.json("internal/yamlnode/testdata/golden/decode.json", decodeVectors())
	hashes, err := hashVectors()
	if err != nil {
		return err
	}
	out.json("internal/yamldiff/testdata/golden/hash.json", hashes)
	if err := compareVectors(root, out); err != nil {
		return err
	}
	return loadVectors(root, out)
}

// ---- tags ----------------------------------------------------------------

var fixedTagForms = []string{
	"yes", "Yes", "no", "on", "off", "y", "n", "true", "True", "TRUE", "false", "tRue",
	"0x10", "0X10", "0o17", "0O17", "017", "0b101", "+0x10", "-0x10", "-0b101", "-0o17",
	"1_000", "0x1_0", "1__0", "1_", "_1", "0x", "0xZZ", "0o8", "1e", "1.2.3", "1:20",
	"+1", "-1", "+0", "-0", "00", "9223372036854775807", "9223372036854775808",
	"08", "09.5", "18446744073709551615", "18446744073709551616", "-9223372036854775809",
	"1.0", "1.", ".5", "-.5", "+.5", "0.", "-0.0", "1e3", "1E3", "1e+3", "12e03",
	"1.5e-3", "1_0.5", ".inf", ".Inf", ".INF", "-.inf", "+.inf", ".nan", ".NaN", ".NAN",
	"inf", "nan", "2001-12-14", "2002-1-1", "2001-12-14T21:59:43Z",
	"2001-12-14t21:59:43.10-05:00", "2001-12-14 21:59:43.10 -5", "2001-12-14 21:59:43.10",
	"~", "null", "Null", "NULL", "nULL", "<<", "hello", "a b", "é",
}

type tagVector struct {
	In  string `json:"in"`
	Tag string `json:"tag"`
}

func tagVectors() []tagVector {
	forms := append([]string{}, fixedTagForms...)
	const alphabet = "019._+-exob:"
	var gen func(prefix string, depth int)
	gen = func(prefix string, depth int) {
		if depth == 0 {
			return
		}
		for _, r := range alphabet {
			s := prefix + string(r)
			forms = append(forms, s)
			gen(s, depth-1)
		}
	}
	gen("", 3)

	seen := map[string]bool{}
	var result []tagVector
	for _, f := range forms {
		if seen[f] {
			continue
		}
		seen[f] = true
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte("v: "+f+"\n"), &doc); err != nil {
			continue
		}
		if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode || len(doc.Content[0].Content) != 2 {
			continue
		}
		v := doc.Content[0].Content[1]
		if v.Kind != yaml.ScalarNode || v.Style != 0 || v.Value != f {
			continue
		}
		result = append(result, tagVector{In: f, Tag: v.Tag})
	}
	return result
}

// ---- nodes ---------------------------------------------------------------

type nodeVector struct {
	Input     string   `json:"input,omitempty"`
	Documents []*gNode `json:"documents"`
	Error     string   `json:"error,omitempty"`
}

func decodeAll(data []byte) ([]*gNode, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var docs []*gNode
	for {
		var n yaml.Node
		if err := dec.Decode(&n); err != nil {
			if errors.Is(err, io.EOF) {
				return docs, nil
			}
			return docs, err
		}
		docs = append(docs, dumpNode(&n))
	}
}

func nodeVectors(root string, out outputs) error {
	dir := filepath.Join(root, "internal/yamlnode/testdata/nodes")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !(strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		base := "internal/yamlnode/testdata/golden/nodes/" + strings.TrimSuffix(strings.TrimSuffix(name, ".yml"), ".yaml")
		out.json(base+".json", nodeVectorFor(data))
		// yaml.v3 mangles comment text on CRLF input, and graft does not
		// copy that, so a CRLF fixture also gets the golden of its LF form.
		if bytes.Contains(data, []byte("\r\n")) {
			out.json(base+".lf.json", nodeVectorFor(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))))
		}
	}
	return nil
}

func nodeVectorFor(data []byte) nodeVector {
	docs, err := decodeAll(data)
	v := nodeVector{Documents: docs}
	if err != nil {
		v.Error = err.Error()
	}
	return v
}

// ---- scalar shapes -------------------------------------------------------

// scalarShapeInputs generates block, quoted, and plain scalars in the
// shapes where a parser can silently disagree with libyaml: chomping,
// indentation indicators, trailing spaces and tabs, blank lines, more
// indented lines, folding, and the end of the stream.
func scalarShapeInputs() []string {
	headers := []string{"|", "|-", "|+", ">", ">-", ">+", "|2", ">2-"}
	bodies := [][]string{
		{"x"}, {"x "}, {"x\t"}, {"x", "y"}, {"x", "", "y"}, {"x ", ""}, {"  x", "y"}, {"x", "  y", "z"},
	}
	tails := []string{"\n", "\n\n", "\nnext: 1\n", "\n\nnext: 1\n", ""}
	var out []string
	for _, h := range headers {
		for _, body := range bodies {
			lines := make([]string, len(body))
			for i, l := range body {
				lines[i] = "  " + l
				if l == "" {
					lines[i] = ""
				}
			}
			for _, tail := range tails {
				out = append(out, "k: "+h+"\n"+strings.Join(lines, "\n")+tail)
			}
		}
	}
	out = append(out,
		"k: \"a\n  b\"\n", "k: \"a\n\n  b\"\n", "k: \"a \\\n  b\"\n", "k: \"\\x41\\u00e9\\t\"\n",
		"k: 'a\n  b'\n", "k: 'a\n\n  b'\n", "k: 'it''s'\n",
		"k: a\n  b\n", "k: a\n\n  b\n", "k: a  b \n", "k: a # c\n", "- a\n  b\n- c\n",
	)
	return out
}

func scalarShapeVectors() []nodeVector {
	var result []nodeVector
	for _, in := range scalarShapeInputs() {
		docs, err := decodeAll([]byte(in))
		v := nodeVector{Input: in, Documents: docs}
		if err != nil {
			v.Error = err.Error()
		}
		result = append(result, v)
	}
	return result
}

// ---- values --------------------------------------------------------------

type valueVector struct {
	Name  string `json:"name"`
	Node  *gNode `json:"node,omitempty"`
	Error string `json:"error,omitempty"`
	Panic string `json:"panic,omitempty"`
}

func marshalReparse(v interface{}) (vec valueVector) {
	defer func() {
		if r := recover(); r != nil {
			vec.Panic = fmt.Sprint(r)
		}
	}()
	b, err := yaml.Marshal(v)
	if err != nil {
		vec.Error = err.Error()
		return vec
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		vec.Error = "reparse: " + err.Error()
		return vec
	}
	vec.Node = dumpNode(&doc)
	return vec
}

func valueVectors() []valueVector {
	cat := valueCatalog()
	names := make([]string, 0, len(cat))
	for name := range cat {
		names = append(names, name)
	}
	sort.Strings(names)
	var result []valueVector
	for _, name := range names {
		vec := marshalReparse(cat[name])
		vec.Name = name
		result = append(result, vec)
	}
	return result
}

// ---- decode --------------------------------------------------------------

var decodeInputs = []string{
	"0x10", "017", "0o17", "1_000", "1e3", "08", "9223372036854775807", "9223372036854775808",
	"18446744073709551616", ".inf", "-.inf", "~", "", "true", "yes", "on", "2001-12-14",
	"2001-12-14t21:59:43.10-05:00", "2001-12-14T21:59:43Z", "!!binary aGVsbG8=",
	"!!str 1", "!!int x", "!!float 1", "!!bool yes", "!foo bar", "'1'", "\"x\"", "hello",
	"a: 1\nb: x\n",
	"1: a\nb: 2\n",
	"a: 1\na: 2\n",
	"base: &b {x: 1, y: 2}\nm:\n  <<: *b\n  y: 3\n",
	"a: &a {x: 1}\nb: &b {x: 2, y: 2}\nm:\n  <<: [*a, *b]\n  z: 3\n",
	"a: &x [1, 2]\nb: *x\n",
	"- 1\n- [2, 3]\n- {k: v}\n- ~\n",
	"'<<': 1\n",
	"? [a, b]\n: c\n",
}

// laughs builds a document whose aliases expand far past yaml.v3's limits.
func laughs() string {
	var b strings.Builder
	b.WriteString("a: &a [\"lol\",\"lol\",\"lol\",\"lol\",\"lol\",\"lol\",\"lol\",\"lol\",\"lol\"]\n")
	prev := "a"
	for _, name := range []string{"b", "c", "d", "e", "f", "g", "h", "i"} {
		fmt.Fprintf(&b, "%s: &%s [*%s,*%s,*%s,*%s,*%s,*%s,*%s,*%s,*%s]\n", name, name, prev, prev, prev, prev, prev, prev, prev, prev, prev)
		prev = name
	}
	return b.String()
}

type decodeVector struct {
	In    string `json:"in"`
	Value *typed `json:"value,omitempty"`
	Error string `json:"error,omitempty"`
}

func decodeVectors() []decodeVector {
	var result []decodeVector
	for _, in := range append(append([]string{}, decodeInputs...), laughs()) {
		vec := decodeVector{In: in}
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte(in), &doc); err != nil {
			vec.Error = err.Error()
			result = append(result, vec)
			continue
		}
		var v interface{}
		if err := doc.Decode(&v); err != nil {
			vec.Error = err.Error()
		} else {
			tv := toTyped(v)
			vec.Value = &tv
		}
		result = append(result, vec)
	}
	return result
}

// ---- hash ----------------------------------------------------------------

// These two functions are dyff v1.12.0's core.go:1051-1111 with the
// IgnoreOrderChanges branch removed, because graft never sets it.
// Copyright © 2019 The Homeport Team, MIT License.

func followAliasV3(n *yaml.Node) *yaml.Node {
	if n != nil && n.Alias != nil {
		return followAliasV3(n.Alias)
	}
	return n
}

func basicType(n *yaml.Node) interface{} {
	switch n.Kind {
	case yaml.MappingNode:
		result := map[interface{}]interface{}{}
		for i := 0; i < len(n.Content); i += 2 {
			result[basicType(followAliasV3(n.Content[i]))] = basicType(followAliasV3(n.Content[i+1]))
		}
		return result
	case yaml.SequenceNode:
		result := []interface{}{}
		for _, e := range n.Content {
			result = append(result, basicType(followAliasV3(e)))
		}
		return result
	case yaml.AliasNode:
		return basicType(n.Alias)
	default:
		return n.Value
	}
}

func calcNodeHash(n *yaml.Node) (uint64, error) {
	switch n.Kind {
	case yaml.MappingNode, yaml.SequenceNode:
		return hashstructure.Hash(basicType(n), hashstructure.FormatV2, nil)
	case yaml.ScalarNode:
		return hashstructure.Hash(n.Tag+"/"+n.Value, hashstructure.FormatV2, nil)
	case yaml.AliasNode:
		return calcNodeHash(followAliasV3(n))
	default:
		return 0, fmt.Errorf("kind %v is not supported", n.Kind)
	}
}

var hashInputs = []string{
	"- a\n- 1\n- '1'\n- ~\n- null\n- ''\n- true\n- 1.5\n",
	"- {a: 1}\n- {a: '1'}\n- {b: 1, a: 2}\n- {a: 2, b: 1}\n- {a: 1, a: 2}\n- {}\n",
	"- [1, [2, 3]]\n- []\n- [a, b]\n- [b, a]\n",
	"- &x {k: v}\n- *x\n- {k: v}\n",
	"- {l: [1, {m: n}]}\n- {1: a}\n- {'1': a}\n",
}

type hashVector struct {
	YAML   string   `json:"yaml"`
	Hashes []string `json:"hashes"`
}

func hashVectors() ([]hashVector, error) {
	var result []hashVector
	for _, in := range hashInputs {
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte(in), &doc); err != nil {
			return nil, err
		}
		vec := hashVector{YAML: in}
		for _, item := range doc.Content[0].Content {
			h, err := calcNodeHash(item)
			if err != nil {
				return nil, err
			}
			vec.Hashes = append(vec.Hashes, strconv.FormatUint(h, 10))
		}
		result = append(result, vec)
	}
	return result, nil
}

// ---- compare -------------------------------------------------------------

type gElem struct {
	Idx  int    `json:"idx"`
	Key  string `json:"key,omitempty"`
	Name string `json:"name,omitempty"`
}

type gDetail struct {
	Kind string `json:"kind"`
	From *gNode `json:"from,omitempty"`
	To   *gNode `json:"to,omitempty"`
}

type gDiff struct {
	NilPath     bool      `json:"nil_path,omitempty"`
	NilElements bool      `json:"nil_elements,omitempty"`
	Dot         string    `json:"dot"`
	GoPatch     string    `json:"gopatch"`
	Root        string    `json:"root"`
	DocumentIdx int       `json:"document_idx"`
	Elements    []gElem   `json:"elements,omitempty"`
	Details     []gDetail `json:"details"`
}

type gReport struct {
	FromDocuments int      `json:"from_documents"`
	FromNames     []string `json:"from_names,omitempty"`
	ToNames       []string `json:"to_names,omitempty"`
	Diffs         []gDiff  `json:"diffs"`
	Error         string   `json:"error,omitempty"`
}

func dumpReport(r dyff.Report) gReport {
	g := gReport{FromDocuments: len(r.From.Documents), FromNames: r.From.Names, ToNames: r.To.Names, Diffs: []gDiff{}}
	for _, d := range r.Diffs {
		gd := gDiff{Details: []gDetail{}}
		if d.Path == nil {
			gd.NilPath = true
		} else {
			gd.Dot = d.Path.ToDotStyle()
			gd.GoPatch = d.Path.ToGoPatchStyle()
			gd.Root = d.Path.RootDescription()
			gd.DocumentIdx = d.Path.DocumentIdx
			gd.NilElements = d.Path.PathElements == nil
			for _, e := range d.Path.PathElements {
				gd.Elements = append(gd.Elements, gElem{Idx: e.Idx, Key: e.Key, Name: e.Name})
			}
		}
		for _, det := range d.Details {
			gd.Details = append(gd.Details, gDetail{Kind: string(det.Kind), From: dumpNode(det.From), To: dumpNode(det.To)})
		}
		g.Diffs = append(g.Diffs, gd)
	}
	return g
}

func single(dir, prefix string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, prefix+".*"))
	if err != nil || len(matches) != 1 {
		return "", fmt.Errorf("%s: want exactly one %s.* file, found %v", dir, prefix, matches)
	}
	return filepath.Base(matches[0]), nil
}

func compareVectors(root string, out outputs) error {
	base := filepath.Join(root, "internal/yamldiff/testdata/compare")
	entries, err := os.ReadDir(base)
	if err != nil {
		return err
	}
	cwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(cwd) }()
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(base, e.Name())
		from, err := single(dir, "from")
		if err != nil {
			return err
		}
		to, err := single(dir, "to")
		if err != nil {
			return err
		}
		if err := os.Chdir(dir); err != nil {
			return err
		}
		var g gReport
		f, t, err := ytbx.LoadFiles(from, to)
		if err == nil {
			var r dyff.Report
			r, err = dyff.CompareInputFiles(f, t)
			g = dumpReport(r)
		}
		if err != nil {
			g = gReport{Diffs: []gDiff{}, Error: err.Error()}
		}
		out.json("internal/yamldiff/testdata/golden/compare/"+e.Name()+".json", g)
	}
	return nil
}

// ---- load ----------------------------------------------------------------

var loadLocations = []string{
	"testdata/load/inputs/comment.yml",
	"testdata/load/inputs/flow.yml",
	"testdata/load/inputs/j1.json",
	"testdata/load/inputs/j2.json",
	"testdata/load/inputs/j3.json",
	"testdata/load/inputs/t.toml",
	"testdata/load/inputs/two.yml",
	"testdata/load/inputs/empty.yml",
	"testdata/load/inputs/dashes.yml",
	"testdata/load/inputs/bad.json",
	"testdata/load/inputs/bad.yml",
	"testdata/load/inputs/dirA",
	"testdata/load/inputs/dirC",
	"testdata/load/inputs/nosuch.yml",
	"/nonexistent/graft-oracle/nosuch.yml",
}

type loadVector struct {
	Location  string   `json:"location"`
	Documents []*gNode `json:"documents,omitempty"`
	Names     []string `json:"names,omitempty"`
	Error     string   `json:"error,omitempty"`
}

func loadVectors(root string, out outputs) error {
	cwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(filepath.Join(root, "internal/yamldiff")); err != nil {
		return err
	}
	var result []loadVector
	for _, loc := range loadLocations {
		vec := loadVector{Location: loc}
		f, err := ytbx.LoadFile(loc)
		if err != nil {
			vec.Error = err.Error()
		} else {
			for _, d := range f.Documents {
				vec.Documents = append(vec.Documents, dumpNode(d))
			}
			vec.Names = f.Names
		}
		result = append(result, vec)
	}
	out.json("internal/yamldiff/testdata/golden/load.json", result)
	return nil
}

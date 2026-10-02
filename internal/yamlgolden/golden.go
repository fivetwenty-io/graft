// Package yamlgolden reads the golden files the oracle module in
// tests/diff-parity/oracle writes, and converts graft's yamlnode trees and
// decoded Go values into the same JSON shapes, so tests can compare graft
// with yaml.v3 and dyff field by field. Only tests import it.
package yamlgolden

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fivetwenty-io/graft/internal/yamlnode"
)

// Node is the JSON shape of one node, matching the oracle's gNode.
type Node struct {
	Kind        string  `json:"kind"`
	Tag         string  `json:"tag,omitempty"`
	Value       string  `json:"value,omitempty"`
	Anchor      string  `json:"anchor,omitempty"`
	Alias       string  `json:"alias,omitempty"`
	HeadComment string  `json:"head,omitempty"`
	LineComment string  `json:"line_comment,omitempty"`
	FootComment string  `json:"foot,omitempty"`
	Line        int     `json:"line,omitempty"`
	Content     []*Node `json:"content,omitempty"`
}

var kindNames = map[yamlnode.Kind]string{
	yamlnode.DocumentNode: "document",
	yamlnode.SequenceNode: "sequence",
	yamlnode.MappingNode:  "mapping",
	yamlnode.ScalarNode:   "scalar",
	yamlnode.AliasNode:    "alias",
}

// FromNode converts a yamlnode tree into the oracle's shape. An alias
// records its anchor name and stops, as the oracle does.
func FromNode(n *yamlnode.Node) *Node {
	if n == nil {
		return nil
	}
	g := &Node{
		Kind:        kindNames[n.Kind],
		Tag:         n.Tag,
		Value:       n.Value,
		Anchor:      n.Anchor,
		HeadComment: n.HeadComment,
		LineComment: n.LineComment,
		FootComment: n.FootComment,
		Line:        n.Line,
	}
	if n.Kind == yamlnode.AliasNode {
		g.Alias = n.Value
		return g
	}
	for _, c := range n.Content {
		g.Content = append(g.Content, FromNode(c))
	}
	return g
}

// Strip returns a deep copy of n that keeps comments only when
// keepComments is set and keeps Line only on mapping keys when
// keepKeyLines is set, so a test compares exactly the fields it owns.
func Strip(n *Node, keepComments, keepKeyLines bool) *Node {
	return strip(n, keepComments, keepKeyLines, false)
}

func strip(n *Node, keepComments, keepKeyLines, isKey bool) *Node {
	if n == nil {
		return nil
	}
	c := *n
	if !keepComments {
		c.HeadComment, c.LineComment, c.FootComment = "", "", ""
	}
	if !keepKeyLines || !isKey {
		c.Line = 0
	}
	c.Content = nil
	for i, child := range n.Content {
		c.Content = append(c.Content, strip(child, keepComments, keepKeyLines, n.Kind == "mapping" && i%2 == 0))
	}
	return &c
}

// Typed is the JSON shape of a decoded Go value, matching the oracle's
// typed: its %T, its text, and its items or sorted entries.
type Typed struct {
	T string     `json:"t"`
	V string     `json:"v,omitempty"`
	L []Typed    `json:"l,omitempty"`
	M [][2]Typed `json:"m,omitempty"`
}

// FromValue converts a decoded Go value into the oracle's typed shape.
func FromValue(v interface{}) Typed {
	t := Typed{T: fmt.Sprintf("%T", v)}
	switch x := v.(type) {
	case nil:
		t.T = "nil"
	case []interface{}:
		for _, item := range x {
			t.L = append(t.L, FromValue(item))
		}
	case map[string]interface{}:
		for k, item := range x {
			t.M = append(t.M, [2]Typed{FromValue(k), FromValue(item)})
		}
	case map[interface{}]interface{}:
		for k, item := range x {
			t.M = append(t.M, [2]Typed{FromValue(k), FromValue(item)})
		}
	case time.Time:
		t.V = x.Format(time.RFC3339Nano) + "|" + x.Location().String()
	case float64:
		t.V = strconv.FormatFloat(x, 'g', -1, 64)
	default:
		t.V = fmt.Sprintf("%v", x)
	}
	sort.Slice(t.M, func(i, j int) bool {
		return t.M[i][0].T+"\x00"+t.M[i][0].V < t.M[j][0].T+"\x00"+t.M[j][0].V
	})
	return t
}

// ReadJSON decodes the JSON file at path into v and fails the test when
// the file is missing or malformed.
func ReadJSON(t testing.TB, path string, v interface{}) {
	t.Helper()
	// #nosec G304 -- the path comes from the calling test's own testdata
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v (regenerate it with make oracle-vectors)", path, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("decoding %s: %v", path, err)
	}
}

// Diff marshals want and got as indented JSON and describes the first
// line where they differ, or returns "" when they are equal.
func Diff(want, got interface{}) string {
	w, _ := json.MarshalIndent(want, "", "  ")
	g, _ := json.MarshalIndent(got, "", "  ")
	if bytes.Equal(w, g) {
		return ""
	}
	wl, gl := strings.Split(string(w), "\n"), strings.Split(string(g), "\n")
	for i := 0; i < len(wl) || i < len(gl); i++ {
		var a, b string
		if i < len(wl) {
			a = wl[i]
		}
		if i < len(gl) {
			b = gl[i]
		}
		if a != b {
			return fmt.Sprintf("first difference at JSON line %d:\n  want %s\n  got  %s", i+1, a, b)
		}
	}
	return "values differ"
}

// PinLocal sets time.Local to a fixed UTC-4 zone for the rest of the test,
// matching the oracle, and restores the previous zone afterwards.
func PinLocal(t testing.TB) {
	t.Helper()
	prev := time.Local
	time.Local = time.FixedZone("UTC-4", -4*60*60)
	t.Cleanup(func() { time.Local = prev })
}

// LineErrorPrefix cuts msg just after its first "yaml: line N: ", keeping
// everything before that point. That is the part of a parse error graft
// matches spruce on when libyaml and goccy agree on the line. It reports
// false when msg holds no such prefix.
func LineErrorPrefix(msg string) (string, bool) {
	const marker = "yaml: line "
	i := strings.Index(msg, marker)
	if i < 0 {
		return "", false
	}
	rest := msg[i+len(marker):]
	j := strings.Index(rest, ": ")
	if j <= 0 || strings.Trim(rest[:j], "0123456789") != "" {
		return "", false
	}
	return msg[:i+len(marker)+j+2], true
}

// OnlyMatch returns the path of the one file in dir whose name is prefix
// followed by a dot and an extension, such as from.yml or to.json, and
// fails the test unless exactly one file matches.
func OnlyMatch(t testing.TB, dir, prefix string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, prefix+".*"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("%s: want one %s.* file, found %v", dir, prefix, matches)
	}
	return matches[0]
}

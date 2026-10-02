package main

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	yaml "go.yaml.in/yaml/v3"
)

// gNode is the JSON shape of one yaml.v3 node. graft's
// internal/yamlgolden package defines the same shape for yamlnode trees.
type gNode struct {
	Kind        string   `json:"kind"`
	Tag         string   `json:"tag,omitempty"`
	Value       string   `json:"value,omitempty"`
	Anchor      string   `json:"anchor,omitempty"`
	Alias       string   `json:"alias,omitempty"`
	HeadComment string   `json:"head,omitempty"`
	LineComment string   `json:"line_comment,omitempty"`
	FootComment string   `json:"foot,omitempty"`
	Line        int      `json:"line,omitempty"`
	Content     []*gNode `json:"content,omitempty"`
}

var kindNames = map[yaml.Kind]string{
	yaml.DocumentNode: "document",
	yaml.SequenceNode: "sequence",
	yaml.MappingNode:  "mapping",
	yaml.ScalarNode:   "scalar",
	yaml.AliasNode:    "alias",
}

func dumpNode(n *yaml.Node) *gNode {
	if n == nil {
		return nil
	}
	g := &gNode{
		Kind:        kindNames[n.Kind],
		Tag:         n.Tag,
		Value:       n.Value,
		Anchor:      n.Anchor,
		HeadComment: n.HeadComment,
		LineComment: n.LineComment,
		FootComment: n.FootComment,
		Line:        n.Line,
	}
	if n.Kind == yaml.AliasNode {
		g.Alias = n.Value
		return g
	}
	for _, c := range n.Content {
		g.Content = append(g.Content, dumpNode(c))
	}
	return g
}

// typed is the JSON shape of a decoded Go value: its %T, its text, and
// its items or entries. graft's internal/yamlgolden defines the same shape.
type typed struct {
	T string     `json:"t"`
	V string     `json:"v,omitempty"`
	L []typed    `json:"l,omitempty"`
	M [][2]typed `json:"m,omitempty"`
}

func toTyped(v interface{}) typed {
	t := typed{T: fmt.Sprintf("%T", v)}
	switch x := v.(type) {
	case nil:
		t.T = "nil"
	case []interface{}:
		for _, item := range x {
			t.L = append(t.L, toTyped(item))
		}
	case map[string]interface{}:
		for k, item := range x {
			t.M = append(t.M, [2]typed{toTyped(k), toTyped(item)})
		}
	case map[interface{}]interface{}:
		for k, item := range x {
			t.M = append(t.M, [2]typed{toTyped(k), toTyped(item)})
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

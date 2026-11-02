package yaml

import (
	"strconv"

	"github.com/fivetwenty-io/graft/internal/yamlnode"
)

// collapseDuplicates walks the tree under root in document order. For
// each later occurrence of a scalar key in a mapping it calls report,
// and it drops the later pair after moving its value into the first
// pair, so the mapping keeps one pair for the key, at the first key's
// place, holding the last value. Merge keys, and keys that are aliases
// or collections, are left alone. A node reached a second time, through
// an alias, is not walked again.
func collapseDuplicates(root *Node, report func(DuplicateKey)) {
	w := nodeDupWalker{report: report, seen: make(map[*Node]bool)}
	w.walk(root, "")
}

type nodeDupWalker struct {
	report func(DuplicateKey)
	seen   map[*Node]bool
}

func (w nodeDupWalker) walk(n *Node, path string) {
	if n == nil || w.seen[n] {
		return
	}
	w.seen[n] = true
	switch n.Kind {
	case DocumentNode:
		for _, child := range n.Content {
			w.walk(child, path)
		}
	case SequenceNode:
		for i, item := range n.Content {
			w.walk(item, path+"["+strconv.Itoa(i)+"]")
		}
	case MappingNode:
		w.mapping(n, path)
	case ScalarNode, AliasNode:
	}
}

// mapping walks the pairs of one mapping node, and rebuilds its Content
// without the later pairs of a repeated key. A key repeats an earlier
// one as yamlnode.KeyID describes, and a later pair's value moves into
// the last pair kept whose key reads as the same key in a map, so that
// a decode of the mapping keeps that value.
func (w nodeDupWalker) mapping(n *Node, path string) {
	var keys yamlnode.KeySet[*Node]
	lastAt := make(map[string]int, len(n.Content)/2)
	kept := make([]*Node, 0, len(n.Content))
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, val := n.Content[i], n.Content[i+1]
		child := path
		if key != nil && key.Kind == ScalarNode && key.Tag != yamlnode.TagMerge {
			if child != "" {
				child += "."
			}
			child += key.Value
			id := yamlnode.KeyID{Text: key.Value, Null: yamlnode.ShortTag(key.Tag) == yamlnode.TagNull}
			if first, repeat := keys.Add(id, key); repeat {
				w.report(DuplicateKey{Path: child, Line: key.Line, Column: key.Column, FirstLine: first.Line, FirstColumn: first.Column})
				w.walk(val, child)
				kept[lastAt[id.Slot()]+1] = val
				continue
			}
			lastAt[id.Slot()] = len(kept)
		}
		w.walk(val, child)
		kept = append(kept, key, val)
	}
	if len(n.Content)%2 == 1 {
		kept = append(kept, n.Content[len(n.Content)-1])
	}
	n.Content = kept
}

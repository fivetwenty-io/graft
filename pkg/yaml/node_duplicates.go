package yaml

import "strconv"

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
// without the later pairs of a repeated key.
func (w nodeDupWalker) mapping(n *Node, path string) {
	firstAt := make(map[string]int, len(n.Content)/2)
	kept := make([]*Node, 0, len(n.Content))
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, val := n.Content[i], n.Content[i+1]
		child := path
		if key != nil && key.Kind == ScalarNode && key.Tag != "!!merge" {
			if child != "" {
				child += "."
			}
			child += key.Value
			if at, seen := firstAt[key.Value]; seen {
				first := kept[at]
				w.report(DuplicateKey{Path: child, Line: key.Line, Column: key.Column, FirstLine: first.Line, FirstColumn: first.Column})
				w.walk(val, child)
				kept[at+1] = val
				continue
			}
			firstAt[key.Value] = len(kept)
		}
		w.walk(val, child)
		kept = append(kept, key, val)
	}
	if len(n.Content)%2 == 1 {
		kept = append(kept, n.Content[len(n.Content)-1])
	}
	n.Content = kept
}

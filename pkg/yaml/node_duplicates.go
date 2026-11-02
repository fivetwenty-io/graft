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
// a decode of the mapping keeps that value. Merge keys are never
// reported. The first one keeps its place, and its value becomes a
// sequence of the sources of every merge key in the mapping, in order,
// which a decode merges as it would merge each in turn, so a key in a
// later source wins.
func (w nodeDupWalker) mapping(n *Node, path string) {
	var keys yamlnode.KeySet[*Node]
	lastAt := make(map[string]int, len(n.Content)/2)
	mergeAt := -1
	var sources *Node
	kept := make([]*Node, 0, len(n.Content))
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, val := n.Content[i], n.Content[i+1]
		if key != nil && key.Kind == ScalarNode && key.Tag == yamlnode.TagMerge {
			w.walk(val, path)
			if mergeAt >= 0 {
				if sources == nil {
					sources = mergeSequence(kept[mergeAt+1])
					kept[mergeAt+1] = sources
				}
				sources.Content = append(sources.Content, sourcesOf(val)...)
				continue
			}
			mergeAt = len(kept)
			kept = append(kept, key, val)
			continue
		}
		child := path
		if key != nil && key.Kind == ScalarNode {
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

// mergeSequence returns a flow sequence at first's place of the merge
// sources that first, the value of a merge key, names. A merge key with
// that sequence as its value merges the sources in turn, as the merge
// keys that named them would, and the sources of a later merge key are
// appended to it.
func mergeSequence(first *Node) *Node {
	out := &Node{
		Kind: SequenceNode, Tag: yamlnode.TagSeq, Line: first.Line, Column: first.Column,
		style: yamlnode.StyleFlow, text: first.text, opts: first.opts,
	}
	out.Content = append(out.Content, sourcesOf(first)...)
	return out
}

// sourcesOf returns the merge sources n, the value of a merge key, names.
func sourcesOf(n *Node) []*Node {
	switch {
	case n.Kind == SequenceNode:
		return n.Content
	case n.Kind == AliasNode && n.Alias != nil && n.Alias.Kind == SequenceNode:
		return n.Alias.Content
	}
	return []*Node{n}
}

// Package sortnode sorts the mappings of a YAML node tree in place. It
// lives apart from pkg/sortkeys so that the public package exposes no
// yamlnode type.
package sortnode

import (
	"sort"

	"github.com/fivetwenty-io/graft/internal/yamlnode"
	"github.com/fivetwenty-io/graft/pkg/sortkeys"
)

// Sort stably sorts the keys of every mapping under n, comments and all,
// by the scalar text of each key. None leaves the tree alone. Aliases are
// not followed, since the anchored node is sorted where it is defined.
func Sort(n *yamlnode.Node, m sortkeys.Mode) {
	less := sortkeys.Less(m)
	if less == nil || n == nil {
		return
	}
	sortNode(n, less)
}

func sortNode(n *yamlnode.Node, less func(a, b string) bool) {
	for _, c := range n.Content {
		sortNode(c, less)
	}
	if n.Kind != yamlnode.MappingNode {
		return
	}
	pairs := len(n.Content) / 2
	order := make([]int, pairs)
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		return less(n.Content[2*order[i]].Value, n.Content[2*order[j]].Value)
	})
	out := make([]*yamlnode.Node, 0, len(n.Content))
	for _, at := range order {
		out = append(out, n.Content[2*at], n.Content[2*at+1])
	}
	copy(n.Content, out)
}

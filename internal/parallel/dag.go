// Package parallel provides parallelization primitives for concurrent task execution.
package parallel

import (
	"bytes"
	"fmt"
	"sort"
	"sync"
)

// Dead code, kept deliberately (see the C9a dependency-graph-and-eval-plan
// cluster notes in the graft library-API plan): DAG has zero production
// callers anywhere in this module. NewDAG is constructed only from this
// package's own tests (parallel_test.go), GetWaves has only test callers
// (including BenchmarkDAG_GetWaves), and the live parallel evaluation
// path (Scheduler, in scheduler.go - the type runOpsWithScheduler and the
// public graft.BuildEvalPlan both actually build and schedule) never
// references DAG at all. Do not build new public API on top of this type
// under the assumption it is an exercised implementation: it revives
// untested-in-production code rather than reusing a working one. Deletion
// was considered and deliberately deferred rather than done as a side
// effect of an unrelated cluster - see the C9a work notes for the
// reachability evidence and the reasoning for not deleting it in that
// pass.

// Node represents a node in the dependency graph.
type Node struct {
	// ID uniquely identifies this node
	ID string

	// Dependencies are IDs of nodes this node depends on (must complete before this node)
	Dependencies []string

	// Data holds arbitrary data associated with this node
	Data interface{}

	// Priority is an optional priority hint (higher = more important)
	Priority int
}

// DAG represents a directed acyclic graph for dependency tracking.
type DAG struct {
	mu    sync.RWMutex
	nodes map[string]*dagNode

	// cached results
	sortedCache []string
	wavesCache  [][]string
	cacheValid  bool
}

// dagNode is an internal representation with computed fields.
type dagNode struct {
	*Node
	dependents []string // nodes that depend on this node
	inDegree   int      // number of unprocessed dependencies
}

// NewDAG creates a new empty DAG.
func NewDAG() *DAG {
	return &DAG{
		nodes: make(map[string]*dagNode),
	}
}

// AddNode adds a node to the DAG.
// If a node with the same ID exists, it will be replaced.
func (d *DAG) AddNode(node *Node) {
	if node == nil || node.ID == "" {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	d.invalidateCache()

	// Create internal node
	internal := &dagNode{
		Node:       node,
		dependents: make([]string, 0),
		inDegree:   len(node.Dependencies),
	}

	// If replacing an existing node, clean up old dependents references
	if old, exists := d.nodes[node.ID]; exists {
		for _, depID := range old.Dependencies {
			if dep, ok := d.nodes[depID]; ok {
				dep.dependents = removeString(dep.dependents, node.ID)
			}
		}
	}

	d.nodes[node.ID] = internal

	// Update dependents for all dependencies
	for _, depID := range node.Dependencies {
		if dep, exists := d.nodes[depID]; exists {
			dep.dependents = append(dep.dependents, node.ID)
		}
	}
}

// AddEdge adds a dependency edge from one node to another.
// This means "from" depends on "to" (to must complete before from).
func (d *DAG) AddEdge(from, to string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	fromNode, fromExists := d.nodes[from]
	toNode, toExists := d.nodes[to]

	if !fromExists {
		return fmt.Errorf("node %q does not exist", from)
	}
	if !toExists {
		return fmt.Errorf("node %q does not exist", to)
	}

	// Check if edge already exists
	for _, dep := range fromNode.Dependencies {
		if dep == to {
			return nil // Edge already exists
		}
	}

	d.invalidateCache()

	// Add the dependency
	fromNode.Dependencies = append(fromNode.Dependencies, to)
	fromNode.inDegree++
	toNode.dependents = append(toNode.dependents, from)

	return nil
}

// RemoveNode removes a node from the DAG.
func (d *DAG) RemoveNode(id string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	node, exists := d.nodes[id]
	if !exists {
		return
	}

	d.invalidateCache()

	// Remove this node from its dependencies' dependents list
	for _, depID := range node.Dependencies {
		if dep, ok := d.nodes[depID]; ok {
			dep.dependents = removeString(dep.dependents, id)
		}
	}

	// Remove this node from its dependents' dependencies list
	for _, depID := range node.dependents {
		if dep, ok := d.nodes[depID]; ok {
			dep.Dependencies = removeString(dep.Dependencies, id)
			dep.inDegree--
		}
	}

	delete(d.nodes, id)
}

// GetNode returns a node by ID.
func (d *DAG) GetNode(id string) (*Node, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if node, exists := d.nodes[id]; exists {
		return node.Node, true
	}
	return nil, false
}

// GetDependencies returns the IDs of nodes that the given node depends on.
func (d *DAG) GetDependencies(id string) []string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if node, exists := d.nodes[id]; exists {
		result := make([]string, len(node.Dependencies))
		copy(result, node.Dependencies)
		return result
	}
	return nil
}

// GetDependents returns the IDs of nodes that depend on the given node.
func (d *DAG) GetDependents(id string) []string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if node, exists := d.nodes[id]; exists {
		result := make([]string, len(node.dependents))
		copy(result, node.dependents)
		return result
	}
	return nil
}

// Size returns the number of nodes in the DAG.
func (d *DAG) Size() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.nodes)
}

// HasCycle returns true if the DAG contains a cycle.
func (d *DAG) HasCycle() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	visited := make(map[string]bool)
	inStack := make(map[string]bool)

	var hasCycle bool
	var dfs func(id string) bool
	dfs = func(id string) bool {
		if inStack[id] {
			return true // Cycle detected
		}
		if visited[id] {
			return false
		}

		visited[id] = true
		inStack[id] = true

		if node, exists := d.nodes[id]; exists {
			for _, depID := range node.Dependencies {
				if dfs(depID) {
					return true
				}
			}
		}

		inStack[id] = false
		return false
	}

	for id := range d.nodes {
		if !visited[id] {
			if dfs(id) {
				hasCycle = true
				break
			}
		}
	}

	return hasCycle
}

// TopologicalSort returns a topological ordering of the DAG.
// Returns an error if the graph contains a cycle.
func (d *DAG) TopologicalSort() ([]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.cacheValid && d.sortedCache != nil {
		result := make([]string, len(d.sortedCache))
		copy(result, d.sortedCache)
		return result, nil
	}

	// Kahn's algorithm
	inDegree := make(map[string]int)
	for id, node := range d.nodes {
		inDegree[id] = node.inDegree
	}

	// Find all nodes with no dependencies
	queue := make([]string, 0)
	for id, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}

	// Sort initial queue by priority (higher priority first)
	sort.Slice(queue, func(i, j int) bool {
		return d.nodes[queue[i]].Priority > d.nodes[queue[j]].Priority
	})

	result := make([]string, 0, len(d.nodes))

	for len(queue) > 0 {
		// Pop from queue
		id := queue[0]
		queue = queue[1:]
		result = append(result, id)

		// Reduce in-degree for all dependents
		if node, exists := d.nodes[id]; exists {
			newReady := make([]string, 0)
			for _, depID := range node.dependents {
				inDegree[depID]--
				if inDegree[depID] == 0 {
					newReady = append(newReady, depID)
				}
			}

			// Sort new ready nodes by priority
			sort.Slice(newReady, func(i, j int) bool {
				return d.nodes[newReady[i]].Priority > d.nodes[newReady[j]].Priority
			})
			queue = append(queue, newReady...)
		}
	}

	if len(result) != len(d.nodes) {
		return nil, fmt.Errorf("cycle detected: processed %d nodes out of %d", len(result), len(d.nodes))
	}

	// Cache the result
	d.sortedCache = make([]string, len(result))
	copy(d.sortedCache, result)
	d.cacheValid = true

	return result, nil
}

// GetWaves returns nodes grouped into parallel execution waves.
// Each wave contains nodes that can be executed concurrently.
// Waves must be executed in order (wave N+1 can only start after wave N completes).
func (d *DAG) GetWaves() ([][]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.cacheValid && d.wavesCache != nil {
		result := make([][]string, len(d.wavesCache))
		for i, wave := range d.wavesCache {
			result[i] = make([]string, len(wave))
			copy(result[i], wave)
		}
		return result, nil
	}

	// Build temporary in-degree map
	inDegree := make(map[string]int)
	for id, node := range d.nodes {
		inDegree[id] = node.inDegree
	}

	waves := make([][]string, 0)
	remaining := len(d.nodes)

	for remaining > 0 {
		// Find all nodes with no remaining dependencies
		wave := make([]string, 0)
		for id, degree := range inDegree {
			if degree == 0 {
				wave = append(wave, id)
			}
		}

		if len(wave) == 0 {
			return nil, fmt.Errorf("cycle detected: %d nodes remaining but none ready", remaining)
		}

		// Sort wave by priority (higher priority first)
		sort.Slice(wave, func(i, j int) bool {
			return d.nodes[wave[i]].Priority > d.nodes[wave[j]].Priority
		})

		// Remove processed nodes and update dependents
		for _, id := range wave {
			delete(inDegree, id)
			remaining--

			if node, exists := d.nodes[id]; exists {
				for _, depID := range node.dependents {
					if _, stillExists := inDegree[depID]; stillExists {
						inDegree[depID]--
					}
				}
			}
		}

		waves = append(waves, wave)
	}

	// Cache the result
	d.wavesCache = make([][]string, len(waves))
	for i, wave := range waves {
		d.wavesCache[i] = make([]string, len(wave))
		copy(d.wavesCache[i], wave)
	}
	d.cacheValid = true

	return waves, nil
}

// GetAncestors returns all nodes that the given node transitively depends on.
func (d *DAG) GetAncestors(id string) []string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	visited := make(map[string]bool)
	var result []string

	var visit func(nodeID string)
	visit = func(nodeID string) {
		if node, exists := d.nodes[nodeID]; exists {
			for _, depID := range node.Dependencies {
				if !visited[depID] {
					visited[depID] = true
					result = append(result, depID)
					visit(depID)
				}
			}
		}
	}

	visit(id)
	return result
}

// GetDescendants returns all nodes that transitively depend on the given node.
func (d *DAG) GetDescendants(id string) []string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	visited := make(map[string]bool)
	var result []string

	var visit func(nodeID string)
	visit = func(nodeID string) {
		if node, exists := d.nodes[nodeID]; exists {
			for _, depID := range node.dependents {
				if !visited[depID] {
					visited[depID] = true
					result = append(result, depID)
					visit(depID)
				}
			}
		}
	}

	visit(id)
	return result
}

// ToDOT returns a DOT format representation of the DAG for visualization.
func (d *DAG) ToDOT(name string) string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("digraph %s {\n", name))
	buf.WriteString("  rankdir=TB;\n")
	buf.WriteString("  node [shape=box];\n\n")

	// Sort node IDs for deterministic output
	ids := make([]string, 0, len(d.nodes))
	for id := range d.nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	// Write nodes
	for _, id := range ids {
		node := d.nodes[id]
		label := id
		if node.Priority != 0 {
			label = fmt.Sprintf("%s\\n(priority: %d)", id, node.Priority)
		}
		buf.WriteString(fmt.Sprintf("  %q [label=%q];\n", id, label))
	}

	buf.WriteString("\n")

	// Write edges
	for _, id := range ids {
		node := d.nodes[id]
		for _, depID := range node.Dependencies {
			buf.WriteString(fmt.Sprintf("  %q -> %q;\n", depID, id))
		}
	}

	buf.WriteString("}\n")
	return buf.String()
}

// Clear removes all nodes from the DAG.
func (d *DAG) Clear() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.nodes = make(map[string]*dagNode)
	d.invalidateCache()
}

// Clone creates a deep copy of the DAG.
func (d *DAG) Clone() *DAG {
	d.mu.RLock()
	defer d.mu.RUnlock()

	clone := NewDAG()
	for id, node := range d.nodes {
		deps := make([]string, len(node.Dependencies))
		copy(deps, node.Dependencies)

		clone.nodes[id] = &dagNode{
			Node: &Node{
				ID:           node.ID,
				Dependencies: deps,
				Data:         node.Data,
				Priority:     node.Priority,
			},
			dependents: make([]string, len(node.dependents)),
			inDegree:   node.inDegree,
		}
		copy(clone.nodes[id].dependents, node.dependents)
	}

	return clone
}

// invalidateCache invalidates the cached results.
func (d *DAG) invalidateCache() {
	d.sortedCache = nil
	d.wavesCache = nil
	d.cacheValid = false
}

// removeString removes a string from a slice.
func removeString(slice []string, s string) []string {
	result := make([]string, 0, len(slice))
	for _, item := range slice {
		if item != s {
			result = append(result, item)
		}
	}
	return result
}

package graft

import (
	"context"
	"fmt"
	"sync"

	"gopkg.in/yaml.v3"
)

// COWEvaluator is a thread-safe evaluator using Copy-on-Write trees.
type COWEvaluator struct {
	cowTree ThreadSafeTree
	mu      sync.RWMutex
}

// NewCOWEvaluator creates a new COW-based evaluator.
func NewCOWEvaluator(data map[string]interface{}) *COWEvaluator {
	cowTree := NewCOWTree(data)
	return &COWEvaluator{
		cowTree: cowTree,
	}
}

// Evaluate performs thread-safe evaluation using COW semantics.
// It exports the COW tree data, runs it through the standard engine
// evaluation pipeline, and imports the results back on success.
// On error the original tree data is preserved.
func (ce *COWEvaluator) Evaluate(ctx context.Context) error {
	ce.mu.Lock()
	defer ce.mu.Unlock()

	// Extract current tree data
	cowTree, ok := ce.cowTree.(*COWTree)
	if !ok {
		return fmt.Errorf("unsupported tree type for evaluation")
	}

	data := cowTree.toStringMap()

	// Short-circuit: nothing to evaluate in an empty tree
	if len(data) == 0 {
		return nil
	}

	// Check context cancellation before doing work
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Marshal tree data to YAML so the engine can parse and evaluate it.
	// This round-trip ensures the engine sees the data in its expected format
	// with operator expressions as string values ready for resolution.
	yamlBytes, err := yaml.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal tree data: %w", err)
	}

	// Create an engine with external services skipped (local data only)
	engine, err := NewEngine(
		WithSkipVault(true),
		WithSkipAws(true),
		WithSkipNats(true),
	)
	if err != nil {
		return fmt.Errorf("failed to create evaluation engine: %w", err)
	}

	// Parse the YAML into a Document the engine understands
	doc, err := engine.ParseYAML(yamlBytes)
	if err != nil {
		return fmt.Errorf("failed to parse tree data for evaluation: %w", err)
	}
	if doc == nil {
		return nil
	}

	// Run evaluation through the standard pipeline
	result, err := engine.Evaluate(ctx, doc)
	if err != nil {
		// On error, the original tree is preserved (we never replaced it)
		return err
	}

	// Extract evaluated data and replace the COW tree
	resultData, ok := result.RawData().(map[string]interface{})
	if !ok {
		return fmt.Errorf("evaluation result is not a map")
	}

	ce.cowTree = NewCOWTree(resultData)
	return nil
}

// GetTree returns the underlying COW tree.
func (ce *COWEvaluator) GetTree() ThreadSafeTree {
	ce.mu.RLock()
	defer ce.mu.RUnlock()
	return ce.cowTree
}

// SetValue safely sets a value using COW semantics.
func (ce *COWEvaluator) SetValue(value interface{}, path ...string) error {
	return ce.cowTree.Set(joinPath(path...), value)
}

// GetValue safely gets a value using COW semantics.
func (ce *COWEvaluator) GetValue(path ...string) (interface{}, error) {
	return ce.cowTree.Get(joinPath(path...))
}

// CreateSnapshot creates a lightweight snapshot of the current state.
func (ce *COWEvaluator) CreateSnapshot() *COWEvaluator {
	ce.mu.RLock()
	defer ce.mu.RUnlock()

	// COW copy is very fast - just shares the root
	snapshot := ce.cowTree.Copy()

	return &COWEvaluator{
		cowTree: snapshot,
	}
}

// GetVersion returns the current version of the tree.
func (ce *COWEvaluator) GetVersion() int64 {
	if cowTree, ok := ce.cowTree.(*COWTree); ok {
		return cowTree.GetVersion()
	}
	return 0
}

// EnhancedMigrationHelper provides COW-aware migration utilities.
type EnhancedMigrationHelper struct {
	cowTree    ThreadSafeTree
	cowEval    *COWEvaluator
	snapshots  []*COWEvaluator
	snapshotMu sync.RWMutex
}

// NewEnhancedMigrationHelper creates a COW-aware migration helper.
func NewEnhancedMigrationHelper(data map[string]interface{}) *EnhancedMigrationHelper {
	cowTree := NewCOWTree(data)
	cowEval := &COWEvaluator{cowTree: cowTree}

	return &EnhancedMigrationHelper{
		cowTree:   cowTree,
		cowEval:   cowEval,
		snapshots: make([]*COWEvaluator, 0),
	}
}

// GetCOWEvaluator returns the COW-based evaluator.
func (emh *EnhancedMigrationHelper) GetCOWEvaluator() *COWEvaluator {
	return emh.cowEval
}

// GetThreadSafeTree returns the COW tree.
func (emh *EnhancedMigrationHelper) GetThreadSafeTree() ThreadSafeTree {
	return emh.cowTree
}

// CreateSnapshot creates a snapshot for point-in-time access.
func (emh *EnhancedMigrationHelper) CreateSnapshot() *COWEvaluator {
	emh.snapshotMu.Lock()
	defer emh.snapshotMu.Unlock()

	snapshot := emh.cowEval.CreateSnapshot()
	emh.snapshots = append(emh.snapshots, snapshot)

	return snapshot
}

// GetSnapshots returns all created snapshots.
func (emh *EnhancedMigrationHelper) GetSnapshots() []*COWEvaluator {
	emh.snapshotMu.RLock()
	defer emh.snapshotMu.RUnlock()

	// Return a copy of the snapshots slice
	result := make([]*COWEvaluator, len(emh.snapshots))
	copy(result, emh.snapshots)
	return result
}

// UpdateFromEvaluator updates the COW tree with data from a traditional evaluator.
func (emh *EnhancedMigrationHelper) UpdateFromEvaluator(ev *Evaluator) error {
	// Convert the evaluator's tree to interface{} map
	data := make(map[string]interface{})
	for k, v := range ev.Tree {
		data[k] = v
	}

	// Replace the entire COW tree with new data
	emh.cowTree = NewCOWTree(data)

	// Re-wrap with COWEvaluator
	emh.cowEval = &COWEvaluator{
		cowTree: emh.cowTree,
	}

	return nil
}

// ExportToEvaluator creates a traditional evaluator with current COW tree data.
func (emh *EnhancedMigrationHelper) ExportToEvaluator() (*Evaluator, error) {
	if cowTree, ok := emh.cowTree.(*COWTree); ok {
		rawData := cowTree.toStringMap()

		ev := &Evaluator{
			Tree:     rawData,
			SkipEval: false,
			CheckOps: make([]*Opcall, 0),
			Only:     []string{},
		}

		return ev, nil
	}

	return nil, fmt.Errorf("unsupported tree type for export")
}

// COWTreeFactory provides utilities for creating different types of COW trees.
type COWTreeFactory struct{}

// NewCOWTreeFactory creates a new factory.
func NewCOWTreeFactory() *COWTreeFactory {
	return &COWTreeFactory{}
}

// CreateFromData creates a COW tree from data.
func (f *COWTreeFactory) CreateFromData(data map[string]interface{}) ThreadSafeTree {
	return NewCOWTree(data)
}

// CreateEmpty creates an empty COW tree.
func (f *COWTreeFactory) CreateEmpty() ThreadSafeTree {
	return NewCOWTree(nil)
}

// CreateFromYAML creates a COW tree from YAML data.
func (f *COWTreeFactory) CreateFromYAML(yamlData []byte) (ThreadSafeTree, error) {
	if len(yamlData) == 0 {
		return NewCOWTree(nil), nil
	}

	var raw interface{}
	if err := yaml.Unmarshal(yamlData, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	if raw == nil {
		return NewCOWTree(nil), nil
	}

	data, ok := raw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("root of YAML document is not a hash/map")
	}

	converted := DefaultYAMLCompat().ConvertMapValues(data)
	return NewCOWTree(converted), nil
}

// COWPerformanceMonitor tracks performance metrics for COW operations.
type COWPerformanceMonitor struct {
	copyCount   int64
	modifyCount int64
	sharedNodes int64
	clonedNodes int64
	mu          sync.RWMutex
}

// NewCOWPerformanceMonitor creates a performance monitor.
func NewCOWPerformanceMonitor() *COWPerformanceMonitor {
	return &COWPerformanceMonitor{}
}

// RecordCopy records a copy operation.
func (pm *COWPerformanceMonitor) RecordCopy() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.copyCount++
}

// RecordModify records a modify operation.
func (pm *COWPerformanceMonitor) RecordModify() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.modifyCount++
}

// GetStats returns current performance statistics.
func (pm *COWPerformanceMonitor) GetStats() map[string]int64 {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	return map[string]int64{
		"copies":   pm.copyCount,
		"modifies": pm.modifyCount,
		"shared":   pm.sharedNodes,
		"cloned":   pm.clonedNodes,
	}
}

// COWTreeComparator provides utilities for comparing COW tree versions.
type COWTreeComparator struct{}

// NewCOWTreeComparator creates a new comparator.
func NewCOWTreeComparator() *COWTreeComparator {
	return &COWTreeComparator{}
}

// Compare compares two COW trees and returns differences.
func (c *COWTreeComparator) Compare(tree1, tree2 ThreadSafeTree) (map[string]interface{}, error) {
	// This is a simplified implementation
	// A full implementation would provide detailed diff information

	if cow1, ok := tree1.(*COWTree); ok {
		if cow2, ok := tree2.(*COWTree); ok {
			version1 := cow1.GetVersion()
			version2 := cow2.GetVersion()

			return map[string]interface{}{
				"version1":  version1,
				"version2":  version2,
				"different": version1 != version2,
			}, nil
		}
	}

	return nil, fmt.Errorf("unsupported tree types for comparison")
}

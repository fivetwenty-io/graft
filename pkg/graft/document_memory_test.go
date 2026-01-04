package graft_test

import (
	"context"
	"testing"
	"time"

	. "github.com/fivetwenty-io/graft/pkg/graft"
	_ "github.com/fivetwenty-io/graft/pkg/graft/tree"
)

//nolint:gocyclo // test function covers comprehensive document memory functionality
func TestDocumentMemory(t *testing.T) {
	t.Run("basic functionality", func(t *testing.T) {
		config := MemoryConfig{
			Enabled:            true,
			MaxVersionsPerNode: 10,
			MaxTotalVersions:   100,
			MaxMemoryMB:        10,
			CompressAfter:      1 * time.Hour,
			CleanupInterval:    5 * time.Minute,
			TrackMergePhase:    true,
			TrackEvalPhase:     true,
			EnableCompression:  true,
			CompressThreshold:  5,
		}
		memory := NewDocumentMemory(config)
		defer memory.StopBackgroundCleanup()

		t.Run("track changes to nodes", func(t *testing.T) {
			err := memory.RecordChange("config.host", nil, "localhost", PhaseMerge, OpSet, "test.yaml")
			if err != nil {
				t.Fatalf("failed to record change: %v", err)
			}

			history, err := memory.GetHistory("config.host")
			if err != nil {
				t.Fatalf("failed to get history: %v", err)
			}

			if len(history.Versions) != 1 {
				t.Errorf("expected 1 version, got %d", len(history.Versions))
			}
			if history.Versions[0].Value != "localhost" {
				t.Errorf("expected value 'localhost', got %v", history.Versions[0].Value)
			}
			if history.Versions[0].Phase != PhaseMerge {
				t.Errorf("expected phase PhaseMerge, got %v", history.Versions[0].Phase)
			}
			if history.Versions[0].Operation != OpSet {
				t.Errorf("expected operation OpSet, got %v", history.Versions[0].Operation)
			}
			if history.Versions[0].Source != "test.yaml" {
				t.Errorf("expected source 'test.yaml', got %v", history.Versions[0].Source)
			}
		})

		t.Run("track multiple versions", func(t *testing.T) {
			err := memory.RecordChange("config.port", nil, int64(8080), PhaseInitial, OpSet, "initial")
			if err != nil {
				t.Fatalf("failed to record change: %v", err)
			}

			err = memory.RecordChange("config.port", int64(8080), int64(9090), PhaseMerge, OpMerge, "override.yaml")
			if err != nil {
				t.Fatalf("failed to record change: %v", err)
			}

			err = memory.RecordChange("config.port", int64(9090), int64(3000), PhaseEval, OpTransform, "calc")
			if err != nil {
				t.Fatalf("failed to record change: %v", err)
			}

			history, err := memory.GetHistory("config.port")
			if err != nil {
				t.Fatalf("failed to get history: %v", err)
			}

			if len(history.Versions) != 3 {
				t.Errorf("expected 3 versions, got %d", len(history.Versions))
			}
			if history.Current != 3 {
				t.Errorf("expected current version 3, got %d", history.Current)
			}

			// Check version progression
			if history.Versions[0].Value != int64(8080) {
				t.Errorf("expected first value 8080, got %v", history.Versions[0].Value)
			}
			if history.Versions[1].Value != int64(9090) {
				t.Errorf("expected second value 9090, got %v", history.Versions[1].Value)
			}
			if history.Versions[2].Value != int64(3000) {
				t.Errorf("expected third value 3000, got %v", history.Versions[2].Value)
			}
		})

		t.Run("get current value", func(t *testing.T) {
			err := memory.RecordChange("app.name", nil, "myapp", PhaseInitial, OpSet, "config.yaml")
			if err != nil {
				t.Fatalf("failed to record change: %v", err)
			}

			err = memory.RecordChange("app.name", "myapp", "newapp", PhaseMerge, OpReplace, "override.yaml")
			if err != nil {
				t.Fatalf("failed to record change: %v", err)
			}

			current, err := memory.GetCurrentValue("app.name")
			if err != nil {
				t.Fatalf("failed to get current value: %v", err)
			}

			if current != "newapp" {
				t.Errorf("expected current value 'newapp', got %v", current)
			}
		})

		t.Run("track deletions", func(t *testing.T) {
			err := memory.RecordChange("temp.value", nil, "temporary", PhaseInitial, OpSet, "config.yaml")
			if err != nil {
				t.Fatalf("failed to record change: %v", err)
			}

			err = memory.RecordChange("temp.value", "temporary", nil, PhaseEval, OpPrune, "prune operator")
			if err != nil {
				t.Fatalf("failed to record change: %v", err)
			}

			history, err := memory.GetHistory("temp.value")
			if err != nil {
				t.Fatalf("failed to get history: %v", err)
			}

			if len(history.Versions) != 2 {
				t.Errorf("expected 2 versions, got %d", len(history.Versions))
			}
			if history.Versions[1].Value != nil {
				t.Errorf("expected nil value for deletion, got %v", history.Versions[1].Value)
			}
			if history.Versions[1].Operation != OpPrune {
				t.Errorf("expected operation OpPrune, got %v", history.Versions[1].Operation)
			}
		})
	})

	t.Run("version comparison", func(t *testing.T) {
		config := MemoryConfig{Enabled: true}
		memory := NewDocumentMemory(config)
		defer memory.StopBackgroundCleanup()

		t.Run("compare two versions", func(t *testing.T) {
			err := memory.RecordChange("data.value", nil, int64(100), PhaseInitial, OpSet, "init")
			if err != nil {
				t.Fatalf("failed to record change: %v", err)
			}

			err = memory.RecordChange("data.value", int64(100), int64(200), PhaseMerge, OpMerge, "merge")
			if err != nil {
				t.Fatalf("failed to record change: %v", err)
			}

			err = memory.RecordChange("data.value", int64(200), int64(300), PhaseEval, OpTransform, "calc")
			if err != nil {
				t.Fatalf("failed to record change: %v", err)
			}

			diff, err := memory.Compare("data.value", 1, 3)
			if err != nil {
				t.Fatalf("failed to compare versions: %v", err)
			}

			if diff.FromValue != int64(100) {
				t.Errorf("expected from value 100, got %v", diff.FromValue)
			}
			if diff.ToValue != int64(300) {
				t.Errorf("expected to value 300, got %v", diff.ToValue)
			}
			if len(diff.Changes) != 3 {
				t.Errorf("expected 3 changes, got %d", len(diff.Changes))
			}
		})

		t.Run("handle invalid comparisons", func(t *testing.T) {
			err := memory.RecordChange("data.value", nil, int64(100), PhaseInitial, OpSet, "init")
			if err != nil {
				t.Fatalf("failed to record change: %v", err)
			}

			_, err = memory.Compare("data.value", 1, 99)
			if err == nil {
				t.Error("expected error for invalid version")
			}

			_, err = memory.Compare("nonexistent.path", 1, 2)
			if err == nil {
				t.Error("expected error for nonexistent path")
			}
		})
	})

	t.Run("timeline and querying", func(t *testing.T) {
		t.Run("maintain chronological timeline", func(t *testing.T) {
			config := MemoryConfig{Enabled: true}
			memory := NewDocumentMemory(config)
			defer memory.StopBackgroundCleanup()

			// Add changes with slight delays to ensure ordering
			err := memory.RecordChange("node1", nil, "value1", PhaseInitial, OpSet, "source1")
			if err != nil {
				t.Fatalf("failed to record change: %v", err)
			}
			time.Sleep(10 * time.Millisecond)

			err = memory.RecordChange("node2", nil, "value2", PhaseInitial, OpSet, "source2")
			if err != nil {
				t.Fatalf("failed to record change: %v", err)
			}
			time.Sleep(10 * time.Millisecond)

			err = memory.RecordChange("node1", "value1", "value1-updated", PhaseMerge, OpMerge, "source3")
			if err != nil {
				t.Fatalf("failed to record change: %v", err)
			}

			timeline := memory.GetTimeline()
			if len(timeline) != 3 {
				t.Errorf("expected 3 timeline events, got %d", len(timeline))
			}

			// Timeline should be in chronological order
			if timeline[0].Path != "node1" {
				t.Errorf("expected first event path 'node1', got %v", timeline[0].Path)
			}
			if timeline[1].Path != "node2" {
				t.Errorf("expected second event path 'node2', got %v", timeline[1].Path)
			}
			if timeline[2].Path != "node1" {
				t.Errorf("expected third event path 'node1', got %v", timeline[2].Path)
			}
		})

		t.Run("query by phase", func(t *testing.T) {
			config := MemoryConfig{Enabled: true}
			memory := NewDocumentMemory(config)
			defer memory.StopBackgroundCleanup()

			err := memory.RecordChange("a", nil, 1, PhaseInitial, OpSet, "init")
			if err != nil {
				t.Fatalf("failed to record change: %v", err)
			}

			err = memory.RecordChange("b", nil, 2, PhaseMerge, OpMerge, "merge")
			if err != nil {
				t.Fatalf("failed to record change: %v", err)
			}

			err = memory.RecordChange("c", nil, 3, PhaseEval, OpTransform, "eval")
			if err != nil {
				t.Fatalf("failed to record change: %v", err)
			}

			mergePhase := PhaseMerge
			results := memory.Query(HistoryFilter{Phase: &mergePhase})
			if len(results) != 1 {
				t.Errorf("expected 1 result, got %d", len(results))
			}
			if results[0].Path != "b" {
				t.Errorf("expected path 'b', got %v", results[0].Path)
			}
		})

		t.Run("query by operation", func(t *testing.T) {
			config := MemoryConfig{Enabled: true}
			memory := NewDocumentMemory(config)
			defer memory.StopBackgroundCleanup()

			err := memory.RecordChange("x", nil, "new", PhaseInitial, OpSet, "init")
			if err != nil {
				t.Fatalf("failed to record change: %v", err)
			}

			err = memory.RecordChange("y", "old", "new", PhaseMerge, OpReplace, "merge")
			if err != nil {
				t.Fatalf("failed to record change: %v", err)
			}

			err = memory.RecordChange("z", "value", nil, PhaseEval, OpPrune, "prune")
			if err != nil {
				t.Fatalf("failed to record change: %v", err)
			}

			pruneOp := OpPrune
			results := memory.Query(HistoryFilter{Operation: &pruneOp})
			if len(results) != 1 {
				t.Errorf("expected 1 result, got %d", len(results))
			}
			if results[0].Path != "z" {
				t.Errorf("expected path 'z', got %v", results[0].Path)
			}
		})
	})

	t.Run("memory management", func(t *testing.T) {
		t.Run("prune old versions", func(t *testing.T) {
			smallConfig := MemoryConfig{
				Enabled:            true,
				MaxVersionsPerNode: 3,
				MaxTotalVersions:   100,
			}
			smallMemory := NewDocumentMemory(smallConfig)
			defer smallMemory.StopBackgroundCleanup()

			// Add more versions than the limit
			for i := 0; i < 5; i++ {
				err := smallMemory.RecordChange("test.path", i, i+1, PhaseEval, OpTransform, "test")
				if err != nil {
					t.Fatalf("failed to record change: %v", err)
				}
			}

			history, err := smallMemory.GetHistory("test.path")
			if err != nil {
				t.Fatalf("failed to get history: %v", err)
			}

			if len(history.Versions) > 3 {
				t.Errorf("expected at most 3 versions, got %d", len(history.Versions))
			}
		})

		t.Run("track memory usage", func(t *testing.T) {
			config := MemoryConfig{Enabled: true}
			memory := NewDocumentMemory(config)
			defer memory.StopBackgroundCleanup()

			err := memory.RecordChange("large.data", nil, "a very long string that takes up some memory", PhaseInitial, OpSet, "init")
			if err != nil {
				t.Fatalf("failed to record change: %v", err)
			}

			stats := memory.GetMemoryStats()
			if stats["total_versions"] != 1 {
				t.Errorf("expected 1 total version, got %v", stats["total_versions"])
			}
			memUsage, ok := stats["memory_usage_bytes"].(int64)
			if !ok || memUsage <= 0 {
				t.Error("expected positive memory usage")
			}
			if stats["num_paths"] != 1 {
				t.Errorf("expected 1 path, got %v", stats["num_paths"])
			}
		})

		t.Run("enable and disable tracking", func(t *testing.T) {
			config := MemoryConfig{Enabled: true}
			memory := NewDocumentMemory(config)
			defer memory.StopBackgroundCleanup()

			memory.Disable()
			if memory.IsEnabled() {
				t.Error("expected memory to be disabled")
			}

			err := memory.RecordChange("test", nil, "value", PhaseInitial, OpSet, "init")
			if err != nil {
				t.Fatalf("failed to record change: %v", err)
			}

			// No history should be recorded when disabled
			timeline := memory.GetTimeline()
			if len(timeline) != 0 {
				t.Errorf("expected empty timeline, got %d events", len(timeline))
			}

			memory.Enable()
			if !memory.IsEnabled() {
				t.Error("expected memory to be enabled")
			}

			err = memory.RecordChange("test2", nil, "value2", PhaseInitial, OpSet, "init")
			if err != nil {
				t.Fatalf("failed to record change: %v", err)
			}

			timeline = memory.GetTimeline()
			if len(timeline) != 1 {
				t.Errorf("expected 1 timeline event, got %d", len(timeline))
			}
		})

		t.Run("clear all history", func(t *testing.T) {
			config := MemoryConfig{Enabled: true}
			memory := NewDocumentMemory(config)
			defer memory.StopBackgroundCleanup()

			err := memory.RecordChange("path1", nil, "value1", PhaseInitial, OpSet, "init")
			if err != nil {
				t.Fatalf("failed to record change: %v", err)
			}

			err = memory.RecordChange("path2", nil, "value2", PhaseInitial, OpSet, "init")
			if err != nil {
				t.Fatalf("failed to record change: %v", err)
			}

			memory.Clear()

			timeline := memory.GetTimeline()
			if len(timeline) != 0 {
				t.Errorf("expected empty timeline after clear, got %d events", len(timeline))
			}

			stats := memory.GetMemoryStats()
			if stats["total_versions"] != 0 {
				t.Errorf("expected 0 versions after clear, got %v", stats["total_versions"])
			}
			if stats["num_paths"] != 0 {
				t.Errorf("expected 0 paths after clear, got %v", stats["num_paths"])
			}
		})
	})

	t.Run("integration with engine", func(t *testing.T) {
		t.Run("track changes during merge and evaluation", func(t *testing.T) {
			// TODO: This test requires nested path tracking during merge operations.
			// Currently, the simple merge path only records top-level key changes (e.g., "base")
			// but not nested paths (e.g., "base.value"). Implementing full nested path tracking
			// requires passing path context through all merge functions.
			// See: merge_builder_impl.go mergeInto() and mergeValues()
			t.Skip("Skipping: nested path tracking during merge not yet implemented")

			// Create engine with memory tracking enabled
			engineConfig := DefaultEngineConfig()
			engineConfig.MemoryConfig.Enabled = true
			engine := NewDefaultEngineWithConfig(engineConfig)

			// Parse documents
			doc1, err := engine.ParseYAML([]byte(`
base:
  value: 100
  name: "original"
`))
			if err != nil {
				t.Fatalf("failed to parse doc1: %v", err)
			}

			doc2, err := engine.ParseYAML([]byte(`
base:
  value: 200
  extra: "added"
computed:
  result: (( grab base.value ))
  label: (( concat base.name "-modified" ))
`))
			if err != nil {
				t.Fatalf("failed to parse doc2: %v", err)
			}

			// Merge and evaluate
			merged, err := engine.Merge(context.TODO(), doc1, doc2).Execute()
			if err != nil {
				t.Fatalf("failed to merge: %v", err)
			}

			// Check the memory tracking
			memoryTracker := engine.GetMemoryTracker()
			if memoryTracker == nil {
				t.Fatal("expected memory tracker to be available")
			}

			// Type assert to get the actual DocumentMemory
			docMemory, ok := memoryTracker.(*DocumentMemory)
			if !ok {
				t.Fatal("expected memory tracker to be DocumentMemory")
			}

			// Verify merge phase tracking
			history, err := docMemory.GetHistory("base.value")
			if err != nil {
				t.Fatalf("failed to get history: %v", err)
			}
			if len(history.Versions) < 2 {
				t.Errorf("expected at least 2 versions for base.value, got %d", len(history.Versions))
			}

			// Check timeline
			timeline := docMemory.GetTimeline()
			if len(timeline) == 0 {
				t.Error("expected non-empty timeline")
			}

			// Verify merge tracking shows the source
			mergePhase := PhaseMerge
			mergeEvents := docMemory.Query(HistoryFilter{Phase: &mergePhase})
			if len(mergeEvents) == 0 {
				t.Error("expected merge events")
			}

			_ = merged // Use the merged result to avoid unused variable warning
		})
	})
}

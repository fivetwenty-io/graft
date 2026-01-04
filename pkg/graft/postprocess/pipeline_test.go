// Package postprocess provides post-processing handlers for graft documents.
package postprocess

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Test constants for repeated string literals.
const (
	testProcPrune     = "prune"
	testProcInject    = "inject"
	testHostLocalhost = "localhost"
	testStrValue      = "value"
)

// PruneMarker is a test marker type that matches the operators.PruneMarker type name.
// Used for testing without importing the operators package (to avoid import cycle).
type PruneMarker struct{}

// InjectMarker is a test marker type that matches the operators.InjectMarker type name.
// Used for testing without importing the operators package (to avoid import cycle).
type InjectMarker struct {
	Source interface{}
}

// =============================================================================
// PruneProcessor Tests
// =============================================================================

func TestPruneProcessor_SimplePrune(t *testing.T) {
	proc := NewPruneProcessor()

	doc := map[string]interface{}{
		"a": 1,
		"b": "(( prune ))",
	}

	result, err := proc.Process(context.Background(), doc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be map[string]interface{}")
	}

	if _, ok := resultMap["b"]; ok {
		t.Error("expected 'b' to be pruned")
	}
	if val, ok := resultMap["a"]; !ok || val != 1 {
		t.Errorf("expected 'a' = 1, got %v", val)
	}
}

func TestPruneProcessor_NestedPrune(t *testing.T) {
	proc := NewPruneProcessor()

	doc := map[string]interface{}{
		"x": map[string]interface{}{
			"y": "(( prune ))",
			"z": 42,
		},
	}

	result, err := proc.Process(context.Background(), doc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be map[string]interface{}")
	}
	xMap, ok := resultMap["x"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'x' to be map[string]interface{}")
	}

	if _, ok := xMap["y"]; ok {
		t.Error("expected 'x.y' to be pruned")
	}
	if val, ok := xMap["z"]; !ok || val != 42 {
		t.Errorf("expected 'x.z' = 42, got %v", val)
	}
}

func TestPruneProcessor_ArrayPrune(t *testing.T) {
	proc := NewPruneProcessor()

	doc := map[string]interface{}{
		"items": []interface{}{1, "(( prune ))", 3},
	}

	result, err := proc.Process(context.Background(), doc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be map[string]interface{}")
	}
	items, ok := resultMap["items"].([]interface{})
	if !ok {
		t.Fatal("expected 'items' to be []interface{}")
	}

	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}
	if items[0] != 1 || items[1] != 3 {
		t.Errorf("expected [1, 3], got %v", items)
	}
}

func TestPruneProcessor_PruneMarkerType(t *testing.T) {
	proc := NewPruneProcessor()

	doc := map[string]interface{}{
		"a": 1,
		"b": PruneMarker{},
	}

	result, err := proc.Process(context.Background(), doc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be map[string]interface{}")
	}

	if _, ok := resultMap["b"]; ok {
		t.Error("expected 'b' with PruneMarker to be pruned")
	}
	if val, ok := resultMap["a"]; !ok || val != 1 {
		t.Errorf("expected 'a' = 1, got %v", val)
	}
}

func TestPruneProcessor_Name(t *testing.T) {
	proc := NewPruneProcessor()
	if proc.Name() != testProcPrune {
		t.Errorf("expected name '%s', got '%s'", testProcPrune, proc.Name())
	}
}

func TestPruneProcessor_Phase(t *testing.T) {
	proc := NewPruneProcessor()
	if proc.Phase() != PhaseEarly {
		t.Errorf("expected phase PhaseEarly, got %v", proc.Phase())
	}
}

func TestPruneProcessor_Priority(t *testing.T) {
	proc := NewPruneProcessor()
	if proc.Priority() != 10 {
		t.Errorf("expected priority 10, got %d", proc.Priority())
	}
}

// =============================================================================
// InjectProcessor Tests
// =============================================================================

func TestInjectProcessor_SimpleInject(t *testing.T) {
	proc := NewInjectProcessor()

	shared := map[string]interface{}{
		"host": testHostLocalhost,
		"port": 8080,
	}

	doc := map[string]interface{}{
		"<<":   shared,
		"name": "myapp",
	}

	result, err := proc.Process(context.Background(), doc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be map[string]interface{}")
	}

	if val, ok := resultMap["host"]; !ok || val != testHostLocalhost {
		t.Errorf("expected 'host' = '%s', got %v", testHostLocalhost, val)
	}
	if val, ok := resultMap["port"]; !ok || val != 8080 {
		t.Errorf("expected 'port' = 8080, got %v", val)
	}
	if val, ok := resultMap["name"]; !ok || val != "myapp" {
		t.Errorf("expected 'name' = 'myapp', got %v", val)
	}
}

func TestInjectProcessor_InjectMarkerType(t *testing.T) {
	proc := NewInjectProcessor()

	shared := map[string]interface{}{
		"host": testHostLocalhost,
		"port": 8080,
	}

	doc := map[string]interface{}{
		"inject_key": &InjectMarker{Source: shared},
		"name":       "myapp",
	}

	result, err := proc.Process(context.Background(), doc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be map[string]interface{}")
	}

	if val, ok := resultMap["host"]; !ok || val != testHostLocalhost {
		t.Errorf("expected 'host' = '%s', got %v", testHostLocalhost, val)
	}
	if val, ok := resultMap["port"]; !ok || val != 8080 {
		t.Errorf("expected 'port' = 8080, got %v", val)
	}
	if _, ok := resultMap["inject_key"]; ok {
		t.Error("expected 'inject_key' to be removed after injection")
	}
}

func TestInjectProcessor_NestedInject(t *testing.T) {
	proc := NewInjectProcessor()

	shared := map[string]interface{}{
		"timeout": 30,
	}

	doc := map[string]interface{}{
		"database": map[string]interface{}{
			"<<":   shared,
			"host": "db.example.com",
		},
	}

	result, err := proc.Process(context.Background(), doc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be map[string]interface{}")
	}
	dbMap, ok := resultMap["database"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'database' to be map[string]interface{}")
	}

	if val, ok := dbMap["timeout"]; !ok || val != 30 {
		t.Errorf("expected 'database.timeout' = 30, got %v", val)
	}
	if val, ok := dbMap["host"]; !ok || val != "db.example.com" {
		t.Errorf("expected 'database.host' = 'db.example.com', got %v", val)
	}
}

func TestInjectProcessor_Name(t *testing.T) {
	proc := NewInjectProcessor()
	if proc.Name() != testProcInject {
		t.Errorf("expected name '%s', got '%s'", testProcInject, proc.Name())
	}
}

func TestInjectProcessor_Phase(t *testing.T) {
	proc := NewInjectProcessor()
	if proc.Phase() != PhaseEarly {
		t.Errorf("expected phase PhaseEarly, got %v", proc.Phase())
	}
}

func TestInjectProcessor_Priority(t *testing.T) {
	proc := NewInjectProcessor()
	if proc.Priority() != 5 {
		t.Errorf("expected priority 5, got %d", proc.Priority())
	}
}

// =============================================================================
// KeySorter Tests
// =============================================================================

func TestKeySorter_AlphabeticalOrder(t *testing.T) {
	proc := NewKeySorter(true)

	doc := map[string]interface{}{
		"zebra":  1,
		"apple":  2,
		"mango":  3,
		"banana": 4,
	}

	result, err := proc.Process(context.Background(), doc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sortedMap, ok := result.(*SortedMap)
	if !ok {
		t.Fatal("expected result to be *SortedMap")
	}

	keys := sortedMap.Keys()
	expected := []string{"apple", "banana", "mango", "zebra"}

	for i, key := range keys {
		if key != expected[i] {
			t.Errorf("expected key %d to be '%s', got '%s'", i, expected[i], key)
		}
	}
}

func TestKeySorter_Disabled(t *testing.T) {
	proc := NewKeySorter(false)

	doc := map[string]interface{}{
		"z": 1,
		"a": 2,
	}

	result, err := proc.Process(context.Background(), doc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should return the same map, not a SortedMap
	if _, ok := result.(*SortedMap); ok {
		t.Error("expected result to NOT be *SortedMap when disabled")
	}
}

func TestKeySorter_NestedMaps(t *testing.T) {
	proc := NewKeySorter(true)
	proc.Recursive = true

	doc := map[string]interface{}{
		"z": map[string]interface{}{
			"c": 1,
			"a": 2,
			"b": 3,
		},
		"a": 4,
	}

	result, err := proc.Process(context.Background(), doc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sortedMap, ok := result.(*SortedMap)
	if !ok {
		t.Fatal("expected result to be *SortedMap")
	}
	keys := sortedMap.Keys()

	if keys[0] != "a" || keys[1] != "z" {
		t.Errorf("expected top-level keys ['a', 'z'], got %v", keys)
	}

	// Check nested map is also sorted
	nestedVal, _ := sortedMap.Get("z")
	nestedSorted, ok := nestedVal.(*SortedMap)
	if !ok {
		t.Fatal("expected nested map to be *SortedMap")
	}

	nestedKeys := nestedSorted.Keys()
	if nestedKeys[0] != "a" || nestedKeys[1] != "b" || nestedKeys[2] != "c" {
		t.Errorf("expected nested keys ['a', 'b', 'c'], got %v", nestedKeys)
	}
}

func TestKeySorter_Name(t *testing.T) {
	proc := NewKeySorter(true)
	if proc.Name() != "key-sorter" {
		t.Errorf("expected name 'key-sorter', got '%s'", proc.Name())
	}
}

func TestKeySorter_Phase(t *testing.T) {
	proc := NewKeySorter(true)
	if proc.Phase() != PhaseLate {
		t.Errorf("expected phase PhaseLate, got %v", proc.Phase())
	}
}

func TestKeySorter_Priority(t *testing.T) {
	proc := NewKeySorter(true)
	if proc.Priority() != 100 {
		t.Errorf("expected priority 100, got %d", proc.Priority())
	}
}

// =============================================================================
// CherryPickProcessor Tests
// =============================================================================

func TestCherryPickProcessor_SinglePath(t *testing.T) {
	proc := NewCherryPickProcessor([]string{"database.host"})

	doc := map[string]interface{}{
		"database": map[string]interface{}{
			"host": testHostLocalhost,
			"port": 5432,
		},
		"cache": map[string]interface{}{
			"host": "redis.local",
		},
	}

	result, err := proc.Process(context.Background(), doc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be map[string]interface{}")
	}
	dbMap, ok := resultMap["database"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'database' to exist in result")
	}

	if val, ok := dbMap["host"]; !ok || val != testHostLocalhost {
		t.Errorf("expected 'database.host' = '%s', got %v", testHostLocalhost, val)
	}

	if _, ok := dbMap["port"]; ok {
		t.Error("expected 'database.port' to NOT be in result")
	}

	if _, ok := resultMap["cache"]; ok {
		t.Error("expected 'cache' to NOT be in result")
	}
}

func TestCherryPickProcessor_MultiplePaths(t *testing.T) {
	proc := NewCherryPickProcessor([]string{"database.host", "cache.host"})

	doc := map[string]interface{}{
		"database": map[string]interface{}{
			"host": testHostLocalhost,
			"port": 5432,
		},
		"cache": map[string]interface{}{
			"host": "redis.local",
			"port": 6379,
		},
		"app": map[string]interface{}{
			"name": "myapp",
		},
	}

	result, err := proc.Process(context.Background(), doc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be map[string]interface{}")
	}

	dbMap, ok := resultMap["database"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'database' to be map[string]interface{}")
	}
	if val := dbMap["host"]; val != testHostLocalhost {
		t.Errorf("expected 'database.host' = '%s', got %v", testHostLocalhost, val)
	}

	cacheMap, ok := resultMap["cache"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'cache' to be map[string]interface{}")
	}
	if val := cacheMap["host"]; val != "redis.local" {
		t.Errorf("expected 'cache.host' = 'redis.local', got %v", val)
	}

	if _, ok := resultMap["app"]; ok {
		t.Error("expected 'app' to NOT be in result")
	}
}

func TestCherryPickProcessor_MissingPathsIgnored(t *testing.T) {
	proc := NewCherryPickProcessor([]string{"exists.value", "missing.path"})

	doc := map[string]interface{}{
		"exists": map[string]interface{}{
			"value": 42,
		},
	}

	result, err := proc.Process(context.Background(), doc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be map[string]interface{}")
	}
	existsMap, ok := resultMap["exists"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'exists' to be map[string]interface{}")
	}

	if val := existsMap["value"]; val != 42 {
		t.Errorf("expected 'exists.value' = 42, got %v", val)
	}

	// Missing path should not cause an error
	if _, ok := resultMap["missing"]; ok {
		t.Error("expected 'missing' to NOT be in result")
	}
}

func TestCherryPickProcessor_EmptyPaths(t *testing.T) {
	proc := NewCherryPickProcessor(nil)

	doc := map[string]interface{}{
		"a": 1,
		"b": 2,
	}

	result, err := proc.Process(context.Background(), doc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With no paths, should return original document
	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be map[string]interface{}")
	}
	if resultMap["a"] != 1 || resultMap["b"] != 2 {
		t.Error("expected original document when no paths specified")
	}
}

func TestCherryPickProcessor_Name(t *testing.T) {
	proc := NewCherryPickProcessor(nil)
	if proc.Name() != "cherry-pick" {
		t.Errorf("expected name 'cherry-pick', got '%s'", proc.Name())
	}
}

func TestCherryPickProcessor_Phase(t *testing.T) {
	proc := NewCherryPickProcessor(nil)
	if proc.Phase() != PhaseLate {
		t.Errorf("expected phase PhaseLate, got %v", proc.Phase())
	}
}

func TestCherryPickProcessor_Priority(t *testing.T) {
	proc := NewCherryPickProcessor(nil)
	if proc.Priority() != 50 {
		t.Errorf("expected priority 50, got %d", proc.Priority())
	}
}

// =============================================================================
// Pipeline Tests
// =============================================================================

func TestPipeline_PriorityOrderExecution(t *testing.T) {
	pipeline := NewPipeline()

	// Add processors in wrong order
	pipeline.Add(NewKeySorter(true))   // Priority 100
	pipeline.Add(NewPruneProcessor())  // Priority 10
	pipeline.Add(NewInjectProcessor()) // Priority 5

	// Verify they execute in correct priority order
	doc := map[string]interface{}{
		"<<": map[string]interface{}{
			"injected": testStrValue,
		},
		"keep":   "me",
		"remove": "(( prune ))",
	}

	result, err := pipeline.Process(doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check inject happened first (priority 5)
	sortedResult, ok := result.(*SortedMap)
	if !ok {
		t.Fatal("expected result to be *SortedMap")
	}
	if val, ok := sortedResult.Get("injected"); !ok || val != testStrValue {
		t.Error("expected injection to occur")
	}

	// Check prune happened second (priority 10)
	if _, ok := sortedResult.Get("remove"); ok {
		t.Error("expected prune to remove 'remove' key")
	}

	// Check sort happened last (priority 100)
	keys := sortedResult.Keys()
	if keys[0] != "injected" || keys[1] != "keep" {
		t.Errorf("expected sorted keys ['injected', 'keep'], got %v", keys)
	}
}

func TestPipeline_ErrorPropagation(t *testing.T) {
	pipeline := NewPipeline()

	// Add a processor that returns an error
	errorProc := NewTransformProcessor("error-proc", PhaseNormal, func(ctx context.Context, doc interface{}, meta *Metadata) (interface{}, error) {
		return nil, context.DeadlineExceeded
	})
	pipeline.Add(errorProc)

	_, err := pipeline.Process(map[string]interface{}{})
	if err == nil {
		t.Error("expected error to be propagated")
	}
}

func TestPipeline_ContextCancellation(t *testing.T) {
	pipeline := NewPipeline()

	// Add a slow processor
	slowProc := NewTransformProcessor("slow-proc", PhaseNormal, func(ctx context.Context, doc interface{}, meta *Metadata) (interface{}, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
			return doc, nil
		}
	})
	pipeline.Add(slowProc)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := pipeline.ProcessWithContext(ctx, map[string]interface{}{}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestPipeline_Add(t *testing.T) {
	pipeline := NewPipeline()

	if pipeline.Count() != 0 {
		t.Error("expected empty pipeline")
	}

	pipeline.Add(NewPruneProcessor())
	if pipeline.Count() != 1 {
		t.Errorf("expected count 1, got %d", pipeline.Count())
	}

	pipeline.Add(NewInjectProcessor())
	if pipeline.Count() != 2 {
		t.Errorf("expected count 2, got %d", pipeline.Count())
	}
}

func TestPipeline_Remove(t *testing.T) {
	pipeline := NewPipeline()
	pipeline.Add(NewPruneProcessor())
	pipeline.Add(NewInjectProcessor())

	removed := pipeline.Remove("prune")
	if !removed {
		t.Error("expected Remove to return true")
	}
	if pipeline.Count() != 1 {
		t.Errorf("expected count 1, got %d", pipeline.Count())
	}

	removed = pipeline.Remove("nonexistent")
	if removed {
		t.Error("expected Remove to return false for nonexistent processor")
	}
}

func TestPipeline_Get(t *testing.T) {
	pipeline := NewPipeline()
	pipeline.Add(NewPruneProcessor())

	proc, found := pipeline.Get("prune")
	if !found {
		t.Error("expected to find 'prune' processor")
	}
	if proc.Name() != "prune" {
		t.Errorf("expected name 'prune', got '%s'", proc.Name())
	}

	_, found = pipeline.Get("nonexistent")
	if found {
		t.Error("expected not to find 'nonexistent' processor")
	}
}

func TestPipeline_Clear(t *testing.T) {
	pipeline := NewPipeline()
	pipeline.Add(NewPruneProcessor())
	pipeline.Add(NewInjectProcessor())

	pipeline.Clear()

	if pipeline.Count() != 0 {
		t.Errorf("expected count 0, got %d", pipeline.Count())
	}
}

func TestPipeline_List(t *testing.T) {
	pipeline := NewPipeline()
	pipeline.Add(NewPruneProcessor())
	pipeline.Add(NewInjectProcessor())

	names := pipeline.List()
	if len(names) != 2 {
		t.Errorf("expected 2 names, got %d", len(names))
	}
}

func TestDefaultPipeline(t *testing.T) {
	pipeline := DefaultPipeline()

	if pipeline.Count() != 2 {
		t.Errorf("expected 2 processors, got %d", pipeline.Count())
	}

	names := pipeline.List()
	hasInject := false
	hasPrune := false
	for _, name := range names {
		if name == "inject" {
			hasInject = true
		}
		if name == "prune" {
			hasPrune = true
		}
	}

	if !hasInject || !hasPrune {
		t.Error("expected DefaultPipeline to include inject and prune")
	}
}

func TestFullPipeline(t *testing.T) {
	pipeline := FullPipeline()

	if pipeline.Count() != 4 {
		t.Errorf("expected 4 processors, got %d", pipeline.Count())
	}

	names := pipeline.List()
	expected := map[string]bool{
		"inject":      false,
		"prune":       false,
		"cherry-pick": false,
		"key-sorter":  false,
	}

	for _, name := range names {
		expected[name] = true
	}

	for name, found := range expected {
		if !found {
			t.Errorf("expected FullPipeline to include '%s'", name)
		}
	}
}

// =============================================================================
// Helper Function Tests
// =============================================================================

func TestSplitPath(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"", nil},
		{"foo", []string{"foo"}},
		{"foo.bar", []string{"foo", "bar"}},
		{"foo.bar.baz", []string{"foo", "bar", "baz"}},
		{"items[0]", []string{"items", "0"}},
		{"items[0].name", []string{"items", "0", "name"}},
		{"[0].name", []string{"0", "name"}},
	}

	for _, tt := range tests {
		result := splitPath(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("splitPath(%q): expected %v, got %v", tt.input, tt.expected, result)
			continue
		}
		for i, v := range result {
			if v != tt.expected[i] {
				t.Errorf("splitPath(%q)[%d]: expected %q, got %q", tt.input, i, tt.expected[i], v)
			}
		}
	}
}

func TestGetPath(t *testing.T) {
	doc := map[string]interface{}{
		"database": map[string]interface{}{
			"host": "localhost",
			"port": 5432,
		},
		"items": []interface{}{
			map[string]interface{}{"name": "first"},
			map[string]interface{}{"name": "second"},
		},
	}

	tests := []struct {
		path     string
		expected interface{}
	}{
		{"database.host", "localhost"},
		{"database.port", 5432},
		{"items[0].name", "first"},
		{"items[1].name", "second"},
		{"nonexistent", nil},
		{"database.nonexistent", nil},
	}

	for _, tt := range tests {
		result := getPath(doc, tt.path)
		if result != tt.expected {
			t.Errorf("getPath(%q): expected %v, got %v", tt.path, tt.expected, result)
		}
	}
}

func TestSetPath(t *testing.T) {
	doc := make(map[string]interface{})
	setPath(doc, "database.host", testHostLocalhost)

	dbMap, ok := doc["database"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'database' to be a map")
	}

	if dbMap["host"] != testHostLocalhost {
		t.Errorf("expected 'database.host' = 'localhost', got %v", dbMap["host"])
	}
}

func TestPhase_String(t *testing.T) {
	tests := []struct {
		phase    Phase
		expected string
	}{
		{PhaseEarly, "early"},
		{PhaseNormal, "normal"},
		{PhaseLate, "late"},
		{Phase(99), "Phase(99)"},
	}

	for _, tt := range tests {
		result := tt.phase.String()
		if result != tt.expected {
			t.Errorf("Phase(%d).String(): expected %q, got %q", tt.phase, tt.expected, result)
		}
	}
}

// =============================================================================
// SortedMap Tests
// =============================================================================

func TestSortedMap_Keys(t *testing.T) {
	sm := &SortedMap{
		Data: map[string]interface{}{
			"z": 1,
			"a": 2,
			"m": 3,
		},
	}

	keys := sm.Keys()
	expected := []string{"a", "m", "z"}

	for i, key := range keys {
		if key != expected[i] {
			t.Errorf("expected key %d to be '%s', got '%s'", i, expected[i], key)
		}
	}
}

func TestSortedMap_Get(t *testing.T) {
	sm := &SortedMap{
		Data: map[string]interface{}{
			"a": 1,
			"b": 2,
		},
	}

	val, ok := sm.Get("a")
	if !ok || val != 1 {
		t.Errorf("expected Get('a') = (1, true), got (%v, %v)", val, ok)
	}

	_, ok = sm.Get("nonexistent")
	if ok {
		t.Error("expected Get('nonexistent') to return false")
	}
}

func TestSortedMap_Range(t *testing.T) {
	sm := &SortedMap{
		Data: map[string]interface{}{
			"c": 3,
			"a": 1,
			"b": 2,
		},
	}

	var keys []string
	sm.Range(func(key string, value interface{}) bool {
		keys = append(keys, key)
		return true
	})

	expected := []string{"a", "b", "c"}
	for i, key := range keys {
		if key != expected[i] {
			t.Errorf("expected key %d to be '%s', got '%s'", i, expected[i], key)
		}
	}
}

func TestSortedMap_Range_EarlyStop(t *testing.T) {
	sm := &SortedMap{
		Data: map[string]interface{}{
			"a": 1,
			"b": 2,
			"c": 3,
		},
	}

	var keys []string
	sm.Range(func(key string, value interface{}) bool {
		keys = append(keys, key)
		return len(keys) < 2 // Stop after 2 items
	})

	if len(keys) != 2 {
		t.Errorf("expected 2 keys, got %d", len(keys))
	}
}

// =============================================================================
// PathPruner Tests
// =============================================================================

func TestPathPruner_SinglePath(t *testing.T) {
	proc := NewPathPruner([]string{"database.password"})

	doc := map[string]interface{}{
		"database": map[string]interface{}{
			"host":     "localhost",
			"password": "secret",
		},
	}

	result, err := proc.Process(context.Background(), doc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be map[string]interface{}")
	}
	dbMap, ok := resultMap["database"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'database' to be map[string]interface{}")
	}

	if _, ok := dbMap["password"]; ok {
		t.Error("expected 'database.password' to be removed")
	}
	if val, ok := dbMap["host"]; !ok || val != testHostLocalhost {
		t.Errorf("expected 'database.host' = '%s', got %v", testHostLocalhost, val)
	}
}

func TestPathPruner_MultiplePaths(t *testing.T) {
	proc := NewPathPruner([]string{"a", "c"})

	doc := map[string]interface{}{
		"a": 1,
		"b": 2,
		"c": 3,
	}

	result, err := proc.Process(context.Background(), doc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be map[string]interface{}")
	}

	if _, ok := resultMap["a"]; ok {
		t.Error("expected 'a' to be removed")
	}
	if _, ok := resultMap["c"]; ok {
		t.Error("expected 'c' to be removed")
	}
	if val, ok := resultMap["b"]; !ok || val != 2 {
		t.Errorf("expected 'b' = 2, got %v", val)
	}
}

func TestPathPruner_EmptyPaths(t *testing.T) {
	proc := NewPathPruner(nil)

	doc := map[string]interface{}{
		"a": 1,
		"b": 2,
	}

	result, err := proc.Process(context.Background(), doc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be map[string]interface{}")
	}
	if resultMap["a"] != 1 || resultMap["b"] != 2 {
		t.Error("expected original document when no paths specified")
	}
}

func TestPathPruner_ArrayIndex(t *testing.T) {
	proc := NewPathPruner([]string{"items[1]"})

	doc := map[string]interface{}{
		"items": []interface{}{"a", "b", "c"},
	}

	result, err := proc.Process(context.Background(), doc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be map[string]interface{}")
	}
	items, ok := resultMap["items"].([]interface{})
	if !ok {
		t.Fatal("expected 'items' to be []interface{}")
	}

	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}
	if items[0] != "a" || items[1] != "c" {
		t.Errorf("expected ['a', 'c'], got %v", items)
	}
}

func TestPathPruner_Name(t *testing.T) {
	proc := NewPathPruner(nil)
	if proc.Name() != "path-pruner" {
		t.Errorf("expected name 'path-pruner', got '%s'", proc.Name())
	}
}

func TestPathPruner_Phase(t *testing.T) {
	proc := NewPathPruner(nil)
	if proc.Phase() != PhaseLate {
		t.Errorf("expected phase PhaseLate, got %v", proc.Phase())
	}
}

// =============================================================================
// TransformProcessor Tests
// =============================================================================

func TestTransformProcessor_CustomTransform(t *testing.T) {
	proc := NewTransformProcessor("upper", PhaseNormal, func(ctx context.Context, doc interface{}, meta *Metadata) (interface{}, error) {
		// Convert all string values to uppercase
		if m, ok := doc.(map[string]interface{}); ok {
			result := make(map[string]interface{})
			for k, v := range m {
				if s, ok := v.(string); ok {
					result[k] = s + "_TRANSFORMED"
				} else {
					result[k] = v
				}
			}
			return result, nil
		}
		return doc, nil
	})

	doc := map[string]interface{}{
		"name": "test",
		"num":  42,
	}

	result, err := proc.Process(context.Background(), doc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be map[string]interface{}")
	}
	if resultMap["name"] != "test_TRANSFORMED" {
		t.Errorf("expected 'test_TRANSFORMED', got %v", resultMap["name"])
	}
	if resultMap["num"] != 42 {
		t.Errorf("expected 42, got %v", resultMap["num"])
	}
}

func TestTransformProcessor_NilTransform(t *testing.T) {
	proc := NewTransformProcessor("noop", PhaseNormal, nil)

	doc := map[string]interface{}{"a": 1}

	result, err := proc.Process(context.Background(), doc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be map[string]interface{}")
	}
	if resultMap["a"] != 1 {
		t.Error("expected original document when transform is nil")
	}
}

func TestTransformProcessor_Name(t *testing.T) {
	proc := NewTransformProcessor("my-transform", PhaseNormal, nil)
	if proc.Name() != "my-transform" {
		t.Errorf("expected name 'my-transform', got '%s'", proc.Name())
	}
}

func TestTransformProcessor_Phase(t *testing.T) {
	proc := NewTransformProcessor("test", PhaseEarly, nil)
	if proc.Phase() != PhaseEarly {
		t.Errorf("expected phase PhaseEarly, got %v", proc.Phase())
	}
}

// =============================================================================
// Utility Function Tests
// =============================================================================

func TestFlatten(t *testing.T) {
	doc := map[string]interface{}{
		"database": map[string]interface{}{
			"host": "localhost",
			"port": 5432,
		},
		"items": []interface{}{"a", "b"},
	}

	flat := Flatten(doc)

	if flat["database.host"] != "localhost" {
		t.Errorf("expected 'database.host' = 'localhost', got %v", flat["database.host"])
	}
	if flat["database.port"] != 5432 {
		t.Errorf("expected 'database.port' = 5432, got %v", flat["database.port"])
	}
	if flat["items[0]"] != "a" {
		t.Errorf("expected 'items[0]' = 'a', got %v", flat["items[0]"])
	}
	if flat["items[1]"] != "b" {
		t.Errorf("expected 'items[1]' = 'b', got %v", flat["items[1]"])
	}
}

func TestUnflatten(t *testing.T) {
	flat := map[string]interface{}{
		"database.host": "localhost",
		"database.port": 5432,
	}

	doc := Unflatten(flat)

	dbMap, ok := doc["database"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'database' to be map[string]interface{}")
	}
	if dbMap["host"] != testHostLocalhost {
		t.Errorf("expected 'database.host' = '%s', got %v", testHostLocalhost, dbMap["host"])
	}
	if dbMap["port"] != 5432 {
		t.Errorf("expected 'database.port' = 5432, got %v", dbMap["port"])
	}
}

func TestNormalizeMapKeys(t *testing.T) {
	doc := map[interface{}]interface{}{
		"string_key": "value1",
		123:          "value2",
		true:         "value3",
	}

	normalized := NormalizeMapKeys(doc)

	normalizedMap, ok := normalized.(map[string]interface{})
	if !ok {
		t.Fatal("expected normalized to be map[string]interface{}")
	}
	if normalizedMap["string_key"] != "value1" {
		t.Error("expected 'string_key' = 'value1'")
	}
	if normalizedMap["123"] != "value2" {
		t.Error("expected '123' = 'value2'")
	}
	if normalizedMap["true"] != "value3" {
		t.Error("expected 'true' = 'value3'")
	}
}

func TestNormalizeMapKeys_Nested(t *testing.T) {
	doc := map[interface{}]interface{}{
		"outer": map[interface{}]interface{}{
			"inner": testStrValue,
		},
	}

	normalized := NormalizeMapKeys(doc)

	normalizedMap, ok := normalized.(map[string]interface{})
	if !ok {
		t.Fatal("expected normalized to be map[string]interface{}")
	}
	outerMap, ok := normalizedMap["outer"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'outer' to be map[string]interface{}")
	}
	if outerMap["inner"] != testStrValue {
		t.Error("expected 'outer.inner' = 'value'")
	}
}

func TestNormalizeMapKeys_Array(t *testing.T) {
	doc := []interface{}{
		map[interface{}]interface{}{
			"key": "value",
		},
	}

	normalized := NormalizeMapKeys(doc)

	normalizedSlice, ok := normalized.([]interface{})
	if !ok {
		t.Fatal("expected normalized to be []interface{}")
	}
	normalizedMap, ok := normalizedSlice[0].(map[string]interface{})
	if !ok {
		t.Fatal("expected array element to be map[string]interface{}")
	}
	if normalizedMap["key"] != testStrValue {
		t.Error("expected array element to be normalized")
	}
}

// =============================================================================
// Marker Type Detection Tests
// =============================================================================

func TestIsPruneMarkerType(t *testing.T) {
	if !isPruneMarkerType(PruneMarker{}) {
		t.Error("expected isPruneMarkerType to return true for PruneMarker")
	}

	if isPruneMarkerType("string") {
		t.Error("expected isPruneMarkerType to return false for string")
	}

	if isPruneMarkerType(nil) {
		t.Error("expected isPruneMarkerType to return false for nil")
	}
}

func TestGetInjectMarkerSource(t *testing.T) {
	shared := map[string]interface{}{"key": "value"}

	// Test with pointer to InjectMarker
	source := getInjectMarkerSource(&InjectMarker{Source: shared})
	if source == nil {
		t.Error("expected getInjectMarkerSource to return source")
	}
	sourceMap, ok := source.(map[string]interface{})
	if !ok {
		t.Fatal("expected source to be map[string]interface{}")
	}
	if sourceMap["key"] != testStrValue {
		t.Error("expected source to contain the correct data")
	}

	// Test with non-InjectMarker
	if getInjectMarkerSource("string") != nil {
		t.Error("expected getInjectMarkerSource to return nil for non-InjectMarker")
	}

	// Test with nil
	if getInjectMarkerSource(nil) != nil {
		t.Error("expected getInjectMarkerSource to return nil for nil")
	}
}

// =============================================================================
// Additional Edge Case Tests
// =============================================================================

func TestPruneProcessor_MapInterfaceInterface(t *testing.T) {
	proc := NewPruneProcessor()

	doc := map[interface{}]interface{}{
		"keep":   1,
		"remove": "(( prune ))",
	}

	result, err := proc.Process(context.Background(), doc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be map[string]interface{}")
	}

	if _, ok := resultMap["remove"]; ok {
		t.Error("expected 'remove' to be pruned")
	}
	if val, ok := resultMap["keep"]; !ok || val != 1 {
		t.Errorf("expected 'keep' = 1, got %v", val)
	}
}

func TestInjectProcessor_MapInterfaceInterface(t *testing.T) {
	proc := NewInjectProcessor()

	shared := map[interface{}]interface{}{
		"host": "localhost",
	}

	doc := map[interface{}]interface{}{
		"<<":   shared,
		"name": "myapp",
	}

	result, err := proc.Process(context.Background(), doc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be map[string]interface{}")
	}

	if val, ok := resultMap["host"]; !ok || val != testHostLocalhost {
		t.Errorf("expected 'host' = '%s', got %v", testHostLocalhost, val)
	}
}

func TestInjectProcessor_Array(t *testing.T) {
	proc := NewInjectProcessor()

	doc := []interface{}{
		map[string]interface{}{
			"<<": map[string]interface{}{
				"shared": "value",
			},
			"name": "item",
		},
	}

	result, err := proc.Process(context.Background(), doc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultSlice, ok := result.([]interface{})
	if !ok {
		t.Fatal("expected result to be []interface{}")
	}
	resultMap, ok := resultSlice[0].(map[string]interface{})
	if !ok {
		t.Fatal("expected array element to be map[string]interface{}")
	}

	if val, ok := resultMap["shared"]; !ok || val != testStrValue {
		t.Errorf("expected 'shared' = '%s', got %v", testStrValue, val)
	}
}

func TestKeySorter_MapInterfaceInterface(t *testing.T) {
	proc := NewKeySorter(true)

	doc := map[interface{}]interface{}{
		"z": 1,
		"a": 2,
	}

	result, err := proc.Process(context.Background(), doc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sortedMap, ok := result.(*SortedMap)
	if !ok {
		t.Fatal("expected result to be *SortedMap")
	}
	keys := sortedMap.Keys()

	if keys[0] != "a" || keys[1] != "z" {
		t.Errorf("expected sorted keys ['a', 'z'], got %v", keys)
	}
}

func TestKeySorter_SortedMapInput(t *testing.T) {
	proc := NewKeySorter(true)

	doc := &SortedMap{
		Data: map[string]interface{}{
			"z": 1,
			"a": 2,
		},
	}

	result, err := proc.Process(context.Background(), doc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sortedMap, ok := result.(*SortedMap)
	if !ok {
		t.Fatal("expected result to be *SortedMap")
	}
	keys := sortedMap.Keys()

	if keys[0] != "a" || keys[1] != "z" {
		t.Errorf("expected sorted keys ['a', 'z'], got %v", keys)
	}
}

func TestKeySorter_NonRecursive(t *testing.T) {
	proc := &KeySorter{Recursive: false, Enabled: true}

	doc := map[string]interface{}{
		"z": map[string]interface{}{
			"c": 1,
			"a": 2,
		},
		"a": 3,
	}

	result, err := proc.Process(context.Background(), doc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sortedMap, ok := result.(*SortedMap)
	if !ok {
		t.Fatal("expected result to be *SortedMap")
	}
	keys := sortedMap.Keys()

	if keys[0] != "a" || keys[1] != "z" {
		t.Errorf("expected sorted keys ['a', 'z'], got %v", keys)
	}

	// Nested map should NOT be sorted
	nestedVal, _ := sortedMap.Get("z")
	if _, ok := nestedVal.(*SortedMap); ok {
		t.Error("expected nested map to NOT be sorted when Recursive is false")
	}
}

func TestKeySorter_Array(t *testing.T) {
	proc := NewKeySorter(true)

	doc := []interface{}{
		map[string]interface{}{
			"z": 1,
			"a": 2,
		},
	}

	result, err := proc.Process(context.Background(), doc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultSlice, ok := result.([]interface{})
	if !ok {
		t.Fatal("expected result to be []interface{}")
	}
	sortedMap, ok := resultSlice[0].(*SortedMap)
	if !ok {
		t.Fatal("expected array element to be *SortedMap")
	}
	keys := sortedMap.Keys()

	if keys[0] != "a" || keys[1] != "z" {
		t.Errorf("expected sorted keys ['a', 'z'], got %v", keys)
	}
}

func TestDeepCopy(t *testing.T) {
	original := map[string]interface{}{
		"a": 1,
		"b": map[string]interface{}{
			"c": 2,
		},
		"d": []interface{}{1, 2, 3},
	}

	copied := deepCopy(original)

	// Modify original (these assertions are safe because we just created the map)
	origB, ok := original["b"].(map[string]interface{})
	if !ok {
		t.Fatal("expected original 'b' to be map[string]interface{}")
	}
	origB["c"] = 200
	origD, ok := original["d"].([]interface{})
	if !ok {
		t.Fatal("expected original 'd' to be []interface{}")
	}
	origD[0] = 100
	original["a"] = 100

	// Check that copy is unchanged
	copiedMap, ok := copied.(map[string]interface{})
	if !ok {
		t.Fatal("expected copied to be map[string]interface{}")
	}
	if copiedMap["a"] != 1 {
		t.Error("expected copied 'a' to be 1")
	}
	copiedB, ok := copiedMap["b"].(map[string]interface{})
	if !ok {
		t.Fatal("expected copied 'b' to be map[string]interface{}")
	}
	if copiedB["c"] != 2 {
		t.Error("expected copied 'b.c' to be 2")
	}
	copiedD, ok := copiedMap["d"].([]interface{})
	if !ok {
		t.Fatal("expected copied 'd' to be []interface{}")
	}
	if copiedD[0] != 1 {
		t.Error("expected copied 'd[0]' to be 1")
	}
}

func TestGetPath_EmptyPath(t *testing.T) {
	doc := map[string]interface{}{"a": 1}
	result := getPath(doc, "")
	// Check it returns the same map by checking its content
	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be map[string]interface{}")
	}
	if resultMap["a"] != 1 {
		t.Error("expected empty path to return original document")
	}
}

func TestGetPath_SortedMap(t *testing.T) {
	sm := &SortedMap{
		Data: map[string]interface{}{
			"a": map[string]interface{}{
				"b": "value",
			},
		},
	}

	result := getPath(sm, "a.b")
	if result != "value" {
		t.Errorf("expected 'value', got %v", result)
	}
}

func TestSetPath_EmptyPath(t *testing.T) {
	doc := make(map[string]interface{})
	setPath(doc, "", "value")
	if len(doc) != 0 {
		t.Error("expected empty path to do nothing")
	}
}

func TestDeletePath_NestedArray(t *testing.T) {
	doc := map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{
				"name": "item1",
				"val":  "a",
			},
		},
	}

	result := deletePath(doc, "items[0].val")

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be map[string]interface{}")
	}
	items, ok := resultMap["items"].([]interface{})
	if !ok {
		t.Fatal("expected 'items' to be []interface{}")
	}
	item, ok := items[0].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'items[0]' to be map[string]interface{}")
	}

	if _, ok := item["val"]; ok {
		t.Error("expected 'items[0].val' to be deleted")
	}
	if item["name"] != "item1" {
		t.Error("expected 'items[0].name' to remain")
	}
}

func TestPipeline_AddNil(t *testing.T) {
	pipeline := NewPipeline()
	pipeline.Add(nil)

	if pipeline.Count() != 0 {
		t.Error("expected nil processor to not be added")
	}
}

func TestPipeline_EmptyProcess(t *testing.T) {
	pipeline := NewPipeline()

	doc := map[string]interface{}{"a": 1}
	result, err := pipeline.Process(doc)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be map[string]interface{}")
	}
	if resultMap["a"] != 1 {
		t.Error("expected original document when no processors")
	}
}

func TestCherryPickProcessor_SetPaths(t *testing.T) {
	proc := NewCherryPickProcessor(nil)
	proc.SetPaths([]string{"a", "b"})

	if len(proc.Paths) != 2 || proc.Paths[0] != "a" || proc.Paths[1] != "b" {
		t.Errorf("expected paths ['a', 'b'], got %v", proc.Paths)
	}
}

func TestCherryPickProcessor_AddPath(t *testing.T) {
	proc := NewCherryPickProcessor([]string{"a"})
	proc.AddPath("b")

	if len(proc.Paths) != 2 || proc.Paths[1] != "b" {
		t.Errorf("expected paths ['a', 'b'], got %v", proc.Paths)
	}
}

func TestCherryPickProcessor_CherryPickValue(t *testing.T) {
	proc := NewCherryPickProcessor([]string{"a.b"})

	value := map[string]interface{}{
		"a": map[string]interface{}{
			"b": "value",
			"c": "other",
		},
		"d": "ignored",
	}

	result, err := proc.CherryPickValue(value)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be map[string]interface{}")
	}
	aMap, ok := resultMap["a"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'a' to be map[string]interface{}")
	}

	if aMap["b"] != testStrValue {
		t.Errorf("expected 'a.b' = '%s', got %v", testStrValue, aMap["b"])
	}
	if _, ok := aMap["c"]; ok {
		t.Error("expected 'a.c' to not be in result")
	}
}

func TestPruneProcessor_PruneValue(t *testing.T) {
	proc := NewPruneProcessor()

	value := map[string]interface{}{
		"keep":   1,
		"remove": "(( prune ))",
	}

	result, err := proc.PruneValue(value)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be map[string]interface{}")
	}
	if _, ok := resultMap["remove"]; ok {
		t.Error("expected 'remove' to be pruned")
	}
}

func TestInjectProcessor_InjectValue(t *testing.T) {
	proc := NewInjectProcessor()

	value := map[string]interface{}{
		"<<": map[string]interface{}{
			"injected": "value",
		},
		"original": "kept",
	}

	result, err := proc.InjectValue(value)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be map[string]interface{}")
	}
	if resultMap["injected"] != testStrValue {
		t.Errorf("expected 'injected' = '%s', got %v", testStrValue, resultMap["injected"])
	}
}

func TestKeySorter_SortValue(t *testing.T) {
	proc := NewKeySorter(true)

	value := map[string]interface{}{
		"z": 1,
		"a": 2,
	}

	result, err := proc.SortValue(value)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sortedMap, ok := result.(*SortedMap)
	if !ok {
		t.Fatal("expected result to be *SortedMap")
	}
	keys := sortedMap.Keys()

	if keys[0] != "a" || keys[1] != "z" {
		t.Errorf("expected sorted keys ['a', 'z'], got %v", keys)
	}
}

func TestKeySorter_SortValue_Disabled(t *testing.T) {
	proc := NewKeySorter(false)

	value := map[string]interface{}{
		"z": 1,
		"a": 2,
	}

	result, err := proc.SortValue(value)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should return original value when disabled
	if _, ok := result.(*SortedMap); ok {
		t.Error("expected result to NOT be SortedMap when disabled")
	}
}

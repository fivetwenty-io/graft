package parallel

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================================
// DAG Tests
// ============================================================================

func TestDAG_Basic(t *testing.T) {
	dag := NewDAG()

	// Add nodes
	dag.AddNode(&Node{ID: "a"})
	dag.AddNode(&Node{ID: "b", Dependencies: []string{"a"}})
	dag.AddNode(&Node{ID: "c", Dependencies: []string{"a"}})
	dag.AddNode(&Node{ID: "d", Dependencies: []string{"b", "c"}})

	if dag.Size() != 4 {
		t.Errorf("expected size 4, got %d", dag.Size())
	}

	// Test topological sort
	sorted, err := dag.TopologicalSort()
	if err != nil {
		t.Fatalf("TopologicalSort failed: %v", err)
	}

	// Verify order: a must come before b, c; b, c must come before d
	pos := make(map[string]int)
	for i, id := range sorted {
		pos[id] = i
	}

	if pos["a"] > pos["b"] || pos["a"] > pos["c"] {
		t.Error("a should come before b and c")
	}
	if pos["b"] > pos["d"] || pos["c"] > pos["d"] {
		t.Error("b and c should come before d")
	}
}

func TestDAG_Waves(t *testing.T) {
	dag := NewDAG()

	// Diamond pattern: a -> b,c -> d
	dag.AddNode(&Node{ID: "a"})
	dag.AddNode(&Node{ID: "b", Dependencies: []string{"a"}})
	dag.AddNode(&Node{ID: "c", Dependencies: []string{"a"}})
	dag.AddNode(&Node{ID: "d", Dependencies: []string{"b", "c"}})

	waves, err := dag.GetWaves()
	if err != nil {
		t.Fatalf("GetWaves failed: %v", err)
	}

	if len(waves) != 3 {
		t.Errorf("expected 3 waves, got %d", len(waves))
	}

	// Wave 0 should contain only "a"
	if len(waves[0]) != 1 || waves[0][0] != "a" {
		t.Errorf("wave 0 should be [a], got %v", waves[0])
	}

	// Wave 1 should contain "b" and "c"
	if len(waves[1]) != 2 {
		t.Errorf("wave 1 should have 2 elements, got %d", len(waves[1]))
	}

	// Wave 2 should contain only "d"
	if len(waves[2]) != 1 || waves[2][0] != "d" {
		t.Errorf("wave 2 should be [d], got %v", waves[2])
	}
}

func TestDAG_Cycle(t *testing.T) {
	dag := NewDAG()

	dag.AddNode(&Node{ID: "a", Dependencies: []string{"c"}})
	dag.AddNode(&Node{ID: "b", Dependencies: []string{"a"}})
	dag.AddNode(&Node{ID: "c", Dependencies: []string{"b"}})

	if !dag.HasCycle() {
		t.Error("expected cycle to be detected")
	}

	_, err := dag.TopologicalSort()
	if err == nil {
		t.Error("expected error from TopologicalSort with cycle")
	}
}

func TestDAG_AddEdge(t *testing.T) {
	dag := NewDAG()

	dag.AddNode(&Node{ID: "a"})
	dag.AddNode(&Node{ID: "b"})

	if err := dag.AddEdge("b", "a"); err != nil {
		t.Fatalf("AddEdge failed: %v", err)
	}

	deps := dag.GetDependencies("b")
	if len(deps) != 1 || deps[0] != "a" {
		t.Errorf("expected b to depend on a, got %v", deps)
	}
}

func TestDAG_ToDOT(t *testing.T) {
	dag := NewDAG()

	dag.AddNode(&Node{ID: "a", Priority: 1})
	dag.AddNode(&Node{ID: "b", Dependencies: []string{"a"}})

	dot := dag.ToDOT("test")

	if dot == "" {
		t.Error("expected non-empty DOT output")
	}

	// Check it contains expected elements
	if !contains(dot, "digraph test") {
		t.Error("DOT output should contain digraph declaration")
	}
}

func TestDAG_Clone(t *testing.T) {
	dag := NewDAG()
	dag.AddNode(&Node{ID: "a", Data: "test"})
	dag.AddNode(&Node{ID: "b", Dependencies: []string{"a"}})

	clone := dag.Clone()

	// Modify original
	dag.RemoveNode("a")

	// Clone should be unaffected
	if clone.Size() != 2 {
		t.Error("clone should not be affected by changes to original")
	}
}

// ============================================================================
// Worker Pool Tests
// ============================================================================

func TestPool_Basic(t *testing.T) {
	pool, err := NewPool(2, 4)
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}
	defer pool.ShutdownWait()

	var counter atomic.Int32

	for i := 0; i < 10; i++ {
		err := pool.SubmitWait(func(ctx context.Context) error {
			counter.Add(1)
			return nil
		})
		if err != nil {
			t.Errorf("SubmitWait failed: %v", err)
		}
	}

	if counter.Load() != 10 {
		t.Errorf("expected 10 executions, got %d", counter.Load())
	}
}

func TestPool_Submit(t *testing.T) {
	pool, err := NewPool(2, 4)
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}

	var counter atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		err := pool.Submit(func(ctx context.Context) error {
			counter.Add(1)
			wg.Done()
			return nil
		})
		if err != nil {
			t.Errorf("Submit failed: %v", err)
			wg.Done()
		}
	}

	wg.Wait()
	pool.ShutdownWait()

	if counter.Load() != 10 {
		t.Errorf("expected 10 executions, got %d", counter.Load())
	}
}

func TestPool_SubmitBatch(t *testing.T) {
	pool, err := NewPool(2, 8)
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}
	defer pool.ShutdownWait()

	var counter atomic.Int32

	tasks := make([]TaskFunc, 20)
	for i := range tasks {
		tasks[i] = func(ctx context.Context) error {
			counter.Add(1)
			time.Sleep(10 * time.Millisecond)
			return nil
		}
	}

	results := pool.SubmitBatch(tasks)

	for i, err := range results {
		if err != nil {
			t.Errorf("task %d failed: %v", i, err)
		}
	}

	if counter.Load() != 20 {
		t.Errorf("expected 20 executions, got %d", counter.Load())
	}
}

func TestPool_Shutdown(t *testing.T) {
	pool, err := NewPool(2, 4)
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}

	pool.ShutdownWait()

	err = pool.Submit(func(ctx context.Context) error {
		return nil
	})

	if !errors.Is(err, ErrPoolShutdown) {
		t.Errorf("expected ErrPoolShutdown, got %v", err)
	}
}

func TestPool_Context(t *testing.T) {
	pool, err := NewPool(2, 4)
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}
	defer pool.ShutdownWait()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err = pool.SubmitWaitContext(ctx, func(ctx context.Context) error {
		return nil
	})

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestPool_Stats(t *testing.T) {
	pool, err := NewPool(2, 4)
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}
	defer pool.ShutdownWait()

	// Submit some tasks
	for i := 0; i < 5; i++ {
		_ = pool.SubmitWait(func(ctx context.Context) error {
			return nil
		})
	}

	stats := pool.Stats()

	if stats.TasksCompleted != 5 {
		t.Errorf("expected 5 completed, got %d", stats.TasksCompleted)
	}

	if stats.TasksSubmitted != 5 {
		t.Errorf("expected 5 submitted, got %d", stats.TasksSubmitted)
	}
}

func TestPool_ErrorHandling(t *testing.T) {
	pool, err := NewPool(2, 4)
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}
	defer pool.ShutdownWait()

	expectedErr := errors.New("test error")

	err = pool.SubmitWait(func(ctx context.Context) error {
		return expectedErr
	})

	if !errors.Is(err, expectedErr) {
		t.Errorf("expected test error, got %v", err)
	}

	stats := pool.Stats()
	if stats.TasksFailed != 1 {
		t.Errorf("expected 1 failed, got %d", stats.TasksFailed)
	}
}

// ============================================================================
// Rate Limiter Tests
// ============================================================================

func TestTokenBucket_Basic(t *testing.T) {
	// 10 tokens per second, burst of 5
	tb := NewTokenBucket(10, 5)
	defer tb.Stop()

	// Should be able to acquire burst immediately
	for i := 0; i < 5; i++ {
		if !tb.TryAcquire() {
			t.Errorf("expected to acquire token %d", i)
		}
	}

	// Next should fail immediately
	if tb.TryAcquire() {
		t.Error("expected to fail acquiring 6th token")
	}
}

func TestTokenBucket_Wait(t *testing.T) {
	// 100 tokens per second
	tb := NewTokenBucket(100, 1)
	defer tb.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Acquire the one available token
	if err := tb.Wait(ctx); err != nil {
		t.Fatalf("first Wait failed: %v", err)
	}

	// Second should wait but complete within timeout
	if err := tb.Wait(ctx); err != nil {
		t.Fatalf("second Wait failed: %v", err)
	}
}

func TestTokenBucket_ContextCancel(t *testing.T) {
	tb := NewTokenBucket(0.1, 1) // Very slow rate
	defer tb.Stop()

	// Drain the bucket
	tb.TryAcquire()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := tb.Wait(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestSlidingWindowLimiter(t *testing.T) {
	sw := NewSlidingWindowLimiter(5, 100*time.Millisecond)
	defer sw.Stop()

	// Should allow 5 requests
	for i := 0; i < 5; i++ {
		if !sw.TryAcquire() {
			t.Errorf("expected to allow request %d", i)
		}
	}

	// 6th should fail
	if sw.TryAcquire() {
		t.Error("expected 6th request to be rejected")
	}

	// Wait for window to slide
	time.Sleep(150 * time.Millisecond)

	// Should allow again
	if !sw.TryAcquire() {
		t.Error("expected request to be allowed after window")
	}
}

func TestConcurrencyLimiter(t *testing.T) {
	cl := NewConcurrencyLimiter(2)
	defer cl.Stop()

	// Acquire 2 slots
	cl.TryAcquire()
	cl.TryAcquire()

	if cl.Available() != 0 {
		t.Errorf("expected 0 available, got %d", cl.Available())
	}

	// 3rd should fail
	if cl.TryAcquire() {
		t.Error("expected 3rd acquire to fail")
	}

	// Release one
	cl.Release()

	if cl.Available() != 1 {
		t.Errorf("expected 1 available, got %d", cl.Available())
	}
}

// ============================================================================
// Scheduler Tests
// ============================================================================

func TestScheduler_Basic(t *testing.T) {
	s := NewScheduler()

	_ = s.AddTask(&Task{ID: "a"})
	_ = s.AddTask(&Task{ID: "b", Dependencies: []string{"a"}})
	_ = s.AddTask(&Task{ID: "c", Dependencies: []string{"a"}})
	_ = s.AddTask(&Task{ID: "d", Dependencies: []string{"b", "c"}})

	waves, err := s.Schedule()
	if err != nil {
		t.Fatalf("Schedule failed: %v", err)
	}

	if len(waves) != 3 {
		t.Errorf("expected 3 waves, got %d", len(waves))
	}
}

func TestScheduler_Cycle(t *testing.T) {
	s := NewScheduler()

	_ = s.AddTask(&Task{ID: "a", Dependencies: []string{"b"}})
	_ = s.AddTask(&Task{ID: "b", Dependencies: []string{"a"}})

	if !s.HasCycle() {
		t.Error("expected cycle to be detected")
	}

	_, err := s.Schedule()
	if !errors.Is(err, ErrCycleDetected) {
		t.Errorf("expected ErrCycleDetected, got %v", err)
	}
}

func TestScheduler_Execute(t *testing.T) {
	pool, err := NewPool(2, 4)
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}
	defer pool.ShutdownWait()

	s := NewScheduler()

	var order []string
	var mu sync.Mutex

	_ = s.AddTask(&Task{
		ID: "a",
		Execute: func(ctx context.Context) error {
			mu.Lock()
			order = append(order, "a")
			mu.Unlock()
			return nil
		},
	})
	_ = s.AddTask(&Task{
		ID:           "b",
		Dependencies: []string{"a"},
		Execute: func(ctx context.Context) error {
			mu.Lock()
			order = append(order, "b")
			mu.Unlock()
			return nil
		},
	})

	results, err := s.Execute(context.Background(), pool)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}

	// Check order
	mu.Lock()
	if len(order) != 2 || order[0] != "a" {
		t.Errorf("expected a to run first, got %v", order)
	}
	mu.Unlock()
}

func TestScheduler_ExecuteSequential(t *testing.T) {
	s := NewScheduler()

	var counter int

	_ = s.AddTask(&Task{
		ID: "a",
		Execute: func(ctx context.Context) error {
			counter++
			return nil
		},
	})
	_ = s.AddTask(&Task{
		ID:           "b",
		Dependencies: []string{"a"},
		Execute: func(ctx context.Context) error {
			counter++
			return nil
		},
	})

	results, err := s.ExecuteSequential(context.Background())
	if err != nil {
		t.Fatalf("ExecuteSequential failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}

	if counter != 2 {
		t.Errorf("expected counter=2, got %d", counter)
	}
}

func TestScheduler_Priority(t *testing.T) {
	s := NewScheduler()

	// All tasks have no dependencies, so priority should determine order within wave
	_ = s.AddTask(&Task{ID: "low", Priority: 1})
	_ = s.AddTask(&Task{ID: "high", Priority: 10})
	_ = s.AddTask(&Task{ID: "medium", Priority: 5})

	waves, err := s.Schedule()
	if err != nil {
		t.Fatalf("Schedule failed: %v", err)
	}

	if len(waves) != 1 {
		t.Fatalf("expected 1 wave, got %d", len(waves))
	}

	// High priority should come first
	if waves[0][0].ID != "high" {
		t.Errorf("expected high priority first, got %s", waves[0][0].ID)
	}
}

func TestScheduler_CriticalPath(t *testing.T) {
	s := NewScheduler()

	// Create a diamond pattern
	_ = s.AddTask(&Task{ID: "a"})
	_ = s.AddTask(&Task{ID: "b", Dependencies: []string{"a"}})
	_ = s.AddTask(&Task{ID: "c", Dependencies: []string{"a"}})
	_ = s.AddTask(&Task{ID: "d", Dependencies: []string{"b", "c"}})

	path, err := s.CriticalPath()
	if err != nil {
		t.Fatalf("CriticalPath failed: %v", err)
	}

	// Path should be a -> b or c -> d (length 3)
	if len(path) != 3 {
		t.Errorf("expected path length 3, got %d: %v", len(path), path)
	}

	if path[0] != "a" {
		t.Errorf("path should start with a, got %s", path[0])
	}

	if path[2] != "d" {
		t.Errorf("path should end with d, got %s", path[2])
	}
}

// ============================================================================
// Monitor Tests
// ============================================================================

func TestMonitor_Basic(t *testing.T) {
	m := NewMonitor(nil)

	stats := m.Stats()

	if stats.NumCPU != runtime.NumCPU() {
		t.Errorf("expected NumCPU=%d, got %d", runtime.NumCPU(), stats.NumCPU)
	}

	if stats.NumGoroutine <= 0 {
		t.Error("expected positive NumGoroutine")
	}
}

func TestMonitor_WithPool(t *testing.T) {
	pool, err := NewPool(2, 4)
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}
	defer pool.ShutdownWait()

	m := NewMonitor(nil)
	m.ConnectPool(pool)
	m.Start()
	defer m.Stop()

	// Submit some tasks
	for i := 0; i < 5; i++ {
		_ = pool.Submit(func(ctx context.Context) error {
			time.Sleep(10 * time.Millisecond)
			return nil
		})
	}

	time.Sleep(50 * time.Millisecond)

	stats := m.Stats()

	// Pool should be connected
	if stats.QueueCapacity == 0 {
		t.Error("expected pool to be connected")
	}
}

func TestMonitor_HealthCheck(t *testing.T) {
	m := NewMonitor(nil)

	issues := m.HealthCheck()

	// Under normal conditions, should have no critical issues
	for _, issue := range issues {
		if contains(issue, "critical") {
			t.Logf("Health issue: %s", issue)
		}
	}
}

func TestMonitor_Recommendation(t *testing.T) {
	m := NewMonitor(&MonitorConfig{
		SampleInterval:     10 * time.Millisecond,
		HistorySize:        5,
		CPUHighThreshold:   0.8,
		CPULowThreshold:    0.3,
		MemHighThreshold:   0.9,
		QueueHighThreshold: 0.8,
		QueueLowThreshold:  0.2,
	})

	rec, reason := m.GetRecommendation()

	// Should return a valid recommendation
	if rec < ScaleHold || rec > ScaleDown {
		t.Errorf("invalid recommendation: %d", rec)
	}

	if reason == "" {
		t.Error("expected non-empty reason")
	}
}

// ============================================================================
// Concurrency Tests
// ============================================================================

func TestDAG_Concurrent(t *testing.T) {
	dag := NewDAG()

	var wg sync.WaitGroup

	// Concurrent adds
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			dag.AddNode(&Node{ID: string(rune('a'+id%26)) + string(rune('0'+id/26))})
		}(i)
	}

	wg.Wait()

	if dag.Size() != 100 {
		t.Errorf("expected 100 nodes, got %d", dag.Size())
	}
}

func TestPool_Concurrent(t *testing.T) {
	pool, err := NewPool(4, 16)
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}
	defer pool.ShutdownWait()

	var counter atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = pool.SubmitWait(func(ctx context.Context) error {
				counter.Add(1)
				return nil
			})
		}()
	}

	wg.Wait()

	if counter.Load() != 1000 {
		t.Errorf("expected 1000 executions, got %d", counter.Load())
	}
}

// ============================================================================
// Benchmarks
// ============================================================================

func BenchmarkDAG_AddNode(b *testing.B) {
	dag := NewDAG()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dag.AddNode(&Node{ID: string(rune(i))})
	}
}

func BenchmarkDAG_TopologicalSort(b *testing.B) {
	dag := NewDAG()

	// Create a chain of 100 nodes
	for i := 0; i < 100; i++ {
		deps := []string{}
		if i > 0 {
			deps = []string{string(rune(i - 1))}
		}
		dag.AddNode(&Node{ID: string(rune(i)), Dependencies: deps})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dag.invalidateCache()
		_, _ = dag.TopologicalSort()
	}
}

func BenchmarkDAG_GetWaves(b *testing.B) {
	dag := NewDAG()

	// Create a diamond pattern
	for i := 0; i < 100; i++ {
		deps := []string{}
		if i > 0 && i%10 != 0 {
			deps = []string{string(rune(i - 1))}
		}
		dag.AddNode(&Node{ID: string(rune(i)), Dependencies: deps})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dag.invalidateCache()
		_, _ = dag.GetWaves()
	}
}

func BenchmarkPool_Submit(b *testing.B) {
	pool, _ := NewPool(4, 16)
	defer pool.ShutdownWait()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = pool.Submit(func(ctx context.Context) error {
			return nil
		})
	}
}

func BenchmarkPool_SubmitWait(b *testing.B) {
	pool, _ := NewPool(4, 16)
	defer pool.ShutdownWait()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = pool.SubmitWait(func(ctx context.Context) error {
			return nil
		})
	}
}

func BenchmarkTokenBucket_TryAcquire(b *testing.B) {
	tb := NewTokenBucket(float64(b.N)*10, b.N)
	defer tb.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tb.TryAcquire()
	}
}

func BenchmarkScheduler_Schedule(b *testing.B) {
	s := NewScheduler()

	// Create a complex task graph
	for i := 0; i < 100; i++ {
		deps := []string{}
		if i > 0 {
			deps = append(deps, string(rune(i-1)))
		}
		if i > 10 {
			deps = append(deps, string(rune(i-10)))
		}
		_ = s.AddTask(&Task{ID: string(rune(i)), Dependencies: deps})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = s.Schedule()
	}
}

// Helper function.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || s != "" && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

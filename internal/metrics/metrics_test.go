package metrics

import (
	"math"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// Labels Tests
// =============================================================================

func TestNewLabels(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected Labels
	}{
		{
			name:     "empty",
			input:    []string{},
			expected: Labels{},
		},
		{
			name:     "single pair",
			input:    []string{"method", "GET"},
			expected: Labels{"method": "GET"},
		},
		{
			name:     "multiple pairs",
			input:    []string{"method", "GET", "status", "200"},
			expected: Labels{"method": "GET", "status": "200"},
		},
		{
			name:     "odd number truncates",
			input:    []string{"method", "GET", "orphan"},
			expected: Labels{"method": "GET"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			labels := NewLabels(tt.input...)
			if !labels.Equal(tt.expected) {
				t.Errorf("NewLabels(%v) = %v, want %v", tt.input, labels, tt.expected)
			}
		})
	}
}

func TestLabelsClone(t *testing.T) {
	original := Labels{"key": "value"}
	clone := original.Clone()

	// Modify clone
	clone["key"] = "modified"

	// Original should be unchanged
	if original["key"] != "value" {
		t.Errorf("Clone() modified original: got %v, want %v", original["key"], "value")
	}
}

func TestLabelsWith(t *testing.T) {
	original := Labels{"a": "1"}
	withB := original.With("b", "2")

	// Original should be unchanged
	if _, ok := original["b"]; ok {
		t.Error("With() modified original")
	}

	// New labels should have both
	if withB["a"] != "1" || withB["b"] != "2" {
		t.Errorf("With() = %v, want {a:1, b:2}", withB)
	}
}

func TestLabelsMerge(t *testing.T) {
	a := Labels{"a": "1", "b": "2"}
	b := Labels{"b": "3", "c": "4"}
	merged := a.Merge(b)

	expected := Labels{"a": "1", "b": "3", "c": "4"}
	if !merged.Equal(expected) {
		t.Errorf("Merge() = %v, want %v", merged, expected)
	}
}

func TestLabelsString(t *testing.T) {
	labels := Labels{"method": "GET", "status": "200"}
	expected := "method=GET,status=200"
	if labels.String() != expected {
		t.Errorf("String() = %v, want %v", labels.String(), expected)
	}
}

func TestLabelsKeys(t *testing.T) {
	labels := Labels{"b": "2", "a": "1", "c": "3"}
	keys := labels.Keys()

	expected := []string{"a", "b", "c"}
	if len(keys) != len(expected) {
		t.Fatalf("Keys() length = %d, want %d", len(keys), len(expected))
	}
	for i, k := range keys {
		if k != expected[i] {
			t.Errorf("Keys()[%d] = %v, want %v", i, k, expected[i])
		}
	}
}

// =============================================================================
// Counter Tests
// =============================================================================

func TestCounterBasic(t *testing.T) {
	counter := NewCounter("test_counter", Labels{"env": "test"})

	// Initial value should be 0
	if counter.Get() != 0 {
		t.Errorf("initial value = %v, want 0", counter.Get())
	}

	// Test Inc
	counter.Inc()
	if counter.Get() != 1 {
		t.Errorf("after Inc() = %v, want 1", counter.Get())
	}

	// Test Add
	counter.Add(5)
	if counter.Get() != 6 {
		t.Errorf("after Add(5) = %v, want 6", counter.Get())
	}

	// Test negative Add (should be ignored)
	counter.Add(-1)
	if counter.Get() != 6 {
		t.Errorf("after Add(-1) = %v, want 6 (unchanged)", counter.Get())
	}

	// Test Name and Type
	if counter.Name() != "test_counter" {
		t.Errorf("Name() = %v, want test_counter", counter.Name())
	}
	if counter.Type() != MetricTypeCounter {
		t.Errorf("Type() = %v, want counter", counter.Type())
	}

	// Test Labels
	if !counter.Labels().Equal(Labels{"env": "test"}) {
		t.Errorf("Labels() = %v, want {env: test}", counter.Labels())
	}

	// Test Value interface
	if v, ok := counter.Value().(float64); !ok || v != 6 {
		t.Errorf("Value() = %v, want 6", counter.Value())
	}

	// Test Reset
	counter.Reset()
	if counter.Get() != 0 {
		t.Errorf("after Reset() = %v, want 0", counter.Get())
	}
}

func TestCounterConcurrency(t *testing.T) {
	counter := NewCounter("concurrent_counter", nil)
	var wg sync.WaitGroup
	iterations := 1000
	goroutines := 10

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				counter.Inc()
			}
		}()
	}

	wg.Wait()

	expected := float64(goroutines * iterations)
	if counter.Get() != expected {
		t.Errorf("concurrent counter = %v, want %v", counter.Get(), expected)
	}
}

func TestCounterVec(t *testing.T) {
	vec := NewCounterVec("http_requests", []string{"method", "status"})

	// Get counter with labels
	counter1 := vec.WithLabelValues("GET", "200")
	counter1.Inc()

	counter2 := vec.WithLabelValues("POST", "201")
	counter2.Add(5)

	// Same labels should return same counter
	counter3 := vec.WithLabelValues("GET", "200")
	if counter3.Get() != 1 {
		t.Errorf("WithLabelValues returned different counter: got %v, want 1", counter3.Get())
	}

	// Collect all counters
	counters := vec.Collect()
	if len(counters) != 2 {
		t.Errorf("Collect() returned %d counters, want 2", len(counters))
	}

	// Wrong label count should return a no-op counter
	counter4 := vec.WithLabelValues("GET")
	counter4.Inc()
	// This should not affect the vec
	counters = vec.Collect()
	if len(counters) != 2 {
		t.Errorf("Wrong label count affected vec: %d counters", len(counters))
	}
}

// =============================================================================
// Gauge Tests
// =============================================================================

func TestGaugeBasic(t *testing.T) {
	gauge := NewGauge("test_gauge", Labels{"env": "test"})

	// Initial value should be 0
	if gauge.Get() != 0 {
		t.Errorf("initial value = %v, want 0", gauge.Get())
	}

	// Test Set
	gauge.Set(42)
	if gauge.Get() != 42 {
		t.Errorf("after Set(42) = %v, want 42", gauge.Get())
	}

	// Test Inc
	gauge.Inc()
	if gauge.Get() != 43 {
		t.Errorf("after Inc() = %v, want 43", gauge.Get())
	}

	// Test Dec
	gauge.Dec()
	if gauge.Get() != 42 {
		t.Errorf("after Dec() = %v, want 42", gauge.Get())
	}

	// Test Add
	gauge.Add(8)
	if gauge.Get() != 50 {
		t.Errorf("after Add(8) = %v, want 50", gauge.Get())
	}

	// Test Sub
	gauge.Sub(10)
	if gauge.Get() != 40 {
		t.Errorf("after Sub(10) = %v, want 40", gauge.Get())
	}

	// Test negative values
	gauge.Set(-5)
	if gauge.Get() != -5 {
		t.Errorf("after Set(-5) = %v, want -5", gauge.Get())
	}

	// Test Name and Type
	if gauge.Name() != "test_gauge" {
		t.Errorf("Name() = %v, want test_gauge", gauge.Name())
	}
	if gauge.Type() != MetricTypeGauge {
		t.Errorf("Type() = %v, want gauge", gauge.Type())
	}
}

func TestGaugeConcurrency(t *testing.T) {
	gauge := NewGauge("concurrent_gauge", nil)
	var wg sync.WaitGroup
	iterations := 1000
	goroutines := 10

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				gauge.Inc()
			}
		}()
	}

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				gauge.Dec()
			}
		}()
	}

	wg.Wait()

	// After equal increments and decrements, should be 0
	if gauge.Get() != 0 {
		t.Errorf("concurrent gauge = %v, want 0", gauge.Get())
	}
}

func TestGaugeVec(t *testing.T) {
	vec := NewGaugeVec("memory_usage", []string{"type"})

	gauge1 := vec.WithLabelValues("heap")
	gauge1.Set(1024)

	gauge2 := vec.WithLabelValues("stack")
	gauge2.Set(512)

	// Collect all gauges
	gauges := vec.Collect()
	if len(gauges) != 2 {
		t.Errorf("Collect() returned %d gauges, want 2", len(gauges))
	}
}

// =============================================================================
// Histogram Tests
// =============================================================================

func TestHistogramBasic(t *testing.T) {
	hist := NewHistogram("request_duration", Labels{"endpoint": "/api"}, nil)

	// Initial values
	if hist.Count() != 0 {
		t.Errorf("initial count = %v, want 0", hist.Count())
	}
	if hist.Sum() != 0 {
		t.Errorf("initial sum = %v, want 0", hist.Sum())
	}

	// Observe some values
	hist.Observe(0.005)
	hist.Observe(0.01)
	hist.Observe(0.1)
	hist.Observe(1.0)
	hist.Observe(5.0)

	// Check count and sum
	if hist.Count() != 5 {
		t.Errorf("count = %v, want 5", hist.Count())
	}
	expectedSum := 0.005 + 0.01 + 0.1 + 1.0 + 5.0
	if math.Abs(hist.Sum()-expectedSum) > 0.0001 {
		t.Errorf("sum = %v, want %v", hist.Sum(), expectedSum)
	}

	// Check buckets
	buckets := hist.Buckets()

	// 0.005 should be in the first bucket
	if buckets[0.005] < 1 {
		t.Errorf("bucket[0.005] = %v, want >= 1", buckets[0.005])
	}

	// All values should be in +Inf bucket
	if buckets[math.Inf(1)] != 5 {
		t.Errorf("bucket[+Inf] = %v, want 5", buckets[math.Inf(1)])
	}

	// Test Name and Type
	if hist.Name() != "request_duration" {
		t.Errorf("Name() = %v, want request_duration", hist.Name())
	}
	if hist.Type() != MetricTypeHistogram {
		t.Errorf("Type() = %v, want histogram", hist.Type())
	}

	// Test Mean
	expectedMean := expectedSum / 5
	if math.Abs(hist.Mean()-expectedMean) > 0.0001 {
		t.Errorf("Mean() = %v, want %v", hist.Mean(), expectedMean)
	}
}

func TestHistogramCustomBuckets(t *testing.T) {
	buckets := []float64{1, 5, 10, 50, 100}
	hist := NewHistogram("custom_histogram", nil, buckets)

	hist.Observe(0.5) // bucket 1
	hist.Observe(3)   // bucket 5
	hist.Observe(7)   // bucket 10
	hist.Observe(25)  // bucket 50
	hist.Observe(75)  // bucket 100
	hist.Observe(200) // bucket +Inf

	histBuckets := hist.Buckets()

	// Check cumulative counts
	if histBuckets[1] != 1 {
		t.Errorf("bucket[1] = %v, want 1", histBuckets[1])
	}
	if histBuckets[5] != 2 {
		t.Errorf("bucket[5] = %v, want 2", histBuckets[5])
	}
	if histBuckets[10] != 3 {
		t.Errorf("bucket[10] = %v, want 3", histBuckets[10])
	}
	if histBuckets[50] != 4 {
		t.Errorf("bucket[50] = %v, want 4", histBuckets[50])
	}
	if histBuckets[100] != 5 {
		t.Errorf("bucket[100] = %v, want 5", histBuckets[100])
	}
	if histBuckets[math.Inf(1)] != 6 {
		t.Errorf("bucket[+Inf] = %v, want 6", histBuckets[math.Inf(1)])
	}
}

func TestHistogramConcurrency(t *testing.T) {
	hist := NewHistogram("concurrent_histogram", nil, nil)
	var wg sync.WaitGroup
	iterations := 1000
	goroutines := 10

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				hist.Observe(1.0)
			}
		}()
	}

	wg.Wait()

	expected := uint64(goroutines * iterations)
	if hist.Count() != expected {
		t.Errorf("concurrent histogram count = %v, want %v", hist.Count(), expected)
	}

	expectedSum := float64(goroutines * iterations)
	if hist.Sum() != expectedSum {
		t.Errorf("concurrent histogram sum = %v, want %v", hist.Sum(), expectedSum)
	}
}

func TestHistogramTimer(t *testing.T) {
	hist := NewHistogram("timer_test", nil, nil)

	// Save original timeNow and restore after test
	origTimeNow := timeNow
	defer func() { timeNow = origTimeNow }()

	// Mock time
	mockTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	timeNow = func() time.Time { return mockTime }

	timer := hist.Timer()

	// Advance mock time by 100ms
	mockTime = mockTime.Add(100 * time.Millisecond)

	duration := timer.ObserveDuration()

	if duration < 100*time.Millisecond || duration > 200*time.Millisecond {
		t.Errorf("Timer duration = %v, want ~100ms", duration)
	}

	if hist.Count() != 1 {
		t.Errorf("Timer did not record observation: count = %v", hist.Count())
	}
}

func TestHistogramVec(t *testing.T) {
	vec := NewHistogramVec("http_request_duration", []string{"method"}, nil)

	hist1 := vec.WithLabelValues("GET")
	hist1.Observe(0.1)
	hist1.Observe(0.2)

	hist2 := vec.WithLabelValues("POST")
	hist2.Observe(0.5)

	// Collect all histograms
	histograms := vec.Collect()
	if len(histograms) != 2 {
		t.Errorf("Collect() returned %d histograms, want 2", len(histograms))
	}
}

// =============================================================================
// Bucket Helper Tests
// =============================================================================

func TestLinearBuckets(t *testing.T) {
	buckets := LinearBuckets(0, 10, 5)
	expected := []float64{0, 10, 20, 30, 40}

	if len(buckets) != len(expected) {
		t.Fatalf("LinearBuckets length = %d, want %d", len(buckets), len(expected))
	}

	for i, b := range buckets {
		if b != expected[i] {
			t.Errorf("LinearBuckets[%d] = %v, want %v", i, b, expected[i])
		}
	}
}

func TestExponentialBuckets(t *testing.T) {
	buckets := ExponentialBuckets(1, 2, 5)
	expected := []float64{1, 2, 4, 8, 16}

	if len(buckets) != len(expected) {
		t.Fatalf("ExponentialBuckets length = %d, want %d", len(buckets), len(expected))
	}

	for i, b := range buckets {
		if b != expected[i] {
			t.Errorf("ExponentialBuckets[%d] = %v, want %v", i, b, expected[i])
		}
	}
}

// =============================================================================
// Registry Tests
// =============================================================================

func TestRegistryBasic(t *testing.T) {
	registry := NewRegistry()

	counter := NewCounter("my_counter", nil)
	err := registry.Register("my_counter", counter)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	// Try to register same name again
	err = registry.Register("my_counter", NewCounter("my_counter", nil))
	if err == nil {
		t.Error("Register() should fail for duplicate name")
	}

	// Get metric
	m := registry.Get("my_counter")
	if m != counter {
		t.Error("Get() returned wrong metric")
	}

	// Get non-existent metric
	m = registry.Get("nonexistent")
	if m != nil {
		t.Error("Get() should return nil for non-existent metric")
	}

	// Unregister
	registry.Unregister("my_counter")
	m = registry.Get("my_counter")
	if m != nil {
		t.Error("Unregister() did not remove metric")
	}
}

func TestRegistryMustRegister(t *testing.T) {
	registry := NewRegistry()

	// Should not panic
	registry.MustRegister("counter1", NewCounter("counter1", nil))

	// Should panic
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustRegister() should panic for duplicate name")
		}
	}()
	registry.MustRegister("counter1", NewCounter("counter1", nil))
}

func TestRegistryGetOrCreate(t *testing.T) {
	registry := NewRegistry()

	// GetOrCreateCounter
	counter1 := registry.GetOrCreateCounter("requests", Labels{"method": "GET"})
	counter1.Inc()

	counter2 := registry.GetOrCreateCounter("requests", Labels{"method": "GET"})
	if counter2.Get() != 1 {
		t.Errorf("GetOrCreateCounter returned different counter")
	}

	// GetOrCreateGauge
	gauge1 := registry.GetOrCreateGauge("connections", Labels{"type": "active"})
	gauge1.Set(10)

	gauge2 := registry.GetOrCreateGauge("connections", Labels{"type": "active"})
	if gauge2.Get() != 10 {
		t.Errorf("GetOrCreateGauge returned different gauge")
	}

	// GetOrCreateHistogram
	hist1 := registry.GetOrCreateHistogram("latency", Labels{"endpoint": "/api"}, nil)
	hist1.Observe(0.5)

	hist2 := registry.GetOrCreateHistogram("latency", Labels{"endpoint": "/api"}, nil)
	if hist2.Count() != 1 {
		t.Errorf("GetOrCreateHistogram returned different histogram")
	}
}

func TestRegistryAll(t *testing.T) {
	registry := NewRegistry()

	// Add some metrics
	registry.MustRegister("counter1", NewCounter("counter1", nil))
	registry.MustRegister("gauge1", NewGauge("gauge1", nil))

	// Use GetOrCreate to add labeled metrics
	registry.GetOrCreateCounter("labeled_counter", Labels{"env": "prod"})
	registry.GetOrCreateCounter("labeled_counter", Labels{"env": "dev"})

	metrics := registry.All()
	if len(metrics) < 4 {
		t.Errorf("All() returned %d metrics, want at least 4", len(metrics))
	}
}

func TestRegistryReset(t *testing.T) {
	registry := NewRegistry()

	registry.MustRegister("counter1", NewCounter("counter1", nil))
	registry.GetOrCreateCounter("counter2", Labels{"env": "test"})

	registry.Reset()

	if len(registry.All()) != 0 {
		t.Error("Reset() did not clear all metrics")
	}
}

func TestRegistrySnapshot(t *testing.T) {
	registry := NewRegistry()

	counter := NewCounter("requests", Labels{"method": "GET"})
	counter.Inc()
	counter.Inc()
	registry.MustRegister("requests", counter)

	gauge := NewGauge("connections", nil)
	gauge.Set(42)
	registry.MustRegister("connections", gauge)

	snapshot := registry.Snapshot()

	// Check that snapshot contains expected values
	if len(snapshot) < 2 {
		t.Errorf("Snapshot has %d entries, want at least 2", len(snapshot))
	}
}

func TestRegistryConcurrency(t *testing.T) {
	registry := NewRegistry()
	var wg sync.WaitGroup
	iterations := 100
	goroutines := 10

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				counter := registry.GetOrCreateCounter("concurrent_counter", Labels{"id": "1"})
				counter.Inc()
			}
		}()
	}

	wg.Wait()

	counter := registry.GetOrCreateCounter("concurrent_counter", Labels{"id": "1"})
	expected := float64(goroutines * iterations)
	if counter.Get() != expected {
		t.Errorf("concurrent counter = %v, want %v", counter.Get(), expected)
	}
}

// =============================================================================
// Default Registry Tests
// =============================================================================

func TestDefaultRegistry(t *testing.T) {
	// Reset default registry before test
	ResetDefaultRegistry()
	defer ResetDefaultRegistry()

	// Test Register
	counter := NewCounter("default_counter", nil)
	err := Register("default_counter", counter)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	// Test Get
	m := Get("default_counter")
	if m != counter {
		t.Error("Get() returned wrong metric")
	}

	// Test GetOrCreateCounter
	c := GetOrCreateCounter("test_counter", Labels{"env": "test"})
	c.Inc()
	if c.Get() != 1 {
		t.Errorf("GetOrCreateCounter().Get() = %v, want 1", c.Get())
	}

	// Test GetOrCreateGauge
	g := GetOrCreateGauge("test_gauge", Labels{"type": "memory"})
	g.Set(100)
	if g.Get() != 100 {
		t.Errorf("GetOrCreateGauge().Get() = %v, want 100", g.Get())
	}

	// Test GetOrCreateHistogram
	h := GetOrCreateHistogram("test_histogram", Labels{"endpoint": "/api"}, nil)
	h.Observe(0.5)
	if h.Count() != 1 {
		t.Errorf("GetOrCreateHistogram().Count() = %v, want 1", h.Count())
	}

	// Test All
	metrics := All()
	if len(metrics) < 4 {
		t.Errorf("All() returned %d metrics, want at least 4", len(metrics))
	}

	// Test Unregister
	Unregister("default_counter")
	if Get("default_counter") != nil {
		t.Error("Unregister() did not remove metric")
	}
}

// =============================================================================
// Sample Tests
// =============================================================================

func TestNewSample(t *testing.T) {
	labels := Labels{"env": "test"}
	sample := NewSample(42.5, labels)

	if sample.Value != 42.5 {
		t.Errorf("Sample.Value = %v, want 42.5", sample.Value)
	}

	if sample.Timestamp.IsZero() {
		t.Error("Sample.Timestamp should not be zero")
	}

	if !sample.Labels.Equal(labels) {
		t.Errorf("Sample.Labels = %v, want %v", sample.Labels, labels)
	}
}

// =============================================================================
// HistogramValue Tests
// =============================================================================

func TestHistogramValue(t *testing.T) {
	hist := NewHistogram("test_hist", nil, []float64{1, 5, 10})
	hist.Observe(0.5)
	hist.Observe(3)
	hist.Observe(7)
	hist.Observe(15)

	value, ok := hist.Value().(HistogramValue)
	if !ok {
		t.Fatal("hist.Value() is not a HistogramValue")
	}

	if value.Count != 4 {
		t.Errorf("HistogramValue.Count = %v, want 4", value.Count)
	}

	expectedSum := 0.5 + 3 + 7 + 15
	if math.Abs(value.Sum-expectedSum) > 0.0001 {
		t.Errorf("HistogramValue.Sum = %v, want %v", value.Sum, expectedSum)
	}

	// Check bucket counts (cumulative)
	if value.Buckets[1] != 1 {
		t.Errorf("HistogramValue.Buckets[1] = %v, want 1", value.Buckets[1])
	}
	if value.Buckets[5] != 2 {
		t.Errorf("HistogramValue.Buckets[5] = %v, want 2", value.Buckets[5])
	}
	if value.Buckets[10] != 3 {
		t.Errorf("HistogramValue.Buckets[10] = %v, want 3", value.Buckets[10])
	}
	if value.Buckets[math.Inf(1)] != 4 {
		t.Errorf("HistogramValue.Buckets[+Inf] = %v, want 4", value.Buckets[math.Inf(1)])
	}
}

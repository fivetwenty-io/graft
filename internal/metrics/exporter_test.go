package metrics

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// TestNewExporter tests the exporter factory function.
func TestNewExporter(t *testing.T) {
	tests := []struct {
		name        string
		format      Format
		wantType    string
		wantErr     bool
		contentType string
	}{
		{
			name:        "prometheus format",
			format:      FormatPrometheus,
			wantType:    "*metrics.PrometheusExporter",
			contentType: "text/plain; version=0.0.4; charset=utf-8",
		},
		{
			name:        "otel format",
			format:      FormatOtel,
			wantType:    "*metrics.OtelExporter",
			contentType: "application/json",
		},
		{
			name:        "json format",
			format:      FormatJSON,
			wantType:    "*metrics.JSONExporter",
			contentType: "application/json",
		},
		{
			name:        "text format",
			format:      FormatText,
			wantType:    "*metrics.TextExporter",
			contentType: "text/plain",
		},
		{
			name:    "invalid format",
			format:  Format("invalid"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := NewExporter(tt.format)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if e.ContentType() != tt.contentType {
				t.Errorf("content type = %q, want %q", e.ContentType(), tt.contentType)
			}
		})
	}
}

// TestMustNewExporter tests that MustNewExporter panics on invalid format.
func TestMustNewExporter(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic, got none")
		}
	}()

	MustNewExporter(Format("invalid"))
}

// TestPrometheusExporter tests Prometheus format export.
func TestPrometheusExporter(t *testing.T) {
	exporter := NewPrometheusExporter()

	t.Run("counter export", func(t *testing.T) {
		counter := NewCounter("http_requests_total", Labels{"method": "GET", "status": "200"})
		counter.Add(100)

		output, err := exporter.Export([]Metric{counter})
		if err != nil {
			t.Fatalf("export error: %v", err)
		}

		outputStr := string(output)

		// Check for required elements
		if !strings.Contains(outputStr, "# HELP http_requests_total") {
			t.Error("missing HELP comment")
		}
		if !strings.Contains(outputStr, "# TYPE http_requests_total counter") {
			t.Error("missing TYPE comment")
		}
		if !strings.Contains(outputStr, `http_requests_total{method="GET",status="200"} 100`) {
			t.Errorf("unexpected output: %s", outputStr)
		}
	})

	t.Run("gauge export", func(t *testing.T) {
		gauge := NewGauge("temperature_celsius", Labels{"location": "server_room"})
		gauge.Set(23.5)

		output, err := exporter.Export([]Metric{gauge})
		if err != nil {
			t.Fatalf("export error: %v", err)
		}

		outputStr := string(output)

		if !strings.Contains(outputStr, "# TYPE temperature_celsius gauge") {
			t.Error("missing TYPE comment")
		}
		if !strings.Contains(outputStr, `temperature_celsius{location="server_room"} 23.5`) {
			t.Errorf("unexpected output: %s", outputStr)
		}
	})

	t.Run("histogram export", func(t *testing.T) {
		histogram := NewHistogram("request_duration_seconds", nil, []float64{0.01, 0.05, 0.1, 0.5, 1.0})
		histogram.Observe(0.02)
		histogram.Observe(0.03)
		histogram.Observe(0.15)
		histogram.Observe(0.8)

		output, err := exporter.Export([]Metric{histogram})
		if err != nil {
			t.Fatalf("export error: %v", err)
		}

		outputStr := string(output)

		// Check for histogram elements
		if !strings.Contains(outputStr, "# TYPE request_duration_seconds histogram") {
			t.Error("missing TYPE comment")
		}
		if !strings.Contains(outputStr, `request_duration_seconds_bucket{le="0.01"} 0`) {
			t.Errorf("missing bucket le=0.01: %s", outputStr)
		}
		if !strings.Contains(outputStr, `request_duration_seconds_bucket{le="0.05"} 2`) {
			t.Errorf("missing bucket le=0.05: %s", outputStr)
		}
		if !strings.Contains(outputStr, `request_duration_seconds_bucket{le="+Inf"} 4`) {
			t.Errorf("missing bucket le=+Inf: %s", outputStr)
		}
		if !strings.Contains(outputStr, "request_duration_seconds_sum") {
			t.Error("missing _sum")
		}
		if !strings.Contains(outputStr, "request_duration_seconds_count 4") {
			t.Error("missing _count")
		}
	})

	t.Run("label escaping", func(t *testing.T) {
		counter := NewCounter("test_metric", Labels{"path": `/foo/bar"baz`})
		counter.Inc()

		output, err := exporter.Export([]Metric{counter})
		if err != nil {
			t.Fatalf("export error: %v", err)
		}

		outputStr := string(output)
		if !strings.Contains(outputStr, `path="/foo/bar\"baz"`) {
			t.Errorf("label not properly escaped: %s", outputStr)
		}
	})
}

// TestOtelExporter tests OpenTelemetry format export.
func TestOtelExporter(t *testing.T) {
	exporter := NewOtelExporter()

	t.Run("counter export", func(t *testing.T) {
		counter := NewCounter("http_requests_total", Labels{"method": "GET"})
		counter.Add(50)

		output, err := exporter.Export([]Metric{counter})
		if err != nil {
			t.Fatalf("export error: %v", err)
		}

		// Parse and verify JSON structure
		var result otelExportMetricsServiceRequest
		if err := json.Unmarshal(output, &result); err != nil {
			t.Fatalf("failed to parse output: %v", err)
		}

		if len(result.ResourceMetrics) == 0 {
			t.Fatal("no resource metrics")
		}

		rm := result.ResourceMetrics[0]
		if len(rm.ScopeMetrics) == 0 {
			t.Fatal("no scope metrics")
		}

		sm := rm.ScopeMetrics[0]
		if sm.Scope.Name != "graft" {
			t.Errorf("scope name = %q, want %q", sm.Scope.Name, "graft")
		}

		if len(sm.Metrics) == 0 {
			t.Fatal("no metrics")
		}

		metric := sm.Metrics[0]
		if metric.Name != "http_requests_total" {
			t.Errorf("metric name = %q, want %q", metric.Name, "http_requests_total")
		}
		if metric.Sum == nil {
			t.Fatal("expected sum metric")
		}
		if !metric.Sum.IsMonotonic {
			t.Error("counter should be monotonic")
		}
	})

	t.Run("gauge export", func(t *testing.T) {
		gauge := NewGauge("temperature", nil)
		gauge.Set(42.0)

		output, err := exporter.Export([]Metric{gauge})
		if err != nil {
			t.Fatalf("export error: %v", err)
		}

		var result otelExportMetricsServiceRequest
		if err := json.Unmarshal(output, &result); err != nil {
			t.Fatalf("failed to parse output: %v", err)
		}

		metric := result.ResourceMetrics[0].ScopeMetrics[0].Metrics[0]
		if metric.Gauge == nil {
			t.Fatal("expected gauge metric")
		}
		if len(metric.Gauge.DataPoints) == 0 {
			t.Fatal("no data points")
		}
		if metric.Gauge.DataPoints[0].AsDouble != 42.0 {
			t.Errorf("value = %v, want 42.0", metric.Gauge.DataPoints[0].AsDouble)
		}
	})

	t.Run("histogram export", func(t *testing.T) {
		histogram := NewHistogram("latency", nil, []float64{0.1, 0.5, 1.0})
		histogram.Observe(0.2)
		histogram.Observe(0.3)
		histogram.Observe(0.7)

		output, err := exporter.Export([]Metric{histogram})
		if err != nil {
			t.Fatalf("export error: %v", err)
		}

		var result otelExportMetricsServiceRequest
		if err := json.Unmarshal(output, &result); err != nil {
			t.Fatalf("failed to parse output: %v", err)
		}

		metric := result.ResourceMetrics[0].ScopeMetrics[0].Metrics[0]
		if metric.Histogram == nil {
			t.Fatal("expected histogram metric")
		}
		if len(metric.Histogram.DataPoints) == 0 {
			t.Fatal("no data points")
		}

		dp := metric.Histogram.DataPoints[0]
		if dp.Count != 3 {
			t.Errorf("count = %d, want 3", dp.Count)
		}
	})

	t.Run("with resource attributes", func(t *testing.T) {
		exporterWithAttrs := NewOtelExporterWithResource(map[string]string{
			"service.version": "1.2.3",
			"deployment.env":  "production",
		})

		counter := NewCounter("test", nil)
		output, err := exporterWithAttrs.Export([]Metric{counter})
		if err != nil {
			t.Fatalf("export error: %v", err)
		}

		var result otelExportMetricsServiceRequest
		if err := json.Unmarshal(output, &result); err != nil {
			t.Fatalf("failed to parse output: %v", err)
		}

		attrs := result.ResourceMetrics[0].Resource.Attributes
		if len(attrs) < 3 { // service.name + 2 custom
			t.Errorf("expected at least 3 attributes, got %d", len(attrs))
		}
	})
}

// TestJSONExporter tests JSON format export.
func TestJSONExporter(t *testing.T) {
	t.Run("basic export", func(t *testing.T) {
		exporter := NewJSONExporter(false)

		counter := NewCounter("requests", Labels{"method": "POST"})
		counter.Add(25)

		gauge := NewGauge("connections", nil)
		gauge.Set(10)

		output, err := exporter.Export([]Metric{counter, gauge})
		if err != nil {
			t.Fatalf("export error: %v", err)
		}

		var result jsonOutput
		if err := json.Unmarshal(output, &result); err != nil {
			t.Fatalf("failed to parse output: %v", err)
		}

		if len(result.Metrics) != 2 {
			t.Errorf("expected 2 metrics, got %d", len(result.Metrics))
		}
	})

	t.Run("pretty print", func(t *testing.T) {
		exporter := NewJSONExporter(true)

		counter := NewCounter("test", nil)
		output, err := exporter.Export([]Metric{counter})
		if err != nil {
			t.Fatalf("export error: %v", err)
		}

		if !strings.Contains(string(output), "\n") {
			t.Error("expected pretty-printed output with newlines")
		}
	})

	t.Run("histogram export", func(t *testing.T) {
		exporter := NewJSONExporter(false)

		histogram := NewHistogram("duration", nil, []float64{0.1, 0.5, 1.0})
		histogram.Observe(0.2)
		histogram.Observe(0.6)

		output, err := exporter.Export([]Metric{histogram})
		if err != nil {
			t.Fatalf("export error: %v", err)
		}

		var result jsonOutput
		if err := json.Unmarshal(output, &result); err != nil {
			t.Fatalf("failed to parse output: %v", err)
		}

		if len(result.Metrics) != 1 {
			t.Fatalf("expected 1 metric, got %d", len(result.Metrics))
		}

		metric := result.Metrics[0]
		if metric.Type != "histogram" {
			t.Errorf("type = %q, want histogram", metric.Type)
		}
		if metric.Histogram == nil {
			t.Fatal("expected histogram data")
		}
		if metric.Histogram.Count != 2 {
			t.Errorf("count = %d, want 2", metric.Histogram.Count)
		}
	})
}

// TestTextExporter tests human-readable text format export.
func TestTextExporter(t *testing.T) {
	exporter := NewTextExporter()

	t.Run("counter export", func(t *testing.T) {
		counter := NewCounter("http_requests", Labels{"method": "GET"})
		counter.Add(100)

		output, err := exporter.Export([]Metric{counter})
		if err != nil {
			t.Fatalf("export error: %v", err)
		}

		outputStr := string(output)

		if !strings.Contains(outputStr, "=== http_requests (counter) ===") {
			t.Errorf("missing header: %s", outputStr)
		}
		if !strings.Contains(outputStr, "method=GET") {
			t.Errorf("missing labels: %s", outputStr)
		}
		if !strings.Contains(outputStr, "100") {
			t.Errorf("missing value: %s", outputStr)
		}
	})

	t.Run("histogram export", func(t *testing.T) {
		histogram := NewHistogram("request_latency", nil, []float64{0.01, 0.05, 0.1, 0.5, 1.0})
		histogram.Observe(0.02)
		histogram.Observe(0.03)
		histogram.Observe(0.15)
		histogram.Observe(0.8)

		output, err := exporter.Export([]Metric{histogram})
		if err != nil {
			t.Fatalf("export error: %v", err)
		}

		outputStr := string(output)

		// Check for histogram summary elements
		if !strings.Contains(outputStr, "=== request_latency (histogram) ===") {
			t.Errorf("missing header: %s", outputStr)
		}
		if !strings.Contains(outputStr, "count:") {
			t.Errorf("missing count: %s", outputStr)
		}
		if !strings.Contains(outputStr, "sum:") {
			t.Errorf("missing sum: %s", outputStr)
		}
		if !strings.Contains(outputStr, "mean:") {
			t.Errorf("missing mean: %s", outputStr)
		}
		if !strings.Contains(outputStr, "percentiles:") {
			t.Errorf("missing percentiles: %s", outputStr)
		}
		if !strings.Contains(outputStr, "distribution:") {
			t.Errorf("missing distribution: %s", outputStr)
		}
	})

	t.Run("multiple metrics", func(t *testing.T) {
		counter := NewCounter("requests", nil)
		counter.Add(50)

		gauge := NewGauge("active_users", nil)
		gauge.Set(25)

		output, err := exporter.Export([]Metric{counter, gauge})
		if err != nil {
			t.Fatalf("export error: %v", err)
		}

		outputStr := string(output)

		if !strings.Contains(outputStr, "=== requests") {
			t.Error("missing requests section")
		}
		if !strings.Contains(outputStr, "=== active_users") {
			t.Error("missing active_users section")
		}
	})
}

// TestExportRegistry tests the convenience function for registry export.
func TestExportRegistry(t *testing.T) {
	registry := NewRegistry()

	counter := NewCounter("test_counter", nil)
	counter.Add(10)
	registry.MustRegister("test_counter", counter)

	gauge := NewGauge("test_gauge", nil)
	gauge.Set(5)
	registry.MustRegister("test_gauge", gauge)

	t.Run("prometheus format", func(t *testing.T) {
		output, err := ExportRegistry(registry, FormatPrometheus)
		if err != nil {
			t.Fatalf("export error: %v", err)
		}

		if !strings.Contains(string(output), "test_counter") {
			t.Error("missing test_counter")
		}
		if !strings.Contains(string(output), "test_gauge") {
			t.Error("missing test_gauge")
		}
	})

	t.Run("json format", func(t *testing.T) {
		output, err := ExportRegistry(registry, FormatJSON)
		if err != nil {
			t.Fatalf("export error: %v", err)
		}

		var result jsonOutput
		if err := json.Unmarshal(output, &result); err != nil {
			t.Fatalf("failed to parse output: %v", err)
		}

		if len(result.Metrics) != 2 {
			t.Errorf("expected 2 metrics, got %d", len(result.Metrics))
		}
	})

	t.Run("invalid format", func(t *testing.T) {
		_, err := ExportRegistry(registry, Format("invalid"))
		if err == nil {
			t.Error("expected error for invalid format")
		}
	})
}

// TestEscapeString tests label value escaping.
func TestEscapeString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{`with"quote`, `with\"quote`},
		{"with\\backslash", "with\\\\backslash"},
		{"with\nnewline", "with\\nnewline"},
		{`all"three\n`, `all\"three\\n`},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := escapeString(tt.input)
			if result != tt.expected {
				t.Errorf("escapeString(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestFormatLabels tests label formatting.
func TestFormatLabels(t *testing.T) {
	tests := []struct {
		name     string
		labels   Labels
		expected string
	}{
		{
			name:     "empty labels",
			labels:   nil,
			expected: "",
		},
		{
			name:     "single label",
			labels:   Labels{"method": "GET"},
			expected: `{method="GET"}`,
		},
		{
			name:     "multiple labels sorted",
			labels:   Labels{"status": "200", "method": "GET"},
			expected: `{method="GET",status="200"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatLabels(tt.labels)
			if result != tt.expected {
				t.Errorf("formatLabels() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestFormatFloat tests float formatting.
func TestFormatFloat(t *testing.T) {
	tests := []struct {
		value    float64
		expected string
	}{
		{0, "0"},
		{1.5, "1.5"},
		{100, "100"},
		{math.NaN(), "NaN"},
		{math.Inf(1), "+Inf"},
		{math.Inf(-1), "-Inf"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatFloat(tt.value)
			if result != tt.expected {
				t.Errorf("formatFloat(%v) = %q, want %q", tt.value, result, tt.expected)
			}
		})
	}
}

// TestSortMetrics tests metric sorting.
func TestSortMetrics(t *testing.T) {
	m1 := NewCounter("zebra", nil)
	m2 := NewCounter("alpha", Labels{"a": "1"})
	m3 := NewCounter("alpha", Labels{"a": "2"})
	m4 := NewCounter("beta", nil)

	metrics := []Metric{m1, m2, m3, m4}
	sorted := sortMetrics(metrics)

	expectedOrder := []string{"alpha", "alpha", "beta", "zebra"}
	for i, m := range sorted {
		if m.Name() != expectedOrder[i] {
			t.Errorf("position %d: got %q, want %q", i, m.Name(), expectedOrder[i])
		}
	}

	// Check that m2 comes before m3 (a=1 < a=2)
	if sorted[0].Labels()["a"] != "1" || sorted[1].Labels()["a"] != "2" {
		t.Error("alpha metrics not sorted by labels")
	}
}

// TestEmptyExport tests exporting empty metric slices.
func TestEmptyExport(t *testing.T) {
	exporters := []struct {
		name     string
		exporter Exporter
	}{
		{"prometheus", NewPrometheusExporter()},
		{"otel", NewOtelExporter()},
		{"json", NewJSONExporter(false)},
		{"text", NewTextExporter()},
	}

	for _, e := range exporters {
		t.Run(e.name, func(t *testing.T) {
			output, err := e.exporter.Export([]Metric{})
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			// Empty output is acceptable for empty metrics
			// Just ensure no error occurred
			_ = output
		})
	}
}

// BenchmarkPrometheusExport benchmarks Prometheus export.
func BenchmarkPrometheusExport(b *testing.B) {
	exporter := NewPrometheusExporter()
	metrics := createBenchmarkMetrics()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = exporter.Export(metrics)
	}
}

// BenchmarkOtelExport benchmarks OpenTelemetry export.
func BenchmarkOtelExport(b *testing.B) {
	exporter := NewOtelExporter()
	metrics := createBenchmarkMetrics()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = exporter.Export(metrics)
	}
}

// BenchmarkJSONExport benchmarks JSON export.
func BenchmarkJSONExport(b *testing.B) {
	exporter := NewJSONExporter(false)
	metrics := createBenchmarkMetrics()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = exporter.Export(metrics)
	}
}

// BenchmarkTextExport benchmarks text export.
func BenchmarkTextExport(b *testing.B) {
	exporter := NewTextExporter()
	metrics := createBenchmarkMetrics()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = exporter.Export(metrics)
	}
}

// createBenchmarkMetrics creates a set of metrics for benchmarking.
func createBenchmarkMetrics() []Metric {
	metrics := make([]Metric, 0, 50)

	// Add counters
	for i := 0; i < 10; i++ {
		c := NewCounter("counter_"+string(rune('a'+i)), Labels{"instance": "server1"})
		c.Add(float64(i * 100))
		metrics = append(metrics, c)
	}

	// Add gauges
	for i := 0; i < 10; i++ {
		g := NewGauge("gauge_"+string(rune('a'+i)), Labels{"instance": "server1"})
		g.Set(float64(i * 10))
		metrics = append(metrics, g)
	}

	// Add histograms
	for i := 0; i < 5; i++ {
		h := NewHistogram("histogram_"+string(rune('a'+i)), Labels{"instance": "server1"}, nil)
		for j := 0; j < 100; j++ {
			h.Observe(float64(j) / 100.0)
		}
		metrics = append(metrics, h)
	}

	return metrics
}

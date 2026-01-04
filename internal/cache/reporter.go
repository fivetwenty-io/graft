// Package cache provides a high-performance caching system with support for
// sharded concurrent access, LRU eviction, and TTL-based expiration.
package cache

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"
)

// Reporter generates human-readable and machine-readable cache status reports.
// It provides insights into cache performance, recommendations for tuning,
// and detailed metrics breakdowns.
type Reporter struct {
	// Analytics source (optional, for detailed metrics)
	analytics *Analytics

	// Cache sources
	caches map[string]Cache

	// Hierarchical cache (optional)
	hierarchical *HierarchicalCache

	// Report options
	options ReporterOptions
}

// ReporterOptions holds configuration for the reporter.
type ReporterOptions struct {
	// IncludeKeys includes top keys in reports.
	IncludeKeys bool

	// TopKeysCount is the number of top keys to include.
	TopKeysCount int

	// IncludePatterns includes pattern statistics in reports.
	IncludePatterns bool

	// IncludeSizeStats includes size distribution in reports.
	IncludeSizeStats bool

	// IncludeRecommendations includes tuning recommendations.
	IncludeRecommendations bool

	// Verbose enables detailed output.
	Verbose bool
}

// ReporterOption is a functional option for configuring a Reporter.
type ReporterOption func(*ReporterOptions)

// DefaultReporterOptions returns sensible default options.
func DefaultReporterOptions() ReporterOptions {
	return ReporterOptions{
		IncludeKeys:            true,
		TopKeysCount:           10,
		IncludePatterns:        true,
		IncludeSizeStats:       true,
		IncludeRecommendations: true,
		Verbose:                false,
	}
}

// WithReporterKeys enables or disables key statistics.
func WithReporterKeys(enabled bool, count int) ReporterOption {
	return func(o *ReporterOptions) {
		o.IncludeKeys = enabled
		if count > 0 {
			o.TopKeysCount = count
		}
	}
}

// WithReporterPatterns enables or disables pattern statistics.
func WithReporterPatterns(enabled bool) ReporterOption {
	return func(o *ReporterOptions) {
		o.IncludePatterns = enabled
	}
}

// WithReporterSizeStats enables or disables size statistics.
func WithReporterSizeStats(enabled bool) ReporterOption {
	return func(o *ReporterOptions) {
		o.IncludeSizeStats = enabled
	}
}

// WithReporterRecommendations enables or disables recommendations.
func WithReporterRecommendations(enabled bool) ReporterOption {
	return func(o *ReporterOptions) {
		o.IncludeRecommendations = enabled
	}
}

// WithReporterVerbose enables or disables verbose output.
func WithReporterVerbose(enabled bool) ReporterOption {
	return func(o *ReporterOptions) {
		o.Verbose = enabled
	}
}

// NewReporter creates a new cache reporter.
func NewReporter(opts ...ReporterOption) *Reporter {
	options := DefaultReporterOptions()
	for _, opt := range opts {
		opt(&options)
	}

	return &Reporter{
		caches:  make(map[string]Cache),
		options: options,
	}
}

// SetAnalytics sets the analytics source for detailed metrics.
func (r *Reporter) SetAnalytics(a *Analytics) {
	r.analytics = a
}

// SetHierarchicalCache sets the hierarchical cache for L1/L2 metrics.
func (r *Reporter) SetHierarchicalCache(hc *HierarchicalCache) {
	r.hierarchical = hc
}

// AddCache adds a named cache to report on.
func (r *Reporter) AddCache(name string, cache Cache) {
	r.caches[name] = cache
}

// Report holds the complete report data.
type Report struct {
	GeneratedAt     time.Time           `json:"generated_at"`
	Summary         ReportSummary       `json:"summary"`
	Caches          []PerCacheReport    `json:"caches,omitempty"`
	Hierarchical    *HierarchicalReport `json:"hierarchical,omitempty"`
	TopKeys         []KeyReport         `json:"top_keys,omitempty"`
	Patterns        []PatternReport     `json:"patterns,omitempty"`
	SizeStats       *SizeReport         `json:"size_stats,omitempty"`
	Recommendations []string            `json:"recommendations,omitempty"`
}

// ReportSummary holds summary statistics.
type ReportSummary struct {
	TotalHits      uint64  `json:"total_hits"`
	TotalMisses    uint64  `json:"total_misses"`
	TotalEvictions uint64  `json:"total_evictions"`
	TotalSize      int     `json:"total_size"`
	OverallHitRate float64 `json:"overall_hit_rate"`
	Effectiveness  float64 `json:"effectiveness"`
	Uptime         string  `json:"uptime"`
}

// PerCacheReport holds statistics for a single cache.
type PerCacheReport struct {
	Name      string  `json:"name"`
	Size      int     `json:"size"`
	Hits      uint64  `json:"hits"`
	Misses    uint64  `json:"misses"`
	Evictions uint64  `json:"evictions"`
	HitRate   float64 `json:"hit_rate"`
}

// HierarchicalReport holds statistics for hierarchical cache.
type HierarchicalReport struct {
	L1Size     int     `json:"l1_size"`
	L2Size     int     `json:"l2_size"`
	L1HitRate  float64 `json:"l1_hit_rate"`
	L2HitRate  float64 `json:"l2_hit_rate"`
	Promotions uint64  `json:"promotions"`
	Demotions  uint64  `json:"demotions"`
}

// KeyReport holds statistics for a single key.
type KeyReport struct {
	Key         string  `json:"key"`
	Hits        uint64  `json:"hits"`
	Misses      uint64  `json:"misses"`
	AccessCount uint64  `json:"access_count"`
	HitRate     float64 `json:"hit_rate"`
}

// PatternReport holds statistics for a key pattern.
type PatternReport struct {
	Pattern string  `json:"pattern"`
	Hits    uint64  `json:"hits"`
	Misses  uint64  `json:"misses"`
	HitRate float64 `json:"hit_rate"`
}

// SizeReport holds size distribution statistics.
type SizeReport struct {
	Distribution map[string]int64 `json:"distribution"`
	TotalCount   int64            `json:"total_count"`
	TotalSize    int64            `json:"total_size"`
	AverageSize  int64            `json:"average_size"`
	MaxSize      int64            `json:"max_size"`
	MinSize      int64            `json:"min_size"`
}

// Generate generates a complete report.
func (r *Reporter) Generate() *Report {
	report := &Report{
		GeneratedAt: time.Now(),
	}

	// Generate summary
	r.generateSummary(report)

	// Generate cache reports
	r.generateCacheReports(report)

	// Generate hierarchical report
	r.generateHierarchicalReport(report)

	// Generate key reports
	if r.options.IncludeKeys && r.analytics != nil {
		r.generateKeyReports(report)
	}

	// Generate pattern reports
	if r.options.IncludePatterns && r.analytics != nil {
		r.generatePatternReports(report)
	}

	// Generate size reports
	if r.options.IncludeSizeStats && r.analytics != nil {
		r.generateSizeReport(report)
	}

	// Generate recommendations
	if r.options.IncludeRecommendations {
		r.generateRecommendations(report)
	}

	return report
}

// generateSummary populates the report summary.
func (r *Reporter) generateSummary(report *Report) {
	var totalHits, totalMisses, totalEvictions uint64
	var totalSize int
	var uptime time.Duration

	// Aggregate from caches
	for _, cache := range r.caches {
		stats := cache.Stats()
		totalHits += stats.Hits
		totalMisses += stats.Misses
		totalEvictions += stats.Evictions
		totalSize += stats.Size
	}

	// Use analytics if available
	if r.analytics != nil {
		summary := r.analytics.Summary()
		totalHits = summary.TotalHits
		totalMisses = summary.TotalMisses
		totalEvictions = summary.TotalEvictions
		uptime = summary.Uptime
		report.Summary.Effectiveness = summary.Effectiveness
	}

	total := totalHits + totalMisses
	var hitRate float64
	if total > 0 {
		hitRate = float64(totalHits) / float64(total)
	}

	report.Summary = ReportSummary{
		TotalHits:      totalHits,
		TotalMisses:    totalMisses,
		TotalEvictions: totalEvictions,
		TotalSize:      totalSize,
		OverallHitRate: hitRate,
		Effectiveness:  report.Summary.Effectiveness,
		Uptime:         formatDuration(uptime),
	}
}

// generateCacheReports populates individual cache reports.
func (r *Reporter) generateCacheReports(report *Report) {
	for name, cache := range r.caches {
		stats := cache.Stats()
		total := stats.Hits + stats.Misses
		var hitRate float64
		if total > 0 {
			hitRate = float64(stats.Hits) / float64(total)
		}

		report.Caches = append(report.Caches, PerCacheReport{
			Name:      name,
			Size:      stats.Size,
			Hits:      stats.Hits,
			Misses:    stats.Misses,
			Evictions: stats.Evictions,
			HitRate:   hitRate,
		})
	}
}

// generateHierarchicalReport populates hierarchical cache report.
func (r *Reporter) generateHierarchicalReport(report *Report) {
	if r.hierarchical == nil {
		return
	}

	stats := r.hierarchical.HierarchicalStats()
	report.Hierarchical = &HierarchicalReport{
		L1Size:     r.hierarchical.L1Size(),
		L2Size:     r.hierarchical.L2Size(),
		L1HitRate:  stats.L1HitRate(),
		L2HitRate:  stats.L2HitRate(),
		Promotions: stats.Promotions,
		Demotions:  stats.Demotions,
	}
}

// generateKeyReports populates top key reports.
func (r *Reporter) generateKeyReports(report *Report) {
	topKeys := r.analytics.TopKeys(r.options.TopKeysCount)
	for _, key := range topKeys {
		var hitRate float64
		if key.AccessCount > 0 {
			hitRate = float64(key.Hits) / float64(key.AccessCount)
		}

		report.TopKeys = append(report.TopKeys, KeyReport{
			Key:         truncateString(key.Key, 50),
			Hits:        key.Hits,
			Misses:      key.Misses,
			AccessCount: key.AccessCount,
			HitRate:     hitRate,
		})
	}
}

// generatePatternReports populates pattern reports.
func (r *Reporter) generatePatternReports(report *Report) {
	patterns := r.analytics.PatternStats()
	for _, p := range patterns {
		report.Patterns = append(report.Patterns, PatternReport(p))
	}
}

// generateSizeReport populates size distribution report.
func (r *Reporter) generateSizeReport(report *Report) {
	sizeStats := r.analytics.SizeDistribution()
	report.SizeStats = &SizeReport{
		Distribution: sizeStats.Distribution,
		TotalCount:   sizeStats.TotalCount,
		TotalSize:    sizeStats.TotalSize,
		AverageSize:  sizeStats.AverageSize,
		MaxSize:      sizeStats.MaxSize,
		MinSize:      sizeStats.MinSize,
	}
}

// generateRecommendations populates recommendations.
func (r *Reporter) generateRecommendations(report *Report) {
	if r.analytics != nil {
		report.Recommendations = r.analytics.Recommendations()
	} else {
		// Generate basic recommendations from cache stats
		report.Recommendations = r.basicRecommendations(report)
	}
}

// basicRecommendations generates recommendations without analytics.
func (r *Reporter) basicRecommendations(report *Report) []string {
	var recs []string

	// Check overall hit rate
	if report.Summary.OverallHitRate < 0.5 && (report.Summary.TotalHits+report.Summary.TotalMisses) > 100 {
		recs = append(recs, "Overall hit rate is below 50%. Consider increasing cache sizes.")
	}

	// Check individual caches
	for _, cache := range report.Caches {
		if cache.HitRate < 0.3 && (cache.Hits+cache.Misses) > 50 {
			recs = append(recs, fmt.Sprintf("Cache '%s' has low hit rate (%.1f%%). Review cache strategy.", cache.Name, cache.HitRate*100))
		}
	}

	// Check hierarchical cache
	if report.Hierarchical != nil {
		if report.Hierarchical.L1HitRate < 0.7 {
			recs = append(recs, "L1 hit rate is low. Consider increasing L1 cache size.")
		}
		if report.Hierarchical.Promotions > 0 && report.Hierarchical.Demotions > report.Hierarchical.Promotions*2 {
			recs = append(recs, "High demotion rate detected. L1 cache may be too small.")
		}
	}

	if len(recs) == 0 {
		recs = append(recs, "Cache performance is healthy. No immediate optimizations needed.")
	}

	return recs
}

// GenerateReport generates a formatted text report.
func (r *Reporter) GenerateReport() string {
	var buf bytes.Buffer
	r.WriteReport(&buf)
	return buf.String()
}

// WriteReport writes a formatted text report to a writer.
func (r *Reporter) WriteReport(w io.Writer) {
	report := r.Generate()

	_, _ = fmt.Fprintf(w, "================================================================================\n")
	_, _ = fmt.Fprintf(w, "                         CACHE PERFORMANCE REPORT                               \n")
	_, _ = fmt.Fprintf(w, "================================================================================\n\n")

	_, _ = fmt.Fprintf(w, "Generated: %s\n", report.GeneratedAt.Format("2006-01-02 15:04:05 MST"))
	if report.Summary.Uptime != "" {
		_, _ = fmt.Fprintf(w, "Uptime:    %s\n", report.Summary.Uptime)
	}
	_, _ = fmt.Fprintln(w)

	// Summary section
	_, _ = fmt.Fprintf(w, "SUMMARY\n")
	_, _ = fmt.Fprintf(w, "--------------------------------------------------------------------------------\n")
	_, _ = fmt.Fprintf(w, "Overall Hit Rate:  %.1f%%\n", report.Summary.OverallHitRate*100)
	if report.Summary.Effectiveness > 0 {
		_, _ = fmt.Fprintf(w, "Effectiveness:     %.1f%%\n", report.Summary.Effectiveness*100)
	}
	_, _ = fmt.Fprintf(w, "Total Entries:     %d\n", report.Summary.TotalSize)
	_, _ = fmt.Fprintf(w, "Total Hits:        %d\n", report.Summary.TotalHits)
	_, _ = fmt.Fprintf(w, "Total Misses:      %d\n", report.Summary.TotalMisses)
	_, _ = fmt.Fprintf(w, "Total Evictions:   %d\n", report.Summary.TotalEvictions)
	_, _ = fmt.Fprintln(w)

	// Cache details section
	if len(report.Caches) > 0 {
		_, _ = fmt.Fprintf(w, "CACHE DETAILS\n")
		_, _ = fmt.Fprintf(w, "--------------------------------------------------------------------------------\n")

		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "Name\tSize\tHits\tMisses\tEvictions\tHit Rate")
		_, _ = fmt.Fprintln(tw, "----\t----\t----\t------\t---------\t--------")

		for _, cache := range report.Caches {
			_, _ = fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%.1f%%\n",
				cache.Name,
				cache.Size,
				cache.Hits,
				cache.Misses,
				cache.Evictions,
				cache.HitRate*100,
			)
		}
		_ = tw.Flush() // Flush errors are non-critical for report output
		_, _ = fmt.Fprintln(w)
	}

	// Hierarchical cache section
	if report.Hierarchical != nil {
		_, _ = fmt.Fprintf(w, "HIERARCHICAL CACHE (L1/L2)\n")
		_, _ = fmt.Fprintf(w, "--------------------------------------------------------------------------------\n")
		_, _ = fmt.Fprintf(w, "L1 Size:       %d entries\n", report.Hierarchical.L1Size)
		_, _ = fmt.Fprintf(w, "L2 Size:       %d entries\n", report.Hierarchical.L2Size)
		_, _ = fmt.Fprintf(w, "L1 Hit Rate:   %.1f%%\n", report.Hierarchical.L1HitRate*100)
		_, _ = fmt.Fprintf(w, "L2 Hit Rate:   %.1f%%\n", report.Hierarchical.L2HitRate*100)
		_, _ = fmt.Fprintf(w, "Promotions:    %d\n", report.Hierarchical.Promotions)
		_, _ = fmt.Fprintf(w, "Demotions:     %d\n", report.Hierarchical.Demotions)
		_, _ = fmt.Fprintln(w)
	}

	// Top keys section
	if len(report.TopKeys) > 0 {
		_, _ = fmt.Fprintf(w, "TOP ACCESSED KEYS\n")
		_, _ = fmt.Fprintf(w, "--------------------------------------------------------------------------------\n")

		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "Key\tAccesses\tHits\tMisses\tHit Rate")
		_, _ = fmt.Fprintln(tw, "---\t--------\t----\t------\t--------")

		for _, key := range report.TopKeys {
			_, _ = fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%.1f%%\n",
				key.Key,
				key.AccessCount,
				key.Hits,
				key.Misses,
				key.HitRate*100,
			)
		}
		_ = tw.Flush() // Flush errors are non-critical for report output
		_, _ = fmt.Fprintln(w)
	}

	// Pattern statistics section
	if len(report.Patterns) > 0 {
		_, _ = fmt.Fprintf(w, "PATTERN STATISTICS\n")
		_, _ = fmt.Fprintf(w, "--------------------------------------------------------------------------------\n")

		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "Pattern\tHits\tMisses\tHit Rate")
		_, _ = fmt.Fprintln(tw, "-------\t----\t------\t--------")

		for _, p := range report.Patterns {
			_, _ = fmt.Fprintf(tw, "%s\t%d\t%d\t%.1f%%\n",
				truncateString(p.Pattern, 40),
				p.Hits,
				p.Misses,
				p.HitRate*100,
			)
		}
		_ = tw.Flush() // Flush errors are non-critical for report output
		_, _ = fmt.Fprintln(w)
	}

	// Size statistics section
	if report.SizeStats != nil && report.SizeStats.TotalCount > 0 {
		_, _ = fmt.Fprintf(w, "SIZE DISTRIBUTION\n")
		_, _ = fmt.Fprintf(w, "--------------------------------------------------------------------------------\n")
		_, _ = fmt.Fprintf(w, "Total Values:  %d\n", report.SizeStats.TotalCount)
		_, _ = fmt.Fprintf(w, "Total Size:    %s\n", formatBytes(report.SizeStats.TotalSize))
		_, _ = fmt.Fprintf(w, "Average Size:  %s\n", formatBytes(report.SizeStats.AverageSize))
		_, _ = fmt.Fprintf(w, "Min Size:      %s\n", formatBytes(report.SizeStats.MinSize))
		_, _ = fmt.Fprintf(w, "Max Size:      %s\n", formatBytes(report.SizeStats.MaxSize))
		_, _ = fmt.Fprintln(w)

		_, _ = fmt.Fprintf(w, "Distribution:\n")
		_, _ = fmt.Fprintf(w, "  Tiny (<100B):     %d (%.1f%%)\n",
			report.SizeStats.Distribution["tiny"],
			percentage(report.SizeStats.Distribution["tiny"], report.SizeStats.TotalCount))
		_, _ = fmt.Fprintf(w, "  Small (100B-1KB): %d (%.1f%%)\n",
			report.SizeStats.Distribution["small"],
			percentage(report.SizeStats.Distribution["small"], report.SizeStats.TotalCount))
		_, _ = fmt.Fprintf(w, "  Medium (1-10KB):  %d (%.1f%%)\n",
			report.SizeStats.Distribution["medium"],
			percentage(report.SizeStats.Distribution["medium"], report.SizeStats.TotalCount))
		_, _ = fmt.Fprintf(w, "  Large (10-100KB): %d (%.1f%%)\n",
			report.SizeStats.Distribution["large"],
			percentage(report.SizeStats.Distribution["large"], report.SizeStats.TotalCount))
		_, _ = fmt.Fprintf(w, "  Huge (>100KB):    %d (%.1f%%)\n",
			report.SizeStats.Distribution["huge"],
			percentage(report.SizeStats.Distribution["huge"], report.SizeStats.TotalCount))
		_, _ = fmt.Fprintln(w)
	}

	// Recommendations section
	if len(report.Recommendations) > 0 {
		_, _ = fmt.Fprintf(w, "RECOMMENDATIONS\n")
		_, _ = fmt.Fprintf(w, "--------------------------------------------------------------------------------\n")
		for _, rec := range report.Recommendations {
			_, _ = fmt.Fprintf(w, "  * %s\n", rec)
		}
		_, _ = fmt.Fprintln(w)
	}

	_, _ = fmt.Fprintf(w, "================================================================================\n")
}

// GenerateJSON generates a JSON report.
func (r *Reporter) GenerateJSON() ([]byte, error) {
	report := r.Generate()
	return json.MarshalIndent(report, "", "  ")
}

// GenerateCompact generates a compact single-line summary.
func (r *Reporter) GenerateCompact() string {
	report := r.Generate()

	parts := []string{
		fmt.Sprintf("hit_rate=%.1f%%", report.Summary.OverallHitRate*100),
		fmt.Sprintf("size=%d", report.Summary.TotalSize),
		fmt.Sprintf("hits=%d", report.Summary.TotalHits),
		fmt.Sprintf("misses=%d", report.Summary.TotalMisses),
	}

	if report.Summary.Effectiveness > 0 {
		parts = append(parts, fmt.Sprintf("effectiveness=%.1f%%", report.Summary.Effectiveness*100))
	}

	return strings.Join(parts, " ")
}

// GenerateMetrics generates a Prometheus-compatible metrics output.
func (r *Reporter) GenerateMetrics(w io.Writer) {
	report := r.Generate()

	_, _ = fmt.Fprintf(w, "# HELP cache_hits_total Total number of cache hits\n")
	_, _ = fmt.Fprintf(w, "# TYPE cache_hits_total counter\n")
	_, _ = fmt.Fprintf(w, "cache_hits_total %d\n\n", report.Summary.TotalHits)

	_, _ = fmt.Fprintf(w, "# HELP cache_misses_total Total number of cache misses\n")
	_, _ = fmt.Fprintf(w, "# TYPE cache_misses_total counter\n")
	_, _ = fmt.Fprintf(w, "cache_misses_total %d\n\n", report.Summary.TotalMisses)

	_, _ = fmt.Fprintf(w, "# HELP cache_evictions_total Total number of cache evictions\n")
	_, _ = fmt.Fprintf(w, "# TYPE cache_evictions_total counter\n")
	_, _ = fmt.Fprintf(w, "cache_evictions_total %d\n\n", report.Summary.TotalEvictions)

	_, _ = fmt.Fprintf(w, "# HELP cache_size Current number of entries in cache\n")
	_, _ = fmt.Fprintf(w, "# TYPE cache_size gauge\n")
	_, _ = fmt.Fprintf(w, "cache_size %d\n\n", report.Summary.TotalSize)

	_, _ = fmt.Fprintf(w, "# HELP cache_hit_rate Cache hit rate\n")
	_, _ = fmt.Fprintf(w, "# TYPE cache_hit_rate gauge\n")
	_, _ = fmt.Fprintf(w, "cache_hit_rate %.4f\n\n", report.Summary.OverallHitRate)

	if report.Summary.Effectiveness > 0 {
		_, _ = fmt.Fprintf(w, "# HELP cache_effectiveness Cache effectiveness score\n")
		_, _ = fmt.Fprintf(w, "# TYPE cache_effectiveness gauge\n")
		_, _ = fmt.Fprintf(w, "cache_effectiveness %.4f\n\n", report.Summary.Effectiveness)
	}

	// Per-cache metrics
	for _, cache := range report.Caches {
		labels := fmt.Sprintf(`cache=%q`, cache.Name)
		_, _ = fmt.Fprintf(w, "cache_hits{%s} %d\n", labels, cache.Hits)
		_, _ = fmt.Fprintf(w, "cache_misses{%s} %d\n", labels, cache.Misses)
		_, _ = fmt.Fprintf(w, "cache_evictions{%s} %d\n", labels, cache.Evictions)
		_, _ = fmt.Fprintf(w, "cache_entries{%s} %d\n", labels, cache.Size)
		_, _ = fmt.Fprintf(w, "cache_hit_rate_per_cache{%s} %.4f\n", labels, cache.HitRate)
	}

	// Hierarchical cache metrics
	if report.Hierarchical != nil {
		_, _ = fmt.Fprintf(w, "\ncache_l1_size %d\n", report.Hierarchical.L1Size)
		_, _ = fmt.Fprintf(w, "cache_l2_size %d\n", report.Hierarchical.L2Size)
		_, _ = fmt.Fprintf(w, "cache_l1_hit_rate %.4f\n", report.Hierarchical.L1HitRate)
		_, _ = fmt.Fprintf(w, "cache_l2_hit_rate %.4f\n", report.Hierarchical.L2HitRate)
		_, _ = fmt.Fprintf(w, "cache_promotions_total %d\n", report.Hierarchical.Promotions)
		_, _ = fmt.Fprintf(w, "cache_demotions_total %d\n", report.Hierarchical.Demotions)
	}
}

// Helper functions

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func formatDuration(d time.Duration) string {
	if d == 0 {
		return ""
	}

	days := int(d.Hours() / 24)
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	var parts []string
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if minutes > 0 {
		parts = append(parts, fmt.Sprintf("%dm", minutes))
	}
	if seconds > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%ds", seconds))
	}

	return strings.Join(parts, " ")
}

func formatBytes(b int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case b >= GB:
		return fmt.Sprintf("%.2f GB", float64(b)/GB)
	case b >= MB:
		return fmt.Sprintf("%.2f MB", float64(b)/MB)
	case b >= KB:
		return fmt.Sprintf("%.2f KB", float64(b)/KB)
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func percentage(part, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total) * 100
}

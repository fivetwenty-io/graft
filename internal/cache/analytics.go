// Package cache provides a high-performance caching system with support for
// sharded concurrent access, LRU eviction, and TTL-based expiration.
package cache

import (
	"math"
	"regexp"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Analytics provides detailed cache performance metrics and analysis.
// It tracks hit/miss rates, access patterns, and calculates effectiveness scores.
type Analytics struct {
	mu sync.RWMutex

	// Time-based metrics (rolling windows)
	windows    map[time.Duration]*timeWindow
	windowDurs []time.Duration

	// Pattern-based metrics
	patterns    map[string]*patternStats
	patternsMu  sync.RWMutex
	patternList []*regexp.Regexp

	// Key-level statistics
	keyStats   map[string]*KeyStats
	keyStatsMu sync.RWMutex
	maxKeys    int

	// Global counters
	totalHits      atomic.Uint64
	totalMisses    atomic.Uint64
	totalEvictions atomic.Uint64
	totalSets      atomic.Uint64

	// Size tracking
	sizeDistribution *sizeDistribution

	// Start time for rate calculations
	startTime time.Time
}

// timeWindow tracks metrics over a rolling time window.
type timeWindow struct {
	duration time.Duration
	buckets  []*windowBucket
	mu       sync.Mutex
}

// windowBucket holds metrics for a single time bucket.
type windowBucket struct {
	timestamp time.Time
	hits      uint64
	misses    uint64
}

// patternStats holds statistics for a key pattern.
type patternStats struct {
	Pattern string
	Hits    atomic.Uint64
	Misses  atomic.Uint64
	Sets    atomic.Uint64
}

// KeyStats holds detailed statistics for a single cache key.
type KeyStats struct {
	Key          string
	Hits         uint64
	Misses       uint64
	LastAccess   time.Time
	FirstAccess  time.Time
	AccessCount  uint64
	AvgValueSize int64
	TotalSize    int64
	SizeCount    int64
}

// sizeDistribution tracks the distribution of value sizes.
type sizeDistribution struct {
	mu      sync.Mutex
	buckets map[string]int64 // size range -> count
	total   int64
	sumSize int64
	maxSize int64
	minSize int64
}

// AnalyticsOptions holds configuration for the analytics tracker.
type AnalyticsOptions struct {
	// Windows specifies the time windows to track (default: 1min, 5min, 15min).
	Windows []time.Duration

	// MaxKeys limits the number of keys tracked for per-key stats.
	MaxKeys int

	// Patterns are regex patterns for grouping key statistics.
	Patterns []string

	// TrackSizes enables tracking of value size distribution.
	TrackSizes bool
}

// AnalyticsOption is a functional option for configuring Analytics.
type AnalyticsOption func(*AnalyticsOptions)

// DefaultAnalyticsOptions returns sensible default options.
func DefaultAnalyticsOptions() AnalyticsOptions {
	return AnalyticsOptions{
		Windows:    []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute},
		MaxKeys:    10000,
		TrackSizes: true,
	}
}

// WithAnalyticsWindows sets the time windows to track.
func WithAnalyticsWindows(windows []time.Duration) AnalyticsOption {
	return func(o *AnalyticsOptions) {
		o.Windows = windows
	}
}

// WithAnalyticsMaxKeys sets the maximum keys to track.
func WithAnalyticsMaxKeys(maxKeys int) AnalyticsOption {
	return func(o *AnalyticsOptions) {
		if maxKeys > 0 {
			o.MaxKeys = maxKeys
		}
	}
}

// WithAnalyticsPatterns sets the key patterns to track.
func WithAnalyticsPatterns(patterns []string) AnalyticsOption {
	return func(o *AnalyticsOptions) {
		o.Patterns = patterns
	}
}

// WithAnalyticsTrackSizes enables or disables size tracking.
func WithAnalyticsTrackSizes(enabled bool) AnalyticsOption {
	return func(o *AnalyticsOptions) {
		o.TrackSizes = enabled
	}
}

// NewAnalytics creates a new analytics tracker.
func NewAnalytics(opts ...AnalyticsOption) *Analytics {
	options := DefaultAnalyticsOptions()
	for _, opt := range opts {
		opt(&options)
	}

	a := &Analytics{
		windows:     make(map[time.Duration]*timeWindow),
		windowDurs:  options.Windows,
		patterns:    make(map[string]*patternStats),
		keyStats:    make(map[string]*KeyStats),
		maxKeys:     options.MaxKeys,
		startTime:   time.Now(),
		patternList: make([]*regexp.Regexp, 0),
	}

	// Initialize time windows
	for _, dur := range options.Windows {
		a.windows[dur] = newTimeWindow(dur)
	}

	// Compile patterns
	for _, pattern := range options.Patterns {
		if re, err := regexp.Compile(pattern); err == nil {
			a.patternList = append(a.patternList, re)
			a.patterns[pattern] = &patternStats{Pattern: pattern}
		}
	}

	// Initialize size distribution
	if options.TrackSizes {
		a.sizeDistribution = newSizeDistribution()
	}

	return a
}

// newTimeWindow creates a new time window tracker.
func newTimeWindow(duration time.Duration) *timeWindow {
	// Use 60 buckets for granularity
	bucketCount := 60
	bucketDur := duration / time.Duration(bucketCount)
	if bucketDur < time.Second {
		bucketDur = time.Second
		bucketCount = int(duration / time.Second)
	}

	buckets := make([]*windowBucket, bucketCount)
	now := time.Now()
	for i := range buckets {
		buckets[i] = &windowBucket{
			timestamp: now.Add(-time.Duration(i) * bucketDur),
		}
	}

	return &timeWindow{
		duration: duration,
		buckets:  buckets,
	}
}

// newSizeDistribution creates a new size distribution tracker.
func newSizeDistribution() *sizeDistribution {
	return &sizeDistribution{
		buckets: map[string]int64{
			"tiny":   0, // < 100 bytes
			"small":  0, // 100B - 1KB
			"medium": 0, // 1KB - 10KB
			"large":  0, // 10KB - 100KB
			"huge":   0, // > 100KB
		},
		minSize: math.MaxInt64,
	}
}

// RecordHit records a cache hit.
func (a *Analytics) RecordHit(key string, size int) {
	a.totalHits.Add(1)
	a.recordAccess(key, true, size)
}

// RecordMiss records a cache miss.
func (a *Analytics) RecordMiss(key string) {
	a.totalMisses.Add(1)
	a.recordAccess(key, false, 0)
}

// RecordSet records a cache set operation.
func (a *Analytics) RecordSet(key string, size int) {
	a.totalSets.Add(1)

	// Record size
	if a.sizeDistribution != nil {
		a.sizeDistribution.record(int64(size))
	}

	// Update key stats
	a.updateKeyStats(key, false, size)

	// Update pattern stats
	a.updatePatternStats(key, false)
}

// RecordEviction records a cache eviction.
func (a *Analytics) RecordEviction() {
	a.totalEvictions.Add(1)
}

// recordAccess records a cache access (hit or miss).
func (a *Analytics) recordAccess(key string, hit bool, size int) {
	now := time.Now()

	// Update time windows
	for _, tw := range a.windows {
		tw.record(now, hit)
	}

	// Update key stats
	a.updateKeyStats(key, hit, size)

	// Update pattern stats
	a.updatePatternStats(key, hit)
}

// record adds a data point to the time window.
func (tw *timeWindow) record(t time.Time, hit bool) {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	// Find or create the appropriate bucket
	bucketDur := tw.duration / time.Duration(len(tw.buckets))
	bucketIdx := int(time.Since(t) / bucketDur)

	if bucketIdx < 0 {
		bucketIdx = 0
	}
	if bucketIdx >= len(tw.buckets) {
		return // Outside window
	}

	bucket := tw.buckets[bucketIdx]
	if hit {
		bucket.hits++
	} else {
		bucket.misses++
	}
}

// hitRate returns the hit rate for this time window.
func (tw *timeWindow) hitRate() float64 {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	var hits, misses uint64
	cutoff := time.Now().Add(-tw.duration)

	for _, bucket := range tw.buckets {
		if bucket.timestamp.After(cutoff) {
			hits += bucket.hits
			misses += bucket.misses
		}
	}

	total := hits + misses
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total)
}

// updateKeyStats updates statistics for a specific key.
func (a *Analytics) updateKeyStats(key string, hit bool, size int) {
	a.keyStatsMu.Lock()
	defer a.keyStatsMu.Unlock()

	stats, exists := a.keyStats[key]
	if !exists {
		if len(a.keyStats) >= a.maxKeys {
			// Evict least accessed key
			a.evictLeastAccessedKey()
		}
		stats = &KeyStats{
			Key:         key,
			FirstAccess: time.Now(),
		}
		a.keyStats[key] = stats
	}

	stats.LastAccess = time.Now()
	stats.AccessCount++

	if hit {
		stats.Hits++
	} else {
		stats.Misses++
	}

	if size > 0 {
		stats.TotalSize += int64(size)
		stats.SizeCount++
		stats.AvgValueSize = stats.TotalSize / stats.SizeCount
	}
}

// evictLeastAccessedKey removes the least accessed key from tracking.
func (a *Analytics) evictLeastAccessedKey() {
	var minKey string
	var minAccess uint64 = math.MaxUint64

	for key, stats := range a.keyStats {
		if stats.AccessCount < minAccess {
			minAccess = stats.AccessCount
			minKey = key
		}
	}

	if minKey != "" {
		delete(a.keyStats, minKey)
	}
}

// updatePatternStats updates statistics for matching patterns.
func (a *Analytics) updatePatternStats(key string, hit bool) {
	a.patternsMu.Lock()
	defer a.patternsMu.Unlock()

	for _, re := range a.patternList {
		if !re.MatchString(key) {
			continue
		}
		pattern := re.String()
		stats, exists := a.patterns[pattern]
		if !exists {
			stats = &patternStats{Pattern: pattern}
			a.patterns[pattern] = stats
		}

		if hit {
			stats.Hits.Add(1)
		} else {
			stats.Misses.Add(1)
		}

		// Only match first pattern
		break
	}
}

// record adds a size measurement.
func (sd *sizeDistribution) record(size int64) {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	sd.total++
	sd.sumSize += size

	if size > sd.maxSize {
		sd.maxSize = size
	}
	if size < sd.minSize {
		sd.minSize = size
	}

	// Categorize
	switch {
	case size < 100:
		sd.buckets["tiny"]++
	case size < 1024:
		sd.buckets["small"]++
	case size < 10*1024:
		sd.buckets["medium"]++
	case size < 100*1024:
		sd.buckets["large"]++
	default:
		sd.buckets["huge"]++
	}
}

// HitRate returns the overall cache hit rate.
func (a *Analytics) HitRate() float64 {
	hits := a.totalHits.Load()
	misses := a.totalMisses.Load()
	total := hits + misses
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total)
}

// HitRateWindow returns the hit rate for a specific time window.
func (a *Analytics) HitRateWindow(duration time.Duration) float64 {
	a.mu.RLock()
	tw, exists := a.windows[duration]
	a.mu.RUnlock()

	if !exists {
		return 0
	}
	return tw.hitRate()
}

// WindowHitRates returns hit rates for all configured time windows.
func (a *Analytics) WindowHitRates() map[time.Duration]float64 {
	rates := make(map[time.Duration]float64)
	a.mu.RLock()
	for dur, tw := range a.windows {
		rates[dur] = tw.hitRate()
	}
	a.mu.RUnlock()
	return rates
}

// Effectiveness calculates an overall cache effectiveness score (0.0 - 1.0).
// The score considers hit rate, eviction rate, and access patterns.
func (a *Analytics) Effectiveness() float64 {
	hits := a.totalHits.Load()
	misses := a.totalMisses.Load()
	evictions := a.totalEvictions.Load()
	total := hits + misses

	if total == 0 {
		return 0
	}

	// Hit rate component (50% weight)
	hitRate := float64(hits) / float64(total)
	hitScore := hitRate

	// Eviction rate component (30% weight)
	// Lower eviction rate is better
	evictionRate := float64(evictions) / float64(total)
	evictionScore := 1.0 - math.Min(evictionRate*5, 1.0) // Penalize high eviction

	// Access pattern component (20% weight)
	// Based on key reuse (hits vs unique keys)
	a.keyStatsMu.RLock()
	uniqueKeys := len(a.keyStats)
	a.keyStatsMu.RUnlock()

	var reuseScore float64
	if uniqueKeys > 0 {
		avgAccessPerKey := float64(total) / float64(uniqueKeys)
		reuseScore = math.Min(avgAccessPerKey/10, 1.0) // Cap at 10 accesses per key
	}

	// Weighted combination
	effectiveness := hitScore*0.5 + evictionScore*0.3 + reuseScore*0.2

	return effectiveness
}

// TopKeys returns the N most frequently accessed keys.
func (a *Analytics) TopKeys(n int) []KeyStats {
	a.keyStatsMu.RLock()
	defer a.keyStatsMu.RUnlock()

	keys := make([]KeyStats, 0, len(a.keyStats))
	for _, stats := range a.keyStats {
		keys = append(keys, *stats)
	}

	// Sort by access count descending
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].AccessCount > keys[j].AccessCount
	})

	if n > 0 && n < len(keys) {
		keys = keys[:n]
	}

	return keys
}

// HotKeys returns keys with high hit rates and frequent access.
func (a *Analytics) HotKeys(n int) []KeyStats {
	a.keyStatsMu.RLock()
	defer a.keyStatsMu.RUnlock()

	keys := make([]KeyStats, 0, len(a.keyStats))
	for _, stats := range a.keyStats {
		keys = append(keys, *stats)
	}

	// Sort by hits descending
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].Hits > keys[j].Hits
	})

	if n > 0 && n < len(keys) {
		keys = keys[:n]
	}

	return keys
}

// ColdKeys returns keys with low hit rates (potential candidates for eviction).
func (a *Analytics) ColdKeys(n int) []KeyStats {
	a.keyStatsMu.RLock()
	defer a.keyStatsMu.RUnlock()

	keys := make([]KeyStats, 0, len(a.keyStats))
	for _, stats := range a.keyStats {
		keys = append(keys, *stats)
	}

	// Sort by hit rate ascending
	sort.Slice(keys, func(i, j int) bool {
		iRate := float64(keys[i].Hits) / float64(keys[i].AccessCount)
		jRate := float64(keys[j].Hits) / float64(keys[j].AccessCount)
		return iRate < jRate
	})

	if n > 0 && n < len(keys) {
		keys = keys[:n]
	}

	return keys
}

// PatternStats returns statistics grouped by key pattern.
func (a *Analytics) PatternStats() []PatternStatistics {
	a.patternsMu.RLock()
	defer a.patternsMu.RUnlock()

	stats := make([]PatternStatistics, 0, len(a.patterns))
	for _, ps := range a.patterns {
		hits := ps.Hits.Load()
		misses := ps.Misses.Load()
		total := hits + misses
		var hitRate float64
		if total > 0 {
			hitRate = float64(hits) / float64(total)
		}

		stats = append(stats, PatternStatistics{
			Pattern: ps.Pattern,
			Hits:    hits,
			Misses:  misses,
			HitRate: hitRate,
		})
	}

	// Sort by total accesses descending
	sort.Slice(stats, func(i, j int) bool {
		return (stats[i].Hits + stats[i].Misses) > (stats[j].Hits + stats[j].Misses)
	})

	return stats
}

// PatternStatistics holds statistics for a key pattern.
type PatternStatistics struct {
	Pattern string
	Hits    uint64
	Misses  uint64
	HitRate float64
}

// SizeDistribution returns the distribution of value sizes.
func (a *Analytics) SizeDistribution() SizeStats {
	if a.sizeDistribution == nil {
		return SizeStats{}
	}

	a.sizeDistribution.mu.Lock()
	defer a.sizeDistribution.mu.Unlock()

	buckets := make(map[string]int64)
	for k, v := range a.sizeDistribution.buckets {
		buckets[k] = v
	}

	var avgSize int64
	if a.sizeDistribution.total > 0 {
		avgSize = a.sizeDistribution.sumSize / a.sizeDistribution.total
	}

	minSize := a.sizeDistribution.minSize
	if minSize == math.MaxInt64 {
		minSize = 0
	}

	return SizeStats{
		Distribution: buckets,
		TotalCount:   a.sizeDistribution.total,
		TotalSize:    a.sizeDistribution.sumSize,
		AverageSize:  avgSize,
		MaxSize:      a.sizeDistribution.maxSize,
		MinSize:      minSize,
	}
}

// SizeStats holds size distribution statistics.
type SizeStats struct {
	Distribution map[string]int64
	TotalCount   int64
	TotalSize    int64
	AverageSize  int64
	MaxSize      int64
	MinSize      int64
}

// Summary returns a summary of all analytics data.
func (a *Analytics) Summary() AnalyticsSummary {
	return AnalyticsSummary{
		TotalHits:      a.totalHits.Load(),
		TotalMisses:    a.totalMisses.Load(),
		TotalSets:      a.totalSets.Load(),
		TotalEvictions: a.totalEvictions.Load(),
		HitRate:        a.HitRate(),
		WindowRates:    a.WindowHitRates(),
		Effectiveness:  a.Effectiveness(),
		TopKeys:        a.TopKeys(10),
		PatternStats:   a.PatternStats(),
		SizeStats:      a.SizeDistribution(),
		Uptime:         time.Since(a.startTime),
	}
}

// AnalyticsSummary holds a complete summary of analytics data.
type AnalyticsSummary struct {
	TotalHits      uint64
	TotalMisses    uint64
	TotalSets      uint64
	TotalEvictions uint64
	HitRate        float64
	WindowRates    map[time.Duration]float64
	Effectiveness  float64
	TopKeys        []KeyStats
	PatternStats   []PatternStatistics
	SizeStats      SizeStats
	Uptime         time.Duration
}

// Reset clears all analytics data.
func (a *Analytics) Reset() {
	a.totalHits.Store(0)
	a.totalMisses.Store(0)
	a.totalEvictions.Store(0)
	a.totalSets.Store(0)

	a.keyStatsMu.Lock()
	a.keyStats = make(map[string]*KeyStats)
	a.keyStatsMu.Unlock()

	a.patternsMu.Lock()
	for _, ps := range a.patterns {
		ps.Hits.Store(0)
		ps.Misses.Store(0)
		ps.Sets.Store(0)
	}
	a.patternsMu.Unlock()

	a.mu.Lock()
	for dur := range a.windows {
		a.windows[dur] = newTimeWindow(dur)
	}
	a.mu.Unlock()

	if a.sizeDistribution != nil {
		a.sizeDistribution = newSizeDistribution()
	}

	a.startTime = time.Now()
}

// AddPattern adds a new pattern for tracking at runtime.
func (a *Analytics) AddPattern(pattern string) error {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}

	a.patternsMu.Lock()
	defer a.patternsMu.Unlock()

	a.patternList = append(a.patternList, re)
	a.patterns[pattern] = &patternStats{Pattern: pattern}

	return nil
}

// Recommendations returns tuning recommendations based on analytics.
func (a *Analytics) Recommendations() []string {
	var recs []string

	// Check hit rate
	hitRate := a.HitRate()
	if hitRate < 0.5 && a.totalHits.Load()+a.totalMisses.Load() > 100 {
		recs = append(recs, "Hit rate is below 50%. Consider increasing cache size.")
	}

	// Check eviction rate
	evictions := a.totalEvictions.Load()
	total := a.totalHits.Load() + a.totalMisses.Load()
	if total > 0 {
		evictionRate := float64(evictions) / float64(total)
		if evictionRate > 0.1 {
			recs = append(recs, "High eviction rate detected. Consider increasing cache capacity or TTL.")
		}
	}

	// Check for hot keys
	hotKeys := a.HotKeys(5)
	if len(hotKeys) > 0 {
		topKey := hotKeys[0]
		if total > 0 && float64(topKey.AccessCount)/float64(total) > 0.2 {
			recs = append(recs, "Single key accounts for >20% of accesses. Consider dedicated caching.")
		}
	}

	// Check size distribution
	sizeStats := a.SizeDistribution()
	if sizeStats.TotalCount > 0 {
		hugePercent := float64(sizeStats.Distribution["huge"]) / float64(sizeStats.TotalCount)
		if hugePercent > 0.1 {
			recs = append(recs, "Many large values detected. Consider compression or value optimization.")
		}
	}

	// Check effectiveness
	effectiveness := a.Effectiveness()
	if effectiveness < 0.6 && total > 100 {
		recs = append(recs, "Cache effectiveness is low. Review access patterns and configuration.")
	}

	if len(recs) == 0 {
		recs = append(recs, "Cache performance is healthy. No immediate optimizations needed.")
	}

	return recs
}

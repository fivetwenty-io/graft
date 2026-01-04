// Package cache provides a high-performance caching system with support for
// sharded concurrent access, LRU eviction, and TTL-based expiration.
package cache

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// Warmer provides cache warming capabilities for preloading cache entries.
// It supports warming from files, functions, and pluggable sources with
// progress reporting.
type Warmer struct {
	cache    Cache
	options  WarmerOptions
	progress atomic.Int64
	total    atomic.Int64
	errors   atomic.Int64
	running  atomic.Bool
}

// WarmerOptions holds configuration for the cache warmer.
type WarmerOptions struct {
	// BatchSize is the number of entries to load per batch.
	BatchSize int

	// Concurrency is the number of parallel workers for warming.
	Concurrency int

	// Timeout is the maximum duration for a warming operation.
	Timeout time.Duration

	// OnProgress is called periodically with progress updates.
	OnProgress func(loaded, total int64, errors int64)

	// ProgressInterval is how often OnProgress is called.
	ProgressInterval time.Duration

	// ContinueOnError continues warming even if some entries fail.
	ContinueOnError bool

	// TTL is the default TTL for warmed entries.
	TTL time.Duration
}

// WarmerOption is a functional option for configuring a Warmer.
type WarmerOption func(*WarmerOptions)

// DefaultWarmerOptions returns sensible default options.
func DefaultWarmerOptions() WarmerOptions {
	return WarmerOptions{
		BatchSize:        100,
		Concurrency:      4,
		Timeout:          5 * time.Minute,
		ProgressInterval: time.Second,
		ContinueOnError:  true,
		TTL:              0,
	}
}

// WithWarmerBatchSize sets the batch size for warming operations.
func WithWarmerBatchSize(size int) WarmerOption {
	return func(o *WarmerOptions) {
		if size > 0 {
			o.BatchSize = size
		}
	}
}

// WithWarmerConcurrency sets the number of parallel workers.
func WithWarmerConcurrency(n int) WarmerOption {
	return func(o *WarmerOptions) {
		if n > 0 {
			o.Concurrency = n
		}
	}
}

// WithWarmerTimeout sets the maximum duration for warming.
func WithWarmerTimeout(timeout time.Duration) WarmerOption {
	return func(o *WarmerOptions) {
		o.Timeout = timeout
	}
}

// WithWarmerProgress sets the progress callback and interval.
func WithWarmerProgress(fn func(loaded, total int64, errors int64), interval time.Duration) WarmerOption {
	return func(o *WarmerOptions) {
		o.OnProgress = fn
		if interval > 0 {
			o.ProgressInterval = interval
		}
	}
}

// WithWarmerContinueOnError sets whether to continue on errors.
func WithWarmerContinueOnError(cont bool) WarmerOption {
	return func(o *WarmerOptions) {
		o.ContinueOnError = cont
	}
}

// WithWarmerTTL sets the default TTL for warmed entries.
func WithWarmerTTL(ttl time.Duration) WarmerOption {
	return func(o *WarmerOptions) {
		o.TTL = ttl
	}
}

// NewWarmer creates a new cache warmer for the given cache.
func NewWarmer(cache Cache, opts ...WarmerOption) *Warmer {
	options := DefaultWarmerOptions()
	for _, opt := range opts {
		opt(&options)
	}

	return &Warmer{
		cache:   cache,
		options: options,
	}
}

// WarmingSource is an interface for pluggable warming data sources.
type WarmingSource interface {
	// Next returns the next key-value pair to warm.
	// Returns io.EOF when exhausted.
	Next() (key string, value interface{}, err error)

	// Count returns the total number of entries, or -1 if unknown.
	Count() int64

	// Close releases any resources held by the source.
	Close() error
}

// WarmEntry represents a single cache entry to warm.
type WarmEntry struct {
	Key   string        `json:"key"`
	Value interface{}   `json:"value"`
	TTL   time.Duration `json:"ttl,omitempty"`
}

// WarmingStats holds statistics from a warming operation.
type WarmingStats struct {
	Loaded    int64
	Errors    int64
	Total     int64
	Duration  time.Duration
	StartTime time.Time
	EndTime   time.Time
}

// WarmFromFile loads cache entries from a JSON file.
// The file should contain JSON objects with "key" and "value" fields,
// one per line (JSON Lines format).
func (w *Warmer) WarmFromFile(path string) (*WarmingStats, error) {
	cleanPath := filepath.Clean(path)
	file, err := os.Open(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer func() { _ = file.Close() }()

	source := NewReaderSource(file)
	defer func() { _ = source.Close() }()

	return w.WarmFromSource(source)
}

// WarmFromFunc loads cache entries from a function that returns key-value pairs.
func (w *Warmer) WarmFromFunc(fn func() map[string]interface{}) (*WarmingStats, error) {
	entries := fn()
	source := NewMapSource(entries)
	return w.WarmFromSource(source)
}

// WarmFromMap loads cache entries from a map.
func (w *Warmer) WarmFromMap(entries map[string]interface{}) (*WarmingStats, error) {
	source := NewMapSource(entries)
	return w.WarmFromSource(source)
}

// WarmFromSlice loads cache entries from a slice of WarmEntry.
func (w *Warmer) WarmFromSlice(entries []WarmEntry) (*WarmingStats, error) {
	source := NewSliceSource(entries)
	return w.WarmFromSource(source)
}

// WarmFromSource loads cache entries from a WarmingSource.
func (w *Warmer) WarmFromSource(source WarmingSource) (*WarmingStats, error) {
	return w.WarmFromSourceContext(context.Background(), source)
}

// WarmFromSourceContext loads cache entries with context for cancellation.
func (w *Warmer) WarmFromSourceContext(ctx context.Context, source WarmingSource) (*WarmingStats, error) {
	// Prevent concurrent warming operations
	if !w.running.CompareAndSwap(false, true) {
		return nil, fmt.Errorf("warming already in progress")
	}
	defer w.running.Store(false)

	// Reset counters
	w.progress.Store(0)
	w.errors.Store(0)
	total := source.Count()
	w.total.Store(total)

	stats := &WarmingStats{
		Total:     total,
		StartTime: time.Now(),
	}

	// Apply timeout if configured
	if w.options.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, w.options.Timeout)
		defer cancel()
	}

	// Start progress reporter if callback is set
	var progressDone chan struct{}
	if w.options.OnProgress != nil {
		progressDone = make(chan struct{})
		go w.reportProgress(ctx, progressDone)
	}

	// Create worker pool
	entries := make(chan WarmEntry, w.options.BatchSize)
	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < w.options.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.worker(ctx, entries)
		}()
	}

	// Feed entries to workers
	err := w.feedEntries(ctx, source, entries)
	close(entries)

	// Wait for workers to finish
	wg.Wait()

	// Stop progress reporter
	if progressDone != nil {
		close(progressDone)
	}

	stats.EndTime = time.Now()
	stats.Duration = stats.EndTime.Sub(stats.StartTime)
	stats.Loaded = w.progress.Load()
	stats.Errors = w.errors.Load()

	// Report final progress
	if w.options.OnProgress != nil {
		w.options.OnProgress(stats.Loaded, stats.Total, stats.Errors)
	}

	return stats, err
}

// WarmAsync starts warming in the background and returns immediately.
// Use the returned channel to receive completion notification.
func (w *Warmer) WarmAsync(source WarmingSource) <-chan *WarmingStats {
	result := make(chan *WarmingStats, 1)

	go func() {
		stats, _ := w.WarmFromSource(source)
		result <- stats
		close(result)
	}()

	return result
}

// WarmAsyncContext starts warming with context and returns a channel.
func (w *Warmer) WarmAsyncContext(ctx context.Context, source WarmingSource) <-chan *WarmingStats {
	result := make(chan *WarmingStats, 1)

	go func() {
		stats, _ := w.WarmFromSourceContext(ctx, source)
		result <- stats
		close(result)
	}()

	return result
}

// Progress returns the current warming progress.
func (w *Warmer) Progress() (loaded, total, errCount int64) {
	return w.progress.Load(), w.total.Load(), w.errors.Load()
}

// IsRunning returns true if a warming operation is in progress.
func (w *Warmer) IsRunning() bool {
	return w.running.Load()
}

// worker processes entries from the channel.
func (w *Warmer) worker(ctx context.Context, entries <-chan WarmEntry) {
	ttlCache, hasTTL := w.cache.(TTLCache)

	for {
		select {
		case <-ctx.Done():
			return
		case entry, ok := <-entries:
			if !ok {
				return
			}

			// Set entry with TTL if available
			ttl := entry.TTL
			if ttl == 0 {
				ttl = w.options.TTL
			}

			if hasTTL && ttl > 0 {
				ttlCache.SetWithTTL(entry.Key, entry.Value, ttl)
			} else {
				w.cache.Set(entry.Key, entry.Value)
			}

			w.progress.Add(1)
		}
	}
}

// feedEntries reads from source and sends to workers.
func (w *Warmer) feedEntries(ctx context.Context, source WarmingSource, entries chan<- WarmEntry) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		key, value, err := source.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			w.errors.Add(1)
			if !w.options.ContinueOnError {
				return err
			}
			continue
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case entries <- WarmEntry{Key: key, Value: value}:
		}
	}
}

// reportProgress periodically calls the progress callback.
func (w *Warmer) reportProgress(ctx context.Context, done <-chan struct{}) {
	ticker := time.NewTicker(w.options.ProgressInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			w.options.OnProgress(w.progress.Load(), w.total.Load(), w.errors.Load())
		}
	}
}

// ReaderSource implements WarmingSource for io.Reader (JSON Lines format).
type ReaderSource struct {
	scanner *bufio.Scanner
	count   int64
}

// NewReaderSource creates a WarmingSource from an io.Reader.
// Expects JSON Lines format (one JSON object per line).
func NewReaderSource(r io.Reader) *ReaderSource {
	return &ReaderSource{
		scanner: bufio.NewScanner(r),
		count:   -1, // Unknown
	}
}

// Next returns the next entry from the reader.
func (s *ReaderSource) Next() (key string, value interface{}, err error) {
	if !s.scanner.Scan() {
		if err := s.scanner.Err(); err != nil {
			return "", nil, err
		}
		return "", nil, io.EOF
	}

	var entry WarmEntry
	if err := json.Unmarshal(s.scanner.Bytes(), &entry); err != nil {
		return "", nil, fmt.Errorf("failed to parse entry: %w", err)
	}

	return entry.Key, entry.Value, nil
}

// Count returns -1 (unknown) for reader sources.
func (s *ReaderSource) Count() int64 {
	return s.count
}

// Close is a no-op for ReaderSource.
func (s *ReaderSource) Close() error {
	return nil
}

// MapSource implements WarmingSource for a map.
type MapSource struct {
	entries []WarmEntry
	index   int
	mu      sync.Mutex
}

// NewMapSource creates a WarmingSource from a map.
func NewMapSource(m map[string]interface{}) *MapSource {
	entries := make([]WarmEntry, 0, len(m))
	for k, v := range m {
		entries = append(entries, WarmEntry{Key: k, Value: v})
	}
	return &MapSource{entries: entries}
}

// Next returns the next entry from the map.
func (s *MapSource) Next() (key string, value interface{}, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.index >= len(s.entries) {
		return "", nil, io.EOF
	}

	entry := s.entries[s.index]
	s.index++
	return entry.Key, entry.Value, nil
}

// Count returns the number of entries in the map.
func (s *MapSource) Count() int64 {
	return int64(len(s.entries))
}

// Close is a no-op for MapSource.
func (s *MapSource) Close() error {
	return nil
}

// SliceSource implements WarmingSource for a slice of WarmEntry.
type SliceSource struct {
	entries []WarmEntry
	index   int
	mu      sync.Mutex
}

// NewSliceSource creates a WarmingSource from a slice.
func NewSliceSource(entries []WarmEntry) *SliceSource {
	return &SliceSource{entries: entries}
}

// Next returns the next entry from the slice.
func (s *SliceSource) Next() (key string, value interface{}, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.index >= len(s.entries) {
		return "", nil, io.EOF
	}

	entry := s.entries[s.index]
	s.index++
	return entry.Key, entry.Value, nil
}

// Count returns the number of entries in the slice.
func (s *SliceSource) Count() int64 {
	return int64(len(s.entries))
}

// Close is a no-op for SliceSource.
func (s *SliceSource) Close() error {
	return nil
}

// FuncSource implements WarmingSource for a generator function.
type FuncSource struct {
	fn    func() (string, interface{}, error)
	count int64
}

// NewFuncSource creates a WarmingSource from a generator function.
// The function should return io.EOF when exhausted.
func NewFuncSource(fn func() (string, interface{}, error), count int64) *FuncSource {
	return &FuncSource{
		fn:    fn,
		count: count,
	}
}

// Next calls the generator function.
func (s *FuncSource) Next() (key string, value interface{}, err error) {
	return s.fn()
}

// Count returns the entry count, or -1 if unknown.
func (s *FuncSource) Count() int64 {
	return s.count
}

// Close is a no-op for FuncSource.
func (s *FuncSource) Close() error {
	return nil
}

// ChannelSource implements WarmingSource for a channel.
type ChannelSource struct {
	ch    <-chan WarmEntry
	count int64
}

// NewChannelSource creates a WarmingSource from a channel.
func NewChannelSource(ch <-chan WarmEntry, count int64) *ChannelSource {
	return &ChannelSource{
		ch:    ch,
		count: count,
	}
}

// Next reads the next entry from the channel.
func (s *ChannelSource) Next() (key string, value interface{}, err error) {
	entry, ok := <-s.ch
	if !ok {
		return "", nil, io.EOF
	}
	return entry.Key, entry.Value, nil
}

// Count returns the entry count, or -1 if unknown.
func (s *ChannelSource) Count() int64 {
	return s.count
}

// Close is a no-op for ChannelSource.
func (s *ChannelSource) Close() error {
	return nil
}

// FileSource implements WarmingSource for a file with pre-counted entries.
type FileSource struct {
	file    *os.File
	scanner *bufio.Scanner
	count   int64
}

// NewFileSource creates a WarmingSource from a file path.
// It pre-counts entries for accurate progress reporting.
func NewFileSource(path string) (*FileSource, error) {
	cleanPath := filepath.Clean(path)
	// First pass: count lines
	file, err := os.Open(cleanPath)
	if err != nil {
		return nil, err
	}

	var count int64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		count++
	}
	if err := scanner.Err(); err != nil {
		_ = file.Close() // Ignore error in cleanup path
		return nil, err
	}

	// Reset file for reading
	if _, err := file.Seek(0, 0); err != nil {
		_ = file.Close() // Ignore error in cleanup path
		return nil, err
	}

	return &FileSource{
		file:    file,
		scanner: bufio.NewScanner(file),
		count:   count,
	}, nil
}

// Next returns the next entry from the file.
func (s *FileSource) Next() (key string, value interface{}, err error) {
	if !s.scanner.Scan() {
		if err := s.scanner.Err(); err != nil {
			return "", nil, err
		}
		return "", nil, io.EOF
	}

	var entry WarmEntry
	if err := json.Unmarshal(s.scanner.Bytes(), &entry); err != nil {
		return "", nil, fmt.Errorf("failed to parse entry: %w", err)
	}

	return entry.Key, entry.Value, nil
}

// Count returns the number of entries in the file.
func (s *FileSource) Count() int64 {
	return s.count
}

// Close closes the underlying file.
func (s *FileSource) Close() error {
	return s.file.Close()
}

// CombinedSource combines multiple sources into one.
type CombinedSource struct {
	sources []WarmingSource
	current int
	mu      sync.Mutex
}

// NewCombinedSource creates a WarmingSource that reads from multiple sources.
func NewCombinedSource(sources ...WarmingSource) *CombinedSource {
	return &CombinedSource{sources: sources}
}

// Next returns the next entry from the current source.
func (s *CombinedSource) Next() (key string, value interface{}, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for s.current < len(s.sources) {
		key, value, err := s.sources[s.current].Next()
		if errors.Is(err, io.EOF) {
			s.current++
			continue
		}
		return key, value, err
	}

	return "", nil, io.EOF
}

// Count returns the total count of all sources.
func (s *CombinedSource) Count() int64 {
	var total int64
	for _, src := range s.sources {
		count := src.Count()
		if count < 0 {
			return -1 // Unknown if any source is unknown
		}
		total += count
	}
	return total
}

// Close closes all underlying sources.
func (s *CombinedSource) Close() error {
	var lastErr error
	for _, src := range s.sources {
		if err := src.Close(); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

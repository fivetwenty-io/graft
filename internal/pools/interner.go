package pools

import (
	"hash/fnv"
	"math"
	"sync"
	"sync/atomic"
)

const (
	// DefaultShardCount is the number of shards for the string interner.
	// Using 16 shards provides a good balance between memory and contention reduction.
	DefaultShardCount = 16

	// DefaultMaxInternerSize is the default maximum number of strings to intern.
	// Set to 0 for unlimited.
	DefaultMaxInternerSize = 0

	// defaultInitialShardSize is the initial map size for each shard.
	defaultInitialShardSize = 64
)

// shard represents a single shard of the interner's map.
type shard struct {
	mu     sync.RWMutex
	intern map[string]string
}

// StringInterner provides thread-safe string interning for deduplication.
// It uses sharding to reduce lock contention in concurrent scenarios.
//
// String interning returns a canonical version of a string, allowing
// multiple equal strings to share a single underlying allocation.
type StringInterner struct {
	shards    []*shard
	numShards uint32
	maxSize   int64 // 0 means unlimited
	size      int64 // current total size across all shards
}

// NewStringInterner creates a new StringInterner with the specified options.
// Use shardCount of 0 for the default (16 shards).
// Use maxSize of 0 for unlimited size.
func NewStringInterner(shardCount, maxSize int) *StringInterner {
	if shardCount <= 0 {
		shardCount = DefaultShardCount
	}

	// Ensure shard count is a power of 2 for efficient modulo
	shardCount = nextPowerOfTwo(shardCount)

	// Validate upper bound to prevent integer overflow in uint32 conversion
	if shardCount > math.MaxInt32 {
		shardCount = DefaultShardCount
	}

	si := &StringInterner{
		shards:    make([]*shard, shardCount),
		numShards: uint32(shardCount), // #nosec G115 - bounds checked above
		maxSize:   int64(maxSize),
	}

	for i := range si.shards {
		si.shards[i] = &shard{
			intern: make(map[string]string, defaultInitialShardSize),
		}
	}

	return si
}

// nextPowerOfTwo returns the next power of two >= n.
func nextPowerOfTwo(n int) int {
	if n <= 1 {
		return 1
	}
	n--
	n |= n >> 1
	n |= n >> 2
	n |= n >> 4
	n |= n >> 8
	n |= n >> 16
	return n + 1
}

// getShard returns the shard for the given string based on its hash.
func (si *StringInterner) getShard(s string) *shard {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	idx := h.Sum32() & (si.numShards - 1)
	return si.shards[idx]
}

// Intern returns a canonical version of the string.
// If the string has been interned before, the previously interned version is returned.
// Otherwise, the string is stored and returned.
//
// This is safe for concurrent use.
func (si *StringInterner) Intern(s string) string {
	sh := si.getShard(s)

	// Fast path: check if already interned (read lock)
	sh.mu.RLock()
	if interned, ok := sh.intern[s]; ok {
		sh.mu.RUnlock()
		return interned
	}
	sh.mu.RUnlock()

	// Slow path: intern the string (write lock)
	sh.mu.Lock()
	defer sh.mu.Unlock()

	// Double-check after acquiring write lock
	if interned, ok := sh.intern[s]; ok {
		return interned
	}

	// Check size limit
	if si.maxSize > 0 {
		currentSize := atomic.LoadInt64(&si.size)
		if currentSize >= si.maxSize {
			// At capacity, just return the string without interning
			return s
		}
	}

	// Intern the string
	sh.intern[s] = s
	atomic.AddInt64(&si.size, 1)

	return s
}

// InternBytes is like Intern but accepts a byte slice.
// This avoids an allocation when the caller already has bytes.
func (si *StringInterner) InternBytes(b []byte) string {
	return si.Intern(string(b))
}

// Contains checks if a string has been interned.
func (si *StringInterner) Contains(s string) bool {
	sh := si.getShard(s)

	sh.mu.RLock()
	_, ok := sh.intern[s]
	sh.mu.RUnlock()

	return ok
}

// Size returns the total number of interned strings across all shards.
func (si *StringInterner) Size() int {
	return int(atomic.LoadInt64(&si.size))
}

// Clear removes all interned strings and resets the interner.
// This is safe for concurrent use but will block all other operations.
func (si *StringInterner) Clear() {
	for _, sh := range si.shards {
		sh.mu.Lock()
	}

	// Clear all shards
	for _, sh := range si.shards {
		sh.intern = make(map[string]string, defaultInitialShardSize)
	}

	atomic.StoreInt64(&si.size, 0)

	// Unlock in reverse order
	for i := len(si.shards) - 1; i >= 0; i-- {
		si.shards[i].mu.Unlock()
	}
}

// Stats returns statistics about the interner.
func (si *StringInterner) Stats() InternerStats {
	stats := InternerStats{
		ShardCount: int(si.numShards),
		TotalSize:  si.Size(),
		MaxSize:    int(si.maxSize),
		ShardSizes: make([]int, si.numShards),
	}

	for i, sh := range si.shards {
		sh.mu.RLock()
		stats.ShardSizes[i] = len(sh.intern)
		sh.mu.RUnlock()
	}

	return stats
}

// InternerStats contains statistics about a StringInterner.
type InternerStats struct {
	ShardCount int   // Number of shards
	TotalSize  int   // Total interned strings
	MaxSize    int   // Maximum size (0 = unlimited)
	ShardSizes []int // Size of each shard
}

// Strings is the global string interner instance.
// It uses 16 shards and has no size limit by default.
var Strings = NewStringInterner(DefaultShardCount, DefaultMaxInternerSize)

// InternString interns a string using the global interner.
// This is a convenience function equivalent to Strings.Intern(s).
func InternString(s string) string {
	return Strings.Intern(s)
}

// PreInternCommonStrings pre-populates the global interner with common strings
// used throughout graft. This reduces allocations for frequently used strings.
func PreInternCommonStrings() {
	// Operator names
	operators := []string{
		"grab", "concat", "vault", "static_ips", "calc", "defer",
		"join", "keys", "sort", "prune", "param", "inject",
		"file", "base64", "empty", "load", "stringify", "null",
		"ips", "cartesian-product", "shuffle", "awsparam", "awssecret",
		"vault-try", "ternary", "negate", "base64-decode",
		"add", "subtract", "multiply", "divide", "modulo",
	}

	// Common literals
	literals := []string{
		"true", "false", "null", "nil", "",
		"name", "type", "value", "key", "path",
		"0", "1", "-1", "2", "3", "4", "5", "6", "7", "8", "9", "10",
	}

	// Boolean operators
	boolOps := []string{
		"and", "or", "not", "&&", "||", "!",
		"==", "!=", "<", ">", "<=", ">=",
	}

	// Common path components
	pathComponents := []string{
		".", "..", "/", "meta", "properties", "spec", "status",
		"metadata", "annotations", "labels", "resources",
	}

	// Intern all common strings
	for _, s := range operators {
		Strings.Intern(s)
	}
	for _, s := range literals {
		Strings.Intern(s)
	}
	for _, s := range boolOps {
		Strings.Intern(s)
	}
	for _, s := range pathComponents {
		Strings.Intern(s)
	}
}

//nolint:gochecknoinits // Pre-interning common strings at startup improves performance
func init() {
	// Initialize the global interner with common strings
	PreInternCommonStrings()
}

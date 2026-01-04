package pools

import (
	"sync"
)

const (
	// DefaultMaxSliceCapacity is the maximum slice capacity that will be returned to the pool.
	// Slices larger than this are discarded to prevent memory bloat.
	DefaultMaxSliceCapacity = 1024

	// defaultSliceInitialCapacity is the initial capacity for new slices.
	defaultSliceInitialCapacity = 16
)

// StringSlicePool manages a pool of reusable []string slices.
// It uses sync.Pool internally for efficient allocation and reuse.
type StringSlicePool struct {
	pool        sync.Pool
	maxCapacity int
}

// NewStringSlicePool creates a new StringSlicePool with the specified maximum capacity.
// Slices exceeding this capacity will be discarded rather than returned to the pool.
func NewStringSlicePool(maxCapacity int) *StringSlicePool {
	if maxCapacity <= 0 {
		maxCapacity = DefaultMaxSliceCapacity
	}

	sp := &StringSlicePool{
		maxCapacity: maxCapacity,
	}

	sp.pool = sync.Pool{
		New: func() interface{} {
			s := make([]string, 0, defaultSliceInitialCapacity)
			return &s
		},
	}

	return sp
}

// Get retrieves a string slice from the pool with at least the specified capacity.
// The returned slice is guaranteed to be empty (length 0) but will have capacity
// at least equal to the requested capacity.
//
// If the pooled slice has insufficient capacity, a new slice is allocated.
// The caller should return the slice to the pool using Put when done.
func (sp *StringSlicePool) Get(capacity int) []string {
	ptr, ok := sp.pool.Get().(*[]string)
	if !ok {
		return make([]string, 0, capacity)
	}
	slice := *ptr

	// Reset length but keep capacity
	slice = slice[:0]

	// If we need more capacity than available, allocate new slice
	if cap(slice) < capacity {
		// Return the small slice to the pool for reuse
		sp.pool.Put(ptr)
		// Return a new slice with the requested capacity
		return make([]string, 0, capacity)
	}

	// Return the slice - caller must call Put() when done
	return slice
}

// GetPtr retrieves a pointer to a string slice from the pool.
// This is useful when you need to append and the slice might grow.
// The returned slice is guaranteed to be empty (length 0).
//
// Callers must use PutPtr to return the pointer to the pool.
func (sp *StringSlicePool) GetPtr() *[]string {
	ptr, ok := sp.pool.Get().(*[]string)
	if !ok {
		s := make([]string, 0, defaultSliceInitialCapacity)
		return &s
	}
	*ptr = (*ptr)[:0] // Reset length but keep capacity
	return ptr
}

// Put returns a string slice to the pool for reuse.
// Slices with capacity exceeding the maximum are discarded.
// The slice elements are not zeroed; use PutClean for sensitive data.
func (sp *StringSlicePool) Put(slice []string) {
	if slice == nil {
		return
	}

	// Only return slices within the capacity limit
	if cap(slice) <= sp.maxCapacity {
		s := slice[:0]
		sp.pool.Put(&s)
	}
	// Oversized slices are left for garbage collection
}

// PutPtr returns a string slice pointer to the pool.
// This should be used when the slice was obtained via GetPtr.
func (sp *StringSlicePool) PutPtr(ptr *[]string) {
	if ptr == nil || *ptr == nil {
		return
	}

	// Only return slices within the capacity limit
	if cap(*ptr) <= sp.maxCapacity {
		*ptr = (*ptr)[:0]
		sp.pool.Put(ptr)
	}
}

// PutClean returns a string slice to the pool after clearing all elements.
// This should be used when the slice may contain sensitive data or
// references that should be released for garbage collection.
func (sp *StringSlicePool) PutClean(slice []string) {
	if slice == nil {
		return
	}

	// Clear all elements to release references
	for i := range slice {
		slice[i] = ""
	}

	sp.Put(slice)
}

// MaxCapacity returns the maximum slice capacity that will be pooled.
func (sp *StringSlicePool) MaxCapacity() int {
	return sp.maxCapacity
}

// StringSlices is the global string slice pool instance.
// It uses the default maximum capacity of 1024 elements.
var StringSlices = NewStringSlicePool(DefaultMaxSliceCapacity)

// GetStringSlice retrieves a string slice from the global pool with default capacity.
// This is a convenience function equivalent to StringSlices.Get(0).
func GetStringSlice() []string {
	return StringSlices.Get(0)
}

// GetStringSliceN retrieves a string slice from the global pool with at least n capacity.
// This is a convenience function equivalent to StringSlices.Get(n).
func GetStringSliceN(n int) []string {
	return StringSlices.Get(n)
}

// PutStringSlice returns a string slice to the global pool.
// This is a convenience function equivalent to StringSlices.Put(slice).
func PutStringSlice(slice []string) {
	StringSlices.Put(slice)
}

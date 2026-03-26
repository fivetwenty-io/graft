package backends

import "context"

// SecretBackend provides a uniform interface for secret/value retrieval.
type SecretBackend interface {
	// Get retrieves a value at the given path and key.
	Get(ctx context.Context, path, key string) (interface{}, error)

	// GetWithTarget retrieves a value using a named target configuration.
	GetWithTarget(ctx context.Context, target, path, key string) (interface{}, error)

	// IsSkipped returns true if this backend should be skipped.
	IsSkipped() bool

	// Close releases backend resources.
	Close() error
}

// CachingBackend extends SecretBackend with cache management.
type CachingBackend interface {
	SecretBackend

	// GetCached retrieves from cache, returns (value, found).
	GetCached(path, key string) (interface{}, bool)

	// SetCached stores a value in cache.
	SetCached(path, key string, value interface{})

	// ClearCache resets the cache.
	ClearCache()
}

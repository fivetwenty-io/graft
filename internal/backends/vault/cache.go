package vault

import "sync"

// SecretCache provides thread-safe caching for vault secrets keyed by path.
// It replaces the per-engine vault cache that was previously on OperatorState.
var SecretCache = &secretCache{
	data: make(map[string]map[string]interface{}),
}

type secretCache struct {
	mu   sync.RWMutex
	data map[string]map[string]interface{}
}

// Get returns cached secret data for the given path, or nil and false.
func (c *secretCache) Get(path string) (map[string]interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.data[path]
	return v, ok
}

// Set stores secret data in the cache for the given path.
func (c *secretCache) Set(path string, data map[string]interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[path] = data
}

// Reset clears the entire cache.
func (c *secretCache) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = make(map[string]map[string]interface{})
}

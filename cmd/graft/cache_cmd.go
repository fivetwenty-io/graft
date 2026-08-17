package main

// The `graft cache` subcommands: inspection and maintenance for the
// persistent cache (see merge_cache.go and merge_parse_cache.go). Both
// work regardless of GRAFT_CACHE_L2_ENABLED - the directory may hold
// entries from earlier enabled runs - and neither ever creates a cache
// directory that does not already exist.

import (
	"os"
	"path/filepath"

	"github.com/fivetwenty-io/graft/internal/cache"
	"github.com/fivetwenty-io/graft/internal/config"
	"github.com/fivetwenty-io/graft/log"
)

// persistentCacheLayers names every store subdirectory under the cache
// root.
var persistentCacheLayers = []string{"output", "parse"}

// persistentCacheRoot resolves the cache root directory: the configured
// path, or the platform user cache directory.
func persistentCacheRoot(cfg config.CacheConfig) (string, bool) {
	dir := cfg.L2Path
	if dir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return "", false
		}
		dir = filepath.Join(base, "graft")
	}
	return dir, true
}

// layerStats returns entry count and byte size for one layer, without
// creating its directory when absent.
func layerStats(root, layer string) (int, int64) {
	dir := filepath.Join(root, layer)
	if _, err := os.Stat(dir); err != nil {
		return 0, 0
	}
	store, err := cache.OpenFileStore(dir, 0)
	if err != nil {
		return 0, 0
	}
	count, size, err := store.Stats()
	if err != nil {
		return 0, 0
	}
	return count, size
}

func handleCacheStats(cfg config.CacheConfig) int {
	root, ok := persistentCacheRoot(cfg)
	if !ok {
		log.PrintStdErrf("Unable to determine the cache directory\n")
		return 1
	}
	printStdOutf("Cache directory: %s\n", root)
	for _, layer := range persistentCacheLayers {
		count, size := layerStats(root, layer)
		printStdOutf("%s: %d entries, %d bytes\n", layer, count, size)
	}
	return 0
}

func handleCacheClear(cfg config.CacheConfig) int {
	root, ok := persistentCacheRoot(cfg)
	if !ok {
		log.PrintStdErrf("Unable to determine the cache directory\n")
		return 1
	}
	cleared := 0
	for _, layer := range persistentCacheLayers {
		dir := filepath.Join(root, layer)
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		store, err := cache.OpenFileStore(dir, 0)
		if err != nil {
			log.PrintStdErrf("Unable to open cache layer %s: %s\n", layer, err.Error())
			return 1
		}
		count, _, _ := store.Stats()
		if err := store.Clear(); err != nil {
			log.PrintStdErrf("Unable to clear cache layer %s: %s\n", layer, err.Error())
			return 1
		}
		cleared += count
	}
	printStdOutf("Cleared %d entries from %s\n", cleared, root)
	return 0
}

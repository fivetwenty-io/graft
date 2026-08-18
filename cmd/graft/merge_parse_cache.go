package main

// Layer 2 of graft's persistent cache: the per-document parse cache. A
// document whose bytes were parsed by any previous run - in this merge
// or any other - is materialized by gob-decoding its stored tree instead
// of re-running control-flow expansion, goccy parsing, and compat
// conversion. It fires on invocations Layer 1 cannot serve (a vault
// merge re-evaluates every run, but its input documents rarely change).
//
// Correctness:
//
//   - The key is the document's exact bytes (hashed) salted with the
//     graft version and a schema tag. Parsing is a pure function of the
//     bytes for every document without control-flow markers, so nothing
//     else can influence the result.
//
//   - Documents with control-flow markers are excluded entirely (never
//     looked up, never stored): marker expansion evaluates operators -
//     including external ones like vault - during parse.
//
//   - go-patch array documents never reach the cache; the go-patch
//     branch in parseOneYamlFile returns before the lookup.
//
//   - Every hit decodes a fresh tree, so cached entries can never alias
//     a tree the merge later mutates in place. The store happens
//     immediately after parse, before any merge runs.
//
//   - Encode or decode trouble is never an error: an unencodable tree
//     is simply not stored, a corrupt entry is a miss.

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"strconv"
	"sync"
	"time"

	"github.com/fivetwenty-io/graft/internal/cache"
)

// registerTreeGobTypes registers the composite types that appear inside
// interface{} slots of parsed trees; gob needs them to move a tree.
// Scalars (string, int, int64, uint64, float64, bool) are predefined by
// gob. Run before every encode and decode, once per process.
var registerTreeGobTypes = sync.OnceFunc(func() {
	gob.Register(map[string]interface{}{})
	gob.Register([]interface{}{})
	gob.Register(map[interface{}]interface{}{})
	gob.Register(time.Time{})
})

// openMergeParseCache returns the parse-cache store for this invocation,
// or nil when the cache must not participate (same gates as the output
// cache: disabled, debug/trace, unusable directory).
func openMergeParseCache(opts *mergeOpts) *cache.FileStore {
	return openPersistentStore(opts, "parse")
}

// parseCacheKey derives the cache key for one document's bytes. The
// schema tag separates this namespace from the output cache; the graft
// version salts it because parsing behavior (compat conversions, parser
// workarounds) can change between releases.
func parseCacheKey(data []byte) string {
	h := sha256.New()
	field := func(b []byte) {
		var lenBuf [10]byte
		h.Write(strconv.AppendInt(lenBuf[:0], int64(len(b)), 10))
		h.Write([]byte{':'})
		h.Write(b)
	}
	field([]byte("graft-parse-tree-v1"))
	field([]byte(Version))
	field(data)
	return hex.EncodeToString(h.Sum(nil))
}

// cachedParseTree is one parse-cache entry: the materialized tree of a
// successfully parsed document.
type cachedParseTree struct {
	Tree map[string]interface{}
}

func encodeCachedTree(tree map[string]interface{}) ([]byte, error) {
	registerTreeGobTypes()
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(cachedParseTree{Tree: tree}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decodeCachedTree(data []byte) (map[string]interface{}, bool) {
	registerTreeGobTypes()
	var entry cachedParseTree
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&entry); err != nil {
		return nil, false
	}
	if entry.Tree == nil {
		entry.Tree = map[string]interface{}{}
	}
	return entry.Tree, true
}

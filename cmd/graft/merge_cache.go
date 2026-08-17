package main

// Layer 1 of graft's persistent cache: the merge output cache. A repeat
// invocation with byte-identical inputs and an identical configuration
// replays the previous run's exact stdout and stderr bytes without
// parsing, merging, evaluating, or marshaling anything.
//
// Correctness rests on two pillars:
//
//   - The key (mergeOutputCacheKey) covers everything that can influence
//     the output bytes: graft version, every merge flag, the
//     DEFAULT_ARRAY_MERGE_KEY environment override, the resolved color
//     mode (stderr warnings render differently), and the ordered content
//     hashes of every input document. Content hashes - never paths or
//     mtimes - so Genesis-style temp files hit across runs and any edit
//     misses.
//
//   - The store gate (outputCacheable) admits only invocations whose
//     output is a pure function of those inputs: no operator that
//     consults an external system (vault, aws, nats), the filesystem
//     (file, load), the environment (raw_env, $VAR references), or
//     randomness (shuffle), and no control-flow markers (which can
//     evaluate any of the above during parse). The scan over-approximates
//     freely - a false "impure" only costs a cache miss - but must never
//     under-approximate.

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fivetwenty-io/graft/internal/cache"
	"github.com/fivetwenty-io/graft/internal/utils/ansi"
	"github.com/fivetwenty-io/graft/log"
	"github.com/fivetwenty-io/graft/pkg/graft"
	"github.com/fivetwenty-io/graft/pkg/graft/controlflow"
)

// persistentCacheTTL bounds how long a persistent cache entry may be
// served. It exists to keep the cache directory from growing without
// bound, not for correctness - entries are content-addressed, so a stale
// hit is impossible; an unused entry just lingers until this age.
const persistentCacheTTL = 7 * 24 * time.Hour

// openMergeOutputCache returns the store for this invocation's output
// cache, or nil when the cache must not participate: disabled by
// configuration, a debug/trace run (its diagnostics have to come from a
// real merge), or an unusable cache directory (cache trouble must never
// break a merge).
func openMergeOutputCache(opts *mergeOpts) *cache.FileStore {
	if !opts.CacheCfg.L2Enabled || log.DebugOn || log.TraceOn {
		return nil
	}
	dir := opts.CacheCfg.L2Path
	if dir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return nil
		}
		dir = filepath.Join(base, "graft")
	}
	store, err := cache.OpenFileStore(filepath.Join(dir, "output"), persistentCacheTTL)
	if err != nil {
		return nil
	}
	return store
}

// cachedMergeOutput is one output-cache entry: the exact bytes a
// successful merge wrote to stdout and stderr. Exit codes are not stored
// because only exit-0 runs are ever cached.
type cachedMergeOutput struct {
	Stdout []byte
	Stderr []byte
}

func encodeCachedOutput(out cachedMergeOutput) ([]byte, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decodeCachedOutput(data []byte) (cachedMergeOutput, bool) {
	var out cachedMergeOutput
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&out); err != nil {
		return cachedMergeOutput{}, false
	}
	return out, true
}

// handleMergeCached is handleMerge's merge execution with the output
// cache in front: resolve and read every input up front, replay a hit's
// stored stdout/stderr bytes, and on a miss run the ordinary merge with
// stderr teed so a successful, cacheable run can be stored for next
// time.
func handleMergeCached(opts *mergeOpts, store *cache.FileStore) int {
	files, err := resolveMergeInputFiles(opts)
	if err != nil {
		log.PrintStdErrf("%s\n", err.Error())
		return 2
	}

	inputs := make([][]byte, len(files))
	for i := range files {
		data, readErr := readFile(&files[i])
		if readErr != nil {
			// The message is the same one a cache-off run prints for
			// this file; only the surfacing order can differ (the
			// cache-off path reads and parses concurrently, so an
			// earlier file's parse error could win the race). A read
			// failing after a successful open is rare enough to accept
			// that.
			log.PrintStdErrf("%s\n", readErr.Error())
			return 2
		}
		inputs[i] = data
		files[i].Reader = io.NopCloser(bytes.NewReader(data))
	}

	key := mergeOutputCacheKey(opts, inputs, ansi.IsColorEnabled())
	if data, ok := store.Get(key); ok {
		if out, valid := decodeCachedOutput(data); valid {
			if len(out.Stderr) > 0 {
				log.PrintStdErrf("%s", string(out.Stderr))
			}
			printStdOutf("%s", string(out.Stdout))
			return 0
		}
		// Corrupt entry: fall through to a real merge, which will
		// overwrite it with a good one.
	}

	// Tee stderr so a stored entry can replay warnings byte-for-byte.
	origErrf := log.PrintStdErrf
	var stderrBuf bytes.Buffer
	log.PrintStdErrf = func(format string, args ...interface{}) {
		fmt.Fprintf(&stderrBuf, format, args...)
		origErrf(format, args...)
	}
	defer func() { log.PrintStdErrf = origErrf }()

	tree, _, err := mergeAllDocs(files, opts)
	if err != nil {
		log.PrintStdErrf("%s\n", err.Error())
		return 2
	}

	out, rc := renderMergedTree(tree)
	if rc != 0 {
		return rc
	}
	printStdOutf("%s", string(out))

	if outputCacheable(inputs, opts.SkipEval) {
		entry := cachedMergeOutput{Stdout: out, Stderr: stderrBuf.Bytes()}
		if encoded, encErr := encodeCachedOutput(entry); encErr == nil {
			_ = store.Put(key, encoded)
		}
	}
	return 0
}

// pureOperators lists every registered operator whose evaluation is a
// deterministic function of the input documents alone. An operator name
// that is registered but absent here - including any operator added in
// the future - is treated as impure, so new operators default to
// uncacheable until someone proves purity and adds them.
var pureOperators = map[string]bool{
	// Named operators.
	"base64": true, "base64-decode": true, "calc": true,
	"cartesian": true, "cartesian-product": true, "concat": true,
	"defer": true, "empty": true, "flatten": true, "grab": true,
	"inject": true, "ips": true, "join": true, "keys": true,
	"negate": true, "null": true, "param": true, "prune": true,
	"sort": true, "split": true, "static_ips": true, "stringify": true,
	"type": true, "uniq": true,
	// Symbol operators (only "-" can surface from the token scan, but
	// list them all so the set reads as a complete purity census).
	"-": true, "+": true, "*": true, "/": true, "%": true, "!": true,
	"==": true, "!=": true, "<": true, "<=": true, ">": true, ">=": true,
	"&&": true, "||": true, "?:": true,
}

// registeredOperatorNames returns the set of operator names known to the
// process-global registry, computed once. Matching scan tokens against
// this set (instead of a hardcoded impure list) is what makes unknown
// future operators default to impure: they are registered, not in
// pureOperators, and therefore poison cacheability.
var registeredOperatorNames = sync.OnceValue(func() map[string]bool {
	names := graft.DefaultRegistry.ListOperators()
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[name] = true
	}
	return set
})

// outputCacheable reports whether a merge over the given input documents
// may be served from / stored into the output cache. skipEval mirrors
// --skip-eval: operators are then inert strings, so only parse-time
// evaluation (control-flow markers) can break purity.
func outputCacheable(inputs [][]byte, skipEval bool) bool {
	for _, data := range inputs {
		if !bytes.Contains(data, []byte("((")) {
			continue
		}
		if controlflow.HasMarkers(data) {
			return false
		}
		if skipEval {
			continue
		}
		if !operatorSpansArePure(string(data)) {
			return false
		}
	}
	return true
}

// operatorSpansArePure scans every "(( ... ))" span in text and reports
// whether all of them are pure. Impure or unscannable spans - env
// references, impure or unknown-registered operator names, an
// unterminated span - report false.
func operatorSpansArePure(text string) bool {
	i := 0
	for {
		rel := strings.Index(text[i:], "((")
		if rel < 0 {
			return true
		}
		start := i + rel + 2
		end, ok := findSpanEnd(text, start)
		if !ok {
			// No closing "))" outside quotes: we cannot tell where the
			// operator expression ends, so assume the worst.
			return false
		}
		if !operatorSpanIsPure(text[start:end]) {
			return false
		}
		i = end + 2
	}
}

// findSpanEnd returns the index of the "))" closing the operator span
// that starts at text[from], honoring quoted strings (so a literal "))"
// inside an argument does not end the span) and nested parentheses (so a
// nested call's ")" does not pair with the span's opener).
func findSpanEnd(text string, from int) (int, bool) {
	depth := 0
	i := from
	n := len(text)
	for i < n {
		switch text[i] {
		case '"', '\'':
			j := skipQuotedArg(text, i)
			if j < 0 {
				return 0, false
			}
			i = j
		case '(':
			depth++
			i++
		case ')':
			if depth == 0 {
				if i+1 < n && text[i+1] == ')' {
					return i, true
				}
				return 0, false // unbalanced close
			}
			depth--
			i++
		default:
			i++
		}
	}
	return 0, false
}

// skipQuotedArg returns the index just past the closing quote matching
// the quote character at text[start], honoring backslash escapes, or -1
// if unterminated.
func skipQuotedArg(text string, start int) int {
	quote := text[start]
	n := len(text)
	j := start + 1
	for j < n {
		switch {
		case text[j] == '\\' && j+1 < n:
			j += 2
		case text[j] == quote:
			return j + 1
		default:
			j++
		}
	}
	return -1
}

// operatorSpanIsPure inspects one span's text. A "$" anywhere means a
// possible environment-variable reference (expr_evaluation resolves
// $NAME via os.Getenv). Every dot-free identifier-like token that names a
// registered operator must be in pureOperators; dotted tokens are tree
// references and always pure.
func operatorSpanIsPure(span string) bool {
	if strings.ContainsRune(span, '$') {
		return false
	}
	registered := registeredOperatorNames()
	n := len(span)
	for i := 0; i < n; {
		if !isTokenByte(span[i]) {
			i++
			continue
		}
		j := i
		for j < n && isTokenByte(span[j]) {
			j++
		}
		token := span[i:j]
		i = j
		if strings.ContainsRune(token, '.') {
			continue
		}
		if registered[token] && !pureOperators[token] {
			return false
		}
	}
	return true
}

// isTokenByte defines the token alphabet for the purity scan: the
// characters operator names are built from ("base64-decode",
// "static_ips") plus "." so tree references come out as single dotted
// tokens and are recognized as references rather than operator names.
func isTokenByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
		c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.'
}

// mergeOutputCacheKey derives the output-cache key for one merge
// invocation. Every field is written length-delimited into the hash so
// adjacent values can never collide by concatenation.
func mergeOutputCacheKey(opts *mergeOpts, inputs [][]byte, colorEnabled bool) string {
	h := sha256.New()
	field := func(s string) {
		var lenBuf [10]byte
		h.Write(strconv.AppendInt(lenBuf[:0], int64(len(s)), 10))
		h.Write([]byte{':'})
		h.Write([]byte(s))
	}

	field("graft-merge-output-v1")
	field(Version)
	field(strconv.FormatBool(opts.SkipEval))
	field(strconv.FormatBool(opts.MultiDoc))
	field(strconv.FormatBool(opts.EnableGoPatch))
	field(strconv.FormatBool(opts.FallbackAppend))
	field(opts.DataflowOrder)
	field(strconv.Itoa(len(opts.Prune)))
	for _, p := range opts.Prune {
		field(p)
	}
	field(strconv.Itoa(len(opts.CherryPick)))
	for _, c := range opts.CherryPick {
		field(c)
	}
	field(os.Getenv("DEFAULT_ARRAY_MERGE_KEY"))
	field(strconv.FormatBool(colorEnabled))
	field(strconv.Itoa(len(inputs)))
	for _, data := range inputs {
		sum := sha256.Sum256(data)
		field(string(sum[:]))
	}

	return hex.EncodeToString(h.Sum(nil))
}

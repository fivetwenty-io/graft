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
	"encoding/hex"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/fivetwenty-io/graft/pkg/graft"
	"github.com/fivetwenty-io/graft/pkg/graft/controlflow"
)

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

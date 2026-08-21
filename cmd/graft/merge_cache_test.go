package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strconv"
	"testing"
)

// docs is shorthand for building the [][]byte input-list shape the cache
// helpers take.
func docs(ss ...string) [][]byte {
	out := make([][]byte, len(ss))
	for i, s := range ss {
		out[i] = []byte(s)
	}
	return out
}

func TestOutputCacheablePureDocuments(t *testing.T) {
	cases := []struct {
		name string
		doc  string
	}{
		{"no operators", "meta:\n  name: thing\n  script: |\n    echo $HOME\n"},
		{"grab", "a: (( grab meta.b ))\nmeta: {b: 1}\n"},
		{"concat and calc", "a: (( concat \"x-\" meta.b ))\nc: (( calc \"2 * 3\" ))\n"},
		{"join keys sort", "a: (( join \",\" meta.list ))\nb: (( keys meta ))\nc: (( sort ))\n"},
		{"arithmetic", "a: (( 5 - 3 ))\nb: (( meta.x || \"default\" ))\n"},
		{"reference containing vault as a path segment", "a: (( grab meta.vault.path ))\n"},
		{"defer", "a: (( defer grab meta.b ))\n"},
		{"static_ips", "a: (( static_ips 0 1 2 ))\n"},
	}
	for _, tc := range cases {
		if !outputCacheable(docs(tc.doc), false) {
			t.Errorf("%s: expected cacheable", tc.name)
		}
	}
}

func TestOutputCacheableImpureDocuments(t *testing.T) {
	cases := []struct {
		name string
		doc  string
	}{
		{"vault", "secret: (( vault \"a/b:c\" ))\n"},
		{"vault-try", "secret: (( vault-try \"a\" \"b\" \"x\" ))\n"},
		{"nested vault inside concat", "secret: (( concat \"p-\" (vault \"a/b:c\") ))\n"},
		{"awsparam", "p: (( awsparam \"/x\" ))\n"},
		{"awssecret", "p: (( awssecret \"x\" ))\n"},
		{"nats", "p: (( nats \"kv:store/key\" ))\n"},
		{"file", "p: (( file \"other.yml\" ))\n"},
		{"load", "p: (( load \"other.yml\" ))\n"},
		{"raw_env", "p: (( raw_env HOME ))\n"},
		{"shuffle is nondeterministic", "p: (( shuffle meta.list ))\n"},
		{"env reference", "p: (( grab $HOME ))\n"},
		{"quoted close hiding a nested vault", "p: (( concat \"a))b\" (vault \"a/b:c\") ))\n"},
		{"unterminated span", "p: (( grab meta.x\n"},
	}
	for _, tc := range cases {
		if outputCacheable(docs(tc.doc), false) {
			t.Errorf("%s: expected NOT cacheable", tc.name)
		}
	}
}

// TestOutputCacheableSkipEval: with --skip-eval operators are inert
// strings, so even vault calls are cacheable - but control-flow markers
// still evaluate during parse and stay uncacheable.
func TestOutputCacheableSkipEval(t *testing.T) {
	vaultDoc := "secret: (( vault \"a/b:c\" ))\n"
	if !outputCacheable(docs(vaultDoc), true) {
		t.Error("skip-eval vault doc: expected cacheable")
	}

	cfDoc := "(( if meta.on ))\nsecret: (( vault \"a/b:c\" ))\n(( fi ))\n"
	if outputCacheable(docs(cfDoc), true) {
		t.Error("skip-eval control-flow doc: expected NOT cacheable")
	}
}

func TestOutputCacheableControlFlow(t *testing.T) {
	cfDoc := "(( if meta.on ))\nname: x\n(( fi ))\n"
	if outputCacheable(docs(cfDoc), false) {
		t.Error("control-flow doc: expected NOT cacheable")
	}
}

// TestOutputCacheableAnyImpureDocPoisons: one impure document anywhere in
// the input list makes the whole invocation uncacheable.
func TestOutputCacheableAnyImpureDocPoisons(t *testing.T) {
	pure := "a: (( grab meta.b ))\n"
	impure := "s: (( vault \"a/b:c\" ))\n"
	if outputCacheable(docs(pure, impure, pure), false) {
		t.Error("expected NOT cacheable with one impure doc present")
	}
	if !outputCacheable(docs(pure, pure), false) {
		t.Error("expected cacheable with only pure docs")
	}
}

func TestMergeOutputCacheKeyStability(t *testing.T) {
	opts := &mergeOpts{}
	inputs := docs("a: 1\n", "b: 2\n")

	k1 := mergeOutputCacheKey(opts, inputs, false)
	k2 := mergeOutputCacheKey(opts, inputs, false)
	if k1 != k2 {
		t.Fatal("identical invocations must produce identical keys")
	}
}

func TestMergeOutputCacheKeySensitivity(t *testing.T) {
	base := func() (*mergeOpts, [][]byte, bool) {
		return &mergeOpts{DataflowOrder: "alphabetical"}, docs("a: 1\n", "b: 2\n"), false
	}
	opts, inputs, color := base()
	baseKey := mergeOutputCacheKey(opts, inputs, color)

	mutations := []struct {
		name string
		key  func() string
	}{
		{"input content", func() string {
			o, in, c := base()
			in[1] = []byte("b: 3\n")
			return mergeOutputCacheKey(o, in, c)
		}},
		{"input order", func() string {
			o, in, c := base()
			in[0], in[1] = in[1], in[0]
			return mergeOutputCacheKey(o, in, c)
		}},
		{"input count", func() string {
			o, in, c := base()
			return mergeOutputCacheKey(o, append(in, []byte("c: 3\n")), c)
		}},
		{"skip-eval", func() string {
			o, in, c := base()
			o.SkipEval = true
			return mergeOutputCacheKey(o, in, c)
		}},
		{"multi-doc", func() string {
			o, in, c := base()
			o.MultiDoc = true
			return mergeOutputCacheKey(o, in, c)
		}},
		{"go-patch", func() string {
			o, in, c := base()
			o.EnableGoPatch = true
			return mergeOutputCacheKey(o, in, c)
		}},
		{"fallback-append", func() string {
			o, in, c := base()
			o.FallbackAppend = true
			return mergeOutputCacheKey(o, in, c)
		}},
		{"dataflow order", func() string {
			o, in, c := base()
			o.DataflowOrder = "insertion"
			return mergeOutputCacheKey(o, in, c)
		}},
		{"prune list", func() string {
			o, in, c := base()
			o.Prune = []string{"meta"}
			return mergeOutputCacheKey(o, in, c)
		}},
		{"cherry-pick list", func() string {
			o, in, c := base()
			o.CherryPick = []string{"jobs"}
			return mergeOutputCacheKey(o, in, c)
		}},
		{"color", func() string {
			o, in, _ := base()
			return mergeOutputCacheKey(o, in, true)
		}},
	}

	seen := map[string]string{baseKey: "base"}
	for _, m := range mutations {
		k := m.key()
		if prev, dup := seen[k]; dup {
			t.Errorf("%s: key collides with %s", m.name, prev)
		}
		seen[k] = m.name
	}
}

// TestMergeOutputCacheKeyListValueVsBoundary: list entries must be length
// -delimited, not naively concatenated - --prune a,b and --prune ab must
// not collide.
func TestMergeOutputCacheKeyListValueVsBoundary(t *testing.T) {
	inputs := docs("a: 1\n")
	k1 := mergeOutputCacheKey(&mergeOpts{Prune: []string{"a", "b"}}, inputs, false)
	k2 := mergeOutputCacheKey(&mergeOpts{Prune: []string{"ab"}}, inputs, false)
	if k1 == k2 {
		t.Fatal("prune [a b] and [ab] must produce different keys")
	}
}

func TestMergeOutputCacheKeyEnvAndVersion(t *testing.T) {
	inputs := docs("a: 1\n")
	opts := &mergeOpts{}

	before := mergeOutputCacheKey(opts, inputs, false)
	t.Setenv("DEFAULT_ARRAY_MERGE_KEY", "id")
	after := mergeOutputCacheKey(opts, inputs, false)
	if before == after {
		t.Error("DEFAULT_ARRAY_MERGE_KEY must be part of the key")
	}

	origVersion := Version
	defer func() { Version = origVersion }()
	Version = origVersion + "-test"
	t.Setenv("DEFAULT_ARRAY_MERGE_KEY", "")
	bumped := mergeOutputCacheKey(opts, inputs, false)
	if bumped == before {
		t.Error("graft version must salt the key")
	}
}

// legacyV1MergeOutputCacheKey reproduces the pre-"---\n"-prefix output
// cache key exactly as mergeOutputCacheKey computed it before
// renderMergedTree started prepending "---\n" to merge output, using
// mergeOutputCacheKey's own field-writer shape (length-delimited, same
// field order) but the old "graft-merge-output-v1" schema string. It
// exists only for TestMergeOutputCacheKeySchemaVersionBumped below.
func legacyV1MergeOutputCacheKey(opts *mergeOpts, inputs [][]byte, colorEnabled bool) string {
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

// TestMergeOutputCacheKeySchemaVersionBumped is the cache-replay-safety
// regression for the "---\n" merge-output-prefix fix: an output-cache
// entry a v1-schema (pre-fix) graft binary stored - bare YAML, no
// leading "---\n" - must never be replayed by the current binary as if
// it were valid for identical opts/inputs/color, even when Version is
// unchanged (an unreleased/dev build, where the Version field alone
// would not salt the key differently). Bumping the schema string to
// "graft-merge-output-v2" (mergeOutputCacheKey) guarantees this
// independently of Version.
func TestMergeOutputCacheKeySchemaVersionBumped(t *testing.T) {
	opts := &mergeOpts{DataflowOrder: "alphabetical", Prune: []string{"a"}}
	inputs := docs("a: 1\n", "b: 2\n")

	legacyKey := legacyV1MergeOutputCacheKey(opts, inputs, false)
	currentKey := mergeOutputCacheKey(opts, inputs, false)
	if legacyKey == currentKey {
		t.Fatal("current key must differ from the pre-\"---\\n\"-prefix (v1) key for identical opts/inputs, so a v1 cache entry can never be replayed as a hit")
	}
}

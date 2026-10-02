// Command vectors writes the golden vector files that graft's unit tests
// read. It runs the real yaml.v3, dyff, ytbx, bunt, neat, text,
// hashstructure, levenshtein, and ciede2000 code at spruce v1.35.16's
// pins, so graft's ports are checked against the code they replace.
//
// Usage, from tests/diff-parity/oracle:
//
//	go run ./cmd/vectors -root ../../..          # write every vector file
//	go run ./cmd/vectors -root ../../.. -check   # fail if any file is stale
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// outputs maps a path relative to the graft root to the bytes it holds.
type outputs map[string][]byte

func (o outputs) json(path string, v interface{}) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		panic(fmt.Errorf("%s: %w", path, err))
	}
	o[path] = append(b, '\n')
}

func main() {
	root := flag.String("root", "../../..", "graft repository root")
	check := flag.Bool("check", false, "compare instead of writing")
	only := flag.String("only", "", "engine or render; empty means both")
	flag.Parse()

	// The Makefile's TZ=Etc/GMT+4 is what reaches BurntSushi/toml, which
	// captures its zone before main runs; this line covers the rest.
	time.Local = time.FixedZone("UTC-4", -4*60*60)
	abs, err := filepath.Abs(*root)
	if err != nil {
		fail(err)
	}

	out := outputs{}
	if *only == "" || *only == "engine" {
		if err := engineVectors(abs, out); err != nil {
			fail(err)
		}
	}
	if *only == "" || *only == "render" {
		if err := renderVectors(abs, out); err != nil {
			fail(err)
		}
	}

	paths := make([]string, 0, len(out))
	for p := range out {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	stale := 0
	for _, p := range paths {
		full := filepath.Join(abs, p)
		have, err := os.ReadFile(full)
		current := err == nil && same(p, have, out[p])
		if *check {
			if !current {
				fmt.Fprintf(os.Stderr, "stale: %s\n", p)
				stale++
			}
			continue
		}
		if current {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			fail(err)
		}
		if err := os.WriteFile(full, out[p], 0o644); err != nil {
			fail(err)
		}
	}
	if stale > 0 {
		fmt.Fprintf(os.Stderr, "%d vector files are stale; run make oracle-vectors\n", stale)
		os.Exit(1)
	}
	fmt.Printf("%d vector files %s\n", len(paths), map[bool]string{true: "checked", false: "written"}[*check])
}

// approxEqual lists the vector files whose floats can differ in their
// last bits between CPU architectures, each with the comparison that
// decides whether the file on disk still matches. -check uses it, and a
// write leaves such a file alone when it already matches, so
// regenerating on another architecture never churns it.
var approxEqual = map[string]func(have, want []byte) bool{
	ciedePath: sameCiede,
}

func same(path string, have, want []byte) bool {
	if bytes.Equal(have, want) {
		return true
	}
	eq, ok := approxEqual[path]
	return ok && eq(have, want)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "vectors:", err)
	os.Exit(1)
}

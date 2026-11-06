package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// TestParseYAMLReadsLikeAMerge checks that the debugger reads a document
// the way a merge reads it, with YAML 1.1 booleans and octal numbers,
// only the first document, and the merge's alias limits.
func TestParseYAMLReadsLikeAMerge(t *testing.T) {
	got, err := parseYAML([]byte("enabled: yes\nmode: 010\nquoted: \"yes\"\nx: (( grab y ))\n---\nother: 1\n"))
	if err != nil {
		t.Fatalf("parseYAML: %v", err)
	}
	want := map[string]interface{}{"enabled": true, "mode": 8, "quoted": "yes", "x": "(( grab y ))"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseYAML = %#v, want %#v", got, want)
	}

	var b strings.Builder
	b.WriteString("a: &a [x, x, x, x, x, x, x, x, x, x]\n")
	prev := "a"
	for _, name := range []string{"b", "c", "d", "e", "f", "g", "h", "i"} {
		b.WriteString(name + ": &" + name + " [")
		for j := 0; j < 10; j++ {
			if j > 0 {
				b.WriteString(", ")
			}
			b.WriteString("*" + prev)
		}
		b.WriteString("]\n")
		prev = name
	}
	if got, err := parseYAML([]byte(b.String())); err == nil {
		t.Errorf("parseYAML expanded an alias-heavy document to %d keys", len(got))
	}
}

// TestDeferredRewriterKeepsBooleans checks that wrapping a session's
// deferred paths leaves a plain yes a boolean, so the merge that reads
// the rewritten document gives what a plain merge gives.
func TestDeferredRewriterKeepsBooleans(t *testing.T) {
	s := &debugSession{deferred: map[string]string{"x": ""}}
	out := s.deferredDocRewriter()([]byte("enabled: yes\nx: (( grab enabled ))\n"))
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	doc, err := engine.ParseYAML(out)
	if err != nil {
		t.Fatalf("ParseYAML(%q): %v", out, err)
	}
	v, err := doc.Get("enabled")
	if err != nil {
		t.Fatalf("Get(enabled): %v", err)
	}
	if v != true {
		t.Errorf("enabled is %#v after the rewrite of %q, want true", v, out)
	}
}

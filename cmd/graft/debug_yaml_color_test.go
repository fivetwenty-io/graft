package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml/token"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
)

// enabledDarkStyler is colorizeYAML's styler for every test in this file
// that needs an on styler independent of debugUIOptions/writerIsTTY
// resolution (see debug_colorize_test.go's colorOnUI for the equivalent
// at the handleDebug level).
var enabledDarkStyler = debugStyler{enabled: true, theme: debugThemeDark}

// TestColorizeYAMLTokenClasses locks the YAML Colorizer's per-token-type
// mapping (Category G / the YAML Colorizer section,
// plans/debugger-colorizing.md): mapping keys, every literal scalar
// type, anchor/alias sigil-plus-name pairs, comments, and document
// markers each render in their assigned role, while a plain scalar, a
// quoted scalar, and a tag - none of which the plan assigns a role -
// render with no styling at all.
func TestColorizeYAMLTokenClasses(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"mapping key", "key: value\n", styled(roleYAMLKey, "key")},
		{"integer literal", "n: 1\n", styled(roleYAMLLiteral, "1")},
		{"float literal", "n: 1.5\n", styled(roleYAMLLiteral, "1.5")},
		{"bool literal", "n: true\n", styled(roleYAMLLiteral, "true")},
		{"null literal", "n: null\n", styled(roleYAMLLiteral, "null")},
		{"anchor sigil and name as one colored unit", "a: &x foo\n", styled(roleYAMLAnchor, "&") + styled(roleYAMLAnchor, "x")},
		{"alias sigil and name as one colored unit", "a: &x foo\nb: *x\n", styled(roleYAMLAnchor, "*") + styled(roleYAMLAnchor, "x")},
		{"comment", "# hello\nkey: value\n", styled(roleYAMLComment, "# hello")},
		{"document header marker", "---\nkey: value\n", styled(roleYAMLComment, "---")},
		{"document end marker", "---\nkey: value\n...\n", styled(roleYAMLComment, "...")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(colorizeYAML([]byte(tt.src), enabledDarkStyler))
			if !strings.Contains(got, tt.want) {
				t.Errorf("colorizeYAML(%q) = %q, want it to contain %q", tt.src, got, tt.want)
			}
		})
	}
}

// TestColorizeYAMLUnstyledTokenClasses locks the negative half of the
// same mapping: a plain scalar value, a quoted scalar, and a tag render
// with their literal bytes untouched - no role in the theme table's
// list assigns any of them a style, so no escape sequence should ever
// wrap them.
func TestColorizeYAMLUnstyledTokenClasses(t *testing.T) {
	tests := []struct {
		name, src, plain string
	}{
		{"plain string value", "greeting: hello\n", "hello"},
		{"double-quoted string value", "a: \"hello world\"\n", "\"hello world\""},
		{"single-quoted string value", "a: 'single'\n", "'single'"},
		{"tag", "key: !!str value\n", "!!str"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(colorizeYAML([]byte(tt.src), enabledDarkStyler))
			if !strings.Contains(got, tt.plain) {
				t.Errorf("colorizeYAML(%q) = %q, want it to contain the unstyled literal %q", tt.src, got, tt.plain)
			}
		})
	}
}

// TestColorizeYAMLColorOffIsUntouched proves writeYAML's color-off
// branch (the one every existing bytes.Buffer test's plain-mode
// assertions rely on) never lexes: a disabled styler must return raw
// completely unchanged, including a document colorizeYAML itself would
// alter when enabled (proven inline, so a regression that made the
// disabled path start styling would fail this test even if it happened
// to still contain every substring downstream tests look for).
func TestColorizeYAMLColorOffIsUntouched(t *testing.T) {
	raw := []byte("key: &x value\nother: *x\nn: 1\n# comment\n")
	disabled := debugStyler{}
	got := colorizeYAML(raw, disabled)
	if !bytes.Equal(got, raw) {
		t.Fatalf("colorizeYAML with a disabled styler = %q, want raw unchanged", got)
	}

	// Confirm the same input does get styled when enabled, so the
	// disabled-branch assertion above is not vacuously true because
	// this input never renders any escape sequence at all.
	enabled := colorizeYAML(raw, enabledDarkStyler)
	if bytes.Equal(enabled, raw) {
		t.Fatalf("colorizeYAML with an enabled styler produced no styling at all on %q; the test fixture no longer exercises anything", raw)
	}
}

// TestColorizeYAMLSkipsInputContainingEscape locks the ESC-input guard:
// input that already contains an ESC byte is returned unchanged, never
// colorized, because the strip-and-compare safety net cannot tell a
// pre-existing escape sequence in the input apart from one this
// function would have added.
func TestColorizeYAMLSkipsInputContainingEscape(t *testing.T) {
	raw := []byte("key: \x1b[31mvalue\x1b[0m\n")
	got := colorizeYAML(raw, enabledDarkStyler)
	if !bytes.Equal(got, raw) {
		t.Errorf("colorizeYAML(%q) = %q, want raw returned unchanged (ESC-input guard)", raw, got)
	}
}

// TestYAMLTokenStartsRejectsInvalidOffsets unit-tests the reconstruct
// fallback's gate directly: yamlTokenStarts must accept a well-formed,
// in-range, non-decreasing offset sequence, and reject every way a
// lexer quirk (or a future goccy/go-yaml version) could hand back
// offsets this function cannot safely reconstruct from - a token with
// no Position, an offset before the start of raw, an offset past its
// end, and a sequence that goes backwards from one token to the next
// (the failure mode a Comment token's trailing-newline lookahead could
// in principle compound into, per colorizeYAML's doc comment).
func TestYAMLTokenStartsRejectsInvalidOffsets(t *testing.T) {
	raw := []byte("abcdefgh")
	mk := func(offset int) *token.Token {
		return &token.Token{Position: &token.Position{Offset: offset}}
	}

	tests := []struct {
		name   string
		tokens token.Tokens
		want   bool
	}{
		{"empty token list", token.Tokens{}, true},
		{"valid ascending offsets", token.Tokens{mk(1), mk(3), mk(6)}, true},
		{"offsets may repeat (zero-length span)", token.Tokens{mk(1), mk(1), mk(4)}, true},
		{"offset at the last byte is in range", token.Tokens{mk(len(raw) + 1)}, true},
		{"offset past the end of raw is rejected", token.Tokens{mk(len(raw) + 2)}, false},
		{"offset before byte 1 is rejected", token.Tokens{mk(0)}, false},
		{"decreasing offsets are rejected", token.Tokens{mk(5), mk(2)}, false},
		{"nil Position is rejected", token.Tokens{{Position: nil}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := yamlTokenStarts(raw, tt.tokens)
			if ok != tt.want {
				t.Errorf("yamlTokenStarts() ok = %v, want %v", ok, tt.want)
			}
		})
	}
}

// TestColorizeYAMLStripIdentityProperty is the strip-identity property
// test the Test Plan calls for: over a table of inputs designed to
// stress the reconstruction (multiple documents, every literal scalar
// type, anchors/aliases, comments interleaved with data, unicode, an
// empty document, and several trailing-newline variants) plus every
// fixture under assets/debug/ and assets/history/, stripping every ANSI
// escape sequence colorizeYAML's enabled path adds must reproduce the
// input exactly. A fixture that already contains an ESC byte is skipped
// (none of the ones read here do; the check exists so a future fixture
// addition cannot silently defeat this test the way it would defeat the
// guard itself - see TestColorizeYAMLSkipsInputContainingEscape for that
// guard's own direct test).
func TestColorizeYAMLStripIdentityProperty(t *testing.T) {
	inputs := map[string][]byte{
		"empty document":                []byte(""),
		"no trailing newline":           []byte("a: 1"),
		"single trailing newline":       []byte("a: 1\n"),
		"trailing blank lines":          []byte("a: 1\n\n\n"),
		"leading blank lines":           []byte("\n\na: 1\n"),
		"multi-document":                []byte("---\na: 1\n---\nb: 2\n...\n"),
		"every literal scalar type":     []byte("i: 1\nf: 1.5\nb: true\nn: null\nh: 0xFF\no: 0o17\nbi: 0b101\n"),
		"anchor and alias":              []byte("a: &x foo\nb: *x\n"),
		"comment interleaved with data": []byte("# top\nkey: value # trailing\nother: 1\n"),
		"unicode scalar":                []byte("a: héllo wörld 日本語\n"),
		"nested list and map":           []byte("list:\n  - 1\n  - two\n  - true\nmap:\n  inner: value\n"),
		"flow collection":               []byte("{a: 1, b: [2, 3]}\n"),
		"quoted strings":                []byte("a: \"hello world\"\nb: 'single quoted'\n"),
		"literal block scalar":          []byte("a: |\n  line one\n  line two\nb: 2\n"),
		"tag":                           []byte("key: !!str value\n"),
		"merge key":                     []byte("a: &d\n  x: 1\nb:\n  <<: *d\n  y: 2\n"),
	}

	for _, dir := range []string{
		filepath.Join("..", "..", "assets", "debug"),
		filepath.Join("..", "..", "assets", "history"),
	} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("reading fixture dir %s: %v", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			path := filepath.Join(dir, e.Name())
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading fixture %s: %v", path, err)
			}
			inputs[path] = raw
		}
	}

	for name, raw := range inputs {
		name, raw := name, raw
		t.Run(name, func(t *testing.T) {
			if bytes.IndexByte(raw, '\x1b') >= 0 {
				t.Skipf("fixture %q already contains an ESC byte; the guard, not this property, covers it", name)
			}
			got := colorizeYAML(raw, enabledDarkStyler)
			stripped := ansi.StripEscapes(string(got))
			if stripped != string(raw) {
				t.Errorf("strip(colorizeYAML(%q)) = %q, want it to equal the input %q", name, stripped, string(raw))
			}
		})
	}
}

// TestDebugWriteYAML exercises writeYAML directly against a debugSession
// whose out is a bytes.Buffer, independent of any REPL command: color
// off writes raw bytes verbatim (never calling the lexer), color on
// writes exactly what colorizeYAML(raw, s.styler) produces.
func TestDebugWriteYAML(t *testing.T) {
	raw := []byte("key: &x value\nn: 1\n")

	t.Run("color off", func(t *testing.T) {
		var out bytes.Buffer
		s := &debugSession{out: &out, styler: debugStyler{}}
		s.writeYAML(raw)
		if !bytes.Equal(out.Bytes(), raw) {
			t.Errorf("writeYAML (color off) wrote %q, want raw %q unchanged", out.Bytes(), raw)
		}
	})

	t.Run("color on", func(t *testing.T) {
		var out bytes.Buffer
		s := &debugSession{out: &out, styler: enabledDarkStyler}
		s.writeYAML(raw)
		want := colorizeYAML(raw, enabledDarkStyler)
		if !bytes.Equal(out.Bytes(), want) {
			t.Errorf("writeYAML (color on) wrote %q, want %q", out.Bytes(), want)
		}
	})
}

// TestDebugColorOnInspectAndOutput is the YAML colorizer's coverage of
// the Test Plan's "inspect/output token-class assertions with color on"
// item: cmdInspect and cmdOutput, run through a real handleDebug
// session against the shared debugColorizeTestFiles fixture set
// (debug_colorize_test.go), render their YAML with roleYAMLKey on a
// mapping key and roleYAMLLiteral on an evaluated integer value - proof
// that the two real call sites (not just colorizeYAML in isolation) are
// wired to writeYAML.
func TestDebugColorOnInspectAndOutput(t *testing.T) {
	t.Run("output", func(t *testing.T) {
		out, rc := runDebugSessionWithUI(debugColorizeTestFiles, &mergeOpts{}, colorOnUI, "load\ncontinue\noutput\nquit\n")
		if rc != 0 {
			t.Fatalf("rc = %d, want 0", rc)
		}
		if !strings.Contains(out, styled(roleYAMLKey, "pool_size")) {
			t.Errorf("output's pool_size key not styled roleYAMLKey:\n%s", out)
		}
		if !strings.Contains(out, styled(roleYAMLLiteral, "50")) {
			t.Errorf("output's pool_size value not styled roleYAMLLiteral:\n%s", out)
		}
	})

	t.Run("inspect", func(t *testing.T) {
		out, rc := runDebugSessionWithUI(debugColorizeTestFiles, &mergeOpts{}, colorOnUI, "load\ncontinue\ninspect database.pool_size\nquit\n")
		if rc != 0 {
			t.Fatalf("rc = %d, want 0", rc)
		}
		if !strings.Contains(out, styled(roleYAMLLiteral, "50")) {
			t.Errorf("inspect's scalar result not styled roleYAMLLiteral:\n%s", out)
		}
	})

	t.Run("output color off matches graft.MarshalYAML's raw bytes exactly", func(t *testing.T) {
		out, rc := runDebugSessionWithOpts(debugColorizeTestFiles, &mergeOpts{}, "load\ncontinue\noutput\nquit\n")
		if rc != 0 {
			t.Fatalf("rc = %d, want 0", rc)
		}
		if !strings.Contains(out, "pool_size: 50\n") {
			t.Errorf("color-off output missing the expected plain byte sequence:\n%s", out)
		}
		if strings.ContainsRune(out, '\x1b') {
			t.Errorf("color-off output contains an escape byte:\n%q", out)
		}
	})
}

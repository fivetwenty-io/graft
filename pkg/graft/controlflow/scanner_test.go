package controlflow

import (
	"strings"
	"testing"
)

func TestMatchMarker(t *testing.T) {
	cases := []struct {
		in      string
		wantRaw string
		wantOK  bool
	}{
		{`(( if x == "production" ))`, `if x == "production"`, true},
		{`(( fi ))`, `fi`, true},
		{`(( for svc in services ))`, `for svc in services`, true},
		{`(( grab foo ))`, `grab foo`, true}, // matches syntactically; caller filters by keyword
		{`not a marker`, ``, false},
		{`(( if x == "))" ))`, `if x == "))"`, true},                       // quoted ")) " inside string must not confuse the scanner
		{`(( if (grab a) == (grab b) ))`, `if (grab a) == (grab b)`, true}, // nested parens
		{`(( if x )) trailing`, ``, false},                                 // trailing content after "))" is not a marker
		{`prefix (( if x ))`, ``, false},                                   // marker must start the trimmed line
		{`(( unterminated`, ``, false},
	}
	for _, c := range cases {
		raw, ok := matchMarker(c.in)
		if ok != c.wantOK || (ok && raw != c.wantRaw) {
			t.Errorf("matchMarker(%q) = (%q, %v), want (%q, %v)", c.in, raw, ok, c.wantRaw, c.wantOK)
		}
	}
}

func TestClassifyLines_MarkerVsBody(t *testing.T) {
	src := "environment: production\n" +
		"(( if environment == \"production\" ))\n" +
		"replicas: 5\n" +
		"(( else ))\n" +
		"replicas: 1\n" +
		"(( fi ))\n"
	lines := classifyLines(src)
	if len(lines) != 6 {
		t.Fatalf("got %d lines, want 6 (source: %q)", len(lines), src)
	}
	wantMarker := []bool{false, true, false, true, false, true}
	wantKeyword := []string{"", "if", "", "else", "", "fi"}
	for i, l := range lines {
		if l.isMarker != wantMarker[i] {
			t.Errorf("line %d (%q): isMarker = %v, want %v", i, l.text, l.isMarker, wantMarker[i])
		}
		if l.keyword != wantKeyword[i] {
			t.Errorf("line %d (%q): keyword = %q, want %q", i, l.text, l.keyword, wantKeyword[i])
		}
	}
}

func TestClassifyLines_KeywordAliases(t *testing.T) {
	src := "(( if x ))\na: 1\n(( elsif y ))\nb: 2\n(( endif ))\n"
	lines := classifyLines(src)
	got := []string{}
	for _, l := range lines {
		if l.isMarker {
			got = append(got, l.keyword)
		}
	}
	want := []string{"if", "elif", "fi"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("aliases: got %v, want %v", got, want)
	}
}

func TestClassifyLines_BlockScalarExclusion(t *testing.T) {
	// A marker-shaped line inside a "|" block scalar must not be treated as
	// a marker (spec decision C-21).
	src := "script: |\n" +
		"  (( if x ))\n" +
		"  echo hi\n" +
		"  (( fi ))\n" +
		"real: value\n" +
		"(( if x ))\n" +
		"a: 1\n" +
		"(( fi ))\n"
	lines := classifyLines(src)
	for i, want := range map[int]bool{
		1: false, // inside block scalar
		3: false, // inside block scalar
		5: true,  // real marker after the block scalar ends
		7: true,  // real marker
	} {
		if lines[i].isMarker != want {
			t.Errorf("line %d (%q): isMarker = %v, want %v", i, lines[i].text, lines[i].isMarker, want)
		}
	}
}

func TestClassifyLines_BlockScalarEndsOnDedent(t *testing.T) {
	src := "a: |\n" +
		"  line one\n" +
		"  line two\n" +
		"(( if x ))\n" +
		"b: 1\n" +
		"(( fi ))\n"
	lines := classifyLines(src)
	if !lines[3].isMarker {
		t.Errorf("marker line after block scalar dedent should be recognized as a marker, got %+v", lines[3])
	}
}

func TestHasControlFlowMarkers(t *testing.T) {
	if hasControlFlowMarkers(classifyLines("a: 1\nb: (( grab a ))\n")) {
		t.Error("plain document with no control-flow keywords should report no markers")
	}
	if !hasControlFlowMarkers(classifyLines("(( if a ))\nb: 1\n(( fi ))\n")) {
		t.Error("document with an if/fi block should report markers")
	}
}

func TestParseDocument_SimpleIfElseFi(t *testing.T) {
	src := "(( if a ))\nx: 1\n(( else ))\nx: 2\n(( fi ))\n"
	items, err := parseDocument(classifyLines(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 || items[0].kind != itemIf {
		t.Fatalf("expected a single itemIf, got %+v", items)
	}
	if len(items[0].clauses) != 2 {
		t.Fatalf("expected 2 clauses (if, else), got %d", len(items[0].clauses))
	}
	if items[0].clauses[0].kind != "if" || items[0].clauses[0].condRaw != "a" {
		t.Errorf("clause 0 = %+v", items[0].clauses[0])
	}
	if items[0].clauses[1].kind != "else" {
		t.Errorf("clause 1 = %+v", items[0].clauses[1])
	}
}

func TestParseDocument_ForHeaderParsing(t *testing.T) {
	src := "(( for idx, zone in zones ))\na: 1\n(( done ))\n"
	items, err := parseDocument(classifyLines(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	f := items[0]
	if f.kind != itemFor {
		t.Fatalf("expected itemFor, got %+v", f)
	}
	if len(f.loopVars) != 2 || f.loopVars[0] != "idx" || f.loopVars[1] != "zone" {
		t.Errorf("loopVars = %v, want [idx zone]", f.loopVars)
	}
	if f.iterableRaw != "zones" {
		t.Errorf("iterableRaw = %q, want %q", f.iterableRaw, "zones")
	}
}

func TestParseDocument_CaseWhenDefaultEsac(t *testing.T) {
	src := `(( case cloud ))
(( when "aws" | "gcp" ))
a: 1
(( default ))
a: 2
(( esac ))
`
	items, err := parseDocument(classifyLines(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := items[0]
	if c.kind != itemCase || c.subjectRaw != "cloud" {
		t.Fatalf("unexpected case item: %+v", c)
	}
	if len(c.whens) != 1 || len(c.whens[0].patterns) != 2 {
		t.Fatalf("whens = %+v", c.whens)
	}
	if c.whens[0].patterns[0] != `"aws"` || c.whens[0].patterns[1] != `"gcp"` {
		t.Errorf("patterns = %v", c.whens[0].patterns)
	}
	if !c.hasDefault {
		t.Error("expected hasDefault = true")
	}
}

func TestParseDocument_Nesting(t *testing.T) {
	src := "(( if a ))\n(( for x in y ))\nz: 1\n(( done ))\n(( fi ))\n"
	items, err := parseDocument(classifyLines(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	outer := items[0]
	if outer.kind != itemIf {
		t.Fatalf("expected outer itemIf, got %+v", outer)
	}
	body := outer.clauses[0].body
	if len(body) != 1 || body[0].kind != itemFor {
		t.Fatalf("expected nested itemFor in if-body, got %+v", body)
	}
}

func TestParseDocument_Errors(t *testing.T) {
	cases := map[string]string{
		"unclosed if":       "(( if a ))\nx: 1\n",
		"orphan fi":         "(( fi ))\n",
		"orphan elif":       "(( elif a ))\n",
		"duplicate else":    "(( if a ))\n(( else ))\n(( else ))\n(( fi ))\n",
		"elif after else":   "(( if a ))\n(( else ))\n(( elif b ))\n(( fi ))\n",
		"mismatched for/fi": "(( for x in y ))\nz: 1\n(( fi ))\n",
		"default not last":  "(( case a ))\n(( default ))\nb: 1\n(( when \"x\" ))\nb: 2\n(( esac ))\n",
		"duplicate default": "(( case a ))\n(( default ))\nb: 1\n(( default ))\nb: 2\n(( esac ))\n",
		"bad for header":    "(( for in y ))\nz: 1\n(( done ))\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parseDocument(classifyLines(src))
			if err == nil {
				t.Errorf("%s: expected an error, got nil", name)
			}
		})
	}
}

func TestParseDocument_DepthLimit(t *testing.T) {
	var b strings.Builder
	depth := maxBlockNestingDepth + 1
	for i := 0; i < depth; i++ {
		b.WriteString("(( if a ))\n")
	}
	b.WriteString("x: 1\n")
	for i := 0; i < depth; i++ {
		b.WriteString("(( fi ))\n")
	}
	_, err := parseDocument(classifyLines(b.String()))
	if err == nil {
		t.Fatal("expected a nesting-too-deep error")
	}
	if !strings.Contains(err.Error(), "too deep") {
		t.Errorf("error = %v, want mention of nesting depth", err)
	}
}

// TestClassifyLines_TrailingComment pins that a trailing YAML comment does
// not stop a marker line from being recognized, while a "#" that is not a
// comment (no separating whitespace) still leaves the line as body text.
func TestClassifyLines_TrailingComment(t *testing.T) {
	cases := []struct {
		name     string
		line     string
		isMarker bool
		keyword  string
	}{
		{"trailing comment", `(( if a )) # why`, true, "if"},
		{"trailing comment no text", `(( fi )) #`, true, "fi"},
		{"tab before comment", "(( done ))\t# why", true, "done"},
		{"no separating space", `(( fi ))#why`, false, ""},
		{"trailing content", `(( fi )) trailing`, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyLines(tc.line)
			if got[0].isMarker != tc.isMarker {
				t.Fatalf("isMarker = %v, want %v", got[0].isMarker, tc.isMarker)
			}
			if got[0].keyword != tc.keyword {
				t.Errorf("keyword = %q, want %q", got[0].keyword, tc.keyword)
			}
		})
	}
}

// TestParseDocument_ClauseOrderMessages pins the specific diagnostics for
// out-of-order if/case clauses. Without them these inputs still fail, but
// only via the generic "no matching block start" path, which points the
// author at the wrong line and the wrong problem.
func TestParseDocument_ClauseOrderMessages(t *testing.T) {
	cases := map[string]struct {
		src  string
		want string
	}{
		"elif after else": {
			"(( if a ))\n(( else ))\n(( elif b ))\n(( fi ))\n",
			"(( elif )) at line 3 follows (( else ))",
		},
		"duplicate else": {
			"(( if a ))\n(( else ))\n(( else ))\n(( fi ))\n",
			"duplicate (( else )) at line 3",
		},
		"when after default": {
			"(( case a ))\n(( default ))\nb: 1\n(( when \"x\" ))\nb: 2\n(( esac ))\n",
			"(( when )) at line 4 follows (( default ))",
		},
		"duplicate default": {
			"(( case a ))\n(( default ))\nb: 1\n(( default ))\nb: 2\n(( esac ))\n",
			"duplicate (( default )) at line 4",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parseDocument(classifyLines(tc.src))
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tc.want)
			}
		})
	}
}

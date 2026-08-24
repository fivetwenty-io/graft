package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTempYAML writes content to a uniquely named file under t.TempDir()
// and returns its path.
func writeTempYAML(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
	return path
}

// yamlFilesFor opens each path as a YamlFile, closing them via t.Cleanup.
func yamlFilesFor(t *testing.T, paths ...string) []YamlFile {
	t.Helper()
	files := make([]YamlFile, 0, len(paths))
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			t.Fatalf("Open(%s) error = %v", p, err)
		}
		t.Cleanup(func() { _ = f.Close() })
		files = append(files, YamlFile{Path: p, Reader: io.NopCloser(f)})
	}
	return files
}

func TestBuildEngineAndDocsCollectsSourceRefs(t *testing.T) {
	withOps := writeTempYAML(t, "ops.yml", "meta:\n  a: (( grab meta.b ))\n  b: 1\n")
	plain := writeTempYAML(t, "plain.yml", "other:\n  x: 1\n")

	_, _, refs, err := buildEngineAndDocs(yamlFilesFor(t, withOps, plain), &mergeOpts{})
	if err != nil {
		t.Fatalf("buildEngineAndDocs() error = %v", err)
	}

	if len(refs) != 2 {
		t.Fatalf("got %d refs, want 2 (one per input, in merge order)", len(refs))
	}
	if refs[0].Name != withOps {
		t.Errorf("refs[0].Name = %q, want %q", refs[0].Name, withOps)
	}
	if !strings.Contains(string(refs[0].Bytes), "(( grab meta.b ))") {
		t.Errorf("refs[0].Bytes did not retain the operator-bearing file's contents")
	}
	if refs[1].Bytes != nil {
		t.Errorf("refs[1].Bytes = %q, want nil: a file with no operator text is not retained", refs[1].Bytes)
	}
	if refs[1].Opaque {
		t.Errorf("refs[1].Opaque = true, want false: a plain YAML file is fully indexable")
	}
}

func TestBuildEngineAndDocsMarksControlFlowOpaque(t *testing.T) {
	cf := writeTempYAML(t, "cf.yml", "meta:\n  enabled: true\n(( if meta.enabled ))\ngated:\n  a: 1\n(( fi ))\n")

	_, _, refs, err := buildEngineAndDocs(yamlFilesFor(t, cf), &mergeOpts{})
	if err != nil {
		t.Fatalf("buildEngineAndDocs() error = %v", err)
	}

	if len(refs) != 1 {
		t.Fatalf("got %d refs, want 1", len(refs))
	}
	if !refs[0].Opaque {
		t.Errorf("refs[0].Opaque = false, want true: the expander rewrites a control-flow document wholesale")
	}
	if refs[0].Bytes != nil {
		t.Errorf("refs[0].Bytes is set; an opaque input must retain nothing")
	}
}

func TestCycleOutputAcrossTwoFiles(t *testing.T) {
	a := writeTempYAML(t, "a.yml", "meta:\n  foo: (( grab meta.bar ))\n")
	b := writeTempYAML(t, "b.yml", "meta:\n  bar: (( grab meta.foo ))\n")

	stderr, rc := runGraftCapturingOutput(t, []string{"merge", a, b})

	if rc == 0 {
		t.Fatalf("exit code = 0; want a failure")
	}
	for _, want := range []string{
		" - cycle detected in operator data-flow graph",
		"   inputs:",
		"     [1] " + a,
		"     [2] " + b,
		"   cycle (2 nodes): meta.bar -> meta.foo -> meta.bar",
		"     " + b + ":2  meta.bar: (( grab meta.foo ))",
		"     " + a + ":2  meta.foo: (( grab meta.bar ))",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr missing %q:\n%s", want, stderr)
		}
	}
}

func TestCycleOutputLastTwoLinesNameTheClosingEdge(t *testing.T) {
	a := writeTempYAML(t, "a.yml", "meta:\n  a: (( grab meta.b ))\n")
	b := writeTempYAML(t, "b.yml", "meta:\n  b: (( grab meta.c ))\n")
	c := writeTempYAML(t, "c.yml", "meta:\n  c: (( grab meta.a ))\n")

	stderr, _ := runGraftCapturingOutput(t, []string{"merge", a, b, c})

	lines := strings.Split(strings.TrimRight(stderr, "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("stderr has %d lines:\n%s", len(lines), stderr)
	}
	last := lines[len(lines)-2:]

	// The requirement in one assertion: the final two lines name the two
	// files and lines whose edge closes the loop.
	if !strings.Contains(last[0], c+":2") || !strings.Contains(last[0], "(( grab meta.a ))") {
		t.Errorf("second-to-last line = %q, want c.yml's closing operator", last[0])
	}
	if !strings.Contains(last[1], a+":2") || !strings.Contains(last[1], "(( grab meta.b ))") {
		t.Errorf("last line = %q, want a.yml's wrap line", last[1])
	}
}

func TestCycleOutputSelfCycle(t *testing.T) {
	a := writeTempYAML(t, "a.yml", "meta:\n  a: (( grab meta.a ))\n")

	stderr, rc := runGraftCapturingOutput(t, []string{"merge", a})

	if rc == 0 {
		t.Fatalf("exit code = 0; want a failure")
	}
	if !strings.Contains(stderr, "cycle (1 node): meta.a -> meta.a") {
		t.Errorf("stderr missing the self-cycle chain line:\n%s", stderr)
	}
	// One detail line, not two: a self-cycle needs no wrap duplicate.
	if n := strings.Count(stderr, "meta.a: (( grab meta.a ))"); n != 1 {
		t.Errorf("got %d detail lines for the self-cycle, want 1:\n%s", n, stderr)
	}
}

func TestCycleOutputSameFile(t *testing.T) {
	a := writeTempYAML(t, "a.yml", "meta:\n  foo: (( grab meta.bar ))\n  bar: (( grab meta.foo ))\n")

	stderr, _ := runGraftCapturingOutput(t, []string{"merge", a})

	if !strings.Contains(stderr, a+":3  meta.bar: (( grab meta.foo ))") {
		t.Errorf("stderr missing meta.bar at line 3:\n%s", stderr)
	}
	if !strings.Contains(stderr, a+":2  meta.foo: (( grab meta.bar ))") {
		t.Errorf("stderr missing meta.foo at line 2:\n%s", stderr)
	}
}

func TestCycleOutputHasNoEscapeBytesOnANonTTY(t *testing.T) {
	a := writeTempYAML(t, "a.yml", "meta:\n  a: (( grab meta.b ))\n  b: (( grab meta.a ))\n")

	stderr, _ := runGraftCapturingOutput(t, []string{"merge", a})

	if strings.Contains(stderr, "\033") {
		t.Errorf("stderr contains an escape byte:\n%q", stderr)
	}
}

func TestCycleOutputSurvivesHostilePayloads(t *testing.T) {
	// All three proved payloads at once: a color directive, a raw escape
	// sequence, and a literal newline carrying a forged genesis error
	// line - the last inside a mapping key as well as an expression.
	//
	// The b: value is wrapped in YAML single quotes because its unquoted
	// form is not valid YAML: a bare (plain) scalar cannot contain ": "
	// (the "$.evil: boom" fragment reads as a nested mapping key to any
	// spec-compliant YAML parser, graft's included - confirmed against
	// both PyYAML and graft's own parser). Quoting the whole expression
	// at the YAML level changes nothing about the payload graft's
	// operator parser sees.
	a := writeTempYAML(t, "a.yml",
		"meta:\n"+
			"  a: (( concat \"@r{x}\" meta.b ))\n"+
			"  b: '(( concat \"y\\n - $.evil: boom\" meta.a ))'\n")

	stderr, rc := runGraftCapturingOutput(t, []string{"merge", a})

	if rc == 0 {
		t.Fatalf("exit code = 0; want a failure")
	}
	if strings.Contains(stderr, "\033") {
		t.Errorf("stderr contains an escape byte:\n%q", stderr)
	}
	if !strings.Contains(stderr, "@r{x}") {
		t.Errorf("the @r{...} sequence was reprocessed instead of printed verbatim:\n%s", stderr)
	}
	if got := len(adaptiveMergeErrorLines(stderr)); got != 1 {
		t.Errorf("got %d lines starting with %q, want exactly 1:\n%s", got, " - ", stderr)
	}
}

func TestCycleOutputEscapesARealNewlineFromTheCLI(t *testing.T) {
	// The sibling hostile-payload test cannot reach this branch. A cycle
	// short-circuits before concat ever evaluates its arguments, and
	// CycleNode.Expr is the operator's raw source text, so a DSL-level
	// \n escape is never processed no matter how the fixture is written.
	// A YAML literal block scalar is what puts a real 0x0A inside the
	// operator source, which is the only way sanitizeDisplay's newline
	// branch runs on the CLI path.
	src := "meta:\n" +
		"  a: (( grab meta.b ))\n" +
		"  b: |-\n" +
		"    (( concat \"y\n" +
		"     - $.evil: boom\" meta.a ))\n"
	if !strings.Contains(src, "\n     - $.evil: boom") {
		t.Fatalf("fixture does not carry a real newline before the forged line")
	}
	a := writeTempYAML(t, "a.yml", src)

	stderr, rc := runGraftCapturingOutput(t, []string{"merge", a})

	if rc == 0 {
		t.Fatalf("exit code = 0; want a failure")
	}
	// The real newline must render as the two characters backslash-n, so
	// the forged error line never becomes a line of its own.
	if !strings.Contains(stderr, `\n - $.evil: boom`) {
		t.Errorf("the real newline was not escaped to a literal \\n:\n%q", stderr)
	}
	if got := len(adaptiveMergeErrorLines(stderr)); got != 1 {
		t.Errorf("got %d lines starting with %q, want exactly 1:\n%s", got, " - ", stderr)
	}
	if strings.Contains(stderr, "\033") {
		t.Errorf("stderr contains an escape byte:\n%q", stderr)
	}
}

func TestCycleOutputFromStdin(t *testing.T) {
	// A cycle read from STDIN has exactly one input, so the file can be
	// named. Its lines resolve normally because the bytes are retained.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	go func() {
		_, _ = w.WriteString("meta:\n  a: (( grab meta.b ))\n  b: (( grab meta.a ))\n")
		_ = w.Close()
	}()

	prevStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = prevStdin; _ = r.Close() })

	stderr, rc := runGraftCapturingOutput(t, []string{"merge", "-"})

	if rc == 0 {
		t.Fatalf("exit code = 0; want a failure")
	}
	if !strings.Contains(stderr, "[1] STDIN") {
		t.Errorf("stderr does not name STDIN as the input:\n%s", stderr)
	}
}

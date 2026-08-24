package main

import (
	"io"
	"io/fs"
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

func TestCycleOutputMultiDocumentInput(t *testing.T) {
	// graft parses only the first document of a multi-document input
	// (pkg/graft/yaml_compat.go), so the cycle always lives above the
	// first "---" and no line below it may ever be cited.
	//
	// The assertions match on ":line  path: expr" rather than on the
	// full input path, because t.TempDir() under a long TMPDIR produces
	// a path that the display sanitizer shortens.
	for name, second := range map[string]string{
		"different expressions": "---\nmeta:\n  a: (( grab meta.zzz ))\n  b: (( grab meta.yyy ))\n",
		"repeated expressions":  "---\nmeta:\n  a: (( grab meta.b ))\n  b: (( grab meta.a ))\n",
	} {
		t.Run(name, func(t *testing.T) {
			a := writeTempYAML(t, "md.yml",
				"meta:\n  a: (( grab meta.b ))\n  b: (( grab meta.a ))\n"+second)

			stderr, rc := runGraftCapturingOutput(t, []string{"merge", a})

			if rc == 0 {
				t.Fatalf("exit code = 0; want a failure")
			}
			if !strings.Contains(stderr, ":2  meta.a: (( grab meta.b ))") {
				t.Errorf("stderr missing meta.a at line 2:\n%s", stderr)
			}
			if !strings.Contains(stderr, ":3  meta.b: (( grab meta.a ))") {
				t.Errorf("stderr missing meta.b at line 3:\n%s", stderr)
			}
			for _, unwanted := range []string{":6  meta.a", ":7  meta.b"} {
				if strings.Contains(stderr, unwanted) {
					t.Errorf("stderr cites %q, a line the merge never parsed:\n%s", unwanted, stderr)
				}
			}
		})
	}
}

func TestCycleOutputIdenticalWithTheL2CacheEnabled(t *testing.T) {
	// The persistent parse cache carries a document's tree across runs.
	// A cache hit must not change one byte of the cycle block, including
	// the file and line each node is attributed to, so a cached run is
	// compared against a disabled run and against its own cold run.
	a := writeTempYAML(t, "a.yml", "meta:\n  foo: (( grab meta.bar ))\n")
	b := writeTempYAML(t, "b.yml", "meta:\n  bar: (( grab meta.foo ))\n")
	args := []string{"merge", a, b}

	t.Setenv("GRAFT_CACHE_L2_ENABLED", "false")
	disabled, rc := runGraftCapturingOutput(t, args)
	if rc == 0 {
		t.Fatalf("exit code = 0 with the cache disabled; want a failure")
	}
	if !strings.Contains(disabled, "cycle detected in operator data-flow graph") {
		t.Fatalf("the cache-disabled run did not report a cycle:\n%s", disabled)
	}

	cacheDir := t.TempDir()
	t.Setenv("GRAFT_CACHE_L2_ENABLED", "true")
	t.Setenv("GRAFT_CACHE_L2_PATH", cacheDir)

	cold, rcCold := runGraftCapturingOutput(t, args)
	// Without this the comparison below would be vacuous: an unengaged
	// cache trivially produces identical output.
	if n := countFilesUnder(t, cacheDir); n == 0 {
		t.Fatalf("the cache directory %s is empty after the cold run; the L2 cache was never engaged", cacheDir)
	}
	warm, rcWarm := runGraftCapturingOutput(t, args)

	if rcCold != rc || rcWarm != rc {
		t.Errorf("exit codes = %d (cold) and %d (warm), want %d", rcCold, rcWarm, rc)
	}
	if cold != disabled {
		t.Errorf("the cold cached run differs from the cache-disabled run:\ncold:\n%s\ndisabled:\n%s", cold, disabled)
	}
	if warm != disabled {
		t.Errorf("the warm cached run differs from the cache-disabled run:\nwarm:\n%s\ndisabled:\n%s", warm, disabled)
	}
}

// countFilesUnder returns how many regular files exist beneath dir.
func countFilesUnder(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir(%s) error = %v", dir, err)
	}
	return n
}

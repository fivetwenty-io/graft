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

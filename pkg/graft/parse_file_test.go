package graft

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTempFile is defined in json_test.go and reused here.

func TestParseFile_PlainYAML(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "plain.yml", "name: myapp\nport: 8080\n")

	engine := NewDefaultEngine()
	doc, err := engine.ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	if doc == nil {
		t.Fatal("ParseFile() returned nil Document for non-empty input")
	}
	name, err := doc.GetString("name")
	if err != nil || name != "myapp" {
		t.Errorf("GetString(name) = %q, %v; want myapp, nil", name, err)
	}
	port, err := doc.GetInt("port")
	if err != nil || port != 8080 {
		t.Errorf("GetInt(port) = %d, %v; want 8080, nil", port, err)
	}
}

func TestParseFile_MultiDocYAML(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "multi.yml", "name: doc-one\n---\nname: doc-two\n")

	engine := NewDefaultEngine()

	// ParseFile only ever sees/returns the first document: multi-document
	// splitting is ParseMultiDocFile's job, not ParseFile's. The "\n---\n"
	// separator itself is not valid as a second YAML root at the ParseFile
	// level, so this exercises that ParseFile does not silently drop or
	// merge the second document.
	doc, err := engine.ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	name, _ := doc.GetString("name")
	if name != "doc-one" {
		t.Errorf("GetString(name) = %q; want doc-one (first document only)", name)
	}
}

func TestParseFile_JSONExtension(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "config.json", `{"name":"myapp","port":8080}`)

	engine := NewDefaultEngine()
	doc, err := engine.ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	name, err := doc.GetString("name")
	if err != nil || name != "myapp" {
		t.Errorf("GetString(name) = %q, %v; want myapp, nil", name, err)
	}
}

func TestParseFile_JSONContentWithYMLExtension(t *testing.T) {
	dir := t.TempDir()
	// JSON is valid YAML, so a .yml file holding JSON content still
	// parses correctly through the YAML path (no ".json" extension to
	// trigger ParseJSON dispatch).
	path := writeTempFile(t, dir, "config.yml", `{"name":"myapp","port":8080}`)

	engine := NewDefaultEngine()
	doc, err := engine.ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	name, err := doc.GetString("name")
	if err != nil || name != "myapp" {
		t.Errorf("GetString(name) = %q, %v; want myapp, nil", name, err)
	}
}

func TestParseFile_InvalidJSONExtension(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "bad.json", `{"name": "myapp",}`) // trailing comma: invalid JSON

	engine := NewDefaultEngine()
	doc, err := engine.ParseFile(path)
	if err == nil {
		t.Fatal("ParseFile() error = nil; want a JSON parse error")
	}
	if doc != nil {
		t.Errorf("ParseFile() doc = %v; want nil on error", doc)
	}
}

func TestParseFile_GoPatchRootArray(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "patch.yml", "- type: replace\n  path: /key\n  value: 10\n")

	engine := NewDefaultEngine()
	doc, err := engine.ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	if !IsGoPatchDocument(doc) {
		t.Fatalf("ParseFile() returned a non-go-patch document for an array-rooted file")
	}
	ops, ok := GetGoPatchOps(doc)
	if !ok || len(ops) != 1 {
		t.Errorf("GetGoPatchOps() = %v, %v; want exactly 1 op", ops, ok)
	}
}

func TestParseFile_GoPatchInvalidArrayRoot(t *testing.T) {
	dir := t.TempDir()
	// A valid array root, but not a valid go-patch operation shape
	// (missing "type"/"path").
	path := writeTempFile(t, dir, "notpatch.yml", "- 1\n- 2\n- 3\n")

	engine := NewDefaultEngine()
	doc, err := engine.ParseFile(path)
	if err == nil {
		t.Fatal("ParseFile() error = nil; want a go-patch parse error for a non-op array")
	}
	if doc != nil {
		t.Errorf("ParseFile() doc = %v; want nil on error", doc)
	}
	if !strings.Contains(err.Error(), "go-patch") {
		t.Errorf("ParseFile() error = %q; want it to mention go-patch", err.Error())
	}
}

func TestParseFile_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "empty.yml", "")

	engine := NewDefaultEngine()
	doc, err := engine.ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile() error = %v; want nil for empty input", err)
	}
	if doc != nil {
		t.Errorf("ParseFile() doc = %v; want nil for empty input", doc)
	}
}

func TestParseFile_EmptyJSONFile(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "empty.json", "")

	engine := NewDefaultEngine()
	doc, err := engine.ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile() error = %v; want nil for empty input", err)
	}
	if doc != nil {
		t.Errorf("ParseFile() doc = %v; want nil for empty input", doc)
	}
}

func TestParseFile_MissingFile(t *testing.T) {
	engine := NewDefaultEngine()
	doc, err := engine.ParseFile(filepath.Join(t.TempDir(), "does-not-exist.yml"))
	if err == nil {
		t.Fatal("ParseFile() error = nil; want a not-exist error")
	}
	if !os.IsNotExist(err) {
		t.Errorf("os.IsNotExist(ParseFile err) = false; want true (err = %v)", err)
	}
	if doc != nil {
		t.Errorf("ParseFile() doc = %v; want nil on error", doc)
	}
}

func TestParseFile_DirectoryPath(t *testing.T) {
	engine := NewDefaultEngine()
	doc, err := engine.ParseFile(t.TempDir())
	if err == nil {
		t.Fatal("ParseFile() error = nil; want an error for a directory path")
	}
	if doc != nil {
		t.Errorf("ParseFile() doc = %v; want nil on error", doc)
	}
}

func TestParseFile_Stdin(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	origStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = origStdin })

	go func() {
		_, _ = w.WriteString("name: from-stdin\n")
		_ = w.Close()
	}()

	engine := NewDefaultEngine()
	doc, err := engine.ParseFile("-")
	if err != nil {
		t.Fatalf("ParseFile(\"-\") error = %v", err)
	}
	name, err := doc.GetString("name")
	if err != nil || name != "from-stdin" {
		t.Errorf("GetString(name) = %q, %v; want from-stdin, nil", name, err)
	}
}

func TestParseReader_Basic(t *testing.T) {
	engine := NewDefaultEngine()
	doc, err := engine.ParseReader(strings.NewReader("name: reader-value\n"))
	if err != nil {
		t.Fatalf("ParseReader() error = %v", err)
	}
	name, err := doc.GetString("name")
	if err != nil || name != "reader-value" {
		t.Errorf("GetString(name) = %q, %v; want reader-value, nil", name, err)
	}
}

func TestParseReader_Empty(t *testing.T) {
	engine := NewDefaultEngine()
	doc, err := engine.ParseReader(strings.NewReader(""))
	if err != nil {
		t.Fatalf("ParseReader() error = %v; want nil for empty input", err)
	}
	if doc != nil {
		t.Errorf("ParseReader() doc = %v; want nil for empty input", doc)
	}
}

// failingReader always returns an error from Read, exercising
// ParseReader's/MergeReaders' io.ReadAll failure path.
type failingReader struct{}

func (failingReader) Read(_ []byte) (int, error) {
	return 0, errors.New("simulated read failure")
}

func TestParseReader_ReadError(t *testing.T) {
	engine := NewDefaultEngine()
	doc, err := engine.ParseReader(failingReader{})
	if err == nil {
		t.Fatal("ParseReader() error = nil; want the reader's error")
	}
	if doc != nil {
		t.Errorf("ParseReader() doc = %v; want nil on error", doc)
	}
}

func TestParseMultiDocFile(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "multi.yml", "name: doc-one\n---\nname: doc-two\n")

	engine := NewDefaultEngine()
	docs, err := engine.ParseMultiDocFile(path)
	if err != nil {
		t.Fatalf("ParseMultiDocFile() error = %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("ParseMultiDocFile() returned %d docs; want 2", len(docs))
	}
	first, _ := docs[0].GetString("name")
	second, _ := docs[1].GetString("name")
	if first != "doc-one" || second != "doc-two" {
		t.Errorf("docs = [%q, %q]; want [doc-one, doc-two]", first, second)
	}
}

func TestParseMultiDocFile_MissingFile(t *testing.T) {
	engine := NewDefaultEngine()
	docs, err := engine.ParseMultiDocFile(filepath.Join(t.TempDir(), "missing.yml"))
	if err == nil {
		t.Fatal("ParseMultiDocFile() error = nil; want an error for a missing file")
	}
	if docs != nil {
		t.Errorf("ParseMultiDocFile() docs = %v; want nil on error", docs)
	}
}

func TestMergeFiles_HappyPath(t *testing.T) {
	dir := t.TempDir()
	base := writeTempFile(t, dir, "base.yml", "name: base\nport: 80\n")
	overlay := writeTempFile(t, dir, "overlay.yml", "port: 443\n")

	engine := NewDefaultEngine()
	result, err := engine.MergeFiles(context.Background(), base, overlay).Execute()
	if err != nil {
		t.Fatalf("MergeFiles().Execute() error = %v", err)
	}
	name, _ := result.GetString("name")
	port, _ := result.GetInt("port")
	if name != "base" || port != 443 {
		t.Errorf("merged name/port = %q/%d; want base/443", name, port)
	}
}

// TestMergeFiles_MissingFileDoesNotPanic pins P0-3: MergeFiles used to
// return an untyped nil MergeBuilder on a load failure, which panicked on
// .Execute() instead of surfacing an error.
func TestMergeFiles_MissingFileDoesNotPanic(t *testing.T) {
	engine := NewDefaultEngine()
	builder := engine.MergeFiles(context.Background(), filepath.Join(t.TempDir(), "missing.yml"))
	if builder == nil {
		t.Fatal("MergeFiles() returned a nil MergeBuilder; must return a typed builder carrying the error")
	}

	result, err := builder.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil; want an error for a missing merge input file")
	}
	if result != nil {
		t.Errorf("Execute() result = %v; want nil on error", result)
	}
}

func TestMergeFiles_NilContext(t *testing.T) {
	dir := t.TempDir()
	base := writeTempFile(t, dir, "base.yml", "name: base\n")

	engine := NewDefaultEngine()
	//nolint:staticcheck // intentionally exercising the documented nil-context fallback
	result, err := engine.MergeFiles(nil, base).Execute()
	if err != nil {
		t.Fatalf("MergeFiles(nil, ...).Execute() error = %v", err)
	}
	if result == nil {
		t.Fatal("MergeFiles(nil, ...).Execute() result = nil")
	}
}

func TestMergeReaders_HappyPath(t *testing.T) {
	engine := NewDefaultEngine()
	result, err := engine.MergeReaders(context.Background(),
		strings.NewReader("name: base\nport: 80\n"),
		strings.NewReader("port: 443\n"),
	).Execute()
	if err != nil {
		t.Fatalf("MergeReaders().Execute() error = %v", err)
	}
	name, _ := result.GetString("name")
	port, _ := result.GetInt("port")
	if name != "base" || port != 443 {
		t.Errorf("merged name/port = %q/%d; want base/443", name, port)
	}
}

// TestMergeReaders_FailingReaderDoesNotPanic is MergeReaders' half of the
// P0-3 fix.
func TestMergeReaders_FailingReaderDoesNotPanic(t *testing.T) {
	engine := NewDefaultEngine()
	builder := engine.MergeReaders(context.Background(), failingReader{})
	if builder == nil {
		t.Fatal("MergeReaders() returned a nil MergeBuilder; must return a typed builder carrying the error")
	}

	result, err := builder.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil; want an error for a failing merge input reader")
	}
	if result != nil {
		t.Errorf("Execute() result = %v; want nil on error", result)
	}
}

func TestDetectArrayRoot(t *testing.T) {
	tests := []struct {
		name      string
		data      string
		wantArray bool
		wantErr   bool
	}{
		{name: "map root", data: "key: value\n", wantArray: false, wantErr: false},
		{name: "array root", data: "- 1\n- 2\n", wantArray: true, wantErr: true},
		{name: "empty", data: "", wantArray: false, wantErr: false},
		{name: "blank document", data: "---\n", wantArray: false, wantErr: false},
		// The byte-probe fast path answers nil for anything it can prove
		// is not array-rooted, even when a full parse would error; the
		// caller's real parse owns reporting those errors.
		{name: "scalar root", data: "1234\n", wantArray: false, wantErr: false},
		{name: "invalid yaml", data: "key: [unterminated\n", wantArray: false, wantErr: false},
		{name: "ambiguous scalar root", data: "-1234\n", wantArray: false, wantErr: true},
		{name: "ambiguous invalid yaml", data: "- [unterminated\n", wantArray: false, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := DetectArrayRoot([]byte(tt.data))
			if (err != nil) != tt.wantErr {
				t.Fatalf("DetectArrayRoot(%q) error = %v; wantErr = %v", tt.data, err, tt.wantErr)
			}
			if got := IsArrayError(err); got != tt.wantArray {
				t.Errorf("IsArrayError(DetectArrayRoot(%q)) = %v; want %v", tt.data, got, tt.wantArray)
			}
		})
	}
}

func TestParseGoPatch(t *testing.T) {
	ops, err := ParseGoPatch([]byte("- type: replace\n  path: /key\n  value: 10\n"))
	if err != nil {
		t.Fatalf("ParseGoPatch() error = %v", err)
	}
	if len(ops) != 1 {
		t.Fatalf("ParseGoPatch() returned %d ops; want 1", len(ops))
	}
}

func TestParseGoPatch_InvalidDefinitions(t *testing.T) {
	_, err := ParseGoPatch([]byte("- 1\n- 2\n"))
	if err == nil {
		t.Fatal("ParseGoPatch() error = nil; want an error for a non-op array")
	}
}

func TestIsArrayError_UnrelatedError(t *testing.T) {
	if IsArrayError(errors.New("some other error")) {
		t.Error("IsArrayError(unrelated error) = true; want false")
	}
	if IsArrayError(nil) {
		t.Error("IsArrayError(nil) = true; want false")
	}
}

func TestNewRootIsArrayError(t *testing.T) {
	err := NewRootIsArrayError("custom message")
	if err.Error() != "custom message" {
		t.Errorf("Error() = %q; want %q", err.Error(), "custom message")
	}
	if !IsArrayError(err) {
		t.Error("IsArrayError(NewRootIsArrayError(...)) = false; want true")
	}
}

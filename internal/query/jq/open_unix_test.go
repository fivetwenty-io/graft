//go:build unix

package jq_test

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/fivetwenty-io/graft/internal/query/jq"
)

// A FIFO that has no writer must be refused and must not hang the
// compiler, however a search came to name it.
func TestModulesRefuseFIFOs(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "f.json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "g.jq"), 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		opts := jq.CompileOptions{SearchPaths: []string{dir}}
		wantErrorOpts(t, opts, `import "f" as $f; 1`, `null`, `module not found: f`)
		wantErrorOpts(t, opts, `import "g" as g; 1`, `null`, `module not found: g`)
		wantErrorOpts(t, jq.CompileOptions{AutoInclude: filepath.Join(dir, "g.jq")}, `1`, `null`, `not a regular file`)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("compiling a FIFO import hung")
	}
}

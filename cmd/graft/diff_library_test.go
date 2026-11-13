package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

// renderFailures names the cases where the report renders only partly and
// graft diff prints the part anyway.
var renderFailures = map[string]bool{"binary-invalid": true}

// TestLibraryDiffMatchesTheCommand holds yaml.Diff and DiffReport.Write
// to the output of graft diff over every YAML and JSON parity case that
// graft diff can read and every output mode.
func TestLibraryDiffMatchesTheCommand(t *testing.T) {
	cases, err := filepath.Glob("../../tests/diff-parity/cases/*")
	if err != nil {
		t.Fatal(err)
	}
	compared := 0
	for _, dir := range cases {
		from, _ := filepath.Glob(filepath.Join(dir, "from.*"))
		to, _ := filepath.Glob(filepath.Join(dir, "to.*"))
		if len(from) != 1 || len(to) != 1 || strings.HasSuffix(from[0], ".toml") || strings.HasSuffix(to[0], ".toml") {
			continue
		}
		a, err := os.ReadFile(from[0])
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(to[0])
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range parityModes {
			stdout, _, code := renderDefaultDiff([]string{from[0], to[0]}, m.opts)
			r, err := yaml.Diff(a, b, nil)
			if code == 2 {
				if err == nil {
					t.Errorf("%s %s: graft diff failed but yaml.Diff did not", filepath.Base(dir), m.name)
				}
				continue
			}
			if err != nil {
				t.Errorf("%s %s: yaml.Diff: %v", filepath.Base(dir), m.name, err)
				continue
			}
			var buf bytes.Buffer
			style := yaml.ReportStyle{Color: m.opts.Color, TrueColor: m.opts.TrueColor, Width: m.opts.Width}
			// graft diff prints what it rendered and ignores
			// a render error that isn't fatal, as with a binary
			// value that isn't base64. Write returns that error
			// after the same output.
			var diffErr *yaml.DiffError
			if err := r.Write(&buf, style); err != nil {
				fatal := errors.As(err, &diffErr)
				expected := renderFailures[filepath.Base(dir)]
				if fatal || !expected {
					t.Errorf("%s %s: Write: %v", filepath.Base(dir), m.name, err)
					continue
				}
			}
			if buf.String()+"\n" != stdout {
				t.Errorf("%s %s: Write differs from graft diff", filepath.Base(dir), m.name)
			}
			if r.Empty() != (code == 0) {
				t.Errorf("%s %s: Empty() = %v, exit %d", filepath.Base(dir), m.name, r.Empty(), code)
			}
			compared++
		}
	}
	if floor := 71 * len(parityModes); compared < floor {
		t.Fatalf("compared %d case-mode pairs, want at least %d", compared, floor)
	}
}

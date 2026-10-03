package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fivetwenty-io/graft/internal/humanreport"
	"github.com/fivetwenty-io/graft/internal/yamlgolden"
)

// init pins the zone the goldens were rendered in, once, before any test
// runs. Earlier tests in this package leak cleanup tickers whose goroutines
// call time.Now, which reads time.Local, so a write made from inside a test
// (as yamlgolden.PinLocal does) races with them. Only a write made before
// any test starts is race-free.
//
//nolint:gochecknoinits // The pin must happen before any test starts, and TestMain is already a test name here.
func init() {
	time.Local = time.FixedZone("UTC-4", -4*60*60)
}

var parityModes = []struct {
	name string
	opts humanreport.Options
}{
	{"plain-80", humanreport.Options{Width: 80}},
	{"plain-0", humanreport.Options{Width: 0}},
	{"plain-200", humanreport.Options{Width: 200}},
	{"truecolor-80", humanreport.Options{Color: true, TrueColor: true, Width: 80}},
	{"truecolor-0", humanreport.Options{Color: true, TrueColor: true, Width: 0}},
	{"truecolor-200", humanreport.Options{Color: true, TrueColor: true, Width: 200}},
	{"ansi16-80", humanreport.Options{Color: true, Width: 80}},
	{"ansi16-0", humanreport.Options{Color: true, Width: 0}},
}

func TestDefaultDiffMatchesSpruceGoldens(t *testing.T) {
	cases, err := filepath.Glob("../../tests/diff-parity/cases/*")
	if err != nil || len(cases) != 51 {
		t.Fatalf("found %d corpus cases (%v), want 51", len(cases), err)
	}
	for _, rel := range cases {
		dir, err := filepath.Abs(rel)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(filepath.Base(dir), func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(dir, "want", "exit"))
			if err != nil {
				t.Fatal(err)
			}
			wantCode, err := strconv.Atoi(strings.TrimSpace(string(raw)))
			if err != nil {
				t.Fatal(err)
			}
			from, to := filepath.Base(yamlgolden.OnlyMatch(t, dir, "from")), filepath.Base(yamlgolden.OnlyMatch(t, dir, "to"))
			t.Chdir(dir)
			for _, m := range parityModes {
				stdout, stderr, code := renderDefaultDiff([]string{from, to}, m.opts)
				wantOut, err := os.ReadFile(filepath.Join(dir, "want", m.name))
				if err != nil {
					t.Fatal(err)
				}
				wantErr, err := os.ReadFile(filepath.Join(dir, "want", m.name+".stderr"))
				if err != nil {
					t.Fatal(err)
				}
				if code != wantCode {
					t.Errorf("%s: exit %d, want %d (stderr %q)", m.name, code, wantCode, stderr)
				}
				if stdout != string(wantOut) {
					t.Errorf("%s stdout:\n got %q\nwant %q", m.name, stdout, string(wantOut))
				}
				// The corpus's parse-error cases all have lines that libyaml and
				// goccy agree on, so D6 compares them through "yaml: line N: ".
				if prefix, ok := yamlgolden.LineErrorPrefix(string(wantErr)); ok {
					if !strings.HasPrefix(stderr, prefix) || !strings.HasSuffix(stderr, "\n") {
						t.Errorf("%s stderr %q, want the prefix %q and a final newline", m.name, stderr, prefix)
					}
				} else if stderr != string(wantErr) {
					t.Errorf("%s stderr %q, want %q", m.name, stderr, string(wantErr))
				}
			}
		})
	}
}

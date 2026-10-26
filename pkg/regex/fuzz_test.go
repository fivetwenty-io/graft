package regex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The fuzz targets run on their seeds in every test run. make fuzz-regex
// runs each one for 30 seconds outside the normal suite.

func FuzzCompile(f *testing.F) {
	for _, s := range []string{`a`, `(a|b)*c`, `(?<n>\d+)-\k<n>`, `[[:alpha:]]+`, `(?<=a)b`} {
		f.Add(s)
	}
	for _, c := range fuzzParityCases(f) {
		f.Add(c.Pattern)
	}
	f.Fuzz(func(t *testing.T, pat string) {
		for _, flavor := range []Flavor{PCRE, Oniguruma} {
			_, err := Compile(pat, WithFlavor(flavor))
			wantCompileError(t, pat, err)
		}
	})
}

func FuzzFind(f *testing.F) {
	f.Add(`(a+)+$`, "aaaaaaaaaaaaaaaaaaaab")
	for _, c := range fuzzParityCases(f) {
		f.Add(c.Pattern, c.Subject)
	}
	f.Fuzz(func(t *testing.T, pat, subj string) {
		for _, flavor := range []Flavor{PCRE, Oniguruma} {
			p, err := Compile(pat, WithFlavor(flavor), WithBudget(Budget{Steps: 100000}))
			if err != nil {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			began := time.Now()
			m, err := p.Find(ctx, subj, 0)
			elapsed := time.Since(began)
			cancel()
			switch {
			case err == nil:
				wantWellFormedMatch(t, p, m, subj)
			case !errors.Is(err, ErrBudget) && !errors.Is(err, context.DeadlineExceeded):
				t.Fatalf("%q against %q: unexpected error %v", pat, subj, err)
			}
			// The deadline or the budget stops the search well before a
			// second has gone by, even on a loaded machine.
			if elapsed > time.Second {
				t.Fatalf("%q against %q ran for %v", pat, subj, elapsed)
			}
		}
	})
}

// wantCompileError fails unless err is nil or one of the errors Compile
// documents.
func wantCompileError(t *testing.T, pat string, err error) {
	t.Helper()
	var se *SyntaxError
	var ue *UnsupportedError
	switch {
	case err == nil, errors.As(err, &ue), errors.Is(err, ErrBudget):
	case errors.As(err, &se):
		if se.Pos < 0 || se.Pos > len(pat) {
			t.Fatalf("%q: syntax error position %d is outside the pattern", pat, se.Pos)
		}
	default:
		t.Fatalf("%q: Compile returned %T %v", pat, err, err)
	}
}

// wantWellFormedMatch fails unless m, when there is one, lies within subj
// and its groups agree with its offsets.
func wantWellFormedMatch(t *testing.T, p *Pattern, m *Match, subj string) {
	t.Helper()
	if m == nil {
		return
	}
	if m.Start < 0 || m.Start > m.End || m.End > len(subj) || m.RuneStart > m.RuneEnd {
		t.Fatalf("match %+v does not fit a subject of %d bytes", m, len(subj))
	}
	if len(m.Groups) != len(p.GroupNames()) {
		t.Fatalf("match has %d groups, the pattern %d", len(m.Groups), len(p.GroupNames()))
	}
	if g := m.Groups[0]; !g.Matched || g.Start != m.Start || g.End != m.End {
		t.Fatalf("group 0 %+v is not the match %+v", g, m)
	}
	for i, g := range m.Groups {
		switch {
		case !g.Matched && (g.Start != -1 || g.End != -1):
			t.Fatalf("unmatched group %d has offsets %d and %d", i, g.Start, g.End)
		case g.Matched && (g.Start < 0 || g.Start > g.End || g.End > len(subj)):
			t.Fatalf("group %d %+v does not fit a subject of %d bytes", i, g, len(subj))
		}
	}
}

// fuzzParityCases reads the parity corpus for seeds.
func fuzzParityCases(f *testing.F) []parityCase {
	f.Helper()
	data, err := os.ReadFile(filepath.Join(parityDir, "cases.yaml"))
	if err != nil {
		f.Fatal(err)
	}
	cases, err := parseParityCases(string(data))
	if err != nil {
		f.Fatal(err)
	}
	return cases
}

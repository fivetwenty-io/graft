package humanreport

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/termstyle"
	"github.com/fivetwenty-io/graft/internal/yamldiff"
	"github.com/fivetwenty-io/graft/internal/yamlgolden"
)

var corpusModes = []struct {
	name string
	opts Options
}{
	{"plain-80", Options{Width: 80}},
	{"plain-0", Options{Width: 0}},
	{"plain-200", Options{Width: 200}},
	{"truecolor-80", Options{Color: true, TrueColor: true, Width: 80}},
	{"truecolor-0", Options{Color: true, TrueColor: true, Width: 0}},
	{"truecolor-200", Options{Color: true, TrueColor: true, Width: 200}},
	{"ansi16-80", Options{Color: true, Width: 80}},
	{"ansi16-0", Options{Color: true, Width: 0}},
}

func TestWriteMatchesCorpusGoldens(t *testing.T) {
	yamlgolden.PinLocal(t)
	cases, err := filepath.Glob("../../tests/diff-parity/cases/*")
	if err != nil || len(cases) != 74 {
		t.Fatalf("found %d corpus cases (%v), want 74", len(cases), err)
	}
	for _, rel := range cases {
		dir, err := filepath.Abs(rel)
		if err != nil {
			t.Fatal(err)
		}
		exit, err := os.ReadFile(filepath.Join(dir, "want", "exit"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(string(exit)) == "2" {
			continue // load and compare errors are the CLI's job; see cmd/graft
		}
		t.Run(filepath.Base(dir), func(t *testing.T) {
			from, to := filepath.Base(yamlgolden.OnlyMatch(t, dir, "from")), filepath.Base(yamlgolden.OnlyMatch(t, dir, "to"))
			t.Chdir(dir)
			for _, m := range corpusModes {
				// Each mode gets a fresh load and comparison, because
				// rendering reorders shared nodes in place.
				f, g, err := yamldiff.LoadFiles(from, to)
				if err != nil {
					t.Fatalf("LoadFiles: %v", err)
				}
				report, err := yamldiff.CompareInputFiles(f, g)
				if err != nil {
					t.Fatalf("CompareInputFiles: %v", err)
				}
				var buf bytes.Buffer
				werr := Write(&buf, report, m.opts)
				var fatal *FatalError
				if errors.As(werr, &fatal) {
					t.Fatalf("%s: fatal render error %v", m.name, werr)
				}
				buf.WriteByte('\n')
				want, err := os.ReadFile(filepath.Join(dir, "want", m.name))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(buf.Bytes(), want) {
					t.Errorf("%s:\n got %q\nwant %q", m.name, buf.String(), string(want))
				}
			}
		})
	}
}

func reportFor(t *testing.T, from, to string) yamldiff.Report {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{"from.yml": from, "to.yml": to} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f, g, err := yamldiff.LoadFiles(filepath.Join(dir, "from.yml"), filepath.Join(dir, "to.yml"))
	if err != nil {
		t.Fatal(err)
	}
	r, err := yamldiff.CompareInputFiles(f, g)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestWritePathForms(t *testing.T) {
	// dyff substitutes a key into its styled format raw, so escapes in a
	// key print untouched in every mode and never fail the report. These
	// bytes come from spruce v1.35.17 on the sgr-in-keys corpus case.
	r := reportFor(t, "\"k\\e[38mx\": 1\n\"n\\e[31my\": a\n", "\"k\\e[38mx\": 2\n\"n\\e[31my\": b\n")
	for _, c := range []struct {
		opts Options
		want []string
	}{
		{Options{Width: 80}, []string{"\nk\x1b[38mx\n± value change\n", "\nn\x1b[31my\n± value change\n"}},
		{Options{Color: true, TrueColor: true, Width: 80}, []string{"\n\x1b[1mk\x1b[38mx\x1b[0m\n", "\n\x1b[1mn\x1b[31my\x1b[0m\n"}},
	} {
		var b bytes.Buffer
		if err := Write(&b, r, c.opts); err != nil {
			t.Fatalf("Write(%+v) = %v, want the keys printed raw as spruce does", c.opts, err)
		}
		for _, w := range c.want {
			if !strings.Contains(b.String(), w) {
				t.Errorf("Write(%+v) = %q, want it to contain %q", c.opts, b.String(), w)
			}
		}
	}
}

func TestWriteStopsAtFailingDetail(t *testing.T) {
	r := reportFor(t, "a: x\nb: !!binary aGk=\nc: 1\n", "a: y\nb: !!binary \"@@@@\"\nc: 2\n")
	var buf bytes.Buffer
	err := Write(&buf, r, Options{Width: 80})
	var fatal *FatalError
	if err == nil || errors.As(err, &fatal) {
		t.Fatalf("err = %v, want an ordinary render error", err)
	}
	if want := "\na\n± value change\n- x\n+ y\n\nb\n"; buf.String() != want {
		t.Fatalf("partial output %q, want %q", buf.String(), want)
	}
}

func TestWriteMalformedEscapeIsFatal(t *testing.T) {
	r := reportFor(t, "a: x\n", "a: \"y\\e[38mz\"\n")
	err := Write(&bytes.Buffer{}, r, Options{Width: 80})
	var fatal *FatalError
	if !errors.As(err, &fatal) {
		t.Fatalf("err = %v, want *FatalError where spruce panics", err)
	}
}

func TestLocationStyler(t *testing.T) {
	tc := LocationStyler(Options{Color: true, TrueColor: true})
	ansi := LocationStyler(Options{Color: true})
	plain := LocationStyler(Options{})
	url := "http://127.0.0.1:9/x.yml"
	for _, c := range []struct {
		style yamldiff.LocationStyler
		kind  yamldiff.LocationKind
		loc   string
		want  string
	}{
		{tc, yamldiff.LocationFile, "bad.yml", "\x1b[1mbad.yml\x1b[0m"},
		{tc, yamldiff.LocationStdin, "-", "\x1b[1;3mstdin\x1b[0m"},
		{tc, yamldiff.LocationURI, url, "\x1b[4;38;2;100;149;237m" + url + "\x1b[0m"},
		{ansi, yamldiff.LocationURI, url, "\x1b[4;94m" + url + "\x1b[0m"},
		{tc, yamldiff.LocationPlain, "nosuch.yml", "nosuch.yml"},
		{plain, yamldiff.LocationFile, "bad.yml", "bad.yml"},
		{plain, yamldiff.LocationStdin, "-", "stdin"},
	} {
		if got := c.style(c.kind, c.loc); got != c.want {
			t.Errorf("style(%v, %q) = %q, want %q", c.kind, c.loc, got, c.want)
		}
	}
}

func fatalMessage(t *testing.T, from, to string) string {
	t.Helper()
	err := Write(&bytes.Buffer{}, reportFor(t, from, to), Options{Width: 80})
	var fatal *FatalError
	if !errors.As(err, &fatal) {
		t.Fatalf("err = %v, want *FatalError", err)
	}
	if strings.ContainsRune(err.Error(), '\x1b') {
		t.Errorf("message %q holds a raw escape byte", err.Error())
	}
	return err.Error()
}

func TestFatalErrorNamesDiffPath(t *testing.T) {
	const cause = "unsupported foreground color selection '[38]'"
	for _, c := range []struct {
		name     string
		from, to string
		want     string
	}{
		{"nested key", "a:\n  b: x\n", "a:\n  b: \"y\\e[38mz\"\n", "a.b: " + cause},
		{"list entry", "a:\n- name: web\n  v: x\n", "a:\n- name: web\n  v: \"y\\e[38mz\"\n", "a.web.v: " + cause},
		{"root level", "x\n", "\"y\\e[38mz\"\n", "(root level): " + cause},
		{"key with escape", "\"k\\e[1mx\": a\n", "\"k\\e[1mx\": \"y\\e[38mz\"\n", "\"k\\x1b[1mx\": " + cause},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := fatalMessage(t, c.from, c.to); got != c.want {
				t.Errorf("message %q, want %q", got, c.want)
			}
		})
	}
}

func TestFatalErrorKeepsCause(t *testing.T) {
	err := Write(&bytes.Buffer{}, reportFor(t, "a: x\n", "a: \"y\\e[38mz\"\n"), Options{Width: 80})
	var fatal *FatalError
	if !errors.As(err, &fatal) {
		t.Fatalf("err = %v, want *FatalError", err)
	}
	var sgr *termstyle.SGRError
	if fatal.Path != "a" {
		t.Errorf("Path = %q, want %q", fatal.Path, "a")
	}
	if !errors.As(err, &sgr) {
		t.Errorf("err = %v, want the *termstyle.SGRError reachable through Unwrap", err)
	}
}

func TestFatalErrorNamesDocumentInMultiDocumentInput(t *testing.T) {
	from := "a: 1\n---\nb:\n  c: x\n"
	to := "a: 1\n---\nb:\n  c: \"y\\e[38mz\"\n"
	want := "b.c  (document #2): unsupported foreground color selection '[38]'"
	if got := fatalMessage(t, from, to); got != want {
		t.Errorf("message %q, want %q", got, want)
	}
}

func TestPlainPathLabelWithoutPath(t *testing.T) {
	for _, showPathRoot := range []bool{false, true} {
		if got, want := plainPathLabel(nil, showPathRoot), "(file level)"; got != want {
			t.Errorf("plainPathLabel(nil, %v) = %q, want %q", showPathRoot, got, want)
		}
	}
}

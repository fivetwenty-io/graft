package yamldiff

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/yamlgolden"
)

func TestLoadFileMatchesYtbx(t *testing.T) {
	yamlgolden.PinLocal(t)
	var vectors []struct {
		Location  string             `json:"location"`
		Documents []*yamlgolden.Node `json:"documents"`
		Names     []string           `json:"names"`
		Error     string             `json:"error"`
	}
	yamlgolden.ReadJSON(t, "testdata/golden/load.json", &vectors)
	for _, v := range vectors {
		t.Run(v.Location, func(t *testing.T) {
			f, err := LoadFile(v.Location)
			if v.Error != "" {
				want := v.Error
				if prefix, ok := yamlgolden.LineErrorPrefix(want); ok {
					want = prefix
				}
				if err == nil || !strings.HasPrefix(err.Error(), want) {
					t.Fatalf("LoadFile error = %v, want the prefix %q", err, want)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadFile: %v", err)
			}
			if len(f.Documents) != len(v.Documents) {
				t.Fatalf("%d documents, want %d", len(f.Documents), len(v.Documents))
			}
			for i := range f.Documents {
				w := yamlgolden.Strip(v.Documents[i], false, false)
				if d := yamlgolden.Diff(w, yamlgolden.Strip(yamlgolden.FromNode(f.Documents[i]), false, false)); d != "" {
					t.Fatalf("document %d: %s", i, d)
				}
			}
			if strings.Join(f.Names, ",") != strings.Join(v.Names, ",") {
				t.Fatalf("names %v, want %v", f.Names, v.Names)
			}
		})
	}
}

func TestCompareLoadedFilesMatchesDyffGolden(t *testing.T) {
	yamlgolden.PinLocal(t)
	pkgDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range compareCases(t) {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(pkgDir, "testdata", "compare", name)
			from, to := filepath.Base(yamlgolden.OnlyMatch(t, dir, "from")), filepath.Base(yamlgolden.OnlyMatch(t, dir, "to"))
			t.Chdir(dir)
			f, g, err := LoadFiles(from, to)
			if err != nil {
				t.Fatalf("LoadFiles: %v", err)
			}
			report, err := CompareInputFiles(f, g)
			t.Chdir(pkgDir)
			checkCompareGolden(t, name, report, err)
		})
	}
}

func TestLoadFilesReportsFromErrorFirst(t *testing.T) {
	_, _, err := LoadFiles("testdata/load/inputs/nosuch-from.yml", "testdata/load/inputs/nosuch-to.yml")
	if err == nil || !strings.Contains(err.Error(), "nosuch-from.yml") || strings.Contains(err.Error(), "nosuch-to.yml") {
		t.Fatalf("err = %v, want only the from-file's error", err)
	}
	_, _, err = LoadFiles("testdata/load/inputs/two.yml", "testdata/load/inputs/nosuch-to.yml")
	if err == nil || !strings.Contains(err.Error(), "nosuch-to.yml") {
		t.Fatalf("err = %v, want the to-file's error when only it fails", err)
	}
}

func TestLoadURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok.yml" {
			_, _ = w.Write([]byte("a: 1\n"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("nope"))
	}))
	defer srv.Close()

	f, err := LoadFile(srv.URL + "/ok.yml")
	if err != nil || len(f.Documents) != 1 {
		t.Fatalf("LoadFile(url) = %+v, %v", f, err)
	}
	loc := srv.URL + "/missing.yml"
	_, err = LoadFile(loc)
	want := "unable to load data from " + loc + ": failed to retrieve data from location " + loc + ": nope"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	var le *LoadError
	if !errors.As(err, &le) || le.Kind != LocationURI || le.Op != "load" {
		t.Fatalf("err = %#v, want a *LoadError for a URI", err)
	}
}

func TestLoadStdin(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	prev := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = prev })
	_, _ = w.WriteString("a: 1\n---\nb: 2\n")
	_ = w.Close()

	f, err := LoadFile(" - ")
	if err != nil || len(f.Documents) != 2 {
		t.Fatalf("LoadFile(-) = %d documents, %v; want 2", len(f.Documents), err)
	}
	if !IsStdin("-") || IsStdin("--") {
		t.Fatal("IsStdin must accept exactly a trimmed dash")
	}
}

func TestLoadErrorStyledClassifiesLocations(t *testing.T) {
	style := func(kind LocationKind, loc string) string {
		return map[LocationKind]string{LocationPlain: "plain", LocationStdin: "stdin", LocationFile: "file", LocationURI: "uri"}[kind] + "<" + loc + ">"
	}
	for loc, kind := range map[string]LocationKind{
		"-":                             LocationStdin,
		"testdata/load/inputs/two.yml":  LocationFile,
		"/nonexistent/graft/nosuch.yml": LocationURI,
		"nosuch.yml":                    LocationPlain,
		"https://example.invalid/x.yml": LocationURI,
	} {
		if got := ClassifyLocation(loc); got != kind {
			t.Errorf("ClassifyLocation(%q) = %v, want %v", loc, got, kind)
		}
	}
	e := &LoadError{Op: "parse", Location: "bad.yml", Kind: LocationFile, Err: errors.New("boom")}
	if e.Error() != "unable to parse data from bad.yml: boom" || e.Styled(style) != "unable to parse data from file<bad.yml>: boom" {
		t.Fatalf("Error %q, Styled %q", e.Error(), e.Styled(style))
	}
	// ytbx prints the word stdin, not "-", even without color.
	in := &LoadError{Op: "parse", Location: "-", Kind: LocationStdin, Err: errors.New("boom")}
	if in.Error() != "unable to parse data from stdin: boom" {
		t.Fatalf("stdin Error %q", in.Error())
	}
}

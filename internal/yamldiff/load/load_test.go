package load

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/yamldiff"
	"github.com/fivetwenty-io/graft/internal/yamlgolden"
	"github.com/fivetwenty-io/graft/internal/yamlnode"
)

func TestFileMatchesYtbx(t *testing.T) {
	inCoreDir(t)
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
			f, err := File(v.Location)
			if v.Error != "" {
				want := v.Error
				if prefix, ok := yamlgolden.LineErrorPrefix(want); ok {
					want = prefix
				}
				if err == nil || !strings.HasPrefix(err.Error(), want) {
					t.Fatalf("File error = %v, want the prefix %q", err, want)
				}
				return
			}
			if err != nil {
				t.Fatalf("File: %v", err)
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

func TestFilesReportsFromErrorFirst(t *testing.T) {
	inCoreDir(t)
	_, _, err := Files("testdata/load/inputs/nosuch-from.yml", "testdata/load/inputs/nosuch-to.yml")
	if err == nil || !strings.Contains(err.Error(), "nosuch-from.yml") || strings.Contains(err.Error(), "nosuch-to.yml") {
		t.Fatalf("err = %v, want only the from-file's error", err)
	}
	_, _, err = Files("testdata/load/inputs/two.yml", "testdata/load/inputs/nosuch-to.yml")
	if err == nil || !strings.Contains(err.Error(), "nosuch-to.yml") {
		t.Fatalf("err = %v, want the to-file's error when only it fails", err)
	}
}

func TestURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok.yml" {
			_, _ = w.Write([]byte("a: 1\n"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("nope"))
	}))
	defer srv.Close()

	f, err := File(srv.URL + "/ok.yml")
	if err != nil || len(f.Documents) != 1 {
		t.Fatalf("File(url) = %+v, %v", f, err)
	}
	loc := srv.URL + "/missing.yml"
	_, err = File(loc)
	want := "unable to load data from " + loc + ": failed to retrieve data from location " + loc + ": nope"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	var le *Error
	if !errors.As(err, &le) || le.Kind != yamldiff.LocationURI || le.Op != "load" {
		t.Fatalf("err = %#v, want a *Error for a URI", err)
	}
}

func TestStdin(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	prev := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = prev })
	_, _ = w.WriteString("a: 1\n---\nb: 2\n")
	_ = w.Close()

	f, err := File(" - ")
	if err != nil || len(f.Documents) != 2 {
		t.Fatalf("File(-) = %d documents, %v; want 2", len(f.Documents), err)
	}
	if !IsStdin("-") || IsStdin("--") {
		t.Fatal("IsStdin must accept exactly a trimmed dash")
	}
}

func TestErrorStyledClassifiesLocations(t *testing.T) {
	inCoreDir(t)
	style := func(kind yamldiff.LocationKind, loc string) string {
		return map[yamldiff.LocationKind]string{yamldiff.LocationPlain: "plain", yamldiff.LocationStdin: "stdin", yamldiff.LocationFile: "file", yamldiff.LocationURI: "uri"}[kind] + "<" + loc + ">"
	}
	for loc, kind := range map[string]yamldiff.LocationKind{
		"-":                             yamldiff.LocationStdin,
		"testdata/load/inputs/two.yml":  yamldiff.LocationFile,
		"/nonexistent/graft/nosuch.yml": yamldiff.LocationURI,
		"nosuch.yml":                    yamldiff.LocationPlain,
		"https://example.invalid/x.yml": yamldiff.LocationURI,
	} {
		if got := ClassifyLocation(loc); got != kind {
			t.Errorf("ClassifyLocation(%q) = %v, want %v", loc, got, kind)
		}
	}
	e := &Error{Op: "parse", Location: "bad.yml", Kind: yamldiff.LocationFile, Err: errors.New("boom")}
	if e.Error() != "unable to parse data from bad.yml: boom" || e.Styled(style) != "unable to parse data from file<bad.yml>: boom" {
		t.Fatalf("Error %q, Styled %q", e.Error(), e.Styled(style))
	}
	// ytbx prints the word stdin, not "-", even without color.
	in := &Error{Op: "parse", Location: "-", Kind: yamldiff.LocationStdin, Err: errors.New("boom")}
	if in.Error() != "unable to parse data from stdin: boom" {
		t.Fatalf("stdin Error %q", in.Error())
	}
}

func TestDocumentsStillReadsTOML(t *testing.T) {
	docs, err := Documents([]byte("a = 1\n[b]\nc = \"x\"\n"))
	if err != nil || len(docs) != 1 || docs[0].Content[0].Kind != yamlnode.MappingNode {
		t.Fatalf("Documents(toml) = %v, %v", docs, err)
	}
}

// TestDocumentsReadsEmptyInputBeforeTOML pins that blank input and a
// bare document marker are one null document, where TOML would read them
// as an empty table.
func TestDocumentsReadsEmptyInputBeforeTOML(t *testing.T) {
	for _, in := range []string{"", "---"} {
		docs, err := Documents([]byte(in))
		if err != nil || len(docs) != 1 || docs[0].Content[0].Kind != yamlnode.ScalarNode || docs[0].Content[0].Tag != yamlnode.TagNull {
			t.Errorf("Documents(%q) = %v, %v", in, docs, err)
		}
	}
}

// inCoreDir runs the test from internal/yamldiff, where the fixtures and
// the goldens live.
func inCoreDir(t *testing.T) {
	t.Helper()
	t.Chdir("..")
}

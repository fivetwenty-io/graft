package yamlcodec_test

import (
	"reflect"
	"testing"

	"github.com/fivetwenty-io/graft/internal/yamlcodec"
)

// walk parses doc with duplicates allowed and returns each report.
func walk(t *testing.T, doc string) []yamlcodec.DuplicateKey {
	t.Helper()
	var got []yamlcodec.DuplicateKey
	body, err := yamlcodec.ParseMergeBodyDuplicates([]byte(doc), false, func(d yamlcodec.DuplicateKey) {
		got = append(got, d)
	})
	if err != nil {
		t.Fatalf("ParseMergeBodyDuplicates: %v", err)
	}
	if body == nil {
		t.Fatal("body is nil, want a mapping")
	}
	return got
}

func TestWalkDuplicateKeysReportsLaterOccurrences(t *testing.T) {
	doc := "a: 1\nb:\n  c: 1\n  c: 2\n  d: [{e: 1, e: 2}]\na: 3\na: 4\n"
	want := []yamlcodec.DuplicateKey{
		{Path: "b.c", Line: 4, Column: 3, FirstLine: 3, FirstColumn: 3},
		{Path: "b.d[0].e", Line: 5, Column: 14, FirstLine: 5, FirstColumn: 8},
		{Path: "a", Line: 6, Column: 1, FirstLine: 1, FirstColumn: 1},
		{Path: "a", Line: 7, Column: 1, FirstLine: 1, FirstColumn: 1},
	}
	if got := walk(t, doc); !reflect.DeepEqual(got, want) {
		t.Fatalf("reports = %+v, want %+v", got, want)
	}
}

func TestWalkDuplicateKeysSkipsMergeKeys(t *testing.T) {
	doc := "src: &s {x: 1}\n" +
		"m:\n  <<: *s\n  <<: *s\n"
	if got := walk(t, doc); len(got) != 0 {
		t.Fatalf("reports = %+v, want none", got)
	}
}

func TestWalkDuplicateKeysWalksAnAnchorOnce(t *testing.T) {
	doc := "a: &b\n  x: 1\n  x: 2\nc: *b\nd: *b\n"
	want := []yamlcodec.DuplicateKey{{Path: "a.x", Line: 3, Column: 3, FirstLine: 2, FirstColumn: 3}}
	if got := walk(t, doc); !reflect.DeepEqual(got, want) {
		t.Fatalf("reports = %+v, want %+v", got, want)
	}
}

func TestWalkDuplicateKeysTaggedAndAnchoredKeys(t *testing.T) {
	doc := "&k a: 1\n!!str a: 2\n\"a\": 3\n"
	want := []yamlcodec.DuplicateKey{
		{Path: "a", Line: 2, Column: 1, FirstLine: 1, FirstColumn: 1},
		{Path: "a", Line: 3, Column: 1, FirstLine: 1, FirstColumn: 1},
	}
	if got := walk(t, doc); !reflect.DeepEqual(got, want) {
		t.Fatalf("reports = %+v, want %+v", got, want)
	}
}

func TestWalkDuplicateKeysRootSequence(t *testing.T) {
	doc := "- a: 1\n  a: 2\n- - b: 1\n    b: 2\n"
	want := []yamlcodec.DuplicateKey{
		{Path: "[0].a", Line: 2, Column: 3, FirstLine: 1, FirstColumn: 3},
		{Path: "[1][0].b", Line: 4, Column: 5, FirstLine: 3, FirstColumn: 5},
	}
	if got := walk(t, doc); !reflect.DeepEqual(got, want) {
		t.Fatalf("reports = %+v, want %+v", got, want)
	}
}

func TestParseMergeBodyDuplicatesEmptyAndInvalid(t *testing.T) {
	called := false
	report := func(yamlcodec.DuplicateKey) { called = true }
	for _, doc := range []string{"", "# only a comment\n"} {
		body, err := yamlcodec.ParseMergeBodyDuplicates([]byte(doc), false, report)
		if err != nil || body != nil {
			t.Errorf("doc %q: body = %v, err = %v, want nil and nil", doc, body, err)
		}
	}
	_, err := yamlcodec.ParseMergeBodyDuplicates([]byte("a: [1\na: 2\n"), false, report)
	if err == nil {
		t.Fatal("want a syntax error")
	}
	if called {
		t.Fatal("report called for an empty or invalid document")
	}
}

func TestWalkDuplicateKeysNilPairAndNilBody(t *testing.T) {
	yamlcodec.WalkDuplicateKeys(nil, func(yamlcodec.DuplicateKey) { t.Fatal("report called") })
}

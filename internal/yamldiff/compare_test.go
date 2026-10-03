package yamldiff

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/fivetwenty-io/graft/internal/yamlgolden"
	"github.com/fivetwenty-io/graft/internal/yamlnode"
)

// loaderOnly cases depend on ytbx's loader (an empty file becomes one
// null document, and JSON and TOML convert through marshaling), so Task
// 16's loader test covers them.
var loaderOnly = map[string]bool{"empty-vs-map": true, "json-key-order": true, "toml-dates": true}

func parsedInput(t *testing.T, path string) InputFile {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	docs, err := yamlnode.Parse(data)
	if err != nil {
		t.Fatalf("Parse(%s): %v", path, err)
	}
	return InputFile{Location: filepath.Base(path), Documents: docs}
}

func checkCompareGolden(t *testing.T, name string, report Report, err error) {
	t.Helper()
	var want goldenReport
	yamlgolden.ReadJSON(t, "testdata/golden/compare/"+name+".json", &want)
	if want.Error != "" {
		if err == nil || err.Error() != want.Error {
			t.Fatalf("CompareInputFiles error = %v, want %q", err, want.Error)
		}
		return
	}
	if err != nil {
		t.Fatalf("CompareInputFiles: %v", err)
	}
	if d := yamlgolden.Diff(stripGoldenReport(want), toGoldenReport(report)); d != "" {
		t.Fatal(d)
	}
}

func TestCompareInputFilesMatchesDyffGolden(t *testing.T) {
	for _, name := range compareCases(t) {
		if loaderOnly[name] {
			continue
		}
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join("testdata", "compare", name)
			from := parsedInput(t, yamlgolden.OnlyMatch(t, dir, "from"))
			to := parsedInput(t, yamlgolden.OnlyMatch(t, dir, "to"))
			report, err := CompareInputFiles(from, to)
			checkCompareGolden(t, name, report, err)
		})
	}
}

func TestCompareBracePlaceholdersReportValueChange(t *testing.T) {
	from := InputFile{Location: "a", Documents: mustParse(t, "a: {{x}}\n")}
	to := InputFile{Location: "b", Documents: mustParse(t, "a: {{y}}\n")}
	report, err := CompareInputFiles(from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Diffs) != 1 || report.Diffs[0].Path.ToDotStyle() != "a" {
		t.Fatalf("diffs = %+v, want one diff at a", report.Diffs)
	}
	det := report.Diffs[0].Details[0]
	if det.Kind != MODIFICATION || det.From.Value != "{{x}}" || det.To.Value != "{{y}}" || det.To.Tag != "!!str" {
		t.Fatalf("detail = %+v, want a !!str modification from {{x}} to {{y}}", det)
	}
}

func TestCompareSharesNodesWithInputs(t *testing.T) {
	from := InputFile{Documents: mustParse(t, "a: {x: 1}\nb: 2\n")}
	to := InputFile{Documents: mustParse(t, "b: 2\n")}
	report, err := CompareInputFiles(from, to)
	if err != nil {
		t.Fatal(err)
	}
	removed := report.Diffs[0].Details[0].From
	original := from.Documents[0].Content[0]
	if removed.Content[0] != original.Content[0] || removed.Content[1] != original.Content[1] {
		t.Fatal("a removal fragment must reuse the input's key and value nodes, as dyff's does")
	}
}

func TestCompareUnhashableKeyIsAnError(t *testing.T) {
	collectionKey := &yamlnode.Node{Kind: yamlnode.SequenceNode, Tag: "!!seq", Line: 2}
	entry := &yamlnode.Node{Kind: yamlnode.MappingNode, Tag: "!!map", Content: []*yamlnode.Node{collectionKey, {Kind: yamlnode.ScalarNode, Tag: "!!int", Value: "1"}}}
	scalar := func(v string) *yamlnode.Node {
		return &yamlnode.Node{Kind: yamlnode.ScalarNode, Tag: "!!str", Value: v}
	}
	list := func(items ...*yamlnode.Node) InputFile {
		root := &yamlnode.Node{Kind: yamlnode.MappingNode, Tag: "!!map", Content: []*yamlnode.Node{scalar("l"), {Kind: yamlnode.SequenceNode, Tag: "!!seq", Content: items}}}
		return InputFile{Documents: []*yamlnode.Node{{Kind: yamlnode.DocumentNode, Content: []*yamlnode.Node{root}}}}
	}
	_, err := CompareInputFiles(list(entry, scalar("x")), list(scalar("y"), scalar("x")))
	var unhashable *UnhashableKeyError
	if !errors.As(err, &unhashable) {
		t.Fatalf("err = %v, want *UnhashableKeyError", err)
	}
}

// TestCompareNonStandardIdentifierThreshold pins dyff's guess threshold
// from both sides. A guessed field names list entries only when it has
// more than three distinct values, so three entries compare as a simple
// list and four compare by name.
func TestCompareNonStandardIdentifierThreshold(t *testing.T) {
	const entries = "list:\n- job: a\n  count: 1\n- job: b\n  count: 1\n"
	const fromC, toC = "- job: c\n  count: 1\n", "- job: c\n  count: 2\n"
	const d = "- job: d\n  count: 1\n"

	t.Run("three entries stay a simple list", func(t *testing.T) {
		from := InputFile{Documents: mustParse(t, entries+fromC)}
		to := InputFile{Documents: mustParse(t, entries+toC)}
		report, err := CompareInputFiles(from, to)
		if err != nil {
			t.Fatal(err)
		}
		if len(report.Diffs) != 1 {
			t.Fatalf("got %d diffs, want 1", len(report.Diffs))
		}
		diff := report.Diffs[0]
		if diff.Path.ToDotStyle() != "list" || diff.Path.ToGoPatchStyle() != "/list" {
			t.Fatalf("path = %q (%q), want list (/list)", diff.Path.ToDotStyle(), diff.Path.ToGoPatchStyle())
		}
		if len(diff.Details) != 2 || diff.Details[0].Kind != REMOVAL || diff.Details[1].Kind != ADDITION {
			t.Fatalf("details = %+v, want a removal then an addition", diff.Details)
		}
		for _, fragment := range []*yamlnode.Node{diff.Details[0].From, diff.Details[1].To} {
			if fragment.Kind != yamlnode.SequenceNode || fragment.Tag != "!!seq" || len(fragment.Content) != 1 {
				t.Fatalf("fragment = %+v, want a !!seq sequence of one entry", fragment)
			}
			if job, ok := ValueByKey(fragment.Content[0], "job"); !ok || job.Value != "c" {
				t.Fatalf("fragment entry = %+v, want the entry whose job is c", fragment.Content[0])
			}
		}
	})

	t.Run("four entries become a named list", func(t *testing.T) {
		from := InputFile{Documents: mustParse(t, entries+fromC+d)}
		to := InputFile{Documents: mustParse(t, entries+toC+d)}
		report, err := CompareInputFiles(from, to)
		if err != nil {
			t.Fatal(err)
		}
		if len(report.Diffs) != 1 {
			t.Fatalf("got %d diffs, want 1", len(report.Diffs))
		}
		diff := report.Diffs[0]
		if diff.Path.ToDotStyle() != "list.c.count" || diff.Path.ToGoPatchStyle() != "/list/job=c/count" {
			t.Fatalf("path = %q (%q), want list.c.count (/list/job=c/count)", diff.Path.ToDotStyle(), diff.Path.ToGoPatchStyle())
		}
		if len(diff.Details) != 1 {
			t.Fatalf("details = %+v, want one modification", diff.Details)
		}
		det := diff.Details[0]
		if det.Kind != MODIFICATION || det.From.Value != "1" || det.To.Value != "2" {
			t.Fatalf("detail = %+v, want a modification from 1 to 2", det)
		}
	})
}

// TestCompareListDetailsCarryEntryIndexes pins the positions a list
// addition or removal records. The k-th surplus copy of a repeated value
// takes the k-th position of that value after the ones the other list
// matches, and a named entry takes its own position.
func TestCompareListDetailsCarryEntryIndexes(t *testing.T) {
	for _, c := range []struct {
		name, from, to string
		removals       []int
		additions      []int
	}{
		{"append", "l: [a]\n", "l: [a, b, c]\n", nil, []int{1, 2}},
		{"tail removal", "l: [a, b, c]\n", "l: [a]\n", []int{1, 2}, nil},
		{"front removal and end addition", "l: [a, b]\n", "l: [b, c]\n", []int{0}, []int{1}},
		{"repeated value", "l: [a, a]\n", "l: [a, a, a]\n", nil, []int{2}},
		{"two surplus copies", "l: [a, b, a, a]\n", "l: [b, a]\n", []int{2, 3}, nil},
		{"named entries", "l: [{name: x}, {name: y}]\n", "l: [{name: x}, {name: z}, {name: y}]\n", nil, []int{1}},
	} {
		t.Run(c.name, func(t *testing.T) {
			report, err := CompareInputFiles(
				InputFile{Documents: mustParse(t, c.from)},
				InputFile{Documents: mustParse(t, c.to)},
			)
			if err != nil {
				t.Fatal(err)
			}
			var removals, additions []int
			for _, diff := range report.Diffs {
				for _, det := range diff.Details {
					switch det.Kind {
					case REMOVAL:
						removals = append(removals, det.Indexes...)
						if len(det.Indexes) != len(det.From.Content) {
							t.Fatalf("removal has %d indexes for %d entries", len(det.Indexes), len(det.From.Content))
						}
					case ADDITION:
						additions = append(additions, det.Indexes...)
						if len(det.Indexes) != len(det.To.Content) {
							t.Fatalf("addition has %d indexes for %d entries", len(det.Indexes), len(det.To.Content))
						}
					case MODIFICATION, ORDERCHANGE:
					}
				}
			}
			if !reflect.DeepEqual(removals, c.removals) || !reflect.DeepEqual(additions, c.additions) {
				t.Fatalf("removals %v additions %v, want %v and %v", removals, additions, c.removals, c.additions)
			}
		})
	}
}

package yamldiff

import (
	"errors"
	"os"
	"path/filepath"
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

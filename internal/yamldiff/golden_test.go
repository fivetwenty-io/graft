package yamldiff

import (
	"os"
	"sort"
	"testing"

	"github.com/fivetwenty-io/graft/internal/yamlgolden"
)

type goldenElem struct {
	Idx  int    `json:"idx"`
	Key  string `json:"key,omitempty"`
	Name string `json:"name,omitempty"`
}

type goldenDetail struct {
	Kind string           `json:"kind"`
	From *yamlgolden.Node `json:"from,omitempty"`
	To   *yamlgolden.Node `json:"to,omitempty"`
}

type goldenDiff struct {
	NilPath     bool           `json:"nil_path,omitempty"`
	NilElements bool           `json:"nil_elements,omitempty"`
	Dot         string         `json:"dot"`
	GoPatch     string         `json:"gopatch"`
	Root        string         `json:"root"`
	DocumentIdx int            `json:"document_idx"`
	Elements    []goldenElem   `json:"elements,omitempty"`
	Details     []goldenDetail `json:"details"`
}

type goldenReport struct {
	FromDocuments int          `json:"from_documents"`
	FromNames     []string     `json:"from_names,omitempty"`
	ToNames       []string     `json:"to_names,omitempty"`
	Diffs         []goldenDiff `json:"diffs"`
	Error         string       `json:"error,omitempty"`
}

func toGoldenReport(r Report) goldenReport {
	g := goldenReport{FromDocuments: len(r.From.Documents), FromNames: r.From.Names, ToNames: r.To.Names, Diffs: []goldenDiff{}}
	for _, d := range r.Diffs {
		gd := goldenDiff{Details: []goldenDetail{}}
		if d.Path == nil {
			gd.NilPath = true
		} else {
			gd.Dot, gd.GoPatch, gd.Root = d.Path.ToDotStyle(), d.Path.ToGoPatchStyle(), d.Path.RootDescription()
			gd.DocumentIdx, gd.NilElements = d.Path.DocumentIdx, d.Path.PathElements == nil
			for _, e := range d.Path.PathElements {
				gd.Elements = append(gd.Elements, goldenElem(e))
			}
		}
		for _, det := range d.Details {
			gd.Details = append(gd.Details, goldenDetail{
				Kind: string(det.Kind),
				From: yamlgolden.Strip(yamlgolden.FromNode(det.From), false, false),
				To:   yamlgolden.Strip(yamlgolden.FromNode(det.To), false, false),
			})
		}
		g.Diffs = append(g.Diffs, gd)
	}
	return g
}

func stripGoldenReport(g goldenReport) goldenReport {
	for i := range g.Diffs {
		for j := range g.Diffs[i].Details {
			det := &g.Diffs[i].Details[j]
			det.From, det.To = yamlgolden.Strip(det.From, false, false), yamlgolden.Strip(det.To, false, false)
		}
	}
	return g
}

func compareCases(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir("testdata/compare")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) != 19 {
		t.Fatalf("found %d compare cases, want 19", len(names))
	}
	return names
}

package histdiff

import (
	"fmt"
	"reflect"
	"testing"
)

func TestCompareIdenticalDocumentsReturnsNoChanges(t *testing.T) {
	from := map[string]interface{}{"database": map[string]interface{}{"host": "localhost", "port": 5432}}
	to := map[string]interface{}{"database": map[string]interface{}{"host": "localhost", "port": 5432}}

	changes, err := Compare("base.yml", from, "modified.yml", to)
	if err != nil {
		t.Fatalf("Compare returned error: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("expected no changes for identical documents, got %+v", changes)
	}
}

func TestCompareDetectsModification(t *testing.T) {
	from := map[string]interface{}{"database": map[string]interface{}{"host": "localhost", "port": 5432}}
	to := map[string]interface{}{"database": map[string]interface{}{"host": "db.prod.example.com", "port": 5432}}

	changes, err := Compare("base.yml", from, "modified.yml", to)
	if err != nil {
		t.Fatalf("Compare returned error: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected exactly one change, got %+v", changes)
	}
	c := changes[0]
	if c.Path != "database.host" {
		t.Errorf("Path = %q, want %q", c.Path, "database.host")
	}
	if c.Kind != Modified {
		t.Errorf("Kind = %v, want Modified", c.Kind)
	}
	if c.Old != "localhost" {
		t.Errorf("Old = %v, want localhost", c.Old)
	}
	if c.New != "db.prod.example.com" {
		t.Errorf("New = %v, want db.prod.example.com", c.New)
	}
}

func TestCompareDetectsAdditionAndRemoval(t *testing.T) {
	from := map[string]interface{}{"database": map[string]interface{}{"host": "localhost"}, "meta": map[string]interface{}{"internal": true}}
	to := map[string]interface{}{"database": map[string]interface{}{"host": "localhost", "ssl": true}}

	changes, err := Compare("base.yml", from, "modified.yml", to)
	if err != nil {
		t.Fatalf("Compare returned error: %v", err)
	}

	var sawAdded, sawRemoved bool
	for _, c := range changes {
		switch {
		case c.Path == "database.ssl" && c.Kind == Added:
			sawAdded = true
			if c.New != true {
				t.Errorf("added change New = %v, want true", c.New)
			}
		case c.Path == "meta" && c.Kind == Removed:
			sawRemoved = true
		}
	}
	if !sawAdded {
		t.Errorf("expected an Added change at database.ssl, got %+v", changes)
	}
	if !sawRemoved {
		t.Errorf("expected a Removed change at meta, got %+v", changes)
	}
}

func TestCompareResultIsSortedByPath(t *testing.T) {
	from := map[string]interface{}{"z": 1, "a": 1, "m": 1}
	to := map[string]interface{}{"z": 2, "a": 2, "m": 2}

	changes, err := Compare("base.yml", from, "modified.yml", to)
	if err != nil {
		t.Fatalf("Compare returned error: %v", err)
	}
	if len(changes) != 3 {
		t.Fatalf("expected 3 changes, got %d: %+v", len(changes), changes)
	}
	for i := 1; i < len(changes); i++ {
		if changes[i-1].Path >= changes[i].Path {
			t.Fatalf("changes not sorted by path: %+v", changes)
		}
	}
}

func TestTopLevelPaths(t *testing.T) {
	changes := []Change{
		{Path: "database.host", Kind: Modified},
		{Path: "database.port", Kind: Modified},
		{Path: "meta", Kind: Removed},
		{Path: "api.key", Kind: Added},
	}
	got := TopLevelPaths(changes)
	want := []string{"api", "database", "meta"}
	if len(got) != len(want) {
		t.Fatalf("TopLevelPaths() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("TopLevelPaths() = %v, want %v", got, want)
		}
	}
}

func TestCountChanges(t *testing.T) {
	changes := []Change{
		{Kind: Modified}, {Kind: Modified}, {Kind: Added}, {Kind: Removed},
	}
	counts := CountChanges(changes)
	if counts.Modified != 2 || counts.Added != 1 || counts.Removed != 1 {
		t.Fatalf("CountChanges() = %+v, want {Modified:2 Added:1 Removed:1}", counts)
	}
}

func TestCompareDetectsSimpleListAddition(t *testing.T) {
	from := map[string]interface{}{"features": []interface{}{"auth"}}
	to := map[string]interface{}{"features": []interface{}{"auth", "logging"}}

	changes, err := Compare("base.yml", from, "modified.yml", to)
	if err != nil {
		t.Fatalf("Compare returned error: %v", err)
	}

	var sawAdded bool
	for _, c := range changes {
		if c.Kind == Added && c.New == "logging" {
			sawAdded = true
		}
	}
	if !sawAdded {
		t.Fatalf("expected an Added change with value 'logging', got %+v", changes)
	}
}

func TestCompareScalarRootTypeChange(t *testing.T) {
	// A root-level type change (map -> scalar) should not error even though
	// it's an unusual document shape; yamldiff represents it as a modification
	// at the root.
	from := map[string]interface{}{"a": 1}
	to := map[string]interface{}{"a": "one"}

	changes, err := Compare("base.yml", from, "modified.yml", to)
	if err != nil {
		t.Fatalf("Compare returned error: %v", err)
	}
	if len(changes) != 1 || changes[0].Path != "a" {
		t.Fatalf("expected single change at 'a', got %+v", changes)
	}
}

func TestCompareNumbersListEntriesByRealIndex(t *testing.T) {
	type pair struct {
		Kind Kind
		Path string
	}
	elevenItems := make([]interface{}, 11)
	for i := range elevenItems {
		elevenItems[i] = fmt.Sprintf("item%d", i)
	}
	elevenAdded := make([]pair, 11)
	for i := range elevenAdded {
		elevenAdded[i] = pair{Added, fmt.Sprintf("l[%d]", i)}
	}

	for _, c := range []struct {
		name     string
		from, to interface{}
		want     []pair
	}{
		{"append", l{"a"}, l{"a", "b", "c"}, []pair{{Added, "l[1]"}, {Added, "l[2]"}}},
		{"tail removal", l{"a", "b", "c"}, l{"a"}, []pair{{Removed, "l[1]"}, {Removed, "l[2]"}}},
		{"front removal and end addition", l{"a", "b"}, l{"b", "c"}, []pair{{Removed, "l[0]"}, {Added, "l[1]"}}},
		{"repeated value", l{"a", "a"}, l{"a", "a", "a"}, []pair{{Added, "l[2]"}}},
		{"repeated value removal", l{"a", "a", "a"}, l{"a", "a"}, []pair{{Removed, "l[2]"}}},
		{"named entries", l{m{"name": "x"}}, l{m{"name": "x"}, m{"name": "y"}}, []pair{{Added, "l[1]"}}},
		{"named entry removal", l{m{"name": "x"}, m{"name": "y"}, m{"name": "z"}}, l{m{"name": "x"}, m{"name": "z"}}, []pair{{Removed, "l[1]"}}},
		{"eleven appended entries in numeric order", l{}, elevenItems, elevenAdded},
	} {
		t.Run(c.name, func(t *testing.T) {
			changes, err := Compare("from", m{"l": c.from}, "to", m{"l": c.to})
			if err != nil {
				t.Fatalf("Compare: %v", err)
			}
			var got []pair
			for _, ch := range changes {
				if ch.Kind == Modified {
					continue
				}
				got = append(got, pair{ch.Kind, ch.Path})
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestComparePathOrderIsNumericWithinListIndexes(t *testing.T) {
	changes := []Change{{Path: "l[10]"}, {Path: "l[2]"}, {Path: "l[1].b"}, {Path: "k"}, {Path: "l[1]"}}
	sortChanges(changes)
	var got []string
	for _, c := range changes {
		got = append(got, c.Path)
	}
	want := []string{"k", "l[1]", "l[1].b", "l[2]", "l[10]"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestCompareDocumentsNumbersTheDocumentThatDiffers(t *testing.T) {
	from := []interface{}{map[string]interface{}{"a": 1}, map[string]interface{}{"b": 1}}
	to := []interface{}{map[string]interface{}{"a": 1}, map[string]interface{}{"b": 2}}

	changes, err := CompareDocuments("from.yml", from, "to.yml", to)
	if err != nil {
		t.Fatalf("CompareDocuments returned error: %v", err)
	}
	want := []Change{{Path: "b", Kind: Modified, Old: 1, New: 2, Document: 2}}
	if !reflect.DeepEqual(changes, want) {
		t.Fatalf("changes = %+v, want %+v", changes, want)
	}
}

func TestCompareDocumentsLeavesSingleDocumentsUnnumbered(t *testing.T) {
	from := []interface{}{map[string]interface{}{"b": 1}}
	to := []interface{}{map[string]interface{}{"b": 2}}

	changes, err := CompareDocuments("from.yml", from, "to.yml", to)
	if err != nil {
		t.Fatalf("CompareDocuments returned error: %v", err)
	}
	if len(changes) != 1 || changes[0].Document != 0 {
		t.Fatalf("changes = %+v, want one change with Document 0", changes)
	}
}

func TestCompareDocumentsRejectsDifferentDocumentCounts(t *testing.T) {
	from := []interface{}{map[string]interface{}{"a": 1}, map[string]interface{}{"b": 1}}
	to := []interface{}{map[string]interface{}{"a": 1}}

	_, err := CompareDocuments("from.yml", from, "to.yml", to)
	if err == nil {
		t.Fatal("CompareDocuments accepted inputs with different numbers of documents")
	}
	want := "comparing YAMLs with a different number of documents is currently not supported"
	if err.Error() != want {
		t.Fatalf("error = %q, want the default diff's %q", err.Error(), want)
	}
}

func kubernetesResource(kind, name string, value int) map[string]interface{} {
	return map[string]interface{}{
		"apiVersion": "v1",
		"kind":       kind,
		"metadata":   map[string]interface{}{"name": name},
		"value":      value,
	}
}

func TestCompareDocumentsMatchesKubernetesDocumentsByName(t *testing.T) {
	a, b := kubernetesResource("A", "a", 1), kubernetesResource("B", "b", 2)

	changes, err := CompareDocuments("from.yml", []interface{}{a, b}, "to.yml", []interface{}{b, a})
	if err != nil {
		t.Fatalf("CompareDocuments returned error: %v", err)
	}
	want := []Change{{
		Kind:      Modified,
		Old:       []interface{}{"v1/A/a", "v1/B/b"},
		New:       []interface{}{"v1/B/b", "v1/A/a"},
		FileLevel: true,
	}}
	if !reflect.DeepEqual(changes, want) {
		t.Fatalf("documents in swapped order must match by name and report only the order change, got %+v, want %+v", changes, want)
	}

	changedB := kubernetesResource("B", "b", 3)
	changes, err = CompareDocuments("from.yml", []interface{}{a, b}, "to.yml", []interface{}{changedB, a})
	if err != nil {
		t.Fatalf("CompareDocuments returned error: %v", err)
	}
	want = []Change{
		{Path: "value", Kind: Modified, Old: 2, New: 3, Document: 2},
		{
			Kind:      Modified,
			Old:       []interface{}{"v1/A/a", "v1/B/b"},
			New:       []interface{}{"v1/B/b", "v1/A/a"},
			FileLevel: true,
		},
	}
	if !reflect.DeepEqual(changes, want) {
		t.Fatalf("changes = %+v, want %+v (numbered by the document's place in from)", changes, want)
	}
}

func TestCompareDocumentsReportsKubernetesDocumentsOnlyOneSideHas(t *testing.T) {
	a, b, c := kubernetesResource("A", "a", 1), kubernetesResource("B", "b", 2), kubernetesResource("C", "c", 3)

	changes, err := CompareDocuments("from.yml", []interface{}{a, b}, "to.yml", []interface{}{a, c})
	if err != nil {
		t.Fatalf("CompareDocuments returned error: %v", err)
	}
	if len(changes) != 3 || !changes[2].FileLevel {
		t.Fatalf("changes = %+v, want one removal, one addition, and the file-level order change last", changes)
	}
	byKind := map[Kind]Change{changes[0].Kind: changes[0], changes[1].Kind: changes[1]}
	if got := byKind[Added]; got.Document != 2 || !reflect.DeepEqual(got.New, c) {
		t.Errorf("addition = %+v, want document 2 added with %v", got, c)
	}
	if got := byKind[Removed]; got.Document != 2 || !reflect.DeepEqual(got.Old, b) {
		t.Errorf("removal = %+v, want document 2 removed with %v", got, b)
	}
}

func TestCompareDocumentPairsHoldsTheDecodedDocuments(t *testing.T) {
	from := []interface{}{map[string]interface{}{"a": 1}, map[string]interface{}{"b": 1}}
	to := []interface{}{map[string]interface{}{"a": 1}, map[string]interface{}{"b": 2}}

	documents, err := CompareDocumentPairs("from.yml", from, "to.yml", to)
	if err != nil {
		t.Fatalf("CompareDocumentPairs returned error: %v", err)
	}
	if len(documents) != 1 {
		t.Fatalf("documents = %+v, want only the document that differs", documents)
	}
	if got := documents[0]; got.Document != 2 || !reflect.DeepEqual(got.From, from[1]) || !reflect.DeepEqual(got.To, to[1]) || len(got.Changes) != 1 {
		t.Fatalf("document = %+v, want document 2 with its decoded values and one change", got)
	}
}

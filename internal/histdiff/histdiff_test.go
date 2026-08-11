package histdiff

import (
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
	// it's an unusual document shape; dyff represents it as a modification
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

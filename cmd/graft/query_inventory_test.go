package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"testing"

	"github.com/goccy/go-yaml"

	parity "github.com/fivetwenty-io/graft/tests/query-parity"
)

// inventoryEntry is one behavior graft jq or graft yq must have, with the
// phase that delivers it and the differential cases that exercise it.
type inventoryEntry struct {
	ID    string   `yaml:"id"`
	Area  string   `yaml:"area"`
	Phase string   `yaml:"phase"`
	Cases []string `yaml:"cases"`
}

// areaStat counts one area of the inventory. Passing counts the entries
// whose verdict is Pass, and the percentage is Passing * 100 / Total, so
// an allowlisted entry never raises it. An out-of-scope entry is counted
// only under outOfScope, so it is in no area and no percentage.
type areaStat struct {
	Total, Allowlisted, Passing int
}

// inventoryAreas lists each dialect's areas in the order the report prints
// them.
var inventoryAreas = map[string][]string{
	"jq": {"grammar", "builtin", "flag", "exit", "number", "string", "regex", "date", "math", "module", "stream", "cli"},
	"yq": {"operator", "flag", "exit", "format", "meta", "cli"},
}

// inventoryPhases lists the phases each dialect's entries may name.
var inventoryPhases = map[string][]string{
	"jq": {"J1", "J2", "J3"},
	"yq": {"Y1", "Y2"},
}

// outOfScope is the stats key and the allowlist kind for the entries that
// need a format the design leaves out.
const outOfScope = "out-of-scope"

var (
	inventoryMu    sync.Mutex
	inventoryCache = map[string][]inventoryEntry{}
	matchCache     = map[string]map[string]bool{}
)

// loadInventory reads a dialect's inventory.yaml.
func loadInventory(t *testing.T, dialect string) []inventoryEntry {
	t.Helper()
	inventoryMu.Lock()
	defer inventoryMu.Unlock()
	if entries, ok := inventoryCache[dialect]; ok {
		return entries
	}
	data, err := os.ReadFile(filepath.Join(parityRoot, dialect, "inventory.yaml")) // #nosec G304 -- the test reads its own checked-in inventory
	if err != nil {
		t.Fatal(err)
	}
	var entries []inventoryEntry
	if err := yaml.Unmarshal(data, &entries); err != nil {
		t.Fatalf("%s inventory: %v", dialect, err)
	}
	inventoryCache[dialect] = entries
	return entries
}

// caseNames returns the names of every case directory of a dialect.
func caseNames(t *testing.T, dialect string) map[string]bool {
	t.Helper()
	dirs, err := os.ReadDir(filepath.Join(parityRoot, dialect, "cases"))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, d := range dirs {
		if d.IsDir() {
			names[d.Name()] = true
		}
	}
	return names
}

func referenced(entries []inventoryEntry, name string) bool {
	for _, e := range entries {
		if slices.Contains(e.Cases, name) {
			return true
		}
	}
	return false
}

func hasEntry(entries []inventoryEntry, id string) bool {
	return slices.ContainsFunc(entries, func(e inventoryEntry) bool { return e.ID == id })
}

// parityResults replays every case of a dialect once per test binary and
// returns whether each one matched its golden.
func parityResults(t *testing.T, dialect string) map[string]bool {
	t.Helper()
	inventoryMu.Lock()
	defer inventoryMu.Unlock()
	if matched, ok := matchCache[dialect]; ok {
		return matched
	}
	results := map[string]bool{}
	cases := loadParityCases(t, dialect)
	for i := range cases {
		c := cases[i]
		matched, _ := caseMatches(t, c)
		results[c.Name] = matched
	}
	matchCache[dialect] = results
	return results
}

// allMatched reports whether every named case matched. An entry that
// names no case, or a case that does not exist, never does.
func allMatched(names []string, matched map[string]bool) bool {
	if len(names) == 0 {
		return false
	}
	for _, n := range names {
		if !matched[n] {
			return false
		}
	}
	return true
}

// inventoryVerdicts decides what each entry's outcome means at the phase.
func inventoryVerdicts(entries []inventoryEntry, allow map[string]parity.AllowEntry, matched map[string]bool, phase string) map[string]parity.Verdict {
	verdicts := map[string]parity.Verdict{}
	for _, e := range entries {
		var a *parity.AllowEntry
		if x, ok := allow[e.ID]; ok {
			a = &x
		}
		verdicts[e.ID] = parity.Evaluate(parity.Expectation{Entry: e.ID, Phase: e.Phase, Allow: a}, allMatched(e.Cases, matched), phase)
	}
	return verdicts
}

// inventoryStats counts each area and the whole inventory under "overall",
// and the out-of-scope entries on their own under outOfScope.
func inventoryStats(entries []inventoryEntry, verdicts map[string]parity.Verdict, allow map[string]parity.AllowEntry) map[string]areaStat {
	stats := map[string]areaStat{}
	for _, e := range entries {
		keys := []string{e.Area, "overall"}
		if allow[e.ID].Kind == outOfScope {
			keys = []string{outOfScope}
		}
		for _, key := range keys {
			s := stats[key]
			s.Total++
			if _, ok := allow[e.ID]; ok {
				s.Allowlisted++
			}
			if verdicts[e.ID] == parity.Pass {
				s.Passing++
			}
			stats[key] = s
		}
	}
	return stats
}

// percent is an area's passing share of its entries, and 100 for an area
// with none.
func percent(stats map[string]areaStat, area string) int {
	s := stats[area]
	if s.Total == 0 {
		return 100
	}
	return s.Passing * 100 / s.Total
}

func countKind(allow map[string]parity.AllowEntry, kind string) int {
	n := 0
	for _, a := range allow {
		if a.Kind == kind {
			n++
		}
	}
	return n
}

// checkInventoryComplete fails for an entry that is listed twice, names no
// case, names a case that does not exist or has another phase, or has an
// unknown phase or area, for a case no entry names, and for an allowlist
// line that names no entry.
func checkInventoryComplete(t *testing.T, dialect string) {
	t.Helper()
	entries := loadInventory(t, dialect)
	cases := caseNames(t, dialect)
	parityCases := map[string]parityCase{}
	pcs := loadParityCases(t, dialect)
	for i := range pcs {
		parityCases[pcs[i].Name] = pcs[i]
	}
	areas := map[string]bool{}
	for _, a := range inventoryAreas[dialect] {
		areas[a] = true
	}
	ids := map[string]bool{}
	for _, e := range entries {
		if ids[e.ID] {
			t.Errorf("entry %s is listed twice", e.ID)
		}
		ids[e.ID] = true
		if len(e.Cases) == 0 {
			t.Errorf("%s names no case", e.ID)
		}
		for _, c := range e.Cases {
			if !cases[c] {
				t.Errorf("%s names missing case %s", e.ID, c)
			} else if parityCases[c].Phase != e.Phase {
				t.Errorf("%s is %s but its case %s is %s", e.ID, e.Phase, c, parityCases[c].Phase)
			}
		}
		if !slices.Contains(inventoryPhases[dialect], e.Phase) {
			t.Errorf("%s has phase %q", e.ID, e.Phase)
		}
		if !areas[e.Area] {
			t.Errorf("%s has area %q", e.ID, e.Area)
		}
	}
	for c := range cases {
		if !referenced(entries, c) {
			t.Errorf("case %s belongs to no entry", c)
		}
	}
	for id, a := range mustReadAllowlist(t, dialect) {
		if !hasEntry(entries, id) {
			t.Errorf("allowlist names unknown entry %s", id)
		}
		if a.Kind == "pending" && a.Phase == "" {
			t.Errorf("pending entry %s names no phase", id)
		}
	}
}

// checkInventoryFloors fails for an entry that fails in its own phase or
// passes while expected to fail, for an entry the baseline lists as
// passing that no longer does, for a pass ratio below the floor, for
// departures past the cap, and for an inventory smaller than the
// baseline's. It returns the stats it computed.
func checkInventoryFloors(t *testing.T, dialect string) map[string]areaStat {
	t.Helper()
	floors := mustReadFloors(t).For(dialect)
	allow := mustReadAllowlist(t, dialect)
	base := mustReadBaseline(t, dialect)
	entries := loadInventory(t, dialect)
	verdicts := inventoryVerdicts(entries, allow, parityResults(t, dialect), floors.Phase)

	for _, e := range entries {
		switch v := verdicts[e.ID]; {
		case v == parity.UnexpectedPass:
			t.Errorf("%s passes but is expected to fail; move its phase or drop it from the allowlist", e.ID)
		case v == parity.Fail:
			t.Errorf("%s fails in its own phase", e.ID)
		case base.Passing[e.ID] && v != parity.Pass:
			t.Errorf("%s passed at the last baseline and no longer does", e.ID)
		}
	}
	stats := inventoryStats(entries, verdicts, allow)
	if pct := percent(stats, "overall"); pct < floors.Inventory {
		t.Errorf("%s inventory at %d%%, floor %d%%", dialect, pct, floors.Inventory)
	}
	if n := countKind(allow, "departure"); n*100 > stats["overall"].Total*floors.DepartureCapPercent {
		t.Errorf("%d departures exceed the %d%% cap", n, floors.DepartureCapPercent)
	}
	if len(entries) < base.Entries {
		t.Errorf("inventory shrank from %d to %d entries", base.Entries, len(entries))
	}
	return stats
}

// logInventoryPassing logs a dialect's entry count and the IDs of the
// entries that pass, one per line, for scripts/compat-baseline.sh.
func logInventoryPassing(t *testing.T, dialect string) {
	t.Helper()
	floors := mustReadFloors(t).For(dialect)
	entries := loadInventory(t, dialect)
	verdicts := inventoryVerdicts(entries, mustReadAllowlist(t, dialect), parityResults(t, dialect), floors.Phase)
	t.Logf("compat-entries: %d", len(entries))
	var ids []string
	for _, e := range entries {
		if verdicts[e.ID] == parity.Pass {
			ids = append(ids, e.ID)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		t.Logf("compat-passing: %s", id)
	}
}

// logInventoryReport logs a dialect's compatibility report, one line per
// t.Logf, for scripts/compat-report.sh to collect.
func logInventoryReport(t *testing.T, dialect string) {
	t.Helper()
	floors := mustReadFloors(t).For(dialect)
	allow := mustReadAllowlist(t, dialect)
	entries := loadInventory(t, dialect)
	verdicts := inventoryVerdicts(entries, allow, parityResults(t, dialect), floors.Phase)
	stats := inventoryStats(entries, verdicts, allow)
	for _, area := range append(slices.Clone(inventoryAreas[dialect]), "overall") {
		s, ok := stats[area]
		if !ok {
			continue
		}
		t.Logf("compat-report: %s %d %d %d %d", area, s.Total, s.Allowlisted, s.Passing, percent(stats, area))
	}
	if s, ok := stats[outOfScope]; ok {
		t.Logf("compat-report: %s %d", outOfScope, s.Total)
	}
	ids := make([]string, 0, len(allow))
	for id := range allow {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		a := allow[id]
		if a.Kind == "pending" {
			t.Logf("compat-report: allow %s pending %s %s", id, a.Phase, a.Reason)
		} else {
			t.Logf("compat-report: allow %s %s %s", id, a.Kind, a.Reason)
		}
	}
}

func TestJQInventoryIsComplete(t *testing.T) {
	checkInventoryComplete(t, "jq")
}

func TestJQInventoryMeetsFloors(t *testing.T) {
	stats := checkInventoryFloors(t, "jq")
	if floor := mustReadFloors(t).Regex; percent(stats, "regex") < floor {
		t.Errorf("jq regex at %d%%, floor %d%%", percent(stats, "regex"), floor)
	}
}

// TestJQInventoryPassing logs the IDs of the jq entries that pass.
func TestJQInventoryPassing(t *testing.T) {
	logInventoryPassing(t, "jq")
}

// TestJQInventoryReport logs the jq compatibility report.
func TestJQInventoryReport(t *testing.T) {
	logInventoryReport(t, "jq")
}

func TestYQInventoryIsComplete(t *testing.T) {
	checkInventoryComplete(t, "yq")
}

func TestYQInventoryMeetsFloors(t *testing.T) {
	checkInventoryFloors(t, "yq")
}

// TestYQInventoryPassing logs the IDs of the yq entries that pass.
func TestYQInventoryPassing(t *testing.T) {
	logInventoryPassing(t, "yq")
}

// TestYQInventoryReport logs the yq compatibility report.
func TestYQInventoryReport(t *testing.T) {
	logInventoryReport(t, "yq")
}

func TestInventoryStats(t *testing.T) {
	entries := []inventoryEntry{
		{ID: "a", Area: "grammar"}, {ID: "b", Area: "grammar"}, {ID: "c", Area: "regex"}, {ID: "d", Area: "regex"}, {ID: "e", Area: "regex"},
	}
	verdicts := map[string]parity.Verdict{"a": parity.Pass, "b": parity.ExpectedFail, "c": parity.ExpectedFail, "d": parity.ExpectedFail, "e": parity.ExpectedFail}
	allow := map[string]parity.AllowEntry{"b": {ID: "b", Kind: "departure", Reason: "r"}, "e": {ID: "e", Kind: outOfScope, Reason: "r"}}
	stats := inventoryStats(entries, verdicts, allow)
	if got, want := stats["grammar"], (areaStat{Total: 2, Allowlisted: 1, Passing: 1}); got != want {
		t.Errorf("grammar stat %+v, want %+v", got, want)
	}
	if got, want := stats["overall"], (areaStat{Total: 4, Allowlisted: 1, Passing: 1}); got != want {
		t.Errorf("overall stat %+v, want %+v", got, want)
	}
	if got, want := stats["regex"], (areaStat{Total: 2}); got != want {
		t.Errorf("regex stat %+v, want %+v", got, want)
	}
	if got, want := stats[outOfScope], (areaStat{Total: 1, Allowlisted: 1}); got != want {
		t.Errorf("out-of-scope stat %+v, want %+v", got, want)
	}
	if got := percent(stats, "overall"); got != 25 {
		t.Errorf("overall percent %d, want 25", got)
	}
	if got := percent(stats, "regex"); got != 0 {
		t.Errorf("regex percent %d, want 0", got)
	}
	if got := percent(stats, "module"); got != 100 {
		t.Errorf("empty area percent %d, want 100", got)
	}
	if fmt.Sprint(allMatched(nil, nil)) != "false" {
		t.Error("an entry with no case passed")
	}
}

func TestParityResultsAreKeyedByDialect(t *testing.T) {
	inventoryMu.Lock()
	matchCache["other"] = map[string]bool{"planted-case": true}
	inventoryMu.Unlock()
	defer func() {
		inventoryMu.Lock()
		delete(matchCache, "other")
		inventoryMu.Unlock()
	}()
	if _, leaked := parityResults(t, "jq")["planted-case"]; leaked {
		t.Fatal("jq results carry a case another dialect cached")
	}
}

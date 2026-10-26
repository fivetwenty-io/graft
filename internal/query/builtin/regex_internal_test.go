package builtin

import (
	"strconv"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/regex"
)

func TestPatternCacheKeepsTheRecentPatterns(t *testing.T) {
	c := &patternCache{entries: map[patternKey]*cachedPattern{}}
	get := func(pattern string) *regex.Pattern {
		t.Helper()
		p, err := c.get(pattern, 0, regex.Budget{})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	first, second := get("p0"), get("p1")
	for i := 2; i < patternCacheSize; i++ {
		get("p" + strconv.Itoa(i))
	}
	if get("p0") != first {
		t.Fatal("a cached pattern was compiled again")
	}
	get("one more") // evicts p1, the least recently used
	if len(c.entries) != patternCacheSize {
		t.Fatalf("the cache holds %d patterns, want %d", len(c.entries), patternCacheSize)
	}
	if get("p0") != first {
		t.Error("the most recently used pattern was evicted")
	}
	if get("p1") == second {
		t.Error("the least recently used pattern was kept")
	}
	if _, err := c.get("(", 0, regex.Budget{}); err == nil {
		t.Error("a pattern that does not compile gave no error")
	}
	if _, ok := c.entries[patternKey{"(", 0, regex.Oniguruma, regex.Budget{}}]; ok {
		t.Error("a pattern that does not compile was cached")
	}
	if p, _ := c.get("p0", regex.IgnoreCase, regex.Budget{}); p == first {
		t.Error("patterns with different flags share an entry")
	}
	if p, _ := c.get("p0", 0, regex.Budget{Steps: 10}); p == first {
		t.Error("patterns with different budgets share an entry")
	}
}

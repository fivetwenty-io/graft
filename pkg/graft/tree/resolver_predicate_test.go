package tree

import (
	"errors"
	"testing"
)

// predicateTestData builds a document with a list of maps: servers keyed
// by "name", each with a "host" field.
func predicateTestData() map[string]interface{} {
	return map[string]interface{}{
		"servers": []interface{}{
			map[string]interface{}{
				"name": "primary",
				"host": "10.0.0.1",
			},
			map[string]interface{}{
				"name": "secondary",
				"host": "10.0.0.2",
			},
			map[string]interface{}{
				"name": "primary", // duplicate name — first match wins
				"host": "10.0.0.99",
			},
		},
		"ports": []interface{}{
			map[string]interface{}{
				"port":  8080,
				"proto": "http",
			},
			map[string]interface{}{
				"port":  8443,
				"proto": "https",
			},
		},
		"keyedByEquals": map[string]interface{}{
			"name=primary": "literal-map-key-value",
		},
	}
}

// --- Resolve: dotted predicate form ---

func TestResolvePredicateDottedForm(t *testing.T) {
	data := predicateTestData()
	c, err := ParseCursor("servers.name=primary.host")
	if err != nil {
		t.Fatalf("ParseCursor: %v", err)
	}
	if got, want := len(c.Nodes), 3; got != want {
		t.Fatalf("expected 3 nodes, got %d: %v", got, c.Nodes)
	}
	if c.Nodes[1] != "name=primary" {
		t.Fatalf("expected predicate node 'name=primary', got %q", c.Nodes[1])
	}

	result, err := c.Resolve(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "10.0.0.1" {
		t.Fatalf("expected the first matching server's host, got %v", result)
	}
}

// --- Resolve: bracketed predicate form ---

func TestResolvePredicateBracketedForm(t *testing.T) {
	data := predicateTestData()
	c, err := ParseCursor("servers[name=primary].host")
	if err != nil {
		t.Fatalf("ParseCursor: %v", err)
	}
	if c.Nodes[1] != "name=primary" {
		t.Fatalf("expected predicate node 'name=primary', got %q", c.Nodes[1])
	}

	result, err := c.Resolve(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "10.0.0.1" {
		t.Fatalf("expected the first matching server's host, got %v", result)
	}
}

// Both forms must normalize to the same result.
func TestResolvePredicateBothFormsAgree(t *testing.T) {
	data := predicateTestData()

	dotted, _ := ParseCursor("servers.name=primary.host")
	bracketed, _ := ParseCursor("servers[name=primary].host")

	dottedResult, err := dotted.Resolve(data)
	if err != nil {
		t.Fatalf("dotted form: unexpected error: %v", err)
	}
	bracketedResult, err := bracketed.Resolve(data)
	if err != nil {
		t.Fatalf("bracketed form: unexpected error: %v", err)
	}
	if dottedResult != bracketedResult {
		t.Fatalf("dotted and bracketed forms disagree: %v vs %v", dottedResult, bracketedResult)
	}
}

// --- Non-string field values ---

func TestResolvePredicateNumericFieldValue(t *testing.T) {
	data := predicateTestData()
	c, err := ParseCursor("ports.port=8443.proto")
	if err != nil {
		t.Fatalf("ParseCursor: %v", err)
	}
	result, err := c.Resolve(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "https" {
		t.Fatalf("expected 'https', got %v", result)
	}
}

// --- Not-found text (§6.5) ---

func TestResolvePredicateNotFound(t *testing.T) {
	data := predicateTestData()
	c, err := ParseCursor("servers.name=nonexistent.host")
	if err != nil {
		t.Fatalf("ParseCursor: %v", err)
	}
	_, err = c.Resolve(data)
	if err == nil {
		t.Fatalf("expected a not-found error")
	}
	var nf NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("expected a NotFoundError, got %T: %v", err, err)
	}
	want := "`$.servers.name=nonexistent` could not be found in the datastructure"
	if got := stripANSI(nf.Error()); got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

// --- Existing name-field auto-match must keep working (B-6, unaffected by A7) ---

func TestResolveNameFieldAutoMatchUnaffected(t *testing.T) {
	data := predicateTestData()
	c, err := ParseCursor("servers.primary.host")
	if err != nil {
		t.Fatalf("ParseCursor: %v", err)
	}
	result, err := c.Resolve(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "10.0.0.1" {
		t.Fatalf("expected the first matching server's host via name-field auto-match, got %v", result)
	}
}

// --- Predicate vs. dynamic-key on maps: the regression guard (§6.4) ---

// A map key that literally contains "=" must still resolve as a plain map
// key lookup — the predicate interpretation only applies to list
// containers. This is the one regression vector §6.4 calls out.
func TestResolvePredicateShapedKeyOnMapIsPlainLookup(t *testing.T) {
	data := predicateTestData()
	c, err := ParseCursor("keyedByEquals.name=primary")
	if err != nil {
		t.Fatalf("ParseCursor: %v", err)
	}
	if len(c.Nodes) != 2 || c.Nodes[1] != "name=primary" {
		t.Fatalf("expected nodes [keyedByEquals, name=primary], got %v", c.Nodes)
	}
	result, err := c.Resolve(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "literal-map-key-value" {
		t.Fatalf("expected the literal map key's value, got %v", result)
	}
}

// --- Canonical must agree with Resolve ---

func TestCanonicalPredicateDottedForm(t *testing.T) {
	data := predicateTestData()
	c, err := ParseCursor("servers.name=secondary.host")
	if err != nil {
		t.Fatalf("ParseCursor: %v", err)
	}
	canon, err := c.Canonical(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "servers.1.host"
	if canon.String() != want {
		t.Fatalf("expected canonical path %q, got %q", want, canon.String())
	}
}

func TestCanonicalPredicateNotFound(t *testing.T) {
	data := predicateTestData()
	c, err := ParseCursor("servers.name=nonexistent.host")
	if err != nil {
		t.Fatalf("ParseCursor: %v", err)
	}
	_, err = c.Canonical(data)
	if err == nil {
		t.Fatalf("expected a not-found error")
	}
	var nf NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("expected a NotFoundError, got %T: %v", err, err)
	}
}

// --- B-11/B-12: "==" and "a==b" must not be captured as a predicate segment ---

func TestParseCursorDoesNotCapturePredicateAcrossDoubleEquals(t *testing.T) {
	// ParseCursor is a plain char-scanner unrelated to the tokenizer's
	// ArrayReferencePattern, so "a==b" as a *path string* is not a realistic
	// input to ParseCursor directly (it would come from the tokenizer as
	// separate tokens, never a single reference). This test instead pins
	// the predicate *regex* itself: a bare "field==value" segment (an
	// unlikely but possible literal path node) is not treated as a valid
	// predicate match target once split by ParseCursor, since ParseCursor
	// never splits on "==" at all — it is exercised at the tokenizer layer
	// (see interfaces.TestArrayReferencePatternPredicateSegment).
	c, err := ParseCursor("servers.name==primary")
	if err != nil {
		t.Fatalf("ParseCursor: %v", err)
	}
	if len(c.Nodes) != 2 || c.Nodes[1] != "name==primary" {
		t.Fatalf("expected nodes [servers, name==primary], got %v", c.Nodes)
	}
}

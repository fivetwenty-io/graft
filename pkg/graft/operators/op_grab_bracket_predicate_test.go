package operators

import "testing"

// TestIsDynamicBracketNodeExcludesPredicateSegments pins the spec cluster A7
// §6.4 regression guard: a bracketed segment shaped like "field=value" must
// not be claimed as a dynamic key reference, so the resolver's predicate
// matcher gets a chance to run instead of resolveGrabDynamicBrackets trying
// (and failing) to resolve "name=primary" as its own path from the document
// root.
func TestIsDynamicBracketNodeExcludesPredicateSegments(t *testing.T) {
	cases := []struct {
		name string
		node string
		want bool
	}{
		{"predicate segment is not dynamic", "name=primary", false},
		{"predicate segment with numeric value is not dynamic", "port=8080", false},
		{"plain identifier is still dynamic", "somekey", true},
		{"env var reference is still not dynamic", "$SOME_VAR", false},
		{"integer index is still not dynamic", "3", false},
		{"empty segment is still not dynamic", "", false},
		{"dotted-path-shaped key is still dynamic (not a predicate)", "meta.other", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isDynamicBracketNode(tc.node)
			if got != tc.want {
				t.Fatalf("isDynamicBracketNode(%q) = %v, want %v", tc.node, got, tc.want)
			}
		})
	}
}

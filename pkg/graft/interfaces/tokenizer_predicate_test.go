package interfaces

import "testing"

// TestArrayReferencePatternPredicateSegment pins the spec cluster A7 §6.3
// tokenizer change: a dotted segment may carry an inline "field=value"
// predicate, and the doubled-'=' / quote-after-'=' cases must still stop at
// the ordinary "==" comparison boundary rather than being captured into the
// reference.
func TestArrayReferencePatternPredicateSegment(t *testing.T) {
	pattern := &ArrayReferencePattern{}

	cases := []struct {
		name       string
		input      string
		wantMatch  bool
		wantLength int
	}{
		{
			name:       "dotted predicate segment is captured whole",
			input:      "servers.name=primary.host",
			wantMatch:  true,
			wantLength: len("servers.name=primary.host"),
		},
		{
			name:       "predicate with numeric value",
			input:      "servers.port=8080.value",
			wantMatch:  true,
			wantLength: len("servers.port=8080.value"),
		},
		{
			name:      "doubled equals stops before capturing the comparison",
			input:     "meta.a==b",
			wantMatch: true,
			// Stops right after "a" (segment boundary); the doubled '=' is
			// left for ordinary "==" tokenization.
			wantLength: len("meta.a"),
		},
		{
			name:       "doubled equals with a quoted right-hand side still stops at the segment boundary",
			input:      `meta.env=="prod"`,
			wantMatch:  true,
			wantLength: len("meta.env"),
		},
		{
			name:       "single equals not followed by an identifier-start or digit is not captured",
			input:      "meta.a=.b",
			wantMatch:  true,
			wantLength: len("meta.a"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			matched, length := pattern.Match(tc.input, 0)
			if matched != tc.wantMatch {
				t.Fatalf("expected matched=%v, got %v", tc.wantMatch, matched)
			}
			if length != tc.wantLength {
				t.Fatalf("expected length=%d (%q), got %d (%q)",
					tc.wantLength, tc.input[:tc.wantLength], length, safeSlice(tc.input, length))
			}
		})
	}
}

func safeSlice(s string, n int) string {
	if n < 0 || n > len(s) {
		return s
	}
	return s[:n]
}

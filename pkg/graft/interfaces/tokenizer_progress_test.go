package interfaces

import "testing"

// TestScanLoneOperatorCharsAdvanceAndTerminate pins the tokenizer livelock
// fix for the four non-advancing arms (scanComplexOperator's lone '=', '&',
// '|' and scanDollarSign's lone '$'). Each must consume exactly one rune,
// emit one TokenInvalid, then reach TokenEOF. Before the fix, NextToken
// returned TokenInvalid forever without moving the tokenizer's position.
func TestScanLoneOperatorCharsAdvanceAndTerminate(t *testing.T) {
	cases := []struct {
		name  string
		input string
		opts  TokenizerOptions
	}{
		{
			name:  "lone equals",
			input: "=",
			opts:  TokenizerOptions{RecognizeReferencePaths: true, AllowEnvironmentVars: true, TrackPositions: true},
		},
		{
			name:  "lone ampersand",
			input: "&",
			opts:  TokenizerOptions{RecognizeReferencePaths: true, AllowEnvironmentVars: true, TrackPositions: true},
		},
		{
			name:  "lone pipe",
			input: "|",
			opts:  TokenizerOptions{RecognizeReferencePaths: true, AllowEnvironmentVars: true, TrackPositions: true},
		},
		{
			name:  "lone dollar, env vars disallowed",
			input: "$",
			opts:  TokenizerOptions{RecognizeReferencePaths: true, AllowEnvironmentVars: false, TrackPositions: true},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tok := NewAdvancedTokenizer(tc.input, tc.opts)

			startPos := tok.Position()

			first := tok.NextToken()
			if first == nil {
				t.Fatalf("NextToken returned nil")
			}
			if first.Type != TokenInvalid {
				t.Fatalf("expected TokenInvalid, got %v", first.Type)
			}
			if tok.Position() <= startPos {
				t.Fatalf("tokenizer made no progress: start=%d after=%d", startPos, tok.Position())
			}

			second := tok.NextToken()
			if second == nil {
				t.Fatalf("second NextToken returned nil")
			}
			if second.Type != TokenEOF {
				t.Fatalf("expected TokenEOF after the invalid token, got %v (input %q left unconsumed)", second.Type, tc.input)
			}
		})
	}
}

// TestScanLoneOperatorCharsDoNotHang bounds the number of tokens produced
// for a short pathological input, guarding against a regression that makes
// progress too slowly rather than not at all.
func TestScanLoneOperatorCharsDoNotHang(t *testing.T) {
	opts := TokenizerOptions{RecognizeReferencePaths: true, AllowEnvironmentVars: true, TrackPositions: true}
	tok := NewAdvancedTokenizer("a=b & c | d", opts)

	count := 0
	const maxTokens = 100 // input is 11 bytes; any correct tokenizer stays far below this
	for tok.HasMore() {
		count++
		if count > maxTokens {
			t.Fatalf("tokenizer produced more than %d tokens for an 11-byte input; likely livelocked", maxTokens)
		}
		next := tok.NextToken()
		if next.Type == TokenEOF {
			break
		}
	}
}

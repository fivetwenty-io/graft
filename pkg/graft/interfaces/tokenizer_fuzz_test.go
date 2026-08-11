package interfaces

import (
	"testing"
	"unicode/utf8"
)

// FuzzAdvancedTokenizerProgress is the fuzz target spec §9.4 calls for: the
// repo's existing fuzz targets never construct a tokenizer, and a
// non-advancing scanner arm is a hang rather than a panic, so "must not
// panic" would never have caught the livelock this stage fixed.
//
// The invariant asserted is the one Parser.tokenize relies on: every token
// before TokenEOF must strictly advance the tokenizer's position. A
// violation is what would make Parser.tokenize's progress assertion fire —
// so this target doubles as a false-positive hunt for that assertion,
// because any legitimate zero-width token would show up here first.
func FuzzAdvancedTokenizerProgress(f *testing.F) {
	seeds := []string{
		"=", "&", "|", "$", "a=b", "a==b", "((", "))",
		"$.", "!", "!=", "<", "<=", ">", ">=", "&&", "||",
		"(( grab a ))", "(( 1 + 2 ))", `(( vault@prod "p:k" ))`,
		"(( grab servers.name=primary.host ))",
		"(( grab servers[name=primary].host ))",
		"meta.a=.b", `meta.env=="prod"`, "a.b=", "a.b=-1",
		"", " ", "\t\n", "\"unterminated", "'", "\\", "@", ",", ".", "?", ":",
		"日本語", "a\x00b",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		// The tokenizer indexes runes; skip inputs that are not valid UTF-8
		// rather than pinning behavior the production path never sees (all
		// input reaches it as a decoded YAML scalar).
		if !utf8.ValidString(input) {
			t.Skip()
		}

		tok := NewAdvancedTokenizer(input, TokenizerOptions{
			RecognizeReferencePaths: true,
			AllowEnvironmentVars:    true,
			TrackPositions:          true,
		})

		// Any correct tokenizer emits at most one token per byte, plus EOF.
		maxTokens := len(input) + 2

		prev := tok.Position()
		for count := 0; tok.HasMore(); count++ {
			if count > maxTokens {
				t.Fatalf("produced more than %d tokens for a %d-byte input %q; livelock",
					maxTokens, len(input), input)
			}

			next := tok.NextToken()
			if next == nil {
				t.Fatalf("NextToken returned nil for input %q", input)
			}
			if next.Type == TokenEOF {
				break
			}

			pos := tok.Position()
			if pos <= prev {
				t.Fatalf("token %v (%q) did not advance the position (%d -> %d) for input %q; "+
					"this is what makes Parser.tokenize's progress assertion fire",
					next.Type, next.Literal, prev, pos, input)
			}
			prev = pos
		}
	})
}

package graft

import (
	"testing"

	"github.com/goccy/go-yaml"
)

// singleQuoteCorpus mixes the common manifest-string shapes the byte
// prefilter must clear without a parse, and the syntax-heavy shapes that
// must keep their full-parse classification.
var singleQuoteCorpus = []string{
	// plain words and identifiers
	"router", "cloud_controller", "cf-smoke-tests", "diego cell",
	"a b c d e", "10.244.0.34", "3.14.15", "v1.32.2",
	"path/to/thing", "a+b=c", "x86_64",
	// URLs and colon shapes
	"https://example.com/path", "host:8443", "a:b:c",
	"key: value", "trailing:",
	// type lookalikes (parse fine, to another type)
	"123", "1.0", "yes", "no", "true", "null", "~",
	// numeric-overflow shapes: must not be cleared if goccy errors on them
	"99999999999999999999999999999999999999", "1e999999999", "0x9999999999999999999",
	// spruce single-quote class: fails a bare-scalar parse
	"*.uaa.((cert))", "*star", "&anchor", "%percent", "[bracket",
	"{brace", "'quote", "\"dquote", "!tag", "|pipe", ">fold",
	"@at", "`tick", "- dash entry", "? complex",
	"a, b, c", "comma,separated", "#hash", "a #comment",
	// escape-needing strings (double-quote class)
	"line\nbreak", "tab\there", "bell\x07",
	// misc
	"", " leading space", "trailing space ", "-flag", "--flag",
	"5d ago", "user@host", "半角カナ", "emoji 🎉",
}

func TestPlainScalarParseCannotFailIsSound(t *testing.T) {
	// The prefilter may only clear strings whose bare-scalar parse
	// cannot fail; a false "safe" would silently change quoting class.
	for _, s := range singleQuoteCorpus {
		if plainScalarParseCannotFail(s) {
			var reparsed interface{}
			if err := yaml.Unmarshal([]byte(s), &reparsed); err != nil {
				t.Errorf("prefilter cleared %q but bare-scalar parse fails: %v", s, err)
			}
		}
	}
}

func TestPlainScalarPrefilterCoversCommonShapes(t *testing.T) {
	// The point of the prefilter is that ordinary manifest strings skip
	// the parse; these must all be cleared.
	common := []string{
		"router", "cloud_controller", "cf-smoke-tests", "diego cell",
		"10.244.0.34", "v1.32.2", "path/to/thing", "x86_64",
		"https://example.com/path", "host:8443", "user@host", "123", "1.0",
	}
	for _, s := range common {
		if !plainScalarParseCannotFail(s) {
			t.Errorf("prefilter should clear common shape %q without a parse", s)
		}
	}
}

func TestPrefersSingleQuoteMatchesFullParse(t *testing.T) {
	// With the prefilter in place, prefersSingleQuote must classify the
	// whole corpus exactly as the unfiltered reference does.
	for _, s := range singleQuoteCorpus {
		got := prefersSingleQuote(s)
		want := prefersSingleQuoteReference(s)
		if got != want {
			t.Errorf("prefersSingleQuote(%q) = %v, reference says %v", s, got, want)
		}
	}
}

// prefersSingleQuoteReference is the pre-prefilter implementation, kept
// verbatim as the behavioral oracle.
func prefersSingleQuoteReference(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	var reparsed interface{}
	return yaml.Unmarshal([]byte(s), &reparsed) != nil
}

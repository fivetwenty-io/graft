package graft

import (
	"bytes"
	"testing"
)

// TestMarshalYAML_TabStringsRoundTrip checks that strings holding a tab
// survive a second merge: the merged output parses back to the same
// strings and marshals to the same bytes. Before spruce's escape rules
// were ported, a lone tab was written raw and read back as null, and a
// value opening with a tab was not valid YAML at all.
func TestMarshalYAML_TabStringsRoundTrip(t *testing.T) {
	engine := NewDefaultEngine()
	for _, s := range []string{"\t", "a\t", "\ta", "\t\"", "\t'", "a\tb", " \t "} {
		for _, data := range []map[string]interface{}{
			{"k": s},
			{s: "v"},
			{"l": []interface{}{s}},
		} {
			first, err := MarshalYAML(data)
			if err != nil {
				t.Fatalf("MarshalYAML(%q): %v", s, err)
			}
			doc, err := engine.ParseYAML(first)
			if err != nil {
				t.Fatalf("re-parsing %q failed: %v\noutput:\n%s", s, err, first)
			}
			second, err := MarshalYAML(doc.RawData())
			if err != nil {
				t.Fatalf("second MarshalYAML(%q): %v", s, err)
			}
			if !bytes.Equal(second, first) {
				t.Errorf("%q did not round-trip:\nfirst  %q\nsecond %q", s, first, second)
			}
		}
	}
}

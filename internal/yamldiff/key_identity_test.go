package yamldiff

import (
	"testing"

	"github.com/fivetwenty-io/graft/internal/yamlnode"
)

// TestCompareKeyIdentityIncludesType checks that a key written as a
// bool, null, or int is a different key from the string with the same
// spelling, so changing one of the pair reports one change.
func TestCompareKeyIdentityIncludesType(t *testing.T) {
	cases := map[string][2]string{
		"bool": {"true: a\n\"true\": b\n", "true: a\n\"true\": c\n"},
		"null": {"null: a\n\"null\": b\n", "null: a\n\"null\": c\n"},
		"int":  {"1: a\n\"1\": b\n", "1: a\n\"1\": c\n"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			parse := func(s string) InputFile {
				docs, err := yamlnode.Parse([]byte(s))
				if err != nil {
					t.Fatal(err)
				}
				return InputFile{Documents: docs}
			}
			report, err := CompareInputFiles(parse(c[0]), parse(c[1]))
			if err != nil {
				t.Fatal(err)
			}
			var details []Detail
			for _, d := range report.Diffs {
				details = append(details, d.Details...)
			}
			if len(details) != 1 {
				t.Fatalf("got %d changes, want 1: %+v", len(details), details)
			}
			d := details[0]
			if d.Kind != MODIFICATION || d.From.Value != "b" ||
				d.To.Value != "c" {
				t.Errorf("got %+v, want b changed to c", d)
			}
		})
	}
}

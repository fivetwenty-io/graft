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

func diffDetails(t *testing.T, from, to string) []Detail {
	t.Helper()
	a, err := ParseDocuments([]byte(from))
	if err != nil {
		t.Fatal(err)
	}
	b, err := ParseDocuments([]byte(to))
	if err != nil {
		t.Fatal(err)
	}
	report, err := CompareInputFiles(InputFile{Documents: a},
		InputFile{Documents: b})
	if err != nil {
		t.Fatal(err)
	}
	var details []Detail
	for _, d := range report.Diffs {
		details = append(details, d.Details...)
	}
	return details
}

// TestCompareKeysPairByTextWhenTypesDiffer checks that a JSON string key
// and a YAML int key with the same text are one key, so a rendered JSON
// document diffs clean against its YAML source.
func TestCompareKeysPairByTextWhenTypesDiffer(t *testing.T) {
	for _, key := range []string{"1", "true", "null"} {
		got := diffDetails(t, `{"`+key+`":"a"}`, key+": a\n")
		if len(got) != 0 {
			t.Errorf("%s: got %d changes, want none", key, len(got))
		}
	}
	// The string key pairs with the string key, and the int key has no
	// partner by text, so it is an addition.
	got := diffDetails(t, `{"1":"a"}`, "1: a\n\"1\": b\n")
	if len(got) != 2 || got[0].Kind != ADDITION ||
		got[1].Kind != MODIFICATION {
		t.Errorf("got %+v, want an addition and a modification", got)
	}
}

// TestCompareStringKeysUnchanged checks a map of string keys, where the
// pairing is by text as it always was.
func TestCompareStringKeysUnchanged(t *testing.T) {
	got := diffDetails(t, "a: 1\nb: 2\nc: 3\n", "a: 1\nb: 5\nd: 3\n")
	if len(got) != 3 {
		t.Fatalf("got %d changes, want 3: %+v", len(got), got)
	}
	kinds := []DetailKind{REMOVAL, ADDITION, MODIFICATION}
	for i, d := range got {
		if d.Kind != kinds[i] {
			t.Errorf("change %d is %v, want %v", i, d.Kind, kinds[i])
		}
	}
}

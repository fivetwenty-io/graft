package yamlnode

import (
	"strings"
	"testing"
)

// TestParseBytesRepeatedKeys pins which repeated keys ParseBytes refuses
// and where. Each want is the first line of the error, or "" when the
// parse succeeds. The keys goccy's own check compared by text compare
// the same way, apart from a null key beside a string key, which
// compares by the key a map with string keys holds for it.
func TestParseBytesRepeatedKeys(t *testing.T) {
	cases := []struct{ doc, want string }{
		{"a: 1\na: 2\n", "[2:1] mapping key already defined at [1:1]"},
		{"- a: 1\n  a: 2\n", "[2:3] mapping key already defined at [1:3]"},
		{"{a: 1, a: 2}\n", "[1:8] mapping key already defined at [1:2]"},
		{"{? a, ? a}\n", "[1:7] mapping key already defined at [1:2]"},
		{"a: &x 1\n*x : 1\n*x : 2\n", "[3:1] mapping key already defined at [2:1]"},
		{"<<: {a: 1}\n<<: {b: 1}\n", ""},
		{"a: 1\n---\nb: 1\nb: 2\n", "[4:1] mapping key already defined at [3:1]"},
		{"~: 1\n~: 2\n", "[2:1] mapping key already defined at [1:1]"},
		{"~: 1\n!!str null: 2\n", "[2:1] mapping key already defined at [1:1]"},
		{"!!str null: 1\n~: 2\n", "[2:1] mapping key already defined at [1:1]"},
		{"\"null\": 1\n~: 2\n", "[2:1] mapping key already defined at [1:1]"},
		{"~: 1\nnull: 2\n!!str null: 3\n", "[3:1] mapping key already defined at [2:1]"},
		{"!<tag:yaml.org,2002:null> x: 1\n\"null\": 2\n", "[2:1] mapping key already defined at [1:1]"},
		{"{a, a}\n", ""},
		{"{a, a: 1}\n", ""},
		{"null: 1\n~: 2\n", ""},
		{"~: 1\n!!str ~: 2\n", ""},
		{"\"~\": 1\n~: 2\n", ""},
		{"!foo ~: 1\n~: 2\n", ""},
		{"x:\n  a: 1\ny:\n  a: 2\n", ""},
		{"a.b: 1\na:\n  b: 2\n", ""},
	}
	for _, c := range cases {
		t.Run(c.doc, func(t *testing.T) {
			_, err := ParseBytes([]byte(c.doc), 0)
			got := ""
			if err != nil {
				got, _, _ = strings.Cut(err.Error(), "\n")
			}
			if got != c.want {
				t.Fatalf("ParseBytes = %q, want %q", got, c.want)
			}
		})
	}
}

// TestParseBytesWithDuplicates pins that the duplicate-allowing parse
// accepts what ParseBytes refuses for a repeated key.
func TestParseBytesWithDuplicates(t *testing.T) {
	for _, doc := range []string{"a: 1\na: 2\n", "~: 1\n!!str null: 2\n"} {
		if _, err := ParseBytesWithDuplicates([]byte(doc), 0); err != nil {
			t.Errorf("%q: %v", doc, err)
		}
	}
}

func TestKeySet(t *testing.T) {
	var s KeySet[int]
	steps := []struct {
		id     KeyID
		first  int
		repeat bool
	}{
		{KeyID{Text: "~", Null: true}, 0, false},
		{KeyID{Text: "null", Null: true}, 1, false},
		{KeyID{Text: "~"}, 2, false},
		{KeyID{Text: "null"}, 1, true},
		{KeyID{Text: "~", Null: true}, 0, true},
		{KeyID{Text: "a"}, 5, false},
		{KeyID{Text: "a"}, 5, true},
	}
	for i, st := range steps {
		first, repeat := s.Add(st.id, i)
		if repeat != st.repeat || repeat && first != st.first {
			t.Errorf("step %d: Add(%+v) = %d, %v, want %d, %v", i, st.id, first, repeat, st.first, st.repeat)
		}
	}
	if got := (KeyID{Text: "~", Null: true}).Slot(); got != "null" {
		t.Errorf("Slot of a null key = %q", got)
	}
}

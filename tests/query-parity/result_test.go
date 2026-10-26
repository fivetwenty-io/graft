package parity

import (
	"strings"
	"testing"
)

func wantEqual[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestEvaluateVerdicts(t *testing.T) {
	wantEqual(t, Evaluate(Expectation{Phase: "J1"}, true, "J1"), Pass)
	wantEqual(t, Evaluate(Expectation{Phase: "J1"}, false, "J1"), Fail)
	wantEqual(t, Evaluate(Expectation{Phase: "J2"}, false, "J1"), ExpectedFail)
	wantEqual(t, Evaluate(Expectation{Phase: "J2"}, true, "J1"), UnexpectedPass)
	wantEqual(t, Evaluate(Expectation{Phase: "J1", Allow: &AllowEntry{Kind: "departure"}}, false, "J3"), ExpectedFail)
	wantEqual(t, Evaluate(Expectation{Phase: "J1", Allow: &AllowEntry{Kind: "pending", Phase: "J2"}}, true, "J1"), UnexpectedPass)
	if !PhaseAfter("Y1", "J3") || PhaseAfter("J1", "J1") {
		t.Fatal("phase order")
	}
}

func TestReadAllowlist(t *testing.T) {
	text := "# comment\n\nfloat-literals departure exact decimal\nseq-raw-string  pending J2   --seq arrives\nxml-output out-of-scope format out of scope\n"
	got, err := ReadAllowlist(strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, len(got), 3)
	wantEqual(t, got["float-literals"], AllowEntry{ID: "float-literals", Kind: "departure", Reason: "exact decimal"})
	wantEqual(t, got["seq-raw-string"], AllowEntry{ID: "seq-raw-string", Kind: "pending", Phase: "J2", Reason: "--seq arrives"})
	wantEqual(t, got["xml-output"], AllowEntry{ID: "xml-output", Kind: "out-of-scope", Reason: "format out of scope"})

	for _, bad := range []string{
		"x pending reason\n",
		"x pending J9 reason\n",
		"x departure\n",
		"x out-of-scope\n",
		"x other J2 reason\n",
		"x departure a\nx departure b\n",
	} {
		if _, err := ReadAllowlist(strings.NewReader(bad)); err == nil {
			t.Errorf("allowlist %q is accepted", bad)
		}
	}

	empty, err := ReadAllowlist(strings.NewReader(""))
	wantEqual(t, err == nil && len(empty) == 0, true)
}

func TestReadFloors(t *testing.T) {
	text := "# floors\nJQ_PHASE=J2\nJQ_INVENTORY_FLOOR=70\nJQ_REGEX_FLOOR=\"90\"\nJQ_DEPARTURE_CAP=3\nYQ_PHASE=Y1\nYQ_INVENTORY_FLOOR=50\n"
	f, err := ReadFloors(strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, f.Phase, "J2")
	wantEqual(t, f.Inventory, 70)
	wantEqual(t, f.Regex, 90)
	wantEqual(t, f.DepartureCapPercent, 3)
	wantEqual(t, f.Values["YQ_INVENTORY_FLOOR"], 50)
	wantEqual(t, f.For("jq"), DialectFloors{Phase: "J2", Inventory: 70, DepartureCapPercent: 3})
	wantEqual(t, f.For("yq"), DialectFloors{Phase: "Y1", Inventory: 50})

	yq, err := ReadFloors(strings.NewReader("YQ_PHASE=Y2\nYQ_DEPARTURE_CAP=2\n"))
	wantEqual(t, err == nil && yq.Phase == "J1", true)
	wantEqual(t, yq.For("yq"), DialectFloors{Phase: "Y2", DepartureCapPercent: 2})

	first, err := ReadFloors(strings.NewReader(""))
	wantEqual(t, err == nil && first.Phase == "J1", true)
	wantEqual(t, first.For("yq").Phase, "Y1")

	for _, bad := range []string{"JQ_PHASE=J9\n", "YQ_PHASE=Y9\n", "JQ_INVENTORY_FLOOR=high\n", "JQ_REGEX_FLOOR=101\n", "nokey\n"} {
		if _, err := ReadFloors(strings.NewReader(bad)); err == nil {
			t.Errorf("floors %q are accepted", bad)
		}
	}
}

func TestReadBaseline(t *testing.T) {
	b, err := ReadBaseline(strings.NewReader("# baseline\nentries 3\na\nb\n"))
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, b.Entries, 3)
	wantEqual(t, len(b.Passing), 2)
	wantEqual(t, b.Passing["a"] && b.Passing["b"] && !b.Passing["c"], true)

	zero, err := ReadBaseline(strings.NewReader("entries 0\n"))
	wantEqual(t, err == nil && zero.Entries == 0 && len(zero.Passing) == 0, true)

	for _, bad := range []string{"", "a\nentries 1\n", "entries many\n"} {
		if _, err := ReadBaseline(strings.NewReader(bad)); err == nil {
			t.Errorf("baseline %q is accepted", bad)
		}
	}
}

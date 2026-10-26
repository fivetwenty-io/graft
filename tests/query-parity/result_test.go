package parity

import (
	"os"
	"path/filepath"
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
	path := filepath.Join(t.TempDir(), "allowlist.txt")
	text := "# comment\n\nfloat-literals\tdeparture\texact decimal\nseq-raw-string\tpending\tJ2\t--seq\n"
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadAllowlist(path)
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, len(got), 2)
	wantEqual(t, got["float-literals"], AllowEntry{Kind: "departure", Reason: "exact decimal"})
	wantEqual(t, got["seq-raw-string"], AllowEntry{Kind: "pending", Phase: "J2", Reason: "--seq"})

	if err := os.WriteFile(path, []byte("x\tpending\treason\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadAllowlist(path); err == nil {
		t.Fatal("a pending entry with no phase is accepted")
	}

	missing, err := ReadAllowlist(filepath.Join(t.TempDir(), "absent"))
	wantEqual(t, err == nil && len(missing) == 0, true)
}

func TestReadFloors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "compat-floors.env")
	if err := os.WriteFile(path, []byte("JQ_PHASE=J2\nYQ_PHASE=\"Y1\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	jq, err := ReadFloors(path, "jq")
	wantEqual(t, jq, "J2")
	wantEqual(t, err == nil, true)
	yq, _ := ReadFloors(path, "yq")
	wantEqual(t, yq, "Y1")

	first, err := ReadFloors(filepath.Join(t.TempDir(), "absent"), "jq")
	wantEqual(t, first, "J1")
	wantEqual(t, err == nil, true)
}

// Package parity decides what each differential case in tests/query-parity
// is expected to do at the current phase, and reads the allowlist and the
// compatibility floors that decision depends on. It holds no case data and
// runs no command, so cmd/graft/query_parity_test.go replays the cases and
// asks this package what the outcome means.
package parity

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Verdict is what a case's outcome means at the current phase.
type Verdict int

const (
	// Pass is a case that was required to match and did.
	Pass Verdict = iota
	// Fail is a case that was required to match and did not.
	Fail
	// ExpectedFail is a case that does not match and is not yet required
	// to, because its phase is later or it is allowlisted.
	ExpectedFail
	// UnexpectedPass is a case that matches though it was expected not
	// to, which means the inventory is out of date.
	UnexpectedPass
)

func (v Verdict) String() string {
	switch v {
	case Pass:
		return "Pass"
	case Fail:
		return "Fail"
	case ExpectedFail:
		return "ExpectedFail"
	case UnexpectedPass:
		return "UnexpectedPass"
	}
	return fmt.Sprintf("Verdict(%d)", int(v))
}

// Observed is what one run of a case produced, in the terms the goldens
// record: stdout, the exit code, whether anything reached stderr, the
// stderr bytes for a case that compares them, and the contents of every
// file the run left behind.
type Observed struct {
	Stdout     string
	Exit       int
	StderrFlag bool
	Stderr     string
	Files      map[string][]byte
}

// AllowEntry is one line of an allowlist. Kind is "departure" for an
// authorized permanent exception, or "pending" for work a later phase
// owes, which names that phase.
type AllowEntry struct {
	Kind   string
	Phase  string
	Reason string
}

// Expectation is what is known about a case before it runs. Phase is the
// phase the case is required from, and Allow is its allowlist entry, or
// nil when it has none.
type Expectation struct {
	Entry string
	Phase string
	Allow *AllowEntry
}

var phaseOrder = map[string]int{"J1": 1, "J2": 2, "J3": 3, "Y1": 4, "Y2": 5}

// PhaseAfter reports whether phase a comes after phase b, in the order
// J1, J2, J3, Y1, Y2. A phase it does not know is never after anything.
func PhaseAfter(a, b string) bool {
	return phaseOrder[a] > phaseOrder[b]
}

// Evaluate decides what a case's outcome means. A case whose phase is
// later than currentPhase, or that is allowlisted, is expected to fail, so
// failing is ExpectedFail and matching is UnexpectedPass. Every other
// case must match.
func Evaluate(exp Expectation, matched bool, currentPhase string) Verdict {
	expectedToFail := PhaseAfter(exp.Phase, currentPhase) || exp.Allow != nil
	switch {
	case expectedToFail && matched:
		return UnexpectedPass
	case expectedToFail:
		return ExpectedFail
	case matched:
		return Pass
	}
	return Fail
}

// ReadAllowlist reads an allowlist file, which holds one tab-separated
// entry per line. A departure line is NAME, "departure", and the reason.
// A pending line is NAME, "pending", the phase that owes the work, and the
// reason. Blank lines and lines starting with # are skipped, and a missing
// file is an empty allowlist.
func ReadAllowlist(path string) (map[string]AllowEntry, error) {
	f, err := os.Open(path) // #nosec G304 -- the harness names its own allowlist
	if os.IsNotExist(err) {
		return map[string]AllowEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	entries := map[string]AllowEntry{}
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		var e AllowEntry
		switch {
		case len(fields) == 3 && fields[1] == "departure":
			e = AllowEntry{Kind: "departure", Reason: fields[2]}
		case len(fields) == 4 && fields[1] == "pending" && phaseOrder[fields[2]] != 0:
			e = AllowEntry{Kind: "pending", Phase: fields[2], Reason: fields[3]}
		default:
			return nil, fmt.Errorf("%s:%d: want NAME<TAB>departure<TAB>REASON or NAME<TAB>pending<TAB>PHASE<TAB>REASON", path, n)
		}
		if _, dup := entries[fields[0]]; dup {
			return nil, fmt.Errorf("%s:%d: %s is listed twice", path, n, fields[0])
		}
		entries[fields[0]] = e
	}
	return entries, sc.Err()
}

// ReadFloors returns the phase the compatibility floors file names for a
// dialect, which is JQ_PHASE for jq and YQ_PHASE for yq. A missing file or
// key means the first phase of the dialect.
func ReadFloors(path, dialect string) (phase string, err error) {
	key, phase := "JQ_PHASE", "J1"
	if dialect == "yq" {
		key, phase = "YQ_PHASE", "Y1"
	}
	data, err := os.ReadFile(path) // #nosec G304 -- the harness names its own floors file
	if os.IsNotExist(err) {
		return phase, nil
	}
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok && k == key {
			phase = strings.Trim(v, `"' `)
		}
	}
	if phaseOrder[phase] == 0 {
		return "", fmt.Errorf("%s: %s names an unknown phase %q", path, key, phase)
	}
	return phase, nil
}

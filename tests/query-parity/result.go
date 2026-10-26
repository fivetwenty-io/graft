// Package parity decides what each differential case in tests/query-parity
// is expected to do at the current phase, and reads the allowlist, the
// compatibility floors, and the baseline that decision depends on. It
// holds no case data and runs no command, so cmd/graft/query_parity_test.go
// replays the cases and asks this package what the outcome means.
package parity

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
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

// AllowEntry is one line of an allowlist. ID names an inventory entry.
// Kind is "departure" for an authorized permanent exception, or "pending"
// for work a later phase owes, which names that phase.
type AllowEntry struct {
	ID     string
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

// ReadAllowlist reads an allowlist, which holds one entry per line in the
// form ID kind [phase] reason..., separated by white space. Kind is
// "departure" or "pending", and a pending line must name its phase between
// the kind and the reason. Blank lines and lines starting with # are
// skipped.
func ReadAllowlist(r io.Reader) (map[string]AllowEntry, error) {
	entries := map[string]AllowEntry{}
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		var e AllowEntry
		switch {
		case len(fields) >= 3 && fields[1] == "departure":
			e = AllowEntry{ID: fields[0], Kind: "departure", Reason: strings.Join(fields[2:], " ")}
		case len(fields) >= 4 && fields[1] == "pending" && phaseOrder[fields[2]] != 0:
			e = AllowEntry{ID: fields[0], Kind: "pending", Phase: fields[2], Reason: strings.Join(fields[3:], " ")}
		default:
			return nil, fmt.Errorf("allowlist line %d: want ID departure REASON or ID pending PHASE REASON", n)
		}
		if _, dup := entries[e.ID]; dup {
			return nil, fmt.Errorf("allowlist line %d: %s is listed twice", n, e.ID)
		}
		entries[e.ID] = e
	}
	return entries, sc.Err()
}

// Floors is what tests/compat-floors.env holds for jq. Phase is the phase
// the gate is at, Inventory and Regex are the percentage floors for the
// whole inventory and for its regex area, DepartureCapPercent caps the
// departures as a share of the inventory, and Values keeps every numeric
// setting the file carries by its key.
type Floors struct {
	Phase               string
	Inventory, Regex    int
	DepartureCapPercent int
	Values              map[string]int
}

// ReadFloors reads a floors file, which holds KEY=VALUE lines. JQ_PHASE
// names the phase and defaults to J1, JQ_INVENTORY_FLOOR, JQ_REGEX_FLOOR,
// and JQ_DEPARTURE_CAP are whole numbers, and every other numeric setting
// lands in Values. Blank lines and lines starting with # are skipped.
func ReadFloors(r io.Reader) (Floors, error) {
	f := Floors{Phase: "J1", Values: map[string]int{}}
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return Floors{}, fmt.Errorf("floors line %d: want KEY=VALUE", n)
		}
		k, v = strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), `"' `)
		if strings.HasSuffix(k, "_PHASE") {
			if phaseOrder[v] == 0 {
				return Floors{}, fmt.Errorf("floors line %d: %s names an unknown phase %q", n, k, v)
			}
			if k == "JQ_PHASE" {
				f.Phase = v
			}
			continue
		}
		num, err := strconv.Atoi(v)
		if err != nil || num < 0 || num > 100 {
			return Floors{}, fmt.Errorf("floors line %d: %s wants a percentage, got %q", n, k, v)
		}
		f.Values[k] = num
	}
	if err := sc.Err(); err != nil {
		return Floors{}, err
	}
	f.Inventory = f.Values["JQ_INVENTORY_FLOOR"]
	f.Regex = f.Values["JQ_REGEX_FLOOR"]
	f.DepartureCapPercent = f.Values["JQ_DEPARTURE_CAP"]
	return f, nil
}

// Baseline is the last gate's record. Entries is how many inventory
// entries there were, and Passing holds the IDs that passed.
type Baseline struct {
	Entries int
	Passing map[string]bool
}

// ReadBaseline reads a baseline file, whose first line is "entries N" and
// whose other lines are the IDs of the entries that passed. Blank lines and
// lines starting with # are skipped.
func ReadBaseline(r io.Reader) (Baseline, error) {
	b := Baseline{Passing: map[string]bool{}}
	sawCount := false
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !sawCount {
			count, ok := strings.CutPrefix(line, "entries ")
			num, err := strconv.Atoi(strings.TrimSpace(count))
			if !ok || err != nil || num < 0 {
				return Baseline{}, fmt.Errorf("baseline line %d: want \"entries N\"", n)
			}
			b.Entries, sawCount = num, true
			continue
		}
		b.Passing[line] = true
	}
	if err := sc.Err(); err != nil {
		return Baseline{}, err
	}
	if !sawCount {
		return Baseline{}, fmt.Errorf("baseline has no \"entries N\" line")
	}
	return b, nil
}

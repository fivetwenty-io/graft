package regex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// parityDir holds the parity corpus, whose goldens were recorded once from
// pcre2test and jq by tests/regex-parity/gen.sh. The tests read the goldens
// and never run either binary.
const parityDir = "../../tests/regex-parity"

// parityCase is one case of the parity corpus. Flavor is pcre or
// oniguruma, and Flags holds jq's flag letters for both flavors.
type parityCase struct{ ID, Flavor, Pattern, Flags, Subject string }

// parityMatch is one match as jq's match prints it, with offsets and
// lengths counted in runes.
type parityMatch struct {
	Offset   int             `json:"offset"`
	Length   int             `json:"length"`
	String   string          `json:"string"`
	Captures []parityCapture `json:"captures"`
}

// parityCapture is one group of a match as jq prints it. A group that took
// no part in the match has offset -1 and a null string, and an unnamed
// group has a null name.
type parityCapture struct {
	Offset int     `json:"offset"`
	Length int     `json:"length"`
	String *string `json:"string"`
	Name   *string `json:"name"`
}

// parityResult is what a case's golden holds, the list of matches or the
// news that the pattern did not compile. The golden keeps the reference
// engine's message for a pattern it refused, but the test asks only that
// ours refuses it too, since the two word their errors differently.
type parityResult struct {
	Failed  bool
	Matches []parityMatch
}

func TestRegexParityGoldens(t *testing.T) {
	for _, c := range loadParityCases(t) {
		t.Run(c.ID, func(t *testing.T) {
			wantEqual(t, matchesAsJSON(t, c), readGolden(t, c.ID))
		})
	}
}

// The corpus covers both flavors evenly and has exactly one golden per
// case.
func TestRegexParityCorpus(t *testing.T) {
	cases := loadParityCases(t)
	if len(cases) < 120 {
		t.Errorf("the corpus has %d cases, want at least 120", len(cases))
	}
	ids := map[string]bool{}
	perFlavor := map[string]int{}
	for _, c := range cases {
		if ids[c.ID] {
			t.Errorf("case %s appears twice", c.ID)
		}
		ids[c.ID] = true
		perFlavor[c.Flavor]++
	}
	for _, flavor := range []string{"pcre", "oniguruma"} {
		if perFlavor[flavor] < 60 {
			t.Errorf("the corpus has %d %s cases, want at least 60", perFlavor[flavor], flavor)
		}
	}
	goldens, err := filepath.Glob(filepath.Join(parityDir, "want", "*.json"))
	wantNil(t, err)
	for _, path := range goldens {
		if id := strings.TrimSuffix(filepath.Base(path), ".json"); !ids[id] {
			t.Errorf("golden %s belongs to no case", path)
		}
	}
	wantEqual(t, len(goldens), len(ids))
}

func TestParseParityCases(t *testing.T) {
	cases, err := parseParityCases(`# a comment
- id: one
  flavor: pcre
  pattern: "a: \"b\"\\d"
  flags: ""
  subject: "é\n"

- id: two
  flavor: oniguruma
  pattern: "x"
  flags: "g"
  subject: ""
`)
	wantNil(t, err)
	wantEqual(t, cases, []parityCase{
		{ID: "one", Flavor: "pcre", Pattern: `a: "b"\d`, Subject: "é\n"},
		{ID: "two", Flavor: "oniguruma", Pattern: "x", Flags: "g"},
	})
	for text, msg := range map[string]string{
		"  id: x\n":                 "line 1: a key outside a case",
		"- id: x\n  shade: red\n":   `line 2: unknown key "shade"`,
		"- id: x\n  flavor: pcre\n": "case x lacks pattern",
		"- id: x\n  pattern: \"a\n": `line 2: pattern: unexpected end of JSON input`,
		"- id: x\n  flavor: perl\n": "line 2: flavor perl is neither pcre nor oniguruma",
		"- id: x\n  id: y\n":        `line 2: case x sets id twice`,
		"- id: x\n  flavor pcre\n":  "line 2: want key: value",
		"- id: x\n  flavor: oniguruma\n  pattern: \"\"\n  flags: \"q\"\n  subject: \"\"\n": "case x: q is not a jq flag",
	} {
		_, err := parseParityCases(text)
		wantErrContains(t, err, msg)
	}
}

// loadParityCases reads the corpus.
func loadParityCases(t *testing.T) []parityCase {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(parityDir, "cases.yaml"))
	wantNil(t, err)
	cases, err := parseParityCases(string(data))
	wantNil(t, err)
	return cases
}

// parseParityCases reads the corpus by hand, since the package's tests
// import only the standard library. The file is a YAML list of block
// mappings, one key to a line, whose values are JSON strings or bare words.
func parseParityCases(text string) ([]parityCase, error) {
	var cases []parityCase
	var seen map[string]bool
	for n, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "- "); ok {
			if err := checkParityCase(cases, seen); err != nil {
				return nil, err
			}
			cases = append(cases, parityCase{})
			seen = map[string]bool{}
			line = "  " + rest
		}
		if len(cases) == 0 || !strings.HasPrefix(line, "  ") {
			return nil, fmt.Errorf("line %d: a key outside a case", n+1)
		}
		key, raw, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			return nil, fmt.Errorf("line %d: want key: value", n+1)
		}
		value, err := parityValue(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("line %d: %s: %w", n+1, key, err)
		}
		c := &cases[len(cases)-1]
		if seen[key] {
			return nil, fmt.Errorf("line %d: case %s sets %s twice", n+1, c.ID, key)
		}
		seen[key] = true
		switch key {
		case "id":
			c.ID = value
		case "flavor":
			if value != "pcre" && value != "oniguruma" {
				return nil, fmt.Errorf("line %d: flavor %s is neither pcre nor oniguruma", n+1, value)
			}
			c.Flavor = value
		case "pattern":
			c.Pattern = value
		case "flags":
			c.Flags = value
		case "subject":
			c.Subject = value
		default:
			return nil, fmt.Errorf("line %d: unknown key %q", n+1, key)
		}
	}
	if err := checkParityCase(cases, seen); err != nil {
		return nil, err
	}
	return cases, nil
}

// parityValue decodes a value, which is a JSON string when it starts with
// a quote and a bare word otherwise.
func parityValue(raw string) (string, error) {
	if !strings.HasPrefix(raw, `"`) {
		return raw, nil
	}
	var s string
	err := json.Unmarshal([]byte(raw), &s)
	return s, err
}

// checkParityCase refuses the last case read when it lacks a key or names
// a flag jq does not know.
func checkParityCase(cases []parityCase, seen map[string]bool) error {
	if len(cases) == 0 {
		return nil
	}
	c := cases[len(cases)-1]
	for _, key := range []string{"id", "flavor", "pattern", "flags", "subject"} {
		if !seen[key] {
			return fmt.Errorf("case %s lacks %s", c.ID, key)
		}
	}
	if _, _, err := c.options(); err != nil {
		return fmt.Errorf("case %s: %w", c.ID, err)
	}
	return nil
}

// options turns the case's flavor and flag letters into the options it
// compiles and matches with, reading the letters as jq's regex builtins do.
func (c parityCase) options() ([]Option, MatchOptions, error) {
	flavor := PCRE
	if c.Flavor == "oniguruma" {
		flavor = Oniguruma
	}
	var flags Flags
	var opts MatchOptions
	for _, r := range c.Flags {
		switch r {
		case 'g':
			opts.Global = true
		case 'i':
			flags |= IgnoreCase
		case 'x':
			flags |= Extended
		case 'n':
			opts.SkipEmpty = true
		case 's':
			flags |= SingleLine
		case 'm':
			flags |= DotAll
		case 'l':
			if flavor == PCRE {
				return nil, opts, fmt.Errorf("l has no PCRE2 equivalent")
			}
			flags |= Longest
		case 'p':
			flags |= SingleLine | DotAll
		default:
			return nil, opts, fmt.Errorf("%c is not a jq flag", r)
		}
	}
	return []Option{WithFlavor(flavor), WithFlags(flags)}, opts, nil
}

// matchesAsJSON compiles the case's pattern with its flavor and flags and
// renders every match the way jq's match would.
func matchesAsJSON(t *testing.T, c parityCase) parityResult {
	t.Helper()
	compileOpts, opts, err := c.options()
	wantNil(t, err)
	p, err := Compile(c.Pattern, compileOpts...)
	if err != nil {
		return parityResult{Failed: true}
	}
	var out parityResult
	for m, err := range p.FindAll(context.Background(), c.Subject, opts) {
		wantNil(t, err)
		out.Matches = append(out.Matches, renderParityMatch(m, c.Subject, c.Flavor == "oniguruma"))
	}
	return out
}

// renderParityMatch renders m as jq's match prints it. jq reports every
// group that took part in an empty match as empty at the match's offset,
// whatever the group captured, so jqEmpty applies that rule for the
// Oniguruma flavor. pcre2test reports the groups as they are.
func renderParityMatch(m *Match, subject string, jqEmpty bool) parityMatch {
	pm := parityMatch{Offset: m.RuneStart, Length: m.RuneEnd - m.RuneStart, String: m.Text(subject), Captures: []parityCapture{}}
	for _, g := range m.Groups[1:] {
		if jqEmpty && m.End == m.Start && g.Matched {
			g.Start, g.End, g.RuneStart, g.RuneEnd = m.Start, m.End, m.RuneStart, m.RuneEnd
		}
		pc := parityCapture{Offset: -1}
		if g.Name != "" {
			pc.Name = new(g.Name)
		}
		if g.Matched {
			pc.Offset, pc.Length, pc.String = g.RuneStart, g.RuneEnd-g.RuneStart, new(g.Text(subject))
		}
		pm.Captures = append(pm.Captures, pc)
	}
	return pm
}

// readGolden reads the golden of the case called id.
func readGolden(t *testing.T, id string) parityResult {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(parityDir, "want", id+".json"))
	wantNil(t, err)
	result, err := decodeGolden(data)
	wantNil(t, err)
	return result
}

// decodeGolden reads a golden, which is jq's list of matches or an object
// holding the reference engine's error.
func decodeGolden(data []byte) (parityResult, error) {
	data = bytes.TrimSpace(data)
	if bytes.HasPrefix(data, []byte("{")) {
		var failure struct {
			Error *string `json:"error"`
		}
		if err := json.Unmarshal(data, &failure); err != nil || failure.Error == nil {
			return parityResult{}, fmt.Errorf("a golden object must hold an error: %s", data)
		}
		return parityResult{Failed: true}, nil
	}
	var matches []parityMatch
	if err := json.Unmarshal(data, &matches); err != nil {
		return parityResult{}, err
	}
	if len(matches) == 0 {
		matches = nil
	}
	return parityResult{Matches: matches}, nil
}

func TestDecodeGolden(t *testing.T) {
	got, err := decodeGolden([]byte(`{"error":"missing )"}` + "\n"))
	wantNil(t, err)
	wantEqual(t, got, parityResult{Failed: true})
	got, err = decodeGolden([]byte("[]\n"))
	wantNil(t, err)
	wantEqual(t, got, parityResult{})
	got, err = decodeGolden([]byte(`[{"offset":1,"length":0,"string":"","captures":[{"offset":-1,"string":null,"length":0,"name":"n"}]}]`))
	wantNil(t, err)
	wantEqual(t, got, parityResult{Matches: []parityMatch{{Offset: 1, Captures: []parityCapture{{Offset: -1, Name: new("n")}}}}})
	_, err = decodeGolden([]byte(`{"matches":[]}`))
	wantErrContains(t, err, "must hold an error")
	_, err = decodeGolden([]byte(`[`))
	wantErrContains(t, err, "unexpected end of JSON input")
}

// The renderer reports an unset group as jq does and moves the groups of
// an empty match to its offset only for the Oniguruma flavor.
func TestRenderParityMatch(t *testing.T) {
	subject := "xab"
	m := find(t, MustCompile(`(?<n>z)?(a)(?=(b))`), subject)
	wantEqual(t, renderParityMatch(m, subject, true), parityMatch{Offset: 1, Length: 1, String: "a", Captures: []parityCapture{
		{Offset: -1, Name: new("n")},
		{Offset: 1, Length: 1, String: new("a")},
		{Offset: 2, Length: 1, String: new("b")},
	}})
	m = find(t, MustCompile(`(?=(a))`), subject)
	wantEqual(t, renderParityMatch(m, subject, false).Captures, []parityCapture{{Offset: 1, Length: 1, String: new("a")}})
	wantEqual(t, renderParityMatch(m, subject, true).Captures, []parityCapture{{Offset: 1, String: new("")}})
}

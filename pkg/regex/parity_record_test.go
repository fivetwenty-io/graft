package regex

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestRecordParityGoldens records a golden for every parity case, from jq
// for an Oniguruma case and from pcre2test for a PCRE one, into the
// directory REGEX_PARITY_OUT names. tests/regex-parity/gen.sh runs it after
// checking the versions of the two binaries, and every other run skips it.
func TestRecordParityGoldens(t *testing.T) {
	out := os.Getenv("REGEX_PARITY_OUT")
	if out == "" {
		t.Skip("set REGEX_PARITY_OUT, JQ_BIN, and PCRE2TEST to record the parity goldens")
	}
	jq, pcre2test := os.Getenv("JQ_BIN"), os.Getenv("PCRE2TEST")
	if jq == "" || pcre2test == "" {
		t.Fatal("JQ_BIN and PCRE2TEST must name the jq and pcre2test binaries")
	}
	//nolint:gosec // G703: the recorder writes where its caller points it
	wantNil(t, os.MkdirAll(out, 0o750))
	for _, c := range loadParityCases(t) {
		var golden []byte
		var err error
		if c.Flavor == "oniguruma" {
			golden, err = recordJQ(t.Context(), jq, c)
		} else {
			golden, err = recordPCRE2(t.Context(), pcre2test, c)
		}
		if err != nil {
			t.Fatalf("%s: %v", c.ID, err)
		}
		//nolint:gosec // G703: the recorder writes where its caller points it
		wantNil(t, os.WriteFile(filepath.Join(out, c.ID+".json"), golden, 0o600))
	}
}

// recordJQ returns what jq's match prints for the case, or jq's error
// when it refuses the pattern. The subject and the pattern go to jq as
// JSON, so either may hold a NUL, which no command argument can.
func recordJQ(ctx context.Context, jq string, c parityCase) ([]byte, error) {
	subject, err := json.Marshal(c.Subject)
	if err != nil {
		return nil, err
	}
	pattern, err := json.Marshal(c.Pattern)
	if err != nil {
		return nil, err
	}
	//nolint:gosec // G702: the recorder runs the jq binary its caller names
	cmd := exec.CommandContext(ctx, jq, "-nc", "--argjson", "s", string(subject), "--argjson", "p", string(pattern),
		"--arg", "f", c.Flags, `$s | [match($p; $f)]`)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err == nil {
		return stdout, nil
	}
	msg, ok := strings.CutPrefix(strings.TrimSpace(stderr.String()), "jq: error (at <unknown>): ")
	if !ok {
		return nil, fmt.Errorf("jq: %w: %s", err, stderr.String())
	}
	return encodeGolden(map[string]string{"error": msg})
}

// recordPCRE2 returns the case's matches as jq would print them, found by
// pcre2test. pcre2test's own global matching retries an empty match with
// different options, so this runs one search at a time from each start
// that this package's global loop would take: the end of a match, or one
// rune past the end of an empty one.
func recordPCRE2(ctx context.Context, pcre2test string, c parityCase) ([]byte, error) {
	if strings.Contains(c.Subject, `\x{`) || strings.Contains(c.Subject, "<unset>") {
		return nil, errors.New(`a PCRE subject cannot hold \x{ or <unset>, which pcre2test's output makes ambiguous`)
	}
	mods := "hex,utf,info,allcaptures,allaftertext"
	subjectMods := ""
	global := false
	for _, r := range c.Flags {
		switch r {
		case 'g':
			global = true
		case 'i':
			mods += ",caseless"
		case 'x':
			mods += ",extended"
		case 'm', 'p':
			mods += ",dotall"
		case 'n':
			subjectMods = ",notempty"
		case 's':
		default:
			return nil, fmt.Errorf("flag %c has no pcre2test equivalent", r)
		}
	}
	matches := []parityMatch{}
	for start := 0; ; {
		m, failure, err := searchPCRE2(ctx, pcre2test, c, mods, subjectMods, start)
		switch {
		case err != nil:
			return nil, err
		case failure != "":
			return encodeGolden(map[string]string{"error": failure})
		case m == nil:
			return encodeGolden(matches)
		}
		matches = append(matches, m.match)
		if !global {
			return encodeGolden(matches)
		}
		start = m.end
		if m.match.Length == 0 {
			if start >= len(c.Subject) {
				return encodeGolden(matches)
			}
			_, w := utf8.DecodeRuneInString(c.Subject[start:])
			start += w
		}
	}
}

// pcre2Match is one match pcre2test found, with the byte offset of its end.
type pcre2Match struct {
	match parityMatch
	end   int
}

// searchPCRE2 runs pcre2test for one search of the case's subject from the
// byte offset start. It returns the match, nil when there is none, or
// pcre2test's message when it refuses the pattern.
func searchPCRE2(ctx context.Context, pcre2test string, c parityCase, mods, subjectMods string, start int) (*pcre2Match, string, error) {
	subjectLine := pcre2Subject(c.Subject) + `\=offset=` + strconv.Itoa(start) + subjectMods
	input := "/" + hex.EncodeToString([]byte(c.Pattern)) + "/" + mods + "\n" + subjectLine + "\n"
	dir, err := os.MkdirTemp("", "regex-parity")
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	path := filepath.Join(dir, "input")
	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		return nil, "", err
	}
	//nolint:gosec // G702: the recorder runs the pcre2test binary its caller names
	output, err := exec.CommandContext(ctx, pcre2test, "-q", path).Output()
	if err != nil {
		return nil, "", fmt.Errorf("pcre2test: %w: %s", err, output)
	}
	return parsePCRE2Output(string(output), subjectLine, c.Subject)
}

// pcre2Subject writes subject for a pcre2test subject line, with every rune
// but an ASCII letter or digit as a \x{...} escape, so that no space,
// backslash, or empty line means anything to pcre2test.
func pcre2Subject(subject string) string {
	var b strings.Builder
	for _, r := range subject {
		if r < utf8.RuneSelf && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			fmt.Fprintf(&b, `\x{%x}`, r)
		}
	}
	return b.String()
}

// parsePCRE2Output reads what pcre2test printed for one pattern and the
// subject line subjectLine. The info lines name the group count and the
// group names, and after the echoed subject line come " n: text" and
// " n+ rest" for each group, or "No match". The text of a group and the
// rest of the subject after it give its rune offsets.
func parsePCRE2Output(output, subjectLine, subject string) (*pcre2Match, string, error) {
	lines := strings.Split(output, "\n")
	groups := -1
	names := map[int]string{}
	at := -1
	for i, line := range lines {
		if msg, ok := strings.CutPrefix(line, "Failed: error "); ok {
			_, msg, _ = strings.Cut(msg, ": ")
			return nil, msg, nil
		}
		if n, ok := strings.CutPrefix(line, "Capture group count = "); ok {
			groups, _ = strconv.Atoi(n)
		}
		if line == "Named capture groups:" {
			for _, entry := range lines[i+1:] {
				if !strings.HasPrefix(entry, "  ") {
					break
				}
				fields := strings.Fields(entry)
				n, err := strconv.Atoi(fields[len(fields)-1])
				if err != nil {
					return nil, "", fmt.Errorf("pcre2test named group line %q", entry)
				}
				names[n] = fields[0]
			}
		}
		if line == subjectLine {
			at = i
			break
		}
	}
	if groups < 0 || at < 0 {
		return nil, "", fmt.Errorf("pcre2test printed no group count or no subject:\n%s", output)
	}
	texts, rests := map[int]string{}, map[int]string{}
	for _, line := range lines[at+1:] {
		line = strings.TrimLeft(line, " ")
		if line == "No match" {
			return nil, "", nil
		}
		digits := strings.IndexFunc(line, func(r rune) bool { return r < '0' || r > '9' })
		if digits <= 0 || len(line) < digits+2 || line[digits+1] != ' ' {
			continue
		}
		n, _ := strconv.Atoi(line[:digits])
		switch body := decodePCRE2Text(line[digits+2:]); line[digits] {
		case ':':
			texts[n] = body
		case '+':
			rests[n] = body
		}
	}
	runes := []rune(subject)
	span := func(n int) (int, int, error) {
		text, rest := texts[n], rests[n]
		end := len(runes) - utf8.RuneCountInString(rest)
		start := end - utf8.RuneCountInString(text)
		if start < 0 || string(runes[start:end]) != text || string(runes[end:]) != rest {
			return 0, 0, fmt.Errorf("pcre2test group %d text %q rest %q do not fit the subject", n, text, rest)
		}
		return start, end, nil
	}
	if _, ok := texts[0]; !ok {
		return nil, "", fmt.Errorf("pcre2test printed neither a match nor No match:\n%s", output)
	}
	start, end, err := span(0)
	if err != nil {
		return nil, "", err
	}
	m := &pcre2Match{
		match: parityMatch{Offset: start, Length: end - start, String: string(runes[start:end]), Captures: []parityCapture{}},
		end:   len(string(runes[:end])),
	}
	for n := 1; n <= groups; n++ {
		pc := parityCapture{Offset: -1}
		if name, ok := names[n]; ok {
			pc.Name = new(name)
		}
		if text, ok := texts[n]; ok && text != "<unset>" {
			gs, ge, err := span(n)
			if err != nil {
				return nil, "", err
			}
			pc.Offset, pc.Length, pc.String = gs, ge-gs, new(text)
		}
		m.match.Captures = append(m.match.Captures, pc)
	}
	return m, "", nil
}

// decodePCRE2Text undoes pcre2test's \x{...} escapes. It prints every other
// rune, a backslash included, as itself.
func decodePCRE2Text(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], `\x{`) {
			if end := strings.IndexByte(s[i:], '}'); end > 3 {
				if v, err := strconv.ParseInt(s[i+3:i+end], 16, 32); err == nil {
					b.WriteRune(rune(v))
					i += end + 1
					continue
				}
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// encodeGolden writes v as one line of JSON, leaving <, >, and & as they
// are, as jq does.
func encodeGolden(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	err := enc.Encode(v)
	return b.Bytes(), err
}

func TestParsePCRE2Output(t *testing.T) {
	output := strings.Join([]string{
		`/2861297c/hex,utf,info,allcaptures,allaftertext`,
		`Capture group count = 3`,
		`Named capture groups:`,
		`  n   2`,
		`Options: utf`,
		`xb\x{5c}\x{e9}\=offset=1`,
		` 0: b\`,
		` 0+ \x{e9}`,
		` 1: <unset>`,
		` 2: b`,
		` 2+ \\x{e9}`,
		` 3: `,
		` 3+ \x{e9}`,
		``,
	}, "\n")
	m, failure, err := parsePCRE2Output(output, `xb\x{5c}\x{e9}\=offset=1`, `xb\é`)
	wantNil(t, err)
	wantEqual(t, failure, "")
	wantEqual(t, m.end, 3)
	wantEqual(t, m.match, parityMatch{Offset: 1, Length: 2, String: `b\`, Captures: []parityCapture{
		{Offset: -1},
		{Offset: 1, Length: 1, String: new("b"), Name: new("n")},
		{Offset: 3, String: new("")},
	}})

	m, failure, err = parsePCRE2Output("Capture group count = 0\nabc\nNo match\n", "abc", "abc")
	wantNil(t, err)
	wantEqual(t, failure, "")
	wantEqual(t, m, (*pcre2Match)(nil))

	_, failure, err = parsePCRE2Output("/a**/\nFailed: error 109 at offset 3: quantifier does not follow a repeatable item\n", "x", "x")
	wantNil(t, err)
	wantEqual(t, failure, "quantifier does not follow a repeatable item")

	_, _, err = parsePCRE2Output("Capture group count = 0\nabc\n 0: zz\n 0+ \n", "abc", "abc")
	wantErrContains(t, err, "do not fit the subject")
	_, _, err = parsePCRE2Output("Capture group count = 0\n", "abc", "abc")
	wantErrContains(t, err, "no group count or no subject")
}

func TestPCRE2Subject(t *testing.T) {
	wantEqual(t, pcre2Subject("a b\\é\n"), `a\x{20}b\x{5c}\x{e9}\x{a}`)
	wantEqual(t, decodePCRE2Text(`a\x{20}b\\x{e9}\x{zz}`), `a b\é\x{zz}`)
}

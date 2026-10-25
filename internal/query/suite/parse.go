// Package suite reads test files in jq's .test format and runs them
// against the engine. A case is a program, an input line, and the outputs
// the program is expected to produce, or a %%FAIL case naming the error it
// must raise.
package suite

import (
	"bufio"
	"io"
	"strings"
)

// Case is one test from a .test file. Line is the line the program is on.
// A %%FAIL case has no input and no outputs, and WantMsg is the error text
// the file expects, which IgnoreMsg waives.
type Case struct {
	Program   string
	Input     string
	Want      []string
	WantFail  bool
	WantMsg   string
	IgnoreMsg bool
	Line      int
	// Skip is a reason to leave the case out. Run reports it as skipped
	// and counts it as neither a pass nor a failure.
	Skip string
}

const failMarker = "%%FAIL"

// ParseFile reads every case in r. Cases are separated by blank lines, and
// a line that starts with # outside a case is a comment. A case whose input
// line is missing still parses, with an empty Input, and Run reports it as
// malformed.
func ParseFile(r io.Reader) ([]Case, error) {
	var lines []string
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	for sc.Scan() {
		lines = append(lines, strings.TrimSuffix(sc.Text(), "\r"))
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	var cases []Case
	for i := 0; i < len(lines); {
		if lines[i] == "" || strings.HasPrefix(lines[i], "#") {
			i++
			continue
		}
		var c Case
		if strings.HasPrefix(lines[i], failMarker) {
			c, i = parseFail(lines, i)
		} else {
			c, i = parseOutputs(lines, i)
		}
		for i < len(lines) && lines[i] != "" {
			i++
		}
		cases = append(cases, c)
	}
	return cases, nil
}

// parseFail reads a %%FAIL case that starts at lines[i], and returns it
// with the index of the first line it did not use.
func parseFail(lines []string, i int) (Case, int) {
	c := Case{WantFail: true, IgnoreMsg: strings.Contains(lines[i], "IGNORE MSG")}
	i++
	if i < len(lines) {
		c.Program, c.Line = lines[i], i+1
		i++
	}
	if i < len(lines) && lines[i] != "" {
		c.WantMsg = lines[i]
		i++
	}
	return c, i
}

// parseOutputs reads a case with a program, an input, and expected outputs
// that starts at lines[i], and returns it with the index of the first line
// it did not use.
func parseOutputs(lines []string, i int) (Case, int) {
	c := Case{Program: lines[i], Line: i + 1}
	i++
	if i < len(lines) && lines[i] != "" {
		c.Input = lines[i]
		i++
	}
	for ; i < len(lines) && lines[i] != ""; i++ {
		if !strings.HasPrefix(lines[i], "#") {
			c.Want = append(c.Want, lines[i])
		}
	}
	return c, i
}

// ReadAllowlist reads the list of known failures, one "file:line reason"
// entry per line. The map is keyed by file:line, and a blank line or a line
// that starts with # is skipped.
func ReadAllowlist(r io.Reader) (map[string]string, error) {
	list := map[string]string{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, reason, _ := strings.Cut(line, " ")
		if k, r, ok := strings.Cut(key, "\t"); ok {
			key, reason = k, r+" "+reason
		}
		list[key] = strings.TrimSpace(reason)
	}
	return list, sc.Err()
}

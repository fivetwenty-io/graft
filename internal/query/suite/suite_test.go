package suite

import (
	"bytes"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/query/jq"
)

func wantEqual[T any](t *testing.T, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func wantContains(t *testing.T, got, sub string) {
	t.Helper()
	if !strings.Contains(got, sub) {
		t.Errorf("%q does not contain %q", got, sub)
	}
}

func mustParseSuite(t *testing.T, src string) []Case {
	t.Helper()
	cases, err := ParseFile(strings.NewReader(src))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	return cases
}

func TestParseJQTestFormat(t *testing.T) {
	cases := mustParseSuite(t, "# c\n.a\n{\"a\":1}\n1\n\n%%FAIL\n{(0):1}\njq: error: Object keys must be strings\n\n%%FAIL IGNORE MSG\n}\nwhatever\n")
	wantEqual(t, len(cases), 3)
	wantEqual(t, cases[0].Want, []string{"1"})
	wantEqual(t, cases[0].Program, ".a")
	wantEqual(t, cases[0].Input, `{"a":1}`)
	wantEqual(t, cases[0].Line, 2)
	wantEqual(t, cases[1].WantFail, true)
	wantEqual(t, cases[1].WantMsg, "jq: error: Object keys must be strings")
	wantEqual(t, cases[1].Line, 7)
	wantEqual(t, cases[2].IgnoreMsg, true)
}

func TestParseKeepsManyOutputsAndSkipsComments(t *testing.T) {
	cases := mustParseSuite(t, "\n\n.[]\n[1,2]\n# note\n1\n2\r\n\n\n\n.\nnull\nnull")
	wantEqual(t, len(cases), 2)
	wantEqual(t, cases[0].Want, []string{"1", "2"})
	wantEqual(t, cases[1].Want, []string{"null"})
}

func TestRunReportsPassAndFail(t *testing.T) {
	var out bytes.Buffer
	pass, fail, _ := Run(mustParseSuite(t, ".a\n{\"a\":1}\n1\n\n.a\n{\"a\":1}\n2\n"), jq.New(), &out)
	wantEqual(t, pass, 1)
	wantEqual(t, fail, 1)
	wantContains(t, out.String(), "1 of 2 tests passed (0 malformed, 0 skipped)")
	wantContains(t, out.String(), "*** Expected 2, but got 1 for test at line number 5: .a")
}

func TestOutputsCompareAsValuesNotText(t *testing.T) {
	pass, _, _ := Run(mustParseSuite(t, ".\n{\"a\":1,\"b\":2}\n{\"b\":2,\"a\":1}\n"), jq.New(), io.Discard)
	wantEqual(t, pass, 1) // jq compares parsed values, so object key order does not matter here
}

func TestRunCountsMalformedCases(t *testing.T) {
	var out bytes.Buffer
	src := ".a/\n1\n1\n\n.[]\n[1,2]\n1\n2\n3\n\n.a\n[1]\n1\n\n.\n1\n"
	pass, fail, res := Run(mustParseSuite(t, src), jq.New(), &out)
	wantEqual(t, pass, 0)
	wantEqual(t, fail, 4)
	wantContains(t, out.String(), "*** Test program failed to compile at line 1: .a/")
	wantContains(t, out.String(), "*** Insufficient results for test at line number 5: .[]")
	wantContains(t, out.String(), "*** Insufficient results for test at line number 11: .a")
	wantContains(t, out.String(), "*** Superfluous result: 1 for test at line number 15, .")
	wantContains(t, out.String(), "0 of 4 tests passed (3 malformed, 0 skipped)")
	wantEqual(t, len(res), 4)
}

func TestRunFailCases(t *testing.T) {
	var out bytes.Buffer
	src := "%%FAIL\n{(0):1}\njq: error: Cannot use number (0) as object key at <top-level>, line 1, column 3:\n\n" +
		"%%FAIL\n{(0):1}\njq: error: wrong\n\n" +
		"%%FAIL IGNORE MSG\n.a.[\nwhatever\n\n" +
		"%%FAIL\nerror(\"x\")\njq: error (at <unknown>): x\n\n" +
		"%%FAIL\n.\njq: error: nothing\n"
	pass, fail, res := Run(mustParseSuite(t, src), jq.New(), &out)
	wantEqual(t, pass, 3)
	wantEqual(t, fail, 2)
	wantContains(t, out.String(), "but expected 'jq: error: wrong' at line number 6: {(0):1}")
	wantContains(t, out.String(), "was expected to fail")
	wantEqual(t, res[1].Pass, false)
	wantContains(t, res[1].Err, "wrong")
}

func TestRunStopsAtRuntimeError(t *testing.T) {
	_, _, res := Run(mustParseSuite(t, ".[]|if .>1 then error(\"boom\") else . end\n[1,2,3]\n1\n"), jq.New(), io.Discard)
	wantEqual(t, res[0].Got, []string{"1"})
	wantEqual(t, res[0].Pass, false)
}

func TestRunMiniSuiteFile(t *testing.T) {
	f, err := os.Open("testdata/mini.test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	cases, err := ParseFile(f)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	pass, fail, _ := Run(cases, jq.New(), &out)
	wantEqual(t, pass, 6)
	wantEqual(t, fail, 0)
	wantContains(t, out.String(), "6 of 6 tests passed (0 malformed, 0 skipped)")
}

func TestReadAllowlist(t *testing.T) {
	m, err := ReadAllowlist(strings.NewReader("# header\n\njq.test:12 uses ltrimstr on numbers\nother.test:7\n"))
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, m, map[string]string{"jq.test:12": "uses ltrimstr on numbers", "other.test:7": ""})
}

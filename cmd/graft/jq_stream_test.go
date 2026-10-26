package main

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestJQStreamEvents(t *testing.T) {
	Convey("--stream prints the events jq prints", t, func() {
		out, _, _ := runJQ(t, []string{"-c", "--stream", "."}, `{"a":[1,2]}`)
		So(out, ShouldEqual, "[[\"a\",0],1]\n[[\"a\",1],2]\n[[\"a\",1]]\n[[\"a\"]]\n")
		out, _, _ = runJQ(t, []string{"-c", "--stream", "."}, `{"a":[1,{"b":2}],"c":"x"}`)
		So(out, ShouldEqual, "[[\"a\",0],1]\n[[\"a\",1,\"b\"],2]\n[[\"a\",1,\"b\"]]\n[[\"a\",1]]\n[[\"c\"],\"x\"]\n[[\"c\"]]\n")
		out, _, _ = runJQ(t, []string{"-c", "--stream", "."}, `[[],{},[1,[]]]`)
		So(out, ShouldEqual, "[[0],[]]\n[[1],{}]\n[[2,0],1]\n[[2,1],[]]\n[[2,1]]\n[[2]]\n")
		out, _, _ = runJQ(t, []string{"-c", "--stream", "."}, "3 \"s\" null\n[] {}")
		So(out, ShouldEqual, "[[],3]\n[[],\"s\"]\n[[],null]\n[[],[]]\n[[],{}]\n")
	})

	Convey("--stream composes with -s and -n", t, func() {
		out, _, _ := runJQ(t, []string{"-c", "-s", "--stream", "."}, `[1,2]`)
		So(out, ShouldEqual, "[[[0],1],[[1],2],[[1]]]\n")
		out, _, _ = runJQ(t, []string{"-c", "-n", "--stream", "[inputs]"}, `[1,2] {"a":null}`)
		So(out, ShouldEqual, "[[[0],1],[[1],2],[[1]],[[\"a\"],null],[[\"a\"]]]\n")
	})

	Convey("--stream streams a YAML document once it is loaded", t, func() {
		out, _, _ := runJQ(t, []string{"-c", "--stream", "."}, "a: [1, 2]\n")
		So(out, ShouldEqual, "[[\"a\",0],1]\n[[\"a\",1],2]\n[[\"a\",1]]\n[[\"a\"]]\n")
	})

	Convey("a value cut short shows only the elements a delimiter confirmed", t, func() {
		out, e, st := runJQ(t, []string{"-c", "--stream", "."}, `{"a":[1,2`)
		So(out, ShouldEqual, "[[\"a\",0],1]\n")
		So(st.Code, ShouldEqual, 5)
		So(e, ShouldContainSubstring, "Unfinished JSON term at EOF")
	})

	Convey("--stream-errors reports the failure as a final event", t, func() {
		for in, want := range map[string]string{
			`[1,`:           "[[0],1]\n[\"Unfinished JSON term at EOF at line 1, column 3\",[1]]\n",
			`{"a":[1,2`:     "[[\"a\",0],1]\n[\"Unfinished JSON term at EOF at line 1, column 9\",[\"a\",1]]\n",
			`[1,]`:          "[[0],1]\n[\"Expected another array element at line 1, column 4\",[1]]\n",
			`[1 2]`:         "[\"Expected separator between values at line 1, column 5\",[0]]\n",
			`{"a":1,}`:      "[[\"a\"],1]\n[\"Expected another key:value pair at line 1, column 8\",[null]]\n",
			`{1:2}`:         "[\"Object keys must be strings at line 1, column 3\",[null]]\n",
			`[1,2}`:         "[[0],1]\n[\"Unmatched '}' in the middle of an array at line 1, column 5\",[1]]\n",
			`[1,2]]`:        "[[0],1]\n[[1],2]\n[[1]]\n[\"Unmatched ']' at the top-level at line 1, column 6\",[]]\n",
			`[nul]`:         "[\"Invalid literal at line 1, column 5\",[0]]\n",
			`"abc`:          "[\"Unfinished string at EOF at line 1, column 4\",[]]\n",
			`["\x"]`:        "[\"Invalid escape at line 1, column 5\",[0]]\n",
			"[1,\n2,\n":     "[[0],1]\n[[1],2]\n[\"Unfinished JSON term at EOF at line 3, column 0\",[2]]\n",
			`{"a":{"b":`:    "[\"Unfinished JSON term at EOF at line 1, column 10\",[\"a\",\"b\"]]\n",
			`{"a":}`:        "[\"Missing value in key:value pair at line 1, column 6\",[\"a\"]]\n",
			`[,]`:           "[\"Expected value before ',' at line 1, column 2\",[0]]\n",
			`1 ]`:           "[[],1]\n[\"Unmatched ']' at the top-level at line 1, column 3\",[]]\n",
			`{"a" 1}`:       "[\"Expected separator between values at line 1, column 7\",[null]]\n",
			`[[1,`:          "[[0,0],1]\n[\"Unfinished JSON term at EOF at line 1, column 4\",[0,1]]\n",
			`[{"a":1},`:     "[[0,\"a\"],1]\n[[0,\"a\"]]\n[\"Unfinished JSON term at EOF at line 1, column 9\",[1]]\n",
			`[1]x`:          "[[0],1]\n[[0]]\n[\"Invalid numeric literal at EOF at line 1, column 4\",[]]\n",
			`{"a":1 "b":2}`: "[\"Expected separator between values at line 1, column 10\",[\"a\"]]\n",
		} {
			out, _, st := runJQ(t, []string{"-c", "--input-format", "json", "--stream-errors", "."}, in)
			So(out, ShouldEqual, want)
			So(st.Code, ShouldEqual, 0)
		}
	})
}

// lockedBuffer is a writer one goroutine fills while another reads it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestJQStreamIsIncremental(t *testing.T) {
	// The reader hands over the first value and then blocks. A loader that
	// read the whole input before it printed anything would never return.
	pr, pw := io.Pipe()
	var out lockedBuffer
	opts, err := parseJQArgs([]string{"-c", "--unbuffered", "--stream", "."}, personaGraft)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		var stderr bytes.Buffer
		handleJQ(opts, pr, &out, &stderr)
	}()
	if _, err := io.WriteString(pw, "[1,2]\n"); err != nil {
		t.Fatal(err)
	}
	want := "[[0],1]\n[[1],2]\n[[1]]\n"
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(out.String()) >= len(want) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := out.String(); got != want {
		t.Errorf("before the rest of the input arrived got %q, want %q", got, want)
	}
	_ = pw.Close()
	<-done
}

func TestJQSeq(t *testing.T) {
	Convey("--seq reads RS-separated texts and writes an RS before each output", t, func() {
		out, _, _ := runJQ(t, []string{"--seq", "-c", "."}, "\x1e1\n\x1e2\n")
		So(out, ShouldEqual, "\x1e1\n\x1e2\n")
		out, _, _ = runJQ(t, []string{"--seq", "-c", "."}, "\x1e[1,2]\n\x1e{\"a\":3}\n")
		So(out, ShouldEqual, "\x1e[1,2]\n\x1e{\"a\":3}\n")
		out, _, _ = runJQ(t, []string{"--seq", "-r", "."}, "\x1e\"x\"\n")
		So(out, ShouldEqual, "x\n") // no RS before raw output
	})

	Convey("--seq numbers the events of --stream too", t, func() {
		out, _, _ := runJQ(t, []string{"--seq", "-c", "--stream", "."}, "\x1e[1]\n")
		So(out, ShouldEqual, "\x1e[[0],1]\n\x1e[[0]]\n")
	})
}

func TestJQInputLineNumber(t *testing.T) {
	Convey("input_line_number counts the lines read so far", t, func() {
		for _, c := range []struct {
			in   string
			prog string
			want string
		}{
			{"1\n2\n", "[., input_line_number]", "[1,1]\n[2,2]\n"},
			{"{\n\"a\":\n1}\n", "input_line_number", "3\n"},
			{"{\n\"a\":\n1}", "input_line_number", "2\n"},
			{"1 2\n3\n", "[., input_line_number]", "[1,1]\n[2,1]\n[3,2]\n"},
			{"1\n\n\n2", "[., input_line_number]", "[1,1]\n[2,3]\n"},
			{"[1,\n2]\n", "[., input_line_number]", "[[1,2],2]\n"},
		} {
			out, _, _ := runJQ(t, []string{"-c", c.prog}, c.in)
			So(out, ShouldEqual, c.want)
		}
		out, _, _ := runJQ(t, []string{"-c", "--stream", "[., input_line_number]"}, "[1,\n2]\n")
		So(out, ShouldEqual, "[[[0],1],1]\n[[[1],2],2]\n[[[1]],2]\n")
		// jq reads a long line in pieces of 4,091 bytes and counts the newline
		// of a piece when it takes the piece, which is 2,045 events in.
		long := "[" + strings.Repeat("1,", 2999) + "1]\n"
		out, _, _ = runJQ(t, []string{"--stream", "input_line_number"}, long)
		So(out, ShouldEqual, strings.Repeat("0\n", 2045)+strings.Repeat("1\n", 956))
		out, _, _ = runJQ(t, []string{"-n", "input_line_number"}, "1\n")
		So(out, ShouldEqual, "0\n")
		out, _, _ = runJQ(t, []string{"-c", "-n", "[input, input_line_number]"}, "{\n\"a\":1}\n")
		So(strings.TrimSpace(out), ShouldEqual, `[{"a":1},2]`)
	})
}

func TestJQNumberSpellings(t *testing.T) {
	Convey("graft jq reads the number spellings jq does", t, func() {
		out, _, _ := runJQ(t, []string{"-c", "--input-format", "json", "."}, "01 -01 nan -nan +1 .5 1. infinity\n")
		So(out, ShouldEqual, "1\n-1\nnull\nnull\n1\n0.5\n1\n1.7976931348623157e+308\n")
		out, _, _ = runJQ(t, []string{"-c", "--stream", "--input-format", "json", "."}, "[01,nan]")
		So(out, ShouldEqual, "[[0],1]\n[[1],null]\n[[1]]\n")
		_, e, st := runJQ(t, []string{"-c", "--input-format", "json", "."}, "0x10")
		So(st.Code, ShouldNotEqual, 0)
		So(e, ShouldContainSubstring, "invalid literal")
	})
}

func TestJQSeqRecovery(t *testing.T) {
	const rs = "\x1e"
	Convey("--seq warns about a damaged text and reads on", t, func() {
		for _, c := range []struct {
			name, in string
			args     []string
			out, err string
		}{
			{"truncated value", rs + `{"a":1` + rs + "{\"b\":2}\n", nil, "{\"b\":2}\n", "Truncated value at line 1, column 8"},
			{"truncated stream", rs + "[1,2\n", []string{"--stream"}, "[[0],1]\n", "Unfinished JSON term at EOF at line 2, column 0"},
			{"truncated stream by RS", rs + "[1,2" + rs + "3\n", []string{"--stream"}, "[[0],1]\n[[],3]\n", "Truncated value at line 1, column 6"},
			{"abandoned text", "1 2\n", nil, "", "Unfinished abandoned text at EOF at line 2, column 0"},
			{"empty input", "", nil, "", "Unfinished abandoned text at EOF at line 1, column 0"},
			{"text before the first RS", "x\n" + rs + "3\n", nil, "3\n", ""},
			{"whitespace before RS", rs + "[1,2 " + rs + "3\n", nil, "3\n", ""},
			{"truncated top-level number", rs + "12" + rs + "4\n", nil, "4\n", "Potentially truncated top-level numeric value at line 1, column 4"},
			{"number at EOF", rs + "12", nil, "", "Potentially truncated top-level numeric value at EOF at line 1, column 3"},
			{"number at EOF streams", rs + "12", []string{"--stream"}, "[[],12]\n", ""},
			{"truncated string", rs + `"ab` + rs + "4\n", nil, "4\n", "Truncated value at line 1, column 5"},
			{"empty string cut", rs + `"` + rs + "3\n", nil, "3\n", ""},
			{"truncated keyword", rs + "true" + rs + "3\n", nil, "3\n", "Truncated value at line 1, column 6"},
			{"complete string at RS", rs + `"a"` + rs + "3\n", []string{"--stream"}, "[[],\"a\"]\n[[],3]\n", ""},
			{"parse error resyncs", rs + "[1,]" + rs + "3\n", nil, "3\n", "Expected another array element at line 1, column 5 (need RS to resync)"},
			{"parse error reads on", rs + "[1,] 3 4" + rs + "5\n", nil, "3\n5\n", "Expected another array element at line 1, column 5 (need RS to resync)"},
			{"unfinished at EOF", rs + "[1,2", nil, "", "Unfinished JSON term at EOF at line 1, column 5"},
			{"keyword at EOF", rs + "true", nil, "true\n", ""},
			{"stream error event", rs + "[1,\n" + rs + "2\n", []string{"--stream-errors"}, "[[0],1]\n[\"Truncated value at line 2, column 1\",[1]]\n[[],2]\n", ""},
		} {
			args := append([]string{"-c", "--seq"}, c.args...)
			// --seq writes an RS before each output.
			out, e, st := runJQ(t, append(args, "."), c.in)
			want := ""
			for _, line := range strings.SplitAfter(c.out, "\n") {
				if line != "" {
					want += rs + line
				}
			}
			So(out, ShouldEqual, want)
			So(st.Code, ShouldEqual, 0)
			if c.err == "" {
				So(e, ShouldEqual, "")
			} else {
				So(e, ShouldContainSubstring, c.err)
			}
		}
	})
}

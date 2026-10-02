package termstyle

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/fivetwenty-io/graft/internal/yamlgolden"
)

func modeFor(name string) Mode {
	switch name {
	case "truecolor":
		return Mode{Color: true, TrueColor: true}
	case "ansi16":
		return Mode{Color: true}
	default:
		return Mode{}
	}
}

func optionsFor(t *testing.T, specs []string) []Option {
	t.Helper()
	var opts []Option
	for _, s := range specs {
		switch {
		case s == "bold":
			opts = append(opts, Bold())
		case s == "italic":
			opts = append(opts, Italic())
		case s == "underline":
			opts = append(opts, Underline())
		case s == "eachline":
			opts = append(opts, EachLine())
		case strings.HasPrefix(s, "fg:"):
			var c RGB
			if _, err := fmt.Sscanf(s, "fg:%d,%d,%d", &c.R, &c.G, &c.B); err != nil {
				t.Fatal(err)
			}
			opts = append(opts, Foreground(c))
		default:
			t.Fatalf("unknown option %q", s)
		}
	}
	return opts
}

func TestStyleMatchesBuntVectors(t *testing.T) {
	var vectors []struct {
		Mode  string   `json:"mode"`
		Text  string   `json:"text"`
		Opts  []string `json:"opts"`
		Out   string   `json:"out"`
		Panic string   `json:"panic"`
	}
	yamlgolden.ReadJSON(t, "testdata/golden/style.json", &vectors)
	if len(vectors) != 1056 {
		t.Fatalf("style.json holds %d vectors, want 1056", len(vectors))
	}
	for _, v := range vectors {
		got, err := modeFor(v.Mode).Style(v.Text, optionsFor(t, v.Opts)...)
		if v.Panic != "" {
			var sgr *SGRError
			if !errors.As(err, &sgr) {
				t.Errorf("%s %q %v: err = %v, want *SGRError (bunt panics with %q)", v.Mode, v.Text, v.Opts, err, v.Panic)
			} else if strings.HasPrefix(v.Panic, "unsupported ") && err.Error() != v.Panic {
				t.Errorf("%s %q: err = %q, want %q", v.Mode, v.Text, err, v.Panic)
			}
			continue
		}
		if err != nil || got != v.Out {
			t.Errorf("%s Style(%q, %v) = %q, %v; want %q", v.Mode, v.Text, v.Opts, got, err, v.Out)
		}
	}
}

func TestStyleKnownBytes(t *testing.T) {
	yellow := RGB{199, 196, 63}
	tc := Mode{Color: true, TrueColor: true}
	for _, c := range []struct {
		mode Mode
		text string
		opts []Option
		want string
	}{
		{tc, "± value change", []Option{EachLine(), Foreground(yellow)}, "\x1b[38;2;199;196;63m± value change\x1b[0m"},
		{Mode{Color: true}, "± value change", []Option{EachLine(), Foreground(yellow)}, "\x1b[93m± value change\x1b[0m"},
		{Mode{}, "red\x1b[31mtext\x1b[0m", nil, "redtext"},
		{Mode{}, "x\x1b[1my", []Option{Bold()}, "xy"},
		{tc, "k:", []Option{Bold()}, "\x1b[1mk:\x1b[0m"},
	} {
		got, err := c.mode.Style(c.text, c.opts...)
		if err != nil || got != c.want {
			t.Errorf("Style(%q) = %q, %v; want %q", c.text, got, err, c.want)
		}
	}
}

func TestStyleKeepsParsedColorOnNewlines(t *testing.T) {
	got, err := Mode{Color: true, TrueColor: true}.Style("\x1b[38;2;255;0;255mm\nn", EachLine(), Foreground(RGB{88, 191, 56}))
	if err != nil || !strings.Contains(got, "\x1b[38;2;255;0;255m\n") {
		t.Fatalf("got %q, %v; EachLine must leave the parsed foreground on the newline, as bunt does", got, err)
	}
}

func TestStripSGRAndPlainLen(t *testing.T) {
	if got := StripSGR("\x1b[1;31mab\x1b[0m日\x1b[m"); got != "ab日" {
		t.Errorf("StripSGR = %q, want %q", got, "ab日")
	}
	if got := PlainLen("\x1b[1;31mab\x1b[0m日"); got != 3 {
		t.Errorf("PlainLen = %d, want 3", got)
	}
	if got := PlainLen("\x1b[Kx"); got != 4 {
		t.Errorf("PlainLen counts escapes bunt's regex does not strip, got %d, want 4", got)
	}
	if got := PlainLen("a\x1b[\x1b[0m1mb"); got != 2 {
		t.Errorf("PlainLen must strip until no escape remains, as bunt does, got %d, want 2", got)
	}
}

func TestStyleLongColorSequenceIsLinear(t *testing.T) {
	text := "\x1b[" + strings.Repeat("38;2;1;2;3;", 19999) + "38;2;1;2;3mtext"
	start := time.Now()
	got, err := Mode{Color: true, TrueColor: true}.Style(text)
	elapsed := time.Since(start)
	if err != nil || !strings.HasSuffix(got, "text\x1b[0m") {
		t.Fatalf("Style = %.40q, %v", got, err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Style took %v on one long escape sequence, want under 2s", elapsed)
	}
}

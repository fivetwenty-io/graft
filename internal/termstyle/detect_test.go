package termstyle

import (
	"errors"
	"os"
	"testing"
)

func TestTrueColorFromEnv(t *testing.T) {
	for value, want := range map[string]bool{"truecolor": true, "24bit": true, "TRUECOLOR": false, "256": false, "": false} {
		getenv := func(k string) string {
			switch k {
			case "COLORTERM":
				return value
			case "TERM":
				return "xterm-256color"
			}
			return ""
		}
		if got := TrueColorFromEnv(getenv); got != want {
			t.Errorf("COLORTERM=%q: TrueColorFromEnv = %v, want %v", value, got, want)
		}
	}
}

func withPid1(t *testing.T, name string, err error) {
	t.Helper()
	prev := pid1Name
	pid1Name = func() (string, error) { return name, err }
	t.Cleanup(func() { pid1Name = prev })
}

func pipeWriter(t *testing.T) *os.File {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
	return w
}

func TestTerminalWidthGarden(t *testing.T) {
	withPid1(t, "garden-init", nil)
	if got := TerminalWidth(pipeWriter(t)); got != 120 {
		t.Fatalf("TerminalWidth in garden = %d, want 120 even on a pipe", got)
	}
}

func TestTerminalWidthPipe(t *testing.T) {
	withPid1(t, "systemd", nil)
	if got := TerminalWidth(pipeWriter(t)); got != 80 {
		t.Fatalf("TerminalWidth on a pipe = %d, want 80", got)
	}
	withPid1(t, "", errors.New("no /proc"))
	if got := TerminalWidth(pipeWriter(t)); got != 80 {
		t.Fatalf("TerminalWidth without /proc = %d, want 80", got)
	}
}

func TestStdoutColorCapablePipe(t *testing.T) {
	if StdoutColorCapable(pipeWriter(t)) {
		t.Fatal("a pipe is never color capable")
	}
}

func TestParseStatName(t *testing.T) {
	for stat, want := range map[string]string{
		"1 (garden-init) S 0 1 1 0 -1": "garden-init",
		"1 (systemd) S 0 1 1 0 -1":     "systemd",
		"1 (a (b)) S 0":                "a (b",
		"garbage":                      "",
	} {
		if got := parseStatName(stat); got != want {
			t.Errorf("parseStatName(%q) = %q, want %q", stat, got, want)
		}
	}
}

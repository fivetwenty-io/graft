package termbg

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

func TestParseColorFGBG(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want Background
	}{
		{"rxvt dark (light text on black)", "7;0", Dark},
		{"light text on white background", "0;15", Light},
		{"Konsole three-field dark", "15;default;0", Dark},
		{"Konsole three-field light", "0;default;15", Light},
		{"garbage, no separator", "banana", Unknown},
		{"empty", "", Unknown},
		{"background field out of range", "0;99", Unknown},
		{"background field non-numeric", "0;abc", Unknown},
		{"single field only", "7", Unknown},
		{"boundary dark slot 6", "0;6", Dark},
		{"boundary dark slot 8", "0;8", Dark},
		{"boundary light slot 9", "0;9", Light},
		{"boundary light slot 15", "0;15", Light},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseColorFGBG(tt.in); got != tt.want {
				t.Errorf("ParseColorFGBG(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// withIsTerminal swaps the IsTerminal seam for the duration of the
// test, restoring it afterward - the same function-var-swap pattern
// cmd/graft uses for isStderrTTY/isStdoutTTY and
// debugInputIsInteractive.
func withIsTerminal(t *testing.T, value bool) {
	t.Helper()
	prev := IsTerminal
	IsTerminal = func(*os.File) bool { return value }
	t.Cleanup(func() { IsTerminal = prev })
}

func TestDetectGuards(t *testing.T) {
	// Every guard test sets a COLORFGBG that would resolve to Dark if
	// consulted, so a Detect result of Unknown proves the guard fired
	// (and did not merely fall through to an unset COLORFGBG).
	const wouldBeDark = "15;0"

	t.Run("neither stream a terminal", func(t *testing.T) {
		withIsTerminal(t, false)
		t.Setenv("COLORFGBG", wouldBeDark)
		if got := Detect(os.Stdin, os.Stdout); got != Unknown {
			t.Errorf("Detect() = %v, want Unknown", got)
		}
	})

	t.Run("inside tmux", func(t *testing.T) {
		withIsTerminal(t, true)
		t.Setenv("TERM", "xterm-256color")
		t.Setenv("TMUX", "/tmp/tmux-1000/default,1234,0")
		t.Setenv("COLORFGBG", wouldBeDark)
		if got := Detect(os.Stdin, os.Stdout); got != Unknown {
			t.Errorf("Detect() = %v, want Unknown", got)
		}
	})

	t.Run("TERM starts with screen", func(t *testing.T) {
		withIsTerminal(t, true)
		t.Setenv("TERM", "screen-256color")
		t.Setenv("TMUX", "")
		t.Setenv("COLORFGBG", wouldBeDark)
		if got := Detect(os.Stdin, os.Stdout); got != Unknown {
			t.Errorf("Detect() = %v, want Unknown", got)
		}
	})

	t.Run("TERM starts with tmux", func(t *testing.T) {
		withIsTerminal(t, true)
		t.Setenv("TERM", "tmux-256color")
		t.Setenv("TMUX", "")
		t.Setenv("COLORFGBG", wouldBeDark)
		if got := Detect(os.Stdin, os.Stdout); got != Unknown {
			t.Errorf("Detect() = %v, want Unknown", got)
		}
	})

	t.Run("TERM empty", func(t *testing.T) {
		withIsTerminal(t, true)
		t.Setenv("TERM", "")
		t.Setenv("TMUX", "")
		t.Setenv("COLORFGBG", wouldBeDark)
		if got := Detect(os.Stdin, os.Stdout); got != Unknown {
			t.Errorf("Detect() = %v, want Unknown", got)
		}
	})

	t.Run("TERM dumb", func(t *testing.T) {
		withIsTerminal(t, true)
		t.Setenv("TERM", "dumb")
		t.Setenv("TMUX", "")
		t.Setenv("COLORFGBG", wouldBeDark)
		if got := Detect(os.Stdin, os.Stdout); got != Unknown {
			t.Errorf("Detect() = %v, want Unknown", got)
		}
	})

	t.Run("inside Emacs", func(t *testing.T) {
		withIsTerminal(t, true)
		t.Setenv("TERM", "xterm-256color")
		t.Setenv("TMUX", "")
		t.Setenv("INSIDE_EMACS", "27.1,comint")
		t.Setenv("COLORFGBG", wouldBeDark)
		if got := Detect(os.Stdin, os.Stdout); got != Unknown {
			t.Errorf("Detect() = %v, want Unknown", got)
		}
	})

	t.Run("every guard passes, COLORFGBG resolves dark", func(t *testing.T) {
		withIsTerminal(t, true)
		t.Setenv("TERM", "xterm-256color")
		t.Setenv("TMUX", "")
		t.Setenv("INSIDE_EMACS", "")
		t.Setenv("COLORFGBG", "15;0")
		if got := Detect(os.Stdin, os.Stdout); got != Dark {
			t.Errorf("Detect() = %v, want Dark", got)
		}
	})

	t.Run("every guard passes, COLORFGBG resolves light", func(t *testing.T) {
		withIsTerminal(t, true)
		t.Setenv("TERM", "xterm-256color")
		t.Setenv("TMUX", "")
		t.Setenv("INSIDE_EMACS", "")
		t.Setenv("COLORFGBG", "0;15")
		if got := Detect(os.Stdin, os.Stdout); got != Light {
			t.Errorf("Detect() = %v, want Light", got)
		}
	})

	t.Run("every guard passes, COLORFGBG unset", func(t *testing.T) {
		withIsTerminal(t, true)
		t.Setenv("TERM", "xterm-256color")
		t.Setenv("TMUX", "")
		t.Setenv("INSIDE_EMACS", "")
		// os.Getenv reports the same empty string for an unset variable
		// as for one explicitly set to "", so t.Setenv("", "") exercises
		// the same code path as an unset COLORFGBG while staying within
		// t.Setenv's automatic restore.
		t.Setenv("COLORFGBG", "")
		if got := Detect(os.Stdin, os.Stdout); got != Unknown {
			t.Errorf("Detect() = %v, want Unknown", got)
		}
	})
}

// withMakeRaw swaps makeRawSeam for the duration of the test with a
// fake that always reports success, restoring it afterward: this lets
// queryOSC11 proceed past raw-mode entry to read/write against a
// scripted stream below, rather than testing whichever way the real
// golang.org/x/term.MakeRaw happens to treat that stream. Tests that
// want the raw-mode-unavailable path instead rely on the real
// makeRawSeam's own outcome against a non-tty stream (an os.Pipe()
// pair), so they never call this helper at all.
func withMakeRaw(t *testing.T) {
	t.Helper()
	prev := makeRawSeam
	makeRawSeam = func(int) (func(), error) { return func() {}, nil }
	t.Cleanup(func() { makeRawSeam = prev })
}

// withPollableDup swaps the pollableDup seam for the duration of the
// test, restoring it afterward, so queryOSC11's own tests can force
// the "no pollable duplicate available" path deterministically -
// something otherwise hard to reach on demand, since the real
// defaultPollableDup succeeds for nearly any valid descriptor. Passing
// a nil err restores the real, platform-specific defaultPollableDup
// instead of faking anything, so tests that want the real duplicate-
// and-reflag logic exercised (against an os.Pipe() pair, which is
// always poll-registrable) never have to call this at all.
func withPollableDup(t *testing.T, err error) {
	t.Helper()
	prev := pollableDup
	pollableDup = func(*os.File) (*os.File, func(), error) { return nil, nil, err }
	t.Cleanup(func() { pollableDup = prev })
}

// withShortOSCTimings shrinks the query timeout, drain budget, and
// drain step to test-friendly durations, restoring the real ones
// afterward, so the timeout/drain tests below run in milliseconds
// rather than waiting out the real 150ms/200ms production values.
func withShortOSCTimings(t *testing.T) {
	t.Helper()
	prevTimeout, prevBudget, prevStep := oscQueryTimeout, oscDrainBudget, oscDrainStep
	oscQueryTimeout = 10 * time.Millisecond
	oscDrainBudget = 60 * time.Millisecond
	oscDrainStep = 10 * time.Millisecond
	t.Cleanup(func() {
		oscQueryTimeout, oscDrainBudget, oscDrainStep = prevTimeout, prevBudget, prevStep
	})
}

func TestParseOSC11Response(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want Background
		ok   bool
	}{
		{"black, BEL terminated, 4 hex digits", "\x1b]11;rgb:0000/0000/0000\a", Dark, true},
		{"white, ST terminated, 4 hex digits", "\x1b]11;rgb:ffff/ffff/ffff\x1b\\", Light, true},
		{"2 hex digits per channel, dark", "\x1b]11;rgb:1e/1e/1e\a", Dark, true},
		{"1 hex digit per channel, light", "\x1b]11;rgb:f/f/f\a", Light, true},
		{"3 hex digits per channel, dark", "\x1b]11;rgb:111/111/111\a", Dark, true},
		{"mixed digit widths", "\x1b]11;rgb:ffff/00/0\a", Dark, true}, // green/blue dark enough to outweigh red
		{"rgba: prefix accepted", "\x1b]11;rgba:0000/0000/0000/ffff\a", Dark, true},
		{"already-stripped leading ESC/introducer", "rgb:0000/0000/0000\a", Dark, true},
		{"no terminator at all", "\x1b]11;rgb:0000/0000/0000", Dark, true},
		{"missing rgb:/rgba: prefix", "\x1b]11;0000/0000/0000\a", Unknown, false},
		{"only two channels", "\x1b]11;rgb:0000/0000\a", Unknown, false},
		{"empty channel", "\x1b]11;rgb:/0000/0000\a", Unknown, false},
		{"channel too long", "\x1b]11;rgb:00000/0000/0000\a", Unknown, false},
		{"non-hex channel", "\x1b]11;rgb:zzzz/0000/0000\a", Unknown, false},
		{"empty response", "", Unknown, false},
		{"garbage", "not an OSC reply at all", Unknown, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseOSC11Response([]byte(tt.in))
			if got != tt.want || ok != tt.ok {
				t.Errorf("parseOSC11Response(%q) = (%v, %v), want (%v, %v)", tt.in, got, ok, tt.want, tt.ok)
			}
		})
	}
}

// TestDetectFallsThroughToOSCQueryWhenColorFGBGUnset proves Detect
// actually reaches queryOSC11 - rather than stopping at COLORFGBG -
// once every guard passes and COLORFGBG is unset. It uses a real
// os.Pipe() pair instead of a real tty: neither end is a terminal, so
// the unfaked makeRawSeam (real golang.org/x/term.MakeRaw) fails
// against it exactly as it would against any non-tty stream, and
// queryOSC11 reports Unknown without reading or writing a single byte -
// proof that reaching the query is itself safe, with no real terminal
// required.
func TestDetectFallsThroughToOSCQueryWhenColorFGBGUnset(t *testing.T) {
	withIsTerminal(t, true)
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("TMUX", "")
	t.Setenv("INSIDE_EMACS", "")
	t.Setenv("COLORFGBG", "")

	inR, _ := newTestPipe(t)
	_, outW := newTestPipe(t)

	if got := Detect(inR, outW); got != Unknown {
		t.Errorf("Detect() = %v, want Unknown (query unreachable on a pipe)", got)
	}
}

// newTestPipe opens an os.Pipe(), fatally failing the test if that
// itself errors, and registers both ends to close on cleanup.
func newTestPipe(t *testing.T) (r, w *os.File) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe(): %v", err)
	}
	t.Cleanup(func() {
		_ = r.Close()
		_ = w.Close()
	})
	return r, w
}

func TestQueryOSC11(t *testing.T) {
	t.Run("raw mode unavailable reports Unknown and touches neither stream", func(t *testing.T) {
		inR, _ := newTestPipe(t)
		outR, outW := newTestPipe(t)

		if got := queryOSC11(inR, outW, 50*time.Millisecond); got != Unknown {
			t.Errorf("queryOSC11() = %v, want Unknown", got)
		}

		// Nothing should have been written to out: raw mode failed
		// before the query was ever sent.
		if err := outW.Close(); err != nil {
			t.Fatalf("outW.Close(): %v", err)
		}
		buf := make([]byte, 1)
		n, _ := outR.Read(buf)
		if n != 0 {
			t.Errorf("out carries %d unexpected byte(s), want 0", n)
		}
	})

	t.Run("no pollable duplicate available reports Unknown and writes nothing", func(t *testing.T) {
		withMakeRaw(t)
		withPollableDup(t, errors.New("no pollable duplicate"))
		inR, _ := newTestPipe(t)
		outR, outW := newTestPipe(t)

		if got := queryOSC11(inR, outW, 50*time.Millisecond); got != Unknown {
			t.Errorf("queryOSC11() = %v, want Unknown", got)
		}

		if err := outW.Close(); err != nil {
			t.Fatalf("outW.Close(): %v", err)
		}
		buf := make([]byte, 1)
		n, _ := outR.Read(buf)
		if n != 0 {
			t.Errorf("out carries %d unexpected byte(s), want 0", n)
		}
	})

	t.Run("a response before the deadline is read and classified", func(t *testing.T) {
		withMakeRaw(t)
		inR, inW := newTestPipe(t)
		outR, outW := newTestPipe(t)

		go func() {
			_, _ = inW.WriteString("\x1b]11;rgb:0000/0000/0000\a")
		}()

		if got := queryOSC11(inR, outW, 200*time.Millisecond); got != Dark {
			t.Errorf("queryOSC11() = %v, want Dark", got)
		}

		if err := outW.Close(); err != nil {
			t.Fatalf("outW.Close(): %v", err)
		}
		written, err := io.ReadAll(outR)
		if err != nil {
			t.Fatalf("reading what queryOSC11 wrote: %v", err)
		}
		if !bytes.Equal(written, oscQueryBytes) {
			t.Errorf("bytes written to out = %q, want %q", written, oscQueryBytes)
		}
	})

	t.Run("no response resolves Unknown after the deadline", func(t *testing.T) {
		withMakeRaw(t)
		inR, _ := newTestPipe(t)
		_, outW := newTestPipe(t)

		if got := queryOSC11(inR, outW, 20*time.Millisecond); got != Unknown {
			t.Errorf("queryOSC11() = %v, want Unknown", got)
		}
	})

	t.Run("a late response is drained, never left for the next reader", func(t *testing.T) {
		withMakeRaw(t)
		withShortOSCTimings(t)
		inR, inW := newTestPipe(t)
		_, outW := newTestPipe(t)

		// Written well after queryOSC11's own read deadline (10ms) but
		// inside its drain window (60ms budget), simulating a reply
		// that arrives late (the high-latency-link case).
		go func() {
			time.Sleep(30 * time.Millisecond)
			_, _ = inW.WriteString("\x1b]11;rgb:ffff/ffff/ffff\a")
		}()

		if got := queryOSC11(inR, outW, oscQueryTimeout); got != Unknown {
			t.Errorf("queryOSC11() = %v, want Unknown (timed out before the late reply)", got)
		}

		// By the time queryOSC11 returns, the drain has already run out
		// its full budget (past the 30ms write), so nothing should be
		// left on inR for a subsequent reader to pick up. queryOSC11
		// only ever closes the pollable duplicate it made of inR, never
		// inR itself, so this read against inR directly is exactly what
		// readline (or a scripted command) would see next.
		if err := inR.SetReadDeadline(time.Now().Add(20 * time.Millisecond)); err != nil {
			t.Fatalf("SetReadDeadline: %v", err)
		}
		buf := make([]byte, 32)
		n, err := inR.Read(buf)
		if n != 0 || !os.IsTimeout(err) {
			t.Errorf("bytes left after drain: n=%d err=%v, want 0 bytes and a timeout (nothing pending)", n, err)
		}
	})
}

// TestQueryOSC11DeadlineUnsupportedNeverWritesTheQuery pins R1: when no
// pollable duplicate of in can be made at all - the production
// scenario being os.Stdin, which Go never registers with its runtime
// poller (see pollableDup's doc comment) - queryOSC11 must never write
// the query to out. The buggy code wrote the query first and only
// discovered the deadline problem afterward, in readOSC11Response,
// leaving the reply (were one to arrive) stranded in the terminal's
// input buffer with nothing to drain it (see R1 in
// plans/debugger-colorizing.md).
func TestQueryOSC11DeadlineUnsupportedNeverWritesTheQuery(t *testing.T) {
	withMakeRaw(t)
	withPollableDup(t, errors.New("simulated: no pollable duplicate, matching os.Stdin"))

	inR, _ := newTestPipe(t)
	outR, outW := newTestPipe(t)

	if got := queryOSC11(inR, outW, 50*time.Millisecond); got != Unknown {
		t.Errorf("queryOSC11() = %v, want Unknown", got)
	}

	if err := outW.Close(); err != nil {
		t.Fatalf("outW.Close(): %v", err)
	}
	buf := make([]byte, 1)
	n, _ := outR.Read(buf)
	if n != 0 {
		t.Errorf("out carries %d unexpected byte(s), want 0: the query must never be written when no pollable duplicate of in is available (R1)", n)
	}
}

// TestQueryOSC11ClearsDeadlineAfterASuccessfulQuery pins R5: once
// queryOSC11 has classified a reply, the read deadline and non-
// blocking flag it armed on its pollable duplicate of in - which
// share in's own underlying open file description (see pollableDup's
// doc comment) - must be released, so whatever reads in next
// (readline, or a scripted command) does not inherit an already-
// expired deadline or a stray O_NONBLOCK and fail with a spurious
// error (see R5 in plans/debugger-colorizing.md). It uses the real
// defaultPollableDup (no seam override): inR is a plain os.Pipe() end,
// which is always poll-registrable, so this exercises the exact
// duplicate-and-reflag logic queryOSC11 uses in production.
func TestQueryOSC11ClearsDeadlineAfterASuccessfulQuery(t *testing.T) {
	withMakeRaw(t)
	inR, inW := newTestPipe(t)
	_, outW := newTestPipe(t)

	go func() {
		_, _ = inW.WriteString("\x1b]11;rgb:0000/0000/0000\a")
	}()

	if got := queryOSC11(inR, outW, 50*time.Millisecond); got != Dark {
		t.Fatalf("queryOSC11() = %v, want Dark", got)
	}

	// The 50ms deadline queryOSC11 armed on its duplicate of inR has
	// now elapsed; if it (or the duplicate's O_NONBLOCK flag, shared
	// with inR itself) was never released, this read - which sets no
	// deadline of its own - would inherit it and fail instead of
	// waiting for the byte inW is about to send.
	time.Sleep(60 * time.Millisecond)
	go func() {
		_, _ = inW.WriteString("x")
	}()
	buf := make([]byte, 1)
	n, err := inR.Read(buf)
	if err != nil {
		t.Errorf("Read after successful query = (n=%d, err=%v), want the byte with no error: the deadline and non-blocking flag queryOSC11 armed must be released once the query succeeds (R5)", n, err)
	}
}

func TestReadOSC11Response(t *testing.T) {
	t.Run("stops at BEL", func(t *testing.T) {
		inR, inW := newTestPipe(t)
		go func() { _, _ = inW.WriteString("\x1b]11;rgb:0/0/0\a") }()

		resp, timedOut := readOSC11Response(inR, time.Now().Add(200*time.Millisecond))
		if timedOut {
			t.Fatal("readOSC11Response() timed out, want a completed read")
		}
		if string(resp) != "\x1b]11;rgb:0/0/0\a" {
			t.Errorf("resp = %q", resp)
		}
	})

	t.Run("stops at ST", func(t *testing.T) {
		inR, inW := newTestPipe(t)
		go func() { _, _ = inW.WriteString("\x1b]11;rgb:0/0/0\x1b\\") }()

		resp, timedOut := readOSC11Response(inR, time.Now().Add(200*time.Millisecond))
		if timedOut {
			t.Fatal("readOSC11Response() timed out, want a completed read")
		}
		if string(resp) != "\x1b]11;rgb:0/0/0\x1b\\" {
			t.Errorf("resp = %q", resp)
		}
	})

	t.Run("caps at oscResponseCap with no terminator", func(t *testing.T) {
		inR, inW := newTestPipe(t)
		garbage := make([]byte, oscResponseCap+16)
		for i := range garbage {
			garbage[i] = 'x'
		}
		go func() { _, _ = inW.Write(garbage) }()

		resp, timedOut := readOSC11Response(inR, time.Now().Add(500*time.Millisecond))
		if timedOut {
			t.Fatal("readOSC11Response() timed out, want it to stop at the cap instead")
		}
		if len(resp) != oscResponseCap {
			t.Errorf("len(resp) = %d, want %d", len(resp), oscResponseCap)
		}
	})

	t.Run("reports timedOut when the deadline passes first", func(t *testing.T) {
		inR, _ := newTestPipe(t)
		resp, timedOut := readOSC11Response(inR, time.Now().Add(10*time.Millisecond))
		if !timedOut {
			t.Fatal("readOSC11Response() did not time out, want it to")
		}
		if len(resp) != 0 {
			t.Errorf("resp = %q, want empty", resp)
		}
	})
}

func TestDrainPending(t *testing.T) {
	t.Run("discards bytes arriving during the budget", func(t *testing.T) {
		inR, inW := newTestPipe(t)
		go func() {
			time.Sleep(10 * time.Millisecond)
			_, _ = inW.WriteString("stray")
		}()

		drainPending(inR, 50*time.Millisecond)

		if err := inR.SetReadDeadline(time.Now().Add(10 * time.Millisecond)); err != nil {
			t.Fatalf("SetReadDeadline: %v", err)
		}
		buf := make([]byte, 8)
		n, err := inR.Read(buf)
		if n != 0 || !os.IsTimeout(err) {
			t.Errorf("bytes left after drainPending: n=%d err=%v, want 0 bytes and a timeout", n, err)
		}
	})

	t.Run("returns without hanging when nothing ever arrives", func(t *testing.T) {
		inR, _ := newTestPipe(t)
		start := time.Now()
		drainPending(inR, 30*time.Millisecond)
		if elapsed := time.Since(start); elapsed < 30*time.Millisecond {
			t.Errorf("drainPending returned after %s, want it to use its full budget", elapsed)
		}
	})
}

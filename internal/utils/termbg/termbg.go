// Package termbg detects whether a terminal's background is dark or
// light, so a caller can choose a legible color palette without asking
// the user. Detection never blocks or writes to a stream it cannot
// safely probe: a non-terminal stream, a multiplexer, and a terminal
// that never answers are all reported as Unknown, leaving the caller
// free to fall back to a documented default.
package termbg

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mattn/go-isatty"
	"golang.org/x/term"
)

// Background is a terminal's detected background brightness.
type Background int

const (
	// Unknown means detection did not run, or ran and could not tell.
	Unknown Background = iota
	// Dark is a detected dark background.
	Dark
	// Light is a detected light background.
	Light
)

// IsTerminal reports whether f is a terminal. A function var, matching
// cmd/graft's isStderrTTY/isStdoutTTY and debugInputIsInteractive, so
// tests can simulate a terminal (or its absence) without a real pty.
var IsTerminal = func(f *os.File) bool { return isatty.IsTerminal(f.Fd()) }

// oscQueryTimeout bounds how long Detect waits for a terminal to answer
// the OSC 11 query before giving up and reporting Unknown (the caller's
// documented dark fallback). A package var, not a const, so tests can
// shrink it to keep the timeout and drain-on-timeout paths fast without
// waiting out a real 150ms per case.
var oscQueryTimeout = 150 * time.Millisecond

// Detect guesses in and out's shared terminal's background. It guards
// against every context where guessing would be unreliable or where the
// live OSC 11 query could hang or leak stray bytes: either stream not
// being a terminal, a multiplexer in the way (a non-empty TMUX, or a
// TERM starting with "screen" or "tmux"), an unset or "dumb" TERM, and a
// terminal-emulating editor (a non-empty INSIDE_EMACS). Past every
// guard it consults the COLORFGBG environment variable first - no I/O,
// and often already set correctly by the terminal or its user - and
// only when that is unset or unparseable does it fall back to the OSC
// 11 query, the one path in this package that touches the terminal
// directly (see queryOSC11).
//
// The guard order is deliberate: the multiplexer/terminal guards run
// before COLORFGBG is even read, so a value merely inherited from a
// parent shell inside tmux can never be trusted as this terminal's own;
// COLORFGBG runs before the query because the query is the riskier
// path (it can be stale after a mid-session terminal theme switch,
// where COLORFGBG's own staleness is the accepted trade - see
// plans/debugger-colorizing.md decision 10).
func Detect(in, out *os.File) Background {
	if !IsTerminal(in) || !IsTerminal(out) {
		return Unknown
	}
	if os.Getenv("TMUX") != "" {
		return Unknown
	}
	termEnv := os.Getenv("TERM")
	if strings.HasPrefix(termEnv, "screen") || strings.HasPrefix(termEnv, "tmux") {
		return Unknown
	}
	if termEnv == "" || termEnv == "dumb" || os.Getenv("INSIDE_EMACS") != "" {
		return Unknown
	}
	if bg := ParseColorFGBG(os.Getenv("COLORFGBG")); bg != Unknown {
		return bg
	}
	return queryOSC11(in, out, oscQueryTimeout)
}

// ParseColorFGBG parses the COLORFGBG environment variable's "fg;bg" or
// "fg;default;bg" form (the rxvt/urxvt/Konsole convention) and
// classifies its background palette index: 0-6 and 8 are the dark
// palette slots, 7 and 9-15 are light. An empty value, a value with
// fewer than two ";"-separated fields, or a background field that is
// not an integer in 0-15 all report Unknown rather than guessing.
func ParseColorFGBG(v string) Background {
	fields := strings.Split(v, ";")
	if len(fields) < 2 {
		return Unknown
	}
	bg, err := strconv.Atoi(fields[len(fields)-1])
	if err != nil {
		return Unknown
	}
	switch {
	case bg == 7 || (bg >= 9 && bg <= 15):
		return Light
	case bg == 8 || (bg >= 0 && bg <= 6):
		return Dark
	default:
		return Unknown
	}
}

// oscQueryBytes is the literal OSC 11 background query graft writes to
// a terminal: "what is your background color?", ST-terminated (the
// modern form); terminals reply with whichever terminator they
// themselves use (BEL or ST), and parseOSC11Response accepts either.
var oscQueryBytes = []byte("\x1b]11;?\x1b\\")

// oscResponseCap bounds how many bytes queryOSC11 reads for one
// response: a real reply is well under 32 bytes
// ("\x1b]11;rgb:ffff/ffff/ffff\x1b\\" is 28), so 64 is generous headroom
// without risking an unbounded read against a terminal that replies
// with garbage and no terminator at all.
const oscResponseCap = 64

// oscDrainBudget bounds the total extra time queryOSC11 spends, past
// its own read deadline, discarding whatever the terminal still sends -
// the hard outer bound the plan calls for, so a terminal that keeps
// writing cannot hang the debugger's own startup. A package var so
// tests can shrink it.
var oscDrainBudget = 200 * time.Millisecond

// oscDrainStep is the read deadline used for each individual read
// during the drain: short enough that a quiet terminal costs little,
// long enough that a response arriving anywhere within oscDrainBudget
// (the high-latency-link case QA calls out) still gets caught and
// discarded rather than left to leak into whatever reads the terminal
// next. A package var so tests can shrink it.
var oscDrainStep = 20 * time.Millisecond

// rawFd returns f's underlying file descriptor without disabling its
// SetReadDeadline support - unlike (*os.File).Fd, whose own doc comment
// warns "SetDeadline methods will stop working" for any caller of it, a
// footgun that would silently turn every deadline in this file into a
// real, unbounded blocking read the moment queryOSC11 asked for in's fd
// to hand to term.MakeRaw (or pollableDup asked for it to duplicate).
// SyscallConn's Control callback hands out the fd without that side
// effect, which is the whole reason this package never calls
// (*os.File).Fd() on any stream it might still need to read a bounded
// reply from.
func rawFd(f *os.File) (int, error) {
	rc, err := f.SyscallConn()
	if err != nil {
		return 0, err
	}
	var fd int
	if ctrlErr := rc.Control(func(descriptor uintptr) { fd = int(descriptor) }); ctrlErr != nil {
		return 0, ctrlErr
	}
	return fd, nil
}

// makeRawSeam enters raw mode on fd and returns a function that
// restores the terminal's previous state, so queryOSC11's own tests
// (and Detect's, through it) never have to touch a real tty's termios:
// production wraps golang.org/x/term, already an indirect module
// dependency this package promotes to direct use; tests substitute a
// trivial success so the read/parse/timeout/drain logic below runs
// against an os.Pipe() pair instead.
var makeRawSeam = func(fd int) (restore func(), err error) {
	prev, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	return func() { _ = term.Restore(fd, prev) }, nil
}

// clearReadDeadline releases f's read deadline once queryOSC11 is done
// needing one, so a caller that later reads the same handle can never
// inherit an already-expired deadline armed for this query alone (see
// R5 in plans/debugger-colorizing.md). Its error is deliberately
// ignored: by the point this runs, f already answered a real
// SetReadDeadline call successfully once (see queryOSC11's probe), so
// this call clearing it back to the zero value is not expected to fail
// on any stream this package still holds open; even if the handle went
// bad between then and now, there is nothing left to leak a deadline
// onto.
func clearReadDeadline(f *os.File) {
	_ = f.SetReadDeadline(time.Time{})
}

// queryOSC11 is Detect's last resort, past every guard and an unset or
// unparseable COLORFGBG: it asks the terminal directly. Raw mode is
// entered on in's own descriptor and restored on every return path
// (the deferred restore, set up immediately once confirmed entered,
// before any output is written or any byte is read). The read itself
// goes through a pollable duplicate of in (see pollableDup), released
// - deadline cleared, flags restored, duplicate closed - on every
// return path as well. Critically, that duplicate's read deadline is
// probed before a single byte is written: a stream that cannot honor a
// deadline at all gets no query, because there would be nowhere safe
// for its reply to land (see R1 in plans/debugger-colorizing.md). Any
// failure along the way - raw mode unavailable, no pollable duplicate,
// the deadline unsupported, the write itself failing, a response that
// never parses - reports Unknown rather than propagating an error:
// this is a best-effort probe with a documented fallback, never
// something a caller must handle.
func queryOSC11(in, out *os.File, timeout time.Duration) Background {
	fd, err := rawFd(in)
	if err != nil {
		return Unknown
	}
	restore, err := makeRawSeam(fd)
	if err != nil {
		return Unknown
	}
	defer restore()

	queryIn, release, err := pollableDup(in)
	if err != nil {
		return Unknown
	}
	defer release()

	deadline := time.Now().Add(timeout)
	if err := queryIn.SetReadDeadline(deadline); err != nil {
		// Cannot honor a deadline on this handle at all: report Unknown
		// without writing the query, rather than send it and have no
		// bounded way to read - or drain - whatever reply comes back.
		return Unknown
	}
	defer clearReadDeadline(queryIn)

	if _, err := out.Write(oscQueryBytes); err != nil {
		return Unknown
	}

	resp, timedOut := readOSC11Response(queryIn, deadline)
	if timedOut {
		// A response that arrives after this point must never reach
		// whatever reads in next (readline, or a scripted command): stay
		// in raw mode and drain it here instead.
		drainPending(queryIn, oscDrainBudget)
		return Unknown
	}

	bg, ok := parseOSC11Response(resp)
	if !ok {
		return Unknown
	}
	return bg
}

// readOSC11Response reads one OSC 11 reply from in, stopping as soon as
// it sees the BEL or ST terminator, hits oscResponseCap, or deadline
// passes. timedOut is true only when deadline was the reason reading
// stopped; the caller must then drain (see drainPending) before giving
// up the terminal, since a reply already in flight can still land after
// this returns. Any other read error (not a timeout) reports
// timedOut=false: there is nothing to drain from a dead or closed
// stream.
func readOSC11Response(in *os.File, deadline time.Time) (resp []byte, timedOut bool) {
	buf := make([]byte, 0, oscResponseCap)
	one := make([]byte, 1)
	for len(buf) < oscResponseCap {
		if err := in.SetReadDeadline(deadline); err != nil {
			// Can't honor a deadline on this stream at all: treat what
			// has been read so far as final rather than risk a hang.
			return buf, false
		}
		n, err := in.Read(one)
		if n > 0 {
			buf = append(buf, one[0])
			if one[0] == '\a' {
				return buf, false
			}
			if len(buf) >= 2 && buf[len(buf)-2] == '\x1b' && buf[len(buf)-1] == '\\' {
				return buf, false
			}
			continue
		}
		if err != nil {
			if os.IsTimeout(err) {
				return buf, true
			}
			return buf, false
		}
	}
	return buf, false
}

// drainPending discards whatever arrives on in for up to budget, in
// short reads bounded by oscDrainStep each, so a response that shows up
// after readOSC11Response's own deadline still cannot leak into
// whatever reads in next. It keeps polling for the entire budget - not
// just until one read comes up empty - because a reply can legitimately
// trickle in partway through that window (a high-latency link, for
// example); it only stops early on a genuine I/O error, since there is
// nothing left to drain from a stream that has gone away.
func drainPending(in *os.File, budget time.Duration) {
	deadline := time.Now().Add(budget)
	buf := make([]byte, oscResponseCap)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return
		}
		step := oscDrainStep
		if remaining < step {
			step = remaining
		}
		if err := in.SetReadDeadline(time.Now().Add(step)); err != nil {
			return
		}
		n, err := in.Read(buf)
		if n == 0 && err != nil && !os.IsTimeout(err) {
			return
		}
	}
}

// parseOSC11Response parses one raw OSC 11 reply: an optional leading
// ESC and its "]11;" introducer, "rgb:" or "rgba:", then one to four hex
// digits per channel separated by "/", terminated by BEL ("\a") or ST
// ("\x1b\\") - either terminator, or none at all, is accepted, since the
// caller may have already stripped it. Each channel is normalized to a
// 0..1 fraction of its own digit width (a 2-digit "1e" is 0x1e/0xff, a
// 4-digit "1e1e" is 0x1e1e/0xffff, and so on), which is exactly the X11
// RGB Device String scaling for the luminance math that follows, so no
// bit-replication step is needed. Anything that does not match - a
// missing prefix, fewer than three channels, a non-hex or empty/over-
// long channel - reports Unknown, ok=false rather than guessing.
func parseOSC11Response(raw []byte) (Background, bool) {
	s := string(raw)
	s = strings.TrimSuffix(s, "\x1b\\")
	s = strings.TrimSuffix(s, "\a")
	s = strings.TrimPrefix(s, "\x1b")
	s = strings.TrimPrefix(s, "]11;")

	var body string
	switch {
	case strings.HasPrefix(s, "rgba:"):
		body = strings.TrimPrefix(s, "rgba:")
	case strings.HasPrefix(s, "rgb:"):
		body = strings.TrimPrefix(s, "rgb:")
	default:
		return Unknown, false
	}

	parts := strings.Split(body, "/")
	if len(parts) < 3 {
		return Unknown, false
	}

	r, ok := parseOSCChannel(parts[0])
	if !ok {
		return Unknown, false
	}
	g, ok := parseOSCChannel(parts[1])
	if !ok {
		return Unknown, false
	}
	b, ok := parseOSCChannel(parts[2])
	if !ok {
		return Unknown, false
	}

	luminance := 0.2126*r + 0.7152*g + 0.0722*b
	if luminance < 0.5 {
		return Dark, true
	}
	return Light, true
}

// parseOSCChannel parses one 1-to-4 hex digit color channel from an OSC
// 11 response, normalized to a 0..1 fraction of its own digit width
// (see parseOSC11Response). An empty string, a string longer than 4
// characters, or one containing a non-hex-digit all report ok=false.
func parseOSCChannel(h string) (float64, bool) {
	if h == "" || len(h) > 4 {
		return 0, false
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return 0, false
	}
	maxVal := uint64(1)<<(4*uint(len(h))) - 1
	return float64(v) / float64(maxVal), true
}

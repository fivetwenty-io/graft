package graft

import (
	"bytes"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/log"
)

// saveLogState snapshots the process-global github.com/fivetwenty-io/graft/log
// state (DebugOn, TraceOn, Writer) and returns a func that restores it,
// isolating tests that exercise WithTraceOutput/WithTraceLevel/
// WithDebugLogging (all of which mutate that global state, by design - see
// WithTraceOutput's doc comment) from one another and from any other test
// in the package that relies on the log package's default state.
func saveLogState(t *testing.T) func() {
	t.Helper()
	debugOn, traceOn, writer := log.DebugOn, log.TraceOn, log.Writer
	return func() {
		log.DebugOn, log.TraceOn, log.Writer = debugOn, traceOn, writer
	}
}

// TestWithTraceOutput_ObservableEffect proves WithTraceOutput actually
// redirects DEBUG/TRACE output, by capturing it in a buffer instead of
// merely asserting EngineOptions.TraceOutput was set.
func TestWithTraceOutput_ObservableEffect(t *testing.T) {
	defer saveLogState(t)()

	var buf bytes.Buffer
	_, err := NewEngine(WithTraceOutput(&buf), WithTraceLevel(TraceLevelDebug))
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	DEBUG("hello %s", "trace")

	if got := buf.String(); !strings.Contains(got, "DEBUG> hello trace") {
		t.Fatalf("captured output = %q, want it to contain %q", got, "DEBUG> hello trace")
	}
}

// TestWithTraceOutput_NilIsNoOp proves a nil writer leaves any previously
// configured destination untouched instead of nil-ing log.Writer out from
// under a concurrently configured engine.
func TestWithTraceOutput_NilIsNoOp(t *testing.T) {
	defer saveLogState(t)()

	var buf bytes.Buffer
	log.Writer = &buf

	if _, err := NewEngine(WithTraceOutput(nil)); err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	if log.Writer != &buf {
		t.Fatal("WithTraceOutput(nil) overwrote a previously configured log.Writer")
	}
}

// TestWithTraceLevel_ObservableEffect proves each TraceLevel value produces
// the DEBUG/TRACE gating the plan documents: TraceLevelTrace enables both,
// TraceLevelDebug enables only DEBUG, and TraceLevelNone disables both.
func TestWithTraceLevel_ObservableEffect(t *testing.T) {
	cases := []struct {
		name          string
		level         TraceLevel
		wantDebugText bool
		wantTraceText bool
	}{
		{"TraceLevelNone", TraceLevelNone, false, false},
		{"TraceLevelDebug", TraceLevelDebug, true, false},
		{"TraceLevelTrace", TraceLevelTrace, true, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer saveLogState(t)()

			var buf bytes.Buffer
			if _, err := NewEngine(WithTraceOutput(&buf), WithTraceLevel(tc.level)); err != nil {
				t.Fatalf("NewEngine failed: %v", err)
			}

			DEBUG("debug-marker")
			TRACE("trace-marker")

			got := buf.String()
			if strings.Contains(got, "debug-marker") != tc.wantDebugText {
				t.Errorf("%s: DEBUG output present = %v, want %v (output: %q)", tc.name, strings.Contains(got, "debug-marker"), tc.wantDebugText, got)
			}
			if strings.Contains(got, "trace-marker") != tc.wantTraceText {
				t.Errorf("%s: TRACE output present = %v, want %v (output: %q)", tc.name, strings.Contains(got, "trace-marker"), tc.wantTraceText, got)
			}
		})
	}
}

// TestWithTraceLevel_UnconfiguredLeavesLoggingUntouched proves an engine
// constructed without WithTraceLevel does not disturb log.DebugOn/TraceOn -
// the critical guard against clobbering a CLI -d/-t flag that was set
// before engine construction.
func TestWithTraceLevel_UnconfiguredLeavesLoggingUntouched(t *testing.T) {
	defer saveLogState(t)()

	log.DebugOn = true
	log.TraceOn = true

	if _, err := NewEngine(); err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	if !log.DebugOn || !log.TraceOn {
		t.Fatal("NewEngine() with no trace/debug options changed log.DebugOn/TraceOn; unconfigured construction must be a no-op on process logging state")
	}
}

// TestWithDebugLogging_ObservableEffect proves WithDebugLogging toggles
// log.DebugOn (the same knob DEBUG checks), and that WithTraceLevel takes
// precedence when both are supplied.
func TestWithDebugLogging_ObservableEffect(t *testing.T) {
	t.Run("enables DEBUG output", func(t *testing.T) {
		defer saveLogState(t)()
		var buf bytes.Buffer
		if _, err := NewEngine(WithTraceOutput(&buf), WithDebugLogging(true)); err != nil {
			t.Fatalf("NewEngine failed: %v", err)
		}
		DEBUG("debug-via-legacy-toggle")
		if !strings.Contains(buf.String(), "debug-via-legacy-toggle") {
			t.Fatalf("WithDebugLogging(true): DEBUG output missing, got %q", buf.String())
		}
	})

	t.Run("WithTraceLevel wins when both are supplied", func(t *testing.T) {
		defer saveLogState(t)()
		var buf bytes.Buffer
		// WithDebugLogging(true) requests DEBUG on; WithTraceLevel(TraceLevelNone)
		// requests both off. Per applyLogging's documented precedence,
		// TraceLevel wins, so DEBUG stays off regardless of option order.
		if _, err := NewEngine(WithDebugLogging(true), WithTraceOutput(&buf), WithTraceLevel(TraceLevelNone)); err != nil {
			t.Fatalf("NewEngine failed: %v", err)
		}
		DEBUG("should-not-appear")
		if strings.Contains(buf.String(), "should-not-appear") {
			t.Fatal("WithTraceLevel(TraceLevelNone) did not win over WithDebugLogging(true)")
		}
	})
}

// TestConfigure_AppliesDeltaAndKeepsSkipNatsInSync is the red test for the
// skipNats staleness bug: UpdateOptions/Configure historically synced
// skipVault and skipAws from EngineOptions but left skipNats (and
// IsNATSSkipped) stale.
func TestConfigure_AppliesDeltaAndKeepsSkipNatsInSync(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	de := engine.(*DefaultEngine)

	if de.IsNATSSkipped() {
		t.Fatal("expected NATS not skipped before Configure")
	}

	if err := de.Configure(WithSkipNats(true)); err != nil {
		t.Fatalf("Configure failed: %v", err)
	}
	if !de.IsNATSSkipped() {
		t.Fatal("Configure(WithSkipNats(true)) did not update IsNATSSkipped()")
	}
	if !de.opts.SkipNats {
		t.Fatal("Configure(WithSkipNats(true)) did not update opts.SkipNats")
	}

	// Prove Configure applies a delta, not a full reset: a field set
	// before Configure but not touched by this call (SkipVault) must
	// survive it.
	if err := de.Configure(WithSkipVault(true)); err != nil {
		t.Fatalf("Configure failed: %v", err)
	}
	if !de.IsVaultSkipped() {
		t.Fatal("Configure(WithSkipVault(true)) did not update IsVaultSkipped()")
	}
	if !de.IsNATSSkipped() {
		t.Fatal("Configure applied as a full reset instead of a delta: SkipNats from the prior Configure call was lost")
	}
}

// TestConfigure_RejectsInvalidConcurrency proves Configure validates the
// resulting configuration (mirroring NewEngine's construction-time check)
// and leaves the engine's existing configuration untouched on failure.
func TestConfigure_RejectsInvalidConcurrency(t *testing.T) {
	engine, err := NewEngine(WithConcurrency(5))
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	de := engine.(*DefaultEngine)

	if err := de.Configure(WithConcurrency(-1)); err == nil {
		t.Fatal("Configure(WithConcurrency(-1)) succeeded, want an error")
	}
	if de.opts.MaxConcurrency != 5 {
		t.Fatalf("Configure with an invalid delta changed MaxConcurrency to %d, want it left at 5", de.opts.MaxConcurrency)
	}
}

// TestConfigure_RebuildsCache proves Configure's cache-related options take
// effect on the live engine, not only on a freshly constructed one.
func TestConfigure_RebuildsCache(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	de := engine.(*DefaultEngine)
	if de.GetCache() == nil {
		t.Fatal("expected a cache instance by default")
	}

	if err := de.Configure(WithCacheDisabled()); err != nil {
		t.Fatalf("Configure failed: %v", err)
	}
	if de.GetCache() != nil {
		t.Fatal("Configure(WithCacheDisabled()) did not remove the cache instance")
	}
}

// TestUpdateOptions_SyncsSkipNats covers the same bug via UpdateOptions,
// which took the fix in the same motion as Configure.
func TestUpdateOptions_SyncsSkipNats(t *testing.T) {
	engine := NewDefaultEngine()
	engine.UpdateOptions(EngineOptions{SkipNats: true})
	if !engine.IsNATSSkipped() {
		t.Fatal("UpdateOptions(EngineOptions{SkipNats: true}) did not update IsNATSSkipped()")
	}
}

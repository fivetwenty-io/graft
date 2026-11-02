package graft

import (
	"strings"
	"sync"
	"testing"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
)

// externalDiffable lacks graft's unexported render method, standing in for
// an implementation supplied by a caller outside this package.
type externalDiffable struct{ out string }

func (e externalDiffable) Changed() bool            { return true }
func (e externalDiffable) String(key string) string { return key + ":" + e.out }
func (e externalDiffable) Value() interface{}       { return nil }

func plainTestDiff(t *testing.T) Diffable {
	t.Helper()
	a := map[string]interface{}{
		"keep":   "same",
		"gone":   "old",
		"scalar": "before",
		"type":   "text",
		"list":   []interface{}{"a", "b"},
		"keyed":  []interface{}{map[string]interface{}{"name": "x", "v": 1}},
	}
	b := map[string]interface{}{
		"keep":   "same",
		"added":  map[string]interface{}{"k": "v"},
		"scalar": "after",
		"type":   []interface{}{"text"},
		"list":   []interface{}{"a", "c", "d"},
		"keyed":  []interface{}{map[string]interface{}{"name": "x", "v": 2}, map[string]interface{}{"name": "y"}},
	}
	d, err := Diff(a, b)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	return d
}

func withColor(t *testing.T, on bool) {
	t.Helper()
	prev := ansi.IsColorEnabled()
	ansi.Color(on)
	t.Cleanup(func() { ansi.Color(prev) })
}

func TestPlainString(t *testing.T) {
	withColor(t, true)
	d := plainTestDiff(t)

	colored := d.String("$")
	if !strings.Contains(colored, "\x1b") {
		t.Fatalf("expected String to emit escapes with the toggle on")
	}

	plain := PlainString(d, "$")
	if strings.Contains(plain, "\x1b") {
		t.Errorf("PlainString contains an escape: %q", plain)
	}
	if want := ansi.StripEscapes(colored); plain != want {
		t.Errorf("PlainString != stripped String\n got: %q\nwant: %q", plain, want)
	}
	if !ansi.IsColorEnabled() {
		t.Errorf("PlainString changed the package-wide toggle")
	}

	ansi.Color(false)
	if got := PlainString(d, "$"); got != plain {
		t.Errorf("PlainString differs with toggle off")
	}
	if ansi.IsColorEnabled() {
		t.Errorf("PlainString changed the package-wide toggle")
	}
}

func TestStringUnchangedByToggle(t *testing.T) {
	d := plainTestDiff(t)
	withColor(t, false)
	off := d.String("$")
	ansi.Color(true)
	on := d.String("$")
	if ansi.StripEscapes(on) != off {
		t.Errorf("String with color on differs from off beyond escapes")
	}
	if !strings.Contains(on, "\x1b") {
		t.Errorf("String with toggle on should still color")
	}
}

func TestPlainStringExternalFallback(t *testing.T) {
	withColor(t, true)
	e := externalDiffable{out: "\x1b[31mred\x1b[0m"}
	if got := PlainString(e, "k"); got != "k:red" {
		t.Errorf("fallback = %q", got)
	}
}

func TestPlainStringConcurrentWithString(t *testing.T) {
	withColor(t, true)
	d := plainTestDiff(t)
	want := PlainString(d, "$")

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if got := PlainString(d, "$"); got != want {
					t.Errorf("PlainString varied under concurrency")
					return
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 200; j++ {
			_ = d.String("$")
		}
	}()
	wg.Wait()
}

// wrappedDiffMap embeds a graft Diffable and overrides String, so it
// inherits the unexported render method and must still use its own String.
type wrappedDiffMap struct{ DiffMap }

func (w wrappedDiffMap) String(key string) string { return "WRAPPED:" + w.DiffMap.String(key) }

func TestPlainStringEmbeddingType(t *testing.T) {
	withColor(t, true)
	inner, ok := plainTestDiff(t).(DiffMap)
	if !ok {
		t.Fatal("expected a DiffMap")
	}
	w := wrappedDiffMap{inner}
	got := PlainString(w, "$")
	if want := ansi.StripEscapes(w.String("$")); got != want {
		t.Errorf("embedder\n got: %q\nwant: %q", got, want)
	}
	if !strings.HasPrefix(got, "WRAPPED:") {
		t.Errorf("override skipped: %q", got)
	}

	// An embedder nested inside a graft diff is reached through String too.
	outer := DiffMap{Common: map[string]Diffable{"w": w}}
	if got := PlainString(outer, "$"); !strings.Contains(got, "WRAPPED:") || strings.Contains(got, "\x1b") {
		t.Errorf("nested embedder: %q", got)
	}
}

func TestPlainStringKeepsUserTextVerbatim(t *testing.T) {
	for _, on := range []bool{true, false} {
		withColor(t, on)
		// Built with the toggle in the state under test, so nothing may be
		// baked into the diff at construction.
		d, err := Diff(
			map[string]interface{}{"@R{k}": "old @R{x}", "n": map[string]interface{}{"@G{z}": 1}},
			map[string]interface{}{"@R{k}": "new @G{y}", "n": map[string]interface{}{"@G{z}": 2}, "@B{add}": "@R{v}"},
		)
		if err != nil {
			t.Fatal(err)
		}
		got := PlainString(d, "@R{root}")
		for _, want := range []string{
			"@R{root}.@R{k} changed value",
			"old @R{x}", "new @G{y}",
			"@R{root}.n.@G{z} changed value",
			"@R{root}.@B{add} added",
			"@R{v}",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("toggle=%v: missing %q in\n%s", on, want, got)
			}
		}
		if strings.Contains(got, "\x1b") {
			t.Errorf("toggle=%v: escape in %q", on, got)
		}
		if ansi.IsColorEnabled() != on {
			t.Errorf("toggle changed")
		}
	}
}

func TestPlainStringConcurrentWithToggleFlips(t *testing.T) {
	withColor(t, true)
	d := plainTestDiff(t)

	stop := make(chan struct{})
	var flipper sync.WaitGroup
	flipper.Add(1)
	go func() {
		defer flipper.Done()
		for {
			select {
			case <-stop:
				return
			default:
				ansi.Color(true)
				ansi.Color(false)
			}
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if got := PlainString(d, "@R{$}"); strings.Contains(got, "\x1b") || got == "" {
					t.Errorf("bad PlainString under toggle flips: %q", got)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(stop)
	flipper.Wait()
}

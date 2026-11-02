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

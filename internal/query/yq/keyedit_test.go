package yq

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
)

// renameAllocs is the bytes allocated renaming every key of a flat map of
// n entries.
func renameAllocs(t *testing.T, n int) uint64 {
	t.Helper()
	var sb strings.Builder
	for i := range n {
		fmt.Fprintf(&sb, "k%d: %d\n", i, i)
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	out, err := runYQ(t, testOpts, `(.. | key) |= "r" + .`, sb.String(), false)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	if want := "rk0: 0\n"; !strings.HasPrefix(out, want) {
		t.Fatalf("output starts %.20q, want %q", out, want)
	}
	return after.TotalAlloc - before.TotalAlloc
}

func TestRenamingKeysCostsLinearlyInTheMapSize(t *testing.T) {
	small, large := renameAllocs(t, 500), renameAllocs(t, 2000)
	// Four times the keys costs about four times the bytes when a rename
	// touches only its own entry, and sixteen times when each one copies
	// the map.
	if ratio := float64(large) / float64(small); ratio > 8 {
		t.Fatalf("2000 keys allocated %d bytes against %d for 500, a ratio of %.1f, want under 8", large, small, ratio)
	}
}

func TestRenamingKeysKeepsEarlierRenames(t *testing.T) {
	in := "a: {b: 1, c: 2}\nd: 3\n"
	wantYQ(t, `(.. | key) |= "x" + .`, in, "xa: {xb: 1, xc: 2}\nxd: 3\n")
	wantYQ(t, `((.a.b | key), (.a | key), (.d | key)) = "z"`, in, "z: {z: 1, c: 2}\nz: 3\n")
}

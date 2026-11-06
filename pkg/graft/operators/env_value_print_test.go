package operators_test

import (
	"testing"
)

// envPrintRow holds the output spruce 1.35.17 gives when join or concat
// prints the value of $P5_ENV, whether the variable is an argument of its
// own or a leaf of a || chain. Each output was taken from the real spruce
// binary. spruce prints an env value whole, the way Go's %v prints it, so
// a list comes out as [a b] and a null as <nil>.
type envPrintRow struct {
	value, join, joinOr, concatOr, joinFallback, concatFallback string
}

var envPrintSpruce = []envPrintRow{
	{"yes", "v: s,true", "v: s,true", "v: x-true", "v: s,true", "v: x-true"},
	{"8080", "v: s,8080", "v: s,8080", "v: x-8080", "v: s,8080", "v: x-8080"},
	{"01234", "v: s,668", "v: s,668", "v: x-668", "v: s,668", "v: x-668"},
	{"~", "v: s,<nil>", "v: s,<nil>", "v: x-<nil>", "v: s,<nil>", "v: x-<nil>"},
	{"   ", "v: s,<nil>", "v: s,<nil>", "v: x-<nil>", "v: s,<nil>", "v: x-<nil>"},
	{"hello", "v: s,hello", "v: s,hello", "v: x-hello", "v: s,hello", "v: x-hello"},
	{"\"q\"", "v: s,\"q\"", "v: s,\"q\"", "v: x-\"q\"", "v: s,\"q\"", "v: x-\"q\""},
	{"[a, b]", "v: s,[a b]", "v: s,[a b]", "v: x-[a b]", "v: s,[a b]", "v: x-[a b]"},
	{"[yes, no]", "v: s,[true false]", "v: s,[true false]", "v: x-[true false]", "v: s,[true false]", "v: x-[true false]"},
	{"- a", "v: s,[a]", "v: s,[a]", "v: x-[a]", "v: s,[a]", "v: x-[a]"},
	{"- ", "v: s,[<nil>]", "v: s,[<nil>]", "v: x-[<nil>]", "v: s,[<nil>]", "v: x-[<nil>]"},
	{"a: b", "v: s,map[a:b]", "v: s,map[a:b]", "v: x-map[a:b]", "v: s,map[a:b]", "v: x-map[a:b]"},
	{"{a: yes}", "v: s,map[a:true]", "v: s,map[a:true]", "v: x-map[a:true]", "v: s,map[a:true]", "v: x-map[a:true]"},
	{"{a: [1, {b: yes}]}", "v: s,map[a:[1 map[b:true]]]", "v: s,map[a:[1 map[b:true]]]", "v: x-map[a:[1 map[b:true]]]", "v: s,map[a:[1 map[b:true]]]", "v: x-map[a:[1 map[b:true]]]"},
	{"- a: 1\n  b: [x, 2]", "v: s,[map[a:1 b:[x 2]]]", "v: s,[map[a:1 b:[x 2]]]", "v: x-[map[a:1 b:[x 2]]]", "v: s,[map[a:1 b:[x 2]]]", "v: x-[map[a:1 b:[x 2]]]"},
	{"[[1, 2]]", "v: s,[[1 2]]", "v: s,[[1 2]]", "v: x-[[1 2]]", "v: s,[[1 2]]", "v: x-[[1 2]]"},
	{"[", "v: s,[", "v: s,[", "v: x-[", "v: s,[", "v: x-["},
	{"1\n---\n2", "v: s,1", "v: s,1", "v: x-1", "v: s,1", "v: x-1"},
}

// TestEnvValuePrintsLikeSpruce runs every row under join and under join
// and concat with the variable on either side of a || chain.
func TestEnvValuePrintsLikeSpruce(t *testing.T) {
	shapes := []struct {
		name, src string
		want      func(envPrintRow) string
	}{
		{"join", "v: (( join \",\" \"s\" $P5_ENV ))\n", func(r envPrintRow) string { return r.join }},
		{"join-or", "v: (( join \",\" \"s\" $P5_ENV || \"d\" ))\n", func(r envPrintRow) string { return r.joinOr }},
		{"concat-or", "v: (( concat \"x-\" $P5_ENV || \"d\" ))\n", func(r envPrintRow) string { return r.concatOr }},
		{"join-fallback", "v: (( join \",\" \"s\" $P5_UNSET || $P5_ENV ))\n", func(r envPrintRow) string { return r.joinFallback }},
		{"concat-fallback", "v: (( concat \"x-\" $P5_UNSET || $P5_ENV ))\n", func(r envPrintRow) string { return r.concatFallback }},
	}
	for _, row := range envPrintSpruce {
		for _, sh := range shapes {
			t.Run(sh.name+"/"+row.value, func(t *testing.T) {
				t.Setenv("P5_ENV", row.value)
				t.Setenv("P5_UNSET", "")
				if got, want := mergeEnvShape(t, sh.src), sh.want(row); got != want {
					t.Errorf("P5_ENV=%q under %s:\n got: %q\nwant: %q", row.value, sh.name, got, want)
				}
			})
		}
	}
}

// TestJoinReferenceFallbackStillJoins checks that a list reached through
// a reference, here the fallback of an unset variable, is still joined
// element by element, as spruce joins it.
func TestJoinReferenceFallbackStillJoins(t *testing.T) {
	t.Setenv("P5_UNSET", "")
	got := mergeEnvShape(t, "l: [a, b]\nv: (( join \",\" \"s\" $P5_UNSET || l ))\n")
	if want := "l:\n- a\n- b\nv: s,a,b"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

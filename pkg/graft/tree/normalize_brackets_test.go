package tree

import "testing"

func TestNormalizeBrackets(t *testing.T) {
	cases := map[string]string{
		"list[0]":       "list.0",
		"jobs[0].k":     "jobs.0.k",
		"jobs.[0].k":    "jobs.0.k",
		"jobs[1]":       "jobs.1",
		"jobs[b]":       "jobs.b",
		"a[0][1]":       "a.0.1",
		"plain.path":    "plain.path",
		"":              "",
		"[a.b].c":       "[a.b].c",
		"x.[a.b]":       "x.[a.b]",
		"jobs[name=b]":  "jobs[name=b]",
		"odd[":          "odd[",
		"odd]":          "odd]",
		"jobs[]":        "jobs[]",
		"q[\"a\"]":      "q[\"a\"]",
		"a[0].[b.c][1]": "a.0.[b.c].1",
	}
	for in, want := range cases {
		if got := NormalizeBrackets(in); got != want {
			t.Errorf("NormalizeBrackets(%q) = %q, want %q", in, got, want)
		}
	}
}

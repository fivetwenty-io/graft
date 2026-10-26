package yq

import "testing"

func TestYQWildcardKeys(t *testing.T) {
	in := "red: 1\ngreen: 2\nblue: 3\ngrey: 4\n"
	wantYQ(t, `."*re*"`, in, "1\n2\n4\n")
	wantYQ(t, `.["b*"]`, in, "3\n")
	wantYQ(t, `."gr?y"`, in, "4\n")
	wantYQ(t, `."*re*" = 0`, in, "red: 0\ngreen: 0\nblue: 3\ngrey: 0\n")
	wantYQ(t, `."*r*" |= . + 10`, in, "red: 11\ngreen: 12\nblue: 3\ngrey: 14\n")
	// A key that names no entry reads as written.
	wantYQ(t, `."z*"`, in, "null\n")
	wantYQ(t, `."z*" = 1`, in, "red: 1\ngreen: 2\nblue: 3\ngrey: 4\nz*: 1\n")
	// A number key matches by its text.
	wantYQ(t, `."?" |= "x"`, "7: a\n", "7: x\n")
}

func TestGlobMatch(t *testing.T) {
	for _, c := range []struct {
		pattern, name string
		want          bool
	}{
		{"*", "", true},
		{"a*c", "abbc", true},
		{"a*c", "abbd", false},
		{"?", "é", true},
		{"??", "a", false},
		{"*a*a", "baba", true},
	} {
		if got := globMatch([]rune(c.pattern), []rune(c.name)); got != c.want {
			t.Errorf("globMatch(%q, %q) = %v, want %v", c.pattern, c.name, got, c.want)
		}
	}
}

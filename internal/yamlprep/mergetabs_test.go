package yamlprep

import (
	"testing"

	"github.com/goccy/go-yaml/token"
)

// TestTokenizeMergeKeyBeforeTab pins a plain << key with a tab before or
// after its colon, which goccy's scanner reads as a merge key only after
// a space. merge says the source holds a merge key.
func TestTokenizeMergeKeyBeforeTab(t *testing.T) {
	cases := []struct {
		src   string
		merge bool
	}{
		{"<<:\t{k: v}\n", true},
		{"m:\n  <<:\t*x\n", true},
		{"<<\t: {k: v}\n", true},
		{"a: {<<:\t*x}\n", true},
		{"<<:\t\n  k: v\n", true},
		{"<<: {k: v}\n", true},
		{"\"<<\":\t{k: v}\n", false},
		{"a: |\n  <<:\tx\n", false},
		{"a: <<\tb\n", false},
		{"<<<:\tx\n", false},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			merge := false
			for _, tk := range Tokenize(c.src) {
				if tk.Type == token.MergeKeyType {
					merge = true
					if tk.Value != "<<" {
						t.Errorf("merge key token = %q", tk.Value)
					}
				}
			}
			if merge != c.merge {
				t.Fatalf("merge key found = %v, want %v", merge, c.merge)
			}
		})
	}
}

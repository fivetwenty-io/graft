package operators

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// awsSubkeyRow holds what spruce 1.35.17 gives for (( awssecret
// "p5/x?key=k" )) when the secret holds value. spruce decodes the whole
// value as a YAML map and prints the subkey's value with %v, so the
// result is always a string. want is that string, or one of the markers
// below for an error. Each result was taken from spruce's own YAML
// library with spruce's rule.
type awsSubkeyRow struct {
	value, want string
}

const (
	awsExtractError = "ERR extract"
	awsInvalidKey   = "ERR invalid key"
)

var awsSubkeySpruce = []awsSubkeyRow{
	{"k: yes", "true"},
	{"k: no", "false"},
	{"k: 010", "8"},
	{"k: 8080", "8080"},
	{"k: 1.0", "1"},
	{"k: 0x10", "16"},
	{"k: ~", "<nil>"},
	{"k: {a: 1}", "map[a:1]"},
	{"k: [1, yes]", "[1 true]"},
	{`k: "q"`, "q"},
	{"k: plain", "plain"},
	{"{k: v}", "v"},
	{"k: v\n---\nk: w", "v"},
	{"k: v\n---\n[", "v"},
	{"k: &a [x]\nj: *a", "[x]"},
	{"plainsecret", awsExtractError},
	{"[1, 2]", awsExtractError},
	{"[", awsExtractError},
	{"k: [", awsExtractError},
	{"\tk: v", awsExtractError},
	{"", awsInvalidKey},
	{"~", awsInvalidKey},
	{"   ", awsInvalidKey},
	{"other: v", awsInvalidKey},
}

// awsSubkeyGaps maps each row where graft differs from spruce to the
// known gap that records it and the result graft gives.
var awsSubkeyGaps = map[string]struct{ gap, want string }{
	"k: y":         {"y-n-boolean-values-not-coerced", "y"},
	"k: 0X10":      {"numbers-libyaml-types-and-graft-reads-as-strings", "0X10"},
	"k: v\nk: w":   {"duplicate-mapping-keys-are-an-error", awsExtractError},
	"k: !!float 3": {"float-tag-over-integer-text in genesis-compat-contract.md", "3"},
}

func awsSubkeyEngine(t *testing.T, value string) graft.Engine {
	t.Helper()
	backend := &awsStubBackend{name: "awssecret", data: map[string]string{"p5/x": value}}
	engine, err := graft.NewEngine(
		graft.WithFeatureFlags(backendRegistryFlagsEnabled()),
		graft.WithBackend(backend),
	)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return engine
}

func checkAwsSubkey(t *testing.T, value, want string) {
	t.Helper()
	doc, err := evaluateYAML(t, awsSubkeyEngine(t, value), "v: (( awssecret \"p5/x?key=k\" ))\n")
	switch want {
	case awsExtractError, awsInvalidKey:
		if err == nil {
			got, _ := doc.GetString("v")
			t.Fatalf("value %q gave %q, want an error", value, got)
		}
		marker := "error extracting key"
		if want == awsInvalidKey {
			marker = "invalid key"
		}
		if !strings.Contains(graft.DetailedError(err), marker) {
			t.Fatalf("value %q gave error %q, want one about %q", value, graft.DetailedError(err), marker)
		}
		return
	}
	if err != nil {
		t.Fatalf("value %q: Evaluate: %v", value, graft.DetailedError(err))
	}
	got, err := doc.GetString("v")
	if err != nil {
		t.Fatalf("value %q: GetString: %v", value, err)
	}
	if got != want {
		t.Fatalf("value %q gave %q, want %q", value, got, want)
	}
}

// TestAwsSubkeyDecodesLikeSpruce runs every row through a stub
// awssecret backend that holds a made-up value, so no AWS call is made.
func TestAwsSubkeyDecodesLikeSpruce(t *testing.T) {
	for _, row := range awsSubkeySpruce {
		t.Run(row.value, func(t *testing.T) {
			checkAwsSubkey(t, row.value, row.want)
		})
	}
	for value, g := range awsSubkeyGaps {
		t.Run("gap/"+value, func(t *testing.T) {
			checkAwsSubkey(t, value, g.want)
		})
	}
}

// TestAwsSubkeyErrorsQuoteNoValue checks that a secret which cannot give
// the subkey fails with an error that names no part of the secret, in
// either the plain or the detailed text.
func TestAwsSubkeyErrorsQuoteNoValue(t *testing.T) {
	const secret = "hunter2-s3cr3t"
	for _, value := range []string{
		secret,
		"[" + secret + "]",
		"[" + secret,
		"k: [" + secret,
		"k: " + secret + "\nk: " + secret,
		"\t" + secret,
		strings.Repeat("[", 20000) + secret,
	} {
		_, err := evaluateYAML(t, awsSubkeyEngine(t, value), "v: (( awssecret \"p5/x?key=k\" ))\n")
		if err == nil {
			t.Errorf("a bad secret gave no error")
			continue
		}
		for _, text := range []string{err.Error(), graft.DetailedError(err)} {
			if strings.Contains(text, "hunter") || strings.Contains(text, "s3cr3t") {
				t.Errorf("error quotes the secret: %q", text)
			}
		}
	}
}

// TestAwsSubkeyRefusesAliasBlowup checks that a secret whose aliases
// would expand past the merge's node cap fails instead of expanding.
func TestAwsSubkeyRefusesAliasBlowup(t *testing.T) {
	var b strings.Builder
	b.WriteString("a: &a [x, x, x, x, x, x, x, x, x, x]\n")
	prev := "a"
	for _, name := range []string{"b", "c", "d", "e", "f", "g", "h", "i"} {
		b.WriteString(name + ": &" + name + " [")
		for j := 0; j < 10; j++ {
			if j > 0 {
				b.WriteString(", ")
			}
			b.WriteString("*" + prev)
		}
		b.WriteString("]\n")
		prev = name
	}
	b.WriteString("k: *i\n")
	_, err := evaluateYAML(t, awsSubkeyEngine(t, b.String()), "v: (( awssecret \"p5/x?key=k\" ))\n")
	if err == nil || !strings.Contains(graft.DetailedError(err), "error extracting key") {
		t.Fatalf("an alias-heavy secret gave %v, want an extraction error", err)
	}
}

// TestAwsSubkeyGapsAreRecorded checks that each gap row names a gap with
// its entry in docs/spruce/known-gaps.md and really differs from spruce.
func TestAwsSubkeyGapsAreRecorded(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "spruce", "known-gaps.md"))
	if err != nil {
		t.Fatalf("reading known-gaps.md: %v", err)
	}
	spruce := map[string]string{
		"k: y":         "true",
		"k: 0X10":      "16",
		"k: v\nk: w":   "w",
		"k: !!float 3": awsExtractError,
	}
	for value, g := range awsSubkeyGaps {
		if s, ok := spruce[value]; !ok || s == g.want {
			t.Errorf("gap row %q has no spruce result that differs from graft's", value)
		}
		if strings.HasSuffix(g.gap, ".md") {
			continue
		}
		if !strings.Contains(string(doc), "\n### "+g.gap+"\n") {
			t.Errorf("gap %q for row %q has no entry in known-gaps.md", g.gap, value)
		}
	}
}

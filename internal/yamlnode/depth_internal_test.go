package yamlnode

import (
	"strings"
	"testing"

	"github.com/goccy/go-yaml/lexer"

	"github.com/fivetwenty-io/graft/internal/yamlprep"
)

// TestProbeDepthFirstDocument checks that the probe, told to stop at the
// first document boundary, still fails deep nesting in the first
// document and ignores it in a later one.
func TestProbeDepthFirstDocument(t *testing.T) {
	deep := strings.Repeat("[", 10001) + "\n"
	for _, c := range []struct {
		name, in string
		first    string // the error with firstDocOnly, "" for none
		all      string // the error without it
	}{
		{"one document", deep, "yaml: exceeded max depth of 10000", "yaml: exceeded max depth of 10000"},
		{"after a header", "---\n" + deep, "yaml: line 2: exceeded max depth of 10000", "yaml: line 2: exceeded max depth of 10000"},
		{"after a directive", "%YAML 1.2\n---\n" + deep, "yaml: line 3: exceeded max depth of 10000", "yaml: line 3: exceeded max depth of 10000"},
		{"after a comment", "# c\n" + deep, "yaml: line 2: exceeded max depth of 10000", "yaml: line 2: exceeded max depth of 10000"},
		{"second document", "a: 1\n---\n" + deep, "", "yaml: line 3: exceeded max depth of 10000"},
		{"after an end marker", "a: 1\n...\n" + deep, "", "yaml: line 3: exceeded max depth of 10000"},
		{"third document", "---\n---\n" + deep, "", "yaml: line 3: exceeded max depth of 10000"},
	} {
		t.Run(c.name, func(t *testing.T) {
			for _, k := range []struct {
				firstDocOnly bool
				want         string
			}{{true, c.first}, {false, c.all}} {
				_, err := probeDepth([]byte(c.in), k.firstDocOnly)
				if got := errText(err); got != k.want {
					t.Errorf("probeDepth(firstDocOnly=%v) = %q, want %q", k.firstDocOnly, got, k.want)
				}
			}
		})
	}
}

// TestProbeDepthMatchesCheckDepth checks that the probe fails exactly
// the inputs that checkDepth fails after the rewrites Parse applies,
// with the same message, on inputs whose nesting runs into text, keys,
// and rewritten lines at different points.
func TestProbeDepthMatchesCheckDepth(t *testing.T) {
	var inputs []string
	for _, n := range []int{9999, 10000, 10001} {
		open, closing := strings.Repeat("[", n), strings.Repeat("]", n)
		dashes, indent := strings.Repeat("- ", n), strings.Repeat(" ", 2*n)
		inputs = append(inputs,
			dashes+"b: 1\n"+indent+"<<<: x\n",
			dashes+"b: 1\n"+indent+"a.<<<: x\n",
			dashes+"<<<: x\n",
			"a:\n"+dashes+"\n"+indent+"-\nk: v\n",
			"a: 1\r\n# c\r\nb: "+open+closing+"\r\n",
			open+closing+"\n",
			"k: "+open+"\"[[[[\"]"+closing+"\n",
			"k: "+open+"'a''[[['"+closing+"\n",
			"k: "+open+"{a: [b]}"+closing+"\n",
			dashes+"x\n",
			dashes+"-x\n",
			dashes+"? x\n",
			dashes+"k: [v]\n",
			dashes+"\"[[[\"\n",
			"a:\n"+dashes+"|\n"+strings.Repeat(" ", 2*n+1)+"[[[\n",
		)
	}
	for _, in := range inputs {
		prepared, _ := yamlprep.Prepare(normalizeLineBreaks([]byte(in)))
		want := errText(checkDepth(lexer.Tokenize(string(prepared))))
		_, err := probeDepth([]byte(in), false)
		if got := errText(err); got != want {
			t.Errorf("probeDepth(%.30q...) = %q, checkDepth says %q", in, got, want)
		}
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

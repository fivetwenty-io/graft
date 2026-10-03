package yamlnode

import (
	"strings"
	"testing"

	"github.com/goccy/go-yaml/lexer"

	"github.com/fivetwenty-io/graft/internal/yamlprep"
)

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
		_, err := probeDepth([]byte(in))
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

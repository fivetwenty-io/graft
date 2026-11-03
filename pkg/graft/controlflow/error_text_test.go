package controlflow

import (
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
	"github.com/fivetwenty-io/graft/pkg/graft"
)

// TestControlFlowErrorTextHoldsNoValue fails each control-flow construct
// on a planted value and checks that Error() leaves the value and color
// out while the detailed text keeps the value.
func TestControlFlowErrorTextHoldsNoValue(t *testing.T) {
	was := ansi.IsColorEnabled()
	ansi.Color(true)
	t.Cleanup(func() { ansi.Color(was) })

	const probe = "PROBEzq81x"
	cases := []struct {
		name  string
		src   string
		probe string
	}{
		{
			name: "bare when pattern",
			src:  "cloud: aws\n(( case cloud ))\n(( when PROBEzq81x ))\nx: 1\n(( esac ))\n",
		},
		{
			name: "for header",
			src:  "list:\n(( for PROBEzq81x ))\n  - a\n(( done ))\n",
		},
		{
			name: "if condition",
			src:  "(( if \"PROBEzq81x\" ] ))\nx: 1\n(( fi ))\n",
		},
		{
			name:  "range bound",
			src:   "list:\n(( for i in range 1 4073.5 ))\n  - (( grab i ))\n(( done ))\n",
			probe: "4073.5",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.probe
			if p == "" {
				p = probe
			}
			err := runMergeYAMLErr(t, tc.src)
			if err == nil {
				t.Fatal("the document must fail")
			}
			if plain := err.Error(); strings.Contains(plain, p) || strings.Contains(plain, "\x1b[") {
				t.Errorf("Error() quotes the value or holds color:\n%q", plain)
			}
			if detail := graft.DetailedError(err); !strings.Contains(detail, p) {
				t.Errorf("DetailedError dropped the value:\n%s", detail)
			}
		})
	}
}

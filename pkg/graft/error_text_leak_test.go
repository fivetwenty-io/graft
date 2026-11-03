package graft_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
	"github.com/fivetwenty-io/graft/pkg/graft"
	_ "github.com/fivetwenty-io/graft/pkg/graft/operators"
)

// errProbe is the value every error-text case plants in its document.
// No Error() text may hold it, and DetailedError keeps it wherever the
// CLI has always printed it.
const errProbe = "PROBEzq81x"

// errProbeMeta is the data each case's operator reads from.
const errProbeMeta = `meta:
  s: PROBEzq81x
  l: [PROBEzq81x]
  m: {k: PROBEzq81x}
  k: {PROBEzq81x: 1}
  n: 3
  f: true
  ip: 10.0.0.1
  cidr: 10.9.8.0/30
  lm: [{k: PROBEzq81x}]
`

func withColorOn(t *testing.T) {
	t.Helper()
	was := ansi.IsColorEnabled()
	ansi.Color(true)
	t.Cleanup(func() { ansi.Color(was) })
}

// errorTextCase is one failing document. out is the value of the out
// key in a document that also holds errProbeMeta, unless doc gives the
// whole document. probe overrides errProbe for a case whose value cannot
// be the usual probe, such as an address. detailed says whether the CLI
// prints the probe for the case, so DetailedError must keep it.
type errorTextCase struct {
	name     string
	out      string
	doc      string
	probe    string
	detailed bool
}

func (tc errorTextCase) document() string {
	if tc.doc != "" {
		return tc.doc
	}
	return errProbeMeta + "out: " + tc.out + "\n"
}

func (tc errorTextCase) value() string {
	if tc.probe != "" {
		return tc.probe
	}
	return errProbe
}

// staticIPsDoc builds a static_ips document on network net1 whose zone,
// address, count, or offset fields hold the probe where the case puts it.
func staticIPsDoc(static, jobAZ string, instances int, offsets string) string {
	return fmt.Sprintf(`networks:
  - name: net1
    subnets:
      - static: [%s]
        azs: [z1]
instance_groups:
  - name: web
    instances: %d
    azs: [%s]
    networks:
      - name: net1
        static_ips: (( static_ips %s ))
`, static, instances, jobAZ, offsets)
}

var operatorErrorCases = []errorTextCase{
	{name: "grab through a scalar", out: "(( grab meta.s.sub ))", detailed: true},
	{name: "grab missing", out: "(( grab meta.nope ))"},
	{name: "concat map", out: `(( concat meta.m "x" ))`},
	{name: "calc variable", out: "(( calc meta.s ))", detailed: true},
	{name: "calc string", out: `(( calc "1 - 'PROBEzq81x'" ))`, detailed: true},
	{name: "calc operand", out: `(( calc "meta.s * 2" ))`},
	{name: "multiply", out: "(( meta.s * 2 ))", detailed: true},
	{name: "subtract", out: "(( meta.s - 1 ))", detailed: true},
	{name: "divide", out: "(( meta.s / 2 ))", detailed: true},
	{name: "modulo", out: "(( meta.s % 2 ))", detailed: true},
	{name: "add map", out: "(( meta.m + 1 ))"},
	{name: "multiply boolean", out: "(( meta.f * 2 ))", probe: "true", detailed: true},
	{name: "divide by zero", out: "(( meta.n / 0 ))"},
	{name: "less than list", out: "(( meta.l < 3 ))", detailed: true},
	{name: "greater than map", out: "(( meta.m > 3 ))", detailed: true},
	{name: "ips address", out: "(( ips meta.s 1 ))", detailed: true},
	{name: "ips range", out: "(( ips meta.cidr 9 ))", probe: "10.9.8.0/30", detailed: true},
	{name: "ips count", out: "(( ips meta.cidr 1 7 ))", probe: "10.9.8.0/30", detailed: true},
	{name: "base64 map", out: "(( base64 meta.m ))"},
	{name: "base64-decode", out: "(( base64-decode meta.s ))"},
	{name: "base64-decode map", out: "(( base64-decode meta.m ))", detailed: true},
	{name: "join separator", out: "(( join meta.m meta.l ))"},
	{name: "join entry", out: `(( join "," meta.lm ))`},
	{name: "keys scalar", out: "(( keys meta.s ))"},
	{name: "sort scalar", out: "(( sort meta.s ))"},
	{name: "uniq scalar", out: "(( uniq meta.s ))"},
	{name: "flatten scalar", out: "(( flatten meta.s ))"},
	{name: "cartesian-product map", out: "(( cartesian-product meta.m ))"},
	{name: "stringify missing", out: "(( stringify meta.nope ))"},
	{name: "load", out: "(( load meta.s ))", detailed: true},
	{name: "load map", out: "(( load meta.m ))"},
	{name: "file", out: "(( file meta.s ))", detailed: true},
	{name: "file literal", out: `(( file "PROBEzq81x" ))`, detailed: true},
	{name: "param", out: `(( param "PROBEzq81x" ))`, detailed: true},
	{name: "split pattern", out: `(( split "/PROBEzq81x[/" meta.s ))`, detailed: true},
	{name: "vault path", out: "(( vault meta.s ))", detailed: true},
	{name: "vault literal path", out: `(( vault "secret/PROBEzq81x:k" ))`, detailed: true},
	{name: "vault argument", out: `(( vault "PROBEzq81x" ))`, detailed: true},
	{name: "vault map", out: "(( vault meta.m ))"},
	{name: "awsparam map", out: "(( awsparam meta.m ))"},
	{name: "inject scalar", out: "\n  <<<: (( inject meta.s ))"},
	{name: "parse error", out: `(( concat "PROBEzq81x ))`},
	{name: "env missing", out: "(( grab $PROBE_ZQ_ENV ))"},
	{name: "static_ips outside a job", out: "(( static_ips 0 ))"},
	{name: "static_ips address", doc: staticIPsDoc("PROBEzq81x", "z1", 1, "0"), detailed: true},
	{name: "static_ips range end", doc: staticIPsDoc("10.0.0.1 - PROBEzq81x", "z1", 1, "0"), detailed: true},
	{name: "static_ips backwards", doc: staticIPsDoc("10.9.8.7 - 10.0.0.1", "z1", 1, "0"), probe: "10.9.8.7", detailed: true},
	{name: "static_ips job zone", doc: staticIPsDoc("10.0.0.1 - 10.0.0.5", "PROBEzq81x", 1, "0"), detailed: true},
	{name: "static_ips zone list", doc: staticIPsDoc("10.0.0.1 - 10.0.0.5", "4077", 1, "0"), probe: "4077", detailed: true},
	{name: "static_ips instances", doc: staticIPsDoc("10.0.0.1 - 10.0.0.5", "z1", 4073, "0"), probe: "4073", detailed: true},
	{name: "static_ips offset", doc: staticIPsDoc("10.0.0.1 - 10.0.0.5", "z1", 1, "4071"), probe: "4071", detailed: true},
	{name: "static_ips zone offset", doc: staticIPsDoc("10.0.0.1 - 10.0.0.5", "z1", 1, `"PROBEzq81x:0"`), detailed: true},
	{name: "static_ips taken", doc: staticIPsDoc("10.9.8.7", "z1", 2, "0 0"), probe: "10.9.8.7", detailed: true},
}

// TestErrorTextHoldsNoValueOrColor fails every operator that can fail on
// a planted value, with the ansi toggle on, and checks the error's two
// texts.
func TestErrorTextHoldsNoValueOrColor(t *testing.T) {
	withColorOn(t)
	for _, mode := range leakModes {
		for _, tc := range operatorErrorCases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				eng, _ := leakEngine(t, mode.parallel)
				doc, err := eng.ParseYAML([]byte(tc.document()))
				if err == nil {
					_, err = eng.Merge(context.Background(), doc).Execute()
				}
				assertErrorText(t, err, tc.value(), tc.detailed)
			})
		}
	}
}

// assertErrorText checks that err's Error() holds neither the probe nor
// an escape code, and that DetailedError holds the probe when detailed
// is set.
func assertErrorText(t *testing.T, err error, probe string, detailed bool) {
	t.Helper()
	if err == nil {
		t.Fatal("the document must fail")
	}
	plain := err.Error()
	if strings.Contains(plain, probe) {
		t.Errorf("Error() quotes the value:\n%s", plain)
	}
	if strings.Contains(plain, "\x1b[") {
		t.Errorf("Error() holds an escape code:\n%q", plain)
	}
	detail := graft.DetailedError(err)
	if detailed && !strings.Contains(detail, probe) {
		t.Errorf("DetailedError dropped the value it has always shown:\n%s", detail)
	}
	if !detailed && strings.Contains(detail, probe) {
		t.Errorf("DetailedError quotes the value, so the case should say detailed:\n%s", detail)
	}
}

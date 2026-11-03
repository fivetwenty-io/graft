package graft_test

import (
	"context"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// colorErrorCases are documents whose error is a fixed message with
// color markup, such as a wrong argument count, a nil argument, or an
// operator used where it has no meaning. over, when set, is merged on
// top of doc. Each one's Error() text must be free of escape codes with
// the ansi toggle on.
var colorErrorCases = []struct {
	name string
	doc  string
	over string
}{
	{name: "grab with no arguments", doc: "out: (( grab ))\n"},
	{name: "join with no arguments", doc: "out: (( join ))\n"},
	{name: "join with only a separator", doc: "out: (( join \",\" ))\n"},
	{name: "split with no arguments", doc: "out: (( split ))\n"},
	{name: "split with only a separator", doc: "out: (( split \",\" ))\n"},
	{name: "split with too many arguments", doc: "out: (( split \",\" \"a\" \"b\" ))\n"},
	{name: "cartesian-product with no arguments", doc: "out: (( cartesian-product ))\n"},
	{name: "inject with no arguments", doc: "out:\n  <<<: (( inject ))\n"},
	{name: "param with two arguments", doc: "out: (( param \"a\" \"b\" ))\n"},
	{name: "base64 of nil", doc: "meta: {n: ~}\nout: (( base64 meta.n ))\n"},
	{name: "base64-decode of nil", doc: "meta: {n: ~}\nout: (( base64-decode meta.n ))\n"},
	{name: "delete outside a list", doc: "out: {}\n", over: "out: (( delete \"a\" ))\n"},
}

// TestFixedErrorTextHoldsNoColor fails each of colorErrorCases with the
// ansi toggle on, and checks Error() and every error a caller can reach
// from it for escape codes. The detailed text keeps its color.
func TestFixedErrorTextHoldsNoColor(t *testing.T) {
	withColorOn(t)
	for _, mode := range leakModes {
		for _, tc := range colorErrorCases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				eng, _ := leakEngine(t, mode.parallel)
				docs := []graft.Document{}
				for _, text := range []string{tc.doc, tc.over} {
					if text == "" {
						continue
					}
					doc, err := eng.ParseYAML([]byte(text))
					if err != nil {
						t.Fatal(err)
					}
					docs = append(docs, doc)
				}
				_, err := eng.Merge(context.Background(), docs...).Execute()
				if err == nil {
					t.Fatal("the document must fail")
				}
				if strings.Contains(err.Error(), "\x1b[") {
					t.Errorf("Error() holds an escape code:\n%q", err.Error())
				}
				assertChainPlain(t, err, "\x1b[")
				if !strings.Contains(graft.DetailedError(err), "\x1b[") {
					t.Errorf("DetailedError lost its color:\n%q", graft.DetailedError(err))
				}
			})
		}
	}
}

// TestEvaluatorCycleErrorHoldsNoColor runs the evaluator's depth check
// on a map that holds itself, with the ansi toggle on.
func TestEvaluatorCycleErrorHoldsNoColor(t *testing.T) {
	withColorOn(t)
	loop := map[string]interface{}{}
	loop["self"] = loop
	ev := &graft.Evaluator{Tree: loop}
	err := ev.CheckForCycles(16)
	if err == nil {
		t.Fatal("CheckForCycles must fail on a map that holds itself")
	}
	if strings.Contains(err.Error(), "\x1b[") {
		t.Errorf("Error() holds an escape code:\n%q", err.Error())
	}
	if !strings.Contains(graft.DetailedError(err), "\x1b[") {
		t.Errorf("DetailedError lost its color:\n%q", graft.DetailedError(err))
	}
}

package graft_test

import (
	"fmt"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// headSlashDoc holds a key "/b" beside a key "b", so a '/' read as a
// division instead of the start of "/b" either fails or reads b.
const headSlashDoc = `
"/b": slash-b
a: 8
b: 2
`

// TestOperatorHeadLeadingSlashIsAPath pins that a '/' right after an
// operator head with a glued ":modifier" begins a reference, the way it
// does after a plain operator name in "(( grab /b ))". An argument
// before the '/' is still a left operand, so the '/' divides it.
func TestOperatorHeadLeadingSlashIsAPath(t *testing.T) {
	cases := []struct {
		name string
		expr string
		want string
	}{
		{"grab with a modifier", "(( grab:nocache /b ))", "slash-b"},
		{"concat with a modifier", `(( concat:nocache /b "-x" ))`, "slash-b-x"},
		{"grab with a modifier right of ||", "(( grab missing || grab:nocache /b ))", "slash-b"},
		{"grab with a modifier after a comma", "(( grab:nocache a, /b ))", "[8 slash-b]"},
		{"an argument after the head still divides", "(( grab:nocache a /b ))", "4"},
	}
	for _, mode := range slashModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				d, err := evalSlashDoc(t, mode.opts, headSlashDoc+"result: "+tc.expr+"\n")
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				got, err := d.Get("result")
				if err != nil {
					t.Fatalf("failed to read result: %v", err)
				}
				if fmt.Sprint(got) != tc.want {
					t.Fatalf("result = %v (%T), want %q", got, got, tc.want)
				}
			})
		}
	}
}

// TestOperatorHeadLeadingSlashParsesAsReference pins the parse of every
// operator form that takes an @target or a :modifier, without a backend.
// The argument after the head is the one reference "/b".
func TestOperatorHeadLeadingSlashParsesAsReference(t *testing.T) {
	exprs := []string{
		"(( grab:nocache /b ))",
		"(( vault@prod /b ))",
		"(( vault:nocache /b ))",
		"(( vault:nocache@prod /b ))",
		"(( awsparam@x /b ))",
		"(( awsparam:nocache /b ))",
		"(( awsparam:nocache@x /b ))",
		"(( awssecret@x /b ))",
		"(( awssecret:nocache /b ))",
		"(( awssecret:nocache@x /b ))",
		"(( nats@x /b ))",
		"(( nats:nocache /b ))",
		"(( nats:nocache@x /b ))",
	}
	for _, src := range exprs {
		t.Run(src, func(t *testing.T) {
			opcall, err := graft.ParseOpcallWithParser(graft.EvalPhase, src)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			if opcall == nil {
				t.Fatalf("parsed as literal text, want an operator call")
			}
			args := opcall.Args()
			if len(args) != 1 {
				t.Fatalf("got %d arguments, want 1", len(args))
			}
			if args[0].Type != graft.Reference || args[0].String() != "/b" {
				t.Fatalf("argument = %s (type %v), want the reference /b", args[0].String(), args[0].Type)
			}
		})
	}
}

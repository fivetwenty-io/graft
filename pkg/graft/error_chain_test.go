package graft_test

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/url"
	"strconv"
	"strings"
	"testing"

	goyaml "github.com/goccy/go-yaml"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// chainNodes returns err and every error reachable from it through
// errors.Unwrap, in the order errors.Is and errors.As visit them.
func chainNodes(err error) []error {
	var out []error
	var walk func(error)
	walk = func(e error) {
		if e == nil {
			return
		}
		out = append(out, e)
		switch u := e.(type) { //nolint:errorlint // the chain is walked one level at a time
		case interface{ Unwrap() error }:
			walk(u.Unwrap())
		case interface{ Unwrap() []error }:
			for _, c := range u.Unwrap() {
				walk(c)
			}
		}
	}
	walk(err)
	return out
}

// asTargets are the errors.As targets a caller might use to dig into an
// error: the third-party types graft's errors have wrapped, and the
// interfaces that match any wrapper.
func asTargets() map[string]func(error) (error, bool) {
	return map[string]func(error) (error, bool){
		"*strconv.NumError": func(err error) (error, bool) {
			var e *strconv.NumError
			ok := errors.As(err, &e)
			return e, ok
		},
		"*fs.PathError": func(err error) (error, bool) {
			var e *fs.PathError
			ok := errors.As(err, &e)
			return e, ok
		},
		"*json.SyntaxError": func(err error) (error, bool) {
			var e *json.SyntaxError
			ok := errors.As(err, &e)
			return e, ok
		},
		"*url.Error": func(err error) (error, bool) {
			var e *url.Error
			ok := errors.As(err, &e)
			return e, ok
		},
		"goccy Error": func(err error) (error, bool) {
			var e goyaml.Error
			ok := errors.As(err, &e)
			return e, ok
		},
		"wrapper": func(err error) (error, bool) {
			var e interface {
				error
				Unwrap() error
			}
			ok := errors.As(err, &e)
			return e, ok
		},
		"multi wrapper": func(err error) (error, bool) {
			var e interface {
				error
				Unwrap() []error
			}
			ok := errors.As(err, &e)
			return e, ok
		},
		"detailer": func(err error) (error, bool) {
			var e interface {
				error
				DetailedError() string
			}
			ok := errors.As(err, &e)
			return e, ok
		},
	}
}

// assertChainPlain checks every error reachable from err, through
// errors.Unwrap and through errors.As with each of asTargets, holds
// neither the probe nor an escape code in its Error() text.
func assertChainPlain(t *testing.T, err error, probe string) {
	t.Helper()
	for _, node := range chainNodes(err) {
		text := node.Error()
		if strings.Contains(text, probe) || strings.Contains(text, "\x1b[") {
			t.Errorf("a %T on the chain has a value or color in Error():\n%q", node, text)
		}
		for name, as := range asTargets() {
			if found, ok := as(node); ok && found != nil {
				text := found.Error()
				if strings.Contains(text, probe) || strings.Contains(text, "\x1b[") {
					t.Errorf("errors.As(%s) reaches a %T with a value or color in Error():\n%q", name, found, text)
				}
			}
		}
	}
}

// TestErrorChainHoldsNoValue fails a parse, an ips call, a grab through
// a scalar, and a document whose siblings hold the probe, and checks
// every error a caller can reach from each one.
func TestErrorChainHoldsNoValue(t *testing.T) {
	withColorOn(t)
	cases := []struct {
		name  string
		doc   string
		parse bool
	}{
		{name: "parse", doc: "a: [PROBEzq81x\n", parse: true},
		{name: "ips", doc: errProbeMeta + "out: (( ips meta.s 0 ))\n"},
		{name: "grab through a scalar", doc: errProbeMeta + "out: (( grab meta.s.deeper ))\n"},
		{name: "arithmetic on a string", doc: errProbeMeta + "out: (( meta.s * -3 ))\n"},
		{name: "vault path", doc: errProbeMeta + "out: (( vault meta.s \":\" meta.s ))\n"},
		{name: "siblings", doc: `meta:
  s: PROBEzq81x
creds:
  user: PROBEzq81x
  pass: (( vault "secret/creds:pass" ))
refs:
  before: PROBEzq81x
  missing: (( grab meta.nope ))
  bad: (( ips meta.s 0 ))
`},
	}
	for _, mode := range leakModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				eng, _ := leakEngine(t, mode.parallel)
				doc, err := eng.ParseYAML([]byte(tc.doc))
				if tc.parse {
					if err == nil {
						t.Fatal("the parse must fail")
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					_, err = eng.Merge(context.Background(), doc).Execute()
					if err == nil {
						t.Fatal("the merge must fail")
					}
				}
				assertChainPlain(t, err, errProbe)
				if !strings.Contains(graft.DetailedError(err), errProbe) {
					t.Errorf("DetailedError lost the value:\n%s", graft.DetailedError(err))
				}
			})
		}
	}
}

// TestGetterErrorChainHoldsNoValue fails a getter on a map that holds a
// list, and checks every error a caller can reach from the failure.
func TestGetterErrorChainHoldsNoValue(t *testing.T) {
	doc := graft.NewDocument(map[string]interface{}{
		"m": map[string]interface{}{"k": []interface{}{errProbe}},
	})
	_, err := doc.GetMapStringString("m")
	if err == nil {
		t.Fatal("the getter must fail")
	}
	assertChainPlain(t, err, errProbe)
	if !errors.Is(err, graft.ErrTypeMismatch) {
		t.Errorf("errors.Is(ErrTypeMismatch) is false for %v", err)
	}
	if !strings.Contains(graft.DetailedError(err), errProbe) {
		t.Errorf("DetailedError lost the value:\n%s", graft.DetailedError(err))
	}
}

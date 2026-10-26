package jq

import (
	_ "embed"
	"sync"

	"github.com/fivetwenty-io/graft/internal/query/builtin"
	"github.com/fivetwenty-io/graft/internal/query/eval"
	"github.com/fivetwenty-io/graft/internal/query/parser"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
)

// Prelude is the jq source of the builtins that are naturally jq
// definitions. Each one sees the natives and every definition before it.
//
//go:embed prelude.jq
var Prelude string

// preludeFile is the file name the prelude's $__loc__ reports.
const preludeFile = "<builtin>"

var (
	regOnce sync.Once
	reg     *runtime.Registry
	rootEnv *eval.Env
)

// registry returns the jq registry, building it and the root environment
// that holds the prelude on first use. A prelude that fails to parse is a
// bug in this package, so it panics.
func registry() *runtime.Registry {
	regOnce.Do(func() {
		r := &runtime.Registry{}
		builtin.RegisterCore(r)
		builtin.RegisterRegex(r)
		registerJQ(r)
		prog, err := parser.ParseFile(Prelude, preludeFile, Grammar())
		if err != nil {
			panic("jq: prelude does not parse: " + err.Error())
		}
		r.AddPrelude(prog)
		reg = r
		rootEnv = eval.NewEnv(r)
	})
	return reg
}

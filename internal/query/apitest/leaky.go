// Package apitest holds a fixture for the public API boundary test in
// cmd/graft. Its Leaky type hides three internal types where a careless
// checker would not look, and nothing outside that test imports it.
package apitest

import (
	"github.com/fivetwenty-io/graft/internal/query/eval"
	"github.com/fivetwenty-io/graft/internal/query/load"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
)

// Holder is the named field type that carries the first internal type.
type Holder struct {
	Ctx *runtime.Ctx
}

// Source is an embedded interface whose method returns the second.
type Source interface {
	Next() (load.Document, error)
}

// Leaky reaches runtime.Ctx through a field, load.Document through an
// embedded interface, and eval.Program through a method result.
type Leaky struct {
	Field Holder
	Source
}

// Program returns the third internal type.
func (Leaky) Program() *eval.Program { return nil }

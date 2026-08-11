package graft

// ControlFlowExpander preprocesses raw YAML source bytes before they reach
// the YAML parser, expanding (( if )), (( for )), (( while )), and
// (( case )) control-flow constructs into plain YAML. Nil means no
// preprocessing is registered.
//
// This package cannot import pkg/graft/controlflow directly: the
// preprocessor needs Evaluator and Engine types from this package to
// evaluate conditions and iterables against a prescan scope, so the
// dependency must run the other way. pkg/graft/controlflow's init()
// assigns this hook when that package is imported (mirroring how
// pkg/graft/operators registers operators via init() — see
// cmd/graft/main.go's blank import of both packages). When no consumer
// imports pkg/graft/controlflow, this variable stays nil and ParseYAML
// behaves exactly as it did before control-flow support existed.
var ControlFlowExpander func(source []byte) ([]byte, error)

// Package controlflow implements graft's (( if )), (( for )), (( while )),
// and (( case )) control-flow constructs as a source-to-source preprocessor.
//
// Control-flow markers occupy whole lines and their bodies are raw,
// possibly-invalid-in-isolation YAML (the same map key repeated across
// branches, list items with no enclosing key, and so on), so these
// constructs cannot be represented as ordinary parsed expressions or
// operators. Expand runs on the raw source bytes of a document, before YAML
// parsing, and produces plain YAML text with every control-flow block
// replaced by the content of whichever branch (or loop iterations) applies.
//
// Importing this package registers it with pkg/graft via a package-level
// hook variable (see ControlFlowExpander in pkg/graft/controlflow_hook.go)
// so pkg/graft itself never needs to import this package — this package
// imports pkg/graft to reach Evaluator/Engine, so the dependency can only
// run one way. Callers that want control-flow support import this package
// for its init() side effect, exactly as they already import
// pkg/graft/operators to register operators (see cmd/graft/main.go).
package controlflow

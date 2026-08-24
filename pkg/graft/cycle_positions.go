package graft

import (
	"strings"

	"github.com/fivetwenty-io/graft/internal/srcpos"
	"github.com/fivetwenty-io/graft/pkg/graft/interfaces"
)

// sourceIndexes holds one position index per merge input, in merge
// order. It is built lazily, only after a cycle has been detected;
// nothing here runs on a successful merge.
type sourceIndexes struct {
	names   []string
	indexes []*srcpos.Index // parallel to names; nil for an opaque input
	// allIndexed is false when any input is opaque. An opaque input
	// cannot be ruled out as a node's origin, which disables the
	// expression fallback for the whole merge.
	allIndexed bool
}

// buildSourceIndexes parses each operator-bearing input into a position
// index.
//
// Each input is put through the same pure rewrites ParseYAML applies, in
// ParseYAML's own order, so paths and lines describe the document the
// merge actually parsed. Control-flow expansion is deliberately not
// reproduced: the expander rewrites a document wholesale, so no mapping
// from expanded text back to original lines exists, and the CLI marks
// such an input opaque instead.
func buildSourceIndexes(refs []SourceRef) *sourceIndexes {
	si := &sourceIndexes{allIndexed: true}
	for _, r := range refs {
		si.names = append(si.names, r.Name)
		if r.Opaque {
			si.indexes = append(si.indexes, nil)
			si.allIndexed = false
			continue
		}
		if len(r.Bytes) == 0 {
			// A complete, legitimately empty index: this input has no
			// operator text, so it can be ruled out as a node's origin.
			si.indexes = append(si.indexes, srcpos.Build(r.Name, nil))
			continue
		}
		data := QuoteInjectKeys(sanitizeBareSequenceTerminators(r.Bytes))
		si.indexes = append(si.indexes, srcpos.Build(r.Name, data))
	}
	return si
}

// resolve locates one operator call in the merge's inputs.
//
// Stage one walks the inputs in reverse merge order, first hit wins,
// trying the canonical path before the literal one. registerOpcall
// (evaluator.go) sets where to the literal numeric path and canonical to
// the name-resolved one, and list indices drift across merges while name
// keys do not.
//
// Stage two runs only when stage one found nothing. It never invents a
// position, and never names a file unless that file is the only
// candidate.
func (si *sourceIndexes) resolve(op *Opcall) interfaces.Position {
	if op == nil {
		return interfaces.Position{}
	}

	if pos, ok := si.resolveByPath(candidatePaths(op)); ok {
		return pos
	}

	if pos, ok := si.resolveByExpr(strings.TrimSpace(op.src)); ok {
		return pos
	}

	// With exactly one input there is no rival candidate, so the file
	// can be named even though the line cannot.
	if len(si.names) == 1 {
		return interfaces.Position{File: si.names[0]}
	}

	// In a multi-input merge, a node whose path is indexed nowhere
	// cannot be attributed to a file at all. A complete index that lacks
	// a path does not prove the operator is absent from that file: list
	// index drift is precisely the case where it is present under a
	// different path. Naming a file on that basis would be a guess.
	return interfaces.Position{}
}

// candidatePaths returns the paths worth trying for op, canonical
// first: registerOpcall (evaluator.go) sets where to the literal
// numeric path and canonical to the name-resolved one, and list indices
// drift across merges while name keys do not.
func candidatePaths(op *Opcall) []string {
	candidates := make([]string, 0, 2)
	if op.canonical != nil {
		candidates = append(candidates, op.canonical.String())
	}
	if op.where != nil {
		if w := op.where.String(); len(candidates) == 0 || w != candidates[0] {
			candidates = append(candidates, w)
		}
	}
	return candidates
}

// resolveByPath is stage one: walk the inputs in reverse merge order,
// first hit wins, trying each candidate path in order.
func (si *sourceIndexes) resolveByPath(candidates []string) (interfaces.Position, bool) {
	for i := len(si.indexes) - 1; i >= 0; i-- {
		idx := si.indexes[i]
		if idx == nil {
			continue
		}
		for _, p := range candidates {
			if e, ok := idx.Lookup(p); ok {
				return e.Pos, true
			}
		}
	}
	return interfaces.Position{}, false
}

// resolveByExpr is stage two, run only when stage one found nothing.
// Uniqueness is computed over the union of all indexes, never within one
// file: scoping it per file lets one file's unique copy of an expression
// claim a node that lives in another, and duplicate short expressions
// across files are routine. When any input is opaque the rung is skipped
// entirely, because an unindexed input cannot be ruled out as the node's
// origin and a unique match elsewhere would confidently cite the wrong
// file.
func (si *sourceIndexes) resolveByExpr(expr string) (interfaces.Position, bool) {
	if !si.allIndexed {
		return interfaces.Position{}, false
	}

	total := 0
	var hit srcpos.Entry
	for _, idx := range si.indexes {
		if idx == nil {
			continue
		}
		total += idx.Exprs()[expr]
		if e, ok := idx.ByExpr(expr); ok {
			hit = e
		}
	}
	if total != 1 {
		return interfaces.Position{}, false
	}
	return hit.Pos, true
}

// buildCycleError assembles the reported error from the extracted cycle
// paths and the operator set still in play. It is called only from the
// cycle branch of kahnSort, so index construction stays off the success
// path.
func buildCycleError(refs []SourceRef, paths []string, all map[string]*Opcall) *CycleError {
	ce := &CycleError{}
	for _, r := range refs {
		ce.Inputs = append(ce.Inputs, r.Name)
	}

	si := buildSourceIndexes(refs)
	for _, p := range paths {
		n := CycleNode{Path: p}
		if op, ok := all[p]; ok && op != nil {
			n.Expr = strings.TrimSpace(op.src)
			n.Pos = si.resolve(op)
		}
		ce.Nodes = append(ce.Nodes, n)
	}
	return ce
}

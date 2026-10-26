package yq

import (
	"strings"

	"github.com/fivetwenty-io/graft/internal/query/ast"
	"github.com/fivetwenty-io/graft/internal/query/eval"
	"github.com/fivetwenty-io/graft/internal/query/load"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// Eval runs expr with doc as its only root and passes each output to emit
// with the document it came from.
func Eval(d *Dialect, expr string, doc load.Document, emit func(load.Document, *value.Value) error) error {
	return run(d, expr, []load.Document{doc}, emit)
}

// EvalAll runs expr once with every document as a root, which is how yq's
// eval-all sees them all in one context. A form that reads its input runs
// once for each document, so "[.a]" gathers across all of them while
// ".a = 3" updates each one. Each output goes to emit with the document
// its candidate came from.
func EvalAll(d *Dialect, expr string, docs []load.Document, emit func(load.Document, *value.Value) error) error {
	return run(d, expr, docs, emit)
}

// run evaluates expr over docs as the candidate set. The run tracks
// cursors, so parent, key, and path can answer for every value without
// back pointers, and the cursor's document is the one an output belongs
// to, which the output's path carries. A value the expression built has
// no path or a detached root, and it belongs to the first document, as yq
// gives it document index 0. The run is lax about undefined variables and
// about left sides that are not paths, as yq is.
func run(d *Dialect, expr string, docs []load.Document, emit func(load.Document, *value.Value) error) error {
	node, err := Parse(expr)
	if err != nil {
		return err
	}
	return RunNode(d, node, docs, nil, emit)
}

// RunNode is EvalAll for an expression that Parse has already parsed, so a
// caller who runs one expression many times parses it once. setup, when it
// is not nil, adjusts the run's context before anything is evaluated, which
// is how a caller sets the deadline, the environment, and the other bounds
// of one run. Eval is RunNode over a single document.
func RunNode(d *Dialect, node ast.Node, docs []load.Document, setup func(*runtime.Ctx), emit func(load.Document, *value.Value) error) error {
	if len(docs) == 0 {
		return nil
	}
	numbers := runtime.NumbersExact
	if d.opts.FloatNumbers {
		numbers = runtime.NumbersFloat
	}
	ctx := runtime.NewCtx(d, numbers)
	ctx.Cursors = true
	ctx.LeftFirst = true
	ctx.NodeKeys = true
	ctx.Lax = true
	ctx.RegexPCRE = true
	if setup != nil {
		setup(ctx)
	}
	roots := make([]*value.Value, len(docs))
	for i, doc := range docs {
		roots[i] = withLeadingContent(doc)
	}
	r := registry()
	prog := eval.NewProgram(node, rootEnv, r)
	split := 0
	return prog.Run(ctx, prog.Env, roots, func(v *value.Value, p value.Path) error {
		doc := docs[0]
		leading := ""
		if m := v.Meta(); m != nil {
			leading = m.LeadingContent
		}
		_, i, rel := runtime.Unroot(p)
		switch {
		case i == splitDoc:
			// split_doc puts each output in a document of its own, which
			// opens with no "---" of the input's. Only a document's root
			// keeps its leading comments, as in yq.
			doc.DocIndex = split
			leading = withoutSeparators(leading)
			if len(rel) > 0 {
				leading = ""
			}
			split++
		case i >= 0 && i < len(docs):
			doc = docs[i]
		}
		if p == nil || i == runtime.Detached {
			// A value the run built prints as the first document's, as
			// yq's new node is, so it never starts a document of its own.
			doc.DocIndex, doc.FileIndex = 0, 0
		}
		doc.LeadingContent = leading
		return emit(doc, v)
	})
}

// docSeparator stands for a "---" line in leading content.
const docSeparator = "$yqDocSeparator$"

// withoutSeparators is leading content without its "---" lines.
func withoutSeparators(leading string) string {
	var b strings.Builder
	for line := range strings.SplitAfterSeq(leading, "\n") {
		if strings.TrimSpace(line) != docSeparator {
			b.WriteString(line)
		}
	}
	return b.String()
}

// withLeadingContent is the document's root carrying the document's
// leading content, which yq keeps on the root node. So head_comment reads
// it, a head comment set on the root replaces it, and only an output that
// still carries it prints it, as in yq, where ".a" prints without it.
func withLeadingContent(doc load.Document) *value.Value {
	if doc.LeadingContent == "" {
		return doc.Value
	}
	var m value.Meta
	if dm := doc.Value.Meta(); dm != nil {
		m = *dm
	}
	m.LeadingContent = doc.LeadingContent
	return doc.Value.WithMeta(&m)
}

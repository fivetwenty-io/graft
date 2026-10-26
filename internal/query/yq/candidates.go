package yq

import (
	"strings"

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
// no path, and it belongs to the first document, as yq gives it document
// index 0. The run is lax about undefined variables and about left sides
// that are not paths, as yq is.
func run(d *Dialect, expr string, docs []load.Document, emit func(load.Document, *value.Value) error) error {
	node, err := Parse(expr)
	if err != nil {
		return err
	}
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
	ctx.Lax = true
	roots := make([]*value.Value, len(docs))
	for i, doc := range docs {
		roots[i] = doc.Value
	}
	r := registry()
	prog := eval.NewProgram(node, rootEnv, r)
	split := 0
	return prog.Run(ctx, prog.Env, roots, func(v *value.Value, p value.Path) error {
		doc := docs[0]
		switch top, i, rel := runtime.Unroot(p); {
		case i == splitDoc:
			// split_doc puts each output in a document of its own, which
			// opens with no "---" of the input's. Only a document's root
			// keeps its leading comments, as in yq.
			doc.DocIndex = split
			doc.LeadingContent = ""
			if len(rel) == 0 {
				doc.LeadingContent = leadingOf(docs, top)
			}
			split++
		case i < len(docs):
			doc = docs[i]
		}
		return emit(doc, v)
	})
}

// docSeparator stands for a "---" line in leading content.
const docSeparator = "$yqDocSeparator$"

// leadingOf is the leading content of the document whose root is root,
// without its "---" lines.
func leadingOf(docs []load.Document, root *value.Value) string {
	var b strings.Builder
	for _, d := range docs {
		if d.Value != root {
			continue
		}
		for line := range strings.SplitAfterSeq(d.LeadingContent, "\n") {
			if strings.TrimSpace(line) != docSeparator {
				b.WriteString(line)
			}
		}
		break
	}
	return b.String()
}

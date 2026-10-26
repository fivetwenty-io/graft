package yq

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/query/load"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// pathsDocs is two documents, the first with leading content. yq counts
// lines from after the leading content, so ".a" is on line 2.
const pathsDocs = "# head\na:\n  b: [1, 2]\n  c: {d: x}\n---\ne: 5\n"

func TestYQPositionOperators(t *testing.T) {
	for _, tc := range []struct{ expr, want string }{
		{".a.b[1] | line", "2\n---\n0\n"},
		{".a.b[1] | column", "10\n---\n0\n"},
		{". | line", "1\n5\n"},
		{".a | root | line", "1\n5\n"},
		{"[.. | [line, column]] | @json", "[[1,1],[2,3],[2,6],[2,7],[2,10],[3,6],[3,10]]\n[[5,1],[5,4]]\n"},
		{".a.b | key | line", "2\n---\n0\n"},
		{".a.b | key | column", "3\n---\n0\n"},
		{`"x" | line`, "0\n0\n"},
		{".zz | line", "0\n---\n0\n"},
		{".a | line | path", "- a\n---\n- a\n"},
		{".a | line | tag", "!!int\n---\n!!int\n"},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			wantYQ(t, tc.expr, pathsDocs, tc.want)
		})
	}
}

func TestYQDocumentAndFileOperators(t *testing.T) {
	for _, tc := range []struct{ expr, want string }{
		{"di", "0\n1\n"},
		{".e | document_index", "0\n---\n1\n"},
		{".a | documentIndex", "0\n---\n1\n"},
		{".a | di | path", "- a\n---\n- a\n"},
		{`"x" | di`, "0\n0\n"},
		{"filename", "test\ntest\n"},
		{".a | file_name", "test\n---\ntest\n"},
		{".a | fileName", "test\n---\ntest\n"},
		{`"x" | filename | tag`, "!!str\n!!str\n"},
		{"fi", "0\n0\n"},
		{".a | file_index", "0\n---\n0\n"},
		{".a | fileIndex", "0\n---\n0\n"},
		{"[fi, di] | @json", "[0,0]\n[0,1]\n"},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			wantYQ(t, tc.expr, pathsDocs, tc.want)
		})
	}
}

func TestYQFileIndexUnderEvalAll(t *testing.T) {
	var docs []load.Document
	for i, src := range []string{"a: 1\n---\nb: 2\n", "z: 1\n"} {
		r := load.Open(load.Source{Name: fmt.Sprintf("f%d", i), Reader: strings.NewReader(src)}, i, load.LoadOptions{Format: load.FormatYAML, KeepMeta: true})
		for {
			d, err := r.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			docs = append(docs, d)
		}
	}
	var got []string
	err := EvalAll(New(Options{}), "[fi, di, filename] | @json", docs, func(_ load.Document, v *value.Value) error {
		got = append(got, v.Str())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, strings.Join(got, "\n"), `[0,0,"f0",0,1,"f0",1,0,"f1"]`)
}

func TestYQParentDepth(t *testing.T) {
	for _, tc := range []struct{ expr, want string }{
		{".a.b[0] | parent(0)", "1\n---\nnull\n"},
		{".a.b[0] | parent(1)", "[1, 2]\n---\n- null\n"},
		{".a.b[0] | parent(2)", "b: [1, 2]\nc: {d: x}\n---\nb:\n  - null\n"},
		{".a.b[0] | parent(5)", ""},
		{".a.b[0] | parent(-2)", "b: [1, 2]\nc: {d: x}\n---\nb:\n  - null\n"},
		{".a.b[0] | parent(-4)", "1\n---\nnull\n"},
		{".a.b[0] | parent(-5)", "1\n---\nnull\n"},
		{".a.b[0] | parent(3) | length", "1\n2\n"},
		{".a.b[0] | parent(4)", ""},
		{". | parent(0) | length", "1\n1\n"},
		{`"x" | parent(0)`, "x\nx\n"},
		{`"x" | parent(-1)`, "x\nx\n"},
		{".a.b[1] | parent(1) | path", "- a\n- b\n---\n- a\n- b\n"},
		{". | parent(1)", ""},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			wantYQ(t, tc.expr, pathsDocs, tc.want)
		})
	}
}

func TestYQParentToRoot(t *testing.T) {
	wantYQ(t, ".a.b[0] | parent(-1)", pathsDocs, "# head\na:\n  b: [1, 2]\n  c: {d: x}\n---\ne: 5\na:\n  b:\n    - null\n")
}

func TestYQParents(t *testing.T) {
	for _, tc := range []struct{ expr, want string }{
		{".a.b[0] | parents", "- [1, 2]\n- b: [1, 2]\n  c: {d: x}\n- a:\n    b: [1, 2]\n    c: {d: x}\n- - null\n- b:\n    - null\n- e: 5\n  a:\n    b:\n      - null\n"},
		{".a.b[0] | [parents] | length", "1\n---\n1\n"},
		{".a.b[0] | parents | path", "[]\n[]\n"},
		{".a.b[0] | parents | .[0] | line", "2\n0\n"},
		{". | parents", "[]\n[]\n"},
		{".a.b | key | parents | length", "2\n2\n"},
		{".a | key | parents", "- a:\n    b: [1, 2]\n    c: {d: x}\n- e: 5\n  a: null\n"},
		{".a.b | key | parent(0)", "b\n---\nb\n"},
		{".a.b[0] | parents | .[2] | head_comment", "head\n\n"},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			wantYQ(t, tc.expr, pathsDocs, tc.want)
		})
	}
}

func TestYQRoot(t *testing.T) {
	wantYQ(t, ".a.c.d | root | path", pathsDocs, "[]\n[]\n")
	wantYQ(t, ".a.c.d | root | length", "a: {c: {d: 1}}\nb: 2\n", "2\n")
	wantYQ(t, ".a.c | root", "a: {c: 1}\n", "a: {c: 1}\n")
	wantYQ(t, `"x" | root`, pathsDocs, "x\nx\n")
	wantYQ(t, `"x" | parent`, pathsDocs, "")
}

func TestYQParentNeedsAnInteger(t *testing.T) {
	for _, expr := range []string{`parent("x")`, "parent(1, 2)", "parent(.)", "parent(1.5)", "parent( 1)", "parent (1)"} {
		t.Run(expr, func(t *testing.T) {
			wantYQError(t, expr, pathsDocs, "bad expression, please check expression syntax")
		})
	}
}

package graft

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// opAt builds a minimal *Opcall for resolution tests. where and canonical
// are the same unless the test needs them to differ.
func opAt(t *testing.T, canonical, src string) *Opcall {
	t.Helper()
	cur, err := tree.ParseCursor(canonical)
	if err != nil {
		t.Fatalf("ParseCursor(%q) error = %v", canonical, err)
	}
	return &Opcall{where: cur, canonical: cur, src: src}
}

func TestResolveFindsCanonicalPathInLaterSource(t *testing.T) {
	refs := []SourceRef{
		{Name: "a.yml", Bytes: []byte("meta:\n  foo: (( grab meta.bar ))\n")},
		{Name: "b.yml", Bytes: []byte("meta:\n  bar: (( grab meta.foo ))\n")},
	}

	si := buildSourceIndexes(refs)
	pos := si.resolve(opAt(t, "meta.bar", "(( grab meta.foo ))"))

	if pos.File != "b.yml" {
		t.Errorf("Pos.File = %q, want b.yml", pos.File)
	}
	if pos.Line != 2 {
		t.Errorf("Pos.Line = %d, want 2", pos.Line)
	}
}

func TestResolvePrefersCanonicalOverLiteralPath(t *testing.T) {
	// The merged document has jobs.0 named "web"; the index records both
	// jobs.0.cmd and its jobs.web.cmd alias. An Opcall whose literal
	// path drifted to jobs.1 must still resolve through the canonical
	// name-keyed form.
	refs := []SourceRef{
		{Name: "a.yml", Bytes: []byte("jobs:\n  - name: web\n    cmd: (( grab meta.x ))\n")},
	}

	where, err := tree.ParseCursor("jobs.1.cmd")
	if err != nil {
		t.Fatalf("ParseCursor error = %v", err)
	}
	canonical, err := tree.ParseCursor("jobs.web.cmd")
	if err != nil {
		t.Fatalf("ParseCursor error = %v", err)
	}
	op := &Opcall{where: where, canonical: canonical, src: "(( grab meta.x ))"}

	pos := buildSourceIndexes(refs).resolve(op)

	if pos.Line != 3 {
		t.Errorf("Pos.Line = %d, want 3: the name-keyed alias must win over the drifted index", pos.Line)
	}
}

func TestResolveUsesUniqueExpressionAcrossTheUnion(t *testing.T) {
	// The node's path is indexed nowhere (it was relocated by a merge),
	// but its expression appears exactly once across all inputs.
	refs := []SourceRef{
		{Name: "a.yml", Bytes: []byte("meta:\n  first: (( grab meta.rare ))\n")},
		{Name: "b.yml", Bytes: []byte("meta:\n  other: (( grab meta.common ))\n")},
	}

	pos := buildSourceIndexes(refs).resolve(opAt(t, "somewhere.else", "(( grab meta.rare ))"))

	if pos.File != "a.yml" || pos.Line != 2 {
		t.Errorf("Pos = %+v, want a.yml:2", pos)
	}
}

func TestResolveRejectsExpressionDuplicatedAcrossFiles(t *testing.T) {
	// Unique within each file, duplicated across the union. Scoping
	// uniqueness per file would cite whichever file was examined first.
	refs := []SourceRef{
		{Name: "a.yml", Bytes: []byte("meta:\n  first: (( grab meta.a ))\n")},
		{Name: "b.yml", Bytes: []byte("meta:\n  other: (( grab meta.a ))\n")},
	}

	pos := buildSourceIndexes(refs).resolve(opAt(t, "somewhere.else", "(( grab meta.a ))"))

	if pos.File != "" || pos.Line != 0 {
		t.Errorf("Pos = %+v, want the zero position: no single candidate can be established", pos)
	}
}

func TestResolveSkipsExpressionRungWhenAnySourceIsOpaque(t *testing.T) {
	// The expression is unique in the YAML file, but the merge also has
	// an opaque input that could equally be the node's origin.
	refs := []SourceRef{
		{Name: "over.json", Opaque: true},
		{Name: "a.yml", Bytes: []byte("meta:\n  first: (( grab meta.rare ))\n")},
	}

	si := buildSourceIndexes(refs)
	if si.allIndexed {
		t.Fatalf("allIndexed = true; an opaque input must clear it")
	}

	pos := si.resolve(opAt(t, "somewhere.else", "(( grab meta.rare ))"))

	if pos.File != "" {
		t.Errorf("Pos.File = %q, want empty: an unindexed input cannot be ruled out", pos.File)
	}
}

func TestResolveNamesTheFileInASingleInputMerge(t *testing.T) {
	refs := []SourceRef{{Name: "only.yml", Opaque: true}}

	pos := buildSourceIndexes(refs).resolve(opAt(t, "meta.a", "(( grab meta.b ))"))

	if pos.File != "only.yml" {
		t.Errorf("Pos.File = %q, want only.yml", pos.File)
	}
	if pos.Line != 0 {
		t.Errorf("Pos.Line = %d, want 0: a line is never invented", pos.Line)
	}
}

func TestResolveAppliesTheSameByteRewritesAsParseYAML(t *testing.T) {
	// A bare "-" sequence terminator is misparsed by goccy v1.19.2 until
	// sanitizeBareSequenceTerminators rewrites it. Indexing the raw
	// bytes would produce paths for a document the merge never saw.
	doc := "list:\n  - one\n  -\nmeta:\n  a: (( grab meta.b ))\n"
	refs := []SourceRef{{Name: "a.yml", Bytes: []byte(doc)}}

	pos := buildSourceIndexes(refs).resolve(opAt(t, "meta.a", "(( grab meta.b ))"))

	if pos.File != "a.yml" || pos.Line != 5 {
		t.Errorf("Pos = %+v, want a.yml:5", pos)
	}
}

func TestResolveInjectBlockNodeIsUnresolved(t *testing.T) {
	// QuoteInjectKeys lets the file parse, and goccy then paths the
	// inject subtree under "<<<", but inject relocates that content into
	// the parent, so the merged node's path never matches what the index
	// recorded.
	doc := "meta:\n  <<<: (( inject other.thing ))\n"
	refs := []SourceRef{
		{Name: "a.yml", Bytes: []byte(doc)},
		{Name: "b.yml", Bytes: []byte("other:\n  thing: 1\n")},
	}

	pos := buildSourceIndexes(refs).resolve(opAt(t, "meta.relocated", "(( inject other.thing ))"))

	// The expression rung may still resolve it, since the expression is
	// unique across the union. What must never happen is a confident
	// citation of a path the index did not record.
	if pos.Line != 0 && pos.File != "a.yml" {
		t.Errorf("Pos = %+v; an inject node must resolve to a.yml or to nothing", pos)
	}
}

func TestBuildCycleErrorAssemblesInputsAndNodes(t *testing.T) {
	refs := []SourceRef{
		{Name: "a.yml", Bytes: []byte("meta:\n  foo: (( grab meta.bar ))\n")},
		{Name: "b.yml", Bytes: []byte("meta:\n  bar: (( grab meta.foo ))\n")},
	}
	all := map[string]*Opcall{
		"meta.foo": opAt(t, "meta.foo", "(( grab meta.bar ))"),
		"meta.bar": opAt(t, "meta.bar", "(( grab meta.foo ))"),
	}

	ce := buildCycleError(refs, []string{"meta.bar", "meta.foo"}, all)

	if len(ce.Inputs) != 2 || ce.Inputs[0] != "a.yml" || ce.Inputs[1] != "b.yml" {
		t.Errorf("Inputs = %v, want [a.yml b.yml] in merge order", ce.Inputs)
	}
	if len(ce.Nodes) != 2 {
		t.Fatalf("got %d nodes, want 2", len(ce.Nodes))
	}
	if ce.Nodes[0].Pos.File != "b.yml" || ce.Nodes[0].Pos.Line != 2 {
		t.Errorf("Nodes[0].Pos = %+v, want b.yml:2", ce.Nodes[0].Pos)
	}
	if ce.Nodes[1].Pos.File != "a.yml" || ce.Nodes[1].Pos.Line != 2 {
		t.Errorf("Nodes[1].Pos = %+v, want a.yml:2", ce.Nodes[1].Pos)
	}
}

func TestBuildCycleErrorWithNoSources(t *testing.T) {
	all := map[string]*Opcall{"meta.a": opAt(t, "meta.a", "(( grab meta.a ))")}

	ce := buildCycleError(nil, []string{"meta.a"}, all)

	if len(ce.Inputs) != 0 {
		t.Errorf("Inputs = %v, want empty", ce.Inputs)
	}
	if ce.Nodes[0].Expr != "(( grab meta.a ))" {
		t.Errorf("Nodes[0].Expr = %q; the expression comes from the Opcall, not the index", ce.Nodes[0].Expr)
	}
	if ce.Nodes[0].Pos.File != "" {
		t.Errorf("Nodes[0].Pos.File = %q, want empty", ce.Nodes[0].Pos.File)
	}
}

func TestMergeCycleReportsFilesAndLines(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	aBytes := []byte("meta:\n  foo: (( grab meta.bar ))\n")
	bBytes := []byte("meta:\n  bar: (( grab meta.foo ))\n")

	aDoc, err := engine.ParseYAML(aBytes)
	if err != nil {
		t.Fatalf("ParseYAML(a) error = %v", err)
	}
	bDoc, err := engine.ParseYAML(bBytes)
	if err != nil {
		t.Fatalf("ParseYAML(b) error = %v", err)
	}

	ctx := WithSourceRefs(context.Background(), []SourceRef{
		{Name: "a.yml", Bytes: aBytes},
		{Name: "b.yml", Bytes: bBytes},
	})

	_, err = engine.Merge(ctx, aDoc, bDoc).Execute()
	if err == nil {
		t.Fatalf("Execute() succeeded; want a cycle error")
	}

	var ce *CycleError
	if !errors.As(err, &ce) {
		t.Fatalf("errors.As(*CycleError) = false; got %T: %v", err, err)
	}
	if !errors.Is(err, ErrDependencyCycle) {
		t.Errorf("errors.Is(err, ErrDependencyCycle) = false, want true")
	}

	out := err.Error()
	for _, want := range []string{
		"cycle detected in operator data-flow graph",
		"[1] a.yml",
		"[2] b.yml",
		"cycle (2 nodes): meta.bar -> meta.foo -> meta.bar",
		"b.yml:2  meta.bar: (( grab meta.foo ))",
		"a.yml:2  meta.foo: (( grab meta.bar ))",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestMergeCycleThroughParallelEvaluation(t *testing.T) {
	// RunPhaseParallel reaches DataFlow before it reaches the worker
	// pool (evaluator_parallel.go), so a cycle surfaces identically on
	// the parallel path. Nothing pinned this before.
	engine, err := NewEngine(WithParallel(true))
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	src := []byte("meta:\n  foo: (( grab meta.bar ))\n  bar: (( grab meta.foo ))\n")
	doc, err := engine.ParseYAML(src)
	if err != nil {
		t.Fatalf("ParseYAML() error = %v", err)
	}

	ctx := WithSourceRefs(context.Background(), []SourceRef{{Name: "only.yml", Bytes: src}})

	_, err = engine.Merge(ctx, doc).Execute()
	if err == nil {
		t.Fatalf("Execute() succeeded; want a cycle error")
	}

	var ce *CycleError
	if !errors.As(err, &ce) {
		t.Fatalf("errors.As(*CycleError) = false; got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "only.yml:") {
		t.Errorf("output does not attribute a line in only.yml:\n%s", err.Error())
	}
}

func TestMergeCycleAlongsideOtherErrors(t *testing.T) {
	// A cycle can share a MultiError with an unrelated failure, and every
	// error gets exactly one " - " line while the cycle's detail block
	// contributes none. That is what Genesis's stderr parser counts on.
	//
	// The unrelated failure must be in a DIFFERENT phase. kahnSort returns
	// on its first stall, so DataFlow never surfaces a second error of its
	// own; the phase loop in evaluate() is what accumulates across phases.
	// inject is MergePhase and the grab cycle is EvalPhase, so the two
	// combine into a genuine two-element MultiError.
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	src := []byte("meta:\n  foo: (( grab meta.bar ))\n  bar: (( grab meta.foo ))\n" +
		"other: (( inject does.not.exist ))\n")
	doc, err := engine.ParseYAML(src)
	if err != nil {
		t.Fatalf("ParseYAML() error = %v", err)
	}

	ctx := WithSourceRefs(context.Background(), []SourceRef{{Name: "only.yml", Bytes: src}})

	_, err = engine.Merge(ctx, doc).Execute()
	if err == nil {
		t.Fatalf("Execute() succeeded; want errors")
	}

	out := err.Error()
	var me MultiError
	if !errors.As(err, &me) {
		t.Fatalf("errors.As(MultiError) = false; got %T: %v", err, err)
	}
	if len(me.Errors) != 2 {
		t.Fatalf("len(me.Errors) = %d, want 2 (the failed inject and the cycle); "+
			"a one-element MultiError makes the per-error line count vacuous:\n%s",
			len(me.Errors), out)
	}

	var ce *CycleError
	if !errors.As(err, &ce) {
		t.Errorf("errors.As(*CycleError) = false; the cycle should survive alongside the other error")
	}

	var prefixed int
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, " - ") {
			prefixed++
		}
	}
	if prefixed != len(me.Errors) {
		t.Errorf("got %d lines starting with %q for %d errors; want one per error:\n%s",
			prefixed, " - ", len(me.Errors), out)
	}
}

func TestResolveRejectsAPathHitCarryingAnotherExpression(t *testing.T) {
	// Both inputs carry an operator at the numeric path jobs.0.cmd, but
	// only d0.yml's is the one on the cycle. Reverse merge order reaches
	// d1.yml first, so without verifying the expression the later file's
	// unrelated operator claims the node and the block prints a line
	// whose text contradicts the expression beside it.
	d0 := []byte("jobs:\n  - name: alpha\n    cmd: (( grab meta.x ))\nmeta:\n  x: (( grab jobs.alpha.cmd ))\n")
	d1 := []byte("jobs:\n  - name: web\n    cmd: (( grab meta.y ))\nmeta:\n  y: hello\n")
	refs := []SourceRef{
		{Name: "d0.yml", Bytes: d0},
		{Name: "d1.yml", Bytes: d1},
	}

	where, err := tree.ParseCursor("jobs.alpha.cmd")
	if err != nil {
		t.Fatalf("ParseCursor error = %v", err)
	}
	canonical, err := tree.ParseCursor("jobs.0.cmd")
	if err != nil {
		t.Fatalf("ParseCursor error = %v", err)
	}
	op := &Opcall{where: where, canonical: canonical, src: "(( grab meta.x ))"}

	pos := buildSourceIndexes(refs).resolve(op)

	if pos.File != "d0.yml" || pos.Line != 3 {
		t.Errorf("Pos = %+v, want d0.yml:3: the cited line must contain the cited expression", pos)
	}
}

// mergeCycleErrorOutput merges refs in order and returns the rendered
// error, failing the test unless the merge produced a cycle.
func mergeCycleErrorOutput(t *testing.T, refs []SourceRef) string {
	t.Helper()

	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	docs := make([]Document, 0, len(refs))
	for _, r := range refs {
		doc, err := engine.ParseYAML(r.Bytes)
		if err != nil {
			t.Fatalf("ParseYAML(%s) error = %v", r.Name, err)
		}
		docs = append(docs, doc)
	}

	_, err = engine.Merge(WithSourceRefs(context.Background(), refs), docs...).Execute()
	if err == nil {
		t.Fatalf("Execute() succeeded; want a cycle error")
	}
	var ce *CycleError
	if !errors.As(err, &ce) {
		t.Fatalf("errors.As(*CycleError) = false; got %T: %v", err, err)
	}
	return err.Error()
}

func TestMergeCycleThroughANameKeyedListCitesTheRightFile(t *testing.T) {
	out := mergeCycleErrorOutput(t, []SourceRef{
		{Name: "d0.yml", Bytes: []byte("jobs:\n  - name: alpha\n    cmd: (( grab meta.x ))\nmeta:\n  x: (( grab jobs.alpha.cmd ))\n")},
		{Name: "d1.yml", Bytes: []byte("jobs:\n  - name: web\n    cmd: (( grab meta.y ))\nmeta:\n  y: hello\n")},
	})

	if !strings.Contains(out, "d0.yml:3  jobs.0.cmd: (( grab meta.x ))") {
		t.Errorf("output does not cite d0.yml:3 for jobs.0.cmd:\n%s", out)
	}
	if strings.Contains(out, "d1.yml:3") {
		t.Errorf("output cites d1.yml:3, which reads (( grab meta.y )):\n%s", out)
	}
}

func TestMergeCycleWithNoNameFieldsCitesTheRightFile(t *testing.T) {
	// Neither input carries a name, key, or id field anywhere, so
	// buildAliases has nothing to work with and where == canonical: the
	// numeric path is the only route to a hit. The prepend moves base.yml's
	// element from index 0 to index 1, and over.yml's own file, read on its
	// own, happens to carry a jobs.1.cmd of its own too - with different
	// text. Only the expression cross-check tells the two apart; nothing
	// here disambiguates them by name.
	out := mergeCycleErrorOutput(t, []SourceRef{
		{Name: "base.yml", Bytes: []byte("jobs:\n  - cmd: (( grab meta.x ))\nmeta:\n  x: (( grab jobs.1.cmd ))\n")},
		{Name: "over.yml", Bytes: []byte("jobs:\n  - (( prepend ))\n  - cmd: (( grab meta.q ))\nmeta:\n  q: hello\n")},
	})

	if !strings.Contains(out, "base.yml:2  jobs.1.cmd: (( grab meta.x ))") {
		t.Errorf("output does not cite base.yml:2 for jobs.1.cmd:\n%s", out)
	}
	if strings.Contains(out, "over.yml:3") {
		t.Errorf("output cites over.yml:3, whose text is (( grab meta.q )), not (( grab meta.x )):\n%s", out)
	}
}

func TestMergeCycleThroughAPrependCitesTheRightFile(t *testing.T) {
	// The prepend moves alpha from index 0 to index 1, so the cycle
	// node's canonical path is jobs.1.cmd - and over.yml has a
	// jobs.1.cmd of its own, zeta's, whose text is byte-identical.
	// Matching on the numeric path cites over.yml:4 for an operator
	// written at base.yml:3. Only the name-keyed alias reaches the right
	// one; the expression cross-check cannot break this tie, because the
	// two expressions are the same bytes.
	out := mergeCycleErrorOutput(t, []SourceRef{
		{Name: "base.yml", Bytes: []byte("jobs:\n  - name: alpha\n    cmd: (( grab meta.x ))\nmeta:\n  x: (( grab jobs.alpha.cmd ))\n")},
		{Name: "over.yml", Bytes: []byte("jobs:\n  - (( prepend ))\n  - name: zeta\n    cmd: (( grab meta.x ))\n")},
	})

	if !strings.Contains(out, "base.yml:3  jobs.1.cmd: (( grab meta.x ))") {
		t.Errorf("output does not cite base.yml:3 for jobs.1.cmd:\n%s", out)
	}
	if strings.Contains(out, "over.yml:4") {
		t.Errorf("output cites over.yml:4, which is zeta's own operator:\n%s", out)
	}
}

package srcpos

import "testing"

func TestBuildIndexesNestedMappingValue(t *testing.T) {
	doc := "meta:\n  foo: (( grab meta.bar ))\n  bar: plain\n"

	idx := Build("a.yml", []byte(doc))

	e, ok := idx.Lookup("meta.foo")
	if !ok {
		t.Fatalf("Lookup(meta.foo) = _, false; want an entry")
	}
	if e.Expr != "(( grab meta.bar ))" {
		t.Errorf("Expr = %q, want %q", e.Expr, "(( grab meta.bar ))")
	}
	if e.Pos.File != "a.yml" {
		t.Errorf("Pos.File = %q, want %q", e.Pos.File, "a.yml")
	}
	if e.Pos.Line != 2 {
		t.Errorf("Pos.Line = %d, want 2", e.Pos.Line)
	}
	if _, ok := idx.Lookup("meta.bar"); ok {
		t.Errorf("Lookup(meta.bar) succeeded; a non-operator scalar must not be indexed")
	}
}

func TestBuildIndexesBareListElement(t *testing.T) {
	doc := "list:\n  - (( grab meta.a ))\n  - plain\n"

	idx := Build("a.yml", []byte(doc))

	e, ok := idx.Lookup("list.0")
	if !ok {
		t.Fatalf("Lookup(list.0) = _, false; a bare list element must be indexed")
	}
	if e.Expr != "(( grab meta.a ))" {
		t.Errorf("Expr = %q, want %q", e.Expr, "(( grab meta.a ))")
	}
	if e.Pos.Line != 2 {
		t.Errorf("Pos.Line = %d, want 2", e.Pos.Line)
	}
}

func TestBuildRecordsNameKeyedAlias(t *testing.T) {
	doc := "jobs:\n  - name: web\n    cmd: (( grab meta.cmd ))\n"

	idx := Build("a.yml", []byte(doc))

	e, ok := idx.Lookup("jobs.0.cmd")
	if !ok {
		t.Fatalf("Lookup(jobs.0.cmd) = _, false; want an entry")
	}
	if e.Alias != "jobs.web.cmd" {
		t.Errorf("Alias = %q, want %q", e.Alias, "jobs.web.cmd")
	}
	byAlias, ok := idx.Lookup("jobs.web.cmd")
	if !ok {
		t.Fatalf("Lookup(jobs.web.cmd) = _, false; the alias must resolve")
	}
	if byAlias.Pos.Line != e.Pos.Line {
		t.Errorf("alias line = %d, path line = %d; want equal", byAlias.Pos.Line, e.Pos.Line)
	}
}

func TestBuildSkipsQuotedPathSegment(t *testing.T) {
	// goccy renders a key containing a dot as $.'a.b'.c, which graft's
	// dotted cursor syntax cannot represent unambiguously.
	doc := "meta:\n  \"a.b\":\n    c: (( grab meta.z ))\n"

	idx := Build("a.yml", []byte(doc))

	if len(idx.Exprs()) != 0 {
		t.Errorf("Exprs() = %v, want empty: a quoted path segment must be skipped", idx.Exprs())
	}
}

func TestBuildSkipsOperatorShapedMappingKey(t *testing.T) {
	doc := "meta:\n  \"(( inject meta.x ))\": value\n  z: (( grab meta.q ))\n"

	idx := Build("a.yml", []byte(doc))

	for expr := range idx.Exprs() {
		if expr == "(( inject meta.x ))" {
			t.Fatalf("an operator-shaped mapping key was indexed; keys must be skipped")
		}
	}
	if _, ok := idx.Lookup("meta.z"); !ok {
		t.Errorf("Lookup(meta.z) = _, false; the sibling value must still index")
	}
}

func TestBuildTrimsBlockScalarExpr(t *testing.T) {
	doc := "blk: |\n  (( grab y ))\n"

	idx := Build("a.yml", []byte(doc))

	e, ok := idx.Lookup("blk")
	if !ok {
		t.Fatalf("Lookup(blk) = _, false; want an entry")
	}
	// A block scalar's value carries a trailing newline; Opcall.src does
	// not. The trim is what makes the expression fallback comparable.
	if e.Expr != "(( grab y ))" {
		t.Errorf("Expr = %q, want %q", e.Expr, "(( grab y ))")
	}
	// goccy reports the content line, not the "|" line (verified against
	// v1.19.2).
	if e.Pos.Line != 2 {
		t.Errorf("Pos.Line = %d, want 2", e.Pos.Line)
	}
}

func TestBuildQuotedScalarHasQuotesStripped(t *testing.T) {
	doc := "q: \"(( grab meta.a ))\"\n"

	idx := Build("a.yml", []byte(doc))

	e, ok := idx.Lookup("q")
	if !ok {
		t.Fatalf("Lookup(q) = _, false; want an entry")
	}
	if e.Expr != "(( grab meta.a ))" {
		t.Errorf("Expr = %q, want the value with quotes stripped", e.Expr)
	}
}

func TestBuildMultiDocumentLaterWins(t *testing.T) {
	doc := "a: (( grab one ))\n---\na: (( grab two ))\n"

	idx := Build("a.yml", []byte(doc))

	e, ok := idx.Lookup("a")
	if !ok {
		t.Fatalf("Lookup(a) = _, false; want an entry")
	}
	if e.Expr != "(( grab two ))" {
		t.Errorf("Expr = %q, want the later document to win", e.Expr)
	}
	if e.Pos.Line != 3 {
		t.Errorf("Pos.Line = %d, want 3: lines run continuously across ---", e.Pos.Line)
	}
}

func TestBuildAliasNodeIsNotIndexed(t *testing.T) {
	// An operator behind an anchor indexes at the anchor's line; a path
	// that reaches it only through an alias carries no "((" text of its
	// own and is therefore not indexed.
	doc := "anchored: &a (( grab meta.x ))\nref: *a\n"

	idx := Build("a.yml", []byte(doc))

	if _, ok := idx.Lookup("anchored"); !ok {
		t.Errorf("Lookup(anchored) = _, false; the anchor's own value must index")
	}
	if _, ok := idx.Lookup("ref"); ok {
		t.Errorf("Lookup(ref) succeeded; an alias-reached path must not index")
	}
}

func TestBuildEmptyAndUnparseableYieldEmptyIndex(t *testing.T) {
	for name, data := range map[string]string{
		"empty":       "",
		"no operator": "a: 1\nb: two\n",
		"unparseable": "a: [1, 2\n  b: {{{\n",
	} {
		idx := Build("a.yml", []byte(data))
		if idx == nil {
			t.Fatalf("%s: Build returned nil; it must always return an Index", name)
		}
		if len(idx.Exprs()) != 0 {
			t.Errorf("%s: Exprs() = %v, want empty", name, idx.Exprs())
		}
	}
}

func TestByExprRequiresUniqueness(t *testing.T) {
	doc := "a: (( grab z ))\nb: (( grab z ))\nc: (( grab y ))\n"

	idx := Build("a.yml", []byte(doc))

	if _, ok := idx.ByExpr("(( grab z ))"); ok {
		t.Errorf("ByExpr returned a hit for a duplicated expression")
	}
	if counts := idx.Exprs()["(( grab z ))"]; counts != 2 {
		t.Errorf("Exprs()[grab z] = %d, want 2", counts)
	}
	if e, ok := idx.ByExpr("(( grab y ))"); !ok || e.Path != "c" {
		t.Errorf("ByExpr(grab y) = %+v, %v; want the entry at path c", e, ok)
	}
}

func TestNilIndexMethodsAreSafe(t *testing.T) {
	var idx *Index
	if _, ok := idx.Lookup("a"); ok {
		t.Errorf("nil Lookup returned true")
	}
	if len(idx.Exprs()) != 0 {
		t.Errorf("nil Exprs returned entries")
	}
	if _, ok := idx.ByExpr("x"); ok {
		t.Errorf("nil ByExpr returned true")
	}
}

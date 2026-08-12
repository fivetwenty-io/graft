package graft

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
)

// allExprTypes enumerates every ExprType constant declared in
// interfaces.go:64-113, in declaration order. There are 24 today. This
// list is maintained by hand — Go gives no way to enumerate the members of
// an int-based const block at runtime from Go code that isn't itself a
// source-analysis tool — so TestAllExprTypesAccountedFor exists
// specifically to make a Wave-A addition that skips updating this file
// fail loudly rather than silently under-testing Accept and Walk.
//
// The ground truth it's checked against is not another hand-maintained
// number: countExprTypeConstants below parses interfaces.go itself with
// go/parser and counts the ExprType const block's members directly, so a
// 25th ExprType added there and nowhere else is caught even though every
// hand-maintained number in this file would otherwise still agree with
// itself.
var allExprTypes = []ExprType{
	Literal, Reference, List, Or, Negate,
	Addition, Subtraction, Multiplication, Division, Modulo,
	Equal, NotEqual, LessThan, LessThanOrEqual, GreaterThan, GreaterThanOrEqual,
	LogicalAnd, LogicalOr, RegexpMatch, EnvVar, BoshVar, OperatorCall,
	VaultGroup, VaultChoice,
}

// wantExhaustiveExprTypeCount is this file's own documented expectation
// for the size of interfaces.go's ExprType const block. It is still a
// literal — the point of countExprTypeConstants is not to remove the
// documented number but to give it something independent to be checked
// against, so a drift between "what this file assumes" and "what
// interfaces.go actually declares" is caught even when nobody remembers
// to update this file by hand.
const wantExhaustiveExprTypeCount = 24

// countExprTypeConstants parses interfaces.go (the file that declares the
// ExprType type and its const block, resolved relative to this test
// file's own directory rather than the process's working directory, so it
// does not depend on how `go test` was invoked) and returns the number of
// identifiers declared in the const block whose first ValueSpec carries
// the explicit type annotation "ExprType" — i.e. the block that opens
// with `Literal ExprType = iota`. Every other spec in that block inherits
// the type and the implicit iota expression from the first spec per Go's
// const-block rules, so checking only the first spec's Type identifies
// the block unambiguously; interfaces.go declares two other const blocks
// (Action, OperatorPhase) that this must not match.
//
// This is the test suite's actual source of truth for "how many ExprType
// constants exist today": unlike allExprTypes and
// TestAcceptDispatchesEveryExprTypeExhaustively's wantMethod table, it
// reads interfaces.go directly instead of re-declaring a hand-maintained
// count, so it changes automatically the moment interfaces.go does.
func countExprTypeConstants(t *testing.T) int {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("countExprTypeConstants: runtime.Caller(0) failed to resolve this test file's own path")
	}
	interfacesPath := filepath.Join(filepath.Dir(thisFile), "interfaces.go")

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, interfacesPath, nil, 0)
	if err != nil {
		t.Fatalf("countExprTypeConstants: parsing %s: %v", interfacesPath, err)
	}

	count := 0
	found := false
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.CONST || len(genDecl.Specs) == 0 {
			continue
		}

		first, ok := genDecl.Specs[0].(*ast.ValueSpec)
		if !ok || first.Type == nil {
			continue
		}
		ident, ok := first.Type.(*ast.Ident)
		if !ok || ident.Name != "ExprType" {
			continue
		}

		if found {
			t.Fatalf("countExprTypeConstants: %s declares more than one const "+
				"block whose first spec is typed ExprType; this helper's "+
				"block-identification assumption no longer holds", interfacesPath)
		}
		found = true

		for _, spec := range genDecl.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				t.Fatalf("countExprTypeConstants: ExprType const block in %s "+
					"contains a non-ValueSpec entry (%T)", interfacesPath, spec)
			}
			count += len(vs.Names)
		}
	}

	if !found {
		t.Fatalf("countExprTypeConstants: no const block whose first spec is "+
			"typed ExprType found in %s", interfacesPath)
	}
	return count
}

func TestAllExprTypesAccountedFor(t *testing.T) {
	sourceCount := countExprTypeConstants(t)

	if sourceCount != wantExhaustiveExprTypeCount {
		t.Fatalf("interfaces.go's ExprType const block declares %d constants, "+
			"but this test file's documented expectation "+
			"(wantExhaustiveExprTypeCount) says %d — interfaces.go changed "+
			"without this file being updated to match", sourceCount, wantExhaustiveExprTypeCount)
	}

	if len(allExprTypes) != sourceCount {
		t.Fatalf("allExprTypes has %d entries, but interfaces.go's ExprType "+
			"const block declares %d — the manual enumeration on allExprTypes "+
			"drifted from the real source (see the comment on allExprTypes)",
			len(allExprTypes), sourceCount)
	}

	seen := map[ExprType]bool{}
	for _, et := range allExprTypes {
		if seen[et] {
			t.Fatalf("ExprType %v listed twice in allExprTypes", et)
		}
		seen[et] = true
	}
}

// --- Walk: nil-safety ---------------------------------------------------

func TestWalkNilExpr(t *testing.T) {
	called := false
	Walk(nil, func(*Expr) bool {
		called = true
		return true
	})
	if called {
		t.Fatalf("Walk(nil, fn) called fn; want no-op")
	}
}

func TestWalkNilFn(t *testing.T) {
	// Must not panic.
	Walk(&Expr{Type: Literal, Literal: 1}, nil)
}

func TestWalkNilBranches(t *testing.T) {
	// Negate only ever sets Left; Right and Call are both nil. Walk must
	// not dereference either.
	e := &Expr{Type: Negate, Left: &Expr{Type: Literal, Literal: true}}
	var visited []ExprType
	Walk(e, func(n *Expr) bool {
		visited = append(visited, n.Type)
		return true
	})
	if len(visited) != 2 || visited[0] != Negate || visited[1] != Literal {
		t.Fatalf("Walk visited %v, want [Negate Literal]", visited)
	}
}

func TestWalkOperatorCallWithNilCallField(t *testing.T) {
	// An OperatorCall Expr with no Call object set (e.g. hand-built, not
	// yet resolved by the parser) has no children to descend into.
	e := &Expr{Type: OperatorCall, Operator: "grab"}
	var visited []ExprType
	Walk(e, func(n *Expr) bool {
		visited = append(visited, n.Type)
		return true
	})
	if len(visited) != 1 || visited[0] != OperatorCall {
		t.Fatalf("Walk visited %v, want [OperatorCall]", visited)
	}
}

// --- Walk: pre-order and short-circuit -----------------------------------

func TestWalkPreOrderHandBuilt(t *testing.T) {
	// (1 + 2) - 3, hand-built: Subtraction(Addition(1, 2), 3).
	one := &Expr{Type: Literal, Literal: 1}
	two := &Expr{Type: Literal, Literal: 2}
	three := &Expr{Type: Literal, Literal: 3}
	add := &Expr{Type: Addition, Left: one, Right: two}
	sub := &Expr{Type: Subtraction, Left: add, Right: three}

	var visited []*Expr
	Walk(sub, func(n *Expr) bool {
		visited = append(visited, n)
		return true
	})

	want := []*Expr{sub, add, one, two, three}
	if len(visited) != len(want) {
		t.Fatalf("visited %d nodes, want %d", len(visited), len(want))
	}
	for i := range want {
		if visited[i] != want[i] {
			t.Fatalf("visited[%d] = %p, want %p (pre-order: node, then Left "+
				"subtree, then Right subtree)", i, visited[i], want[i])
		}
	}
}

func TestWalkReturnFalsePrunesSubtreeNotSiblings(t *testing.T) {
	// LogicalAnd(Negate(a), b): returning false at the Negate node must
	// skip "a" but still visit "b" — the sibling on the parent's Right.
	a := &Expr{Type: Reference, Name: "a"}
	b := &Expr{Type: Reference, Name: "b"}
	neg := &Expr{Type: Negate, Left: a}
	and := &Expr{Type: LogicalAnd, Left: neg, Right: b}

	var visited []ExprType
	Walk(and, func(n *Expr) bool {
		visited = append(visited, n.Type)
		return n.Type != Negate
	})

	want := []ExprType{LogicalAnd, Negate, Reference}
	if len(visited) != len(want) {
		t.Fatalf("visited %v, want %v", visited, want)
	}
	for i := range want {
		if visited[i] != want[i] {
			t.Fatalf("visited %v, want %v", visited, want)
		}
	}
}

func TestWalkOperatorCallArgsInOrder(t *testing.T) {
	// (( concat a b c )): Call.Args() supplies the n-ary children, in
	// order, in addition to (never instead of) Left/Right — which concat's
	// own Expr does not set.
	a := &Expr{Type: Reference, Name: "a"}
	b := &Expr{Type: Reference, Name: "b"}
	c := &Expr{Type: Reference, Name: "c"}
	call := NewOpcall(nil, []*Expr{a, b, c}, "concat a b c")
	e := &Expr{Type: OperatorCall, Operator: "concat", Call: call}

	var visited []*Expr
	Walk(e, func(n *Expr) bool {
		visited = append(visited, n)
		return true
	})

	want := []*Expr{e, a, b, c}
	if len(visited) != len(want) {
		t.Fatalf("visited %d nodes, want %d", len(visited), len(want))
	}
	for i := range want {
		if visited[i] != want[i] {
			t.Fatalf("visited[%d] = %p, want %p", i, visited[i], want[i])
		}
	}
}

// --- Walk: full coverage over real parser output -------------------------

// walkedTypes runs Walk over src (parsed through mustParseInner, i.e. real
// operator-expression grammar, not a hand-built Expr) and returns the set
// of ExprType values it visited.
func walkedTypes(t *testing.T, src string) map[ExprType]bool {
	t.Helper()
	e := mustParseInner(t, src)
	seen := map[ExprType]bool{}
	Walk(e, func(n *Expr) bool {
		seen[n.Type] = true
		return true
	})
	return seen
}

// TestWalkCoversEveryParserProducedExprType parses one real operator
// expression per family and checks that Walk reaches every ExprType the
// parser is capable of emitting. Six of the 24 ExprType constants — List,
// Or, RegexpMatch, BoshVar, VaultGroup, VaultChoice — are never produced by
// the parser (pkg/graft/evaluate_infix.go's infixOperatorSymbols comment);
// those are covered separately in
// TestWalkAndAcceptCoverNonParserProducedTypes below, by constructing them
// directly.
func TestWalkCoversEveryParserProducedExprType(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []ExprType
	}{
		// "grab foo.bar" alone would not do: exprToOpcall (parser.go)
		// flattens a *top-level* OperatorCall Expr into the wrapping
		// Opcall directly, so mustParseInner's args[0] would be the
		// Reference, never an OperatorCall Expr node — and "grab foo.bar
		// || baz" would not either, since parseOperatorCall's own
		// argument loop treats a trailing "||" as grab's fallback
		// operand (the "(( grab this || that ))" form), producing
		// OperatorCall(grab, [LogicalOr(foo.bar, baz)]), which is still
		// the whole top-level expression and still gets flattened.
		// Nesting the call as one operand of a "+" keeps the
		// OperatorCall node alive as a child: the top-level expression
		// is then Addition, not OperatorCall, so exprToOpcall wraps the
		// whole tree in exprOperator instead of flattening it.
		{"operator call and reference", "grab foo.bar + 1", []ExprType{OperatorCall, Reference, Addition, Literal}},
		{"env var", "$MY_ENV_VAR", []ExprType{EnvVar}},
		{"negate", "!flag", []ExprType{Negate, Reference}},
		{"arithmetic chain", "1 + 2 - 3 * 4 / 5 % 6",
			[]ExprType{Addition, Subtraction, Multiplication, Division, Modulo, Literal}},
		{"comparison chain", "1 == 2 != 3 < 4 <= 5 > 6 >= 7",
			[]ExprType{Equal, NotEqual, LessThan, LessThanOrEqual, GreaterThan, GreaterThanOrEqual, Literal}},
		{"logical chain", "true && false || true",
			[]ExprType{LogicalAnd, LogicalOr, Literal}},
	}

	combined := map[ExprType]bool{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seen := walkedTypes(t, tc.src)
			for _, want := range tc.want {
				if !seen[want] {
					t.Errorf("Walk(%q) never visited %v; visited %v", tc.src, want, sortedTypes(seen))
				}
				combined[want] = true
			}
		})
	}

	parserProduced := []ExprType{
		Literal, Reference, Negate, EnvVar, OperatorCall,
		Addition, Subtraction, Multiplication, Division, Modulo,
		Equal, NotEqual, LessThan, LessThanOrEqual, GreaterThan, GreaterThanOrEqual,
		LogicalAnd, LogicalOr,
	}
	if len(parserProduced) != 18 {
		t.Fatalf("test bug: parserProduced has %d entries, want 18", len(parserProduced))
	}
	for _, et := range parserProduced {
		if !combined[et] {
			t.Errorf("no test case covered ExprType %v", et)
		}
	}
}

func sortedTypes(m map[ExprType]bool) []int {
	out := make([]int, 0, len(m))
	for et := range m {
		out = append(out, int(et))
	}
	sort.Ints(out)
	return out
}

// TestWalkAndAcceptCoverNonParserProducedTypes builds the six ExprType
// values the parser never emits directly (see the comment on
// TestWalkCoversEveryParserProducedExprType) and checks that both Walk and
// Accept handle them without panicking and without misclassifying them —
// Accept must route all six to VisitOther, since none has a dedicated
// Visitor method.
func TestWalkAndAcceptCoverNonParserProducedTypes(t *testing.T) {
	leaf := &Expr{Type: Literal, Literal: "x"}
	nonParserProduced := []*Expr{
		{Type: List, Left: leaf},
		{Type: Or, Left: leaf, Right: leaf},
		{Type: RegexpMatch, Left: leaf, Right: leaf},
		{Type: BoshVar, Name: "((az))"},
		{Type: VaultGroup, Left: leaf},
		{Type: VaultChoice, Left: leaf, Right: leaf},
	}
	if len(nonParserProduced) != 6 {
		t.Fatalf("test bug: nonParserProduced has %d entries, want 6", len(nonParserProduced))
	}

	for _, e := range nonParserProduced {
		t.Run(exprTypeName(e.Type), func(t *testing.T) {
			visitedRoot := false
			Walk(e, func(n *Expr) bool {
				if n == e {
					visitedRoot = true
				}
				return true
			})
			if !visitedRoot {
				t.Fatalf("Walk never visited the root node of type %v", e.Type)
			}

			rv := &recordingVisitor{}
			got := Accept(e, rv)
			if rv.calls != 1 || rv.lastMethod != "VisitOther" {
				t.Fatalf("Accept(%v, ...) dispatched to %q (%d calls), want exactly one call to VisitOther",
					e.Type, rv.lastMethod, rv.calls)
			}
			if got != e {
				t.Fatalf("Accept(%v, ...) returned %v, want the recordingVisitor's echoed node", e.Type, got)
			}
		})
	}
}

// exprTypeName gives subtests a readable name; ExprType has no String()
// method (it is a plain int-based enum with no Stringer in interfaces.go),
// so this is local to the test.
func exprTypeName(et ExprType) string {
	switch et {
	case Literal:
		return "Literal"
	case Reference:
		return "Reference"
	case List:
		return "List"
	case Or:
		return "Or"
	case Negate:
		return "Negate"
	case Addition:
		return "Addition"
	case Subtraction:
		return "Subtraction"
	case Multiplication:
		return "Multiplication"
	case Division:
		return "Division"
	case Modulo:
		return "Modulo"
	case Equal:
		return "Equal"
	case NotEqual:
		return "NotEqual"
	case LessThan:
		return "LessThan"
	case LessThanOrEqual:
		return "LessThanOrEqual"
	case GreaterThan:
		return "GreaterThan"
	case GreaterThanOrEqual:
		return "GreaterThanOrEqual"
	case LogicalAnd:
		return "LogicalAnd"
	case LogicalOr:
		return "LogicalOr"
	case RegexpMatch:
		return "RegexpMatch"
	case EnvVar:
		return "EnvVar"
	case BoshVar:
		return "BoshVar"
	case OperatorCall:
		return "OperatorCall"
	case VaultGroup:
		return "VaultGroup"
	case VaultChoice:
		return "VaultChoice"
	default:
		return "Unknown"
	}
}

// --- Accept: nil-safety ---------------------------------------------------

func TestAcceptNilExpr(t *testing.T) {
	rv := &recordingVisitor{}
	got := Accept(nil, rv)
	if got != nil {
		t.Fatalf("Accept(nil, v) = %v, want nil", got)
	}
	if rv.calls != 0 {
		t.Fatalf("Accept(nil, v) called a Visitor method %d times, want 0", rv.calls)
	}
}

func TestAcceptNilVisitor(t *testing.T) {
	// Must not panic.
	got := Accept(&Expr{Type: Literal, Literal: 1}, nil)
	if got != nil {
		t.Fatalf("Accept(e, nil) = %v, want nil", got)
	}
}

// --- Accept: dispatch table over every ExprType ---------------------------

// recordingVisitor implements Visitor by recording which method ran and
// echoing the node it was given, so tests can assert both "which method"
// and "was it actually the node I passed in".
type recordingVisitor struct {
	calls      int
	lastMethod string
}

func (r *recordingVisitor) record(method string, e *Expr) interface{} {
	r.calls++
	r.lastMethod = method
	return e
}

func (r *recordingVisitor) VisitLiteral(e *Expr) interface{}   { return r.record("VisitLiteral", e) }
func (r *recordingVisitor) VisitReference(e *Expr) interface{} { return r.record("VisitReference", e) }
func (r *recordingVisitor) VisitOperatorCall(e *Expr) interface{} {
	return r.record("VisitOperatorCall", e)
}
func (r *recordingVisitor) VisitBinaryOp(e *Expr) interface{} { return r.record("VisitBinaryOp", e) }
func (r *recordingVisitor) VisitUnaryOp(e *Expr) interface{}  { return r.record("VisitUnaryOp", e) }
func (r *recordingVisitor) VisitEnvVar(e *Expr) interface{}   { return r.record("VisitEnvVar", e) }
func (r *recordingVisitor) VisitOther(e *Expr) interface{}    { return r.record("VisitOther", e) }

func TestAcceptDispatchesEveryExprTypeExhaustively(t *testing.T) {
	wantMethod := map[ExprType]string{
		Literal:      "VisitLiteral",
		Reference:    "VisitReference",
		OperatorCall: "VisitOperatorCall",
		EnvVar:       "VisitEnvVar",
		Negate:       "VisitUnaryOp",

		Addition:           "VisitBinaryOp",
		Subtraction:        "VisitBinaryOp",
		Multiplication:     "VisitBinaryOp",
		Division:           "VisitBinaryOp",
		Modulo:             "VisitBinaryOp",
		Equal:              "VisitBinaryOp",
		NotEqual:           "VisitBinaryOp",
		LessThan:           "VisitBinaryOp",
		LessThanOrEqual:    "VisitBinaryOp",
		GreaterThan:        "VisitBinaryOp",
		GreaterThanOrEqual: "VisitBinaryOp",
		LogicalAnd:         "VisitBinaryOp",
		LogicalOr:          "VisitBinaryOp",

		List:        "VisitOther",
		Or:          "VisitOther",
		RegexpMatch: "VisitOther",
		BoshVar:     "VisitOther",
		VaultGroup:  "VisitOther",
		VaultChoice: "VisitOther",
	}

	// Checked against the parsed source count, not just the hand-maintained
	// wantExhaustiveExprTypeCount literal: this is the assertion M5 flagged
	// as missing — without it, a 25th ExprType left out of both this table
	// and allExprTypes would pass every other check in this file silently.
	sourceCount := countExprTypeConstants(t)
	if len(wantMethod) != sourceCount {
		t.Fatalf("wantMethod covers %d ExprTypes, but interfaces.go's ExprType "+
			"const block declares %d — a new ExprType was added without a "+
			"dispatch entry here", len(wantMethod), sourceCount)
	}
	if len(allExprTypes) != sourceCount {
		t.Fatalf("allExprTypes has %d entries, but interfaces.go's ExprType "+
			"const block declares %d — this test would silently under-cover "+
			"Accept's dispatch if it kept iterating allExprTypes alone",
			len(allExprTypes), sourceCount)
	}

	for _, et := range allExprTypes {
		want, ok := wantMethod[et]
		if !ok {
			t.Fatalf("allExprTypes contains %v, which has no entry in wantMethod — "+
				"update wantMethod (VisitOther is the safe default for any brand-new "+
				"ExprType, per the Visitor forward-compatibility contract)", et)
		}
		t.Run(exprTypeName(et), func(t *testing.T) {
			e := &Expr{Type: et}
			rv := &recordingVisitor{}
			got := Accept(e, rv)
			if rv.calls != 1 {
				t.Fatalf("Accept called %d Visitor methods, want exactly 1", rv.calls)
			}
			if rv.lastMethod != want {
				t.Fatalf("Accept(%v, ...) dispatched to %s, want %s", et, rv.lastMethod, want)
			}
			if got != e {
				t.Fatalf("Accept(%v, ...) returned %v, want the node itself (echoed by recordingVisitor)", et, got)
			}
		})
	}
}

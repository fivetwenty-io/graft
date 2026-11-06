package controlflow

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/exprlex"
	"github.com/fivetwenty-io/graft/pkg/graft"
	"github.com/fivetwenty-io/graft/pkg/graft/interfaces"
	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// lexerTestInputs collects every input that the tokenizer's own tests
// feed the lexer. It reads them from the interfaces package's
// tokenizer*_test.go files, so an input added there is checked here as
// well. It takes the "src" or "input" field of every table row and every
// fuzz seed.
func lexerTestInputs(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("..", "interfaces", "tokenizer*_test.go"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no tokenizer test files found: %v", err)
	}
	fset := token.NewFileSet()
	seen := map[string]bool{}
	var inputs []string
	add := func(e ast.Expr) {
		lit, ok := e.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return
		}
		s, uerr := strconv.Unquote(lit.Value)
		if uerr != nil || seen[s] {
			return
		}
		seen[s] = true
		inputs = append(inputs, s)
	}
	for _, path := range paths {
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			t.Fatalf("parsing %s: %v", path, perr)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CompositeLit:
				collectTableInputs(n, add)
			case *ast.AssignStmt:
				if len(n.Lhs) == 1 && len(n.Rhs) == 1 {
					if id, ok := n.Lhs[0].(*ast.Ident); ok && id.Name == "seeds" {
						if cl, ok := n.Rhs[0].(*ast.CompositeLit); ok {
							for _, e := range cl.Elts {
								add(e)
							}
						}
					}
				}
			}
			return true
		})
	}
	return inputs
}

// collectTableInputs hands add the "src" or "input" field of every row of
// a table literal of the form []struct{...}{...}.
func collectTableInputs(cl *ast.CompositeLit, add func(ast.Expr)) {
	arr, ok := cl.Type.(*ast.ArrayType)
	if !ok {
		return
	}
	st, ok := arr.Elt.(*ast.StructType)
	if !ok {
		return
	}
	index, pos := -1, 0
	field := ""
	for _, fl := range st.Fields.List {
		for _, nm := range fl.Names {
			if nm.Name == "src" || nm.Name == "input" {
				index, field = pos, nm.Name
			}
			pos++
		}
	}
	if index < 0 {
		return
	}
	for _, row := range cl.Elts {
		rl, ok := row.(*ast.CompositeLit)
		if !ok || len(rl.Elts) == 0 {
			continue
		}
		if _, keyed := rl.Elts[0].(*ast.KeyValueExpr); keyed {
			for _, e := range rl.Elts {
				if kv, ok := e.(*ast.KeyValueExpr); ok {
					if k, ok := kv.Key.(*ast.Ident); ok && k.Name == field {
						add(kv.Value)
					}
				}
			}
			continue
		}
		if index < len(rl.Elts) {
			add(rl.Elts[index])
		}
	}
}

type lexedToken struct {
	typ   interfaces.TokenType
	lit   string
	start int

	// headName marks the modifier or target name of an operator head,
	// such as "prod" in "vault@prod", which the lexer reports as part of
	// the call and not as a name in the document.
	headName bool
}

// lexForAgreement lexes src with the parser's own tokenizer options.
func lexForAgreement(src string) []lexedToken {
	tz := interfaces.NewAdvancedTokenizer(src, exprlex.Options(func(name string) bool {
		_, isNull := graft.OperatorForEngine(nil, name).(graft.NullOperator)
		return !isNull
	}))
	var out []lexedToken
	for tz.HasMore() {
		before := tz.Position()
		tok := tz.NextToken()
		if tok.Type == interfaces.TokenEOF || tz.Position() <= before {
			break
		}
		out = append(out, lexedToken{typ: tok.Type, lit: tok.Literal, start: tok.Pos.Offset, headName: tz.HeadName()})
	}
	return out
}

// expectedRewrite is the rewrite this test expects of one token. It is
// written from the lexer's tokens and the evaluator's own path reader,
// tree.ParseCursor, and not from the rewrite's code. An identifier equal
// to name is the variable, unless the lexer reads it as the modifier or
// target name of an operator head. A reference is the variable when its first
// node is name and it does not start with '$', and a bracket index is
// when its first dotted part is name.
func expectedRewrite(tok lexedToken, name, target string) (string, bool) {
	switch tok.typ {
	case interfaces.TokenIdentifier:
		if tok.lit == name && !tok.headName {
			return target, true
		}
	case interfaces.TokenReference:
		cursor, err := tree.ParseCursor(tok.lit)
		if err != nil {
			return "", false
		}
		bracketed := tree.BracketsOf(tok.lit)
		lit := tok.lit
		changed := false
		for i, node := range cursor.Nodes {
			if i >= len(bracketed) || !bracketed[i] {
				continue
			}
			if strings.SplitN(node, ".", 2)[0] == name {
				lit = strings.ReplaceAll(lit, "["+node+"]", "["+target+strings.TrimPrefix(node, name)+"]")
				changed = true
			}
		}
		if !strings.HasPrefix(tok.lit, "$") && len(cursor.Nodes) > 0 && cursor.Nodes[0] == name &&
			len(bracketed) > 0 && !bracketed[0] {
			lit = target + strings.TrimPrefix(lit, name)
			changed = true
		}
		return lit, changed
	default:
	}
	return "", false
}

// loopNameRe matches the names a for header accepts as a loop variable,
// and lexerNameRe matches every identifier the lexer reads, which may
// hold a '-' and any letter or digit. The agreement test binds the names
// of both shapes, so the rewrite is checked on "svc-1" as one name as
// well as on "svc" and "1".
var (
	loopNameRe  = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)
	lexerNameRe = regexp.MustCompile(`[\p{L}_][\p{L}\p{N}_-]*`)
)

// hyphenInputs adds inputs with hyphenated names to the lexer's own,
// which hold almost none, so the agreement test binds a name such as
// "my-var" in every position the lexer can read one.
var hyphenInputs = []string{
	"grab my-var",
	"grab my-var.ip",
	"grab svc-1",
	"svc - 1",
	"my-var-2 - my-var",
	`concat my-var "/" my-var`,
	"x || my-var",
	"my-var || x",
	"grab list[my-var]",
	"grab list[my-var.name]",
	"grab jobs.name=my-var.ip",
	"grab jobs[name=my-var].ip",
	"grab $.my-var.k",
	"grab $my-var",
	"grab a/my-var",
	"grab /my-var",
	"my-var+x",
	"my-var ? a-b : b-a",
	"1e+5/2 - my-var",
}

// TestRewriteAgreesWithLexer runs every lexer test input through the
// lexer and through rewriteVarRefs, once for each name in the input,
// bound as a loop variable. A name is any run that a for header accepts
// or that the lexer reads as one identifier, hyphens included. The two must agree on which spans name the
// variable. The rewritten text must also lex into the same tokens, with
// each rewritten identifier now a reference, so the rewrite never
// changes how the rest of the expression reads. That second check skips
// a variable named like an operator, because such a name in operator
// position opens a call, while the loop variable it becomes is an
// operand, so a '/' right after it divides.
func TestRewriteAgreesWithLexer(t *testing.T) {
	inputs := lexerTestInputs(t)
	if len(inputs) < 100 {
		t.Fatalf("found only %d lexer test inputs; the harvest from the tokenizer tests is broken", len(inputs))
	}
	for _, src := range append(inputs, hyphenInputs...) {
		toks := lexForAgreement(src)
		names := map[string]bool{}
		for _, re := range []*regexp.Regexp{loopNameRe, lexerNameRe} {
			for _, nm := range re.FindAllString(src, -1) {
				names[nm] = true
			}
		}
		for name := range names {
			target := "__graft_loop.lT.0." + name
			want := expectedText(src, toks, name, target)
			got := rewriteVarRefs(src, map[string]string{name: target}, nil)
			if got != want {
				t.Errorf("input %q, variable %q:\n got  %q\n want %q", src, name, got, want)
				continue
			}
			if _, isNull := graft.OperatorForEngine(nil, name).(graft.NullOperator); !isNull {
				continue
			}
			checkSameTokens(t, src, name, toks, lexForAgreement(got))
		}
	}
}

// expectedText rebuilds src with every token that expectedRewrite
// rewrites replaced. A rewritten identifier directly followed by '+' or
// '=' gets a space after it, because a dotted path would glue "+name" or
// "=value" onto its last segment.
func expectedText(src string, toks []lexedToken, name, target string) string {
	var b strings.Builder
	if len(toks) > 0 {
		b.WriteString(src[:toks[0].start])
	}
	for i, tok := range toks {
		next := len(src)
		if i+1 < len(toks) {
			next = toks[i+1].start
		}
		lit, ok := expectedRewrite(tok, name, target)
		if !ok {
			b.WriteString(src[tok.start:next])
			continue
		}
		b.WriteString(lit)
		after := tok.start + len(tok.lit)
		if tok.typ == interfaces.TokenIdentifier && after < len(src) && (src[after] == '+' || src[after] == '=') {
			b.WriteByte(' ')
		}
		b.WriteString(src[after:next])
	}
	return b.String()
}

func checkSameTokens(t *testing.T, src, name string, before, after []lexedToken) {
	t.Helper()
	if len(before) != len(after) {
		t.Errorf("input %q, variable %q: the rewrite changed the token count from %d to %d", src, name, len(before), len(after))
		return
	}
	for i := range before {
		bt, at := before[i].typ, after[i].typ
		if bt == at || (bt == interfaces.TokenIdentifier && at == interfaces.TokenReference) {
			continue
		}
		t.Errorf("input %q, variable %q: token %d changed from %v to %v", src, name, i, bt, at)
	}
}

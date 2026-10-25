package parser

import (
	"errors"
	"fmt"
	"strings"

	"github.com/fivetwenty-io/graft/internal/query/ast"
	"github.com/fivetwenty-io/graft/internal/query/lexer"
)

// TopLevelFile is the file name that $__loc__ reports for a program given
// on the command line.
const TopLevelFile = "<top-level>"

// maxDepth is jq's parser nesting limit.
const maxDepth = 10000

// Error is a parse failure at a byte offset in the source.
type Error struct {
	Pos int
	Msg string
}

// Error implements the error interface.
func (e *Error) Error() string {
	return fmt.Sprintf("%s at offset %d", e.Msg, e.Pos)
}

type parser struct {
	src   string
	file  string
	g     Grammar
	toks  []lexer.Token
	i     int
	depth int
}

// Parse reads a whole program. $__loc__ reports TopLevelFile.
func Parse(src string, g Grammar) (ast.Node, error) {
	return ParseFile(src, TopLevelFile, g)
}

// ParseFile is Parse for a program that comes from the named file, which
// is the file name that $__loc__ reports.
func ParseFile(src, file string, g Grammar) (ast.Node, error) {
	toks, err := lexer.Tokenize(src, g.Tokens)
	if err != nil {
		var le *lexer.Error
		if errors.As(err, &le) {
			return nil, &Error{Pos: le.Pos, Msg: le.Msg}
		}
		return nil, err
	}
	p := &parser{src: src, file: file, g: g, toks: toks}
	return p.parseProgram()
}

func (p *parser) peek() lexer.Token { return p.toks[p.i] }

func (p *parser) peekAt(n int) lexer.Token {
	if p.i+n >= len(p.toks) {
		return p.toks[len(p.toks)-1]
	}
	return p.toks[p.i+n]
}

func (p *parser) next() lexer.Token {
	t := p.toks[p.i]
	if t.Kind != lexer.EOF {
		p.i++
	}
	return t
}

func (p *parser) errAt(tok lexer.Token, msg string) error {
	return &Error{Pos: tok.Pos, Msg: msg}
}

func describe(tok lexer.Token) string {
	if tok.Kind == lexer.EOF {
		return "end of input"
	}
	return "'" + tokenSpelling(tok) + "'"
}

// tokenSpelling is how a token appears in a diagnostic.
func tokenSpelling(tok lexer.Token) string {
	if tok.Kind == lexer.StrStart {
		return `"`
	}
	return tok.Text
}

func (p *parser) unexpected(tok lexer.Token) error {
	return p.errAt(tok, "unexpected "+describe(tok))
}

func (p *parser) expected(want string) error {
	tok := p.peek()
	return p.errAt(tok, fmt.Sprintf("expected '%s', found %s", want, describe(tok)))
}

func (p *parser) expect(kind lexer.TokenKind, spelling string) error {
	if p.peek().Kind != kind {
		return p.expected(spelling)
	}
	p.next()
	return nil
}

func (p *parser) isKeyword(word string) bool {
	t := p.peek()
	return t.Kind == lexer.Keyword && t.Text == word
}

func (p *parser) expectKeyword(word string) error {
	if !p.isKeyword(word) {
		return p.expected(word)
	}
	p.next()
	return nil
}

// parseProgram reads the module directives, the library definitions that
// may stand alone at the head of a program, and then the body.
func (p *parser) parseProgram() (ast.Node, error) {
	var chain []ast.Node
	if p.g.Keywords.Modules {
		var err error
		if chain, err = p.parseDirectives(); err != nil {
			return nil, err
		}
	}
	for p.g.Keywords.Def && p.isKeyword("def") {
		def, err := p.parseFuncDefHead()
		if err != nil {
			return nil, err
		}
		chain = append(chain, def)
	}
	var body ast.Node
	if p.peek().Kind == lexer.EOF {
		if len(chain) == 0 {
			body = &ast.Identity{}
		}
	} else {
		var err error
		if body, err = p.parseExpr(); err != nil {
			return nil, err
		}
	}
	if tok := p.peek(); tok.Kind != lexer.EOF {
		return nil, p.unexpected(tok)
	}
	for i := len(chain) - 1; i >= 0; i-- {
		setRest(chain[i], body)
		body = chain[i]
	}
	return body, nil
}

func setRest(directive, rest ast.Node) {
	switch d := directive.(type) {
	case *ast.Module:
		d.Rest = rest
	case *ast.Import:
		d.Rest = rest
	case *ast.Include:
		d.Rest = rest
	case *ast.FuncDef:
		d.Rest = rest
	}
}

func (p *parser) parseDirectives() ([]ast.Node, error) {
	var out []ast.Node
	if t := p.peek(); t.Kind == lexer.Keyword && t.Text == "module" {
		p.next()
		meta, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(lexer.Semicolon, ";"); err != nil {
			return nil, err
		}
		out = append(out, &ast.Module{Meta: meta})
	}
	for p.isKeyword("import") || p.isKeyword("include") {
		d, err := p.parseImport()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

func (p *parser) parseImport() (ast.Node, error) {
	isImport := p.next().Text == "import"
	path, err := p.parseConstString()
	if err != nil {
		return nil, err
	}
	imp := &ast.Import{Path: path}
	if isImport {
		if err := p.expectKeyword("as"); err != nil {
			return nil, err
		}
		tok := p.next()
		switch tok.Kind {
		case lexer.Ident:
			imp.Alias = tok.Text
		case lexer.Variable:
			imp.Alias, imp.Data = tok.Text[1:], true
		default:
			return nil, p.unexpected(tok)
		}
	}
	if p.peek().Kind != lexer.Semicolon {
		if imp.Meta, err = p.parseExpr(); err != nil {
			return nil, err
		}
	}
	if err := p.expect(lexer.Semicolon, ";"); err != nil {
		return nil, err
	}
	if isImport {
		return imp, nil
	}
	return &ast.Include{Path: path, Meta: imp.Meta}, nil
}

// rowFor finds the infix row that a token spells.
func (p *parser) rowFor(tok lexer.Token) (Row, bool) {
	if tok.Kind < lexer.Dot && tok.Kind != lexer.Keyword {
		return Row{}, false
	}
	for _, r := range p.g.Ops {
		if r.Spelling == tok.Text {
			return r, true
		}
	}
	return Row{}, false
}

func (p *parser) rowOfKind(kind RowKind) (Row, bool) {
	for _, r := range p.g.Ops {
		if r.Kind == kind {
			return r, true
		}
	}
	return Row{}, false
}

// exprContext says which of the forms that swallow the rest of an
// expression are allowed where an expression starts. jq allows "def" and
// "label" only at the start of a pipeline or comma operand, and "as" in
// every operand except an object value.
type exprContext struct {
	def  bool
	bind bool
}

var fullContext = exprContext{def: true, bind: true}

// enter counts one level of parser nesting. The returned function leaves
// it, and the error is set when the nesting limit is passed.
func (p *parser) enter() (func(), error) {
	p.depth++
	leave := func() { p.depth-- }
	if p.depth > maxDepth {
		return leave, p.errAt(p.peek(), "exceeds maximum parser nesting depth")
	}
	return leave, nil
}

// parseExpr reads an expression that starts a fresh context.
func (p *parser) parseExpr() (ast.Node, error) {
	return p.parseExprIn(0, fullContext)
}

// parseExprIn is the Pratt loop. It reads one operand and then every infix
// operator that binds at least as tightly as minPrec.
func (p *parser) parseExprIn(minPrec int, c exprContext) (ast.Node, error) {
	leave, err := p.enter()
	defer leave()
	if err != nil {
		return nil, err
	}
	left, err := p.parsePrefix(c)
	if err != nil {
		return nil, err
	}
	for {
		tok := p.peek()
		row, ok := p.rowFor(tok)
		if !ok || row.Prec < minPrec {
			return left, nil
		}
		p.next()
		rhsMin := row.Prec + 1
		if row.Assoc == Right {
			rhsMin = row.Prec
		}
		rc := exprContext{def: c.def && (row.Kind == Pipe || row.Kind == Comma), bind: c.bind}
		right, err := p.parseExprIn(rhsMin, rc)
		if err != nil {
			return nil, err
		}
		left = buildInfix(row, left, right)
		if row.Assoc != NonAssoc {
			continue
		}
		if nt := p.peek(); nt.Kind != lexer.EOF {
			if nr, ok := p.rowFor(nt); ok && nr.Prec == row.Prec && nr.Assoc == NonAssoc {
				return nil, p.errAt(nt, fmt.Sprintf("non-associative operator '%s' cannot follow '%s'", nt.Text, tok.Text))
			}
		}
	}
}

func buildInfix(row Row, l, r ast.Node) ast.Node {
	switch row.Kind {
	case Pipe:
		return &ast.Pipe{L: l, R: r}
	case Comma:
		return &ast.Comma{L: l, R: r}
	case Alt:
		return &ast.Alt{L: l, R: r}
	case Assign:
		return &ast.Assign{Op: row.Spelling, L: l, R: r}
	default:
		return &ast.Binary{Op: row.Spelling, L: l, R: r}
	}
}

// parsePrefix reads the forms that open an expression: the definitions
// and labels that swallow what follows, or an operand with an optional
// binding.
func (p *parser) parsePrefix(c exprContext) (ast.Node, error) {
	tok := p.peek()
	kw := p.g.Keywords
	if tok.Kind == lexer.Keyword && c.def {
		switch {
		case tok.Text == "def" && kw.Def:
			return p.parseFuncDef()
		case tok.Text == "label" && kw.Label:
			return p.parseLabel()
		}
	}
	operand, err := p.parseOperand()
	if err != nil {
		return nil, err
	}
	if c.bind && kw.Bind && p.isKeyword("as") {
		return p.parseBind(operand)
	}
	return operand, nil
}

// parseOperand reads a term, a negated operand, or a try expression. None
// of them takes in a following "as", which binds the whole operand.
func (p *parser) parseOperand() (ast.Node, error) {
	tok := p.peek()
	switch {
	case tok.Kind == lexer.Minus:
		p.next()
		x, err := p.parseOperand()
		if err != nil {
			return nil, err
		}
		return &ast.Unary{Op: "-", X: x}, nil
	case tok.Kind == lexer.Keyword && tok.Text == "try" && p.g.Keywords.Try:
		return p.parseTry()
	}
	return p.parseTerm()
}

func (p *parser) parseBind(src ast.Node) (ast.Node, error) {
	p.next()
	pats, err := p.parsePatterns()
	if err != nil {
		return nil, err
	}
	if err := p.expect(lexer.Pipe, "|"); err != nil {
		return nil, err
	}
	body, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	return &ast.Bind{Src: src, Pats: pats, Body: body}, nil
}

func (p *parser) parseTry() (ast.Node, error) {
	p.next()
	body, err := p.parseOperand()
	if err != nil {
		return nil, err
	}
	t := &ast.Try{Body: body}
	if p.isKeyword("catch") {
		p.next()
		if t.Catch, err = p.parseOperand(); err != nil {
			return nil, err
		}
	}
	return t, nil
}

func (p *parser) parseLabel() (ast.Node, error) {
	p.next()
	tok := p.peek()
	if tok.Kind != lexer.Variable {
		return nil, p.expected("$name")
	}
	p.next()
	if err := p.expect(lexer.Pipe, "|"); err != nil {
		return nil, err
	}
	body, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	return &ast.Label{Name: tok.Text[1:], Body: body}, nil
}

// parseFuncDef reads a definition and the expression that follows it.
func (p *parser) parseFuncDef() (ast.Node, error) {
	def, err := p.parseFuncDefHead()
	if err != nil {
		return nil, err
	}
	def.Rest, err = p.parseExpr()
	return def, err
}

// parseFuncDefHead reads "def name(params): body;" and leaves Rest nil.
func (p *parser) parseFuncDefHead() (*ast.FuncDef, error) {
	p.next()
	name := p.next()
	if name.Kind != lexer.Ident {
		return nil, p.unexpected(name)
	}
	def := &ast.FuncDef{Name: name.Text}
	if p.peek().Kind == lexer.LParen {
		p.next()
		for {
			param := p.next()
			if param.Kind != lexer.Ident && param.Kind != lexer.Variable {
				return nil, p.unexpected(param)
			}
			def.Params = append(def.Params, param.Text)
			if p.peek().Kind != lexer.Semicolon {
				break
			}
			p.next()
		}
		if err := p.expect(lexer.RParen, ")"); err != nil {
			return nil, err
		}
	}
	if err := p.expect(lexer.Colon, ":"); err != nil {
		return nil, err
	}
	var err error
	if def.Body, err = p.parseExpr(); err != nil {
		return nil, err
	}
	return def, p.expect(lexer.Semicolon, ";")
}

// parseTerm reads a primary and the postfix forms that bind to it.
func (p *parser) parseTerm() (ast.Node, error) {
	first := p.peek()
	t, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	// last is the node that a following "?" can mark as optional. It is a
	// node this loop built, or a leading ".name" or ."str" that was not
	// parenthesized.
	var last ast.Node
	if first.Kind == lexer.Field || first.Kind == lexer.Dot {
		switch t.(type) {
		case *ast.Field, *ast.Index:
			last = t
		}
	}
	for {
		tok := p.peek()
		switch tok.Kind {
		case lexer.Field:
			p.next()
			t = &ast.Field{Target: target(t), Name: tok.Text[1:]}
			last = t
		case lexer.Dot:
			nt := p.peekAt(1)
			if nt.Kind != lexer.StrStart && nt.Kind != lexer.LBracket {
				return t, nil
			}
			p.next()
			if t, err = p.parseDotted(t); err != nil {
				return nil, err
			}
			last = t
		case lexer.LBracket:
			if t, err = p.parseBracket(t); err != nil {
				return nil, err
			}
			last = t
		case lexer.Question:
			p.next()
			if last != nil {
				setOpt(last)
				last = nil
			} else {
				t = &ast.Optional{X: t}
			}
		default:
			return t, nil
		}
	}
}

// parseDotted reads what follows a "." that sits between a term and a
// string key or a bracket.
func (p *parser) parseDotted(t ast.Node) (ast.Node, error) {
	if p.peek().Kind == lexer.LBracket {
		return p.parseBracket(t)
	}
	key, err := p.parseString("")
	if err != nil {
		return nil, err
	}
	return &ast.Index{Target: target(t), Key: key}, nil
}

// target is the Target a postfix node gets for t. The input itself is nil.
func target(t ast.Node) ast.Node {
	if _, ok := t.(*ast.Identity); ok {
		return nil
	}
	return t
}

func setOpt(n ast.Node) {
	switch n := n.(type) {
	case *ast.Field:
		n.Opt = true
	case *ast.Index:
		n.Opt = true
	case *ast.Slice:
		n.Opt = true
	case *ast.Iterate:
		n.Opt = true
	}
}

// parseBracket reads "[]", "[key]", and the slice forms.
func (p *parser) parseBracket(t ast.Node) (ast.Node, error) {
	p.next()
	tgt := target(t)
	if p.peek().Kind == lexer.RBracket {
		p.next()
		return &ast.Iterate{Target: tgt}, nil
	}
	var from ast.Node
	if p.peek().Kind != lexer.Colon {
		var err error
		if from, err = p.parseExpr(); err != nil {
			return nil, err
		}
		if p.peek().Kind != lexer.Colon {
			if err := p.expect(lexer.RBracket, "]"); err != nil {
				return nil, err
			}
			return &ast.Index{Target: tgt, Key: from}, nil
		}
	}
	p.next()
	var to ast.Node
	if p.peek().Kind != lexer.RBracket {
		var err error
		if to, err = p.parseExpr(); err != nil {
			return nil, err
		}
	}
	if err := p.expect(lexer.RBracket, "]"); err != nil {
		return nil, err
	}
	return &ast.Slice{Target: tgt, From: from, To: to}, nil
}

func (p *parser) parsePrimary() (ast.Node, error) {
	tok := p.peek()
	switch tok.Kind {
	case lexer.Number:
		p.next()
		return &ast.Literal{Value: ast.Number(tok.Text)}, nil
	case lexer.StrStart:
		return p.parseString("")
	case lexer.Format:
		p.next()
		if p.peek().Kind == lexer.StrStart {
			return p.parseString(tok.Text)
		}
		return &ast.FormatRef{Name: tok.Text}, nil
	case lexer.Dot:
		return p.parseLeadingDot()
	case lexer.DotDot:
		p.next()
		return &ast.RecurseDefault{}, nil
	case lexer.Field:
		p.next()
		return &ast.Field{Name: tok.Text[1:]}, nil
	case lexer.Variable:
		p.next()
		return &ast.Var{Name: tok.Text[1:]}, nil
	case lexer.Loc:
		p.next()
		return p.locAt(tok), nil
	case lexer.LParen:
		return p.parseParen()
	case lexer.LBracket:
		return p.parseArray()
	case lexer.LBrace:
		return p.parseObject()
	case lexer.Ident:
		return p.parseIdent()
	case lexer.Keyword:
		return p.parseKeywordTerm(tok)
	default:
		return nil, p.unexpected(tok)
	}
}

func (p *parser) locAt(tok lexer.Token) *ast.Loc {
	return &ast.Loc{File: p.file, Line: 1 + strings.Count(p.src[:tok.Pos], "\n")}
}

// parseLeadingDot reads "." and the string key that can follow it.
func (p *parser) parseLeadingDot() (ast.Node, error) {
	p.next()
	if p.peek().Kind == lexer.StrStart {
		key, err := p.parseString("")
		if err != nil {
			return nil, err
		}
		return &ast.Index{Key: key}, nil
	}
	return &ast.Identity{}, nil
}

func (p *parser) parseParen() (ast.Node, error) {
	p.next()
	e, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if err := p.expect(lexer.RParen, ")"); err != nil {
		return nil, err
	}
	return e, nil
}

func (p *parser) parseArray() (ast.Node, error) {
	p.next()
	if p.peek().Kind == lexer.RBracket {
		p.next()
		return &ast.ArrayCons{}, nil
	}
	body, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if err := p.expect(lexer.RBracket, "]"); err != nil {
		return nil, err
	}
	return &ast.ArrayCons{Body: body}, nil
}

func (p *parser) parseIdent() (ast.Node, error) {
	tok := p.next()
	switch tok.Text {
	case "true":
		return &ast.Literal{Value: true}, nil
	case "false":
		return &ast.Literal{Value: false}, nil
	case "null":
		return &ast.Literal{}, nil
	}
	call := &ast.Call{Name: tok.Text}
	if p.peek().Kind != lexer.LParen {
		return call, nil
	}
	p.next()
	for {
		arg, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		call.Args = append(call.Args, arg)
		if p.peek().Kind != lexer.Semicolon {
			break
		}
		p.next()
	}
	return call, p.expect(lexer.RParen, ")")
}

func (p *parser) parseKeywordTerm(tok lexer.Token) (ast.Node, error) {
	kw := p.g.Keywords
	switch {
	case tok.Text == "if" && kw.If:
		return p.parseIf()
	case tok.Text == "reduce" && kw.Reduce:
		return p.parseReduce()
	case tok.Text == "foreach" && kw.Foreach:
		return p.parseForeach()
	case tok.Text == "break" && kw.Label:
		return p.parseBreak()
	}
	return nil, p.unexpected(tok)
}

func (p *parser) parseIf() (ast.Node, error) {
	p.next()
	n := &ast.If{}
	var err error
	if n.Cond, err = p.parseExpr(); err != nil {
		return nil, err
	}
	if err := p.expectKeyword("then"); err != nil {
		return nil, err
	}
	if n.Then, err = p.parseExpr(); err != nil {
		return nil, err
	}
	for p.isKeyword("elif") {
		p.next()
		var e ast.Elif
		if e.Cond, err = p.parseExpr(); err != nil {
			return nil, err
		}
		if err := p.expectKeyword("then"); err != nil {
			return nil, err
		}
		if e.Then, err = p.parseExpr(); err != nil {
			return nil, err
		}
		n.Elifs = append(n.Elifs, e)
	}
	if p.isKeyword("else") {
		p.next()
		if n.Else, err = p.parseExpr(); err != nil {
			return nil, err
		}
	}
	return n, p.expectKeyword("end")
}

// parseLoopHead reads "term as patterns (" for reduce and foreach.
func (p *parser) parseLoopHead() (ast.Node, []ast.Pattern, error) {
	p.next()
	src, err := p.parseTerm()
	if err != nil {
		return nil, nil, err
	}
	if err := p.expectKeyword("as"); err != nil {
		return nil, nil, err
	}
	pats, err := p.parsePatterns()
	if err != nil {
		return nil, nil, err
	}
	return src, pats, p.expect(lexer.LParen, "(")
}

func (p *parser) parseReduce() (ast.Node, error) {
	src, pats, err := p.parseLoopHead()
	if err != nil {
		return nil, err
	}
	n := &ast.Reduce{Src: src, Pats: pats}
	if n.Init, err = p.parseExpr(); err != nil {
		return nil, err
	}
	if err := p.expect(lexer.Semicolon, ";"); err != nil {
		return nil, err
	}
	if n.Update, err = p.parseExpr(); err != nil {
		return nil, err
	}
	return n, p.expect(lexer.RParen, ")")
}

func (p *parser) parseForeach() (ast.Node, error) {
	src, pats, err := p.parseLoopHead()
	if err != nil {
		return nil, err
	}
	n := &ast.Foreach{Src: src, Pats: pats}
	if n.Init, err = p.parseExpr(); err != nil {
		return nil, err
	}
	if err := p.expect(lexer.Semicolon, ";"); err != nil {
		return nil, err
	}
	if n.Update, err = p.parseExpr(); err != nil {
		return nil, err
	}
	if p.peek().Kind == lexer.Semicolon {
		p.next()
		if n.Extract, err = p.parseExpr(); err != nil {
			return nil, err
		}
	}
	return n, p.expect(lexer.RParen, ")")
}

func (p *parser) parseBreak() (ast.Node, error) {
	p.next()
	label := p.peek()
	if label.Kind != lexer.Variable {
		return nil, p.expected("$name")
	}
	p.next()
	return &ast.Break{Name: label.Text[1:]}, nil
}

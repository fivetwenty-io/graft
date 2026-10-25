package parser

import (
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
	// tight is above every row's precedence, so parseExpr(tight) reads a
	// single operand with no infix operators.
	tight int
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
		return nil, err
	}
	p := &parser{src: src, file: file, g: g, toks: toks, tight: 1}
	for _, r := range g.Ops {
		if r.Prec >= p.tight {
			p.tight = r.Prec + 1
		}
	}
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
	return "'" + p0(tok) + "'"
}

func p0(tok lexer.Token) string {
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

// parseProgram reads the module directives, then the body.
func (p *parser) parseProgram() (ast.Node, error) {
	var directives []ast.Node
	if p.g.Keywords.Modules {
		var err error
		if directives, err = p.parseDirectives(); err != nil {
			return nil, err
		}
	}
	var body ast.Node
	if p.peek().Kind == lexer.EOF {
		if len(directives) == 0 {
			body = &ast.Identity{}
		}
	} else {
		var err error
		if body, err = p.parseExpr(0); err != nil {
			return nil, err
		}
	}
	if tok := p.peek(); tok.Kind != lexer.EOF {
		return nil, p.unexpected(tok)
	}
	for i := len(directives) - 1; i >= 0; i-- {
		setRest(directives[i], body)
		body = directives[i]
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
	}
}

func (p *parser) parseDirectives() ([]ast.Node, error) {
	var out []ast.Node
	if t := p.peek(); t.Kind == lexer.Keyword && t.Text == "module" {
		p.next()
		if p.peek().Kind != lexer.LBrace {
			return nil, p.expected("{")
		}
		meta, err := p.parseTerm()
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
		if imp.Meta, err = p.parseTerm(); err != nil {
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

// parseConstString reads a string literal that has no interpolation.
func (p *parser) parseConstString() (string, error) {
	start := p.peek()
	s, err := p.parseString("")
	if err != nil {
		return "", err
	}
	path := ""
	for _, part := range s.Parts {
		lit, ok := part.(*ast.Literal)
		text, isText := "", false
		if ok {
			text, isText = lit.Value.(string)
		}
		if !isText {
			return "", p.errAt(start, "import path must be a constant string")
		}
		path += text
	}
	return path, nil
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

// parseExpr is the Pratt loop. It reads one operand and then every infix
// operator that binds at least as tightly as minPrec.
func (p *parser) parseExpr(minPrec int) (ast.Node, error) {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > maxDepth {
		return nil, p.errAt(p.peek(), "exceeds maximum parser nesting depth")
	}
	left, err := p.parsePrefix()
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
		right, err := p.parseExpr(rhsMin)
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

// parsePrefix reads the forms that open an operand: the keyword forms that
// swallow what follows, unary minus, and a term with an optional binding.
func (p *parser) parsePrefix() (ast.Node, error) {
	tok := p.peek()
	kw := p.g.Keywords
	switch {
	case tok.Kind == lexer.Keyword && tok.Text == "try" && kw.Try:
		return p.parseTry()
	case tok.Kind == lexer.Keyword && tok.Text == "def" && kw.Def:
		return p.parseFuncDef()
	case tok.Kind == lexer.Keyword && tok.Text == "label" && kw.Label:
		return p.parseLabel()
	case tok.Kind == lexer.Minus:
		return p.parseNegation()
	}
	term, err := p.parseTerm()
	if err != nil {
		return nil, err
	}
	if kw.Bind && p.isKeyword("as") {
		return p.parseBind(term)
	}
	return term, nil
}

func (p *parser) parseNegation() (ast.Node, error) {
	p.next()
	operandMin := p.tight
	if r, ok := p.rowFor(lexer.Token{Kind: lexer.Minus, Text: "-"}); ok {
		operandMin = r.Prec + 1
	}
	x, err := p.parseExpr(operandMin)
	if err != nil {
		return nil, err
	}
	return &ast.Unary{Op: "-", X: x}, nil
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
	body, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	return &ast.Bind{Src: src, Pats: pats, Body: body}, nil
}

func (p *parser) parseTry() (ast.Node, error) {
	p.next()
	body, err := p.parseExpr(p.tight)
	if err != nil {
		return nil, err
	}
	t := &ast.Try{Body: body}
	if p.isKeyword("catch") {
		p.next()
		if t.Catch, err = p.parseExpr(p.tight); err != nil {
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
	body, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	return &ast.Label{Name: tok.Text[1:], Body: body}, nil
}

func (p *parser) parseFuncDef() (ast.Node, error) {
	p.next()
	name := p.next()
	if name.Kind != lexer.Ident && name.Kind != lexer.Keyword {
		return nil, p.unexpected(name)
	}
	def := &ast.FuncDef{Name: name.Text}
	if p.peek().Kind == lexer.LParen {
		p.next()
		for {
			param := p.next()
			if param.Kind != lexer.Ident && param.Kind != lexer.Variable && param.Kind != lexer.Keyword {
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
	if def.Body, err = p.parseExpr(0); err != nil {
		return nil, err
	}
	if err := p.expect(lexer.Semicolon, ";"); err != nil {
		return nil, err
	}
	if p.peek().Kind == lexer.EOF {
		return def, nil
	}
	def.Rest, err = p.parseExpr(0)
	return def, err
}

// parseTerm reads a primary and the postfix forms that bind to it.
func (p *parser) parseTerm() (ast.Node, error) {
	t, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	// last is the node that a following "?" can mark as optional, which
	// is only a node this loop built.
	var last ast.Node
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
		if from, err = p.parseExpr(0); err != nil {
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
		if to, err = p.parseExpr(0); err != nil {
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
	dot := p.next()
	if nt := p.peek(); nt.Kind == lexer.StrStart && nt.Pos == dot.End {
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
	e, err := p.parseExpr(0)
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
	body, err := p.parseExpr(0)
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
		arg, err := p.parseExpr(0)
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
	if n.Cond, err = p.parseExpr(0); err != nil {
		return nil, err
	}
	if err := p.expectKeyword("then"); err != nil {
		return nil, err
	}
	if n.Then, err = p.parseExpr(0); err != nil {
		return nil, err
	}
	for p.isKeyword("elif") {
		p.next()
		var e ast.Elif
		if e.Cond, err = p.parseExpr(0); err != nil {
			return nil, err
		}
		if err := p.expectKeyword("then"); err != nil {
			return nil, err
		}
		if e.Then, err = p.parseExpr(0); err != nil {
			return nil, err
		}
		n.Elifs = append(n.Elifs, e)
	}
	if p.isKeyword("else") {
		p.next()
		if n.Else, err = p.parseExpr(0); err != nil {
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
	if n.Init, err = p.parseExpr(0); err != nil {
		return nil, err
	}
	if err := p.expect(lexer.Semicolon, ";"); err != nil {
		return nil, err
	}
	if n.Update, err = p.parseExpr(0); err != nil {
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
	if n.Init, err = p.parseExpr(0); err != nil {
		return nil, err
	}
	if err := p.expect(lexer.Semicolon, ";"); err != nil {
		return nil, err
	}
	if n.Update, err = p.parseExpr(0); err != nil {
		return nil, err
	}
	if p.peek().Kind == lexer.Semicolon {
		p.next()
		if n.Extract, err = p.parseExpr(0); err != nil {
			return nil, err
		}
	}
	return n, p.expect(lexer.RParen, ")")
}

// parseString reads a string literal from its opening quote to its closing
// quote. format is the "@name" prefix, which the caller has consumed.
func (p *parser) parseString(format string) (*ast.Str, error) {
	if err := p.expect(lexer.StrStart, `"`); err != nil {
		return nil, err
	}
	s := &ast.Str{Format: format}
	for {
		tok := p.next()
		switch tok.Kind {
		case lexer.StrLit:
			s.Parts = append(s.Parts, &ast.Literal{Value: tok.Text})
		case lexer.InterpStart:
			e, err := p.parseExpr(0)
			if err != nil {
				return nil, err
			}
			if err := p.expect(lexer.InterpEnd, ")"); err != nil {
				return nil, err
			}
			s.Parts = append(s.Parts, e)
		case lexer.StrEnd:
			return s, nil
		default:
			return nil, p.unexpected(tok)
		}
	}
}

// parseObject reads "{...}".
func (p *parser) parseObject() (ast.Node, error) {
	p.next()
	obj := &ast.ObjectCons{}
	if p.peek().Kind == lexer.RBrace {
		p.next()
		return obj, nil
	}
	for {
		e, err := p.parseObjEntry()
		if err != nil {
			return nil, err
		}
		obj.Entries = append(obj.Entries, e)
		if p.peek().Kind != lexer.Comma {
			break
		}
		p.next()
	}
	return obj, p.expect(lexer.RBrace, "}")
}

func (p *parser) parseObjEntry() (ast.ObjEntry, error) {
	tok := p.peek()
	var key ast.Node
	switch tok.Kind {
	case lexer.Variable:
		p.next()
		name := tok.Text[1:]
		return ast.ObjEntry{Key: &ast.Literal{Value: name}, Val: &ast.Var{Name: name}}, nil
	case lexer.Loc:
		p.next()
		k := &ast.Str{Parts: []ast.Node{&ast.Literal{Value: "__loc__"}}}
		return ast.ObjEntry{Key: k, Val: p.locAt(tok)}, nil
	case lexer.Ident, lexer.Keyword:
		p.next()
		key = &ast.Literal{Value: tok.Text}
		if p.peek().Kind != lexer.Colon {
			return ast.ObjEntry{Key: key, Val: &ast.Field{Name: tok.Text}}, nil
		}
	case lexer.StrStart, lexer.Format:
		format := ""
		if tok.Kind == lexer.Format {
			p.next()
			format = tok.Text
		}
		s, err := p.parseString(format)
		if err != nil {
			return ast.ObjEntry{}, err
		}
		key = s
		if p.peek().Kind != lexer.Colon {
			return ast.ObjEntry{Key: key, Val: &ast.Index{Key: key}}, nil
		}
	case lexer.LParen:
		p.next()
		k, err := p.parseExpr(0)
		if err != nil {
			return ast.ObjEntry{}, err
		}
		if err := p.expect(lexer.RParen, ")"); err != nil {
			return ast.ObjEntry{}, err
		}
		key = k
	default:
		return ast.ObjEntry{}, p.unexpected(tok)
	}
	if err := p.expect(lexer.Colon, ":"); err != nil {
		return ast.ObjEntry{}, err
	}
	val, err := p.parseObjVal()
	return ast.ObjEntry{Key: key, Val: val}, err
}

// parseObjVal reads an object value, which stops at the comma that
// separates entries but keeps pipes.
func (p *parser) parseObjVal() (ast.Node, error) {
	floor := 0
	if r, ok := p.rowOfKind(Comma); ok {
		floor = r.Prec + 1
	}
	v, err := p.parseExpr(floor)
	if err != nil {
		return nil, err
	}
	if p.peek().Kind != lexer.Pipe {
		return v, nil
	}
	p.next()
	rest, err := p.parseObjVal()
	if err != nil {
		return nil, err
	}
	return &ast.Pipe{L: v, R: rest}, nil
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

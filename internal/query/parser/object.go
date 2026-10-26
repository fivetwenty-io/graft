package parser

import (
	"github.com/fivetwenty-io/graft/internal/query/ast"
	"github.com/fivetwenty-io/graft/internal/query/lexer"
)

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
		if p.peek().Kind != lexer.Colon {
			return ast.ObjEntry{Key: &ast.Literal{Value: name}, Val: &ast.Var{Name: name}}, nil
		}
		key = &ast.Var{Name: name}
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
		start := p.i
		s, err := p.parseFormattedString()
		if err != nil {
			return ast.ObjEntry{}, err
		}
		key = s
		if p.peek().Kind != lexer.Colon {
			// The index gets a tree of its own, so a pass that rewrites one
			// of the two cannot change the other.
			end := p.i
			p.i = start
			again, err := p.parseFormattedString()
			p.i = end
			return ast.ObjEntry{Key: key, Val: &ast.Index{Key: again}}, err
		}
	default:
		k, err := p.parseOtherKey(tok)
		if err != nil {
			return ast.ObjEntry{}, err
		}
		key = k
	}
	if err := p.expect(lexer.Colon, ":"); err != nil {
		return ast.ObjEntry{}, err
	}
	val, err := p.parseObjVal()
	return ast.ObjEntry{Key: key, Val: val}, err
}

// parseOtherKey reads a key that is a parenthesized expression, or under
// ExprKeys any expression at all.
func (p *parser) parseOtherKey(tok lexer.Token) (ast.Node, error) {
	if tok.Kind == lexer.LParen {
		p.next()
		k, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		return k, p.expect(lexer.RParen, ")")
	}
	if !p.g.ExprKeys || tok.Kind == lexer.RBrace || tok.Kind == lexer.EOF {
		return nil, p.unexpected(tok)
	}
	return p.parseObjVal()
}

// parseObjVal reads an object value, which stops at the comma that
// separates entries but keeps pipes.
func (p *parser) parseObjVal() (ast.Node, error) {
	floor := 0
	if r, ok := p.rowOfKind(Comma); ok {
		floor = r.Prec + 1
	}
	leave, err := p.enter()
	defer leave()
	if err != nil {
		return nil, err
	}
	v, err := p.parseExprIn(floor, exprContext{})
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

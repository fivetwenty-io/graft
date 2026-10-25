package parser

import (
	"github.com/fivetwenty-io/graft/internal/query/ast"
	"github.com/fivetwenty-io/graft/internal/query/lexer"
)

// parsePatterns reads "pattern ?// pattern ...".
func (p *parser) parsePatterns() ([]ast.Pattern, error) {
	var pats []ast.Pattern
	for {
		pat, err := p.parsePattern()
		if err != nil {
			return nil, err
		}
		pats = append(pats, pat)
		if p.peek().Kind != lexer.AltDestructure {
			return pats, nil
		}
		p.next()
	}
}

func (p *parser) parsePattern() (ast.Pattern, error) {
	leave, err := p.enter()
	defer leave()
	if err != nil {
		return ast.Pattern{}, err
	}
	tok := p.peek()
	switch tok.Kind {
	case lexer.Variable:
		p.next()
		return ast.Pattern{Kind: ast.PatVar, Name: tok.Text[1:]}, nil
	case lexer.LBracket:
		p.next()
		return p.parseArrayPattern()
	case lexer.LBrace:
		p.next()
		return p.parseObjectPattern()
	default:
		return ast.Pattern{}, p.unexpected(tok)
	}
}

func (p *parser) parseArrayPattern() (ast.Pattern, error) {
	pat := ast.Pattern{Kind: ast.PatArray}
	for {
		elem, err := p.parsePattern()
		if err != nil {
			return pat, err
		}
		pat.Elems = append(pat.Elems, elem)
		if p.peek().Kind != lexer.Comma {
			break
		}
		p.next()
	}
	return pat, p.expect(lexer.RBracket, "]")
}

func (p *parser) parseObjectPattern() (ast.Pattern, error) {
	pat := ast.Pattern{Kind: ast.PatObject}
	for {
		entry, err := p.parsePatEntry()
		if err != nil {
			return pat, err
		}
		pat.Entries = append(pat.Entries, entry)
		if p.peek().Kind != lexer.Comma {
			break
		}
		p.next()
	}
	return pat, p.expect(lexer.RBrace, "}")
}

func (p *parser) parsePatEntry() (ast.PatEntry, error) {
	tok := p.peek()
	var entry ast.PatEntry
	switch tok.Kind {
	case lexer.Variable:
		p.next()
		entry.KeyVar = tok.Text[1:]
		if p.peek().Kind != lexer.Colon {
			return entry, nil
		}
	case lexer.Ident, lexer.Keyword:
		p.next()
		entry.Key = &ast.Literal{Value: tok.Text}
	case lexer.StrStart:
		key, err := p.parseString("")
		if err != nil {
			return entry, err
		}
		entry.Key = key
	case lexer.LParen:
		p.next()
		key, err := p.parseExpr()
		if err != nil {
			return entry, err
		}
		if err := p.expect(lexer.RParen, ")"); err != nil {
			return entry, err
		}
		entry.Key = key
	default:
		return entry, p.unexpected(tok)
	}
	if err := p.expect(lexer.Colon, ":"); err != nil {
		return entry, err
	}
	val, err := p.parsePattern()
	if err != nil {
		return entry, err
	}
	entry.Val = &val
	return entry, nil
}

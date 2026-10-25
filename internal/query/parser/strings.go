package parser

import (
	"github.com/fivetwenty-io/graft/internal/query/ast"
	"github.com/fivetwenty-io/graft/internal/query/lexer"
)

// parseFormattedString reads a string that may start with an "@name"
// format.
func (p *parser) parseFormattedString() (*ast.Str, error) {
	format := ""
	if tok := p.peek(); tok.Kind == lexer.Format {
		p.next()
		format = tok.Text
	}
	return p.parseString(format)
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
			e, err := p.parseExpr()
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

// Package yq is the yq dialect over the shared query engine: yq's
// grammar and diagnostics, its truthiness, indexing, recursion, and
// assignment, the cursor operators parent, key, and path, and Eval and
// EvalAll, which run an expression over one document or over every
// document at once.
package yq

import (
	"errors"
	"strings"

	"github.com/fivetwenty-io/graft/internal/query/ast"
	"github.com/fivetwenty-io/graft/internal/query/lexer"
	"github.com/fivetwenty-io/graft/internal/query/parser"
)

// ParseError is an expression yq cannot parse. Its message is the one yq
// gives, which names no position. Pos is the byte offset the parser
// stopped at.
type ParseError struct {
	Msg string
	Pos int
}

// Error returns the message.
func (e *ParseError) Error() string { return e.Msg }

// Grammar returns the grammar yq expressions parse under.
func Grammar() parser.Grammar {
	g := parser.YQ()
	g.MetaAssign = MetaAssignNames()
	return g
}

// Parse parses a yq expression. A blank expression is the identity, as it
// is in yq. A name yq's lexer lacks, such as jq's empty, fails first, as
// it does in yq, which lexes the whole expression before it parses. A
// failure is a *ParseError worded the way yq words it where yq's wording
// is known, and the parser's own message otherwise.
func Parse(expr string) (ast.Node, error) {
	if strings.TrimSpace(expr) == "" {
		return &ast.Identity{}, nil
	}
	if toks, err := lexer.Tokenize(expr, Grammar().Tokens); err == nil {
		if msg, at, refused := unlexable(expr, toks); refused {
			return nil, &ParseError{Msg: msg, Pos: at}
		}
		if msg, at, missing := leftMissing(toks); missing {
			return nil, &ParseError{Msg: msg, Pos: at}
		}
	}
	n, err := parser.Parse(expr, Grammar())
	if err == nil {
		return rewrite(expr, n)
	}
	var pe *parser.Error
	if !errors.As(err, &pe) {
		return nil, err
	}
	return nil, &ParseError{Msg: diagnose(expr, pe), Pos: pe.Pos}
}

// diagnose words a parse failure as yq does. Unbalanced brackets come
// first, then an infix operator with an operand missing, and anything
// else keeps the parser's message.
func diagnose(expr string, pe *parser.Error) string {
	toks, err := lexer.Tokenize(expr, Grammar().Tokens)
	if err != nil {
		return pe.Msg
	}
	if msg, ok := unbalanced(toks); ok {
		return msg
	}
	for i, tok := range toks {
		if tok.Pos != pe.Pos {
			continue
		}
		switch {
		case i == 0 && infix(tok):
			return operandMissing(tok)
		case i > 0 && infix(toks[i-1]) && (tok.Kind == lexer.EOF || infix(tok) || closer(tok)):
			return operandMissing(toks[i-1])
		}
		break
	}
	return pe.Msg
}

// unbalanced reports yq's message for the first bracket that does not
// pair up.
func unbalanced(toks []lexer.Token) (string, bool) {
	var open []lexer.TokenKind
	for _, tok := range toks {
		switch tok.Kind {
		case lexer.LParen, lexer.LBracket, lexer.LBrace:
			open = append(open, tok.Kind)
		case lexer.RParen, lexer.RBracket, lexer.RBrace:
			if len(open) == 0 {
				return "bad expression, got close brackets without matching opening bracket", true
			}
			open = open[:len(open)-1]
		default:
		}
	}
	if len(open) == 0 {
		return "", false
	}
	switch open[len(open)-1] {
	case lexer.LParen:
		return "bad expression - probably missing close bracket on (", true
	case lexer.LBracket:
		return "bad expression, could not find matching `]`", true
	default:
		return "bad expression, could not find matching `}`", true
	}
}

// infix reports whether tok is one of yq's infix operators.
func infix(tok lexer.Token) bool {
	if tok.Kind < lexer.Dot && tok.Kind != lexer.Keyword {
		return false
	}
	spelling := tok.Text
	if tok.Kind == lexer.Star || tok.Kind == lexer.MulAssign {
		spelling = strings.TrimRight(spelling, Grammar().Tokens.MergeFlags)
	}
	for _, r := range Grammar().Ops {
		if r.Spelling == spelling {
			return true
		}
	}
	return false
}

// leftMissing reports yq's message for an operator such as "style ="
// with nothing on its left. yq's operator takes the operand of the
// operator before it, which then has one too few, so the message names
// that operator, or the name itself when nothing comes before it.
func leftMissing(toks []lexer.Token) (string, int, bool) {
	for i, tok := range toks {
		if tok.Kind != lexer.Ident || i+1 >= len(toks) {
			continue
		}
		if _, ok := metaNames[tok.Text]; !ok {
			continue
		}
		if next := toks[i+1].Kind; next != lexer.Assign && next != lexer.UpdateAssign {
			continue
		}
		switch {
		case i == 0 || opener(toks[i-1]):
			return operandMissing(tok), tok.Pos, true
		case infix(toks[i-1]):
			return operandMissing(toks[i-1]), toks[i-1].Pos, true
		default:
		}
	}
	return "", 0, false
}

// opener reports whether tok opens a group, or is a keyword that is not
// an operator, either of which an operand follows.
func opener(tok lexer.Token) bool {
	switch tok.Kind {
	case lexer.LParen, lexer.LBracket, lexer.LBrace:
		return true
	case lexer.Keyword:
		return !infix(tok)
	default:
	}
	return false
}

func closer(tok lexer.Token) bool {
	return tok.Kind == lexer.RParen || tok.Kind == lexer.RBracket || tok.Kind == lexer.RBrace
}

func operandMissing(op lexer.Token) string {
	return "'" + op.Text + "' expects 2 args but there is 1"
}

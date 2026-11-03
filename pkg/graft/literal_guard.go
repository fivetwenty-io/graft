package graft

import (
	"strings"

	"github.com/fivetwenty-io/graft/internal/literal"
	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// guardLiteralResponse keeps text that an operator derived from a literal
// from being run as an operator later. Most operators read their
// arguments as plain data, so a (( join )) or a || fallback over a
// literal returns plain strings, and a "(( ... ))" string among them
// would be evaluated by a later phase or a later Evaluate. When op read
// from a literal, every such string in its result is wrapped again.
//
// (( defer )) is left alone: it writes the text of its own arguments,
// which come from the template, not the value they point at.
func (ev *Evaluator) guardLiteralResponse(op *Opcall, resp *Response) {
	if !ev.holdsLiteral || resp == nil || op == nil || op.name == "defer" {
		return
	}
	if resp.Type != Replace && resp.Type != Inject {
		return
	}
	if !ev.argsReadLiteral(op.args) {
		return
	}
	if guarded, changed := literal.GuardText(resp.Value); changed {
		resp.Value = guarded
	}
}

// argsReadLiteral reports whether any of args can read a value from
// inside a literal.
func (ev *Evaluator) argsReadLiteral(args []*Expr) bool {
	for _, arg := range args {
		if ev.exprReadsLiteral(arg) {
			return true
		}
	}
	return false
}

// exprReadsLiteral reports whether e can read a value from inside a
// literal. A reference counts when it resolves to a literal, into one,
// or to a value that holds one. A string that reads as such a path
// counts too, since some operators take a path as a string. A nested
// call counts when its own arguments do, and also when any of them is
// computed, because the call may then use that result as a path.
func (ev *Evaluator) exprReadsLiteral(e *Expr) bool {
	if e == nil {
		return false
	}
	switch e.Type {
	case Reference:
		if e.Reference == nil {
			return false
		}
		if strings.Contains(e.Reference.String(), "$") {
			// The path holds an environment variable that is expanded
			// when the operator runs, so it can point anywhere.
			return true
		}
		return ev.pathReadsLiteral(e.Reference)
	case Literal:
		s, ok := e.Literal.(string)
		if !ok {
			return false
		}
		c, err := tree.ParseCursor(s)
		if err != nil {
			return false
		}
		return ev.pathReadsLiteral(c)
	case OperatorCall:
		for _, arg := range e.Args() {
			if arg == nil {
				continue
			}
			if arg.Type != Literal && arg.Type != Reference {
				return true
			}
			if ev.exprReadsLiteral(arg) {
				return true
			}
		}
		return false
	default:
		return ev.exprReadsLiteral(e.Left) || ev.exprReadsLiteral(e.Right)
	}
}

// pathReadsLiteral reports whether c resolves to a literal, into one, or
// to a value that holds one.
func (ev *Evaluator) pathReadsLiteral(c *tree.Cursor) bool {
	v, err := c.ResolveLiteral(ev.Tree)
	if err != nil {
		return false
	}
	return literal.Contains(v)
}

// minWithheldText is the shortest literal text that withholds an error
// message. A shorter one, such as "a" or "80", turns up in too many
// ordinary messages to tell anything about the literal.
const minWithheldText = 3

// withheldError stands in for an error whose message quotes text from a
// literal. Unwrap still returns the original, so errors.Is and errors.As
// keep working, but the message itself names no content.
type withheldError struct {
	cause error
}

func (e *withheldError) Error() string {
	return "the operator failed on a value from a literal; the message is withheld because it quotes that value"
}

func (e *withheldError) Unwrap() error {
	return e.cause
}

// scrubLiteralError returns err, or a withheldError in its place when
// its message holds text from a literal in the tree. The path of a
// PathError is kept, so the message still says where the error is.
func (ev *Evaluator) scrubLiteralError(err error) error {
	if err == nil || !ev.holdsLiteral {
		return err
	}
	msg := err.Error()
	quoted := false
	for _, text := range literal.Texts(ev.Tree) {
		if len(text) >= minWithheldText && strings.Contains(msg, text) {
			quoted = true
			break
		}
	}
	if !quoted {
		return err
	}
	if pathErr, ok := err.(*PathError); ok { //nolint:errorlint // only the outer PathError's path is kept
		return &PathError{Path: pathErr.Path, Cause: &withheldError{cause: pathErr.Cause}}
	}
	return &withheldError{cause: err}
}

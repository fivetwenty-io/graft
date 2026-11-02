package yaml

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	goyaml "github.com/goccy/go-yaml"

	"github.com/fivetwenty-io/graft/internal/yamlcodec"
	"github.com/fivetwenty-io/graft/internal/yamlnode"
)

// DecodeError reports why Unmarshal failed, and its text never quotes the
// input, so it is safe to log when the input holds credentials.
//
// Only the fields are stable. The text Error returns may change between
// releases.
type DecodeError struct {
	// Line is the 1-based line the failure points at, or 0 when it has none.
	Line int
	// Column is the 1-based column the failure points at, or 0 when it has none.
	Column int
	// Kind says which class of failure this is.
	Kind ErrorKind
	// Msg describes the failure without quoting any scalar from the input.
	Msg string
}

// Error formats the failure as "yaml: line N: Msg", or "yaml: Msg" when
// Line is 0.
func (e *DecodeError) Error() string {
	if e.Line > 0 {
		return "yaml: line " + strconv.Itoa(e.Line) + ": " + e.Msg
	}
	return "yaml: " + e.Msg
}

// ErrorKind classifies a DecodeError.
type ErrorKind uint8

const (
	// ErrorKindSyntax means the input is not well-formed YAML.
	ErrorKindSyntax ErrorKind = iota + 1
	// ErrorKindType means a value does not fit the Go type it decodes
	// into, or the target type is one Unmarshal does not support.
	ErrorKindType
	// ErrorKindLimit means the input exceeds a safety limit, such as the
	// nesting depth or the node count once aliases are expanded.
	ErrorKindLimit
)

// typeError returns an ErrorKindType DecodeError with no position, for a
// failure that concerns the target rather than a place in the input.
func typeError(format string, args ...any) *DecodeError {
	return &DecodeError{Kind: ErrorKindType, Msg: fmt.Sprintf(format, args...)}
}

// translate turns an error from graft's codec or from goccy into a
// DecodeError. It keeps only a message and a position, since the errors
// it reads can print the source lines around the failure, and those may
// hold credentials. It never wraps err, for the same reason.
func translate(err error) *DecodeError {
	if err == nil {
		return nil
	}
	var de *DecodeError
	if errors.As(err, &de) {
		return de
	}

	var tooDeep *yamlcodec.MaxRecursionError
	if errors.As(err, &tooDeep) || errors.Is(err, goyaml.ErrExceededMaxDepth) {
		return limitError(err, tooDeep)
	}

	// goccy's own errors print the offending source lines from Error(),
	// so only GetMessage and the token's position are read.
	var ge goyaml.Error
	if errors.As(err, &ge) {
		out := &DecodeError{Kind: ErrorKindSyntax, Msg: scrub(ge.GetMessage())}
		var overflow *goyaml.OverflowError
		switch {
		case errors.As(err, &overflow):
			// Its message quotes the number that overflowed.
			out.Kind = ErrorKindType
			out.Msg = fmt.Sprintf("cannot unmarshal a number into Go value of type %s (overflow)", overflow.DstType)
		case isGoccyTypeError(err):
			out.Kind = ErrorKindType
		}
		if tk := ge.GetToken(); tk != nil && tk.Position != nil {
			out.Line, out.Column = tk.Position.Line, tk.Position.Column
		}
		return out
	}

	var pe *yamlnode.ParseError
	if errors.As(err, &pe) {
		return &DecodeError{Kind: ErrorKindSyntax, Line: pe.Line, Msg: scrub(pe.Message)}
	}

	// The rest are graft's own messages, such as the self-containing
	// anchor check's, which name an anchor and quote no scalar.
	return &DecodeError{Kind: ErrorKindSyntax, Msg: scrub(err.Error())}
}

// limitError builds the ErrorKindLimit error for err, a depth or
// expansion failure, with the line and message of the depth check when
// it has them. tooDeep is the codec's recursion error inside err, or nil.
func limitError(err error, tooDeep *yamlcodec.MaxRecursionError) *DecodeError {
	out := &DecodeError{Kind: ErrorKindLimit, Msg: yamlcodec.MaxRecursionMessage}
	var pe *yamlnode.ParseError
	switch {
	case errors.As(err, &pe):
		out.Line, out.Msg = pe.Line, scrub(pe.Message)
	case errors.Is(err, goyaml.ErrExceededMaxDepth):
		out.Msg = goyaml.ErrExceededMaxDepth.Error()
	case tooDeep != nil && tooDeep.Unwrap() != nil:
		// The expansion cap says how many nodes it allows.
		out.Msg = scrub(tooDeep.Unwrap().Error())
	}
	return out
}

// isGoccyTypeError reports whether err is one of goccy's errors for a
// value that does not fit its Go type.
func isGoccyTypeError(err error) bool {
	var te *goyaml.TypeError
	var ue *goyaml.UnexpectedNodeTypeError
	return errors.As(err, &te) || errors.As(err, &ue)
}

// scrub removes what a codec message may quote from the input. It keeps
// only the first line, since goccy's formatted errors print the source
// below it, and it drops every double-quoted span and every single-quoted
// span that is not one punctuation character, since goccy quotes keys,
// values, aliases, and escape characters that way. A single punctuation
// character in quotes, as in "',' or ']' must be specified", is the
// codec's own syntax and stays.
func scrub(msg string) string {
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	var b strings.Builder
	for i := 0; i < len(msg); i++ {
		c := msg[i]
		if (c == '"' || c == '\'') && quoteOpens(msg, i) {
			if end, ok := quotedEnd(msg, i); ok {
				if c == '\'' && isPunctuationSpan(msg[i+1:end]) {
					b.WriteString(msg[i : end+1])
				}
				i = end
				continue
			}
		}
		b.WriteByte(c)
	}
	// Dropping a span leaves the spaces on either side of it.
	return strings.Join(strings.Fields(b.String()), " ")
}

// quoteOpens reports whether the quote at msg[i] can open a span, which
// it can at the start or after a character that is not a letter or
// digit, so the apostrophe in "can't" opens nothing.
func quoteOpens(msg string, i int) bool {
	if i == 0 {
		return true
	}
	p := msg[i-1]
	return (p < 'a' || p > 'z') && (p < 'A' || p > 'Z') && (p < '0' || p > '9')
}

// quotedEnd returns the index of the quote that closes the span opened
// at msg[i], skipping backslash escapes as %q writes them.
func quotedEnd(msg string, i int) (int, bool) {
	q := msg[i]
	for j := i + 1; j < len(msg); j++ {
		switch msg[j] {
		case '\\':
			j++
		case q:
			return j, true
		}
	}
	return 0, false
}

// isPunctuationSpan reports whether s, the inside of a single-quoted
// span, is one ASCII punctuation character.
func isPunctuationSpan(s string) bool {
	return len(s) == 1 && strings.IndexByte("!\"#$%&()*+,-./:;<=>?@[]^_`{|}~", s[0]) >= 0
}

// Package parser turns query source into an ast tree with a Pratt parser.
//
// A Grammar supplied by the dialect decides the token set, the binary
// operator table, and which keyword forms are available, so the same
// parser serves jq and yq.
package parser

import "github.com/fivetwenty-io/graft/internal/query/lexer"

// Assoc is how a row's operator groups with another of the same
// precedence.
type Assoc int

const (
	// Left groups "a op b op c" as "(a op b) op c".
	Left Assoc = iota
	// Right groups "a op b op c" as "a op (b op c)".
	Right
	// NonAssoc rejects "a op b op c".
	NonAssoc
)

// RowKind says which tree node a row builds.
type RowKind int

const (
	// Binary builds ast.Binary, which covers arithmetic, comparison, and
	// the boolean keywords.
	Binary RowKind = iota
	// Pipe builds ast.Pipe.
	Pipe
	// Comma builds ast.Comma.
	Comma
	// Alt builds ast.Alt.
	Alt
	// Assign builds ast.Assign.
	Assign
)

// Row is one infix operator. A higher Prec binds tighter.
type Row struct {
	Spelling string
	Prec     int
	Assoc    Assoc
	Kind     RowKind
}

// OperatorTable lists a dialect's infix operators in any order.
type OperatorTable []Row

// KeywordForms switches the keyword-introduced forms a dialect accepts.
// A form that is off leaves its keyword as an unexpected token.
type KeywordForms struct {
	Def     bool
	If      bool
	Try     bool
	Reduce  bool
	Foreach bool
	Label   bool
	// Bind enables "term as patterns | body".
	Bind bool
	// IReduce enables yq's "term as $x ireduce (init; update)".
	IReduce bool
	// VarBind limits "as" to one plain variable, as yq has no
	// destructuring.
	VarBind bool
	// Modules enables module, import, and include directives.
	Modules bool
}

// Grammar is everything a dialect tells the parser.
type Grammar struct {
	Tokens   lexer.TokenSet
	Ops      OperatorTable
	Keywords KeywordForms
}

// JQ returns the jq 1.8 grammar. The rows follow parser.y's declarations
// from the loosest binding to the tightest.
func JQ() Grammar {
	return Grammar{
		Tokens: lexer.JQ(),
		Ops: OperatorTable{
			{"|", 1, Right, Pipe},
			{",", 2, Left, Comma},
			{"//", 3, Right, Alt},
			{"=", 4, NonAssoc, Assign},
			{"|=", 4, NonAssoc, Assign},
			{"+=", 4, NonAssoc, Assign},
			{"-=", 4, NonAssoc, Assign},
			{"*=", 4, NonAssoc, Assign},
			{"/=", 4, NonAssoc, Assign},
			{"%=", 4, NonAssoc, Assign},
			{"//=", 4, NonAssoc, Assign},
			{"or", 5, Left, Binary},
			{"and", 6, Left, Binary},
			{"==", 7, NonAssoc, Binary},
			{"!=", 7, NonAssoc, Binary},
			{"<", 7, NonAssoc, Binary},
			{"<=", 7, NonAssoc, Binary},
			{">", 7, NonAssoc, Binary},
			{">=", 7, NonAssoc, Binary},
			{"+", 8, Left, Binary},
			{"-", 8, Left, Binary},
			{"*", 9, Left, Binary},
			{"/", 9, Left, Binary},
			{"%", 9, Left, Binary},
		},
		Keywords: KeywordForms{
			Def: true, If: true, Try: true, Reduce: true, Foreach: true,
			Label: true, Bind: true, Modules: true,
		},
	}
}

// YQ returns the yq 4 grammar. The precedences are yq's own, and every
// row is right-associative because yq's parser folds operators of equal
// precedence from the right, so "1 - 2 - 3" is "1 - (2 - 3)". The comma
// binds looser than the pipe, the boolean keywords share one level, and
// the arithmetic operators and "//" share the level above the
// assignments and comparisons. "*" and "*=" take yq's merge flags.
func YQ() Grammar {
	return Grammar{
		Tokens: lexer.YQ(),
		Ops: OperatorTable{
			{",", 10, Right, Comma},
			{"or", 20, Right, Binary},
			{"and", 20, Right, Binary},
			{"|", 30, Right, Pipe},
			{"=", 40, Right, Assign},
			{"|=", 40, Right, Assign},
			{"+=", 40, Right, Assign},
			{"-=", 40, Right, Assign},
			{"==", 40, Right, Binary},
			{"!=", 40, Right, Binary},
			{"<", 40, Right, Binary},
			{"<=", 40, Right, Binary},
			{">", 40, Right, Binary},
			{">=", 40, Right, Binary},
			{"*=", 42, Right, Assign},
			{"+", 42, Right, Binary},
			{"-", 42, Right, Binary},
			{"*", 42, Right, Binary},
			{"/", 42, Right, Binary},
			{"%", 42, Right, Binary},
			{"//", 42, Right, Alt},
		},
		Keywords: KeywordForms{If: true, Bind: true, IReduce: true, VarBind: true},
	}
}

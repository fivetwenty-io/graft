// Package ast defines the syntax tree that the query parser produces and
// the evaluator walks. One tree serves both the jq and yq dialects.
package ast

// Node is any expression or directive in a query program. Every concrete
// node type is used through a pointer.
type Node interface {
	node()
}

// Number is the source spelling of a numeric literal. The decimal package
// that gives it a value is applied when the tree is compiled, so parsing
// never rounds.
type Number string

// Identity is ".".
type Identity struct{}

// RecurseDefault is "..".
type RecurseDefault struct{}

// Field is ".name". Target is nil for a field of the input.
type Field struct {
	Target Node
	Name   string
	Opt    bool
}

// Index is "Target[Key]". Target is nil for an index of the input.
type Index struct {
	Target Node
	Key    Node
	Opt    bool
}

// Slice is "Target[From:To]". Either bound may be nil, and so may Target.
type Slice struct {
	Target Node
	From   Node
	To     Node
	Opt    bool
}

// Iterate is "Target[]". Target is nil for an iteration of the input.
type Iterate struct {
	Target Node
	Opt    bool
}

// Literal is null, a boolean, or a string (Go nil, bool, or string), or a
// Number.
type Literal struct {
	Value any
}

// Str is a string literal with interpolation. A part that is a Literal
// string is text, and any other part is an interpolated expression.
// Format is the "@name" prefix, or empty.
type Str struct {
	Parts  []Node
	Format string
}

// FormatRef is a bare "@name" applied to the input.
type FormatRef struct {
	Name string
}

// ArrayCons is "[Body]". Body is nil for "[]".
type ArrayCons struct {
	Body Node
}

// ObjEntry is one key and value of an object construction.
type ObjEntry struct {
	Key Node
	Val Node
}

// ObjectCons is "{...}".
type ObjectCons struct {
	Entries []ObjEntry
}

// Pipe is "L | R".
type Pipe struct {
	L, R Node
}

// Comma is "L, R".
type Comma struct {
	L, R Node
}

// Binary is an arithmetic, comparison, or boolean operator. Op is its
// spelling.
type Binary struct {
	Op   string
	L, R Node
}

// Unary is a prefix operator. Op is its spelling.
type Unary struct {
	Op string
	X  Node
}

// Alt is "L // R".
type Alt struct {
	L, R Node
}

// Assign is an assignment operator. Op is its spelling.
type Assign struct {
	Op   string
	L, R Node
}

// Elif is one "elif Cond then Then" clause.
type Elif struct {
	Cond Node
	Then Node
}

// If is a conditional. Else is nil when the source has no else branch.
type If struct {
	Cond  Node
	Then  Node
	Elifs []Elif
	Else  Node
}

// Try is "try Body catch Catch". Catch is nil when absent.
type Try struct {
	Body  Node
	Catch Node
}

// Reduce is "reduce Src as Pats (Init; Update)".
type Reduce struct {
	Src    Node
	Pats   []Pattern
	Init   Node
	Update Node
}

// Foreach is "foreach Src as Pats (Init; Update; Extract)". Extract is nil
// when absent.
type Foreach struct {
	Src     Node
	Pats    []Pattern
	Init    Node
	Update  Node
	Extract Node
}

// FuncDef is "def Name(Params): Body; Rest". A parameter that starts with
// "$" is a value parameter. Rest is nil when the definition ends a library
// file.
type FuncDef struct {
	Name   string
	Params []string
	Body   Node
	Rest   Node
}

// Call is a function call or a bare function name.
type Call struct {
	Name string
	Args []Node
}

// Var is "$Name". Name omits the dollar sign.
type Var struct {
	Name string
}

// Bind is "Src as Pats | Body". More than one pattern means "?//"
// alternatives.
type Bind struct {
	Src  Node
	Pats []Pattern
	Body Node
}

// Label is "label $Name | Body".
type Label struct {
	Name string
	Body Node
}

// Break is "break $Name".
type Break struct {
	Name string
}

// Optional is a postfix "?" on a term that has no flag of its own.
type Optional struct {
	X Node
}

// Loc is "$__loc__", resolved by the parser to the file and line of the
// token.
type Loc struct {
	File string
	Line int
}

// Module is "module Meta; Rest".
type Module struct {
	Meta Node
	Rest Node
}

// Import is `import "Path" as Alias Meta; Rest`. Data marks the "as $name"
// form. Meta and Rest may be nil.
type Import struct {
	Path  string
	Alias string
	Data  bool
	Meta  Node
	Rest  Node
}

// Include is `include "Path" Meta; Rest`. Meta and Rest may be nil.
type Include struct {
	Path string
	Meta Node
	Rest Node
}

// PatKind says which form a Pattern takes.
type PatKind int

const (
	// PatVar is "$name".
	PatVar PatKind = iota
	// PatArray is "[p, ...]".
	PatArray
	// PatObject is "{...}".
	PatObject
)

// PatEntry is one entry of an object pattern. KeyVar names the "$name"
// form, which binds the variable and also uses the name as the key. Key is
// the key expression otherwise. Val is the nested pattern, which is nil
// only for the bare "$name" form.
type PatEntry struct {
	KeyVar string
	Key    Node
	Val    *Pattern
}

// Pattern is a destructuring pattern.
type Pattern struct {
	Kind    PatKind
	Name    string
	Elems   []Pattern
	Entries []PatEntry
}

func (*Identity) node()       {}
func (*RecurseDefault) node() {}
func (*Field) node()          {}
func (*Index) node()          {}
func (*Slice) node()          {}
func (*Iterate) node()        {}
func (*Literal) node()        {}
func (*Str) node()            {}
func (*FormatRef) node()      {}
func (*ArrayCons) node()      {}
func (*ObjectCons) node()     {}
func (*Pipe) node()           {}
func (*Comma) node()          {}
func (*Binary) node()         {}
func (*Unary) node()          {}
func (*Alt) node()            {}
func (*Assign) node()         {}
func (*If) node()             {}
func (*Try) node()            {}
func (*Reduce) node()         {}
func (*Foreach) node()        {}
func (*FuncDef) node()        {}
func (*Call) node()           {}
func (*Var) node()            {}
func (*Bind) node()           {}
func (*Label) node()          {}
func (*Break) node()          {}
func (*Optional) node()       {}
func (*Loc) node()            {}
func (*Module) node()         {}
func (*Import) node()         {}
func (*Include) node()        {}

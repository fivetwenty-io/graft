package ast

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// Print renders a tree in a fully parenthesized form that shows the shape
// the parser chose. Tests use it to check precedence and associativity.
func Print(n Node) string {
	var sb strings.Builder
	printNode(&sb, n)
	return sb.String()
}

// printNode tries each family of node types in turn. Each family printer
// reports whether n was one of its types.
// child renders an operand of an operator, a postfix form, or a try. The
// forms that swallow what follows them are parenthesized there, so the
// output has one reading.
func child(n Node) string {
	switch n.(type) {
	case *Bind, *FuncDef, *Label, *Try:
		return "(" + Print(n) + ")"
	}
	return Print(n)
}

func printNode(sb *strings.Builder, n Node) {
	_ = printAtom(sb, n) || printAccess(sb, n) || printOperator(sb, n) ||
		printForm(sb, n) || printDirective(sb, n)
}

func printAtom(sb *strings.Builder, n Node) bool {
	switch n := n.(type) {
	case *Identity:
		sb.WriteString(".")
	case *RecurseDefault:
		sb.WriteString("..")
		if n.Keys {
			sb.WriteString(".")
		}
	case *Literal:
		printLiteral(sb, n.Value)
	case *Str:
		printStr(sb, n)
	case *FormatRef:
		sb.WriteString(n.Name)
	case *ArrayCons:
		sb.WriteString("[" + Print(n.Body) + "]")
	case *ObjectCons:
		printObject(sb, n)
	case *Call:
		sb.WriteString(n.Name)
		if len(n.Args) > 0 {
			sb.WriteString("(" + joinNodes(n.Args, "; ") + ")")
		}
	case *Var:
		sb.WriteString("$" + n.Name)
	case *Loc:
		sb.WriteString("$__loc__")
	default:
		return false
	}
	return true
}

func printAccess(sb *strings.Builder, n Node) bool {
	switch n := n.(type) {
	case *Field:
		printField(sb, n)
	case *Index:
		printSuffix(sb, n.Target, "["+Print(n.Key)+"]", n.Opt, true)
	case *Slice:
		printSuffix(sb, n.Target, "["+Print(n.From)+":"+Print(n.To)+"]", n.Opt, true)
	case *Iterate:
		printSuffix(sb, n.Target, "[]", n.Opt, true)
	case *Optional:
		sb.WriteString("(" + child(n.X) + ")?")
	default:
		return false
	}
	return true
}

func printOperator(sb *strings.Builder, n Node) bool {
	switch n := n.(type) {
	case *Pipe:
		printInfix(sb, "|", n.L, n.R)
	case *Comma:
		printInfix(sb, ",", n.L, n.R)
	case *Binary:
		printInfix(sb, n.Op, n.L, n.R)
	case *Alt:
		printInfix(sb, "//", n.L, n.R)
	case *Assign:
		printInfix(sb, n.Op, n.L, n.R)
	case *Unary:
		sb.WriteString("(" + n.Op + child(n.X) + ")")
	default:
		return false
	}
	return true
}

func printForm(sb *strings.Builder, n Node) bool {
	switch n := n.(type) {
	case *If:
		printIf(sb, n)
	case *Try:
		sb.WriteString("try " + child(n.Body))
		if n.Catch != nil {
			sb.WriteString(" catch " + child(n.Catch))
		}
	case *Reduce:
		sb.WriteString("reduce " + child(n.Src) + " as " + printPatterns(n.Pats) +
			" (" + Print(n.Init) + "; " + Print(n.Update) + ")")
	case *Foreach:
		printForeach(sb, n)
	case *FuncDef:
		printFuncDef(sb, n)
	case *Bind:
		word := " as "
		if n.Ref {
			word = " ref "
		}
		sb.WriteString(child(n.Src) + word + printPatterns(n.Pats) + " | " + Print(n.Body))
	case *Label:
		sb.WriteString("label $" + n.Name + " | " + Print(n.Body))
	case *Break:
		sb.WriteString("break $" + n.Name)
	default:
		return false
	}
	return true
}

func printDirective(sb *strings.Builder, n Node) bool {
	switch n := n.(type) {
	case *Module:
		sb.WriteString("module " + Print(n.Meta) + ";")
		printRest(sb, n.Rest)
	case *Import:
		printImport(sb, n)
	case *Include:
		sb.WriteString("include " + quote(n.Path))
		printMeta(sb, n.Meta)
		sb.WriteString(";")
		printRest(sb, n.Rest)
	default:
		return false
	}
	return true
}

func printForeach(sb *strings.Builder, n *Foreach) {
	sb.WriteString("foreach " + child(n.Src) + " as " + printPatterns(n.Pats) +
		" (" + Print(n.Init) + "; " + Print(n.Update))
	if n.Extract != nil {
		sb.WriteString("; " + Print(n.Extract))
	}
	sb.WriteString(")")
}

// printSuffix renders a postfix form. A nil target means the form applies
// to the input, so no parentheses surround it.
// printField renders a field. A leading ".name?" prints with parentheses,
// like the postfix "?" on any other term.
func printField(sb *strings.Builder, n *Field) {
	if n.Target == nil && n.Opt {
		sb.WriteString("(." + n.Name + ")?")
		return
	}
	printSuffix(sb, n.Target, "."+n.Name, n.Opt, false)
}

func printSuffix(sb *strings.Builder, target Node, tail string, opt, bracket bool) {
	if opt {
		tail += "?"
	}
	if target == nil {
		if !bracket {
			sb.WriteString(tail)
			return
		}
		sb.WriteString("." + tail)
		return
	}
	sb.WriteString("(" + child(target) + tail + ")")
}

func printInfix(sb *strings.Builder, op string, l, r Node) {
	sep := " " + op + " "
	if op == "," {
		sep = ", "
	}
	sb.WriteString("(" + child(l) + sep + child(r) + ")")
}

func printRest(sb *strings.Builder, rest Node) {
	if rest != nil {
		sb.WriteString(" " + Print(rest))
	}
}

func printMeta(sb *strings.Builder, meta Node) {
	if meta != nil {
		sb.WriteString(" " + Print(meta))
	}
}

func printImport(sb *strings.Builder, n *Import) {
	sb.WriteString("import " + quote(n.Path) + " as ")
	if n.Data {
		sb.WriteString("$")
	}
	sb.WriteString(n.Alias)
	printMeta(sb, n.Meta)
	sb.WriteString(";")
	printRest(sb, n.Rest)
}

func printFuncDef(sb *strings.Builder, n *FuncDef) {
	sb.WriteString("def " + n.Name)
	if len(n.Params) > 0 {
		sb.WriteString("(" + strings.Join(n.Params, "; ") + ")")
	}
	sb.WriteString(": " + Print(n.Body) + ";")
	printRest(sb, n.Rest)
}

func printIf(sb *strings.Builder, n *If) {
	sb.WriteString("if " + Print(n.Cond) + " then " + Print(n.Then))
	for _, e := range n.Elifs {
		sb.WriteString(" elif " + Print(e.Cond) + " then " + Print(e.Then))
	}
	if n.Else != nil {
		sb.WriteString(" else " + Print(n.Else))
	}
	sb.WriteString(" end")
}

func joinNodes(ns []Node, sep string) string {
	parts := make([]string, len(ns))
	for i, n := range ns {
		parts[i] = Print(n)
	}
	return strings.Join(parts, sep)
}

// quote renders s as a JSON string without escaping HTML characters.
func quote(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return strconv.Quote(s)
	}
	return strings.TrimSuffix(buf.String(), "\n")
}

func printLiteral(sb *strings.Builder, v any) {
	switch v := v.(type) {
	case nil:
		sb.WriteString("null")
	case bool:
		sb.WriteString(strconv.FormatBool(v))
	case string:
		sb.WriteString(quote(v))
	case Number:
		sb.WriteString(string(v))
	case NullText:
		sb.WriteString(string(v))
	}
}

func printStr(sb *strings.Builder, n *Str) {
	if n.Format != "" {
		sb.WriteString(n.Format + " ")
	}
	sb.WriteString(`"`)
	for _, p := range n.Parts {
		if lit, ok := p.(*Literal); ok {
			if s, ok := lit.Value.(string); ok {
				q := quote(s)
				sb.WriteString(q[1 : len(q)-1])
				continue
			}
		}
		sb.WriteString(`\(` + Print(p) + `)`)
	}
	sb.WriteString(`"`)
}

// printKey renders an object key or object-pattern key.
func printKey(k Node) string {
	switch k := k.(type) {
	case *Literal:
		if s, ok := k.Value.(string); ok {
			return s
		}
	case *Str:
		return Print(k)
	case *Var:
		return "$" + k.Name
	}
	return "(" + Print(k) + ")"
}

func printObject(sb *strings.Builder, n *ObjectCons) {
	sb.WriteString("{")
	for i, e := range n.Entries {
		if i > 0 {
			sb.WriteString(", ")
		}
		if v, ok := e.Val.(*Var); ok {
			if k, ok := e.Key.(*Literal); ok && k.Value == v.Name {
				sb.WriteString("$" + v.Name)
				continue
			}
		}
		sb.WriteString(printKey(e.Key) + ": " + Print(e.Val))
	}
	sb.WriteString("}")
}

func printPatterns(ps []Pattern) string {
	parts := make([]string, len(ps))
	for i, p := range ps {
		parts[i] = printPattern(p)
	}
	return strings.Join(parts, " ?// ")
}

func printPattern(p Pattern) string {
	switch p.Kind {
	case PatVar:
		return "$" + p.Name
	case PatObject:
		return printObjectPattern(p)
	case PatArray:
		parts := make([]string, len(p.Elems))
		for i, e := range p.Elems {
			parts[i] = printPattern(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	}
	return ""
}

func printObjectPattern(p Pattern) string {
	parts := make([]string, len(p.Entries))
	for i, e := range p.Entries {
		s := printKey(e.Key)
		if e.KeyVar != "" {
			s = "$" + e.KeyVar
		}
		if e.Val != nil {
			s += ": " + printPattern(*e.Val)
		}
		parts[i] = s
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

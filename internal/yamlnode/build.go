package yamlnode

import (
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/token"
)

// builder turns goccy ASTs into Nodes. One builder serves a whole stream,
// because yaml.v3 keeps anchors across documents. %TAG handles last only
// for the document that declares them, as in libyaml.
type builder struct {
	anchors     map[string]*Node
	tagHandles  map[string]string
	nulledLines map[int]bool

	// lastBlock is the most recent block scalar built, and lastBlockStrips
	// records whether its header asked for strip chomping. Parse uses them
	// when the stream ends inside that scalar.
	lastBlock       *Node
	lastBlockStrips bool
}

func newBuilder(nulledLines []int) *builder {
	b := &builder{
		anchors:     map[string]*Node{},
		tagHandles:  map[string]string{},
		nulledLines: map[int]bool{},
	}
	for _, l := range nulledLines {
		b.nulledLines[l] = true
	}
	return b
}

// document builds the one document a chunk holds. goccy reports %YAML and
// %TAG directives as documents of their own, so we read their handles and
// skip them. A chunk's directives travel with it, so the handles start
// over at every chunk.
func (b *builder) document(file *ast.File, c chunk) (*Node, error) {
	clear(b.tagHandles)
	var body ast.Node
	found := false
	for _, d := range file.Docs {
		if dir, ok := d.Body.(*ast.DirectiveNode); ok {
			b.readDirective(dir)
			continue
		}
		if !found {
			body, found = d.Body, true
		}
	}
	root, err := b.build(body)
	if err != nil {
		return nil, err
	}
	if root.Line == 0 {
		root.Line, root.Column = c.startLine, 1
	}
	return &Node{Kind: DocumentNode, Line: root.Line, Column: 1, Content: []*Node{root}}, nil
}

func (b *builder) readDirective(dir *ast.DirectiveNode) {
	if dir.Name == nil || dir.Name.GetToken().Value != "TAG" || len(dir.Values) != 2 {
		return
	}
	b.tagHandles[dir.Values[0].GetToken().Value] = dir.Values[1].GetToken().Value
}

// build converts one goccy node. Tags and anchors wrap the node they
// apply to, so we peel them off first.
func (b *builder) build(n ast.Node) (*Node, error) {
	explicitTag, anchor := "", ""
	for {
		switch x := n.(type) {
		case *ast.TagNode:
			t, err := b.expandTag(x.Start)
			if err != nil {
				return nil, err
			}
			if t != "" {
				explicitTag = t
			}
			n = x.Value
			continue
		case *ast.AnchorNode:
			anchor = x.Name.GetToken().Value
			n = x.Value
			continue
		}
		break
	}

	if alias, ok := n.(*ast.AliasNode); ok {
		name := alias.Value.GetToken().Value
		target := b.anchors[name]
		if target == nil {
			return nil, &ParseError{Message: "unknown anchor '" + name + "' referenced"}
		}
		out := &Node{Kind: AliasNode, Value: name, Alias: target}
		b.at(out, alias)
		return out, nil
	}

	out, err := b.buildValue(n, anchor)
	if err != nil {
		return nil, err
	}
	if explicitTag != "" {
		out.Tag = explicitTag
	}
	if anchor != "" && out.Anchor == "" {
		out.Anchor = anchor
		b.anchors[anchor] = out
	}
	return out, nil
}

func (b *builder) buildValue(n ast.Node, anchor string) (*Node, error) {
	switch x := n.(type) {
	case nil:
		return &Node{Kind: ScalarNode, Tag: tagNull}, nil
	case *ast.MappingNode:
		out := b.collection(MappingNode, tagMap, anchor, x)
		for _, mv := range x.Values {
			if err := b.pair(out, mv); err != nil {
				return nil, err
			}
		}
		return out, nil
	case *ast.MappingValueNode:
		out := b.collection(MappingNode, tagMap, anchor, x)
		return out, b.pair(out, x)
	case *ast.SequenceNode:
		out := b.collection(SequenceNode, tagSeq, anchor, x)
		for _, v := range x.Values {
			item, err := b.build(v)
			if err != nil {
				return nil, err
			}
			out.Content = append(out.Content, item)
		}
		return out, nil
	case ast.ScalarNode:
		return b.buildScalar(x), nil
	default:
		return nil, &ParseError{Line: n.GetToken().Position.Line, Message: "unsupported YAML node " + n.Type().String()}
	}
}

// buildScalar converts a goccy scalar. A quoted or block scalar is always
// "!!str", a plain scalar gets yaml.v3's resolved tag for its source text,
// and goccy's own typing is ignored.
func (b *builder) buildScalar(n ast.ScalarNode) *Node {
	switch x := n.(type) {
	case *ast.LiteralNode:
		out := b.scalar(x, tagStr, literalValue(x))
		b.lastBlock, b.lastBlockStrips = out, strings.Contains(x.Start.Value, "-")
		return out
	case *ast.MergeKeyNode:
		return b.scalar(x, tagMerge, "<<")
	case *ast.StringNode:
		if tt := x.GetToken().Type; tt == token.SingleQuoteType || tt == token.DoubleQuoteType {
			return b.scalar(x, tagStr, x.Value)
		}
		return b.scalar(x, ResolvePlainTag(x.Value), x.Value)
	case *ast.NullNode:
		tk := x.GetToken()
		v := tk.Value
		if tk.Type == token.ImplicitNullType || (v == "~" && b.nulledLines[tk.Position.Line]) {
			v = ""
		}
		return b.scalar(x, tagNull, v)
	default:
		v := n.GetToken().Value
		return b.scalar(n, ResolvePlainTag(v), v)
	}
}

// collection starts a mapping or sequence and registers its anchor before
// its children are built, as yaml.v3's parser does.
func (b *builder) collection(kind Kind, tag, anchor string, n ast.Node) *Node {
	out := &Node{Kind: kind, Tag: tag, Anchor: anchor}
	if anchor != "" {
		b.anchors[anchor] = out
	}
	b.at(out, n)
	return out
}

func (b *builder) pair(out *Node, mv *ast.MappingValueNode) error {
	var keyNode ast.Node = mv.Key
	if mk, ok := keyNode.(*ast.MappingKeyNode); ok {
		keyNode = mk.Value
	}
	k, err := b.build(keyNode)
	if err != nil {
		return err
	}
	v, err := b.build(mv.Value)
	if err != nil {
		return err
	}
	out.Content = append(out.Content, k, v)
	return nil
}

func (b *builder) scalar(n ast.Node, tag, value string) *Node {
	out := &Node{Kind: ScalarNode, Tag: tag, Value: value}
	b.at(out, n)
	return out
}

// at records the source position of n on out.
func (b *builder) at(out *Node, n ast.Node) {
	if tk := n.GetToken(); tk != nil {
		out.Line, out.Column = tk.Position.Line, tk.Position.Column
	}
}

// expandTag applies yaml.v3's tag rules to an explicit tag: a bare "!" is
// ignored, a verbatim "!<...>" tag is unwrapped, a %TAG handle is
// expanded, and the result is shortened with ShortTag. A named handle
// such as "!e!" that no %TAG directive declared is libyaml's "found
// undefined tag handle" error, which yaml.v3 reports one line above the
// node, and without a line on the first line.
func (b *builder) expandTag(tk *token.Token) (string, error) {
	tag := tk.Value
	switch {
	case tag == "!":
		return "", nil
	case strings.HasPrefix(tag, "!<") && strings.HasSuffix(tag, ">"):
		return ShortTag(tag[2 : len(tag)-1]), nil
	case strings.HasPrefix(tag, "!!"):
		if prefix, ok := b.tagHandles["!!"]; ok {
			return ShortTag(prefix + tag[2:]), nil
		}
		return tag, nil
	}
	end := strings.Index(tag[1:], "!")
	if end < 0 {
		if prefix, ok := b.tagHandles["!"]; ok {
			return ShortTag(prefix + tag[1:]), nil
		}
		return tag, nil
	}
	handle := tag[:end+2]
	prefix, ok := b.tagHandles[handle]
	if !ok {
		return "", &ParseError{Line: tk.Position.Line - 1, Message: "found undefined tag handle"}
	}
	return ShortTag(prefix + tag[end+2:]), nil
}

// literalValue returns a block scalar's value and restores the trailing
// spaces and tabs goccy drops from the last content line of a "-" (strip)
// chomped scalar, keeping any line breaks that follow them.
func literalValue(x *ast.LiteralNode) string {
	value := x.Value.Value
	if !strings.Contains(x.Start.Value, "-") {
		return value
	}
	lines := strings.Split(x.Value.GetToken().Origin, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimRight(lines[i], "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		trailing := line[len(strings.TrimRight(line, " \t")):]
		if !strings.HasSuffix(value, trailing) {
			value += trailing
		}
		break
	}
	return value
}

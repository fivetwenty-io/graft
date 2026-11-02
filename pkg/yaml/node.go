package yaml

import (
	"github.com/fivetwenty-io/graft/internal/yamlcodec"
	"github.com/fivetwenty-io/graft/internal/yamlnode"
)

// Node is one node of a parsed YAML document, laid out like yaml.v3's
// Node so that code walking a v3 tree reads this one unchanged.
//
// Unmarshal into a *Node fills it as a DocumentNode whose single child
// is the document's root. An alias stays an AliasNode whose Alias is the
// very *Node that holds the anchor, so a walker can compare pointers to
// guard against revisiting a node. A << merge key stays in its mapping as
// a key tagged !!merge, with its value beside it, and nothing is merged
// in. Unmarshal refuses an alias inside the collection its anchor names,
// so no tree it returns has a cycle.
//
// A Node also remembers, out of sight, how its scalar was written, such
// as quoted or as a block, and the DecodeOptions it was read with, so
// that Decode reads it as Unmarshal read the text.
type Node struct {
	// Kind says whether the node is a document, sequence, mapping, scalar,
	// or alias.
	Kind Kind
	// Tag is the node's resolved short tag, such as "!!str" or "!!map",
	// and "!!merge" for a "<<" key. Tags resolve as yaml.v3 resolves
	// them, so a plain yes is "!!str" whatever DecodeOptions says. A
	// walker that wants YAML 1.1 booleans calls Decode or reads Value.
	Tag string
	// Value is a scalar's text, or an alias's anchor name.
	Value string
	// Content holds a document's root, a sequence's items, or a mapping's
	// keys and values in alternating order.
	Content []*Node
	// Alias points at the node an alias refers to, which is the same
	// *Node that appears where the anchor was defined.
	Alias *Node
	// Line is the node's 1-based line in the input.
	Line int
	// Column is the node's 1-based column in the input.
	Column int

	// style is how the scalar was written, which decides how Decode
	// writes it again, and it is zero for a Node a caller built.
	style yamlnode.Style
	// read is set on a Node that Unmarshal read, so Decode can tell a
	// plain scalar, whose tag follows YAML 1.2, from one a caller built.
	read bool
	// opts are the options the tree was read with, which Decode uses.
	opts DecodeOptions
}

// Kind says what a Node is, with yaml.v3's type and values.
type Kind uint32

const (
	// DocumentNode is the root of a document, with its single child in Content.
	DocumentNode Kind = 1 << iota
	// SequenceNode is a sequence, with its items in Content.
	SequenceNode
	// MappingNode is a mapping, with its keys and values in Content.
	MappingNode
	// ScalarNode is a scalar, with its text in Value.
	ScalarNode
	// AliasNode is an alias, with its target in Alias.
	AliasNode
)

// publicKinds maps each internal kind, numbered 1 through 5 in yaml.v3's
// order, to the public kind with yaml.v3's value.
var publicKinds = [...]Kind{
	yamlnode.DocumentNode: DocumentNode,
	yamlnode.SequenceNode: SequenceNode,
	yamlnode.MappingNode:  MappingNode,
	yamlnode.ScalarNode:   ScalarNode,
	yamlnode.AliasNode:    AliasNode,
}

// decodeNode is the route for a *Node target. It reads the first document
// of data with graft's YAML reader and copies it into out. Empty input,
// or input that holds only comments, leaves out unchanged.
func decodeNode(data []byte, out *Node, opts DecodeOptions) error {
	doc, err := yamlcodec.FirstMergeDocument(data)
	if err != nil {
		return translate(err)
	}
	docs, err := yamlnode.Parse(doc)
	if err != nil {
		return translate(err)
	}
	if len(docs) == 0 {
		return nil
	}
	c := &converter{seen: make(map[*yamlnode.Node]*Node), opts: opts}
	var root Node
	c.fill(&root, docs[0])
	*out = root
	return nil
}

// converter copies an internal tree into public Nodes. seen maps each
// internal node to its copy, so every alias points at the copy of its
// anchor, and a node is copied once however many times it is reached.
// Each node enters seen before its children are copied, so the copy ends
// on any finite tree, even one with a cycle.
type converter struct {
	seen map[*yamlnode.Node]*Node
	opts DecodeOptions
}

// node returns the copy of in, making it on first sight.
func (c *converter) node(in *yamlnode.Node) *Node {
	if in == nil {
		return nil
	}
	if out, ok := c.seen[in]; ok {
		return out
	}
	out := &Node{}
	c.fill(out, in)
	return out
}

// fill makes out the copy of in.
func (c *converter) fill(out *Node, in *yamlnode.Node) {
	c.seen[in] = out
	out.Tag, out.Value = in.Tag, in.Value
	out.Line, out.Column = in.Line, in.Column
	out.style, out.read, out.opts = in.Style, true, c.opts
	if int(in.Kind) < len(publicKinds) {
		out.Kind = publicKinds[in.Kind]
	}
	out.Alias = c.node(in.Alias)
	if len(in.Content) > 0 {
		out.Content = make([]*Node, len(in.Content))
		for i, child := range in.Content {
			out.Content[i] = c.node(child)
		}
	}
}

// Decode decodes n, and everything under it, into v, with the routes,
// the rules, and the errors of Unmarshal. A plain yes decodes into a bool
// as true, struct tags apply, map[interface{}]interface{} is refused, and
// every error is a *DecodeError. A tree read with UnmarshalWith decodes
// with the options it was read with, and any other Node decodes with the
// default ones, so it reads YAML 1.1 booleans.
//
// Decode reads the tree as it is now, so a Node the caller changed after
// Unmarshal, or built by hand, decodes what it holds. A scalar Unmarshal
// read keeps the form it was written in, so a plain one stays plain and
// a yes in it is still a boolean, even after its Value changes. A scalar
// a caller built is read by its Tag, so a !!str is always a string, and
// one with no Tag reads as the same text written plainly would. An alias resolves
// wherever its anchor sits, inside n or outside it, and a node that
// appears in two places decodes in both. A DocumentNode decodes its
// root, and one with no root leaves v unchanged.
//
// An error's Line and Column are those of the node it is about, so they
// point at the input the tree was read from, and at the anchor's own
// lines for a value reached through an alias. Column is 0 when the error
// falls inside a node's text rather than at its start, and both are 0
// where the failure has no node. A Node built by hand gives the Line and
// Column the caller set, which are 0 unless set.
//
// Decode works by writing n out as YAML text, keeping each scalar quoted,
// plain, or a block as it was written, and decoding that text with
// UnmarshalWith. A type with its own UnmarshalYAML method therefore
// receives that text, which keeps every value but not the layout or the
// comments of the input. A tree that nests more than 32 levels below n is
// written in flow style from there down, where a block scalar has to be
// written quoted, so a block scalar that holds only yes reads as a string
// that deep where it would otherwise read as true.
//
// Decode into a *Node copies n into it. A malformed tree, such as one
// with an alias that points at nothing, a mapping with an odd number of
// children, a mapping key that is a sequence or mapping, or a node inside
// itself, is an ErrorKindSyntax error, and a tree nested more than 10,000
// levels deep is an ErrorKindLimit error, as is one whose aliases expand
// past the limit Unmarshal sets.
// Decode never changes n, so several goroutines may decode one tree at
// once while none of them changes it.
func (n *Node) Decode(v any) error {
	if n == nil {
		return typeError("Decode on a nil *yaml.Node")
	}
	if p, ok := v.(*Node); ok {
		if p == nil {
			return typeError("Decode(nil %T)", p)
		}
		*p = *n
		return nil
	}
	if err := checkTargetFor("Decode", v); err != nil {
		return err
	}
	text, places, err := writeNode(n)
	if err != nil || text == nil {
		return err
	}
	return places.relocate(UnmarshalWith(text, v, n.opts))
}

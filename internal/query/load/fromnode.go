package load

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/fivetwenty-io/graft/internal/yamlnode"
	"github.com/fivetwenty-io/graft/pkg/decimal"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// defaultAliasBudget is the number of nodes alias expansion may produce
// when FromNodeOptions leaves AliasBudget at zero.
const defaultAliasBudget = 1_000_000

// FromNodeOptions selects the YAML-to-value rule. The jq dialect loads with
// StringKeys and without KeepMeta, and the yq dialect the other way round.
type FromNodeOptions struct {
	// KeepMeta gives every value a Meta, leaves alias nodes unexpanded with
	// AliasOf set, and keeps merge keys as entries.
	KeepMeta bool
	// StringKeys formats every key to its text, rejects two keys that
	// collide, and folds << merge keys into the mapping.
	StringKeys bool
	// AliasBudget caps the nodes alias expansion produces, and zero means
	// 1,000,000.
	AliasBudget int
	// DepthLimit caps nesting, aliases included, and zero means 10,000.
	DepthLimit int
}

// nodeError is a conversion failure, with the line of the node at fault
// when yamlnode gives one.
type nodeError struct {
	Line int
	Msg  string
}

func (e *nodeError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("line %d: %s", e.Line, e.Msg)
	}
	return e.Msg
}

func errAt(line int, format string, args ...any) error {
	return &nodeError{Line: line, Msg: fmt.Sprintf(format, args...)}
}

// FromNode converts a YAML node, or a document node's root, to a value.
func FromNode(n *yamlnode.Node, opts FromNodeOptions) (*value.Value, error) {
	v, _, err := convertDocument(n, opts, docSite{})
	return v, err
}

// docSite says where a document came from, for the Meta of its values.
type docSite struct {
	docIndex, fileIndex int
	filename            string
}

type converter struct {
	opts    FromNodeOptions
	site    docSite
	budget  int
	limit   int
	inAlias int
	depth   int
	anchors map[string]*value.Value
}

func convertDocument(n *yamlnode.Node, opts FromNodeOptions, site docSite) (*value.Value, map[string]*value.Value, error) {
	c := &converter{opts: opts, site: site, budget: opts.AliasBudget, anchors: map[string]*value.Value{}}
	c.limit = opts.DepthLimit
	if c.limit <= 0 {
		c.limit = defaultDepthLimit
	}
	if c.budget <= 0 {
		c.budget = defaultAliasBudget
	}
	v, err := c.node(n)
	if err != nil {
		return nil, nil, err
	}
	return v, c.anchors, nil
}

func (c *converter) node(n *yamlnode.Node) (*value.Value, error) {
	if n == nil {
		return value.Null(), nil
	}
	if c.inAlias > 0 {
		c.budget--
		if c.budget < 0 {
			return nil, errAt(n.Line, "alias expansion limit exceeded")
		}
	}
	var v *value.Value
	var err error
	switch n.Kind {
	case yamlnode.DocumentNode:
		if len(n.Content) == 0 {
			return value.Null(), nil
		}
		return c.node(n.Content[0])
	case yamlnode.AliasNode:
		return c.alias(n)
	case yamlnode.SequenceNode, yamlnode.MappingNode:
		c.depth++
		if c.depth > c.limit {
			return nil, errAt(n.Line, "nesting exceeds maximum depth of %d", c.limit)
		}
		if n.Kind == yamlnode.SequenceNode {
			v, err = c.sequence(n)
		} else {
			v, err = c.mapping(n)
		}
		c.depth--
	default:
		v = c.scalar(n)
	}
	if err != nil {
		return nil, err
	}
	if c.opts.KeepMeta {
		v = v.WithMeta(c.meta(n, v))
	}
	if n.Anchor != "" && c.inAlias == 0 {
		c.anchors[n.Anchor] = v
	}
	return v, nil
}

func (c *converter) alias(n *yamlnode.Node) (*value.Value, error) {
	if c.opts.KeepMeta {
		m := c.meta(n, nil)
		m.AliasOf = n.Value
		return value.Null().WithMeta(m), nil
	}
	target := yamlnode.FollowAlias(n)
	if target == nil || target == n || target.Kind == yamlnode.AliasNode {
		return nil, errAt(n.Line, "unknown anchor %q referenced", n.Value)
	}
	c.inAlias++
	defer func() { c.inAlias-- }()
	return c.node(target)
}

func (c *converter) sequence(n *yamlnode.Node) (*value.Value, error) {
	b := value.NewArrBuilder(len(n.Content))
	for _, e := range n.Content {
		v, err := c.node(e)
		if err != nil {
			return nil, err
		}
		b.Append(v)
	}
	return b.Seal(), nil
}

func (c *converter) mapping(n *yamlnode.Node) (*value.Value, error) {
	if c.opts.StringKeys {
		return c.stringKeyed(n)
	}
	entries := make([]value.Entry, 0, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, err := c.key(n.Content[i])
		if err != nil {
			return nil, err
		}
		v, err := c.node(n.Content[i+1])
		if err != nil {
			return nil, err
		}
		entries = append(entries, value.Entry{Key: k, Value: v})
	}
	return value.ObjEntries(entries...), nil
}

// key converts a mapping key, which must be a scalar.
func (c *converter) key(k *yamlnode.Node) (*value.Value, error) {
	if k.Kind == yamlnode.AliasNode && !c.opts.KeepMeta {
		return c.node(k)
	}
	if k.Kind != yamlnode.ScalarNode && k.Kind != yamlnode.AliasNode {
		return nil, errAt(k.Line, "mapping keys must be scalars")
	}
	return c.node(k)
}

// stringKeyed builds an object with text keys, folding merge keys in with
// explicit keys winning and earlier merged mappings winning over later
// ones.
func (c *converter) stringKeyed(n *yamlnode.Node) (*value.Value, error) {
	explicit := make(map[string]bool, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		if isMergeKey(n.Content[i]) {
			continue
		}
		text, err := keyText(n.Content[i])
		if err != nil {
			return nil, err
		}
		if explicit[text] {
			return nil, errAt(n.Content[i].Line, "duplicate key %q", text)
		}
		explicit[text] = true
	}
	entries := make([]value.Entry, 0, len(n.Content)/2)
	merged := map[string]bool{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, vn := n.Content[i], n.Content[i+1]
		if isMergeKey(k) {
			from, err := c.mergeSources(vn)
			if err != nil {
				return nil, err
			}
			for _, e := range from {
				text := e.Key.Str()
				if explicit[text] || merged[text] {
					continue
				}
				merged[text] = true
				entries = append(entries, e)
			}
			continue
		}
		text, _ := keyText(k)
		v, err := c.node(vn)
		if err != nil {
			return nil, err
		}
		entries = append(entries, value.Entry{Key: value.Str(text), Value: v})
	}
	return value.ObjEntries(entries...), nil
}

// mergeSources returns the entries of the mapping, or of each mapping in a
// sequence, that a << key names.
func (c *converter) mergeSources(vn *yamlnode.Node) ([]value.Entry, error) {
	target := yamlnode.FollowAlias(vn)
	var nodes []*yamlnode.Node
	switch target.Kind {
	case yamlnode.MappingNode:
		nodes = []*yamlnode.Node{vn}
	case yamlnode.SequenceNode:
		nodes = target.Content
	case yamlnode.DocumentNode, yamlnode.ScalarNode, yamlnode.AliasNode:
	}
	if nodes == nil {
		return nil, errAt(vn.Line, "merge key value must be a mapping or a sequence of mappings")
	}
	var out []value.Entry
	for _, m := range nodes {
		if yamlnode.FollowAlias(m).Kind != yamlnode.MappingNode {
			return nil, errAt(m.Line, "merge key value must be a mapping or a sequence of mappings")
		}
		v, err := c.expandedAlias(m)
		if err != nil {
			return nil, err
		}
		out = append(out, v.Entries()...)
	}
	return out, nil
}

// expandedAlias converts a node as an alias expansion would, so merged
// content counts against the budget.
func (c *converter) expandedAlias(n *yamlnode.Node) (*value.Value, error) {
	c.inAlias++
	defer func() { c.inAlias-- }()
	return c.node(yamlnode.FollowAlias(n))
}

func isMergeKey(k *yamlnode.Node) bool {
	return k.Kind == yamlnode.ScalarNode && k.Value == "<<" && (k.Tag == yamlnode.TagMerge || k.Tag == "")
}

func keyText(k *yamlnode.Node) (string, error) {
	k = yamlnode.FollowAlias(k)
	if k.Kind != yamlnode.ScalarNode {
		return "", errAt(k.Line, "mapping keys must be scalars")
	}
	return k.Value, nil
}

func (c *converter) scalar(n *yamlnode.Node) *value.Value {
	tag := n.Tag
	if tag == "" {
		tag = yamlnode.ResolvePlainTag(n.Value)
	}
	switch yamlnode.ShortTag(tag) {
	case yamlnode.TagNull:
		return value.Null()
	case yamlnode.TagBool:
		switch strings.ToLower(n.Value) {
		case "true", "yes", "on", "y":
			return value.Bool(true)
		}
		return value.Bool(false)
	case yamlnode.TagInt, yamlnode.TagFloat:
		if v, ok := numberValue(n.Value); ok {
			return v
		}
	}
	return value.Str(n.Value)
}

func (c *converter) meta(n *yamlnode.Node, v *value.Value) *value.Meta {
	m := &value.Meta{
		Anchor:      n.Anchor,
		HeadComment: n.HeadComment, LineComment: n.LineComment, FootComment: n.FootComment,
		Line: n.Line, Column: n.Column,
		DocIndex: c.site.docIndex, FileIndex: c.site.fileIndex, Filename: c.site.filename,
	}
	if n.Kind == yamlnode.AliasNode {
		return m
	}
	if strings.HasPrefix(n.Tag, "!") && !strings.HasPrefix(n.Tag, "!!") {
		m.Tag, m.Style = n.Tag, value.Tagged
	}
	if v != nil && v.Kind() == value.KindNumber && v.Literal() == "" {
		m.Text = n.Value
	}
	return m
}

// numberLiteral returns a number that prints as s. A literal past the decimal
// bounds keeps its text and has no coefficient, so it passes through unchanged.
func numberLiteral(s string) *value.Value {
	d, _ := decimal.Parse(s)
	if d.Err() != nil {
		return value.NumText(s)
	}
	return value.NumLit(d, s)
}

// numberValue reads a YAML number. The text is kept as the literal only when
// it is also a valid JSON number, and the other spellings (0x10, 012, 1_000,
// .5, .inf) are parsed without one.
func numberValue(text string) (*value.Value, bool) {
	if isJSONNumber(text) {
		return numberLiteral(text), true
	}
	plain := strings.ReplaceAll(text, "_", "")
	switch strings.ToLower(strings.TrimLeft(plain, "+-")) {
	case ".inf":
		return value.NumDec(decimal.Inf(signOf(plain))), true
	case ".nan":
		return value.NumDec(decimal.NaN()), true
	}
	if i, ok := new(big.Int).SetString(plain, 0); ok {
		if d, err := decimal.Parse(i.String()); err == nil {
			return value.NumDec(d), true
		}
	}
	if s, ok := jsonFloat(plain); ok {
		if d, err := decimal.Parse(s); err == nil {
			return value.NumDec(d), true
		}
	}
	if f, err := strconv.ParseFloat(plain, 64); err == nil {
		return value.Num(f), true
	}
	return nil, false
}

func signOf(s string) int {
	if strings.HasPrefix(s, "-") {
		return -1
	}
	return 1
}

// jsonFloat rewrites a YAML float such as .5, 1., +1.5, or 09.5e3 in JSON's
// grammar.
func jsonFloat(s string) (string, bool) {
	sign := ""
	if s != "" && (s[0] == '+' || s[0] == '-') {
		if s[0] == '-' {
			sign = "-"
		}
		s = s[1:]
	}
	wholeEnd := skipDigits(s, 0)
	whole := strings.TrimLeft(s[:wholeEnd], "0")
	if whole == "" {
		whole = "0"
	}
	frac, i := "", wholeEnd
	if i < len(s) && s[i] == '.' {
		j := skipDigits(s, i+1)
		frac, i = s[i+1:j], j
	}
	if wholeEnd == 0 && frac == "" {
		return "", false
	}
	exp, end, ok := scanExponent(s, i)
	if !ok || end != len(s) {
		return "", false
	}
	out := sign + whole
	if frac != "" {
		out += "." + frac
	}
	return out + exp, true
}

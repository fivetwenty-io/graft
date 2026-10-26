package yq

import (
	"fmt"
	"io"
	"iter"
	"maps"
	"slices"
	"strings"

	"github.com/fivetwenty-io/graft/internal/query/builtin"
	"github.com/fivetwenty-io/graft/internal/query/load"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/internal/query/yamlout"
	"github.com/fivetwenty-io/graft/internal/yamlnode"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// The names of the operators that read and set a node's details. The
// comment names are also the slots a comment operator reads or writes.
const (
	lineComment = "line_comment"
	headComment = "head_comment"
	footComment = "foot_comment"
	allComments = "comments"
	styleName   = "style"
	tagName     = "tag"
	anchorName  = "anchor"
	aliasName   = "alias"
)

// The kinds yq's kind operator names that more than one operator tests.
const (
	kindScalar = "scalar"
	kindAlias  = "alias"
	kindMap    = "map"
	kindSeq    = "seq"
)

// metaNames maps every spelling yq's lexer gives an operator that can
// stand before "=" or "|=" to the operator's own name.
var metaNames = map[string]string{
	"line_comment": lineComment, "linecomment": lineComment, "lineComment": lineComment,
	"head_comment": headComment, "headcomment": headComment, "headComment": headComment,
	"foot_comment": footComment, "footcomment": footComment, "footComment": footComment,
	allComments: allComments,
	styleName:   styleName,
	tagName:     tagName,
	"type":      tagName,
	anchorName:  anchorName,
	aliasName:   aliasName,
}

// MetaAssignNames are the names that, written just before "=" or "|=",
// make one assignment operator with it, as in ".a style = \"double\"",
// which sets what the name reads on every node the left side names.
func MetaAssignNames() []string {
	return slices.Sorted(maps.Keys(metaNames))
}

// registerMeta adds the operators that read a node's comments, style,
// tag, anchor, alias, and kind, and explode. Each reading emits a string
// at the place of the node it describes, as yq's do. The spelling type
// reads as tag does in yq, and comes with yq's type operators.
func registerMeta(r *runtime.Registry) {
	for spelling, name := range metaNames {
		read := metaReader(name)
		if read == nil || spelling == "type" {
			continue
		}
		r.RegisterSingle(spelling, 0, func(ctx *runtime.Ctx, in *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
			return emitAtCursor(ctx, value.Str(read(in)), emit)
		})
	}
	r.RegisterSingle("kind", 0, func(ctx *runtime.Ctx, in *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		return emitAtCursor(ctx, value.Str(kindOf(in)), emit)
	})
	r.Register("explode", 1, explode)
}

// metaReader returns what the operator called name reads from a node, or
// nil for comments, which yq only assigns.
func metaReader(name string) func(*value.Value) string {
	switch name {
	case lineComment, headComment, footComment:
		return func(v *value.Value) string { return commentOf(v, name) }
	case styleName:
		return styleOf
	case tagName:
		return tagOf
	case anchorName:
		return func(v *value.Value) string {
			if m := v.Meta(); m != nil {
				return m.Anchor
			}
			return ""
		}
	case aliasName:
		return nodeText
	default:
	}
	return nil
}

// emitAtCursor emits v at the cursor's place, where yq puts the string
// that describes a node, so path and parent answer for the node. yq's
// string has the node's parent and not the node's document, so one that
// describes a document's root has no place and belongs to the first
// document, as a value the run built does.
func emitAtCursor(ctx *runtime.Ctx, v *value.Value, emit runtime.Emit) error {
	if c := ctx.Cursor; c != nil && len(c.Path) > 0 {
		return emit(v, runtime.Rooted(c.Root, c.Doc, c.Path))
	}
	return emit(v, nil)
}

// commentOf reads one comment slot of v without the "# " that starts
// each of its lines. A document's head comment is its leading content
// when it has some, printed the way yq prints it without separators.
func commentOf(v *value.Value, slot string) string {
	m := v.Meta()
	if m == nil {
		return ""
	}
	var c string
	switch {
	case slot == lineComment:
		c = m.LineComment
	case slot == headComment && m.LeadingContent != "":
		c = strings.TrimSuffix(yamlout.LeadingContent(m.LeadingContent, false), "\n")
	case slot == headComment:
		c = m.HeadComment
	default:
		c = m.FootComment
	}
	c = strings.TrimPrefix(c, "# ")
	return strings.ReplaceAll(c, "\n# ", "\n")
}

// styleNames are yq's names for the styles a node can be written in.
var styleNames = map[value.StyleFlags]string{
	0:                  "",
	value.Tagged:       "tagged",
	value.DoubleQuoted: "double",
	value.SingleQuoted: "single",
	value.Literal:      "literal",
	value.Folded:       "folded",
	value.Flow:         "flow",
}

// styleOf is yq's name for the style v was written in. yq keeps one
// style per node, so a node written with two, such as a tagged quoted
// scalar, has a style yq cannot name.
func styleOf(v *value.Value) string {
	m := v.Meta()
	if m == nil {
		return ""
	}
	if name, ok := styleNames[m.Style]; ok {
		return name
	}
	return "<unknown>"
}

// parseStyle reads a style name as yq does, where an empty name is no
// style.
func parseStyle(name string) (value.StyleFlags, error) {
	for s, n := range styleNames {
		if n == name {
			return s, nil
		}
	}
	return 0, &query.RuntimeError{Msg: "unknown style " + name}
}

// nodeText is the text yq's node holds: a scalar's text as written, an
// alias's anchor name, and nothing for a collection.
func nodeText(v *value.Value) string {
	m := v.Meta()
	if m != nil && m.AliasOf != "" {
		return m.AliasOf
	}
	switch v.Kind() {
	case value.KindString:
		return v.Str()
	case value.KindNumber:
		return numberText(v)
	case value.KindArray, value.KindObject:
		return ""
	default:
	}
	if m != nil && m.Text != "" {
		return m.Text
	}
	switch v.Kind() {
	case value.KindTrue:
		return "true"
	case value.KindFalse:
		return "false"
	default:
	}
	return "null"
}

// metaOp reads an assignment operator such as "style=" or "tag|=" and
// returns the name of what it sets and whether it is the "|=" form. The
// clobber flag, as in "style=c", changes nothing here.
func metaOp(op string) (string, bool, bool) {
	op = strings.TrimRight(op, "c")
	update := strings.HasSuffix(op, "|=")
	spelling := strings.TrimSuffix(strings.TrimSuffix(op, "="), "|")
	name, ok := metaNames[spelling]
	return name, update, ok
}

// assignMeta sets what name reads on the node at every path. "=" runs
// the right side once on the root and "|=" runs it on each node, and
// either takes the text of its first output, as yq does. A node whose
// update has no output takes the text the node before it took. The
// alias operator leaves a node as it was when the text is empty.
func assignMeta(ctx *runtime.Ctx, root *value.Value, paths []value.Path, rhs func(cur *value.Value, at value.Path) iter.Seq2[*value.Value, error], name string, update bool) (*value.Value, error) {
	var text string
	if !update {
		t, _, err := firstText(rhs(root, nil))
		if err != nil {
			return nil, err
		}
		text = t
	}
	ed := builtin.NewEditor(ctx, root)
	for _, p := range paths {
		cur, err := nodeAt(ed, p)
		if err != nil {
			return nil, err
		}
		if update {
			t, ok, err := firstText(rhs(cur, p))
			if err != nil {
				return nil, err
			}
			if ok {
				text = t
			}
		}
		nv, err := setMeta(cur, name, text)
		if err != nil {
			return nil, err
		}
		if err := setNodeAt(ed, p, nv); err != nil {
			return nil, err
		}
	}
	return ed.Value(), nil
}

// firstText is the text of the first output of seq, and false when it
// has none.
func firstText(seq iter.Seq2[*value.Value, error]) (string, bool, error) {
	for v, err := range seq {
		if err != nil {
			return "", false, err
		}
		return nodeText(v), true, nil
	}
	return "", false, nil
}

// setMeta returns cur with what name reads set to text.
func setMeta(cur *value.Value, name, text string) (*value.Value, error) {
	m := value.Meta{}
	if cm := cur.Meta(); cm != nil {
		m = *cm
	} else if k := cur.Kind(); k == value.KindNull || k == value.KindTrue || k == value.KindFalse {
		// A value the run made has no text, and keeps its spelling.
		m.Text = nodeText(cur)
	}
	switch name {
	case styleName:
		s, err := parseStyle(text)
		if err != nil {
			return nil, err
		}
		m.Style = s
		if s&value.Tagged != 0 && m.Tag == "" {
			// A node yq tags shows the tag yq reads it with.
			m.Tag = tagOf(cur)
		}
	case tagName:
		return retag(cur, m, text), nil
	case anchorName:
		m.Anchor = text
	case aliasName:
		if text == "" {
			return cur, nil
		}
		m.AliasOf, m.Tag = text, ""
		return value.Null().WithMeta(&m), nil
	default:
		setComment(&m, name, text)
	}
	return cur.WithMeta(&m), nil
}

// setComment sets the comment slot name of m to text. A head comment
// replaces the leading content it would print after.
func setComment(m *value.Meta, name, text string) {
	switch name {
	case lineComment:
		m.LineComment = text
	case headComment:
		m.HeadComment, m.LeadingContent = text, ""
	case footComment:
		m.FootComment = text
	case allComments:
		m.HeadComment, m.LineComment, m.FootComment, m.LeadingContent = text, text, text, ""
	default:
	}
}

// retag returns v under tag, with m as its other details. A scalar given
// one of YAML's own scalar tags becomes the value its text reads as, when
// that is a value of the tag, as "1" under !!float is, and a string with
// the tag otherwise, as "x" under !!bool is. Any other node keeps its
// value, and prints with the tag.
func retag(v *value.Value, m value.Meta, tag string) *value.Value {
	m.Tag = tag
	short := yamlnode.ShortTag(tag)
	if kindOf(v) != kindScalar || !coreScalarTag(short) {
		return v.WithMeta(&m)
	}
	text := nodeText(v)
	m.Text = text
	got := value.Str(text)
	if short != yamlnode.TagStr && sameScalarKind(yamlnode.ShortTag(yamlnode.ResolvePlainTag(text)), short) {
		if read, err := load.FromNode(&yamlnode.Node{Kind: yamlnode.ScalarNode, Tag: short, Value: text}, load.FromNodeOptions{}); err == nil {
			got = read
		}
	}
	return got.WithMeta(&m)
}

// coreScalarTag reports whether tag, in its short form, is one of YAML's
// own scalar tags.
func coreScalarTag(tag string) bool {
	switch tag {
	case yamlnode.TagStr, yamlnode.TagInt, yamlnode.TagFloat, yamlnode.TagBool, yamlnode.TagNull:
		return true
	default:
	}
	return false
}

// sameScalarKind reports whether two of YAML's scalar tags give values of
// one kind, which an int and a float do.
func sameScalarKind(a, b string) bool {
	number := func(t string) bool { return t == yamlnode.TagInt || t == yamlnode.TagFloat }
	return a == b || number(a) && number(b)
}

// nodeAt returns the node at p, which for a path ending in KeyMark is the
// map key itself.
func nodeAt(ed *builtin.Editor, p value.Path) (*value.Value, error) {
	if n := len(p); n > 1 && p[n-1] == runtime.KeyMark {
		m, err := ed.Get(p[:n-2])
		if err != nil {
			return nil, err
		}
		if i, ok := entryAt(m, p[n-2]); ok {
			return m.Entries()[i].Key, nil
		}
		return nil, &query.RuntimeError{Msg: "cannot find the key " + nodeText(p[n-2])}
	}
	return ed.Get(p)
}

// setNodeAt puts v at p, replacing the map key itself for a path ending
// in KeyMark.
func setNodeAt(ed *builtin.Editor, p value.Path, v *value.Value) error {
	n := len(p)
	if n < 2 || p[n-1] != runtime.KeyMark {
		return ed.Set(p, v)
	}
	m, err := ed.Get(p[:n-2])
	if err != nil {
		return err
	}
	i, ok := entryAt(m, p[n-2])
	if !ok {
		return nil
	}
	entries := append([]value.Entry(nil), m.Entries()...)
	entries[i].Key = v
	return ed.Set(p[:n-2], value.ObjEntries(entries...).WithMeta(m.Meta()))
}

// entryAt finds the entry of map m whose key is k.
func entryAt(m *value.Value, k *value.Value) (int, bool) {
	if m.Kind() != value.KindObject {
		return 0, false
	}
	want := nodeText(k)
	found := -1
	for i, e := range m.Entries() {
		if nodeText(e.Key) == want {
			found = i
		}
	}
	return found, found >= 0
}

// explode replaces every alias under each path its argument names with
// the node its anchor marks, drops every anchor there, and folds each
// merge key into the map that holds it, then emits its input as changed.
// An alias reads the node its anchor marks where the anchor stands, so
// an anchor the walk has passed through is already exploded.
func explode(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
	root, doc, base := in, 0, value.Path(nil)
	if c := ctx.Cursor; c != nil {
		root, doc, base = c.Root, c.Doc, c.Path
	}
	var targets []value.Path
	err := args[0].Each(in, func(_ *value.Value, p value.Path) error {
		if p != nil {
			_, _, rel := runtime.Unroot(p)
			targets = append(targets, rel)
		}
		return nil
	})
	if err != nil {
		return err
	}
	toSpec := false
	if d, ok := ctx.Dialect.(*Dialect); ok {
		toSpec = d.opts.MergeAnchorToSpec
	}
	// One exploder serves every path, so an alias in a later one still
	// finds an anchor an earlier one exploded away, as yq's alias holds
	// the node itself rather than its anchor's name.
	x := &exploder{anchors: map[string]*value.Value{}, toSpec: toSpec, stderr: ctx.Stderr}
	for _, t := range targets {
		if _, found := resolve(root, t); !found {
			// yq reads the paths as an assignment does, so a missing
			// one is added as null.
			if root, err = builtin.SetPath(ctx, root, t, value.Null()); err != nil {
				return err
			}
		}
		if root, err = x.at(root, value.Path{}, t); err != nil {
			return err
		}
	}
	out, err := builtin.GetPath(ctx, root, base)
	if err != nil {
		return err
	}
	if ctx.Cursor == nil {
		return emit(out, nil)
	}
	return emit(out, runtime.Rooted(root, doc, base))
}

// exploder walks a document in order, keeping the node each anchor it
// has passed marks.
type exploder struct {
	anchors map[string]*value.Value
	toSpec  bool
	// stderr receives the merge key warning, once per run.
	stderr io.Writer
	warned bool
}

// mergeWarning is what a run writes the first time it folds a merge key
// in the way yq does by default.
const mergeWarning = "Warning: --yaml-fix-merge-anchor-to-spec is false, so a merge key overrides the values the map already holds, which the YAML spec does not do. The flag will default to true in a later release.\n"

// warnMerge writes mergeWarning the first time it is called.
func (x *exploder) warnMerge() {
	if x.warned || x.stderr == nil {
		return
	}
	x.warned = true
	_, _ = io.WriteString(x.stderr, mergeWarning)
}

// at walks v, which sits at p, and explodes the node at target, noting
// every anchor before it.
func (x *exploder) at(v *value.Value, p, target value.Path) (*value.Value, error) {
	if len(p) == len(target) {
		return x.node(v)
	}
	x.note(v, v)
	k := target[len(p)]
	switch v.Kind() {
	case value.KindArray:
		i, ok := elementIndex(k, v.Len())
		for j, e := range v.Elems() {
			if ok && j == i {
				ne, err := x.at(e, extend(p, k), target)
				if err != nil {
					return nil, err
				}
				return v.SetIndex(j, ne), nil
			}
			x.notes(e)
		}
	case value.KindObject:
		i, ok := entryAt(v, k)
		entries := v.Entries()
		for j, e := range entries {
			x.notes(e.Key)
			if ok && j == i {
				ne, err := x.at(e.Value, extend(p, k), target)
				if err != nil {
					return nil, err
				}
				out := append([]value.Entry(nil), entries...)
				out[j].Value = ne
				return value.ObjEntries(out...).WithMeta(v.Meta()), nil
			}
			x.notes(e.Value)
		}
	default:
	}
	return v, nil
}

// note records the anchor that orig defines as marking v.
func (x *exploder) note(orig, v *value.Value) {
	if m := orig.Meta(); m != nil && m.Anchor != "" {
		x.anchors[m.Anchor] = v
	}
}

// notes records every anchor under v without changing anything.
func (x *exploder) notes(v *value.Value) {
	x.note(v, v)
	switch v.Kind() {
	case value.KindArray:
		for _, e := range v.Elems() {
			x.notes(e)
		}
	case value.KindObject:
		for _, e := range v.Entries() {
			x.notes(e.Key)
			x.notes(e.Value)
		}
	default:
	}
}

// node explodes v and everything under it.
func (x *exploder) node(v *value.Value) (*value.Value, error) {
	m := v.Meta()
	if m != nil && m.AliasOf != "" {
		target, ok := x.anchors[m.AliasOf]
		if !ok {
			return v, nil
		}
		return aliased(v, target), nil
	}
	var out *value.Value
	var err error
	switch v.Kind() {
	case value.KindArray:
		b := value.NewArrBuilder(v.Len())
		for _, e := range v.Elems() {
			ne, err := x.node(e)
			if err != nil {
				return nil, err
			}
			b.Append(ne)
		}
		out = b.Seal()
	case value.KindObject:
		out, err = x.mapping(v)
		if err != nil {
			return nil, err
		}
	default:
		out = v
	}
	if m != nil {
		nm := *m
		nm.Anchor = ""
		out = out.WithMeta(&nm)
	}
	x.note(v, out)
	return out, nil
}

// aliased is target in the place of alias v: target's value, tag, and
// style, with v's comments and position.
func aliased(v, target *value.Value) *value.Value {
	m := *v.Meta()
	m.AliasOf, m.Anchor = "", ""
	m.Tag, m.Style, m.Text = "", 0, ""
	if tm := target.Meta(); tm != nil {
		m.Tag, m.Style, m.Text = tm.Tag, tm.Style, tm.Text
	}
	return target.WithMeta(&m)
}

// mapping explodes a map, folding its merge keys in as yq does.
func (x *exploder) mapping(v *value.Value) (*value.Value, error) {
	entries := v.Entries()
	merges := false
	for _, e := range entries {
		if isMergeKey(e.Key, x.toSpec) {
			merges = true
			break
		}
	}
	if !merges {
		out := make([]value.Entry, len(entries))
		for i, e := range entries {
			ne, err := x.entry(e)
			if err != nil {
				return nil, err
			}
			out[i] = ne
		}
		return value.ObjEntries(out...), nil
	}
	if x.toSpec {
		return x.mergeToSpec(entries)
	}
	x.warnMerge()
	return x.mergeLegacy(entries)
}

// isMergeKey reports whether k is the merge key "<<": a plain one under
// the YAML spec, and any spelling of it under yq's own merge.
func isMergeKey(k *value.Value, toSpec bool) bool {
	if k.Kind() != value.KindString || k.Str() != "<<" {
		return false
	}
	if !toSpec {
		return true
	}
	m := k.Meta()
	return m == nil || m.Tag == "!!merge" || m.Tag == "" && m.Style&(value.SingleQuoted|value.DoubleQuoted) == 0
}

// entry explodes the key and the value of e.
func (x *exploder) entry(e value.Entry) (value.Entry, error) {
	k, err := x.node(e.Key)
	if err != nil {
		return value.Entry{}, err
	}
	v, err := x.node(e.Value)
	if err != nil {
		return value.Entry{}, err
	}
	return value.Entry{Key: k, Value: v}, nil
}

// mergeLegacy folds merge keys in the way yq does by default. Each map an
// alias under "<<" names lends its entries, and a later entry of the same
// key replaces an earlier one in place, unless the map sets the key
// itself after the merge, which then wins at its own place. A merge of a
// sequence takes its aliases from the last to the first, and a merge of
// anything but an alias adds nothing.
func (x *exploder) mergeLegacy(entries []value.Entry) (*value.Value, error) {
	l := legacyMerge{x: x, entries: entries}
	for i, e := range entries {
		var err error
		switch {
		case !isMergeKey(e.Key, false):
			err = l.put(i, e.Key, e.Value)
		case e.Value.Kind() == value.KindArray && !isAlias(e.Value):
			elems := e.Value.Elems()
			for j := len(elems) - 1; j >= 0 && err == nil; j-- {
				err = l.lend(i, elems[j])
			}
		default:
			err = l.lend(i, e.Value)
		}
		if err != nil {
			return nil, err
		}
	}
	return value.ObjEntries(l.out...), nil
}

// legacyMerge is a map that mergeLegacy is building from entries.
type legacyMerge struct {
	x       *exploder
	entries []value.Entry
	out     []value.Entry
}

// put adds key k with value v, which the entry at index from gives, in
// place of an earlier entry of k, and leaves it out when an entry after
// from sets k.
func (l *legacyMerge) put(from int, k, v *value.Value) error {
	v, err := l.x.node(v)
	if err != nil {
		return err
	}
	for i := range l.out {
		if !isAlias(l.out[i].Key) && !isAlias(k) && nodeText(l.out[i].Key) == nodeText(k) {
			l.out[i].Value = v
			return nil
		}
	}
	for _, e := range l.entries[from+1:] {
		if nodeText(e.Key) == nodeText(k) && !isAlias(e.Key) {
			return nil
		}
	}
	if k, err = l.x.node(k); err != nil {
		return err
	}
	l.out = append(l.out, value.Entry{Key: k, Value: v})
	return nil
}

// lend puts every entry of the map alias names, for the merge key at
// index at.
func (l *legacyMerge) lend(at int, alias *value.Value) error {
	src, ok := l.x.target(alias)
	if !ok {
		return nil
	}
	if src.Kind() != value.KindObject {
		return mergeError(src)
	}
	for _, e := range src.Entries() {
		if err := l.put(at, e.Key, e.Value); err != nil {
			return err
		}
	}
	return nil
}

// mergeToSpec folds merge keys in as the YAML spec says, which yq does
// under --yaml-fix-merge-anchor-to-spec. Each map a merge names lends the
// entries whose keys the map lacks, and the first map to lend a key wins.
func (x *exploder) mergeToSpec(entries []value.Entry) (*value.Value, error) {
	var out []value.Entry
	has := func(key string) bool {
		for _, set := range [][]value.Entry{entries, out} {
			for _, e := range set {
				if nodeText(e.Key) == key {
					return true
				}
			}
		}
		return false
	}
	for _, e := range entries {
		if !isMergeKey(e.Key, true) {
			ne, err := x.entry(e)
			if err != nil {
				return nil, err
			}
			out = append(out, ne)
			continue
		}
		for _, s := range x.mergeSources(e.Value) {
			s, err := x.node(s)
			if err != nil {
				return nil, err
			}
			if s.Kind() != value.KindObject {
				return nil, mergeError(s)
			}
			for _, se := range s.Entries() {
				if !has(nodeText(se.Key)) {
					out = append(out, se)
				}
			}
		}
	}
	return value.ObjEntries(out...), nil
}

// mergeSources are the maps a merge key's value v names: v itself or the
// node it is an alias of, or each element of that when it is a sequence,
// with each alias among them read as its node.
func (x *exploder) mergeSources(v *value.Value) []*value.Value {
	if t, ok := x.target(v); ok {
		v = t
	}
	srcs := []*value.Value{v}
	if v.Kind() == value.KindArray {
		srcs = v.Elems()
	}
	out := make([]*value.Value, len(srcs))
	for i, s := range srcs {
		if t, ok := x.target(s); ok {
			s = t
		}
		out[i] = s
	}
	return out
}

// target is the node the alias v names, and false when v is not an
// alias or names no anchor the walk has passed.
func (x *exploder) target(v *value.Value) (*value.Value, bool) {
	if !isAlias(v) {
		return nil, false
	}
	t, ok := x.anchors[v.Meta().AliasOf]
	return t, ok
}

// isAlias reports whether v is an alias.
func isAlias(v *value.Value) bool {
	m := v.Meta()
	return m != nil && m.AliasOf != ""
}

// mergeError is yq's error for a merge of something that is not a map.
func mergeError(v *value.Value) error {
	return &query.RuntimeError{Msg: fmt.Sprintf("can only use merge anchors with maps (!!map) or sequences (!!seq) of maps, but got sequence containing %v", tagOf(v))}
}

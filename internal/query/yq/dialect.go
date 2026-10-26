package yq

import (
	"iter"
	"math"
	"strconv"
	"strings"

	"github.com/fivetwenty-io/graft/internal/query/builtin"
	"github.com/fivetwenty-io/graft/internal/query/jq"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// Options configure the yq dialect.
type Options struct {
	// ReadOnly stops an assignment from creating the keys and elements
	// its left side names but the input lacks.
	ReadOnly bool
	// Caps are the capabilities yq's --security-* flags leave on.
	Caps runtime.Capabilities
	// MergeAnchorToSpec, StringInterpolation, and FloatNumbers mirror
	// yq's flags of the same names for the operators that read them.
	MergeAnchorToSpec   bool
	StringInterpolation bool
	FloatNumbers        bool
}

// Dialect implements runtime.Dialect for yq. Where yq behaves as jq does,
// the embedded jq dialect answers.
type Dialect struct {
	*jq.Dialect
	opts Options
}

var _ runtime.Dialect = (*Dialect)(nil)

// New returns the yq dialect with opts.
func New(opts Options) *Dialect {
	return &Dialect{Dialect: jq.New(), opts: opts}
}

// Name returns "yq".
func (*Dialect) Name() string { return "yq" }

// Builtins returns the registry of yq's natives and the prelude, which is
// built once and shared.
func (*Dialect) Builtins() *runtime.Registry { return registry() }

// Capabilities returns the capabilities the options leave on.
func (d *Dialect) Capabilities() runtime.Capabilities { return d.opts.Caps }

// Truthy is false for null and false, and for a scalar tagged !!null, or
// tagged !!bool with any text but yq's spellings of true. An alias is
// true, as yq does not look through it.
func (*Dialect) Truthy(v *value.Value) bool {
	m := v.Meta()
	if m != nil && m.AliasOf != "" {
		return true
	}
	switch v.Kind() {
	case value.KindNull, value.KindFalse:
		return false
	case value.KindString:
		if m == nil {
			return true
		}
		switch m.Tag {
		case "!!null":
			return false
		case "!!bool":
			switch strings.ToLower(v.Str()) {
			case "y", "yes", "on", "true":
				return true
			}
			return false
		}
	default:
	}
	return true
}

// Index is yq's .[key]. A key that is missing from a map, or an index
// past the end of a sequence, gives null, which an assignment then
// creates unless the dialect is read-only. The right side of "=" reads
// a map without creating keys, so a key missing from a map or from null
// gives no output there, while an index past the end still reads as
// null. A scalar has no children, so
// indexing one gives no output. A sequence takes an integer, or a string
// that spells one, and counts a negative index from the end.
func (d *Dialect) Index(ctx *runtime.Ctx, c, key *value.Value, mode runtime.IndexMode) (*value.Value, error) {
	if _, _, ok := value.IsSliceComponent(key); ok {
		return builtin.Index(ctx, c, key)
	}
	var out *value.Value
	var err error
	switch c.Kind() {
	case value.KindObject:
		out, err = d.mapIndex(ctx, c, key)
	case value.KindArray:
		out, err = seqIndex(ctx, c, key)
	case value.KindNull:
		out, err = builtin.Index(ctx, c, key)
	default:
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if out.Kind() != value.KindNull || present(c, key) {
		return out, nil
	}
	if mode == runtime.IndexSource && (c.Kind() == value.KindObject || c.Kind() == value.KindNull && key.Kind() != value.KindNumber) {
		return nil, nil
	}
	if d.opts.ReadOnly && mode == runtime.IndexUpdate {
		return nil, nil
	}
	return out, nil
}

// mapIndex finds key in a map. A number finds the entry whose key spells
// it, as yq keeps keys that are numbers.
func (*Dialect) mapIndex(ctx *runtime.Ctx, c, key *value.Value) (*value.Value, error) {
	if key.Kind() != value.KindNumber {
		return builtin.Index(ctx, c, key)
	}
	if v, ok := c.Get(numberText(key)); ok {
		return v, nil
	}
	return value.Null(), nil
}

// seqIndex finds the element of c at key.
func seqIndex(ctx *runtime.Ctx, c, key *value.Value) (*value.Value, error) {
	var i int
	switch key.Kind() {
	case value.KindNumber, value.KindString:
		text := key.Str()
		if key.Kind() == value.KindNumber {
			text = numberText(key)
		}
		n, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, &query.RuntimeError{Msg: "cannot index array with '" + text + "' (" + err.Error() + ")"}
		}
		if n > math.MaxInt32 {
			return value.Null(), nil
		}
		i = int(n)
	default:
		return builtin.Index(ctx, c, key)
	}
	if i < 0 {
		i += c.Len()
		if i < 0 {
			return nil, &query.RuntimeError{Msg: "index [" + strconv.Itoa(i-c.Len()) + "] out of range, array size is " + strconv.Itoa(c.Len())}
		}
	}
	if v, ok := c.Index(i); ok {
		return v, nil
	}
	return value.Null(), nil
}

// present reports whether c holds key, so a null it gives is a value
// rather than a gap.
func present(c, key *value.Value) bool {
	switch c.Kind() {
	case value.KindObject:
		text := key.Str()
		if key.Kind() == value.KindNumber {
			text = numberText(key)
		}
		_, ok := c.Get(text)
		return ok
	case value.KindArray:
		i, ok := elementIndex(key, c.Len())
		return ok && i >= 0 && i < c.Len()
	default:
	}
	return false
}

// elementIndex reads key as an index into n elements, counted from the
// end when negative, and reports false for a key that is not an integer.
func elementIndex(key *value.Value, n int) (int, bool) {
	text := key.Str()
	if key.Kind() == value.KindNumber {
		text = numberText(key)
	}
	i, err := strconv.Atoi(text)
	if err != nil {
		return 0, false
	}
	if i < 0 {
		i += n
	}
	return i, true
}

// numberText is a number as yq spells it in a key or an index: the
// literal it was written as, or its shortest decimal text.
func numberText(v *value.Value) string {
	if m := v.Meta(); m != nil && m.Text != "" {
		return m.Text
	}
	if lit := v.Literal(); lit != "" {
		return lit
	}
	return v.Dec().String()
}

// Iterate emits a map's values and a sequence's elements, and nothing for
// a scalar or null.
func (d *Dialect) Iterate(v *value.Value, emit func(key, elem *value.Value) error) error {
	switch v.Kind() {
	case value.KindArray, value.KindObject:
		return d.Dialect.Iterate(v, emit)
	default:
	}
	return nil
}

// Recurse is ".." with includeKeys unset, which visits every node before
// its children without following aliases, since an alias holds only the
// anchor's name. With includeKeys set it is "...", which also emits each
// map key, at its value's path marked with runtime.KeyMark, just before
// the value.
func (*Dialect) Recurse(v *value.Value, includeKeys bool, emit func(*value.Value, value.Path) error) error {
	var walk func(v *value.Value, p value.Path) error
	walk = func(v *value.Value, p value.Path) error {
		if err := emit(v, p); err != nil {
			return err
		}
		switch v.Kind() {
		case value.KindArray:
			for i, e := range v.Elems() {
				if err := walk(e, extend(p, value.Num(float64(i)))); err != nil {
					return err
				}
			}
		case value.KindObject:
			for _, e := range v.Entries() {
				ep := extend(p, e.Key)
				if includeKeys {
					if err := emit(e.Key, extend(ep, runtime.KeyMark)); err != nil {
						return err
					}
				}
				if err := walk(e.Value, ep); err != nil {
					return err
				}
			}
		default:
		}
		return nil
	}
	return walk(v, value.Path{})
}

// extend returns p with c appended in a new array.
func extend(p value.Path, c ...*value.Value) value.Path {
	out := make(value.Path, 0, len(p)+len(c))
	out = append(out, p...)
	return append(out, c...)
}

// Assign runs yq's assignments, which emit one root each. "=" and the
// arithmetic forms run the right side once on the root and use its last
// output, and leave the root as it was when it has none. "|=" runs the
// right side on each path's value, from the last path to the first so a
// child changes before the parent that holds it, and takes the first
// output, leaving a path whose update has none as it was. The value that
// replaces another takes its place's presentation, as place describes.
func (*Dialect) Assign(ctx *runtime.Ctx, root *value.Value, paths []value.Path, rhs func(cur *value.Value, at value.Path) iter.Seq2[*value.Value, error], mode runtime.AssignMode) iter.Seq2[*value.Value, error] {
	return func(yield func(*value.Value, error) bool) {
		if mode.Op() == runtime.AssignUpdate.Op() {
			yield(update(ctx, root, paths, rhs))
			return
		}
		var last *value.Value
		for v, err := range rhs(root, nil) {
			if err != nil {
				yield(nil, err)
				return
			}
			last = v
		}
		if last == nil {
			yield(root, nil)
			return
		}
		yield(setAll(ctx, root, paths, last, mode))
	}
}

func setAll(ctx *runtime.Ctx, root *value.Value, paths []value.Path, v *value.Value, mode runtime.AssignMode) (*value.Value, error) {
	ed := builtin.NewEditor(ctx, root)
	for _, p := range paths {
		cur, err := ed.Get(p)
		if err != nil {
			return nil, err
		}
		nv, err := mode.Apply(cur, v)
		if err != nil {
			return nil, err
		}
		if err := set(ed, p, cur, nv); err != nil {
			return nil, err
		}
	}
	return ed.Value(), nil
}

func update(ctx *runtime.Ctx, root *value.Value, paths []value.Path, rhs func(cur *value.Value, at value.Path) iter.Seq2[*value.Value, error]) (*value.Value, error) {
	ed := builtin.NewEditor(ctx, root)
	var first *value.Value
	var ferr error
	take := func(v *value.Value, err error) bool {
		first, ferr = v, err
		return false
	}
	for i := len(paths) - 1; i >= 0; i-- {
		p := paths[i]
		cur, err := ed.Get(p)
		if err != nil {
			return nil, err
		}
		first, ferr = nil, nil
		rhs(cur, p)(take)
		if ferr != nil {
			return nil, ferr
		}
		if first == nil {
			continue
		}
		if err := set(ed, p, cur, first); err != nil {
			return nil, err
		}
	}
	return ed.Value(), nil
}

// set puts nv at p in place of cur. An empty collection on the way to p
// is about to gain content, and yq writes an empty collection in flow
// style whatever its style, so such a collection loses its flow style
// here and prints in block style once filled, as it does in yq.
func set(ed *builtin.Editor, p value.Path, cur, nv *value.Value) error {
	for i := range p {
		c, err := ed.Get(p[:i])
		if err != nil {
			break
		}
		if c.Len() == 0 && hasFlow(c) {
			m := *c.Meta()
			m.Style &^= value.Flow
			if err := ed.Set(p[:i], c.WithMeta(&m)); err != nil {
				return err
			}
		}
	}
	return ed.Set(p, place(cur, nv))
}

// styles are the style flags that say how a node is written, as against
// Tagged, which goes with the tag.
const styles = value.SingleQuoted | value.DoubleQuoted | value.Literal | value.Folded | value.Flow

// place returns nv as it reads in cur's place, with the style placeStyle
// gives it, and with cur's comments when it has none of its own.
func place(cur, nv *value.Value) *value.Value {
	cm := cur.Meta()
	if cm == nil {
		return nv
	}
	style := placeStyle(cur, nv)
	comments := hasComments(cm) && !hasComments(nv.Meta())
	if style == 0 && !comments {
		return nv
	}
	m := value.Meta{}
	if nm := nv.Meta(); nm != nil {
		m = *nm
	}
	if style != 0 {
		m.Style = m.Style&^styles | style
	}
	if comments {
		m.HeadComment, m.LineComment, m.FootComment = cm.HeadComment, cm.LineComment, cm.FootComment
	}
	return nv.WithMeta(&m)
}

// placeStyle is the style cur was written in when nv keeps it, which a
// node of the same kind, and for a scalar the same tag, does. It is none
// when cur has none or is an empty collection, and nv keeps its own.
func placeStyle(cur, nv *value.Value) value.StyleFlags {
	if hasFlow(cur) && cur.Len() == 0 || kindOf(cur) != kindOf(nv) {
		return 0
	}
	if kindOf(cur) == "scalar" && tagOf(cur) != tagOf(nv) {
		return 0
	}
	return cur.Meta().Style & styles
}

// hasComments reports whether m carries any comment.
func hasComments(m *value.Meta) bool {
	return m != nil && (m.HeadComment != "" || m.LineComment != "" || m.FootComment != "")
}

// hasFlow reports whether v was written in flow style.
func hasFlow(v *value.Value) bool {
	m := v.Meta()
	return m != nil && m.Style&value.Flow != 0
}

// FormatError returns the error's plain message, which the command prints
// in graft's style rather than after yq's "Error:".
func (*Dialect) FormatError(err error) string { return err.Error() }

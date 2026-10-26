package yq

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/internal/yamlnode"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// maxRepeat is the longest string yq's "*" builds by repeating one.
const maxRepeat = 10 * 1024 * 1024

// nullTag and intTag are the tags of null and an integer.
const (
	nullTag = "!!null"
	mapTag  = "!!map"
	seqTag  = "!!seq"
	intTag  = "!!int"
)

// registerMerge wraps the "*" native r already holds with yq's merge. Two
// maps, two sequences, or null and either merge, a null right side
// leaves the left as it is, and a string and an integer repeat the
// string. Two numbers go to the wrapped native, and anything else fails.
// The three-operand form takes the merge flags written after "*".
func registerMerge(r *runtime.Registry) {
	base, ok := r.Lookup("_multiply", 2)
	if !ok {
		panic("yq: _multiply is not registered")
	}
	run := func(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, flags string, emit runtime.Emit) error {
		return args[0].Each(in, func(lv *value.Value, _ value.Path) error {
			return args[1].Each(in, func(rv *value.Value, _ value.Path) error {
				return multiply(ctx, base, lv, rv, flags, emit)
			})
		})
	}
	r.RegisterSingle("_multiply", 2, func(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		return run(ctx, in, args, "", emit)
	})
	r.RegisterSingle("_multiply", 3, func(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		return args[2].Each(in, func(f *value.Value, _ value.Path) error {
			return run(ctx, in, args, f.Str(), emit)
		})
	})
}

// multiply is yq's "*" of l and r under flags.
func multiply(ctx *runtime.Ctx, base runtime.Native, l, r *value.Value, flags string, emit runtime.Emit) error {
	if tagOf(r) == nullTag {
		return emit(l, nil)
	}
	lk, rk := kindOf(l), kindOf(r)
	lnull := tagOf(l) == nullTag
	if (rk == kindMap || rk == kindSeq) && (lk == rk || lnull) {
		out, err := newMerger(flags).node(withComments(l, r), r, nil)
		if err != nil {
			return err
		}
		return emit(out, nil)
	}
	if lk == kindScalar && rk == kindScalar {
		return multiplyScalars(ctx, base, l, r, emit)
	}
	return cannotMultiply(l, r)
}

// multiplyScalars is yq's "*" of two scalars. Two numbers go to base, and
// so does a scalar with a custom tag whose text reads as a number, as
// multiplyCustom describes. A string and an integer, in either order,
// repeat the string.
func multiplyScalars(ctx *runtime.Ctx, base runtime.Native, l, r *value.Value, emit runtime.Emit) error {
	lt, rt := guessTag(l), guessTag(r)
	switch {
	case l.Kind() == value.KindNumber && r.Kind() == value.KindNumber:
		return base(ctx, nil, []runtime.Thunk{fixedThunk{l}, fixedThunk{r}}, emit)
	case numericTag(lt) && numericTag(rt):
		return multiplyCustom(ctx, base, l, r, emit)
	case tagOf(l) == strTag && rt == intTag:
		return repeat(l, l, r, emit)
	case lt == intTag && (tagOf(r) == strTag || customTag(tagOf(r)) && rt == strTag):
		return repeat(l, r, l, emit)
	case customTag(tagOf(l)) && lt == strTag && rt == intTag:
		return unreadable(l, intTag)
	default:
	}
	return cannotMultiply(l, r)
}

// numericTag reports whether t is the tag of a number.
func numericTag(t string) bool { return t == intTag || t == floatTag }

// multiplyCustom multiplies l and r, at least one of which is a scalar
// with a custom tag whose text reads as a number, as those numbers. With
// the custom tag on the left, the product is the left node with the
// product's text, so it keeps its tag, style, anchor, and comments, as
// yq's does. Otherwise it is the number base gives.
func multiplyCustom(ctx *runtime.Ctx, base runtime.Native, l, r *value.Value, emit runtime.Emit) error {
	ln, lok := asNumber(l)
	rn, rok := asNumber(r)
	if !lok || !rok {
		// yq cannot multiply text such as .inf, which reads as a
		// float but not as a number.
		if !lok {
			return unreadable(l, floatTag)
		}
		return unreadable(r, floatTag)
	}
	return base(ctx, nil, []runtime.Thunk{fixedThunk{ln}, fixedThunk{rn}}, func(v *value.Value, p value.Path) error {
		return emit(retagCustom(l, v), p)
	})
}

// retagCustom is v, the result of arithmetic on l, as the left node with
// v's text when l has a custom tag, so it keeps its tag, style, anchor,
// and comments, and v itself otherwise.
func retagCustom(l, v *value.Value) *value.Value {
	if !customTag(tagOf(l)) || v.Kind() != value.KindNumber {
		return v
	}
	m := metaOf(l)
	m.Text = numberText(v)
	switch f := v.Float(); {
	case math.IsInf(f, 1):
		m.Text = "+Inf"
	case math.IsInf(f, -1):
		m.Text = "-Inf"
	default:
	}
	out := v
	if l.Kind() == value.KindString {
		out = value.Str(m.Text)
	}
	return out.WithMeta(&m)
}

// unreadable is the error for a scalar with a custom tag that "*" takes
// as a number but whose text is not one. yq reports what Go makes of the
// text as an int or a float, as tag says.
func unreadable(v *value.Value, tag string) error {
	text := nodeText(v)
	var err error
	if tag == floatTag {
		_, err = strconv.ParseFloat(text, 64)
	} else {
		_, err = strconv.ParseInt(text, 10, 64)
	}
	if err != nil {
		return &query.RuntimeError{Msg: err.Error()}
	}
	return cannotMultiply(v, v)
}

// asNumber is a scalar as the number its text reads as, and reports
// false when its text reads as no number.
func asNumber(v *value.Value) (*value.Value, bool) {
	if v.Kind() == value.KindNumber {
		return v, true
	}
	n, err := scalarOf(nodeText(v))
	if err != nil || n.Kind() != value.KindNumber {
		return nil, false
	}
	f := n.Float()
	return n, !math.IsInf(f, 0) && !math.IsNaN(f)
}

// cannotMultiply is yq's error for operands "*" does not take.
func cannotMultiply(l, r *value.Value) error {
	return &query.RuntimeError{Msg: fmt.Sprintf("cannot multiply %s with %s", tagOf(l), tagOf(r))}
}

// withComments is l as a merge with r starts from it. It takes r's head,
// foot, and leading comments, except that l keeps its own head comment
// and leading content when it has either, and its own foot comment when
// it has one.
func withComments(l, r *value.Value) *value.Value {
	lm, rm := metaOf(l), metaOf(r)
	m := lm
	m.LeadingContent, m.HeadComment, m.FootComment = rm.LeadingContent, rm.HeadComment, rm.FootComment
	if lm.HeadComment != "" || lm.LeadingContent != "" {
		m.HeadComment, m.LeadingContent = lm.HeadComment, lm.LeadingContent
	}
	if lm.FootComment != "" {
		m.FootComment = lm.FootComment
	}
	return withMeta(l, m)
}

// repeat emits the string s repeated as many times as the integer n
// says, as a node in l's place with s's details.
func repeat(l, s, n *value.Value, emit runtime.Emit) error {
	count, err := strconv.Atoi(numberText(n))
	if err != nil {
		return cannotMultiply(l, n)
	}
	if count < 0 {
		return &query.RuntimeError{Msg: fmt.Sprintf("cannot repeat string by a negative number (%d)", count)}
	}
	text := s.Str()
	if count > 0 && len(text) > maxRepeat/count {
		return &query.RuntimeError{Msg: fmt.Sprintf("result of repeating string (%d bytes) by %d would exceed %d bytes", len(text), count, maxRepeat)}
	}
	m := attributes(metaOf(l), s, assignPrefs{})
	out := strings.Repeat(text, count)
	m.Text = ""
	if sm := s.Meta(); sm != nil && sm.Text == text {
		// A string written as it reads keeps that, a tag included,
		// where one computed has no spelling for the emitter to keep.
		m.Text = out
	}
	return emit(withMeta(value.Str(out), m), nil)
}

// merger walks the right side of a merge from the top down, map keys
// included, and applies each node it meets to the left side at the same
// path, as yq's merge does.
type merger struct {
	// appendSeqs is "+", which appends a sequence to the one it meets
	// rather than replacing it.
	appendSeqs bool
	// deep is "d", which merges sequences element by element.
	deep bool
	// noCreate is "?", which leaves out what the left side lacks.
	noCreate bool
	// prefs carry "n", which writes only where the left side is null,
	// and "c", which replaces custom tags.
	prefs assignPrefs
}

func newMerger(flags string) merger {
	return merger{
		appendSeqs: strings.Contains(flags, "+"),
		deep:       strings.Contains(flags, "d"),
		noCreate:   strings.Contains(flags, "?"),
		prefs: assignPrefs{
			onlyNull: strings.Contains(flags, "n"),
			clobber:  strings.Contains(flags, "c"),
		},
	}
}

// node applies r to l, both at path p of the merge, and then each of r's
// children to l's. A sequence is appended under "+", and replaces l when
// it is not merged element by element, as a scalar and an alias always
// do. A map, or a sequence merged element by element, gives l its
// details only. The walk does not enter an alias, or a sequence it does
// not merge element by element.
func (m merger) node(l, r *value.Value, p value.Path) (*value.Value, error) {
	rk := kindOf(r)
	var err error
	switch {
	case m.appendSeqs && rk == kindSeq:
		l, err = m.add(l, r, p)
	case rk == kindSeq && !m.deep, rk == kindScalar, rk == kindAlias:
		l = m.assign(l, r)
	default:
		if !m.prefs.onlyNull || tagOf(l) == nullTag {
			l = updateAttributesFrom(l, r, m.prefs)
		}
	}
	if err != nil || r.Len() == 0 {
		return l, err
	}
	switch {
	case rk == kindMap:
		return m.entries(l, r, p)
	case rk == kindSeq && m.deep:
		return m.elements(l, r, p)
	default:
	}
	return l, nil
}

// assign replaces l with r, keeping the details yq keeps, unless "n"
// leaves an l that is not null as it is.
func (m merger) assign(l, r *value.Value) *value.Value {
	if m.prefs.onlyNull && tagOf(l) != nullTag {
		return l
	}
	return updateFrom(l, r, m.prefs)
}

// add assigns l the sequence r appended to it. A null l takes r, and an l
// that is not a sequence fails.
func (m merger) add(l, r *value.Value, p value.Path) (*value.Value, error) {
	var sum *value.Value
	switch {
	case tagOf(l) == nullTag:
		sum = r
	case kindOf(l) == kindSeq:
		lm := metaOf(l)
		if l.Len() == 0 {
			lm.Style = 0
		}
		elems := append(append([]*value.Value(nil), l.Elems()...), r.Elems()...)
		sum = withMeta(value.Arr(elems...), lm)
	case kindOf(l) == kindAlias:
		return l, nil
	default:
		at := nicePath(p)
		return nil, &query.RuntimeError{Msg: fmt.Sprintf("%s (%s) cannot be added to a %s (%s)", tagOf(r), at, tagOf(l), at)}
	}
	return m.assign(l, sum), nil
}

// entries applies each entry of the map r to l, key first. A key l lacks
// is added with a null value unless "?" is set, and a map that gains its
// first key that way prints in block style. A key takes the details of
// r's. An l that is not a map has nothing to apply them to.
func (m merger) entries(l, r *value.Value, p value.Path) (*value.Value, error) {
	if kindOf(l) != kindMap {
		return l, nil
	}
	lm := metaOf(l)
	es := append([]value.Entry(nil), l.Entries()...)
	for _, e := range r.Entries() {
		i := findEntry(es, e.Key)
		ok := i >= 0
		if !ok && m.noCreate {
			continue
		}
		if !ok {
			if len(es) == 0 {
				lm.Style = 0
			}
			es = append(es, value.Entry{Key: value.Str(nodeText(e.Key)), Value: value.Null()})
			i = len(es) - 1
		}
		es[i].Key = m.assign(es[i].Key, e.Key)
		v, err := m.node(es[i].Value, e.Value, extend(p, e.Key))
		if err != nil {
			return nil, err
		}
		es[i].Value = v
	}
	return withMeta(value.ObjEntries(es...), lm), nil
}

// findEntry is the index of the last entry whose key reads as k's, or -1.
func findEntry(es []value.Entry, k *value.Value) int {
	want := nodeText(k)
	for i := len(es) - 1; i >= 0; i-- {
		if nodeText(es[i].Key) == want {
			return i
		}
	}
	return -1
}

// elements applies each element of the sequence r to l's element at the
// same index, padding l with nulls to reach it. A sequence that gains its
// first element that way prints in block style.
func (m merger) elements(l, r *value.Value, p value.Path) (*value.Value, error) {
	if kindOf(l) != kindSeq {
		return l, nil
	}
	lm := metaOf(l)
	elems := append([]*value.Value(nil), l.Elems()...)
	for i, e := range r.Elems() {
		for len(elems) <= i {
			if len(elems) == 0 {
				lm.Style = 0
			}
			elems = append(elems, value.Null())
		}
		v, err := m.node(elems[i], e, extend(p, value.Num(float64(i))))
		if err != nil {
			return nil, err
		}
		elems[i] = v
	}
	return withMeta(value.Arr(elems...), lm), nil
}

// nicePath is a path as yq's messages write it, such as a.b[0].
func nicePath(p value.Path) string {
	var b strings.Builder
	for i, c := range p {
		switch {
		case c.Kind() == value.KindNumber:
			b.WriteString("[" + numberText(c) + "]")
		case i == 0:
			b.WriteString(nodeText(c))
		default:
			b.WriteString("." + nodeText(c))
		}
	}
	return b.String()
}

// assignPrefs are the choices of yq's assignments.
type assignPrefs struct {
	// keepAnchor keeps the node's anchor, as "=" and "|=" do.
	keepAnchor bool
	// onlyNull writes only to a null node, which merge's "n" asks for.
	onlyNull bool
	// clobber replaces a custom tag, which the "c" flag asks for.
	clobber bool
}

// prefsFor are the choices an assignment operator makes. "=" and "|="
// keep the node's anchor, and "c" after any of them replaces a custom
// tag.
func prefsFor(op string) assignPrefs {
	base, flags, _ := strings.Cut(op, "=")
	return assignPrefs{
		keepAnchor: base == "" || base == "|",
		clobber:    strings.Contains(flags, "c"),
	}
}

// updateFrom is other in cur's place, as yq replaces one node with
// another. The node keeps its position and leading content, its anchor
// when keepAnchor says so, and its custom tag unless clobber says
// otherwise. It keeps its style while it reads as the same type, and
// unless it is an empty collection, and takes every comment other has.
func updateFrom(cur, other *value.Value, p assignPrefs) *value.Value {
	m := metaOf(cur)
	if collection(cur) && cur.Len() == 0 || guessTag(cur) != guessTag(other) {
		m.Style = metaOf(other).Style
	}
	m.Text = nodeText(other)
	if om := other.Meta(); om != nil {
		// A node read from a document keeps the text it was read with,
		// where one the run built takes the text yq writes it with.
		m.Text = om.Text
	}
	if m = attributes(m, other, p); m == (value.Meta{}) && other.Meta() != nil {
		// A node read from a document keeps its Meta, empty as it
		// is, so the empty null of "a:" still prints as nothing.
		return other.WithMeta(&m)
	}
	return withMeta(other, m)
}

// updateAttributesFrom gives cur other's details, as yq's merge does for
// a map. cur keeps its content unless other is a node of another kind, and
// then it is an empty node of other's kind.
func updateAttributesFrom(cur, other *value.Value, p assignPrefs) *value.Value {
	m := metaOf(cur)
	out := cur
	if kindOf(cur) != kindOf(other) {
		m.Text = ""
		switch kindOf(other) {
		case kindMap:
			out = value.ObjEntries()
		case kindSeq:
			out = value.Arr()
		default:
			out = other
		}
	}
	return withMeta(out, attributes(m, other, p))
}

// attributes is m with other's tag, alias, anchor, style, and comments
// as yq's assignments pass them on. A node that keeps its custom tag or
// anchor keeps it, one with no style takes other's, and a comment other
// lacks stays. A node marked as tagged writes the tag it ends up with.
func attributes(m value.Meta, other *value.Value, p assignPrefs) value.Meta {
	om := metaOf(other)
	if p.clobber || !customTag(m.Tag) {
		m.Tag = om.Tag
	}
	m.AliasOf = om.AliasOf
	if !p.keepAnchor {
		m.Anchor = om.Anchor
	}
	if m.Style == 0 {
		m.Style = om.Style
	}
	for _, c := range []struct{ to, from *string }{
		{&m.HeadComment, &om.HeadComment},
		{&m.LineComment, &om.LineComment},
		{&m.FootComment, &om.FootComment},
	} {
		if *c.from != "" {
			*c.to = *c.from
		}
	}
	if m.Style&value.Tagged != 0 && m.Tag == "" {
		m.Tag = tagOf(other)
	}
	return m
}

// guessTag is a node's tag, or for a custom tag the type its text reads
// as, which is the custom tag itself for a collection.
func guessTag(v *value.Value) string {
	t := tagOf(v)
	if !customTag(t) {
		return t
	}
	if kindOf(v) != kindScalar || nodeText(v) == "" {
		return t
	}
	return yamlnode.ShortTag(yamlnode.ResolvePlainTag(nodeText(v)))
}

// customTag reports whether a tag is one of the document's own rather
// than one of YAML's.
func customTag(t string) bool { return t != "" && !strings.HasPrefix(t, "!!") }

// collection reports whether v is a map or a sequence.
func collection(v *value.Value) bool {
	k := kindOf(v)
	return k == kindMap || k == kindSeq
}

// metaOf is a copy of v's details, which are empty for a value the run
// built.
func metaOf(v *value.Value) value.Meta {
	if m := v.Meta(); m != nil {
		return *m
	}
	return value.Meta{}
}

// withMeta is v with the details m, or with none when m is empty.
func withMeta(v *value.Value, m value.Meta) *value.Value {
	if m == (value.Meta{}) {
		if v.Meta() == nil {
			return v
		}
		return v.WithMeta(nil)
	}
	return v.WithMeta(&m)
}

// fixedThunk is an argument that emits one value.
type fixedThunk struct{ v *value.Value }

// Each emits the value.
func (f fixedThunk) Each(_ *value.Value, emit runtime.Emit) error { return emit(f.v, nil) }

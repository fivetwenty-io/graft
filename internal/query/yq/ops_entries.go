package yq

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/fivetwenty-io/graft/internal/query/builtin"
	"github.com/fivetwenty-io/graft/internal/query/jq"
	"github.com/fivetwenty-io/graft/internal/query/parser"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// entriesPrelude puts yq's entry operators in place of the jq prelude's
// definitions of the same names, which would otherwise shadow them.
const entriesPrelude = `
def to_entries: _yq_to_entries;
def from_entries: _yq_from_entries;
def with_entries(f): _yq_with_entries(f);
def pick(keys): _yq_pick(keys);
def del(f): _yq_delpaths([path(f)]);
def collect(f): [f];
`

// registerEntries adds the operators that read and rebuild a node's keys
// and entries.
func registerEntries(r *runtime.Registry) {
	r.RegisterSingle("keys", 0, func(_ *runtime.Ctx, in *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		ks, err := keysOf(in)
		if err != nil {
			return err
		}
		return emitDetached(ks, emit)
	})
	r.RegisterSingle("has", 1, func(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		wanted, err := firstOf(ctx, args[0], in)
		if err != nil {
			return err
		}
		ok, err := has(in, wanted)
		if err != nil {
			return err
		}
		return emit(value.Bool(ok), nil)
	})
	r.Register("_yq_to_entries", 0, func(_ *runtime.Ctx, in *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		es, ok, err := toEntries(in)
		if err != nil || !ok {
			return err
		}
		return emit(es, nil)
	})
	r.RegisterSingle("_yq_from_entries", 0, func(_ *runtime.Ctx, in *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		m, err := fromEntries(in)
		if err != nil {
			return err
		}
		return emit(m, nil)
	})
	r.Register("_yq_with_entries", 1, withEntries)
	r.RegisterSingle("_yq_pick", 1, func(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		keys, err := firstOf(ctx, args[0], in)
		if err != nil {
			return err
		}
		out, err := pick(ctx, in, keys)
		if err != nil {
			return err
		}
		return emit(out, nil)
	})
	r.RegisterSingle("omit", 1, func(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		keys, err := firstOf(ctx, args[0], in)
		if err != nil {
			return err
		}
		return emit(omit(in, keys), nil)
	})
	r.Register("_yq_delpaths", 1, deletePaths)
}

// addEntriesPrelude adds entriesPrelude after jq's prelude, so its
// definitions win. It is fixed source, so a failure to parse is a bug
// and panics.
func addEntriesPrelude(r *runtime.Registry) {
	prog, err := parser.ParseFile(entriesPrelude, preludeFile, jq.Grammar())
	if err != nil {
		panic("yq: entries prelude does not parse: " + err.Error())
	}
	r.AddPrelude(prog)
}

// firstOf is the first output of arg on in, or null when it has none, as
// yq reads an argument it needs one value from.
func firstOf(_ *runtime.Ctx, arg runtime.Thunk, in *value.Value) (*value.Value, error) {
	var first *value.Value
	err := arg.Each(in, func(v *value.Value, _ value.Path) error {
		if first == nil {
			first = v
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if first == nil {
		return value.Null(), nil
	}
	return first, nil
}

// keysOf is a new sequence of a map's keys, each the key node itself in
// the map's order, or of a sequence's indices.
func keysOf(in *value.Value) (*value.Value, error) {
	switch kindOf(in) {
	case kindMap:
		ks := make([]*value.Value, 0, in.Len())
		for _, e := range in.Entries() {
			ks = append(ks, e.Key)
		}
		return value.Arr(ks...), nil
	case kindSeq:
		ks := make([]*value.Value, in.Len())
		for i := range ks {
			ks[i] = value.Num(float64(i))
		}
		return value.Arr(ks...), nil
	default:
	}
	return nil, &query.RuntimeError{Msg: fmt.Sprintf("cannot get keys of %s, keys only works for maps and arrays", tagOf(in))}
}

// has reports whether a map has a key that reads as wanted does, whatever
// its tag, or whether a sequence has the index wanted names, which must
// be tagged as an integer. Anything else has nothing.
func has(in, wanted *value.Value) (bool, error) {
	text := nodeText(wanted)
	switch kindOf(in) {
	case kindMap:
		for _, e := range in.Entries() {
			if nodeText(e.Key) == text {
				return true, nil
			}
		}
	case kindSeq:
		if tagOf(wanted) != intTag {
			return false, nil
		}
		n, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return false, &query.RuntimeError{Msg: err.Error()}
		}
		return n >= 0 && n < int64(in.Len()), nil
	default:
	}
	return false, nil
}

// toEntries is a sequence of one {key, value} map for each entry of a map
// or each element of a sequence, which keeps the input's comments but not
// its style. Null has no entries, and toEntries reports false for it.
func toEntries(in *value.Value) (*value.Value, bool, error) {
	var es []*value.Value
	switch kindOf(in) {
	case kindMap:
		for _, e := range in.Entries() {
			es = append(es, entry(e.Key, e.Value))
		}
	case kindSeq:
		for i, v := range in.Elems() {
			es = append(es, entry(value.Num(float64(i)), v))
		}
	default:
		if tagOf(in) == nullTag {
			return nil, false, nil
		}
		return nil, false, &query.RuntimeError{Msg: tagOf(in) + " has no keys"}
	}
	m := metaOf(in)
	return withMeta(value.Arr(es...), value.Meta{
		LeadingContent: firstLeadingContent(m),
		HeadComment:    m.HeadComment,
		LineComment:    m.LineComment,
		FootComment:    m.FootComment,
	}), true, nil
}

// firstLeadingContent is the leading content a new node copies from m.
// Only a source's first document keeps any, since a later document's is
// just the "---" that parts it from the one before, and yq's new node,
// which belongs to no document, never prints that.
func firstLeadingContent(m value.Meta) string {
	if m.DocIndex > 0 {
		return ""
	}
	return m.LeadingContent
}

// entry is a new {key, value} map.
func entry(k, v *value.Value) *value.Value {
	return value.ObjEntries(value.Entry{Key: value.Str("key"), Value: k}, value.Entry{Key: value.Str("value"), Value: v})
}

// fromEntries builds a map from a sequence of {key, value} maps. The map
// keeps the sequence's details, and a tag it had becomes !!map.
func fromEntries(in *value.Value) (*value.Value, error) {
	if kindOf(in) != kindSeq {
		return nil, &query.RuntimeError{Msg: "from entries only runs against arrays"}
	}
	es := make([]value.Entry, 0, in.Len())
	for i, e := range in.Elems() {
		k, err := entryField(e, "key", i)
		if err != nil {
			return nil, err
		}
		v, err := entryField(e, "value", i)
		if err != nil {
			return nil, err
		}
		es = append(es, value.Entry{Key: k, Value: v})
	}
	m := metaOf(in)
	if m.Tag != "" {
		m.Tag = "!!map"
	}
	return withMeta(value.ObjEntries(es...), m), nil
}

// entryField is the one value of e at the key name, which an entry must
// have exactly once.
func entryField(e *value.Value, name string, at int) (*value.Value, error) {
	var found *value.Value
	n := 0
	if kindOf(e) == kindMap {
		for _, en := range e.Entries() {
			if nodeText(en.Key) == name {
				found = en.Value
				n++
			}
		}
	}
	if n != 1 {
		return nil, &query.RuntimeError{Msg: fmt.Sprintf("expected to find one '%s' entry but found %d in position %d", name, n, at)}
	}
	return found, nil
}

// withEntries runs f on each entry of the input, collects what it gives
// into a new sequence that keeps the input's head, foot, and leading
// comments, and emits the map that sequence's entries build.
func withEntries(_ *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
	es, ok, err := toEntries(in)
	if err != nil || !ok {
		return err
	}
	var out []*value.Value
	for _, e := range es.Elems() {
		err := args[0].Each(e, func(v *value.Value, _ value.Path) error {
			out = append(out, v)
			return nil
		})
		if err != nil {
			return err
		}
	}
	m := metaOf(es)
	collected := withMeta(value.Arr(out...), value.Meta{
		LeadingContent: m.LeadingContent,
		HeadComment:    m.HeadComment,
		FootComment:    m.FootComment,
	})
	built, err := fromEntries(collected)
	if err != nil {
		return err
	}
	return emitDetached(built, emit)
}

// pick keeps the entries of a map whose keys keys names, or the elements
// of a sequence at the indices it names, in the order keys gives them,
// and drops the rest. A key names an entry whose key reads the same with
// the same tag.
func pick(ctx *runtime.Ctx, in, keys *value.Value) (*value.Value, error) {
	switch kindOf(in) {
	case kindMap:
		var es []value.Entry
		for _, k := range listed(keys) {
			for _, e := range in.Entries() {
				if sameKey(e.Key, k) {
					es = append(es, e)
					break
				}
			}
		}
		return withMeta(value.ObjEntries(es...), metaOf(in)), nil
	case kindSeq:
		var out []*value.Value
		for _, k := range listed(keys) {
			i, err := strconv.Atoi(nodeText(k))
			if err != nil {
				return nil, &query.RuntimeError{Msg: "cannot index array with " + nodeText(k)}
			}
			if v, ok := in.Index(i); ok && i >= 0 {
				out = append(out, v)
			}
		}
		return withMeta(value.Arr(out...), metaOf(in)), nil
	default:
	}
	var p value.Path
	if c := ctx.Cursor; c != nil {
		rel, _ := nodePath(c)
		p, _ = resolve(c.Root, rel)
	}
	return nil, &query.RuntimeError{Msg: fmt.Sprintf("cannot pick indices from type %s (%s)", tagOf(in), nicePath(p))}
}

// omit drops the entries of a map whose keys keys names, or the elements
// of a sequence at the indices it names as integers. A scalar, or an
// empty list of keys, leaves the input as it is.
func omit(in, keys *value.Value) *value.Value {
	ks := listed(keys)
	if len(ks) == 0 {
		return in
	}
	named := func(k *value.Value) bool {
		for _, o := range ks {
			if sameKey(k, o) {
				return true
			}
		}
		return false
	}
	switch kindOf(in) {
	case kindMap:
		var es []value.Entry
		for _, e := range in.Entries() {
			if !named(e.Key) {
				es = append(es, e)
			}
		}
		return withMeta(value.ObjEntries(es...), metaOf(in))
	case kindSeq:
		var out []*value.Value
		for i, v := range in.Elems() {
			if !named(value.Num(float64(i))) {
				out = append(out, v)
			}
		}
		return withMeta(value.Arr(out...), metaOf(in))
	default:
	}
	return in
}

// listed is the elements of a sequence of keys, and none for anything
// else.
func listed(keys *value.Value) []*value.Value {
	if kindOf(keys) != kindSeq {
		return nil
	}
	return keys.Elems()
}

// sameKey reports whether two nodes read the same with the same tag, as
// yq matches the keys pick and omit name.
func sameKey(a, b *value.Value) bool {
	return nodeText(a) == nodeText(b) && tagOf(a) == tagOf(b)
}

// deletePaths is yq's del, which removes the node at each path its
// argument lists. A number names a map's key by its text. The empty path
// names the input itself, which leaves the output when the input is a
// document's root and otherwise stays, since only its parent loses it.
// A collection that loses its last child takes its key's line comment and
// prints in flow style, as yq moves the comment to stay on that line.
func deletePaths(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
	return args[0].Each(in, func(list *value.Value, _ value.Path) error {
		var paths []value.Path
		self := false
		for _, p := range list.Elems() {
			if p.Len() == 0 {
				self = true
				continue
			}
			paths = append(paths, keyedByText(in, p.Elems()))
		}
		out, err := builtin.DelPaths(ctx, in, paths)
		if err != nil {
			return err
		}
		if self && atRoot(ctx) {
			return nil
		}
		return emit(emptiedKeepComment(in, out), nil)
	})
}

// keyedByText is p with each number that indexes a map along the way
// replaced by its text, which is how a map holds that key.
func keyedByText(root *value.Value, p []*value.Value) value.Path {
	out := make(value.Path, len(p))
	v := root
	for i, k := range p {
		if v != nil && v.Kind() == value.KindObject && k.Kind() == value.KindNumber {
			k = value.Str(numberText(k))
		}
		out[i] = k
		next, ok := childOf(v, k)
		if !ok {
			next = nil
		}
		v = next
	}
	return out
}

// childOf is the child of v that k names, if v holds one.
func childOf(v, k *value.Value) (*value.Value, bool) {
	switch {
	case v == nil:
		return nil, false
	case v.Kind() == value.KindObject && k.Kind() == value.KindString:
		return v.Get(k.Str())
	case v.Kind() == value.KindArray && k.Kind() == value.KindNumber:
		if i, ok := elementIndex(k, v.Len()); ok {
			return v.Index(i)
		}
	default:
	}
	return nil, false
}

// atRoot reports whether the cursor is on a document's root.
func atRoot(ctx *runtime.Ctx) bool {
	c := ctx.Cursor
	if c == nil {
		return true
	}
	rel, _ := nodePath(c)
	return len(rel) == 0
}

// emptiedKeepComment walks out against before and gives each map value
// that a deletion emptied its key's line comment, in flow style, when it
// has no line comment of its own. A sequence's elements are walked only
// when it kept all of them, since otherwise they no longer line up.
func emptiedKeepComment(before, out *value.Value) *value.Value {
	switch {
	case out == before:
	case kindOf(out) == kindMap && kindOf(before) == kindMap:
		return emptiedEntries(before, out)
	case kindOf(out) == kindSeq && kindOf(before) == kindSeq && out.Len() == before.Len():
		return emptiedElems(before, out)
	default:
	}
	return out
}

// emptiedEntries is emptiedKeepComment for two maps.
func emptiedEntries(before, out *value.Value) *value.Value {
	es := out.Entries()
	var changed []value.Entry
	for i, e := range es {
		old, ok := before.Get(nodeText(e.Key))
		if !ok || old == e.Value {
			continue
		}
		k, v := movedLineComment(e.Key, old, emptiedKeepComment(old, e.Value))
		if k != e.Key || v != e.Value {
			if changed == nil {
				changed = slices.Clone(es)
			}
			changed[i] = value.Entry{Key: k, Value: v}
		}
	}
	if changed == nil {
		return out
	}
	return withMeta(value.ObjEntries(changed...), metaOf(out))
}

// movedLineComment moves k's line comment onto v, in flow style, when v
// is a collection that a deletion emptied from old and has no line
// comment of its own.
func movedLineComment(k, old, v *value.Value) (*value.Value, *value.Value) {
	km, vm := metaOf(k), metaOf(v)
	if !collection(v) || v.Len() > 0 || old.Len() == 0 || vm.LineComment != "" || km.LineComment == "" {
		return k, v
	}
	vm.LineComment, km.LineComment = km.LineComment, ""
	vm.Style |= value.Flow
	return withMeta(k, km), withMeta(v, vm)
}

// emptiedElems is emptiedKeepComment for two sequences of one length.
func emptiedElems(before, out *value.Value) *value.Value {
	elems := out.Elems()
	var changed []*value.Value
	for i, e := range elems {
		if v := emptiedKeepComment(before.Elems()[i], e); v != e {
			if changed == nil {
				changed = slices.Clone(elems)
			}
			changed[i] = v
		}
	}
	if changed == nil {
		return out
	}
	return withMeta(value.Arr(changed...), metaOf(out))
}

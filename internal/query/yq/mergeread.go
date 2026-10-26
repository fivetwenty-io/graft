package yq

import (
	"io"

	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// maxMergeDepth bounds how far a merge key follows merges inside merged
// maps, which keeps an anchor that merges itself from looping.
const maxMergeDepth = 64

// anchorsOf returns the node each anchor marks in the document the run is
// reading, and nil when it has no document. The last run's answer is kept,
// since every lookup in a document asks for the same one.
func (d *Dialect) anchorsOf(ctx *runtime.Ctx) map[string]*value.Value {
	if ctx == nil || ctx.Cursor == nil || ctx.Cursor.Root == nil {
		return nil
	}
	root := ctx.Cursor.Root
	d.anchorMu.Lock()
	defer d.anchorMu.Unlock()
	if d.anchorRoot != root {
		x := &exploder{anchors: map[string]*value.Value{}}
		x.notes(root)
		d.anchorRoot, d.anchorSet = root, x.anchors
	}
	return d.anchorSet
}

// deref is the node alias v names, or v when it is not an alias or its
// anchor is not in the document.
func (d *Dialect) deref(ctx *runtime.Ctx, v *value.Value) *value.Value {
	if !isAlias(v) {
		return v
	}
	if t, ok := d.anchorsOf(ctx)[v.Meta().AliasOf]; ok {
		return t
	}
	return v
}

// mergedEntries reads the map c the way yq reads it, with the maps its
// merge keys name lending their entries. It reports false for a map with
// no merge key, which reads as it is.
func (d *Dialect) mergedEntries(ctx *runtime.Ctx, c *value.Value) ([]value.Entry, bool) {
	if _, ok := c.Get("<<"); !ok {
		return nil, false
	}
	set := &entrySet{at: map[string]int{}}
	d.fillEntries(ctx, set, c, 0)
	d.warnMerge(ctx)
	return set.list, true
}

// warnMerge writes the warning for folding a merge key in yq's default
// way, once for each dialect.
func (d *Dialect) warnMerge(ctx *runtime.Ctx) {
	if d.opts.MergeAnchorToSpec || ctx.Stderr == nil || !d.warned.CompareAndSwap(false, true) {
		return
	}
	_, _ = io.WriteString(ctx.Stderr, mergeWarning)
}

// entrySet is an ordered map from key text to entry. Setting a key it
// holds replaces the value where the key sits.
type entrySet struct {
	list []value.Entry
	at   map[string]int
}

func (s *entrySet) set(e value.Entry) {
	k := nodeText(e.Key)
	if i, ok := s.at[k]; ok {
		s.list[i].Value = e.Value
		return
	}
	s.at[k] = len(s.list)
	s.list = append(s.list, e)
}

// fillEntries sets the entries of c in set. By default yq goes through
// them in order, so a merge key overrides the keys before it and the keys
// after it override the merge. Under the specification the merges go first,
// from the last named map to the first so that the first wins, and then
// the map's own keys override them.
func (d *Dialect) fillEntries(ctx *runtime.Ctx, set *entrySet, c *value.Value, depth int) {
	if depth > maxMergeDepth {
		return
	}
	toSpec := d.opts.MergeAnchorToSpec
	entries := c.Entries()
	lend := func(e value.Entry) {
		srcs := d.mergeMaps(ctx, e.Value)
		for i := range srcs {
			if toSpec {
				i = len(srcs) - 1 - i
			}
			d.fillEntries(ctx, set, srcs[i], depth+1)
		}
	}
	if toSpec {
		for _, e := range entries {
			if isMergeKey(e.Key, true) {
				lend(e)
			}
		}
	}
	for _, e := range entries {
		switch {
		case !isMergeKey(e.Key, toSpec):
			set.set(e)
		case !toSpec:
			lend(e)
		default:
		}
	}
}

// mergeMaps are the maps a merge key's value v names: v itself or the node
// it aliases, or each element of that when it is a sequence. A node that is
// not a map lends nothing.
func (d *Dialect) mergeMaps(ctx *runtime.Ctx, v *value.Value) []*value.Value {
	v = d.deref(ctx, v)
	srcs := []*value.Value{v}
	if v.Kind() == value.KindArray {
		srcs = v.Elems()
	}
	var out []*value.Value
	for _, s := range srcs {
		if s = d.deref(ctx, s); s.Kind() == value.KindObject {
			out = append(out, s)
		}
	}
	return out
}

// mergedGet finds key among the merged entries of c.
func (d *Dialect) mergedGet(ctx *runtime.Ctx, c, key *value.Value) (v *value.Value, found, merged bool) {
	entries, ok := d.mergedEntries(ctx, c)
	if !ok {
		return nil, false, false
	}
	text := key.Str()
	if key.Kind() == value.KindNumber {
		text = numberText(key)
	}
	for _, e := range entries {
		if nodeText(e.Key) == text {
			return e.Value, true, true
		}
	}
	return nil, false, true
}

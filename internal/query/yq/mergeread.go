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
	set, ok := d.mergedSet(ctx, c)
	if !ok {
		return nil, false
	}
	return set.list, true
}

// mergedSet is mergedEntries with the map each entry comes from.
func (d *Dialect) mergedSet(ctx *runtime.Ctx, c *value.Value) (*entrySet, bool) {
	if _, ok := c.Get("<<"); !ok {
		return nil, false
	}
	set := &entrySet{at: map[string]int{}}
	d.fillEntries(ctx, set, c, 0)
	d.warnMerge(ctx)
	return set, true
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
// holds replaces the value where the key sits. From records the map each
// entry's value comes from.
type entrySet struct {
	list []value.Entry
	from []*value.Value
	at   map[string]int
}

func (s *entrySet) set(e value.Entry, from *value.Value) {
	k := nodeText(e.Key)
	if i, ok := s.at[k]; ok {
		s.list[i].Value = e.Value
		s.from[i] = from
		return
	}
	s.at[k] = len(s.list)
	s.list = append(s.list, e)
	s.from = append(s.from, from)
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
			set.set(e, c)
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

// locate is the path of the map or sequence n under root, found once for
// each document by walking it.
func (d *Dialect) locate(root, n *value.Value) (value.Path, bool) {
	d.anchorMu.Lock()
	defer d.anchorMu.Unlock()
	if d.homeRoot != root || d.homes == nil {
		d.homeRoot, d.homes = root, map[*value.Value]value.Path{}
		var walk func(v *value.Value, p value.Path)
		walk = func(v *value.Value, p value.Path) {
			switch v.Kind() {
			case value.KindArray:
				d.homes[v] = p
				for i, e := range v.Elems() {
					walk(e, append(p[:len(p):len(p)], value.Num(float64(i))))
				}
			case value.KindObject:
				d.homes[v] = p
				for _, e := range v.Entries() {
					walk(e.Value, append(p[:len(p):len(p)], e.Key))
				}
			default:
			}
		}
		walk(root, nil)
	}
	p, ok := d.homes[n]
	return p, ok
}

// placeRoot is the root a path at is under, the document it belongs to, and
// the path below the root. A path kept apart from its document is under the
// input the cursor stands on, which has to hold c where the path says.
func placeRoot(ctx *runtime.Ctx, c *value.Value, at value.Path) (root *value.Value, doc int, rel value.Path, plain, ok bool) {
	root, doc, rel = runtime.Unroot(at)
	if root != nil {
		return root, doc, rel, false, true
	}
	if ctx == nil || ctx.Cursor == nil || ctx.Cursor.Root == nil {
		return nil, 0, nil, true, false
	}
	root = ctx.Cursor.Root
	return root, 0, rel, true, (runtime.Cursor{Root: root, Path: rel}).Node() == c
}

// Place puts a child of a merged map or of an alias where yq finds it,
// which is in the map that holds the entry or in the node the alias names,
// so that an update through the child changes that map.
func (d *Dialect) Place(ctx *runtime.Ctx, c *value.Value, at value.Path, key *value.Value) (value.Path, bool) {
	root, doc, rel, plain, ok := placeRoot(ctx, c, at)
	if !ok {
		return nil, false
	}
	target := d.deref(ctx, c)
	home := rel
	if target != c {
		if home, ok = d.locate(root, target); !ok {
			return nil, false
		}
	}
	child := key
	set, merged := d.mergedSet(ctx, target)
	switch {
	case merged && target.Kind() == value.KindObject:
		if i, found := set.at[keyText(key)]; found {
			child = set.list[i].Key
			if from := set.from[i]; from != target {
				if home, ok = d.locate(root, from); !ok {
					// The entry is in a map outside this input, so it
					// has no path here to update.
					return nil, true
				}
			}
		}
	case target == c:
		return nil, false
	default:
	}
	out := append(home[:len(home):len(home)], child)
	if plain {
		return out, true
	}
	return runtime.Rooted(root, doc, out), true
}

// keyText is the text a map key is kept under for the lookup key.
func keyText(key *value.Value) string {
	if key.Kind() == value.KindNumber {
		return numberText(key)
	}
	return key.Str()
}

// Resolver returns the function that reads an output of a run as a format
// with no aliases does, with every alias replaced by the node its anchor
// marks and every merge key folded into its map. root is the document the
// output belongs to, whose anchors the aliases name. The function keeps
// the anchors of the last root it was given, since the outputs of one
// document all ask for the same ones.
func (d *Dialect) Resolver(stderr io.Writer) func(root, v *value.Value) (*value.Value, error) {
	var (
		last *value.Value
		x    *exploder
	)
	return func(root, v *value.Value) (*value.Value, error) {
		if v == nil {
			return nil, nil
		}
		if root != last || x == nil {
			// Reading the whole document only learns its anchors, so
			// it is silent about the merge keys it folds.
			x = &exploder{anchors: map[string]*value.Value{}, toSpec: d.opts.MergeAnchorToSpec}
			last = root
			if root != nil {
				if _, err := x.node(root); err != nil {
					return nil, err
				}
			}
		}
		if v == root {
			y := &exploder{anchors: map[string]*value.Value{}, toSpec: d.opts.MergeAnchorToSpec, stderr: stderr}
			return y.node(v)
		}
		x.stderr = stderr
		out, err := x.node(v)
		x.stderr = nil
		return out, err
	}
}

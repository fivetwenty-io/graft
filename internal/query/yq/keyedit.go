package yq

import (
	"slices"
	"strings"

	"github.com/fivetwenty-io/graft/internal/query/builtin"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// keyEditor is a builtin.Editor that holds back the renames of map keys,
// so that renaming k keys of a map of n entries copies the map once rather
// than k times. A rename waits in its map's renames until something other
// than a key is read or written, or the edit ends, and then each map with
// waiting renames is set once, the deepest first.
type keyEditor struct {
	ed      *builtin.Editor
	renames map[string]*renames
	order   []string
}

// renames are the keys of one map waiting to be replaced. at finds an
// entry by key text and keys holds the replacement for an entry's key.
type renames struct {
	parent value.Path
	base   *value.Value
	at     map[string]int
	keys   map[int]*value.Value
}

func newKeyEditor(ctx *runtime.Ctx, root *value.Value) *keyEditor {
	return &keyEditor{ed: builtin.NewEditor(ctx, root)}
}

// Get reads p as the edits so far leave it, renames included.
func (k *keyEditor) Get(p value.Path) (*value.Value, error) {
	if err := k.flush(); err != nil {
		return nil, err
	}
	return k.ed.Get(p)
}

// Peek reads p without applying the waiting renames, which only change
// the keys of maps and so leave every value as it reads.
func (k *keyEditor) Peek(p value.Path) (*value.Value, error) {
	return k.ed.Get(p)
}

// Set puts v at p after the waiting renames.
func (k *keyEditor) Set(p value.Path, v *value.Value) error {
	if err := k.flush(); err != nil {
		return err
	}
	return k.ed.Set(p, v)
}

// Done applies the waiting renames and returns the result.
func (k *keyEditor) Done() (*value.Value, error) {
	if err := k.flush(); err != nil {
		return nil, err
	}
	return k.ed.Value(), nil
}

// nodeAt returns the node at p, which for a path ending in KeyMark is the
// map key itself. An array index has no key node to change, so it is read
// as itself and setNodeAt leaves it alone.
func (k *keyEditor) nodeAt(p value.Path) (*value.Value, error) {
	n := len(p)
	if n < 2 || p[n-1] != runtime.KeyMark {
		return k.Get(p)
	}
	r, err := k.renamesOf(p[:n-2])
	if err != nil {
		return nil, err
	}
	if r != nil {
		if i, ok := r.at[nodeText(p[n-2])]; ok {
			return r.keyAt(i), nil
		}
	}
	return p[n-2], nil
}

// setNodeAt puts v at p, replacing the map key itself for a path ending
// in KeyMark.
func (k *keyEditor) setNodeAt(p value.Path, v *value.Value) error {
	n := len(p)
	if n < 2 || p[n-1] != runtime.KeyMark {
		return k.Set(p, v)
	}
	r, err := k.renamesOf(p[:n-2])
	if err != nil || r == nil {
		return err
	}
	i, ok := r.at[nodeText(p[n-2])]
	if !ok {
		return nil
	}
	if r.at[nodeText(r.keyAt(i))] == i {
		delete(r.at, nodeText(r.keyAt(i)))
	}
	r.keys[i] = v
	r.at[nodeText(v)] = i
	return nil
}

// renamesOf is the renames of the map at parent, started when it has
// none, and nil when parent is not a map.
func (k *keyEditor) renamesOf(parent value.Path) (*renames, error) {
	id := pathID(parent)
	if r, ok := k.renames[id]; ok {
		return r, nil
	}
	m, err := k.ed.Get(parent)
	if err != nil {
		return nil, err
	}
	if m.Kind() != value.KindObject {
		return nil, nil //nolint:nilnil // a nil set of renames means there is no map
	}
	r := &renames{parent: parent, base: m, at: map[string]int{}, keys: map[int]*value.Value{}}
	for i, e := range m.Entries() {
		r.at[nodeText(e.Key)] = i
	}
	if k.renames == nil {
		k.renames = map[string]*renames{}
	}
	k.renames[id] = r
	k.order = append(k.order, id)
	return r, nil
}

// keyAt is the key of entry i as the renames leave it.
func (r *renames) keyAt(i int) *value.Value {
	if v, ok := r.keys[i]; ok {
		return v
	}
	return r.base.Entries()[i].Key
}

// flush sets each map with waiting renames, the deepest first so that a
// rename of a map's own key finds the map where the path says.
func (k *keyEditor) flush() error {
	if len(k.order) == 0 {
		return nil
	}
	order := k.order
	slices.SortStableFunc(order, func(a, b string) int {
		return len(k.renames[b].parent) - len(k.renames[a].parent)
	})
	pending := k.renames
	k.renames, k.order = nil, nil
	for _, id := range order {
		r := pending[id]
		if len(r.keys) == 0 {
			continue
		}
		m, err := k.ed.Get(r.parent)
		if err != nil {
			return err
		}
		entries := slices.Clone(m.Entries())
		for i, key := range r.keys {
			entries[i].Key = key
		}
		if err := k.ed.Set(r.parent, value.ObjEntries(entries...).WithMeta(m.Meta())); err != nil {
			return err
		}
	}
	return nil
}

// pathID is a string that tells the paths of one editor apart.
func pathID(p value.Path) string {
	var sb strings.Builder
	for _, c := range p {
		sb.WriteByte(byte('0' + c.Kind()))
		sb.WriteString(nodeText(c))
		sb.WriteByte(0)
	}
	return sb.String()
}

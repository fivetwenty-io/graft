package yaml

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/token"
)

// hookSite is a place in the decode target that an Unmarshaler fills.
// path leads to it from the target, node is the Node that holds its
// value, and typ is the type of the value there.
type hookSite struct {
	path []hookStep
	node *Node
	typ  reflect.Type
}

// stepKind says what one step of a hookSite's path enters.
type stepKind uint8

const (
	stepField stepKind = iota
	stepIndex
	stepKey
)

// hookStep is one step of a path into the decode target. It enters the
// struct field or the sequence item at index, or the map value under
// key. Every pointer the path meets on the way is followed, and filled
// with a new value when it is nil.
type hookStep struct {
	kind  stepKind
	index int
	key   reflect.Value
}

// hookSites finds the places in body, a prepared syntax tree that fills
// a value of type t, that an Unmarshaler fills, and punches a hole at
// each one, so goccy leaves those places alone. It returns the body with
// its holes, which is nil when the root itself is such a place, and the
// sites in document order. owners gives the Node each value came from.
//
// A struct field's pair comes out of its mapping, so goccy leaves the
// field as it was, and a sequence item or a map value becomes a null. A
// null is no site, whether it has no tag or the tag !!null, and goccy
// decodes it as before. The walk
// never changes a node in place, since alias expansion shares a subtree
// among every alias to its anchor. It copies each node on the path to a
// hole and leaves every other subtree shared, as asWritten does.
//
// When more than one field of a struct reads a key, goccy fills each of
// them natively, except a field that a non-inline field one level up
// shadows, which it zeroes. When exactly one field is left, it is the
// key's reader, as when only one field reads the key. When more than one
// is left and one of them can reach an Unmarshaler, goccy would fill
// that field natively and its method would never run, so the walk stops
// at the first such key in document order and returns its refusal.
func hookSites(body ast.Node, t reflect.Type, owners *tokenOwners, opts DecodeOptions) (ast.Node, []hookSite, error) {
	w := &siteWalk{
		owners: owners,
		opts:   opts,
		keys:   &textWalk{structs: make(map[reflect.Type]*structKeys), kinds: make(map[reflect.Type]ownKind)},
		at:     make(map[string]int),
	}
	out, hole, err := w.node(body, t, nil)
	if err != nil {
		return nil, nil, err
	}
	if hole {
		out = nil
	}
	sites := make([]hookSite, 0, len(w.sites))
	for _, s := range w.sites {
		if s.node != nil {
			sites = append(sites, s)
		}
	}
	return out, sites, nil
}

// siteWalk walks a syntax tree and a target type side by side, as
// textWalk does, and collects the hook sites it meets. keys holds the
// struct keys goccy matches fields by, and at holds the index in sites
// of each path seen, so that when a key repeats, only its last site runs.
type siteWalk struct {
	owners *tokenOwners
	opts   DecodeOptions
	keys   *textWalk
	sites  []hookSite
	at     map[string]int
}

// node returns n with holes punched for the hook sites under it, which
// fill a value of type t at path, and reports whether n itself is a hook
// site, which its parent turns into a hole. The error is a key's refusal
// from field.
func (w *siteWalk) node(n ast.Node, t reflect.Type, path []hookStep) (ast.Node, bool, error) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if n == nil || !reachesUnmarshaler(t) {
		return n, false, nil
	}
	if isUnmarshaler(t) {
		owner := w.owners.of(n)
		if owner == nil || nullNode(owner) {
			return n, false, nil
		}
		w.add(hookSite{path: path, node: owner, typ: t})
		return n, true, nil
	}
	// A tag on a collection, such as !!map, stays in the tree, and goccy
	// reads the collection under it.
	tagNode, tagged := n.(*ast.TagNode)
	inner := n
	if tagged {
		inner = tagNode.Value
	}
	var out ast.Node
	var err error
	switch t.Kind() {
	case reflect.Struct:
		out, err = w.structMapping(inner, t, path)
	case reflect.Map:
		out, err = w.typedMapping(inner, t, path)
	case reflect.Slice, reflect.Array:
		out, err = w.sequence(inner, t, path)
	default:
		return n, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if out == inner {
		return n, false, nil
	}
	if tagged {
		c := *tagNode
		c.Value = out
		return &c, false, nil
	}
	return out, false, nil
}

// add records s, in place of an earlier site at the same path, which
// then no longer runs.
func (w *siteWalk) add(s hookSite) {
	id := pathID(s.path)
	if i, ok := w.at[id]; ok {
		w.sites[i].node = nil
	}
	w.at[id] = len(w.sites)
	w.sites = append(w.sites, s)
}

// pathID returns a text that tells path apart from every other path.
func pathID(path []hookStep) string {
	var b strings.Builder
	for _, s := range path {
		switch s.kind {
		case stepField:
			b.WriteString(".")
			b.WriteString(strconv.Itoa(s.index))
		case stepIndex:
			b.WriteString("[")
			b.WriteString(strconv.Itoa(s.index))
			b.WriteString("]")
		case stepKey:
			fmt.Fprintf(&b, "{%#v}", s.key.Interface())
		}
	}
	return b.String()
}

// then returns path with steps after it, in a new slice, since sibling
// paths share their prefix.
func then(path []hookStep, steps ...hookStep) []hookStep {
	out := make([]hookStep, 0, len(path)+len(steps))
	return append(append(out, path...), steps...)
}

// structMapping returns n, a mapping that fills a struct of type t, with
// a hole for each field that is a hook site. A key fills a field when
// exactly one field reads it, as goccy matches keys, and the field's path
// runs through any ,inline structs.
func (w *siteWalk) structMapping(n ast.Node, t reflect.Type, path []hookStep) (ast.Node, error) {
	m, ok := n.(*ast.MappingNode)
	if !ok {
		return n, nil
	}
	keys := w.keys.structKeys(t)
	// pairs stays nil until a pair changes, and then holds every pair kept.
	var pairs []*ast.MappingValueNode
	for i, mv := range m.Values {
		got, hole, err := w.field(mv, keys, t, path)
		if err != nil {
			return nil, err
		}
		if pairs == nil && (hole || got != mv) {
			pairs = append(make([]*ast.MappingValueNode, 0, len(m.Values)), m.Values[:i]...)
		}
		if pairs != nil && !hole {
			pairs = append(pairs, got)
		}
	}
	if pairs == nil {
		return m, nil
	}
	c := *m
	c.Values = pairs
	return &c, nil
}

// field returns the pair mv of a mapping that fills a struct of type t
// at path, with holes punched under its value, and reports whether its
// value is a hook site, so the pair comes out. The key's reader comes
// from reader, which refuses a key that a hook can never see.
func (w *siteWalk) field(mv *ast.MappingValueNode, keys *structKeys, t reflect.Type, path []hookStep) (*ast.MappingValueNode, bool, error) {
	at, shared := reader(t, keys.index[keyString(keyText(mv.Key))])
	if shared {
		return nil, false, sharedHookKeyError(mv.Key, t)
	}
	if at == nil {
		return mv, false, nil
	}
	steps := make([]hookStep, len(at))
	for i, field := range at {
		steps[i] = hookStep{kind: stepField, index: field}
	}
	got, hole, err := w.node(mv.Value, fieldType(t, at), then(path, steps...))
	if err != nil {
		return nil, false, err
	}
	if hole || got == mv.Value {
		return mv, hole, nil
	}
	c := *mv
	c.Value = got
	return &c, false, nil
}

// fieldType returns the type of the field that the field indexes in
// steps lead to from a struct of type t, through any pointers on the way.
func fieldType(t reflect.Type, steps []int) reflect.Type {
	for _, at := range steps {
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		t = t.Field(at).Type
	}
	return t
}

// reader returns the path of the field of a struct of type t that the
// walk enters for a key, among the fields whose paths index holds. One
// path is that field. With more than one, goccy fills every field
// natively except a field it shadows, which it zeroes, and reader
// returns the one field left when it can reach an Unmarshaler. Otherwise
// it returns nil, since the walk has nothing to enter, and it reports
// shared when more than one field is left and one of them can reach an
// Unmarshaler, whose method would then never run.
func reader(t reflect.Type, index [][]int) (at []int, shared bool) {
	if len(index) == 1 {
		return index[0], false
	}
	live, hooked := 0, false
	for _, q := range index {
		if shadowed(q, index) {
			continue
		}
		live++
		at = q
		hooked = hooked || reachesUnmarshaler(fieldType(t, q))
	}
	if live != 1 || !hooked {
		return nil, live > 1 && hooked
	}
	return at, false
}

// shadowed reports whether goccy zeroes the field at path q, among the
// fields whose paths index holds. Once goccy decodes an ,inline struct,
// it zeroes each field of that struct whose key a non-inline field of
// the struct that inlines it also reads. The path of that non-inline
// field is q without its last two steps, plus one step of its own. A
// field two or more ,inline levels below the other is not zeroed.
func shadowed(q []int, index [][]int) bool {
	if len(q) < 2 {
		return false
	}
	for _, r := range index {
		if len(r) == len(q)-1 && slices.Equal(r[:len(r)-1], q[:len(q)-2]) {
			return true
		}
	}
	return false
}

// sharedHookKeyError returns the refusal of key, which more than one
// field of a struct of type t reads, at the key's place. It names only
// the Go type, never the key or its value.
func sharedHookKeyError(key ast.MapKeyNode, t reflect.Type) *DecodeError {
	e := &DecodeError{
		Kind: ErrorKindType,
		Msg:  fmt.Sprintf("a key of Go type %s is read by more than one field, and one of them has an UnmarshalYAML method", t),
	}
	if tk := key.GetToken(); tk != nil && tk.Position != nil {
		e.Line, e.Column = tk.Position.Line, tk.Position.Column
	}
	return e
}

// typedMapping returns n, a mapping that fills a map of type t, with a
// null in place of each value that is a hook site. Each key's Go value
// comes from decoding the key alone into the key type, and a key that
// does not decode leaves its pair alone, for goccy to refuse.
func (w *siteWalk) typedMapping(n ast.Node, t reflect.Type, path []hookStep) (ast.Node, error) {
	m, ok := n.(*ast.MappingNode)
	if !ok {
		return n, nil
	}
	var out *ast.MappingNode
	for i, mv := range m.Values {
		key, ok := w.mapKey(mv.Key, t.Key())
		if !ok {
			continue
		}
		got, hole, err := w.node(mv.Value, t.Elem(), then(path, hookStep{kind: stepKey, key: key}))
		if err != nil {
			return nil, err
		}
		if hole {
			got = nullAt(mv.Value)
		}
		if got == mv.Value {
			continue
		}
		if out == nil {
			c := *m
			c.Values = append([]*ast.MappingValueNode(nil), m.Values...)
			out = &c
		}
		pair := *mv
		pair.Value = got
		out.Values[i] = &pair
	}
	if out == nil {
		return m, nil
	}
	return out, nil
}

// mapKey decodes the key k alone into a value of type kt, after the
// change asWritten makes to the key of a map with string keys, and
// reports whether it decoded.
func (w *siteWalk) mapKey(k ast.MapKeyNode, kt reflect.Type) (reflect.Value, bool) {
	if kt.Kind() == reflect.String && w.keys.kind(kt) == notOwn {
		k = keyText(k)
	}
	var key ast.Node = k
	if explicit, ok := k.(*ast.MappingKeyNode); ok {
		key = explicit.Value
	}
	p := reflect.New(kt)
	if err := decodeBody(key, p.Interface(), w.opts); err != nil {
		return reflect.Value{}, false
	}
	return p.Elem(), true
}

// sequence returns n, a sequence that fills a slice or an array of type
// t, with a null in place of each item that is a hook site. Items past
// the end of an array fill nothing.
func (w *siteWalk) sequence(n ast.Node, t reflect.Type, path []hookStep) (ast.Node, error) {
	seq, ok := n.(*ast.SequenceNode)
	if !ok {
		return n, nil
	}
	var out *ast.SequenceNode
	for i, item := range seq.Values {
		if t.Kind() == reflect.Array && i >= t.Len() {
			break
		}
		got, hole, err := w.node(item, t.Elem(), then(path, hookStep{kind: stepIndex, index: i}))
		if err != nil {
			return nil, err
		}
		if hole {
			got = nullAt(item)
		}
		if got == item {
			continue
		}
		if out == nil {
			c := *seq
			c.Values = append([]ast.Node(nil), seq.Values...)
			out = &c
		}
		out.Values[i] = got
	}
	if out == nil {
		return seq, nil
	}
	return out, nil
}

// nullAt returns a null node at the place of n, the hole goccy decodes in
// n's place.
func nullAt(n ast.Node) ast.Node {
	pos := &token.Position{}
	if tk := n.GetToken(); tk != nil && tk.Position != nil {
		c := *tk.Position
		pos = &c
	}
	return ast.Null(token.New("null", "null", pos))
}

// runHookSites calls the Unmarshaler at each of sites in turn, inside
// root, the decode target's value, and stops at the first failure.
func runHookSites(root reflect.Value, sites []hookSite) error {
	for i := range sites {
		if err := sites[i].fill(root, sites[i].path); err != nil {
			return err
		}
	}
	return nil
}

// fill follows path from v, filling each nil pointer on the way, and
// calls the Unmarshaler at its end through the value's address. A map
// value is read out, filled, and stored back. A place the decode cannot
// set, such as one inside an unexported embedded struct, is left alone,
// as goccy leaves it.
func (s *hookSite) fill(v reflect.Value, path []hookStep) error {
	v, ok := settle(v)
	if !ok {
		return nil
	}
	if len(path) == 0 {
		if !v.CanSet() {
			return nil
		}
		u, ok := v.Addr().Interface().(Unmarshaler)
		if !ok {
			return nil
		}
		return callUnmarshaler(u, s.node, s.typ)
	}
	step, rest := path[0], path[1:]
	switch step.kind {
	case stepField:
		return s.fill(v.Field(step.index), rest)
	case stepIndex:
		if step.index >= v.Len() {
			return nil
		}
		return s.fill(v.Index(step.index), rest)
	default:
		if !v.CanSet() {
			return nil
		}
		if v.IsNil() {
			v.Set(reflect.MakeMap(v.Type()))
		}
		item := reflect.New(v.Type().Elem()).Elem()
		if old := v.MapIndex(step.key); old.IsValid() {
			item.Set(old)
		}
		err := s.fill(item, rest)
		v.SetMapIndex(step.key, item)
		return err
	}
}

// settle follows v through its pointers, filling each nil one with a new
// value, and reports false when it meets a nil pointer it cannot set.
func settle(v reflect.Value) (reflect.Value, bool) {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			if !v.CanSet() {
				return v, false
			}
			v.Set(reflect.New(v.Type().Elem()))
		}
		v = v.Elem()
	}
	return v, true
}

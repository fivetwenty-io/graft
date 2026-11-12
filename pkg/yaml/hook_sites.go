package yaml

import (
	"fmt"
	"hash/maphash"
	"math"
	"math/cmplx"
	"reflect"
	"slices"

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
//
// The step a typed mapping makes for one of its keys also serves as that
// key's entry in the mapping's key tree, which keySlot describes. A path
// that copies the step carries the entry along, and nothing reads it.
type hookStep struct {
	kind  stepKind
	index int
	key   reflect.Value
	slot  keySlot
}

// keySlot is the entry of a typed mapping's key in the tree typedMapping
// keeps of them. The tree orders its entries by hash, the hash of the
// key, and holds one entry for each Go key whose pairs recorded sites.
// first and end bound the sites that the latest of those pairs recorded,
// as indexes into siteWalk.sites. The entry lives in the pair's path, so
// the tree costs the walk no allocation of its own.
type keySlot struct {
	hash        uint64
	left, right *hookStep
	first, end  int
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
		seed:   maphash.MakeSeed(),
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
// struct keys goccy matches fields by, and seed hashes the keys of typed
// mappings.
//
// When a later pair of a mapping fills the place an earlier pair filled,
// goccy keeps only the later value, so every site the earlier pair
// recorded is canceled. A site is canceled by clearing its node, and
// the holes stay as they are, so goccy decodes the same body either way.
type siteWalk struct {
	owners *tokenOwners
	opts   DecodeOptions
	keys   *textWalk
	sites  []hookSite
	seed   maphash.Seed
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

// add records s. A site that a later pair replaces is canceled where
// the later pair is walked, as typedMapping and field describe, since
// only the decoded key tells which places two pairs share.
func (w *siteWalk) add(s hookSite) {
	w.sites = append(w.sites, s)
}

// cancel clears the sites from index first up to end, which a later pair
// has replaced, so their methods never run.
func (w *siteWalk) cancel(first, end int) {
	for i := first; i < end; i++ {
		w.sites[i].node = nil
	}
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
	from := len(w.sites)
	// pairs stays nil until a pair changes, and then holds every pair kept.
	var pairs []*ast.MappingValueNode
	for i, mv := range m.Values {
		got, hole, err := w.field(mv, keys, t, path, from)
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
//
// The mapping's sites start at index from. An alias can give a key that
// an earlier pair of the mapping already has, and goccy then fills the
// field from the later pair alone. So before a pair's value is walked,
// every site the mapping's earlier pairs recorded is checked, and those
// under the same field are canceled. The check runs for every pair that
// fills a field, whether or not its key repeats, so one mapping costs
// its pairs times the sites recorded under it. Each check compares a few
// field indexes and allocates nothing. The struct type bounds the pairs,
// since goccy fills each field from one pair, but nothing bounds the
// sites under an earlier field.
func (w *siteWalk) field(mv *ast.MappingValueNode, keys *structKeys, t reflect.Type, path []hookStep, from int) (*ast.MappingValueNode, bool, error) {
	at, shared := reader(t, keys.index[keyString(keyText(mv.Key))])
	if shared {
		return nil, false, sharedHookKeyError(mv.Key, t)
	}
	if at == nil {
		return mv, false, nil
	}
	for i := from; i < len(w.sites); i++ {
		if entersField(w.sites[i].path[len(path):], at) {
			w.sites[i].node = nil
		}
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

// entersField reports whether steps, the rest of a site's path past a
// struct, starts with the field indexes in at. Only one key reads the
// field at leads to, and no key reads a field above it or below it.
func entersField(steps []hookStep, at []int) bool {
	if len(steps) < len(at) {
		return false
	}
	for i, field := range at {
		if steps[i].kind != stepField || steps[i].index != field {
			return false
		}
	}
	return true
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
//
// Two keys written differently, such as 1 and 0x1, can decode to one Go
// key, and goccy then keeps the value of the later pair, null or not. So
// the walk keeps a tree of the mapping's keys, and a pair whose key is
// already there cancels every site the earlier pair recorded under it.
//
// goccy gives each key of a map with pointer keys a new pointer, so every
// pair keeps an entry, except that nil keys collide. A hook finds its
// entry by the value the key points to, which fails when two non-nil keys
// point to equal values, as 1 and 0x1 do, since only document order tells
// their entries apart. Such a map is refused when either pair holds a
// hook site, as pointedKey describes.
func (w *siteWalk) typedMapping(n ast.Node, t reflect.Type, path []hookStep) (ast.Node, error) {
	m, ok := n.(*ast.MappingNode)
	if !ok {
		return n, nil
	}
	var out *ast.MappingNode
	var slots *hookStep
	var pointed pointedKeys
	for i, mv := range m.Values {
		key, ok := w.mapKey(mv.Key, t.Key())
		if !ok {
			continue
		}
		at := then(path, hookStep{kind: stepKey, key: key})
		step := &at[len(path)]
		var prior *hookStep
		var link **hookStep
		if comparableKey(key) {
			step.slot.hash = keyHash(w.seed, key)
			prior, link = findSlot(&slots, step)
		}
		if prior != nil {
			w.cancel(prior.slot.first, prior.slot.end)
		}
		first := len(w.sites)
		got, hole, err := w.node(mv.Value, t.Elem(), at)
		if err != nil {
			return nil, err
		}
		switch {
		case prior != nil:
			prior.slot.first, prior.slot.end = first, len(w.sites)
		case link != nil && len(w.sites) > first:
			step.slot.first, step.slot.end = first, len(w.sites)
			*link = step
		}
		if err := w.pointedKey(&pointed, m, mv.Key, key, len(w.sites) > first, t); err != nil {
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

// findSlot looks for an entry with step's key in the key tree at root.
// It returns that entry, or else the link where step's entry belongs.
// Keys that a Go map holds in one slot have the same hash, so a search
// takes the path that the entry of an equal key took when it went in.
func findSlot(root **hookStep, step *hookStep) (*hookStep, **hookStep) {
	link := root
	for entry := *link; entry != nil; entry = *link {
		if entry.slot.hash == step.slot.hash && entry.key.Equal(step.key) {
			return entry, nil
		}
		if step.slot.hash < entry.slot.hash {
			link = &entry.slot.left
		} else {
			link = &entry.slot.right
		}
	}
	return nil, link
}

// pointedKeys holds the values that the non-nil keys of one mapping
// with pointer keys point to, by pointeeHash, each with whether its pair
// recorded a hook site.
type pointedKeys map[uint64][]pointedKey

type pointedKey struct {
	value  reflect.Value
	hooked bool
}

// pointedKey records the value that v, the decoded key node of a pair
// of the mapping m, of a map of type t, points to, with hooked, which
// says whether the pair recorded a hook site. It refuses node when an
// earlier key points to a value alikeValues matches, and either pair
// recorded a site, since no hook could tell the two entries apart. A
// key that is no pointer, or is nil, is left alone.
//
// It refuses as well a key whose repeats OnDuplicateKey folded into its
// pair in m, whether or not the pair recorded a site, since goccy keeps
// an entry for each of them, and the fold has dropped every value but
// the last. A fold in another mapping, whose key an alias key in m
// names, leaves the key alone.
func (w *siteWalk) pointedKey(seen *pointedKeys, m *ast.MappingNode, node ast.MapKeyNode, v reflect.Value, hooked bool, t reflect.Type) error {
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return nil
	}
	if repeat := w.owners.repeatOf(m, node); repeat != nil {
		return &DecodeError{
			Kind: ErrorKindType, Line: repeat.Line, Column: repeat.Column,
			Msg: fmt.Sprintf("a key repeats in a map of Go type %s, which has an UnmarshalYAML method under it", t),
		}
	}
	value := v.Elem()
	h := pointeeHash(w.seed, value)
	for _, p := range (*seen)[h] {
		if (p.hooked || hooked) && alikeValues(p.value, value) {
			return pointerKeyError(node, t)
		}
	}
	if *seen == nil {
		*seen = make(pointedKeys)
	}
	(*seen)[h] = append((*seen)[h], pointedKey{value: value, hooked: hooked})
	return nil
}

// pointerKeyError returns the refusal of key, a key of a map of type t
// that points to a value an earlier key points to as well, at the key's
// place. It names only the Go type, never the key or its value.
func pointerKeyError(key ast.MapKeyNode, t reflect.Type) *DecodeError {
	e := &DecodeError{
		Kind: ErrorKindType,
		Msg:  fmt.Sprintf("two keys of a map of Go type %s point to equal values, and a value under one of them has an UnmarshalYAML method", t),
	}
	if tk := key.GetToken(); tk != nil && tk.Position != nil {
		e.Line, e.Column = tk.Position.Line, tk.Position.Column
	}
	return e
}

// alikeValues reports whether a and b, the values two pointer keys point
// to, are equal or are both NaN, so that the values cannot tell the two
// keys apart.
func alikeValues(a, b reflect.Value) bool {
	return reflect.DeepEqual(a.Interface(), b.Interface()) || valueNaN(a) && valueNaN(b)
}

// valueNaN reports whether v, or the value an interface v holds, is a
// float or a complex number that is NaN.
func valueNaN(v reflect.Value) bool {
	for v.Kind() == reflect.Interface && !v.IsNil() {
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.Float32, reflect.Float64:
		return math.IsNaN(v.Float())
	case reflect.Complex64, reflect.Complex128:
		return cmplx.IsNaN(v.Complex())
	default:
		return false
	}
}

// pointeeHash returns the hash of v, the value a pointer key points to,
// under seed. Values that alikeValues matches hash alike, so every NaN,
// and a value of a kind that keyHash would hash by an address, or by
// nothing, hashes to 0.
func pointeeHash(seed maphash.Seed, v reflect.Value) uint64 {
	if valueNaN(v) {
		return 0
	}
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return 0
		}
		return pointeeHash(seed, v.Elem())
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
		return keyHash(seed, v)
	default:
		return 0
	}
}

// comparableKey reports whether v, a decoded map key, can be compared,
// which it cannot be when an interface in it holds a slice or a map.
// reflect.Value.Comparable answers the same but allocates.
func comparableKey(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Interface:
		return v.IsNil() || comparableKey(v.Elem())
	case reflect.Array:
		for i := range v.Len() {
			if !comparableKey(v.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Struct:
		for i := range v.NumField() {
			if !comparableKey(v.Field(i)) {
				return false
			}
		}
		return true
	case reflect.Slice, reflect.Map, reflect.Func:
		return false
	default:
		return true
	}
}

// keyHash returns the hash of v, a comparable map key, under seed. Keys
// that a Go map holds in one slot hash alike. That includes 0 and -0,
// and an interface holding either, so a key's dynamic type stays out of
// its hash.
func keyHash(seed maphash.Seed, v reflect.Value) uint64 {
	switch v.Kind() {
	case reflect.Bool:
		return maphash.Comparable(seed, v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return maphash.Comparable(seed, v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return maphash.Comparable(seed, v.Uint())
	case reflect.Float32, reflect.Float64:
		return maphash.Comparable(seed, v.Float())
	case reflect.Complex64, reflect.Complex128:
		return maphash.Comparable(seed, v.Complex())
	case reflect.String:
		return maphash.String(seed, v.String())
	case reflect.Pointer, reflect.UnsafePointer, reflect.Chan:
		return maphash.Comparable(seed, v.Pointer())
	case reflect.Interface:
		if v.IsNil() {
			return 0
		}
		return keyHash(seed, v.Elem())
	case reflect.Array:
		var h uint64
		for i := range v.Len() {
			h = h*31 + keyHash(seed, v.Index(i))
		}
		return h
	case reflect.Struct:
		var h uint64
		for i := range v.NumField() {
			h = h*31 + keyHash(seed, v.Field(i))
		}
		return h
	default:
		return 0
	}
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
	if kt.Kind() == reflect.Pointer {
		return w.pointerKey(key, kt)
	}
	p := reflect.New(kt)
	if err := decodeBody(key, p.Interface(), w.opts); err != nil {
		return reflect.Value{}, false
	}
	return p.Elem(), true
}

// pointerKey decodes key into a value of the pointer type kt as goccy
// decodes a map key of that type. A null gives a nil pointer, and any
// other key decodes into a new value of the type kt points to at its
// end, behind as many pointers as kt has.
func (w *siteWalk) pointerKey(key ast.Node, kt reflect.Type) (reflect.Value, bool) {
	if key.Type() == ast.NullType {
		return reflect.Zero(kt), true
	}
	base := kt
	for base.Kind() == reflect.Pointer {
		base = base.Elem()
	}
	v := reflect.New(base)
	if err := decodeBody(key, v.Interface(), w.opts); err != nil {
		return reflect.Value{}, false
	}
	for v.Type() != kt {
		p := reflect.New(v.Type())
		p.Elem().Set(v)
		v = p
	}
	return v, true
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
	var entries pointerEntries
	for i := range sites {
		if err := sites[i].fill(root, sites[i].path, &entries); err != nil {
			return err
		}
	}
	return nil
}

// fill follows path from v, filling each nil pointer on the way, and
// calls the Unmarshaler at its end through the value's address. A map
// value is read out, filled, and stored back. A map with pointer keys
// holds the value under the key goccy made, which entries finds. A
// place the decode cannot set, such as one inside an unexported
// embedded struct, is left alone, as goccy leaves it.
func (s *hookSite) fill(v reflect.Value, path []hookStep, entries *pointerEntries) error {
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
		return s.fill(v.Field(step.index), rest, entries)
	case stepIndex:
		if step.index >= v.Len() {
			return nil
		}
		return s.fill(v.Index(step.index), rest, entries)
	default:
		if !v.CanSet() {
			return nil
		}
		if v.IsNil() {
			v.Set(reflect.MakeMap(v.Type()))
		}
		key := step.key
		if key.Kind() == reflect.Pointer {
			key = entries.find(v, key)
		}
		item := reflect.New(v.Type().Elem()).Elem()
		if old := v.MapIndex(key); old.IsValid() {
			item.Set(old)
		}
		err := s.fill(item, rest, entries)
		v.SetMapIndex(key, item)
		return err
	}
}

// pointerEntries holds, for each map with pointer keys that a site
// reaches, the map's non-nil keys by pointeeHash of the values they
// point to. It is built the first time a site reaches the map, before
// any hook in the map has run.
type pointerEntries struct {
	seed maphash.Seed
	maps map[uintptr]map[uint64][]reflect.Value
}

// find returns the key of the map m that points to a value alikeValues
// matches with the value key points to, or key itself when m holds none,
// or when key is nil. typedMapping refuses a map in which a key whose
// pair holds a site points to the same value as another key, so the key
// found is the one goccy made for the site's pair.
func (e *pointerEntries) find(m, key reflect.Value) reflect.Value {
	if key.IsNil() {
		return key
	}
	if e.maps == nil {
		e.seed = maphash.MakeSeed()
		e.maps = make(map[uintptr]map[uint64][]reflect.Value)
	}
	keys, ok := e.maps[m.Pointer()]
	if !ok {
		keys = make(map[uint64][]reflect.Value, m.Len())
		for it := m.MapRange(); it.Next(); {
			if k := it.Key(); !k.IsNil() {
				h := pointeeHash(e.seed, k.Elem())
				keys[h] = append(keys[h], k)
			}
		}
		e.maps[m.Pointer()] = keys
	}
	for _, k := range keys[pointeeHash(e.seed, key.Elem())] {
		if alikeValues(k.Elem(), key.Elem()) {
			return k
		}
	}
	return key
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

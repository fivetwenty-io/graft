package yq

import (
	"strings"
	"sync"

	"github.com/fivetwenty-io/graft/internal/query/builtin"
	"github.com/fivetwenty-io/graft/internal/query/eval"
	"github.com/fivetwenty-io/graft/internal/query/jq"
	"github.com/fivetwenty-io/graft/internal/query/parser"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// preludeFile is the file name the prelude's $__loc__ reports.
const preludeFile = "<builtin>"

var (
	regOnce sync.Once
	reg     *runtime.Registry
	rootEnv *eval.Env
)

// registry returns the yq registry, which is the core natives, yq's own,
// and jq's prelude, building it and the root environment on first use.
// The prelude is jq source, so it parses under jq's grammar. A prelude
// that fails to parse is a bug, so it panics.
func registry() *runtime.Registry {
	regOnce.Do(func() {
		r := &runtime.Registry{}
		builtin.RegisterCore(r)
		registerYQ(r)
		prog, err := parser.ParseFile(jq.Prelude, preludeFile, jq.Grammar())
		if err != nil {
			panic("yq: prelude does not parse: " + err.Error())
		}
		r.AddPrelude(prog)
		addOps(r)
		addEntriesPrelude(r)
		// The merge wraps the arithmetic addOps tags, so two numbers
		// multiply as yq's numbers do, merge flags or not.
		registerMerge(r)
		reg = r
		rootEnv = eval.NewEnv(r)
	})
	return reg
}

// registerYQ adds the operators that read the cursor, parent, key, and
// path, and the node operators that read and change a node's details.
func registerYQ(r *runtime.Registry) {
	r.Register("key", 0, key)
	r.RegisterSingle("path", 0, func(ctx *runtime.Ctx, _ *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		var p value.Path
		if c := ctx.Cursor; c != nil {
			rel, _ := nodePath(c)
			p, _ = resolve(c.Root, rel)
		}
		return emit(value.Arr(p...), nil)
	})
	registerMeta(r)
	registerPaths(r)
	registerEntries(r)
}

// key emits the key or index of the cursor's node in its parent, and
// nothing at a root or for a key, which has no key of its own. The key
// is a node at its value's path, as in yq, with the details the map
// gives it.
func key(ctx *runtime.Ctx, _ *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
	c := ctx.Cursor
	if c == nil || len(c.Path) == 0 {
		return nil
	}
	if _, isKey := nodePath(c); isKey {
		return nil
	}
	p, _ := resolve(c.Root, c.Path)
	k := p[len(p)-1]
	switch orig := c.Path[len(c.Path)-1]; {
	case orig.Meta() != nil:
		k = orig
	case k.Kind() == value.KindString:
		// A key the path names by text reads as the key the map holds.
		if m, err := builtin.GetPath(ctx, c.Root, p[:len(p)-1]); err == nil {
			if i, ok := entryAt(m, k); ok {
				k = m.Entries()[i].Key
			}
		}
	default:
	}
	return emit(k, runtime.Rooted(c.Root, c.Doc, append(p, runtime.KeyMark)))
}

// nodePath is the path of the cursor's node, which for a map key is its
// value's path, and reports whether the node is a key.
func nodePath(c *runtime.Cursor) (value.Path, bool) {
	if n := len(c.Path); n > 0 && c.Path[n-1] == runtime.KeyMark {
		return c.Path[:n-1], true
	}
	return c.Path, false
}

// resolve walks path from root. It returns the path with each key
// stripped of its presentation details and each negative index counted
// from the end, and reports whether every step names a key or an element
// the value it meets holds.
func resolve(root *value.Value, path value.Path) (value.Path, bool) {
	p := make(value.Path, len(path))
	v, found := root, true
	for i, k := range path {
		if k.Meta() != nil {
			k = k.WithMeta(nil)
		}
		next := value.Null()
		switch v.Kind() {
		case value.KindArray:
			if n, ok := elementIndex(k, v.Len()); ok && n >= 0 {
				k = value.Num(float64(n))
				if e, ok := v.Index(n); ok {
					next = e
					break
				}
			}
			found = false
		case value.KindObject:
			text := k.Str()
			if k.Kind() == value.KindNumber {
				text = numberText(k)
			}
			if e, ok := v.Get(text); ok {
				next = e
				break
			}
			found = false
		default:
			found = false
		}
		p[i], v = k, next
	}
	return p, found
}

// strTag and floatTag are the tags of a plain string and a float.
const (
	strTag   = "!!str"
	floatTag = "!!float"
)

// tagOf is a node's tag: the one written in the input, the one its kind
// implies, or none for an alias.
func tagOf(v *value.Value) string {
	if m := v.Meta(); m != nil {
		if m.AliasOf != "" {
			return ""
		}
		if m.Tag != "" {
			return m.Tag
		}
	}
	switch v.Kind() {
	case value.KindObject:
		return "!!map"
	case value.KindArray:
		return "!!seq"
	case value.KindString:
		return strTag
	case value.KindTrue, value.KindFalse:
		return "!!bool"
	case value.KindNumber:
		if isFloat(v) {
			return floatTag
		}
		return "!!int"
	default:
	}
	return "!!null"
}

// isFloat reports whether a number is a float in YAML's terms, which its
// text decides when it has one and its value decides otherwise.
func isFloat(v *value.Value) bool {
	text := numberText(v)
	lower := strings.ToLower(strings.TrimLeft(text, "+-"))
	if strings.HasPrefix(lower, "0x") || strings.HasPrefix(lower, "0o") || strings.HasPrefix(lower, "0b") {
		return false
	}
	return strings.ContainsAny(lower, ".en")
}

// kindOf is yq's node kind.
func kindOf(v *value.Value) string {
	if m := v.Meta(); m != nil && m.AliasOf != "" {
		return kindAlias
	}
	switch v.Kind() {
	case value.KindObject:
		return "map"
	case value.KindArray:
		return "seq"
	default:
	}
	return kindScalar
}

package yq

import (
	"strings"
	"unicode"

	"github.com/fivetwenty-io/graft/internal/query/builtin"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// substituteHint ends yq's message for an operator given a node that is
// not a string.
const substituteHint = "Hint: Most often you'll want to use '|=' over '=' for this operation"

// registerStrings adds yq's string operators. The regular expression
// operators run on the jq builtin _match_impl, and read a pattern as Go
// does, with ^ and $ at the ends of the subject and offsets counted in
// bytes.
func registerStrings(r *runtime.Registry) {
	r.Register("split", 1, func(_ *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		return args[0].Each(in, func(sep *value.Value, _ value.Path) error {
			switch in.Kind() {
			case value.KindNull:
				return nil
			case value.KindString:
			default:
				return &query.RuntimeError{Msg: "cannot split " + tagOf(in) + ", can only split strings"}
			}
			parts := strings.Split(in.Str(), nodeText(sep))
			out := make([]*value.Value, len(parts))
			for i, s := range parts {
				out[i] = value.Str(s)
			}
			return emit(value.Arr(out...), nil)
		})
	})
	r.RegisterSingle("_yq_join", 1, func(_ *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		return args[0].Each(in, func(sep *value.Value, _ value.Path) error {
			if in.Kind() != value.KindArray {
				return &query.RuntimeError{Msg: "cannot join with " + tagOf(in) + ", can only join arrays of scalars"}
			}
			parts := make([]string, in.Len())
			for i, e := range in.Elems() {
				if e.Kind() != value.KindNull {
					parts[i] = nodeText(e)
				}
			}
			return emit(value.Str(strings.Join(parts, nodeText(sep))), nil)
		})
	})
	stringOp(r, "trim", "cannot trim %s, can only operate on strings. ", strings.TrimSpace)
	stringOp(r, "upcase", "cannot change case with %s, can only operate on strings. ", strings.ToUpper)
	stringOp(r, "downcase", "cannot change case with %s, can only operate on strings. ", strings.ToLower)
	for _, name := range []string{"to_number", "tonumber"} {
		r.RegisterSingle(name, 0, toNumber)
	}
	r.RegisterSingle("type", 0, func(_ *runtime.Ctx, in *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		return emit(value.Str(tagOf(in)), nil)
	})
	for _, name := range []string{"to_string", "tostring"} {
		r.RegisterSingle(name, 0, func(_ *runtime.Ctx, in *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
			return emit(value.Str(stringText(in)), nil)
		})
	}
	builtin.RegisterRegex(r)
	matchImpl, _ := r.Lookup("_match_impl", 3)
	rx := regexOps{impl: matchImpl}
	for arity := 1; arity <= 2; arity++ {
		r.Register("_yq_match", arity, rx.native("match", func(in *value.Value, ms []match, emit runtime.Emit) error {
			for _, m := range ms {
				if err := emit(m.object(in.Str()), nil); err != nil {
					return err
				}
			}
			return nil
		}))
		r.Register("_yq_test", arity, rx.native("match", func(_ *value.Value, ms []match, emit runtime.Emit) error {
			return emit(value.Bool(len(ms) > 0), nil)
		}))
		r.Register("_yq_capture", arity, rx.native("match", func(in *value.Value, ms []match, emit runtime.Emit) error {
			for _, m := range ms {
				if err := emit(m.capture(in.Str()), nil); err != nil {
					return err
				}
			}
			return nil
		}))
	}
	r.Register("sub", 1, rx.sub)
	r.Register("_yq_sub", 2, rx.sub)
}

// stringOp registers a string-to-string operator that fails on any other
// node with msg, which names the node's tag.
func stringOp(r *runtime.Registry, name, msg string, f func(string) string) {
	r.RegisterSingle(name, 0, func(_ *runtime.Ctx, in *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		if in.Kind() != value.KindString {
			return &query.RuntimeError{Msg: strings.Replace(msg, "%s", tagOf(in), 1)}
		}
		return emit(value.Str(f(in.Str())), nil)
	})
}

// toNumber reads a string as the number its text spells, keeping the text,
// and passes a number through.
func toNumber(_ *runtime.Ctx, in *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
	if in.Kind() == value.KindNumber {
		return emit(in, nil)
	}
	text := nodeText(in)
	if in.Kind() == value.KindString && strings.TrimLeftFunc(text, unicode.IsSpace) == text {
		v, err := scalarOf(text)
		if err == nil && v.Kind() == value.KindNumber {
			return emit(v, nil)
		}
	}
	return &query.RuntimeError{Msg: "cannot convert node value [" + text + "] at path  of tag " + tagOf(in) + " to number"}
}

// scalarOf reads text as a plain YAML scalar.
func scalarOf(text string) (*value.Value, error) {
	return parseYAML(text, true)
}

// stringText is a node as tostring gives it: a scalar's text, or a
// collection's YAML.
func stringText(v *value.Value) string {
	if v.Kind() == value.KindArray || v.Kind() == value.KindObject {
		return yamlText(v, 2)
	}
	return nodeText(v)
}

// regexOps runs yq's regular expression operators on jq's matcher.
type regexOps struct {
	impl runtime.Native
}

// group is one capture group of a match, in bytes of the subject.
type group struct {
	start, end int
	matched    bool
	name       string
}

// match is one match and its groups, in bytes of the subject.
type match struct {
	group
	groups []group
}

// object is the match as yq's match gives it.
func (m match) object(subject string) *value.Value {
	captures := make([]*value.Value, len(m.groups))
	for i, g := range m.groups {
		pairs := g.pairs(subject)
		if g.matched && g.name != "" {
			pairs = append(pairs, value.Pair{Key: "name", Value: value.Str(g.name)})
		}
		captures[i] = value.Obj(pairs...)
	}
	return value.Obj(append(m.pairs(subject), value.Pair{Key: "captures", Value: value.Arr(captures...)})...)
}

func (g group) pairs(subject string) []value.Pair {
	if !g.matched {
		return []value.Pair{{Key: "string", Value: value.Null()}, {Key: "offset", Value: value.Num(-1)}, {Key: "length", Value: value.Num(0)}}
	}
	return []value.Pair{
		{Key: "string", Value: value.Str(subject[g.start:g.end])},
		{Key: "offset", Value: value.Num(float64(g.start))},
		{Key: "length", Value: value.Num(float64(g.end - g.start))},
	}
}

// capture is the map of the match's named groups.
func (m match) capture(subject string) *value.Value {
	var pairs []value.Pair
	for _, g := range m.groups {
		if g.name == "" {
			continue
		}
		v := value.Null()
		if g.matched {
			v = value.Str(subject[g.start:g.end])
		}
		pairs = append(pairs, value.Pair{Key: g.name, Value: v})
	}
	return value.Obj(pairs...)
}

// native returns an operator over the matches of a pattern. With one
// argument its outputs are the pattern and then the flags, which is how
// yq reads match("a", "g"), and with two they are the pattern and the
// flags.
func (rx regexOps) native(op string, f func(in *value.Value, ms []match, emit runtime.Emit) error) runtime.Native {
	return func(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		return regexArgs(in, args, func(re, flags *value.Value) error {
			if in.Kind() != value.KindString {
				return &query.RuntimeError{Msg: "cannot " + op + " with " + tagOf(in) + ", can only " + op + " strings. " + substituteHint}
			}
			global := false
			mods := ""
			if flags.Kind() != value.KindNull {
				mods = nodeText(flags)
			}
			if strings.ContainsRune(mods, 'i') {
				return &query.RuntimeError{Msg: "'i' is not a valid option for match. To ignore case, use an expression like match(\"(?i)cat\")"}
			}
			for _, c := range mods {
				if c != 'g' {
					return &query.RuntimeError{Msg: "unrecognised match params '" + string(c) + "', please see docs at https://mikefarah.gitbook.io/yq/operators/string-operators"} //nolint:misspell // yq's own spelling
				}
				global = true
			}
			ms, err := rx.find(ctx, in.Str(), nodeText(re), global)
			if err != nil {
				return err
			}
			return f(in, ms, emit)
		})
	}
}

// regexArgs calls f with each pattern and flags the arguments give.
func regexArgs(in *value.Value, args []runtime.Thunk, f func(re, flags *value.Value) error) error {
	if len(args) == 2 {
		return args[1].Each(in, func(flags *value.Value, _ value.Path) error {
			return args[0].Each(in, func(re *value.Value, _ value.Path) error { return f(re, flags) })
		})
	}
	var outs []*value.Value
	if err := args[0].Each(in, func(v *value.Value, _ value.Path) error {
		outs = append(outs, v)
		return nil
	}); err != nil {
		return err
	}
	switch len(outs) {
	case 0:
		return nil
	case 1:
		return f(outs[0], value.Null())
	default:
	}
	return f(outs[0], outs[1])
}

// sub replaces every match of the pattern with the replacement, in which
// $1, $name, and ${name} stand for groups and $$ for a dollar sign. With
// one argument its outputs are the pattern and then the replacement, and
// with fewer than two the input stays as it is.
func (rx regexOps) sub(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
	apply := func(re, repl *value.Value) error {
		if in.Kind() != value.KindString {
			return &query.RuntimeError{Msg: "cannot substitute with " + tagOf(in) + ", can only substitute strings. " + substituteHint}
		}
		subject := in.Str()
		ms, err := rx.find(ctx, subject, nodeText(re), true)
		if err != nil {
			return err
		}
		var b strings.Builder
		last := 0
		for _, m := range ms {
			b.WriteString(subject[last:m.start])
			expand(&b, nodeText(repl), subject, m)
			last = m.end
		}
		b.WriteString(subject[last:])
		return emit(value.Str(b.String()), nil)
	}
	if len(args) == 2 {
		return args[1].Each(in, func(repl *value.Value, _ value.Path) error {
			return args[0].Each(in, func(re *value.Value, _ value.Path) error { return apply(re, repl) })
		})
	}
	var outs []*value.Value
	if err := args[0].Each(in, func(v *value.Value, _ value.Path) error {
		outs = append(outs, v)
		return nil
	}); err != nil {
		return err
	}
	if len(outs) < 2 {
		return emit(in, nil)
	}
	return apply(outs[0], outs[1])
}

// expand writes template with each group reference replaced by the text
// the group matched, and with nothing for a group that is not there.
func expand(b *strings.Builder, template, subject string, m match) {
	for {
		i := strings.IndexByte(template, '$')
		if i < 0 {
			b.WriteString(template)
			return
		}
		b.WriteString(template[:i])
		template = template[i+1:]
		if strings.HasPrefix(template, "$") {
			b.WriteByte('$')
			template = template[1:]
			continue
		}
		name, rest, ok := refName(template)
		if !ok {
			b.WriteByte('$')
			continue
		}
		template = rest
		if g, ok := m.named(name); ok && g.matched {
			b.WriteString(subject[g.start:g.end])
		}
	}
}

// refName reads the group name after a $, braced or not.
func refName(s string) (name, rest string, ok bool) {
	word := func(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }
	if strings.HasPrefix(s, "{") {
		end := strings.IndexByte(s, '}')
		if end < 2 {
			return "", s, false
		}
		name = s[1:end]
		if strings.IndexFunc(name, func(r rune) bool { return !word(r) }) >= 0 {
			return "", s, false
		}
		return name, s[end+1:], true
	}
	end := strings.IndexFunc(s, func(r rune) bool { return !word(r) })
	if end < 0 {
		end = len(s)
	}
	if end == 0 {
		return "", s, false
	}
	return s[:end], s[end:], true
}

// named finds the group a reference names: a number counts groups from
// the whole match at 0, and anything else is a group's name.
func (m match) named(name string) (group, bool) {
	if n, ok := groupNumber(name); ok {
		if n == 0 {
			return m.group, true
		}
		if n <= len(m.groups) {
			return m.groups[n-1], true
		}
		return group{}, false
	}
	for _, g := range m.groups {
		if g.name == name {
			return g, true
		}
	}
	return group{}, false
}

func groupNumber(s string) (int, bool) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
		if n > 1<<20 {
			return 0, false
		}
	}
	return n, true
}

// find runs the pattern over subject through jq's matcher and turns its
// codepoint offsets into byte offsets.
func (rx regexOps) find(ctx *runtime.Ctx, subject, re string, global bool) ([]match, error) {
	mods := "s"
	if global {
		mods += "g"
	}
	bytesAt := make([]int, 0, len(subject)+1)
	for i := range subject {
		bytesAt = append(bytesAt, i)
	}
	bytesAt = append(bytesAt, len(subject))
	at := func(v *value.Value) int {
		n := int(v.Float())
		if n < 0 || n >= len(bytesAt) {
			return len(subject)
		}
		return bytesAt[n]
	}
	span := func(o *value.Value) group {
		off, _ := o.Get("offset")
		length, _ := o.Get("length")
		str, _ := o.Get("string")
		if off.Float() < 0 || str.Kind() == value.KindNull {
			return group{}
		}
		start := at(off)
		return group{start: start, end: at(value.Num(off.Float() + length.Float())), matched: true}
	}
	var out []match
	args := []runtime.Thunk{constant{value.Str(re)}, constant{value.Str(mods)}, constant{value.Bool(false)}}
	err := rx.impl(ctx, value.Str(subject), args, func(arr *value.Value, _ value.Path) error {
		for _, o := range arr.Elems() {
			m := match{group: span(o)}
			caps, _ := o.Get("captures")
			for _, c := range caps.Elems() {
				g := span(c)
				if n, ok := c.Get("name"); ok && n.Kind() == value.KindString {
					g.name = n.Str()
				}
				m.groups = append(m.groups, g)
			}
			out = append(out, m)
		}
		return nil
	})
	return out, err
}

package builtin

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query/value"
	"github.com/fivetwenty-io/graft/pkg/regex"
)

// RegisterRegex adds _match_impl/3, the native that jq's regex builtins
// are defined over. They read patterns in the Oniguruma flavor, which is
// the engine jq links.
func RegisterRegex(r *runtime.Registry) {
	r.RegisterSingle("_match_impl", 3, matchImpl)
}

// matchImpl matches its input against the pattern in its first argument
// under the flags in its second. When the third argument is true it emits
// whether there is a match, and otherwise it emits an array of jq's match
// objects, one for each match that the flags ask for.
func matchImpl(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
	return cartesian(in, args, func(vals []*value.Value) error {
		re, flags, testMode := vals[0], vals[1], vals[2]
		if in.Kind() != value.KindString {
			return fail("%s cannot be matched, as it is not a string", describe(in))
		}
		if re.Kind() != value.KindString {
			return fail("%s is not a string", describe(re))
		}
		var mods string
		switch flags.Kind() {
		case value.KindNull:
		case value.KindString:
			mods = flags.Str()
		default:
			return fail("%s is not a string", describe(flags))
		}
		compileFlags, opts, err := regexFlags(mods)
		if err != nil {
			return err
		}
		p, err := patterns.get(re.Str(), compileFlags, ctx.RegexBudget)
		if err != nil {
			return fail("Regex failure: %s", strings.TrimPrefix(err.Error(), "regex: "))
		}
		deadline := ctx.Deadline
		if deadline == nil {
			deadline = context.Background()
		}
		subject := in.Str()
		if testMode.Kind() == value.KindTrue {
			opts.Global = false
		}
		var matches []*value.Value
		for m, err := range p.FindAll(deadline, subject, opts) {
			if err != nil {
				return matchError(err, re.Str())
			}
			matches = append(matches, matchObject(m, subject))
		}
		if testMode.Kind() == value.KindTrue {
			return emit(value.Bool(len(matches) > 0), nil)
		}
		return emit(value.Arr(matches...), nil)
	})
}

// regexFlags maps jq's modifier letters onto the engine. g finds every
// match, i ignores case, x is extended syntax, n skips empty matches, s
// keeps ^ and $ at the ends of the subject, m lets . match a newline (what
// Oniguruma calls multiline), l prefers the longest match, and p is s and
// m together.
func regexFlags(mods string) (regex.Flags, regex.MatchOptions, error) {
	var flags regex.Flags
	var opts regex.MatchOptions
	for _, c := range mods {
		switch c {
		case 'g':
			opts.Global = true
		case 'i':
			flags |= regex.IgnoreCase
		case 'x':
			flags |= regex.Extended
		case 'n':
			opts.SkipEmpty = true
		case 's':
			flags |= regex.SingleLine
		case 'm':
			flags |= regex.DotAll
		case 'l':
			flags |= regex.Longest
		case 'p':
			flags |= regex.SingleLine | regex.DotAll
		default:
			return 0, opts, fail("%s is not a valid modifier string", mods)
		}
	}
	return flags, opts, nil
}

// matchError names the pattern whose match ran out of a budget, so the
// step bound reads "regex step budget exhausted for PATTERN". The
// context's own errors pass through, so no try catches them.
func matchError(err error, pattern string) error {
	if !errors.Is(err, regex.ErrBudget) {
		return err
	}
	return fail("regex %s for %s", strings.TrimPrefix(err.Error(), "regex: "), pattern)
}

// The keys that jq's match and capture objects share.
const (
	keyOffset = "offset"
	keyLength = "length"
	keyString = "string"
)

// matchObject builds jq's object for one match, with offsets and lengths
// in codepoints.
func matchObject(m *regex.Match, subject string) *value.Value {
	captures := make([]*value.Value, 0, len(m.Groups)-1)
	for _, g := range m.Groups[1:] {
		captures = append(captures, captureObject(g, subject))
	}
	return value.Obj(
		value.Pair{Key: keyOffset, Value: value.Num(float64(m.RuneStart))},
		value.Pair{Key: keyLength, Value: value.Num(float64(m.RuneEnd - m.RuneStart))},
		value.Pair{Key: keyString, Value: value.Str(m.Text(subject))},
		value.Pair{Key: "captures", Value: value.Arr(captures...)},
	)
}

// captureObject builds jq's object for one group. A group that took no
// part in the match has offset -1 and string null, and jq orders the keys
// of a group that matched nothing differently from one that matched text.
func captureObject(g regex.Group, subject string) *value.Value {
	name := value.Null()
	if g.Name != "" {
		name = value.Str(g.Name)
	}
	if !g.Matched || g.End == g.Start {
		offset, text := value.Num(-1), value.Null()
		if g.Matched {
			offset, text = value.Num(float64(g.RuneStart)), value.Str("")
		}
		return value.Obj(
			value.Pair{Key: keyOffset, Value: offset},
			value.Pair{Key: keyString, Value: text},
			value.Pair{Key: keyLength, Value: value.Num(0)},
			value.Pair{Key: "name", Value: name},
		)
	}
	return value.Obj(
		value.Pair{Key: keyOffset, Value: value.Num(float64(g.RuneStart))},
		value.Pair{Key: keyLength, Value: value.Num(float64(g.RuneEnd - g.RuneStart))},
		value.Pair{Key: keyString, Value: value.Str(g.Text(subject))},
		value.Pair{Key: "name", Value: name},
	)
}

// patternCacheSize is how many compiled patterns the regex builtins keep.
const patternCacheSize = 64

// patterns holds the patterns the regex builtins compiled most recently,
// since a filter usually matches the same pattern against every input.
var patterns = &patternCache{entries: map[patternKey]*cachedPattern{}}

// patternKey is everything that decides how a pattern compiles.
type patternKey struct {
	pattern string
	flags   regex.Flags
	flavor  regex.Flavor
	budget  regex.Budget
}

// patternCache keeps the patternCacheSize patterns used most recently. A
// pattern that fails to compile is not kept.
type patternCache struct {
	mu      sync.Mutex
	clock   uint64
	entries map[patternKey]*cachedPattern
}

type cachedPattern struct {
	p    *regex.Pattern
	used uint64 // the clock at the pattern's last use
}

func (c *patternCache) get(pattern string, flags regex.Flags, budget regex.Budget) (*regex.Pattern, error) {
	key := patternKey{pattern, flags, regex.Oniguruma, budget}
	c.mu.Lock()
	if e, ok := c.entries[key]; ok {
		c.clock++
		e.used = c.clock
		c.mu.Unlock()
		return e.p, nil
	}
	c.mu.Unlock()
	p, err := regex.Compile(pattern, regex.WithFlavor(key.flavor), regex.WithFlags(flags), regex.WithBudget(budget))
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clock++
	if e, ok := c.entries[key]; ok {
		e.used = c.clock
		return e.p, nil
	}
	if len(c.entries) >= patternCacheSize {
		var oldest patternKey
		least := c.clock
		for k, e := range c.entries {
			if e.used < least {
				oldest, least = k, e.used
			}
		}
		delete(c.entries, oldest)
	}
	c.entries[key] = &cachedPattern{p: p, used: c.clock}
	return p, nil
}

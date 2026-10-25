package regex

import (
	"cmp"
	"slices"
	"strings"
	"sync"
	"unicode"
)

// runeRange is an inclusive range of runes.
type runeRange struct {
	lo, hi rune
}

// runeSet is a set of runes held as sorted, disjoint, non-adjacent ranges.
// The functions that build one return it normalized. A set is never changed
// once built, so the named sets below are built once and shared by every
// pattern that uses them.
type runeSet []runeRange

// setCache memoizes the sets a key names. Every key it is given is drawn
// from a fixed vocabulary, such as a Unicode table or a property name the
// tables define, so it never grows past a bound the tables set.
type setCache[K comparable] struct {
	mu   sync.Mutex
	sets map[K]runeSet
}

func (c *setCache[K]) get(key K, build func() runeSet) runeSet {
	c.mu.Lock()
	defer c.mu.Unlock()
	if s, ok := c.sets[key]; ok {
		return s
	}
	if c.sets == nil {
		c.sets = map[K]runeSet{}
	}
	s := build()
	c.sets[key] = s
	return s
}

// normalize sorts the ranges and merges the ones that overlap or touch.
func normalize(rs []runeRange) runeSet {
	if len(rs) == 0 {
		return nil
	}
	slices.SortFunc(rs, func(a, b runeRange) int { return cmp.Compare(a.lo, b.lo) })
	out := rs[:1]
	for _, r := range rs[1:] {
		last := &out[len(out)-1]
		if r.lo <= last.hi+1 {
			last.hi = max(last.hi, r.hi)
			continue
		}
		out = append(out, r)
	}
	return out
}

func union(sets ...runeSet) runeSet {
	var all []runeRange
	for _, s := range sets {
		all = append(all, s...)
	}
	return normalize(all)
}

// negate returns every rune s does not hold.
func (s runeSet) negate() runeSet {
	var out runeSet
	next := rune(0)
	for _, r := range s {
		if r.lo > next {
			out = append(out, runeRange{next, r.lo - 1})
		}
		next = r.hi + 1
	}
	if next <= unicode.MaxRune {
		out = append(out, runeRange{next, unicode.MaxRune})
	}
	return out
}

func (s runeSet) contains(r rune) bool {
	lo, hi := 0, len(s)
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		switch {
		case r < s[m].lo:
			hi = m
		case r > s[m].hi:
			lo = m + 1
		default:
			return true
		}
	}
	return false
}

// single reports whether s holds exactly one rune, and which.
func (s runeSet) single() (rune, bool) {
	if len(s) == 1 && s[0].lo == s[0].hi {
		return s[0].lo, true
	}
	return 0, false
}

func setOf(runes ...rune) runeSet {
	rs := make([]runeRange, len(runes))
	for i, r := range runes {
		rs[i] = runeRange{r, r}
	}
	return normalize(rs)
}

var tableSets setCache[*unicode.RangeTable]

// fromTable returns the runes a Unicode table holds.
func fromTable(t *unicode.RangeTable) runeSet {
	return tableSets.get(t, func() runeSet { return buildFromTable(t) })
}

func buildFromTable(t *unicode.RangeTable) runeSet {
	var rs []runeRange
	for _, r := range t.R16 {
		rs = appendStrided(rs, rune(r.Lo), rune(r.Hi), rune(r.Stride))
	}
	for _, r := range t.R32 {
		//nolint:gosec // Unicode tables hold nothing above unicode.MaxRune
		rs = appendStrided(rs, rune(r.Lo), rune(r.Hi), rune(r.Stride))
	}
	return normalize(rs)
}

func appendStrided(rs []runeRange, lo, hi, stride rune) []runeRange {
	if stride == 1 {
		return append(rs, runeRange{lo, hi})
	}
	for r := lo; r <= hi; r += stride {
		rs = append(rs, runeRange{r, r})
	}
	return rs
}

// orbit returns r and every rune that folds to the same letter under simple
// case folding.
func orbit(r rune) []rune {
	out := []rune{r}
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		out = append(out, f)
	}
	return out
}

// canonicalFold maps r to one representative of its orbit, so two runes
// fold to the same letter exactly when their canonical folds are equal.
func canonicalFold(r rune) rune {
	m := r
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		m = min(m, f)
	}
	return m
}

// foldClosure adds to s every rune in the orbit of a rune s holds. It walks
// the runes whose orbit is wider than themselves that fall in s's ranges, so
// its cost follows how many cased runes s holds rather than how many runes,
// and it allocates only for the partners s lacks.
func (s runeSet) foldClosure() runeSet {
	table := foldTable()
	var extra []runeRange
	for _, rr := range s {
		i, _ := slices.BinarySearchFunc(table, rr.lo, func(e foldEntry, t rune) int { return cmp.Compare(e.r, t) })
		for ; i < len(table) && table[i].r <= rr.hi; i++ {
			for _, f := range table[i].partners[:table[i].n] {
				if !s.contains(f) {
					extra = append(extra, runeRange{f, f})
				}
			}
		}
	}
	if len(extra) == 0 {
		return s
	}
	return union(s, extra)
}

// foldEntry is a rune whose case-fold orbit holds other runes, and those
// runes. No orbit holds more than four runes.
type foldEntry struct {
	r        rune
	n        uint8
	partners [3]rune
}

// foldTable lists every rune with a case-fold partner, in order. Only runes
// that have a case mapping can have one, so the table is built from
// unicode.CaseRanges.
var foldTable = sync.OnceValue(func() []foldEntry {
	var table []foldEntry
	for _, cr := range unicode.CaseRanges {
		lo, hi := rune(cr.Lo), rune(cr.Hi)
		for r := lo; r <= hi; r++ {
			o := orbit(r)
			if len(o) == 1 {
				continue
			}
			e := foldEntry{r: r}
			e.n = uint8(copy(e.partners[:], o[1:]))
			table = append(table, e)
		}
	}
	return table
})

// The ASCII sets the PCRE flavor uses for its escapes and POSIX classes.
var (
	asciiDigit = runeSet{{'0', '9'}}
	asciiUpper = runeSet{{'A', 'Z'}}
	asciiLower = runeSet{{'a', 'z'}}
	asciiAlpha = runeSet{{'A', 'Z'}, {'a', 'z'}}
	asciiAlnum = runeSet{{'0', '9'}, {'A', 'Z'}, {'a', 'z'}}
	asciiWord  = runeSet{{'0', '9'}, {'A', 'Z'}, {'_', '_'}, {'a', 'z'}}
	asciiSpace = runeSet{{'\t', '\r'}, {' ', ' '}}
	asciiPunct = runeSet{{'!', '/'}, {':', '@'}, {'[', '`'}, {'{', '~'}}
	hexDigit   = runeSet{{'0', '9'}, {'A', 'F'}, {'a', 'f'}}
)

// horizontalSpace and verticalSpace are PCRE2's \h and \v.
var (
	horizontalSpace = setOf('\t', ' ', 0xa0, 0x1680, 0x180e, 0x2000, 0x2001, 0x2002, 0x2003, 0x2004,
		0x2005, 0x2006, 0x2007, 0x2008, 0x2009, 0x200a, 0x202f, 0x205f, 0x3000)
	verticalSpace = runeSet{{'\n', '\r'}, {0x85, 0x85}, {0x2028, 0x2029}}
)

type escapeKey struct {
	letter rune
	flavor Flavor
}

var escapeSets setCache[escapeKey]

// escapeClass returns the set a class escape such as \d or \W stands for,
// or nil when letter names none.
func escapeClass(letter rune, flavor Flavor) runeSet {
	if !strings.ContainsRune("dDwWsShHvV", letter) {
		return nil
	}
	return escapeSets.get(escapeKey{letter, flavor}, func() runeSet { return buildEscapeClass(letter, flavor) })
}

func buildEscapeClass(letter rune, flavor Flavor) runeSet {
	var s runeSet
	switch unicode.ToLower(letter) {
	case 'd':
		s = digitClass(flavor)
	case 'w':
		s = wordClass(flavor)
	case 's':
		s = spaceClass(flavor)
	case 'h':
		if flavor == Oniguruma {
			s = hexDigit
		} else {
			s = horizontalSpace
		}
	case 'v':
		s = verticalSpace
	default:
		return nil
	}
	if unicode.IsUpper(letter) {
		s = s.negate()
	}
	return s
}

func digitClass(flavor Flavor) runeSet {
	if flavor == Oniguruma {
		return fromTable(unicode.Nd)
	}
	return asciiDigit
}

func wordClass(flavor Flavor) runeSet {
	if flavor == Oniguruma {
		return unicodeWord()
	}
	return asciiWord
}

var unicodeWord = sync.OnceValue(func() runeSet {
	return union(alphabetic(), fromTable(unicode.M), fromTable(unicode.Nd), fromTable(unicode.Pc))
})

func spaceClass(flavor Flavor) runeSet {
	if flavor == Oniguruma {
		return fromTable(unicode.White_Space)
	}
	return asciiSpace
}

// alphabetic is the Unicode Alphabetic property, which Go's tables hold as
// letters, letter numbers, and Other_Alphabetic.
var alphabetic = sync.OnceValue(func() runeSet {
	return union(fromTable(unicode.L), fromTable(unicode.Nl), fromTable(unicode.Other_Alphabetic))
})

type posixKey struct {
	name                string
	flavor              Flavor
	ignoreCase, negated bool
}

var posixSets setCache[posixKey]

// posixClass returns the set a POSIX bracket name such as alpha stands for,
// or its complement when negated, and false when the name is unknown. Under
// IgnoreCase the PCRE flavor reads upper and lower as every ASCII letter, as
// PCRE2 does, where the Oniguruma flavor folds them with the rest of their
// class.
func posixClass(name string, flavor Flavor, ignoreCase, negated bool) (runeSet, bool) {
	set, ok := namedPosixClass(name, flavor, ignoreCase)
	if !ok {
		return nil, false
	}
	if !negated {
		return set, true
	}
	key := posixKey{name, flavor, ignoreCase, negated}
	return posixSets.get(key, set.negate), true
}

func namedPosixClass(name string, flavor Flavor, ignoreCase bool) (runeSet, bool) {
	if flavor == Oniguruma {
		return unicodePosixClass(name)
	}
	if ignoreCase && (name == "upper" || name == "lower") {
		return asciiAlpha, true
	}
	set, ok := asciiPosixClasses[name]
	return set, ok
}

// asciiPosixClasses gives the POSIX names their ASCII meanings for the PCRE
// flavor.
var asciiPosixClasses = map[string]runeSet{
	"alpha":  asciiAlpha,
	"digit":  asciiDigit,
	"alnum":  asciiAlnum,
	"upper":  asciiUpper,
	"lower":  asciiLower,
	"space":  asciiSpace,
	"blank":  {{'\t', '\t'}, {' ', ' '}},
	"cntrl":  {{0, 0x1f}, {0x7f, 0x7f}},
	"graph":  {{'!', '~'}},
	"print":  {{' ', '~'}},
	"punct":  asciiPunct,
	"word":   asciiWord,
	"xdigit": hexDigit,
	"ascii":  {{0, 0x7f}},
}

// unicodePosixClass gives the POSIX names their Unicode meanings for the
// Oniguruma flavor.
func unicodePosixClass(name string) (runeSet, bool) {
	// Both flavors know the same names.
	if _, known := asciiPosixClasses[name]; !known {
		return nil, false
	}
	return posixSets.get(posixKey{name: name, flavor: Oniguruma}, func() runeSet {
		set, _ := buildUnicodePosixClass(name)
		return set
	}), true
}

func buildUnicodePosixClass(name string) (runeSet, bool) {
	switch name {
	case "alpha":
		return alphabetic(), true
	case "digit":
		return fromTable(unicode.Nd), true
	case "alnum":
		return union(alphabetic(), fromTable(unicode.Nd)), true
	case "upper":
		return union(fromTable(unicode.Lu), fromTable(unicode.Other_Uppercase)), true
	case "lower":
		return union(fromTable(unicode.Ll), fromTable(unicode.Other_Lowercase)), true
	case "space":
		return fromTable(unicode.White_Space), true
	case "blank":
		return union(fromTable(unicode.Zs), setOf('\t')), true
	case "cntrl":
		return fromTable(unicode.Cc), true
	case "graph":
		return graphic(), true
	case "print":
		return union(graphic(), fromTable(unicode.Zs)), true
	case "punct":
		return union(fromTable(unicode.P), setOf('$', '+', '<', '=', '>', '^', '`', '|', '~')), true
	case "word":
		return wordClass(Oniguruma), true
	case "xdigit":
		return hexDigit, true
	case "ascii":
		return runeSet{{0, 0x7f}}, true
	}
	return nil, false
}

// graphic is every assigned rune that is neither a space, a control, nor a
// surrogate.
var graphic = sync.OnceValue(func() runeSet {
	return union(fromTable(unicode.L), fromTable(unicode.M), fromTable(unicode.N),
		fromTable(unicode.P), fromTable(unicode.S), fromTable(unicode.Cf), fromTable(unicode.Co))
})

type propertyKey struct {
	name              string
	caseless, negated bool
}

var propertySets setCache[propertyKey]

// propertyClass returns the set \p{name} stands for, or its complement when
// negated. Names match loosely, as in PCRE2, ignoring case, spaces, hyphens,
// and underscores, and they may name a general category, a script, or a
// binary property. When caseless, as under the PCRE flavor's IgnoreCase, the
// cased-letter categories Lu, Ll, and Lt each stand for all three.
func propertyClass(name string, caseless, negated bool) (runeSet, bool) {
	key := looseName(name)
	if key != "any" && key != "l&" && key != "lc" && propertyTables()[key] == nil {
		return nil, false
	}
	return propertySets.get(propertyKey{key, caseless, negated}, func() runeSet {
		set := namedProperty(key, caseless)
		if negated {
			return set.negate()
		}
		return set
	}), true
}

func namedProperty(key string, caseless bool) runeSet {
	switch key {
	case "any":
		return runeSet{{0, unicode.MaxRune}}
	case "l&", "lc":
		return casedLetters()
	case "lu", "ll", "lt":
		if caseless {
			return casedLetters()
		}
	}
	return fromTable(propertyTables()[key])
}

// propertyTables indexes Go's Unicode tables by their loose names. Where two
// tables share a loose name, a general category wins over a script and a
// script over a binary property.
var propertyTables = sync.OnceValue(func() map[string]*unicode.RangeTable {
	index := map[string]*unicode.RangeTable{}
	for _, tables := range []map[string]*unicode.RangeTable{unicode.Categories, unicode.Scripts, unicode.Properties} {
		for n, t := range tables {
			if _, taken := index[looseName(n)]; !taken {
				index[looseName(n)] = t
			}
		}
	}
	return index
})

var casedLetters = sync.OnceValue(func() runeSet {
	return union(fromTable(unicode.Lu), fromTable(unicode.Ll), fromTable(unicode.Lt))
})

func looseName(name string) string {
	return strings.ToLower(strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '_' {
			return -1
		}
		return r
	}, name))
}

//go:generate go run gen_fold.go

// foldExpansions returns the multi-rune sequences a class holding set also
// matches under full case folding, one for each distinct expansion of a rune
// in set.
func foldExpansions(set runeSet) [][]rune {
	var out [][]rune
	for _, f := range fullFolds {
		if set.contains(f.from) && !slices.ContainsFunc(out, func(e []rune) bool { return foldEqual(e, f.to) }) {
			out = append(out, f.to)
		}
	}
	return out
}

// foldEqual reports whether two rune sequences are equal under simple case
// folding.
func foldEqual(a, b []rune) bool {
	return slices.EqualFunc(a, b, func(x, y rune) bool { return canonicalFold(x) == canonicalFold(y) })
}

// fullFoldOf returns the expansion of r under full case folding, or nil
// when r folds to a single rune.
func fullFoldOf(r rune) []rune {
	i, ok := slices.BinarySearchFunc(fullFolds, r, func(f fullFold, t rune) int { return int(f.from - t) })
	if !ok {
		return nil
	}
	return fullFolds[i].to
}

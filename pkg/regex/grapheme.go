package regex

import "unicode/utf8"

//go:generate go run ./internal/gengrapheme -o grapheme_table.go

// graphemeProp is a rune's grapheme cluster break property, with
// Extended_Pictographic folded in as one more value, since no rune has both.
type graphemeProp uint8

const (
	gbOther graphemeProp = iota
	gbCR
	gbLF
	gbControl
	gbExtend
	gbZWJ
	gbRegionalIndicator
	gbPrepend
	gbSpacingMark
	gbL
	gbV
	gbT
	gbLV
	gbLVT
	gbPictographic
)

// graphemeRange gives the runes lo through hi one property.
type graphemeRange struct {
	lo, hi rune
	prop   graphemeProp
}

// graphemeProperty returns the property of c, Other for a rune the table
// does not list.
func graphemeProperty(c rune) graphemeProp {
	lo, hi := 0, len(graphemeRanges)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		switch r := &graphemeRanges[mid]; {
		case c < r.lo:
			hi = mid
		case c > r.hi:
			lo = mid + 1
		default:
			return r.prop
		}
	}
	return gbOther
}

// graphemeBreak reports whether a grapheme cluster boundary falls at the
// byte offset pos of s, by the rules of Oniguruma 6.9.10, which follow
// UAX #29 for Unicode 16.0 without rule GB9c. It also returns how many
// runes it read behind the pair at pos, which rules GB11 and GB12 look back
// over and the caller charges for.
func graphemeBreak(s string, pos int) (bool, int) {
	if pos == 0 || pos >= len(s) {
		return true, 0 // GB1 and GB2
	}
	prev, w := utf8.DecodeLastRuneInString(s[:pos])
	next, _ := decodeAt(s, pos)
	from, to := graphemeProperty(prev), graphemeProperty(next)
	switch {
	case from == gbCR && to == gbLF:
		return false, 0 // GB3
	case breaksAround(from) || breaksAround(to):
		return true, 0 // GB4 and GB5
	case hangul(from) && hangul(to):
		return !hangulJoins(from, to), 0 // GB6, GB7, and GB8
	case joinsAfter(to) || from == gbPrepend:
		return false, 0 // GB9, GB9a, and GB9b
	case from == gbZWJ && to == gbPictographic:
		return pictographBreak(s, pos-w) // GB11
	case from == gbRegionalIndicator && to == gbRegionalIndicator:
		return regionalBreak(s, pos-w) // GB12 and GB13
	}
	return true, 0 // GB999
}

// breaksAround reports whether p is CR, LF, or Control, which a boundary
// comes before and after.
func breaksAround(p graphemeProp) bool {
	return p == gbCR || p == gbLF || p == gbControl
}

// joinsAfter reports whether p is Extend, ZWJ, or SpacingMark, which no
// boundary comes before.
func joinsAfter(p graphemeProp) bool {
	return p == gbExtend || p == gbZWJ || p == gbSpacingMark
}

// hangul reports whether p is one of the Hangul syllable properties.
func hangul(p graphemeProp) bool {
	return p >= gbL && p <= gbLVT
}

// hangulJoins reports whether rules GB6, GB7, and GB8 keep the Hangul
// syllable parts from and to in one cluster.
func hangulJoins(from, to graphemeProp) bool {
	if from == gbL {
		return to != gbT
	}
	if from == gbLV || from == gbV {
		return to == gbV || to == gbT
	}
	return to == gbT // from is LVT or T
}

// pictographBreak applies rule GB11 to a ZWJ at the byte offset at of s and
// the pictograph after it. The two join when a pictograph comes before the
// ZWJ with nothing but Extend runes between them. It also returns how many
// runes it read.
func pictographBreak(s string, at int) (bool, int) {
	read := 0
	for at > 0 {
		c, w := utf8.DecodeLastRuneInString(s[:at])
		at -= w
		read++
		if prop := graphemeProperty(c); prop == gbPictographic {
			return false, read
		} else if prop != gbExtend {
			break
		}
	}
	return true, read
}

// regionalBreak applies rules GB12 and GB13 to a regional indicator at the
// byte offset at of s and the one after it. Regional indicators pair from
// the start of their run, so a boundary falls between the two when an odd
// number of them come before at. It also returns how many runes it read.
func regionalBreak(s string, at int) (bool, int) {
	read, n := 0, 0
	for at > 0 {
		c, w := utf8.DecodeLastRuneInString(s[:at])
		at -= w
		read++
		if graphemeProperty(c) != gbRegionalIndicator {
			break
		}
		n++
	}
	return n%2 == 1, read
}

// graphemeCluster builds \X the way Oniguruma does, as (?>\O(?:\Y\O)*):
// one rune, then each rune that no cluster boundary comes before, kept
// whole once taken.
func graphemeCluster() *node {
	step := &node{kind: kindConcat, subs: []*node{
		{kind: kindAssert, assert: assertNotGrapheme},
		{kind: kindAny, dotAll: true},
	}}
	body := &node{kind: kindConcat, subs: []*node{
		{kind: kindAny, dotAll: true},
		{kind: kindRepeat, subs: []*node{step}, min: 0, max: unbounded, greedy: true},
	}}
	return &node{kind: kindAtomic, subs: []*node{body}}
}

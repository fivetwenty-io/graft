package regex

import (
	"strings"
	"unicode/utf8"
)

// nodeKind names the kinds of node the parser builds.
type nodeKind uint8

const (
	kindLiteral   nodeKind = iota // a run of runes matched in order
	kindClass                     // one rune from a set, or one of the set's expansions
	kindAny                       // any one rune, a newline only under dotAll
	kindConcat                    // subs in order; no subs matches the empty string
	kindAlternate                 // the first of subs that leads to a match
	kindRepeat                    // subs[0] between min and max times
	kindGroup                     // subs[0], captured as group index
	kindAssert                    // a zero-width test at the current position
	kindLook                      // a lookaround: subs[0] must match, or under neg must not, ahead of or behind the position
	kindAtomic                    // subs[0], keeping only its first way of matching
	kindBackref                   // the text group index captured, folded under fold
	kindKeep                      // \K, which moves the start of the match to the position
	kindCond                      // subs[0] when a group refs names has captured, and subs[1] otherwise
	kindAbsent                    // the absent operator: the runes before the first place subs[0] matches, given back one at a time
	kindCall                      // a call of the group target, group index, or of the whole pattern when index is 0
)

// assertKind names the zero-width tests.
type assertKind uint8

const (
	assertBeginText        assertKind = iota // ^ and \A, the start of the subject
	assertEndText                            // $ and \Z, the end of the subject or before a final newline
	assertEndSubject                         // \z, the end of the subject
	assertWordBoundary                       // \b, between a word rune and a rune that is not one
	assertNotBoundary                        // \B, anywhere \b does not match
	assertBeginLine                          // ^ under (?m), the start of the subject or of a line after a newline
	assertEndLine                            // $ under (?m), the end of the subject or before a newline
	assertGraphemeBoundary                   // \y under Oniguruma, a grapheme cluster boundary
	assertNotGrapheme                        // \Y under Oniguruma, anywhere \y does not match
)

// node is one element of a parsed pattern.
type node struct {
	kind nodeKind
	subs []*node

	// runes is a literal's run, and fold says it matches without regard to
	// case.
	runes []rune
	fold  bool

	// set is a class's runes, and expansions are the multi-rune sequences
	// a class also matches under full case folding, tried after set.
	set        runeSet
	expansions [][]rune

	// dotAll lets an any node match a newline.
	dotAll bool

	// min and max bound a repeat, where max is -1 when unbounded, and
	// greedy says whether it prefers more iterations to fewer.
	min, max int
	greedy   bool

	// index is a group's number and name its name, "" when unnamed. A
	// backreference names its group by index, and its min and max are the
	// runes it can match, max unbounded when they are not known.
	index int
	name  string

	// selfRef marks a group that holds a backreference to itself.
	selfRef bool

	// target is the group a call runs, and called marks a group that some
	// call runs. A called group remembers its lengths in wlo and whi once
	// widthKnown is set, and busy is set while they are worked out, so a
	// group that calls itself is measured once.
	target           *node
	called           bool
	widthKnown, busy bool
	wlo, whi         int

	// refs are a conditional's references, one for each group whose
	// capture picks the yes branch.
	refs []*node

	assert assertKind

	// neg and behind say whether a lookaround is negative and whether it
	// looks behind the position rather than ahead of it.
	neg, behind bool

	// nullKnown says that null holds the answer nullable gave for n.
	nullKnown, null bool
}

// unbounded is a repeat's max when the repeat has no upper bound.
const unbounded = -1

// nullable reports whether n can match the empty string. The compiler asks
// it of every repeat's body, so it remembers each answer on the node, and a
// stack of quantifiers costs one walk of the tree rather than one walk a
// level. It is asked only after lowering, which is the last pass that
// changes the tree.
func nullable(n *node) bool {
	if !n.nullKnown {
		n.null, n.nullKnown = emptyMatch(n), true
	}
	return n.null
}

// emptyMatch works out the answer that nullable remembers.
func emptyMatch(n *node) bool {
	switch n.kind {
	case kindLiteral:
		return len(n.runes) == 0
	case kindClass, kindAny:
		return false
	case kindConcat:
		for _, s := range n.subs {
			if !nullable(s) {
				return false
			}
		}
		return true
	case kindAlternate, kindCond:
		for _, s := range n.subs {
			if nullable(s) {
				return true
			}
		}
		return false
	case kindRepeat:
		return n.min == 0 || nullable(n.subs[0])
	case kindGroup, kindAtomic:
		return nullable(n.subs[0])
	default:
		// An assertion, a lookaround, a backreference, a \K, an absent
		// operator, and a call may all match nothing. A call is not
		// followed into its group, which may be the one asking.
		return true
	}
}

// surelyEmpty reports whether n can match the empty string without testing
// the text around it, so a lookbehind of n always holds.
func surelyEmpty(n *node) bool {
	switch n.kind {
	case kindLiteral:
		return len(n.runes) == 0
	case kindConcat:
		for _, s := range n.subs {
			if !surelyEmpty(s) {
				return false
			}
		}
		return true
	case kindAlternate:
		for _, s := range n.subs {
			if surelyEmpty(s) {
				return true
			}
		}
		return false
	case kindRepeat:
		return n.min == 0 || surelyEmpty(n.subs[0])
	case kindGroup, kindAtomic:
		return surelyEmpty(n.subs[0])
	case kindAbsent:
		// It gives back every rune it took, so it always holds empty.
		return true
	default:
		return false
	}
}

// fixedBehind reports whether the lookbehind body n has one length once
// the repeats that lead each of its top-level branches count at their
// fewest, which is the length jq's Oniguruma needs. A lookbehind that a
// repeat of one literal, class, or backreference can satisfy, its fewest
// repetitions satisfy too, so Oniguruma counts such a repeat that way when
// it starts a branch, and when that is none it does the same for the
// repeat after it. Every other part of a branch, such as a choice below
// the top level, a group, or a repeat after a literal, keeps its own
// length, and every branch must come to the same one.
func fixedBehind(n *node) bool {
	branches := []*node{n}
	if n.kind == kindAlternate {
		branches = n.subs
	}
	length := -1
	for _, b := range branches {
		lo, hi := leastWidth(b)
		if lo != hi || length >= 0 && lo != length {
			return false
		}
		length = lo
	}
	return true
}

// leastWidth is the width of the lookbehind branch n with its leading
// repeats counted at their fewest, as fixedBehind describes.
func leastWidth(n *node) (lo, hi int) {
	items := []*node{n}
	if n.kind == kindConcat {
		items = n.subs
	}
	leading := true
	for _, s := range items {
		slo, shi := width(s)
		if leading {
			item, least, ok := leastRepeat(s)
			if ok {
				slo, shi = repeatWidth(&node{kind: kindRepeat, subs: []*node{item}, min: least, max: least})
			}
			leading = ok && least == 0
		}
		lo = min(lo+slo, maxWidth)
		hi = addWidth(hi, shi)
	}
	return lo, hi
}

// leastRepeat returns the item the repeat n repeats and the fewest times
// it must, when that item is a literal, a class, any rune, or a
// backreference. A ?, *, or + repeat of another counts as one repeat, as
// Oniguruma merges the two.
func leastRepeat(n *node) (item *node, least int, ok bool) {
	if n.kind != kindRepeat {
		return nil, 0, false
	}
	least, item = n.min, n.subs[0]
	for plainCount(n) && item.kind == kindRepeat && plainCount(item) {
		least *= item.min
		n, item = item, item.subs[0]
	}
	if item.kind == kindLiteral || item.kind == kindClass || item.kind == kindAny || item.kind == kindBackref {
		return item, least, true
	}
	return nil, 0, false
}

// plainCount reports whether the repeat n is a ?, *, or +, lazy or not.
func plainCount(n *node) bool {
	return n.min <= 1 && (n.max == 1 || n.max == unbounded)
}

// anchoredAtStart reports whether every match of n must begin at the start
// of the subject, which lets a search try only that position.
func anchoredAtStart(n *node) bool {
	switch n.kind {
	case kindAssert:
		return n.assert == assertBeginText
	case kindConcat:
		return len(n.subs) > 0 && anchoredAtStart(n.subs[0])
	case kindAlternate:
		for _, s := range n.subs {
			if !anchoredAtStart(s) {
				return false
			}
		}
		return len(n.subs) > 0
	case kindGroup, kindAtomic:
		return anchoredAtStart(n.subs[0])
	case kindRepeat:
		return n.min > 0 && anchoredAtStart(n.subs[0])
	default:
		return false
	}
}

// leadingLoop returns the loop over one rune with no upper bound that n
// starts with, such as .*, a+, or [^,]{2,}, as a counting loop's
// instruction whose accepts tests the loop's rune, or nil when n starts
// with no such loop or holds a backreference. When a run from a start p
// fails, so does a run from any later start up to and including the first
// rune the loop refuses, because the loop from p could take every rune up
// to where the loop from the later start stopped and then go on the same
// way. A search for such a pattern need only retry after each rune the
// loop refuses.
func leadingLoop(n *node) *inst {
	if hasBackref(n) {
		return nil
	}
	for {
		switch n.kind {
		case kindConcat:
			if len(n.subs) == 0 {
				return nil
			}
			n = n.subs[0]
		case kindGroup:
			n = n.subs[0]
		case kindRepeat:
			one, ok := oneRune(n.subs[0])
			if n.max != unbounded || !ok {
				return nil
			}
			return &inst{op: opCount, body: one.op, r: one.r, set: one.set, lo: n.min, hi: n.max, greedy: n.greedy, peek: -1}
		default:
			return nil
		}
	}
}

// hasBackref reports whether n holds a backreference, a conditional, or a
// call anywhere. The first two read what a group captured, and a call
// runs a group whose start the search cannot plan around.
func hasBackref(n *node) bool {
	if n.kind == kindBackref || n.kind == kindCond || n.kind == kindCall {
		return true
	}
	for _, s := range n.subs {
		if hasBackref(s) {
			return true
		}
	}
	return false
}

// maxRequired caps the text requiredText returns, in bytes. A longer text
// would rule out few more subjects and cost more to look for.
const maxRequired = 256

// need is what requiredText learns about a node. When exact is set, every
// match of the node consumes text and nothing else. best is the longest
// text found that every match of the node holds.
type need struct {
	exact      bool
	text, best string
}

// exactly is the need of a node that always consumes text, which stops
// being exact once it is longer than requiredText keeps.
func exactly(text string) need {
	if len(text) > maxRequired {
		return need{best: capRequired(text)}
	}
	return need{exact: true, text: text, best: text}
}

// requiredText returns text that every match of n holds, or "" when it
// finds none, so a search can rule out a subject that lacks the text before
// trying a single start. It reads the tree after lowering, where a
// case-insensitive literal has become classes and alternations, so a
// literal still standing matches only itself. It learns nothing from a
// lookaround, a backreference, or a choice between different texts, and a
// zero-width assertion consumes nothing between the texts around it.
func requiredText(n *node) string {
	return needOf(n).best
}

func needOf(n *node) need {
	switch n.kind {
	case kindLiteral:
		return literalNeed(n)
	case kindClass:
		if c, ok := n.set.single(); ok && len(n.expansions) == 0 && plainRune(c) {
			return exactly(string(c))
		}
	case kindAssert, kindKeep:
		return exactly("")
	case kindConcat:
		return concatNeed(n.subs)
	case kindAlternate, kindCond:
		return alternateNeed(n.subs)
	case kindRepeat:
		return repeatNeed(n)
	case kindGroup, kindAtomic:
		return needOf(n.subs[0])
	case kindAny, kindLook, kindBackref, kindAbsent, kindCall:
		// Any one rune, a test of text the match may not hold, text that
		// only a capture decides, the runes before an absent, or a call,
		// which may recurse, holds nothing a subject must contain.
	}
	return need{}
}

// plainRune reports whether a subject holds c exactly where its UTF-8
// encoding stands. U+FFFD also matches an invalid byte, and a surrogate
// or a value past U+10FFFF has no encoding, so neither is plain.
func plainRune(c rune) bool {
	return utf8.ValidRune(c) && c != utf8.RuneError
}

// literalNeed is the need of a literal, which is its text when every rune
// in it is plain, and otherwise the longest run of plain runes it holds.
func literalNeed(n *node) need {
	if n.fold {
		return need{}
	}
	var best, run []rune
	for _, r := range n.runes {
		if plainRune(r) {
			run = append(run, r)
			continue
		}
		if len(run) > len(best) {
			best = run
		}
		run = nil
	}
	if len(run) == len(n.runes) {
		return exactly(string(run))
	}
	if len(run) > len(best) {
		best = run
	}
	return need{best: capRequired(string(best))}
}

// concatNeed joins the exact texts of neighboring nodes in a sequence, so
// a.bcd needs bcd, and keeps the longest text any part needs.
func concatNeed(subs []*node) need {
	exact, run, best := true, "", ""
	for _, s := range subs {
		sn := needOf(s)
		switch {
		case !sn.exact:
			best = longest(best, run, sn.best)
			exact, run = false, ""
		case len(run)+len(sn.text) > maxRequired:
			best = longest(best, capRequired(run+sn.text))
			exact, run = false, sn.text
		default:
			run += sn.text
		}
	}
	if exact {
		return exactly(run)
	}
	return need{best: longest(best, run)}
}

// alternateNeed is the need of a choice, which is exact only when every
// branch consumes the same text.
func alternateNeed(subs []*node) need {
	if len(subs) == 0 {
		return need{}
	}
	first := needOf(subs[0])
	for _, s := range subs[1:] {
		if sn := needOf(s); !first.exact || !sn.exact || sn.text != first.text {
			return need{}
		}
	}
	return first
}

// repeatNeed is the need of a repeat. A repeat that may run no times needs
// nothing, and one that runs at least once needs what its body needs, and
// the body's text repeated as often as the repeat must run.
func repeatNeed(n *node) need {
	body := needOf(n.subs[0])
	switch {
	case n.max == 0 || body.exact && body.text == "":
		return exactly("")
	case n.min == 0:
		return need{}
	case !body.exact:
		return need{best: body.best}
	case n.min == n.max && len(body.text)*n.min <= maxRequired:
		return exactly(strings.Repeat(body.text, n.min))
	}
	times := min(n.min, maxRequired/len(body.text)+1)
	return need{best: capRequired(strings.Repeat(body.text, times))}
}

// capRequired cuts text to at most maxRequired bytes on a rune boundary.
func capRequired(text string) string {
	if len(text) <= maxRequired {
		return text
	}
	end := maxRequired
	for !utf8.RuneStart(text[end]) {
		end--
	}
	return text[:end]
}

// longest returns the longest of texts, the first among equals.
func longest(texts ...string) string {
	best := ""
	for _, t := range texts {
		if len(t) > len(best) {
			best = t
		}
	}
	return best
}

// maxWidth caps the lengths width reports, which is far past any length a
// lookbehind can use and keeps the products of nested counts in range.
const maxWidth = 1 << 30

// width returns the fewest and the most runes n can match, with hi
// unbounded when there is no limit.
func width(n *node) (lo, hi int) {
	switch n.kind {
	case kindLiteral:
		return len(n.runes), len(n.runes)
	case kindClass, kindAny:
		return 1, 1
	case kindConcat:
		for _, s := range n.subs {
			slo, shi := width(s)
			lo = min(lo+slo, maxWidth)
			hi = addWidth(hi, shi)
		}
		return lo, hi
	case kindAlternate, kindCond:
		return alternateWidth(n.subs)
	case kindRepeat:
		return repeatWidth(n)
	case kindGroup, kindAtomic:
		return width(n.subs[0])
	case kindBackref:
		return n.min, n.max
	case kindAbsent:
		return 0, unbounded
	case kindCall:
		return callWidth(n)
	default:
		return 0, 0
	}
}

// callWidth is width for a call, which is the width of the group it runs.
// A call met while its group is being measured adds no runes and no limit,
// so a group that recurses has no upper bound, as in Oniguruma. A call not
// yet bound to its group has no known width.
func callWidth(n *node) (lo, hi int) {
	g := n.target
	switch {
	case g == nil || g.busy:
		return 0, unbounded
	case g.widthKnown:
		return g.wlo, g.whi
	}
	g.busy = true
	lo, hi = width(g.subs[0])
	g.busy = false
	g.wlo, g.whi, g.widthKnown = lo, hi, true
	return lo, hi
}

// alternateWidth is width for a choice among subs.
func alternateWidth(subs []*node) (lo, hi int) {
	for i, s := range subs {
		slo, shi := width(s)
		if i == 0 || slo < lo {
			lo = slo
		}
		if i == 0 || hi != unbounded && (shi == unbounded || shi > hi) {
			hi = shi
		}
	}
	return lo, hi
}

// repeatWidth is width for a repeat.
func repeatWidth(n *node) (lo, hi int) {
	slo, shi := width(n.subs[0])
	lo = mulWidth(slo, n.min)
	switch {
	case shi == 0 || n.max == 0:
		hi = 0
	case shi == unbounded || n.max == unbounded:
		hi = unbounded
	default:
		hi = mulWidth(shi, n.max)
	}
	return lo, hi
}

func addWidth(a, b int) int {
	if a == unbounded || b == unbounded {
		return unbounded
	}
	return min(a+b, maxWidth)
}

func mulWidth(a, b int) int {
	if a != 0 && b > maxWidth/a {
		return maxWidth
	}
	return min(a*b, maxWidth)
}

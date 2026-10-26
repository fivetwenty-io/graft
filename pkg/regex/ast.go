package regex

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
)

// assertKind names the zero-width tests.
type assertKind uint8

const (
	assertBeginText    assertKind = iota // ^ and \A, the start of the subject
	assertEndText                        // $ and \Z, the end of the subject or before a final newline
	assertEndSubject                     // \z, the end of the subject
	assertWordBoundary                   // \b, between a word rune and a rune that is not one
	assertNotBoundary                    // \B, anywhere \b does not match
	assertBeginLine                      // ^ under (?m), the start of the subject or of a line after a newline
	assertEndLine                        // $ under (?m), the end of the subject or before a newline
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
	case kindAlternate:
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
		return true
	}
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

// dotLead says where a search may restart for a pattern that leads with a
// loop over . that has no upper bound.
type dotLead uint8

const (
	noDotLead   dotLead = iota // the pattern leads with no such loop, so a search tries every start
	dotLeadLine                // the loop stops only at a newline, so a search retries after each one
	dotLeadAll                 // the loop takes every rune, so a search tries only its first start
)

// leadingDot reports whether n starts with a loop such as .* or .+ that no
// backreference can see. When a run from a start p fails, so does a run
// from any later start up to the next rune . refuses, because the loop
// from p could take every rune up to where the loop from the later start
// stopped and then go on the same way. A search for such a pattern need
// only retry after each rune . refuses, which is a newline unless . takes
// one too.
func leadingDot(n *node) dotLead {
	if hasBackref(n) {
		return noDotLead
	}
	for {
		switch n.kind {
		case kindConcat:
			if len(n.subs) == 0 {
				return noDotLead
			}
			n = n.subs[0]
		case kindGroup:
			n = n.subs[0]
		case kindRepeat:
			switch {
			case n.max != unbounded || n.subs[0].kind != kindAny:
				return noDotLead
			case n.subs[0].dotAll:
				return dotLeadAll
			}
			return dotLeadLine
		default:
			return noDotLead
		}
	}
}

// hasBackref reports whether n holds a backreference anywhere.
func hasBackref(n *node) bool {
	if n.kind == kindBackref {
		return true
	}
	for _, s := range n.subs {
		if hasBackref(s) {
			return true
		}
	}
	return false
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
	case kindAlternate:
		return alternateWidth(n.subs)
	case kindRepeat:
		return repeatWidth(n)
	case kindGroup, kindAtomic:
		return width(n.subs[0])
	case kindBackref:
		return n.min, n.max
	default:
		return 0, 0
	}
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

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
)

// assertKind names the zero-width tests.
type assertKind uint8

const (
	assertBeginText    assertKind = iota // ^ and \A, the start of the subject
	assertEndText                        // $ and \Z, the end of the subject or before a final newline
	assertEndSubject                     // \z, the end of the subject
	assertWordBoundary                   // \b, between a word rune and a rune that is not one
	assertNotBoundary                    // \B, anywhere \b does not match
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

	// index is a group's number and name its name, "" when unnamed.
	index int
	name  string

	assert assertKind
}

// unbounded is a repeat's max when the repeat has no upper bound.
const unbounded = -1

// nullable reports whether n can match the empty string.
func nullable(n *node) bool {
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
	case kindGroup:
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
	case kindGroup:
		return anchoredAtStart(n.subs[0])
	case kindRepeat:
		return n.min > 0 && anchoredAtStart(n.subs[0])
	default:
		return false
	}
}

package regex

// nodeKind names the kinds of node the parser builds.
type nodeKind uint8

const (
	kindLiteral   nodeKind = iota // a run of runes matched in order
	kindClass                     // one rune from a set
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
	assertBeginText assertKind = iota // ^, the start of the subject
	assertEndText                     // $, the end of the subject or before a final newline
)

// node is one element of a parsed pattern.
type node struct {
	kind nodeKind
	subs []*node

	// runes is a literal's run, and fold says it matches without regard to
	// case.
	runes []rune
	fold  bool

	// set is a class's runes.
	set runeSet

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

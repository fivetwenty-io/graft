package regex

import (
	"slices"
	"strings"
)

// maxCallDepth is how many called groups may run at once, the definition
// of a called group included, as jq's Oniguruma allows. A call past it
// fails.
const maxCallDepth = 20

// pendingCall is a call whose group is found once the whole pattern is
// read, since a call may name a group that comes after it. text is the
// name or number as written, for the error that names it, and inAbsent
// says whether the call stands inside an absent operator.
type pendingCall struct {
	n        *node
	pos      int
	text     string
	inAbsent bool
}

// pendingBehind is a lookbehind holding a call, whose lengths are known
// only once the call is bound to its group.
type pendingBehind struct {
	start int
	body  *node
}

// gCall reads the call after \g under Oniguruma, whose backslash is at
// start: a name or a number, signed or not, in angle brackets or single
// quotes.
func (p *parser) gCall(start int) (*node, error) {
	opener := p.pos
	ref := p.bracketed()
	if ref == "" {
		if name := p.src[opener+1:]; name != "" {
			return nil, p.errorAt(start, "invalid group name <"+name+">")
		}
		return nil, p.errorAt(start, "group name is empty")
	}
	return p.call(start, ref[1:len(ref)-1])
}

// callAhead reports whether the group after the ( at the current position
// is a call such as (?1), (?-1), (?+1), or (?&name).
func (p *parser) callAhead() bool {
	rest := p.src[p.pos:]
	if !p.rules().calls || !strings.HasPrefix(rest, "?") {
		return false
	}
	rest = rest[1:]
	if rest != "" && (rest[0] == '+' || rest[0] == '-') {
		rest = rest[1:]
	}
	return strings.HasPrefix(p.src[p.pos:], "?&") || rest != "" && rest[0] >= '0' && rest[0] <= '9'
}

// groupCall reads a call written as a group, (?n), (?-n), (?+n), or
// (?&name), whose ( is at start.
func (p *parser) groupCall(start int) (*node, error) {
	p.pos++ // '?'
	named := p.lookingAt("&")
	if named {
		p.pos++
	}
	end := strings.IndexByte(p.src[p.pos:], ')')
	if end < 0 {
		if name := p.src[p.pos:]; name != "" {
			return nil, p.errorAt(start, "invalid group name <"+name+">")
		}
		return nil, p.errorAt(start, "group name is empty")
	}
	text := p.src[p.pos : p.pos+end]
	p.pos += end + 1
	if named && text != "" && text[0] >= '0' && text[0] <= '9' || !named && !isSignedNumber(text) {
		return nil, p.errorAt(start, "invalid group name <"+text+">")
	}
	return p.call(start, text)
}

// call builds a call of the group that text names, by name or by number.
// A number with a sign counts groups from the call, back for a minus and
// forward for a plus, and 0 names the whole pattern.
func (p *parser) call(start int, text string) (*node, error) {
	n := &node{kind: kindCall}
	switch {
	case text == "":
		return nil, p.errorAt(start, "group name is empty")
	case isSignedNumber(text):
		k := parseCount(strings.TrimLeft(text, "+-"))
		switch {
		case k == 0 && (text[0] == '+' || text[0] == '-'):
			return nil, p.errorAt(start, "invalid group name <"+text+">")
		case text[0] == '-':
			n.index = len(p.names) - k
		case text[0] == '+':
			n.index = len(p.names) - 1 + k
		default:
			n.index = k
		}
		if n.index < 0 || n.index > countLimit {
			return nil, p.errorAt(start, "undefined group <"+text+"> reference")
		}
	default:
		n.name = text
	}
	p.calls = append(p.calls, pendingCall{n: n, pos: start, text: text, inAbsent: p.absents > 0})
	return n, nil
}

// resolveCalls binds each call to its group once the whole pattern is
// read, refuses a group that would recurse without end or a call that
// would run an absent operator inside another, and checks the lookbehinds
// that hold calls. It returns the pattern's root, which becomes a called
// group 0 when a call names the whole pattern.
func (p *parser) resolveCalls(root *node) (*node, error) {
	if len(p.calls) == 0 {
		return root, nil
	}
	whole := &node{kind: kindGroup, subs: []*node{root}}
	var first []pendingCall // the first call of each called group
	for _, c := range p.calls {
		if c.n.name != "" {
			index, err := p.callName(c)
			if err != nil {
				return nil, err
			}
			c.n.index = index
		}
		switch {
		case c.n.index >= len(p.names):
			return nil, p.errorAt(c.pos, "undefined group <"+c.text+"> reference")
		case c.n.index == 0:
			c.n.target = whole
		default:
			c.n.target = p.closed[c.n.index]
		}
		if !c.n.target.called {
			first = append(first, c)
		}
		c.n.target.called = true
	}
	if whole.called {
		root = whole
	}
	work := 0
	for _, c := range first {
		if endlessRecursion(c.n.target, &work) {
			return nil, p.errorAt(c.pos, "never ending recursion")
		}
	}
	if err := p.callsInAbsents(); err != nil {
		return nil, err
	}
	for _, b := range p.behinds {
		if err := p.checkLookbehind(b.start, b.body); err != nil {
			return nil, err
		}
	}
	return root, nil
}

// callsInAbsents refuses a call inside an absent operator that runs a
// group holding an absent operator, directly or through further calls.
// jq's Oniguruma lets the absent operator the call runs change where the
// one around the call stops, as it does for one written inside another,
// which the parser refuses too.
func (p *parser) callsInAbsents() error {
	// One walk visits each group at most once, since a group an earlier
	// walk visited reached no absent operator, or the walk would have
	// ended in the refusal.
	seen := map[*node]bool{}
	for _, c := range p.calls {
		if c.inAbsent && reachesAbsent(c.n.target, seen) {
			return unsupported("absent operator (?~...) called inside an absent operator")
		}
	}
	return nil
}

// callName returns the number of the one group the call c names.
func (p *parser) callName(c pendingCall) (int, error) {
	index := -1
	for i, name := range p.names {
		switch {
		case name != c.n.name:
		case index >= 0:
			return 0, p.errorAt(c.pos, "multiplex definition name <"+name+"> call")
		default:
			index = i
		}
	}
	if index < 0 {
		return 0, p.errorAt(c.pos, "undefined name <"+c.n.name+"> reference")
	}
	return index, nil
}

// hasCall reports whether n holds a call anywhere.
func hasCall(n *node) bool {
	return n.kind == kindCall || slices.ContainsFunc(n.subs, hasCall)
}

// reachesAbsent reports whether running n can run an absent operator, one
// in n itself or in a group a call in n runs. A group in seen counts as
// reaching none, and n's groups join it.
func reachesAbsent(n *node, seen map[*node]bool) bool {
	switch n.kind {
	case kindAbsent:
		return true
	case kindCall:
		return reachesAbsent(n.target, seen)
	case kindGroup:
		if seen[n] {
			return false
		}
		seen[n] = true
	case kindLiteral, kindClass, kindAny, kindConcat, kindAlternate, kindRepeat,
		kindAssert, kindLook, kindAtomic, kindBackref, kindKeep, kindCond:
		// Only the subs of these can run an absent operator.
	}
	return slices.ContainsFunc(n.subs, func(s *node) bool { return reachesAbsent(s, seen) })
}

// The answers recursion gives, as Oniguruma's check for a never ending
// recursion works them out.
const (
	recursionExists   = 1 << iota // some way through calls the group again
	recursionMust                 // every way through calls the group again
	recursionInfinite             // some way calls the group again before matching a rune
)

// maxRecursionWork bounds the nodes the check for a never ending
// recursion visits for one pattern. A check cut short finds nothing, which
// is safe, since the depth limit stops any recursion at run time.
const maxRecursionWork = 1 << 16

// endlessRecursion reports whether every way through the called group g
// calls g again, or some way calls it again before matching any rune,
// either of which jq's Oniguruma refuses as a never ending recursion.
// work counts the nodes visited for the pattern so far.
func endlessRecursion(g *node, work *int) bool {
	r := recursion(g.subs[0], g, true, map[*node]bool{}, work)
	return r&(recursionMust|recursionInfinite) != 0
}

// recursion works out how n reaches the group g through calls. head says
// whether n may start where g started, with no rune matched between. The
// groups in seen are being walked already, so a call that reaches one
// again adds nothing.
func recursion(n, g *node, head bool, seen map[*node]bool, work *int) int {
	*work++
	if *work > maxRecursionWork {
		return 0
	}
	switch n.kind {
	case kindConcat:
		return concatRecursion(n.subs, g, head, seen, work)
	case kindAlternate, kindCond:
		return alternateRecursion(n.subs, g, head, seen, work)
	case kindRepeat:
		if n.max == 0 {
			return 0
		}
		r := recursion(n.subs[0], g, head, seen, work)
		if n.min == 0 {
			r &^= recursionMust
		}
		return r
	case kindAbsent:
		// The absent's body need not match for the absent to, so it never
		// must call g, but it runs from where the absent starts.
		return recursion(n.subs[0], g, head, seen, work) &^ recursionMust
	case kindLook, kindAtomic:
		return recursion(n.subs[0], g, head, seen, work)
	case kindCall:
		return recursion(n.target, g, head, seen, work)
	case kindGroup:
		switch {
		case n == g && head:
			return recursionExists | recursionMust | recursionInfinite
		case n == g:
			return recursionExists | recursionMust
		case seen[n]:
			return 0
		}
		seen[n] = true
		r := recursion(n.subs[0], g, head, seen, work)
		delete(seen, n)
		return r
	default:
		return 0
	}
}

// concatRecursion is recursion for the sequence subs, which stops being at
// the head of g once one of them must match a rune.
func concatRecursion(subs []*node, g *node, head bool, seen map[*node]bool, work *int) int {
	r := 0
	for _, s := range subs {
		ret := recursion(s, g, head, seen, work)
		if ret&recursionInfinite != 0 {
			return ret
		}
		r |= ret
		if head {
			lo, _ := width(s)
			head = lo == 0
		}
	}
	return r
}

// alternateRecursion is recursion for a choice among subs, which must call
// g again only when every branch must.
func alternateRecursion(subs []*node, g *node, head bool, seen map[*node]bool, work *int) int {
	r, must := 0, recursionMust
	for _, s := range subs {
		ret := recursion(s, g, head, seen, work)
		if ret&recursionInfinite != 0 {
			return ret
		}
		r |= ret & recursionExists
		must &= ret
	}
	return r | must
}

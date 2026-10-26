package regex

// opcode names the VM's instructions.
type opcode uint8

const (
	opRune      opcode = iota // match the rune r
	opClass                   // match one rune in set
	opAny                     // match any rune
	opAnyNotNL                // match any rune but a newline
	opSplit                   // continue at x, and on failure at y
	opJmp                     // continue at x
	opSave                    // record the position in slot x
	opLoopCheck               // leave a loop for y when slot x holds the position, as an empty iteration does
	opAssert                  // test assert at the position, with set the word runes for a boundary
	opAhead                   // run the body at x from the position, then continue at y if it matched, or under neg if it did not
	opBehind                  // like opAhead, with the body starting lo to hi runes back and ending at the position
	opAtomic                  // run the body at x, then continue at y from where its first match ended
	opSucceed                 // end a body that opAhead, opBehind, or opAtomic runs
	opBackref                 // match the text group x captured, folding case under fold
	opOpen                    // start group x at the position and mark it unfinished, for a group that refers to itself under Oniguruma
	opCapture                 // finish group x, starting where slot y recorded and ending at the position, for a group that refers to itself under PCRE
	opMatch                   // report a match
)

// inst is one instruction of a compiled program.
type inst struct {
	op     opcode
	r      rune
	set    runeSet
	x, y   int
	assert assertKind
	neg    bool
	fold   bool
	lo, hi int // the runes a lookbehind looks back, hi unbounded when there is no limit
}

// lower rewrites the tree so that case-insensitive literals and classes
// need no special instructions. A folded literal becomes a sequence of
// classes, one per rune, holding each rune's case-fold orbit. Under the
// Oniguruma flavor it also gains alternations for full case folding, and a
// class with expansions becomes an alternation of the class and its
// expansions. The parser shares one node among the places a class repeats,
// and lower shares that class's lowering the same way.
func lower(n *node, cfg *config) *node {
	l := lowering{cfg: cfg, classes: map[*node]*node{}}
	return l.node(n)
}

type lowering struct {
	cfg     *config
	classes map[*node]*node
}

func (l *lowering) node(n *node) *node {
	for i, s := range n.subs {
		n.subs[i] = l.node(s)
	}
	cfg := l.cfg
	switch {
	case n.kind == kindLiteral && n.fold && cfg.flavor == Oniguruma:
		return fullFoldLiteral(n.runes)
	case n.kind == kindLiteral && n.fold:
		seq := &node{kind: kindConcat}
		for _, r := range n.runes {
			seq.append(orbitNode(r))
		}
		return single(seq)
	case n.kind == kindBackref:
		// The parser gave a reference its group's lengths before
		// lowering, which can change them under full case folding, so
		// a lookbehind that holds one tries every length.
		n.min, n.max = 0, unbounded
	case n.kind == kindClass && len(n.expansions) > 0:
		if alt, ok := l.classes[n]; ok {
			return alt
		}
		alt := &node{kind: kindAlternate, subs: []*node{{kind: kindClass, set: n.set}}}
		for _, e := range n.expansions {
			alt.subs = append(alt.subs, orbitSequence(e))
		}
		l.classes[n] = alt
		return alt
	}
	return n
}

// fullFoldLiteral lowers a literal under full case folding. It reads the
// literal from the left, and at each position takes the longest run of runes
// that is the full folding of some rune, matching either that rune or the
// run. A rune that is not the start of such a run matches its orbit, or its
// own full folding when it has one.
func fullFoldLiteral(runes []rune) *node {
	seq := &node{kind: kindConcat}
	for i := 0; i < len(runes); {
		k, sources := longestExpansionAt(runes[i:])
		if k > 0 {
			seq.append(&node{kind: kindAlternate, subs: []*node{
				{kind: kindClass, set: sources},
				orbitSequence(runes[i : i+k]),
			}})
			i += k
			continue
		}
		var expansion []rune
		for _, r := range orbit(runes[i]) {
			if expansion = fullFoldOf(r); expansion != nil {
				break
			}
		}
		if expansion == nil {
			seq.append(orbitNode(runes[i]))
		} else {
			seq.append(&node{kind: kindAlternate, subs: []*node{orbitNode(runes[i]), orbitSequence(expansion)}})
		}
		i++
	}
	return single(seq)
}

// longestExpansionAt finds the longest full folding that runes starts with
// and returns its length and every rune that folds to it, with their orbits.
// It returns 0 when runes starts with none.
func longestExpansionAt(runes []rune) (int, runeSet) {
	best := 0
	var sources []rune
	for _, f := range fullFolds {
		k := len(f.to)
		if k < best || k > len(runes) || !foldEqual(f.to, runes[:k]) {
			continue
		}
		if k > best {
			best, sources = k, nil
		}
		sources = append(sources, orbit(f.from)...)
	}
	if best == 0 {
		return 0, nil
	}
	return best, setOf(sources...)
}

// orbitNode matches r or any rune in its case-fold orbit.
func orbitNode(r rune) *node {
	set := setOf(orbit(r)...)
	if c, ok := set.single(); ok {
		return &node{kind: kindLiteral, runes: []rune{c}}
	}
	return &node{kind: kindClass, set: set}
}

func orbitSequence(runes []rune) *node {
	seq := &node{kind: kindConcat}
	for _, r := range runes {
		seq.append(orbitNode(r))
	}
	return single(seq)
}

// single unwraps a concat of one node.
func single(seq *node) *node {
	if len(seq.subs) == 1 {
		return seq.subs[0]
	}
	return seq
}

// saturating keeps program sizes from overflowing: every sum and product
// stops at limit+1, which is enough to know the limit was passed.
type saturating int

func (c saturating) add(a, b int) int { return min(a+b, int(c)) }

func (c saturating) mul(a, b int) int {
	if a == 0 || b == 0 {
		return 0
	}
	if a > int(c)/b {
		return int(c)
	}
	return min(a*b, int(c))
}

// programSize returns how many instructions n compiles to, or limit+1 when
// that is more than limit. It mirrors compiler.node exactly, so a pattern is
// refused before any of its program is allocated.
func programSize(n *node, limit int) int {
	c := saturating(limit + 1)
	switch n.kind {
	case kindLiteral:
		return min(len(n.runes), int(c))
	case kindConcat:
		total := 0
		for _, s := range n.subs {
			total = c.add(total, programSize(s, limit))
		}
		return total
	case kindAlternate:
		total := 2 * (len(n.subs) - 1)
		for _, s := range n.subs {
			total = c.add(total, programSize(s, limit))
		}
		return total
	case kindGroup, kindAtomic:
		return c.add(programSize(n.subs[0], limit), 2)
	case kindLook:
		if alt := behindBranches(n); alt != nil {
			return programSize(alt, limit)
		}
		return c.add(programSize(n.subs[0], limit), 2)
	case kindRepeat:
		return repeatSize(n, programSize(n.subs[0], limit), c)
	default:
		return 1
	}
}

func repeatSize(n *node, body int, c saturating) int {
	check := 0
	if nullable(n.subs[0]) {
		check = 2
	}
	switch {
	case n.max == 0:
		return 0
	case n.max == unbounded && n.min == 0:
		return c.add(body, 2+check)
	case n.max == unbounded:
		return c.add(c.mul(n.min-1, body), c.add(body, 1+check))
	default:
		return c.add(c.mul(n.min, body), c.mul(n.max-n.min, body+1))
	}
}

// compiler emits a program for a tree.
type compiler struct {
	flavor Flavor
	prog   []inst
	marks  map[*node]int // the loop-mark slot of each repeat that needs one
	starts map[*node]int // the slot holding the start of each self-referring group under PCRE
	base   int           // the first slot after the capture slots
	extra  int           // the slots handed out past base
}

// compile lowers the tree to a program that records the whole match in
// slots 0 and 1. It returns the program and the number of slots it uses
// beyond the capture slots, for loop marks and unfinished group starts.
func compile(root *node, groups int, cfg *config) ([]inst, int, error) {
	limit := cfg.budget.MaxInstructions
	size := saturating(limit+1).add(programSize(root, limit), 3)
	if size > limit {
		return nil, 0, overBudget("compiled program exceeds the budget of %d instructions", limit)
	}
	c := &compiler{flavor: cfg.flavor, prog: make([]inst, 0, size), marks: map[*node]int{}, starts: map[*node]int{}, base: 2 * groups}
	c.emit(inst{op: opSave, x: 0})
	c.node(root)
	c.emit(inst{op: opSave, x: 1})
	c.emit(inst{op: opMatch})
	return c.prog, c.extra, nil
}

// slot returns the extra slot that n holds in slots, handing out the next
// one the first time.
func (c *compiler) slot(slots map[*node]int, n *node) int {
	s, ok := slots[n]
	if !ok {
		s = c.base + c.extra
		c.extra++
		slots[n] = s
	}
	return s
}

func (c *compiler) emit(in inst) int {
	c.prog = append(c.prog, in)
	return len(c.prog) - 1
}

func (c *compiler) node(n *node) {
	switch n.kind {
	case kindLiteral:
		for _, r := range n.runes {
			c.emit(inst{op: opRune, r: r})
		}
	case kindClass:
		c.emit(inst{op: opClass, set: n.set})
	case kindAny:
		c.emit(anyInst(n))
	case kindAssert:
		c.emit(c.assertInst(n))
	case kindConcat:
		for _, s := range n.subs {
			c.node(s)
		}
	case kindAlternate:
		c.alternate(n.subs)
	case kindGroup:
		c.group(n)
	case kindBackref:
		c.emit(inst{op: opBackref, x: n.index, fold: n.fold})
	case kindRepeat:
		c.repeat(n)
	case kindAtomic:
		c.body(inst{op: opAtomic}, n.subs[0])
	case kindLook:
		c.look(n)
	}
}

// anyInst matches the any node n, which takes a newline only under dotAll.
func anyInst(n *node) inst {
	if n.dotAll {
		return inst{op: opAny}
	}
	return inst{op: opAnyNotNL}
}

// assertInst tests the assertion n. A word boundary carries the flavor's
// word class.
func (c *compiler) assertInst(n *node) inst {
	in := inst{op: opAssert, assert: n.assert}
	if n.assert == assertWordBoundary || n.assert == assertNotBoundary {
		in.set = escapeClass('w', c.flavor)
	}
	return in
}

// body emits in, then a body that in runs, ending in opSucceed, and points
// in at the body and at what follows it.
func (c *compiler) body(in inst, body *node) {
	pc := c.emit(in)
	c.node(body)
	c.emit(inst{op: opSucceed})
	c.prog[pc].x, c.prog[pc].y = pc+1, len(c.prog)
}

// look emits a lookaround. A lookbehind with several branches becomes one
// lookbehind per branch, so each branch looks back by its own lengths.
func (c *compiler) look(n *node) {
	if alt := behindBranches(n); alt != nil {
		c.node(alt)
		return
	}
	in := inst{op: opAhead, neg: n.neg}
	if n.behind {
		in.op = opBehind
		in.lo, in.hi = width(n.subs[0])
	}
	c.body(in, n.subs[0])
}

// behindBranches rewrites a lookbehind of several branches as one lookbehind
// per branch, or returns nil for any other node. The positive form tries the
// branches in order inside an atomic group, since an assertion that held is
// never retried, and the negative form requires each branch to fail.
func behindBranches(n *node) *node {
	body := n.subs[0]
	if !n.behind || body.kind != kindAlternate {
		return nil
	}
	looks := make([]*node, len(body.subs))
	for i, b := range body.subs {
		looks[i] = &node{kind: kindLook, subs: []*node{b}, neg: n.neg, behind: true}
	}
	if n.neg {
		return &node{kind: kindConcat, subs: looks}
	}
	return &node{kind: kindAtomic, subs: []*node{{kind: kindAlternate, subs: looks}}}
}

// group emits a capturing group. A group that holds a reference to itself
// must not let the reference see a capture half made. Under Oniguruma the
// reference fails while the group is open, as jq's does, so the group's
// end is cleared when it opens. Under PCRE the reference sees the group's
// last complete capture, as PCRE2's does, so the new start waits in a slot
// of its own until the group closes.
func (c *compiler) group(n *node) {
	start, end := 2*n.index, 2*n.index+1
	switch {
	case !n.selfRef:
		c.emit(inst{op: opSave, x: start})
		c.node(n.subs[0])
		c.emit(inst{op: opSave, x: end})
	case c.flavor == Oniguruma:
		c.emit(inst{op: opOpen, x: n.index})
		c.node(n.subs[0])
		c.emit(inst{op: opSave, x: end})
	default:
		pending := c.slot(c.starts, n)
		c.emit(inst{op: opSave, x: pending})
		c.node(n.subs[0])
		c.emit(inst{op: opCapture, x: n.index, y: pending})
	}
}

// alternate emits each branch behind a split that falls through to the
// next branch, and a jump from the end of each branch to the end of all.
func (c *compiler) alternate(subs []*node) {
	var jumps []int
	for i, s := range subs {
		if i == len(subs)-1 {
			c.node(s)
			break
		}
		split := c.emit(inst{op: opSplit})
		c.prog[split].x = len(c.prog)
		c.node(s)
		jumps = append(jumps, c.emit(inst{op: opJmp}))
		c.prog[split].y = len(c.prog)
	}
	for _, j := range jumps {
		c.prog[j].x = len(c.prog)
	}
}

func (c *compiler) repeat(n *node) {
	body := n.subs[0]
	switch {
	case n.max == 0:
	case n.max == unbounded && n.min == 0:
		c.star(n)
	case n.max == unbounded:
		for range n.min - 1 {
			c.node(body)
		}
		c.plus(n)
	default:
		for range n.min {
			c.node(body)
		}
		exits := make([]int, 0, n.max-n.min)
		for range n.max - n.min {
			exits = append(exits, c.emit(inst{op: opSplit}))
			c.node(body)
		}
		for _, s := range exits {
			c.branch(s, s+1, len(c.prog), n.greedy)
		}
	}
}

// branch sets the split at pc to prefer more when greedy and less when lazy.
func (c *compiler) branch(pc, more, less int, greedy bool) {
	if greedy {
		c.prog[pc].x, c.prog[pc].y = more, less
	} else {
		c.prog[pc].x, c.prog[pc].y = less, more
	}
}

// star emits a loop that runs its body zero or more times.
func (c *compiler) star(n *node) {
	top := c.emit(inst{op: opSplit})
	check := c.iteration(n)
	c.emit(inst{op: opJmp, x: top})
	end := len(c.prog)
	c.branch(top, top+1, end, n.greedy)
	if check >= 0 {
		c.prog[check].y = end
	}
}

// plus emits a loop that runs its body one or more times.
func (c *compiler) plus(n *node) {
	top := len(c.prog)
	check := c.iteration(n)
	split := c.emit(inst{op: opSplit})
	end := len(c.prog)
	c.branch(split, top, end, n.greedy)
	if check >= 0 {
		c.prog[check].y = end
	}
}

// iteration emits one pass of a loop's body. When the body can match the
// empty string it is bracketed by a mark and a check that leaves the loop
// after an iteration that consumed nothing, as PCRE2 does, so the loop
// cannot spin. It returns the check's position for the caller to point at
// the loop's exit, or -1.
func (c *compiler) iteration(n *node) int {
	if !nullable(n.subs[0]) {
		c.node(n.subs[0])
		return -1
	}
	slot := c.slot(c.marks, n)
	c.emit(inst{op: opSave, x: slot})
	c.node(n.subs[0])
	return c.emit(inst{op: opLoopCheck, x: slot})
}

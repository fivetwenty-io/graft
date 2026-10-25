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
	opAssert                  // test assert at the position
	opMatch                   // report a match
)

// inst is one instruction of a compiled program.
type inst struct {
	op     opcode
	r      rune
	set    runeSet
	x, y   int
	assert assertKind
}

// lower rewrites the tree so that case-insensitive literals need no special
// instructions. A folded literal becomes a sequence of classes, one per
// rune, holding each rune's case-fold orbit.
func lower(n *node) *node {
	for i, s := range n.subs {
		n.subs[i] = lower(s)
	}
	if n.kind == kindLiteral && n.fold {
		seq := &node{kind: kindConcat}
		for _, r := range n.runes {
			seq.append(orbitNode(r))
		}
		return single(seq)
	}
	return n
}

// orbitNode matches r or any rune in its case-fold orbit.
func orbitNode(r rune) *node {
	set := setOf(orbit(r)...)
	if c, ok := set.single(); ok {
		return &node{kind: kindLiteral, runes: []rune{c}}
	}
	return &node{kind: kindClass, set: set}
}

// single unwraps a concat of one node.
func single(seq *node) *node {
	if len(seq.subs) == 1 {
		return seq.subs[0]
	}
	return seq
}

// compiler emits a program for a tree.
type compiler struct {
	prog  []inst
	marks map[*node]int // the loop-mark slot of each repeat that needs one
	base  int           // the first slot after the capture slots
}

// compile lowers the tree to a program that records the whole match in
// slots 0 and 1. It returns the program and the number of loop-mark slots
// it uses beyond the capture slots.
func compile(root *node, groups int) ([]inst, int) {
	c := &compiler{marks: map[*node]int{}, base: 2 * groups}
	c.emit(inst{op: opSave, x: 0})
	c.node(root)
	c.emit(inst{op: opSave, x: 1})
	c.emit(inst{op: opMatch})
	return c.prog, len(c.marks)
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
		if n.dotAll {
			c.emit(inst{op: opAny})
		} else {
			c.emit(inst{op: opAnyNotNL})
		}
	case kindAssert:
		c.emit(inst{op: opAssert, assert: n.assert})
	case kindConcat:
		for _, s := range n.subs {
			c.node(s)
		}
	case kindAlternate:
		c.alternate(n.subs)
	case kindGroup:
		c.emit(inst{op: opSave, x: 2 * n.index})
		c.node(n.subs[0])
		c.emit(inst{op: opSave, x: 2*n.index + 1})
	case kindRepeat:
		c.repeat(n)
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
	slot, ok := c.marks[n]
	if !ok {
		slot = c.base + len(c.marks)
		c.marks[n] = slot
	}
	c.emit(inst{op: opSave, x: slot})
	c.node(n.subs[0])
	return c.emit(inst{op: opLoopCheck, x: slot})
}

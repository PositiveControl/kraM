package main

import "fmt"

// bitWidth is the number of wires (bits) per variable in the elementary-gate
// circuit. Arithmetic is mod 2^bitWidth (two's complement).
const bitWidth = 16

// BitOp is an elementary reversible gate.
type BitOp int

const (
	BX     BitOp = iota // NOT on wire T
	BCNOT               // if A then flip T
	BTOFF               // if A and B then flip T  (Toffoli / CCNOT)
)

// BitGate is one elementary gate over wire indices.
type BitGate struct {
	Op      BitOp
	A, B, T int
}

func (g BitGate) String() string {
	switch g.Op {
	case BX:
		return fmt.Sprintf("X    w%d", g.T)
	case BCNOT:
		return fmt.Sprintf("CNOT w%d -> w%d", g.A, g.T)
	case BTOFF:
		return fmt.Sprintf("TOFF w%d,w%d -> w%d", g.A, g.B, g.T)
	}
	return "?"
}

// bitCircuit is a compiled elementary-gate program plus its wire layout.
type bitCircuit struct {
	gates  []BitGate
	base   map[string]int  // var -> index of its bit 0
	nwires int
	procs  map[string]Node // procedure bodies, for inlining call/uncall
}

// compileBits lowers a reversible program to elementary gates over
// {X, CNOT, Toffoli}. Each variable is a bitWidth-bit little-endian register;
// ancillas (carry bits, constant/condition registers) are allocated as needed
// and always returned to zero, so they are reusable scratch. procs supplies
// procedure definitions for inlining call/uncall (nil if none).
func compileBits(n Node, procs map[string]Node) (*bitCircuit, error) {
	c := &bitCircuit{base: map[string]int{}, procs: map[string]Node{}}
	for k, v := range procs {
		c.procs[k] = v
	}
	registerProcs(n, c.procs) // pick up procs defined within the program too
	for _, name := range collectVars(n, c.procs) {
		c.base[name] = c.nwires
		c.nwires += bitWidth
	}
	if err := c.emit(n); err != nil {
		return nil, err
	}
	return c, nil
}

// registerProcs records every ProcDef in the program into procs.
func registerProcs(n Node, procs map[string]Node) {
	switch v := n.(type) {
	case Block:
		for _, s := range v.Stmts {
			registerProcs(s, procs)
		}
	case ProcDef:
		procs[v.Name] = v.Body
	}
}

// reg returns the wire indices (bit 0 .. bitWidth-1) for a variable.
func (c *bitCircuit) reg(name string) []int {
	base, ok := c.base[name]
	if !ok { // a variable that only appears as a swap/operand still needs wires
		base = c.nwires
		c.base[name] = base
		c.nwires += bitWidth
	}
	w := make([]int, bitWidth)
	for i := range w {
		w[i] = base + i
	}
	return w
}

// alloc reserves n fresh ancilla wires (initialised to zero).
func (c *bitCircuit) alloc(n int) []int {
	w := make([]int, n)
	for i := range w {
		w[i] = c.nwires
		c.nwires++
	}
	return w
}

func (c *bitCircuit) x(t int)          { c.gates = append(c.gates, BitGate{BX, 0, 0, t}) }
func (c *bitCircuit) cnot(a, t int)    { c.gates = append(c.gates, BitGate{BCNOT, a, 0, t}) }
func (c *bitCircuit) toff(a, b, t int) { c.gates = append(c.gates, BitGate{BTOFF, a, b, t}) }

func (c *bitCircuit) emit(n Node) error {
	switch v := n.(type) {
	case Block:
		for _, s := range v.Stmts {
			if err := c.emit(s); err != nil {
				return err
			}
		}
		return nil
	case XorAssign:
		return c.emitXor(v)
	case CompoundAssign:
		return c.emitAdd(v)
	case Swap:
		x, y := c.reg(v.A), c.reg(v.B)
		for i := 0; i < bitWidth; i++ { // swap = 3 CNOTs per bit
			c.cnot(x[i], y[i])
			c.cnot(y[i], x[i])
			c.cnot(x[i], y[i])
		}
		return nil
	case ProcDef:
		return nil // definition only — registered in compileBits, emits nothing
	case Call:
		body, ok := c.procs[v.Name]
		if !ok {
			return fmt.Errorf("undefined procedure %q", v.Name)
		}
		return c.emit(body)
	case Uncall:
		body, ok := c.procs[v.Name]
		if !ok {
			return fmt.Errorf("undefined procedure %q", v.Name)
		}
		inv, err := invert(body)
		if err != nil {
			return fmt.Errorf("cannot uncall %q: %w", v.Name, err)
		}
		return c.emit(inv)
	case If:
		return c.emitIf(v)
	case While, ReversibleLoop:
		return fmt.Errorf("loops have data-dependent bounds — not a fixed circuit; unroll to straight-line code")
	}
	return fmt.Errorf("cannot compile %T to elementary gates", n)
}

func (c *bitCircuit) emitXor(v XorAssign) error {
	x := c.reg(v.Name)
	switch val := v.Value.(type) {
	case NumberLit:
		k := int64(val.Val)
		for i := 0; i < bitWidth; i++ {
			if k&(1<<uint(i)) != 0 {
				c.x(x[i]) // XOR by constant = NOT on set bits
			}
		}
		return nil
	case Var:
		if val.Name == v.Name {
			return fmt.Errorf("cannot compile self-referential %q ^= %q", v.Name, v.Name)
		}
		y := c.reg(val.Name)
		for i := 0; i < bitWidth; i++ {
			c.cnot(y[i], x[i]) // XOR from another register = CNOT per bit
		}
		return nil
	}
	return fmt.Errorf("^= operand must be a constant or a variable to compile")
}

func (c *bitCircuit) emitAdd(v CompoundAssign) error {
	target := c.reg(v.Name)
	var addend []int
	var cleanup func()

	switch val := v.Value.(type) {
	case NumberLit:
		// Materialise the constant in a fresh register, add, then uncompute it.
		k := int64(val.Val)
		addend = c.alloc(bitWidth)
		set := func() {
			for i := 0; i < bitWidth; i++ {
				if k&(1<<uint(i)) != 0 {
					c.x(addend[i])
				}
			}
		}
		set()
		cleanup = set // X is self-inverse, so the same gates clear it
	case Var:
		if val.Name == v.Name {
			return fmt.Errorf("cannot compile self-referential %q += %q", v.Name, v.Name)
		}
		addend = c.reg(val.Name)
	default:
		return fmt.Errorf("+= operand must be a constant or a variable to compile")
	}

	add := c.adderGates(addend, target)
	if v.Op == MINUS {
		add = inverseGates(add) // subtraction is the adder run backward
	}
	c.gates = append(c.gates, add...)
	if cleanup != nil {
		cleanup()
	}
	return nil
}

// emitIf lowers a reversible if to controlled gates: compute the condition into
// an ancilla bit, apply each branch gate controlled on it, then uncompute the
// bit. Only `var == const` conditions are supported (an equality comparator);
// the branch must not modify the condition variable, and an exit assertion is
// required (the if must be reversible).
func (c *bitCircuit) emitIf(v If) error {
	if v.Exit == nil {
		return fmt.Errorf("if must be reversible (add an 'assert' exit) to compile")
	}
	condVar, k, err := equalityCond(v.Cond)
	if err != nil {
		return err
	}
	written := map[string]bool{}
	collectWrites(v.Then, written)
	collectWrites(v.Else, written)
	if written[condVar] {
		return fmt.Errorf("branch modifies the condition variable %q — not lowerable to a fixed control", condVar)
	}

	x := c.reg(condVar)
	q := c.alloc(1)[0]

	c.equalityToBit(x, k, q) // q = (condVar == k)

	then, err := c.emitSub(v.Then)
	if err != nil {
		return err
	}
	for _, g := range then {
		c.appendControlled(q, g)
	}
	if v.Else != nil {
		c.x(q) // else runs when condition is false
		els, err := c.emitSub(v.Else)
		if err != nil {
			return err
		}
		for _, g := range els {
			c.appendControlled(q, g)
		}
		c.x(q)
	}

	c.equalityToBit(x, k, q) // uncompute q (the comparator is self-inverse)
	return nil
}

// emitSub emits a node into a fresh gate slice, sharing the wire allocator and
// layout, and returns the produced gates.
func (c *bitCircuit) emitSub(n Node) ([]BitGate, error) {
	saved := c.gates
	c.gates = nil
	err := c.emit(n)
	sub := c.gates
	c.gates = saved
	return sub, err
}

// appendControlled adds one control wire q to a gate: X->CNOT, CNOT->Toffoli,
// Toffoli->C^3X (decomposed via one clean ancilla).
func (c *bitCircuit) appendControlled(q int, g BitGate) {
	switch g.Op {
	case BX:
		c.cnot(q, g.T)
	case BCNOT:
		c.toff(q, g.A, g.T)
	case BTOFF:
		anc := c.alloc(1)[0]
		c.toff(q, g.A, anc)
		c.toff(anc, g.B, g.T)
		c.toff(q, g.A, anc) // restore anc to 0
	}
}

// equalityToBit sets q ^= (reg == k). Self-inverse, restores reg. Flip the bits
// where k is 0 so reg is all-ones exactly when reg==k, AND them into q with a
// multi-controlled NOT, then unflip.
func (c *bitCircuit) equalityToBit(reg []int, k int64, q int) {
	flip := func() {
		for i := 0; i < bitWidth; i++ {
			if k&(1<<uint(i)) == 0 {
				c.x(reg[i])
			}
		}
	}
	flip()
	c.mcx(reg, q)
	flip()
}

// mcx applies a multi-controlled NOT: t ^= AND(controls). Uses a clean ancilla
// ladder that is fully uncomputed.
func (c *bitCircuit) mcx(controls []int, t int) {
	n := len(controls)
	if n == 1 {
		c.cnot(controls[0], t)
		return
	}
	anc := c.alloc(n - 1)
	c.toff(controls[0], controls[1], anc[0])
	for i := 2; i < n; i++ {
		c.toff(controls[i], anc[i-2], anc[i-1])
	}
	c.cnot(anc[n-2], t)
	for i := n - 1; i >= 2; i-- { // uncompute the ladder
		c.toff(controls[i], anc[i-2], anc[i-1])
	}
	c.toff(controls[0], controls[1], anc[0])
}

// equalityCond extracts (varName, constant) from a `var == const` condition.
func equalityCond(n Node) (string, int64, error) {
	b, ok := n.(Binary)
	if !ok || b.Op != EQ {
		return "", 0, fmt.Errorf("circuit if-condition must be 'var == const'")
	}
	if v, ok := b.Left.(Var); ok {
		if k, ok := b.Right.(NumberLit); ok {
			return v.Name, int64(k.Val), nil
		}
	}
	if v, ok := b.Right.(Var); ok {
		if k, ok := b.Left.(NumberLit); ok {
			return v.Name, int64(k.Val), nil
		}
	}
	return "", 0, fmt.Errorf("circuit if-condition must be 'var == const'")
}

// collectWrites records variables a node assigns to (targets and swap operands).
func collectWrites(n Node, w map[string]bool) {
	switch v := n.(type) {
	case nil:
		return
	case Block:
		for _, s := range v.Stmts {
			collectWrites(s, w)
		}
	case XorAssign:
		w[v.Name] = true
	case CompoundAssign:
		w[v.Name] = true
	case Swap:
		w[v.A] = true
		w[v.B] = true
	case If:
		collectWrites(v.Then, w)
		collectWrites(v.Else, w)
	}
}

// adderGates returns a Cuccaro in-place ripple-carry adder: target += addend
// (mod 2^bitWidth). The addend register is restored; one ancilla carry bit is
// used and returned to zero. Gates are returned (not appended) so subtraction
// can reuse them reversed.
func (c *bitCircuit) adderGates(addend, target []int) []BitGate {
	z := c.alloc(1)[0] // carry-in ancilla, starts 0
	g := &bitCircuit{nwires: c.nwires}

	maj := func(ci, bi, ai int) { g.cnot(ai, bi); g.cnot(ai, ci); g.toff(ci, bi, ai) }
	uma := func(ci, bi, ai int) { g.toff(ci, bi, ai); g.cnot(ai, ci); g.cnot(ci, bi) }

	n := bitWidth
	maj(z, target[0], addend[0])
	for i := 1; i < n; i++ {
		maj(addend[i-1], target[i], addend[i])
	}
	// modular: drop the carry-out CNOT (no overflow wire)
	for i := n - 1; i >= 1; i-- {
		uma(addend[i-1], target[i], addend[i])
	}
	uma(z, target[0], addend[0])

	c.nwires = g.nwires
	return g.gates
}

func inverseGates(gs []BitGate) []BitGate {
	out := make([]BitGate, len(gs))
	for i, g := range gs { // X/CNOT/Toffoli are self-inverse; just reverse order
		out[len(gs)-1-i] = g
	}
	return out
}

// simulateBits runs the elementary gates over a bit array and returns it.
func simulateBits(gates []BitGate, nwires int, init []bool) []bool {
	bits := make([]bool, nwires)
	copy(bits, init)
	for _, g := range gates {
		switch g.Op {
		case BX:
			bits[g.T] = !bits[g.T]
		case BCNOT:
			if bits[g.A] {
				bits[g.T] = !bits[g.T]
			}
		case BTOFF:
			if bits[g.A] && bits[g.B] {
				bits[g.T] = !bits[g.T]
			}
		}
	}
	return bits
}

// collectVars returns, in first-appearance order, the variable names a program
// reads or writes — descending into branches and inlined procedures.
func collectVars(n Node, procs map[string]Node) []string {
	var order []string
	seen := map[string]bool{}
	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			order = append(order, name)
		}
	}
	calling := map[string]bool{} // guard against recursive procs
	var walk func(Node)
	addExpr := func(e Node) {
		if y, ok := e.(Var); ok {
			add(y.Name)
		}
	}
	walk = func(n Node) {
		switch v := n.(type) {
		case Block:
			for _, s := range v.Stmts {
				walk(s)
			}
		case XorAssign:
			add(v.Name)
			addExpr(v.Value)
		case CompoundAssign:
			add(v.Name)
			addExpr(v.Value)
		case Swap:
			add(v.A)
			add(v.B)
		case If:
			collectCondVars(v.Cond, add)
			walk(v.Then)
			if v.Else != nil {
				walk(v.Else)
			}
		case Call:
			if body, ok := procs[v.Name]; ok && !calling[v.Name] {
				calling[v.Name] = true
				walk(body)
				calling[v.Name] = false
			}
		case Uncall:
			if body, ok := procs[v.Name]; ok && !calling[v.Name] {
				calling[v.Name] = true
				walk(body)
				calling[v.Name] = false
			}
		}
	}
	walk(n)
	return order
}

// collectCondVars adds the variables referenced in a condition expression.
func collectCondVars(n Node, add func(string)) {
	switch v := n.(type) {
	case Var:
		add(v.Name)
	case Binary:
		collectCondVars(v.Left, add)
		collectCondVars(v.Right, add)
	case Unary:
		collectCondVars(v.Right, add)
	}
}

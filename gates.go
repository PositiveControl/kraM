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
	base   map[string]int // var -> index of its bit 0
	nwires int
}

// compileBits lowers a straight-line reversible program to elementary gates
// over {X, CNOT, Toffoli}. Each variable is a bitWidth-bit little-endian
// register; ancillas (carry bits, constant registers) are allocated as needed
// and always returned to zero, so they are reusable scratch.
func compileBits(n Node) (*bitCircuit, error) {
	c := &bitCircuit{base: map[string]int{}}
	for _, name := range collectVars(n) {
		c.base[name] = c.nwires
		c.nwires += bitWidth
	}
	if err := c.emit(n); err != nil {
		return nil, err
	}
	return c, nil
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
	}
	return fmt.Errorf("cannot compile %T to elementary gates (straight-line += -= ^= <=> only)", n)
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

// collectVars returns, in first-appearance order, the variable names a
// straight-line program reads or writes.
func collectVars(n Node) []string {
	var order []string
	seen := map[string]bool{}
	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			order = append(order, name)
		}
	}
	var walk func(Node)
	walk = func(n Node) {
		switch v := n.(type) {
		case Block:
			for _, s := range v.Stmts {
				walk(s)
			}
		case XorAssign:
			add(v.Name)
			if y, ok := v.Value.(Var); ok {
				add(y.Name)
			}
		case CompoundAssign:
			add(v.Name)
			if y, ok := v.Value.(Var); ok {
				add(y.Name)
			}
		case Swap:
			add(v.A)
			add(v.B)
		}
	}
	walk(n)
	return order
}

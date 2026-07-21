package main

import "fmt"

// bitWidth is the number of wires (bits) per variable in the elementary-gate
// circuit. Arithmetic is mod 2^bitWidth (two's complement).
const bitWidth = 16

// BitOp is an elementary reversible gate.
type BitOp int

const (
	BX    BitOp = iota // NOT on wire T
	BCNOT              // if A then flip T
	BTOFF              // if A and B then flip T  (Toffoli / CCNOT)
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
	width  int                // bits per register (bitWidth unless overridden)
	procs  map[string]ProcDef // procedure definitions, for inlining call/uncall
	free_  []int              // clean (zeroed) ancilla wires available for reuse
	noFree bool               // while true, freed wires are NOT pooled (see emitSub)
	vals   *Interp            // compile-time state, for computing static loop bounds
}

// compileBits lowers a reversible program to elementary gates over
// {X, CNOT, Toffoli}. Each variable is a bitWidth-bit little-endian register;
// ancillas (carry bits, constant/condition registers) are allocated as needed
// and always returned to zero, so they are reusable scratch. procs supplies
// procedure definitions for inlining call/uncall (nil if none).
func compileBits(n Node, ip *Interp) (*bitCircuit, error) {
	return compileBitsW(n, ip, bitWidth)
}

// compileBitsW is compileBits with an explicit register width (the Grover /
// QASM path compiles narrow circuits so they fit on real quantum hardware).
func compileBitsW(n Node, ip *Interp, width int) (*bitCircuit, error) {
	// Compile against a *clone* of the live state: emitting a program advances a
	// shadow (init assignments set variables, so a following loop unrolls from the
	// right values) without mutating the caller's interpreter.
	var vals *Interp
	if ip != nil {
		vals = ip.clone()
	}
	c := &bitCircuit{base: map[string]int{}, procs: map[string]ProcDef{}, vals: vals, width: width}
	if ip != nil {
		for k, v := range ip.procs {
			c.procs[k] = v
		}
	}
	registerProcs(n, c.procs) // pick up procs defined within the program too
	// The unroll shadow (c.vals) executes bodies to advance state; give it the
	// program's procedures so a `call` inside a loop resolves during unrolling.
	if c.vals != nil {
		for name, p := range c.procs {
			c.vals.procs[name] = p
		}
	}
	for _, name := range collectVars(n, c.procs) {
		c.base[name] = c.nwires
		c.nwires += c.width
	}
	if err := c.emit(n); err != nil {
		return nil, err
	}
	return c, nil
}

// registerProcs records every ProcDef in the program into procs.
func registerProcs(n Node, procs map[string]ProcDef) {
	switch v := n.(type) {
	case Block:
		for _, s := range v.Stmts {
			registerProcs(s, procs)
		}
	case ProcDef:
		procs[v.Name] = v
	}
}

// reg returns the wire indices (bit 0 .. bitWidth-1) for a variable.
func (c *bitCircuit) reg(name string) []int {
	base, ok := c.base[name]
	if !ok { // a variable that only appears as a swap/operand still needs wires
		base = c.nwires
		c.base[name] = base
		c.nwires += c.width
	}
	w := make([]int, c.width)
	for i := range w {
		w[i] = base + i
	}
	return w
}

// alloc reserves n ancilla wires, all guaranteed zero — reusing freed ones
// before minting new. Every allocator restores its ancillas to zero before
// freeing, so a reused wire is always clean.
func (c *bitCircuit) alloc(n int) []int {
	w := make([]int, n)
	for i := range w {
		if k := len(c.free_); k > 0 {
			w[i] = c.free_[k-1]
			c.free_ = c.free_[:k-1]
		} else {
			w[i] = c.nwires
			c.nwires++
		}
	}
	return w
}

// free returns ancilla wires to the pool. The caller guarantees they hold zero.
func (c *bitCircuit) free(wires ...int) {
	if c.noFree {
		return
	}
	c.free_ = append(c.free_, wires...)
}

func (c *bitCircuit) x(t int)          { c.gates = append(c.gates, BitGate{BX, 0, 0, t}) }
func (c *bitCircuit) cnot(a, t int)    { c.gates = append(c.gates, BitGate{BCNOT, a, 0, t}) }
func (c *bitCircuit) toff(a, b, t int) { c.gates = append(c.gates, BitGate{BTOFF, a, b, t}) }

func (c *bitCircuit) emit(n Node) error {
	switch v := n.(type) {
	case Block:
		// Emit each statement, then advance the shadow so the next statement (and
		// any loop or index fold) sees its effect. A `local` introduced here is
		// live for the statements after it — which is what lets a nested loop over
		// an array unroll (the inner loop's index folds against the live locals).
		for _, s := range v.Stmts {
			if err := c.emit(s); err != nil {
				return err
			}
			if c.vals != nil {
				if _, err := Eval(s, c.vals); err != nil {
					return err
				}
			}
		}
		return nil
	case XorAssign:
		return c.emitXor(v)
	case CompoundAssign:
		return c.emitAdd(v)
	case RotAssign:
		return c.emitRot(v)
	case Swap:
		x, err := c.locWires(v.A, v.AI)
		if err != nil {
			return err
		}
		y, err := c.locWires(v.B, v.BI)
		if err != nil {
			return err
		}
		for i := 0; i < c.width; i++ { // swap = 3 CNOTs per bit
			c.cnot(x[i], y[i])
			c.cnot(y[i], x[i])
			c.cnot(x[i], y[i])
		}
		return nil
	case IdxUpdate:
		reg, err := c.locWires(v.Name, v.Idx)
		if err != nil {
			return err
		}
		switch v.Op {
		case PLUSEQ:
			return c.addInto(reg, PLUS, v.Value)
		case MINUSEQ:
			return c.addInto(reg, MINUS, v.Value)
		default: // CARETEQ
			return c.xorInto(reg, v.Value)
		}
	case IdxAssign:
		return fmt.Errorf("destructive element assignment is irreversible — use += / -= / ^= / <=>")
	case Forget:
		return fmt.Errorf("forget %q is irreversible erasure — no gate exists", v.Name)
	case Assign:
		// Fresh initialisation lowers to register preparation: set the (zero)
		// register to the value — X the constant's set bits, or CNOT-copy another
		// register — then advance the shadow state so a following loop unrolls
		// from the right start values. Reassignment is irreversible, so rejected.
		if c.vals != nil {
			if _, exists := c.vals.get(v.Name); exists {
				return fmt.Errorf("cannot reassign %q in a circuit — '=' introduces a fresh register; use += / -= / ^= / <=> (:reset first if it is only left over from a previous run)", v.Name)
			}
		}
		// An array literal prepares one register per element (arrays have no single
		// register); a scalar prepares its own register. Either way, advance the
		// shadow so later element indices fold and a following loop unrolls.
		if arr, isArr := v.Value.(ArrayLit); isArr {
			for i, e := range arr.Elems {
				if err := c.initReg(c.elemReg(v.Name, i), e); err != nil {
					return err
				}
			}
		} else {
			base, ok := c.base[v.Name]
			if !ok {
				wires := c.allocReg()
				c.base[v.Name] = wires[0]
				base = wires[0]
			}
			wires := make([]int, c.width)
			for i := range wires {
				wires[i] = base + i
			}
			if err := c.initReg(wires, v.Value); err != nil {
				return err
			}
		}
		return nil // the shadow is advanced by the sequence processor (emit Block)
	case Reverse:
		inv, err := invert(v.Body)
		if err != nil {
			return err
		}
		return c.emit(inv)
	case Print:
		return nil // I/O has no register effect — omitted from the circuit
	case Local:
		// A local is an ancilla register: allocate it, initialise to its value.
		wires := c.allocReg()
		c.base[v.Name] = wires[0]
		return c.initReg(wires, v.Value)
	case Delocal:
		base, ok := c.base[v.Name]
		if !ok {
			return fmt.Errorf("delocal of unknown register %q", v.Name)
		}
		wires := make([]int, c.width)
		for i := range wires {
			wires[i] = base + i
		}
		// initReg is self-inverse: re-running it on a register holding its init
		// value clears it to zero (the program guarantees that value at delocal).
		if err := c.initReg(wires, v.Value); err != nil {
			return err
		}
		c.free(wires...)
		delete(c.base, v.Name)
		return nil
	case ArrayLit, Index:
		return fmt.Errorf("an array expression has no gates on its own")
	case ProcDef:
		return nil // definition only — registered in compileBits, emits nothing
	case Call:
		body, err := bindProcBody(c.procs, v.Name, v.Args)
		if err != nil {
			return err
		}
		return c.emit(body)
	case Uncall:
		body, err := bindProcBody(c.procs, v.Name, v.Args)
		if err != nil {
			return err
		}
		inv, err := invert(body)
		if err != nil {
			return fmt.Errorf("cannot uncall %q: %w", v.Name, err)
		}
		return c.emit(inv)
	case If:
		return c.emitIf(v)
	case ReversibleLoop:
		return c.emitLoop(v)
	case While:
		return fmt.Errorf("classic while is not reversible — use from/loop/until")
	}
	return fmt.Errorf("cannot compile %T to elementary gates", n)
}

func (c *bitCircuit) emitXor(v XorAssign) error { return c.xorInto(c.reg(v.Name), v.Value) }
func (c *bitCircuit) emitAdd(v CompoundAssign) error {
	if v.Op == STAR || v.Op == SLASH {
		return fmt.Errorf("*= and /= do not lower to gates (multiplication by an even factor is not a bijection mod 2^%d) — interpreter-only", c.width)
	}
	return c.addInto(c.reg(v.Name), v.Op, v.Value)
}

// emitRot lowers a bit rotation to a swap network (each wire swap is 3 CNOTs).
// The rotation is a fixed cyclic permutation of the register's wires, applied
// as three segment reversals. The amount must fold to a constant.
func (c *bitCircuit) emitRot(v RotAssign) error {
	if c.width != bitWidth {
		return fmt.Errorf("<<= / >>= are defined on the %d-bit word; this circuit compiles %d-bit registers", bitWidth, c.width)
	}
	k, err := foldRotAmount(v.Value, c.vals)
	if err != nil {
		return err
	}
	if k == 0 {
		return nil // identity
	}
	target := c.reg(v.Name)
	swap := func(a, b int) { c.cnot(a, b); c.cnot(b, a); c.cnot(a, b) }
	rev := func(lo, hi int) {
		for lo < hi {
			swap(target[lo], target[hi])
			lo++
			hi--
		}
	}
	// Value-rotl by k moves bit i to (i+k)%w: as an array op on wires (LSB
	// first) that is an array-rotate-left by w-k; value-rotr by k is an
	// array-rotate-left by k. Three reversals realise it in-place.
	m := k
	if v.Left {
		m = c.width - k
	}
	rev(0, m-1)
	rev(m, c.width-1)
	rev(0, c.width-1)
	return nil
}

// xorInto emits target ^= operand. The operand is a constant (X on set bits) or
// another register (CNOT per bit).
func (c *bitCircuit) xorInto(target []int, valNode Node) error {
	w, k, isConst, err := c.operand(valNode)
	if err != nil {
		return err
	}
	if isConst {
		for i := 0; i < c.width; i++ {
			if k&(1<<uint(i)) != 0 {
				c.x(target[i])
			}
		}
		return nil
	}
	if w[0] == target[0] {
		return fmt.Errorf("cannot compile a self-referential ^=")
	}
	for i := 0; i < c.width; i++ {
		c.cnot(w[i], target[i])
	}
	return nil
}

// addInto emits target += operand (op PLUS) or -= (op MINUS), via the Cuccaro
// adder. A constant operand is materialised in scratch, added, then uncomputed.
func (c *bitCircuit) addInto(target []int, op TokKind, valNode Node) error {
	w, k, isConst, err := c.operand(valNode)
	if err != nil {
		return err
	}
	var addend []int
	var cleanup func()
	if isConst {
		addend = c.alloc(c.width)
		set := func() {
			for i := 0; i < c.width; i++ {
				if k&(1<<uint(i)) != 0 {
					c.x(addend[i])
				}
			}
		}
		set()
		cleanup = set
	} else {
		if w[0] == target[0] {
			return fmt.Errorf("cannot compile a self-referential += / -=")
		}
		addend = w
	}
	add := c.adderGates(addend, target)
	if op == MINUS {
		add = inverseGates(add)
	}
	c.gates = append(c.gates, add...)
	if cleanup != nil {
		cleanup()
		c.free(addend...)
	}
	return nil
}

// operand resolves a += / ^= right-hand value to a constant or a register.
func (c *bitCircuit) operand(node Node) (wires []int, k int64, isConst bool, err error) {
	switch n := node.(type) {
	case NumberLit:
		return nil, int64(n.Val), true, nil
	case Var:
		return c.reg(n.Name), 0, false, nil
	case Index:
		base, ok := n.Arr.(Var)
		if !ok {
			return nil, 0, false, fmt.Errorf("only single-level array indexing compiles")
		}
		w, e := c.locWires(base.Name, n.Idx)
		return w, 0, false, e
	}
	// A compound expression (e.g. a loop bound like `w - 2*o`) that folds to a
	// constant under the compile-time shadow is treated as that constant. If it
	// depends on a value that actually changes at run time, :verify catches the
	// mismatch — folding is only sound for compile-time-invariant operands.
	if c.vals != nil {
		if val, err := Eval(node, c.vals.clone()); err == nil {
			if k, err := asInt(val, "operand"); err == nil {
				return nil, k, true, nil
			}
		}
	}
	return nil, 0, false, fmt.Errorf("operand must be a constant, variable, or constant-index element")
}

// locWires resolves an lvalue (variable or constant-index element) to its
// register wires. A non-nil index is folded to a constant at compile time.
func (c *bitCircuit) locWires(name string, idx Node) ([]int, error) {
	if idx == nil {
		return c.reg(name), nil
	}
	k, err := c.foldIndex(idx)
	if err != nil {
		return nil, err
	}
	return c.elemReg(name, k), nil
}

// allocReg mints a fresh contiguous bitWidth register block (for a local). It
// does not pull from the scattered ancilla pool, so the block stays contiguous
// for name-based addressing; its wires return to the pool when freed.
func (c *bitCircuit) allocReg() []int {
	w := make([]int, c.width)
	for i := range w {
		w[i] = c.nwires
		c.nwires++
	}
	return w
}

// initReg sets a register to a value (constant via X, or a copy of another
// register via CNOT). Self-inverse: applied again to a register holding that
// value, it clears it to zero — which is how delocal uncomputes a local.
func (c *bitCircuit) initReg(target []int, valNode Node) error {
	w, k, isConst, err := c.operand(valNode)
	if err != nil {
		return err
	}
	if isConst {
		for i := 0; i < c.width; i++ {
			if k&(1<<uint(i)) != 0 {
				c.x(target[i])
			}
		}
		return nil
	}
	for i := 0; i < c.width; i++ {
		c.cnot(w[i], target[i])
	}
	return nil
}

// elemReg returns the register for array element name[k].
func (c *bitCircuit) elemReg(name string, k int) []int { return c.reg(elemKey(name, k)) }

func elemKey(name string, k int) string { return fmt.Sprintf("%s[%d]", name, k) }

// foldIndex evaluates an index expression to a constant using compile-time
// state. Loop unrolling advances that state per iteration, so a loop-varying
// index like a[n-1-i] folds correctly each pass.
func (c *bitCircuit) foldIndex(idx Node) (int, error) {
	if c.vals == nil {
		return 0, fmt.Errorf("array index must be a compile-time constant (compile from known state)")
	}
	v, err := Eval(idx, c.vals.clone())
	if err != nil {
		return 0, fmt.Errorf("cannot fold array index: %w — :gates/:verify compile from the current variables, so define the index/loop variables first", err)
	}
	n, err := asInt(v, "array index")
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, fmt.Errorf("negative array index %d", n)
	}
	return int(n), nil
}

// emitIf lowers a reversible if to controlled gates: compute the condition into
// an ancilla bit, apply each branch gate controlled on it, then uncompute the
// bit. The condition may be a comparison or any && / || / ! combination of
// comparisons. The branch must not modify a condition variable, and an exit
// assertion is required (the if must be reversible).
func (c *bitCircuit) emitIf(v If) error {
	if v.Exit == nil {
		return fmt.Errorf("if must be reversible (add an 'assert' exit) to compile")
	}
	written := map[string]bool{}
	collectWrites(v.Then, written)
	collectWrites(v.Else, written)
	bad := ""
	collectCondVars(v.Cond, func(name string) {
		if written[name] {
			bad = name
		}
	})
	if bad != "" {
		return fmt.Errorf("branch modifies condition variable %q — not lowerable to a fixed control", bad)
	}

	q := c.alloc(1)[0]
	if err := c.condToBit(v.Cond, q); err != nil { // q = (condition)
		return err
	}

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

	_ = c.condToBit(v.Cond, q) // uncompute q (condToBit is self-inverse; already validated)
	c.free(q)
	return nil
}

// condToBit sets q ^= (condition), for q a clean (zero) ancilla. It handles
// comparisons and && / || / ! by computing sub-conditions into their own
// ancillas and combining them. Self-inverse: applied twice it restores q and
// all scratch, which is how emitIf uncomputes the control bit.
func (c *bitCircuit) condToBit(cond Node, q int) error {
	switch n := cond.(type) {
	case Unary:
		if n.Op != NOT {
			return fmt.Errorf("unsupported condition")
		}
		qa := c.alloc(1)[0]
		if err := c.condToBit(n.Right, qa); err != nil {
			return err
		}
		c.cnot(qa, q) // q ^= a
		c.x(q)        // q = !a   (q started 0)
		_ = c.condToBit(n.Right, qa)
		c.free(qa)
		return nil
	case Binary:
		switch n.Op {
		case AND, OR:
			qa := c.alloc(1)[0]
			qb := c.alloc(1)[0]
			if err := c.condToBit(n.Left, qa); err != nil {
				return err
			}
			if err := c.condToBit(n.Right, qb); err != nil {
				return err
			}
			if n.Op == OR { // q = a OR b = a XOR b XOR (a AND b)
				c.cnot(qa, q)
				c.cnot(qb, q)
			}
			c.toff(qa, qb, q) // q ^= a AND b
			_ = c.condToBit(n.Right, qb)
			_ = c.condToBit(n.Left, qa)
			c.free(qa, qb)
			return nil
		case EQ, NE, LT, GT, LE, GE:
			ct, err := comparisonCond(n)
			if err != nil {
				return err
			}
			c.compareToBit(ct, q)
			return nil
		}
	}
	return fmt.Errorf("circuit if-condition must be a comparison or a && / || / ! of comparisons")
}

// compareToBit sets q ^= (condition). Everything reduces to equality or the >=
// comparator with optional negation; applied twice it is the identity (each
// piece is self-inverse), so emitIf reuses it to uncompute the condition bit.
func (c *bitCircuit) compareToBit(ct condTerm, q int) {
	x := c.reg(ct.lhs)
	if ct.isConst {
		c.compareConst(x, ct.op, ct.k, q)
		return
	}
	y := c.reg(ct.rhs)
	switch ct.op {
	case EQ:
		c.eqVarToBit(x, y, q)
	case NE:
		c.eqVarToBit(x, y, q)
		c.x(q)
	case GE:
		c.geVarToBit(x, y, q)
	case LT:
		c.geVarToBit(x, y, q)
		c.x(q)
	case GT: // x > y  <=>  not (y >= x)
		c.geVarToBit(y, x, q)
		c.x(q)
	case LE: // x <= y <=>  y >= x
		c.geVarToBit(y, x, q)
	}
}

// compareConst handles a variable-vs-constant comparison.
func (c *bitCircuit) compareConst(reg []int, op TokKind, k int64, q int) {
	switch op {
	case EQ:
		c.equalityToBit(reg, k, q)
	case NE:
		c.equalityToBit(reg, k, q)
		c.x(q)
	case GE:
		c.geToBit(reg, k, q)
	case LT:
		c.geToBit(reg, k, q)
		c.x(q)
	case GT: // x > k  <=>  x >= k+1
		c.geToBit(reg, k+1, q)
	case LE: // x <= k <=>  not (x >= k+1)
		c.geToBit(reg, k+1, q)
		c.x(q)
	}
}

// eqVarToBit sets q ^= (x == y), restoring x and y.
func (c *bitCircuit) eqVarToBit(x, y []int, q int) {
	tmp := c.alloc(c.width)
	var fwd []BitGate
	for i := 0; i < c.width; i++ { // tmp = x XOR y
		fwd = append(fwd, BitGate{BCNOT, x[i], 0, tmp[i]})
		fwd = append(fwd, BitGate{BCNOT, y[i], 0, tmp[i]})
	}
	c.gates = append(c.gates, fwd...)
	for _, w := range tmp { // flip so tmp is all-ones iff x==y
		c.x(w)
	}
	c.mcx(tmp, q)
	for _, w := range tmp {
		c.x(w)
	}
	c.gates = append(c.gates, inverseGates(fwd)...) // clear tmp
	c.free(tmp...)
}

// geVarToBit sets q ^= (x >= y) via compute-copy-uncompute: s = x - y =
// x + ~y + 1 (carry-in 1) exposes carry-out = (x >= y); copy it, then run the
// computation backward to clear all scratch. x and y are restored.
func (c *bitCircuit) geVarToBit(x, y []int, q int) {
	s := c.alloc(c.width)
	cr := c.alloc(c.width)
	z := c.alloc(1)[0]
	cout := c.alloc(1)[0]

	var fwd []BitGate
	for i := 0; i < c.width; i++ { // s = copy of x
		fwd = append(fwd, BitGate{BCNOT, x[i], 0, s[i]})
	}
	for i := 0; i < c.width; i++ { // cr = ~y  (copy y, then NOT)
		fwd = append(fwd, BitGate{BCNOT, y[i], 0, cr[i]})
		fwd = append(fwd, BitGate{BX, 0, 0, cr[i]})
	}
	fwd = append(fwd, BitGate{BX, 0, 0, z}) // carry-in = 1, so s = x + ~y + 1 = x - y
	fwd = append(fwd, cuccaro(cr, s, z, cout)...)

	c.gates = append(c.gates, fwd...)
	c.cnot(cout, q) // cout = (x >= y)
	c.gates = append(c.gates, inverseGates(fwd)...)

	c.free(s...)
	c.free(cr...)
	c.free(z, cout)
}

// geToBit sets q ^= (reg >= k) for unsigned k, leaving reg unchanged. It uses
// compute-copy-uncompute: copy reg into scratch, add (2^w - k) to expose the
// carry-out (= reg >= k), copy that bit into q, then run the whole computation
// backward to clear all scratch.
func (c *bitCircuit) geToBit(reg []int, k int64, q int) {
	m := int64(1) << c.width
	switch {
	case k <= 0: // reg >= (<=0) always holds
		c.x(q)
		return
	case k >= m: // reg >= (>=2^w) never holds
		return
	}
	cst := m - k // two's complement of k, in [1, m-1]
	s := c.alloc(c.width)
	cr := c.alloc(c.width)
	z := c.alloc(1)[0]
	cout := c.alloc(1)[0]

	var fwd []BitGate
	for i := 0; i < c.width; i++ { // s = copy of reg
		fwd = append(fwd, BitGate{BCNOT, reg[i], 0, s[i]})
	}
	for i := 0; i < c.width; i++ { // cr = constant (2^w - k)
		if cst&(1<<uint(i)) != 0 {
			fwd = append(fwd, BitGate{BX, 0, 0, cr[i]})
		}
	}
	fwd = append(fwd, cuccaro(cr, s, z, cout)...) // s += cr, cout = (reg >= k)

	c.gates = append(c.gates, fwd...)
	c.cnot(cout, q)                                 // copy the comparison bit
	c.gates = append(c.gates, inverseGates(fwd)...) // uncompute all scratch

	c.free(s...)
	c.free(cr...)
	c.free(z, cout)
}

// emitSub emits a node into a fresh gate slice, sharing the wire allocator and
// layout, and returns the produced gates.
// emitLoop unrolls a reversible loop. A loop's iteration count is data-
// dependent, so it can only be a fixed circuit once that count is known: we
// shadow-evaluate the loop on the compile-time state to get the count, then
// emit the body that many times. The resulting circuit is specialised to that
// count (valid for inputs that loop the same number of times). The body must
// not itself contain a loop (each iteration would emit different gates).
func (c *bitCircuit) emitLoop(v ReversibleLoop) error {
	if c.vals == nil {
		return fmt.Errorf("cannot unroll a loop without compile-time state (set the loop variables first)")
	}
	// Unroll against an advancing shadow state: each body is emitted with c.vals
	// at that iteration (so a loop-varying array index like a[n-1-i] folds to the
	// right element), then the shadow is advanced by executing the body. A nested
	// loop unrolls the same way — its inner emitLoop clones the shadow at that
	// outer iteration, so each pass emits the gates for its own inner run.
	shadow := c.vals.clone()
	saved := c.vals
	c.vals = shadow
	defer func() { c.vals = saved }()

	const maxIter = 1_000_000
	entry, err := evalCond(v.Entry, shadow, "loop entry assertion")
	if err != nil {
		return fmt.Errorf("%w — :gates/:verify compile from the current variables, so set the loop variables first", err)
	}
	if !entry {
		return fmt.Errorf("loop entry condition is false in the current state — :gates/:verify compile from the current variables, so set them to the loop's starting values first (e.g. :reset and re-init)")
	}
	// c.vals == shadow here, so c.emit advances the shadow itself (see emit Block).
	if err := c.emit(v.Do); err != nil {
		return err
	}
	for count := 1; ; count++ {
		exit, err := evalCond(v.Exit, shadow, "loop exit assertion")
		if err != nil {
			return err
		}
		if exit {
			return nil
		}
		if count >= maxIter {
			return fmt.Errorf("loop exceeds %d iterations while unrolling", maxIter)
		}
		if err := c.emit(v.Rest); err != nil {
			return err
		}
		reentry, err := evalCond(v.Entry, shadow, "loop re-entry assertion")
		if err != nil {
			return err
		}
		if reentry {
			return fmt.Errorf("loop re-entry assertion violated at compile time")
		}
		if err := c.emit(v.Do); err != nil {
			return err
		}
	}
}

// hasLoop reports whether a node contains any loop.
func hasLoop(n Node) bool {
	found := false
	var walk func(Node)
	walk = func(n Node) {
		switch v := n.(type) {
		case Block:
			for _, s := range v.Stmts {
				walk(s)
			}
		case If:
			walk(v.Then)
			if v.Else != nil {
				walk(v.Else)
			}
		case While, ReversibleLoop:
			found = true
		}
	}
	walk(n)
	return found
}

// emitSub emits a node into a fresh gate slice, sharing the wire allocator and
// layout. Freeing is suspended: ancillas the sub allocates stay live (their
// wires are still referenced by the returned gates, which the caller will
// re-emit with an added control), so they must not be reused meanwhile.
func (c *bitCircuit) emitSub(n Node) ([]BitGate, error) {
	saved := c.gates
	savedNoFree := c.noFree
	savedVals := c.vals
	c.gates = nil
	c.noFree = true
	// Sub-emit extracts a branch's gates; it must fold indices against the
	// current shadow but must NOT advance it (the real advance happens once, when
	// the enclosing statement is executed by the sequence processor). Work on a
	// throwaway clone so any in-branch advances don't leak.
	if c.vals != nil {
		c.vals = c.vals.clone()
	}
	err := c.emit(n)
	sub := c.gates
	c.gates = saved
	c.noFree = savedNoFree
	c.vals = savedVals
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
		c.free(anc)
	}
}

// equalityToBit sets q ^= (reg == k). Self-inverse, restores reg. Flip the bits
// where k is 0 so reg is all-ones exactly when reg==k, AND them into q with a
// multi-controlled NOT, then unflip.
func (c *bitCircuit) equalityToBit(reg []int, k int64, q int) {
	flip := func() {
		for i := 0; i < c.width; i++ {
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
	c.free(anc...) // ladder restored to zero
}

// condTerm describes a circuit-lowerable condition: lhs <op> (const k | rhs).
type condTerm struct {
	lhs     string
	op      TokKind
	isConst bool
	k       int64  // when isConst
	rhs     string // when !isConst
}

// comparisonCond parses a `var <cmp> const`, `const <cmp> var`, or
// `var <cmp> var` condition.
func comparisonCond(n Node) (condTerm, error) {
	bad := fmt.Errorf("circuit if-condition must be a comparison (== != < > <= >=) of variables/constants")
	b, ok := n.(Binary)
	if !ok {
		return condTerm{}, bad
	}
	switch b.Op {
	case EQ, NE, LT, GT, LE, GE:
	default:
		return condTerm{}, bad
	}
	lv, lIsVar := b.Left.(Var)
	rv, rIsVar := b.Right.(Var)
	lk, lIsNum := b.Left.(NumberLit)
	rk, rIsNum := b.Right.(NumberLit)

	switch {
	case lIsVar && rIsNum:
		return condTerm{lhs: lv.Name, op: b.Op, isConst: true, k: int64(rk.Val)}, nil
	case lIsNum && rIsVar: // const <op> var  ==  var <flip> const
		return condTerm{lhs: rv.Name, op: flipCmp(b.Op), isConst: true, k: int64(lk.Val)}, nil
	case lIsVar && rIsVar:
		return condTerm{lhs: lv.Name, op: b.Op, rhs: rv.Name}, nil
	}
	return condTerm{}, bad
}

// flipCmp swaps the sense of a comparison when operands are reversed.
func flipCmp(op TokKind) TokKind {
	switch op {
	case LT:
		return GT
	case GT:
		return LT
	case LE:
		return GE
	case GE:
		return LE
	}
	return op // EQ, NE are symmetric
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

// cuccaro builds an in-place ripple-carry adder: target += addend
// (mod 2^bitWidth). z is the carry-in ancilla (must be 0); addend and z are
// restored. If cout >= 0 the carry-out is XORed into it (full, non-modular);
// cout < 0 drops it (modular). Pure gate construction — no allocation.
func cuccaro(addend, target []int, z, cout int) []BitGate {
	g := &bitCircuit{}
	maj := func(ci, bi, ai int) { g.cnot(ai, bi); g.cnot(ai, ci); g.toff(ci, bi, ai) }
	uma := func(ci, bi, ai int) { g.toff(ci, bi, ai); g.cnot(ai, ci); g.cnot(ci, bi) }

	n := len(target)
	maj(z, target[0], addend[0])
	for i := 1; i < n; i++ {
		maj(addend[i-1], target[i], addend[i])
	}
	if cout >= 0 {
		g.cnot(addend[n-1], cout) // carry-out
	}
	for i := n - 1; i >= 1; i-- {
		uma(addend[i-1], target[i], addend[i])
	}
	uma(z, target[0], addend[0])
	return g.gates
}

// adderGates: modular target += addend, allocating and freeing the carry-in.
func (c *bitCircuit) adderGates(addend, target []int) []BitGate {
	z := c.alloc(1)[0]
	gs := cuccaro(addend, target, z, -1)
	c.free(z) // restored to zero by the adder
	return gs
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
func collectVars(n Node, procs map[string]ProcDef) []string {
	var order []string
	seen := map[string]bool{}
	local := map[string]bool{} // names introduced by `local` — allocated on the fly
	add := func(name string) {
		if name != "" && !local[name] && !seen[name] {
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
			if v.AI == nil { // indexed operands use lazily-created element registers
				add(v.A)
			}
			if v.BI == nil {
				add(v.B)
			}
		case If:
			collectCondVars(v.Cond, add)
			walk(v.Then)
			if v.Else != nil {
				walk(v.Else)
			}
		case ReversibleLoop:
			collectCondVars(v.Entry, add)
			collectCondVars(v.Exit, add)
			walk(v.Do)
			walk(v.Rest)
		case Local: // a local is allocated on the fly, not in the base layout
			local[v.Name] = true
			addExpr(v.Value)
		case Delocal:
			addExpr(v.Value)
		case Call:
			if body, err := bindProcBody(procs, v.Name, v.Args); err == nil && !calling[v.Name] {
				calling[v.Name] = true
				walk(body)
				calling[v.Name] = false
			}
		case Uncall:
			if body, err := bindProcBody(procs, v.Name, v.Args); err == nil && !calling[v.Name] {
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

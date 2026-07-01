package main

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// A real backend would infer or declare per-variable register widths. The
// sketch is width-agnostic: bit positions in X gates are exact, while CNOT/SWAP
// act on whole registers of whatever width the target turns out to be.

// Gate is one reversible gate (register-level). Fields carry enough to both
// display and *simulate* the gate; the sketch does not decompose to elementary
// single-bit gates (Note records what each would expand to).
type Gate struct {
	Op      string
	Target  string // register written: X / CNOT / XOR / ADD / SUB
	Ctrl    string // CNOT control register
	A, B    string // SWAP operands
	Mask    int64  // X constant
	Operand Node   // ADD / SUB / XOR / ASSERT right-hand expression
	Note    string
}

func (g Gate) String() string {
	var s string
	switch g.Op {
	case "X":
		s = fmt.Sprintf("X(%s)", g.Target)
	case "CNOT":
		s = fmt.Sprintf("CNOT(%s, %s)", g.Ctrl, g.Target)
	case "SWAP":
		s = fmt.Sprintf("SWAP(%s, %s)", g.A, g.B)
	case "ADD", "SUB", "XOR":
		s = fmt.Sprintf("%s(%s, %s)", g.Op, g.Target, format(g.Operand))
	case "ASSERT":
		s = fmt.Sprintf("ASSERT(%s)", format(g.Operand))
	default:
		s = g.Op
	}
	if g.Note != "" {
		s += "   # " + g.Note
	}
	return s
}

// lowerProgram is the entry for :circuit — it prepares a shadow interpreter
// (procedures registered, state cloned so lowering never mutates the caller)
// then lowers the whole program to a register-level netlist.
func lowerProgram(n Node, ip *Interp) ([]Gate, error) {
	shadow := NewInterp()
	if ip != nil {
		shadow = ip.clone()
	}
	registerProcs(n, shadow.procs)
	return lower(n, shadow)
}

// lower compiles a reversible program to a register-level gate netlist. Control
// flow is unrolled and procedures inlined against `ip`, an advancing shadow of
// the interpreter state (so a data-dependent loop unrolls the right number of
// times); irreversible operations are rejected with the reason why.
func lower(n Node, ip *Interp) ([]Gate, error) {
	switch v := n.(type) {
	case Block:
		var gates []Gate
		for _, s := range v.Stmts {
			gs, err := lower(s, ip)
			if err != nil {
				return nil, err
			}
			gates = append(gates, gs...)
		}
		return gates, nil

	case XorAssign:
		switch val := v.Value.(type) {
		case NumberLit:
			bits := setBits(int64(val.Val))
			if bits == "" {
				return nil, nil // ^= 0 is the identity
			}
			return []Gate{{Op: "X", Target: v.Name, Mask: int64(val.Val), Note: "flip bit(s) " + bits}}, nil
		case Var:
			return []Gate{{Op: "CNOT", Ctrl: val.Name, Target: v.Name,
				Note: fmt.Sprintf("control %s, target %s (per bit)", val.Name, v.Name)}}, nil
		default:
			return []Gate{{Op: "XOR", Target: v.Name, Operand: v.Value,
				Note: "RHS needs an ancilla register to compute first"}}, nil
		}

	case Swap:
		return []Gate{{Op: "SWAP", A: v.A, B: v.B,
			Note: "3 CNOTs per bit (Fredkin-style)"}}, nil

	case CompoundAssign:
		op := "ADD"
		if v.Op == MINUS {
			op = "SUB"
		}
		return []Gate{{Op: op, Target: v.Name, Operand: v.Value,
			Note: "reversible ripple-carry adder block"}}, nil

	case Assert:
		return []Gate{{Op: "ASSERT", Operand: v.Cond, Note: "classical check, not a physical gate"}}, nil

	case Assign:
		// Fresh initialisation from a zero register is the same as XOR-ing the
		// value in (0 ^ k == k), so it lowers to the same X / CNOT prep gates.
		// Advance the shadow so a following loop unrolls from the right values.
		gs, err := lower(XorAssign{Name: v.Name, Value: v.Value}, ip)
		if err != nil {
			return nil, err
		}
		if err := advance(v, ip); err != nil {
			return nil, err
		}
		return gs, nil

	case Local:
		// A scoped temporary — at register level it prepares its register like
		// an init, and its value carries into the shadow for later folding.
		gs, err := lower(XorAssign{Name: v.Name, Value: v.Value}, ip)
		if err != nil {
			return nil, err
		}
		if err := advance(v, ip); err != nil {
			return nil, err
		}
		return gs, nil
	case Delocal:
		// Removing a temporary asserts its value (a classical check), then frees
		// the name in the shadow. No register gate beyond the assertion.
		if err := advance(v, ip); err != nil {
			return nil, err
		}
		return []Gate{{Op: "ASSERT", Operand: Binary{Op: EQ, Left: Var{Name: v.Name}, Right: v.Value},
			Note: "delocal: assert the temporary is clean before freeing it"}}, nil

	case ProcDef:
		return nil, nil // a definition emits nothing; registered in the shadow's procs

	case Call:
		body, err := bindProcBody(ip.procs, v.Name, v.Args)
		if err != nil {
			return nil, err
		}
		return lower(body, ip)
	case Uncall:
		body, err := bindProcBody(ip.procs, v.Name, v.Args)
		if err != nil {
			return nil, err
		}
		inv, err := invert(body)
		if err != nil {
			return nil, fmt.Errorf("cannot uncall %q: %w", v.Name, err)
		}
		return lower(inv, ip)

	case ReversibleLoop:
		return lowerLoop(v, ip)

	case Forget:
		return nil, fmt.Errorf("forget %q is irreversible erasure — no gate exists", v.Name)
	case Print:
		return nil, nil // I/O has no register effect — the circuit simply omits it
	case If:
		return nil, fmt.Errorf(":circuit does not lower a reversible if yet — use :gates (it lowers if to controlled gates)")
	case While:
		return nil, fmt.Errorf("classic while is not reversible — use from/loop/until")
	case Reverse:
		inv, err := invert(v.Body)
		if err != nil {
			return nil, err
		}
		return lower(inv, ip)
	}
	return nil, fmt.Errorf("cannot lower %T to a gate", n)
}

// advance runs a statement on the shadow interpreter to move its state forward,
// so a subsequent loop unroll or index fold sees the right values.
func advance(n Node, ip *Interp) error {
	if ip == nil {
		return nil
	}
	_, err := Eval(n, ip)
	return err
}

// lowerLoop unrolls a reversible loop into register-level gates, advancing a
// clone of the shadow so the iteration count comes from the actual state.
func lowerLoop(v ReversibleLoop, ip *Interp) ([]Gate, error) {
	if ip == nil {
		return nil, fmt.Errorf("cannot unroll a loop without compile-time state (set the loop variables first)")
	}
	if hasLoop(v.Do) || hasLoop(v.Rest) {
		return nil, fmt.Errorf("nested loops cannot be unrolled (each iteration would differ)")
	}
	shadow := ip.clone()
	lowerBody := func(body Node) ([]Gate, error) {
		gs, err := lower(body, shadow)
		if err != nil {
			return nil, err
		}
		if _, err := Eval(body, shadow); err != nil { // advance past this pass
			return nil, err
		}
		return gs, nil
	}

	entry, err := evalCond(v.Entry, shadow, "loop entry assertion")
	if err != nil {
		return nil, fmt.Errorf("%w — :circuit compiles from the current variables, so set the loop variables first", err)
	}
	if !entry {
		return nil, fmt.Errorf("loop entry condition is false in the current state — :circuit compiles from the current variables, so set them to the loop's starting values first (e.g. :reset and re-init)")
	}
	var gates []Gate
	gs, err := lowerBody(v.Do)
	if err != nil {
		return nil, err
	}
	gates = append(gates, gs...)
	const maxIter = 1_000_000
	for count := 1; ; count++ {
		exit, err := evalCond(v.Exit, shadow, "loop exit assertion")
		if err != nil {
			return nil, err
		}
		if exit {
			return gates, nil
		}
		if count >= maxIter {
			return nil, fmt.Errorf("loop exceeds %d iterations while unrolling", maxIter)
		}
		if gs, err = lowerBody(v.Rest); err != nil {
			return nil, err
		}
		gates = append(gates, gs...)
		reentry, err := evalCond(v.Entry, shadow, "loop re-entry assertion")
		if err != nil {
			return nil, err
		}
		if reentry {
			return nil, fmt.Errorf("loop re-entry assertion violated at compile time")
		}
		if gs, err = lowerBody(v.Do); err != nil {
			return nil, err
		}
		gates = append(gates, gs...)
	}
}

// simulate runs a gate netlist on integer registers, returning the final state.
// This is the *circuit* execution — independent of the tree-walk interpreter,
// so agreement between the two validates the lowering.
func simulate(gates []Gate, reg map[string]int64) (map[string]int64, error) {
	out := map[string]int64{}
	for k, v := range reg {
		out[k] = v
	}
	for _, g := range gates {
		switch g.Op {
		case "X":
			out[g.Target] ^= g.Mask
		case "CNOT":
			out[g.Target] ^= out[g.Ctrl]
		case "SWAP":
			out[g.A], out[g.B] = out[g.B], out[g.A]
		case "ADD", "SUB", "XOR":
			v, err := operandInt(g.Operand, out)
			if err != nil {
				return nil, err
			}
			switch g.Op {
			case "ADD":
				out[g.Target] += v
			case "SUB":
				out[g.Target] -= v
			case "XOR":
				out[g.Target] ^= v
			}
		case "ASSERT":
			ok, err := operandBool(g.Operand, out)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, fmt.Errorf("circuit assertion failed: %s", format(g.Operand))
			}
		}
	}
	return out, nil
}

// verify runs CODE both ways from the current variables: the tree-walk
// interpreter (on a clone) and the simulated elementary-gate circuit. It
// reports whether they agree, register by register (mod 2^bitWidth).
func verify(ast Node, ip *Interp) (string, error) {
	bc, err := compileBits(ast, ip)
	if err != nil {
		return "", fmt.Errorf("not compilable to a circuit: %w", err)
	}
	initReg := registersFrom(ip)

	clone := ip.clone()
	_, ierr := Eval(ast, clone)

	initBits := make([]bool, bc.nwires)
	for name, base := range bc.base {
		v := initReg[name]
		for b := 0; b < bitWidth; b++ {
			initBits[base+b] = (v>>uint(b))&1 == 1
		}
	}
	out := simulateBits(bc.gates, bc.nwires, initBits)

	var b strings.Builder
	if ierr != nil {
		fmt.Fprintf(&b, "interpreter errored: %v\n(circuit has %d gates)", ierr, len(bc.gates))
		return b.String(), nil
	}

	names := make([]string, 0, len(bc.base))
	for k := range bc.base {
		names = append(names, k)
	}
	sort.Strings(names)

	match := true
	for _, n := range names {
		var got int64
		base := bc.base[n]
		for bit := 0; bit < bitWidth; bit++ {
			if out[base+bit] {
				got |= 1 << uint(bit)
			}
		}
		want, ok := regWant(clone, n)
		mark := "ok"
		if !ok || want != got {
			mark = "MISMATCH"
			match = false
		}
		fmt.Fprintf(&b, "  %s: interp=%d circuit=%d  %s\n", n, want, got, mark)
	}
	if match {
		b.WriteString("MATCH — gate circuit agrees with interpreter")
	} else {
		b.WriteString("MISMATCH — gate circuit disagrees with interpreter")
	}
	return b.String(), nil
}

// energyReport estimates the energy a compiled circuit must dissipate, via
// Landauer's principle: erasing one bit of information costs at least kT·ln2
// joules. Ideal reversible gates (X/CNOT/Toffoli) dissipate nothing in the
// adiabatic limit — the cost comes from "garbage": scratch bits left set at the
// end that must be erased to reset the machine. A circuit that uncomputes all
// its scratch (the reversible-computing ideal) has zero garbage, hence a zero
// Landauer bound. An un-uncomputed local is exactly such garbage, so it is
// counted (locals are excluded from the logical variable set below).
func energyReport(ast Node, ip *Interp) (string, error) {
	bc, err := compileBits(ast, ip)
	if err != nil {
		return "", fmt.Errorf("not compilable to a circuit: %w", err)
	}

	// Logical variables are the program's real inputs/outputs. collectVars
	// deliberately omits `local` names, so a local left un-delocal'd falls
	// through to the garbage count — which is what it physically is.
	logical := map[string]bool{}
	for _, name := range collectVars(ast, bc.procs) {
		logical[name] = true
	}
	kept := make([]bool, bc.nwires)
	keptWires := 0
	for name, base := range bc.base {
		reg := name
		if n, _, isElem := splitElemKey(name); isElem {
			reg = n // array elements are always real data, never scratch
			logical[reg] = true
		}
		if logical[reg] {
			for b := 0; b < bitWidth; b++ {
				kept[base+b] = true
				keptWires++
			}
		}
	}

	init := registersFrom(ip)
	initBits := make([]bool, bc.nwires)
	for name, base := range bc.base {
		v := init[name]
		for b := 0; b < bitWidth; b++ {
			initBits[base+b] = (v>>uint(b))&1 == 1
		}
	}
	out := simulateBits(bc.gates, bc.nwires, initBits)

	garbage := 0
	for w := 0; w < bc.nwires; w++ {
		if !kept[w] && out[w] {
			garbage++
		}
	}

	const kB = 1.380649e-23 // Boltzmann constant, J/K
	const T = 300.0         // room temperature, K
	bound := float64(garbage) * kB * T * math.Ln2

	var b strings.Builder
	fmt.Fprintf(&b, "energy analysis (Landauer bound, T=%.0fK):\n", T)
	fmt.Fprintf(&b, "  gates:          %d   (reversible — 0 J in the adiabatic limit)\n", len(bc.gates))
	fmt.Fprintf(&b, "  wires:          %d   (%d ancilla scratch)\n", bc.nwires, bc.nwires-keptWires)
	fmt.Fprintf(&b, "  garbage bits:   %d   (scratch left set — must be erased)\n", garbage)
	fmt.Fprintf(&b, "  Landauer bound: %d·kT·ln2 = %.3e J\n", garbage, bound)
	if garbage == 0 {
		b.WriteString("  → adiabatically clean: all scratch uncomputed, 0 J lower bound")
	} else {
		b.WriteString("  → uncompute the scratch (delocal locals) to reach the 0 J ideal")
	}
	return b.String(), nil
}

// registersFrom snapshots integer-valued variables as initial registers; arrays
// are expanded element-by-element (a[k]) so constant-index circuits can load
// their starting values.
func registersFrom(ip *Interp) map[string]int64 {
	reg := map[string]int64{}
	intOf := func(v Value) (int64, bool) {
		if v.Kind == NumKind && v.Num == math.Trunc(v.Num) {
			return int64(v.Num), true
		}
		return 0, false
	}
	for k, b := range ip.vars {
		if !b.exists {
			continue
		}
		if n, ok := intOf(b.val); ok {
			reg[k] = n
		} else if b.val.Kind == ArrKind {
			for i, e := range b.val.Arr {
				if n, ok := intOf(e); ok {
					reg[elemKey(k, i)] = n
				}
			}
		}
	}
	return reg
}

// splitElemKey splits an element register key "name[k]" into its parts.
func splitElemKey(key string) (string, int, bool) {
	i := strings.IndexByte(key, '[')
	if i < 0 || !strings.HasSuffix(key, "]") {
		return key, 0, false
	}
	idx, err := strconv.Atoi(key[i+1 : len(key)-1])
	if err != nil {
		return key, 0, false
	}
	return key[:i], idx, true
}

// regWant returns the interpreter's expected value (mod 2^bitWidth) for a
// register key, resolving array element keys against the array variable.
func regWant(clone *Interp, key string) (int64, bool) {
	m := int64(1) << bitWidth
	mask := func(v int64) int64 { return ((v % m) + m) % m }
	if name, idx, isElem := splitElemKey(key); isElem {
		arr := clone.vars[name].val
		if arr.Kind != ArrKind || idx >= len(arr.Arr) || arr.Arr[idx].Kind != NumKind {
			return 0, false
		}
		return mask(int64(arr.Arr[idx].Num)), true
	}
	v := clone.vars[key].val
	if v.Kind != NumKind {
		return 0, false
	}
	return mask(int64(v.Num)), true
}

// operandInt evaluates an expression against register state to an integer.
func operandInt(n Node, reg map[string]int64) (int64, error) {
	v, err := Eval(n, regInterp(reg))
	if err != nil {
		return 0, err
	}
	return asInt(v, "circuit operand")
}

func operandBool(n Node, reg map[string]int64) (bool, error) {
	return evalCond(n, regInterp(reg), "circuit assertion")
}

// regInterp builds a throwaway interpreter whose variables are the registers,
// so gate operands reuse the normal expression evaluator.
func regInterp(reg map[string]int64) *Interp {
	ip := NewInterp()
	for k, v := range reg {
		ip.vars[k] = binding{val: numVal(float64(v)), exists: true}
	}
	return ip
}

// setBits lists the positions of set bits in n, low to high.
func setBits(n int64) string {
	var b []string
	for i := 0; i < 64; i++ {
		if n&(1<<uint(i)) != 0 {
			b = append(b, fmt.Sprintf("%d", i))
		}
	}
	return strings.Join(b, ",")
}

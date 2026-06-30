package main

import (
	"fmt"
	"math"
	"sort"
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

// lower compiles a straight-line reversible program to a reversible gate
// netlist. SKETCH: register-level gates over fixed-width registers; control
// flow and irreversible operations are rejected (with the reason why).
func lower(n Node) ([]Gate, error) {
	switch v := n.(type) {
	case Block:
		var gates []Gate
		for _, s := range v.Stmts {
			gs, err := lower(s)
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
		return nil, fmt.Errorf("destructive assignment of %q is irreversible — no gate exists", v.Name)
	case Print:
		return nil, fmt.Errorf("print is irreversible I/O — no gate exists")
	case If, While, ReversibleLoop, Reverse:
		return nil, fmt.Errorf("control flow is not lowered yet — straight-line reversible updates only")
	case ProcDef, Call, Uncall:
		return nil, fmt.Errorf("procedures are not lowered yet — inline the body to compile")
	}
	return nil, fmt.Errorf("cannot lower %T to a gate", n)
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
// interpreter (on a clone) and the simulated gate netlist. It reports whether
// they agree, register by register.
func verify(ast Node, ip *Interp) (string, error) {
	gates, err := lower(ast)
	if err != nil {
		return "", fmt.Errorf("not compilable to a circuit: %w", err)
	}
	initReg := registersFrom(ip)

	clone := ip.clone()
	_, ierr := Eval(ast, clone)
	simReg, serr := simulate(gates, initReg)

	var b strings.Builder
	if ierr != nil || serr != nil {
		fmt.Fprintf(&b, "interpreter: %s\ncircuit:     %s", errOrOK(ierr), errOrOK(serr))
		if (ierr == nil) != (serr == nil) {
			b.WriteString("\nMISMATCH — one path errored, the other did not")
		} else {
			b.WriteString("\n(both paths errored)")
		}
		return b.String(), nil
	}

	names := make([]string, 0, len(initReg))
	for k := range initReg {
		names = append(names, k)
	}
	sort.Strings(names)

	match := true
	for _, n := range names {
		iv := clone.vars[n].val
		sv := simReg[n]
		mark := "ok"
		if iv.Kind != NumKind || iv.Num != float64(int64(iv.Num)) || int64(iv.Num) != sv {
			mark = "MISMATCH"
			match = false
		}
		fmt.Fprintf(&b, "  %s: interp=%s circuit=%d  %s\n", n, iv, sv, mark)
	}
	if match {
		b.WriteString("MATCH — circuit agrees with interpreter")
	} else {
		b.WriteString("MISMATCH — circuit disagrees with interpreter")
	}
	return b.String(), nil
}

// registersFrom snapshots the integer-valued variables as initial registers.
func registersFrom(ip *Interp) map[string]int64 {
	reg := map[string]int64{}
	for k, b := range ip.vars {
		if b.exists && b.val.Kind == NumKind && b.val.Num == math.Trunc(b.val.Num) {
			reg[k] = int64(b.val.Num)
		}
	}
	return reg
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

func errOrOK(err error) string {
	if err == nil {
		return "ok"
	}
	return "error: " + err.Error()
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

package main

import (
	"fmt"
	"strings"
)

// circuitWidth is the assumed register size for the sketch. A real backend
// would infer or declare per-variable widths.
const circuitWidth = 8

// Gate is one reversible gate (register-level). The sketch does not decompose
// to single-qubit/bit gates; the Note records what each would expand to.
type Gate struct {
	Op   string
	Args []string
	Note string
}

func (g Gate) String() string {
	s := g.Op
	if len(g.Args) > 0 {
		s += "(" + strings.Join(g.Args, ", ") + ")"
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
			// XOR by a constant = NOT (X gate) on each set bit.
			bits := setBits(int64(val.Val))
			if bits == "" {
				return nil, nil // ^= 0 is the identity
			}
			return []Gate{{Op: "X", Args: []string{v.Name}, Note: "flip bit(s) " + bits}}, nil
		case Var:
			// XOR from another register = CNOT per bit.
			return []Gate{{Op: "CNOT", Args: []string{val.Name, v.Name},
				Note: fmt.Sprintf("control %s, target %s (×%d bits)", val.Name, v.Name, circuitWidth)}}, nil
		default:
			return []Gate{{Op: "XOR", Args: []string{v.Name, format(v.Value)},
				Note: "RHS needs an ancilla register to compute first"}}, nil
		}

	case Swap:
		return []Gate{{Op: "SWAP", Args: []string{v.A, v.B},
			Note: fmt.Sprintf("×%d bits = 3 CNOTs/bit (Fredkin-style)", circuitWidth)}}, nil

	case CompoundAssign:
		op := "ADD"
		if v.Op == MINUS {
			op = "SUB"
		}
		return []Gate{{Op: op, Args: []string{v.Name, format(v.Value)},
			Note: "reversible ripple-carry adder block"}}, nil

	case Assert:
		return []Gate{{Op: "ASSERT", Args: []string{format(v.Cond)},
			Note: "classical check, not a physical gate"}}, nil

	case Assign:
		return nil, fmt.Errorf("destructive assignment of %q is irreversible — no gate exists", v.Name)
	case Print:
		return nil, fmt.Errorf("print is irreversible I/O — no gate exists")
	case If, While, ReversibleLoop, Reverse:
		return nil, fmt.Errorf("control flow is not lowered yet — straight-line reversible updates only")
	}
	return nil, fmt.Errorf("cannot lower %T to a gate", n)
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

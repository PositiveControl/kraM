package main

import (
	"fmt"
	"strings"
)

// OpenQASM 2.0 export. The circuit is built as a flat []qop first and rendered
// second, so tests can statevector-simulate the exact ops that get exported.
//
// OpenQASM 2.0 + qelib1.inc rather than QASM 3: the whole vocabulary here is
// x/cx/ccx/h/measure, all in qelib1, and Qiskit's qasm2 importer is the mature
// path to hardware. A QASM 3 header would change ~5 lines if ever wanted.

type qopKind int

const (
	qX qopKind = iota
	qCX
	qCCX
	qH
)

type qop struct {
	kind    qopKind
	a, b, t int
}

// bitGateOps converts the classical reversible gate list 1:1.
func bitGateOps(gs []BitGate) []qop {
	ops := make([]qop, len(gs))
	for i, g := range gs {
		switch g.Op {
		case BX:
			ops[i] = qop{kind: qX, t: g.T}
		case BCNOT:
			ops[i] = qop{kind: qCX, a: g.A, t: g.T}
		case BTOFF:
			ops[i] = qop{kind: qCCX, a: g.A, b: g.B, t: g.T}
		}
	}
	return ops
}

// groverOps builds the complete Grover circuit for a compiled oracle:
//
//	H on every input wire            (uniform superposition)
//	X, H on the marker               (|-> so the bit-flip oracle phase-kicks)
//	iters × [oracle gates, diffusion]
//
// Diffusion = H^n X^n (h·mcx·h on the last input) X^n H^n — an MCZ conjugated
// into the computational basis. The mcx is emitted through the same bitCircuit
// so its ancilla ladder reuses the oracle's already-zeroed scratch pool: no
// new wires, no separate decomposition.
func groverOps(cond Node, width, iters int) ([]qop, *bitCircuit, oracleLayout, error) {
	bc, lay, err := compileOracle(cond, width)
	if err != nil {
		return nil, nil, oracleLayout{}, err
	}
	oracle := make([]BitGate, len(bc.gates))
	copy(oracle, bc.gates) // safe to replay: the cleanliness sweep guarantees ancillas end at zero

	// Diffusion mcx: controls = inputs[0..n-2], target = last input.
	bc.gates = nil
	last := lay.input[width-1]
	if width > 1 {
		bc.mcx(lay.input[:width-1], last)
	} else {
		// 1-bit search space: MCZ degenerates to Z on the single input, i.e.
		// h·x·h with no controls.
		bc.gates = append(bc.gates, BitGate{BX, 0, 0, last})
	}
	mcxOps := bitGateOps(bc.gates)
	oracleOps := bitGateOps(oracle)

	var ops []qop
	for _, w := range lay.input {
		ops = append(ops, qop{kind: qH, t: w})
	}
	ops = append(ops, qop{kind: qX, t: lay.marker}, qop{kind: qH, t: lay.marker})
	for k := 0; k < iters; k++ {
		ops = append(ops, oracleOps...)
		for _, w := range lay.input {
			ops = append(ops, qop{kind: qH, t: w})
		}
		for _, w := range lay.input {
			ops = append(ops, qop{kind: qX, t: w})
		}
		ops = append(ops, qop{kind: qH, t: last})
		ops = append(ops, mcxOps...)
		ops = append(ops, qop{kind: qH, t: last})
		for _, w := range lay.input {
			ops = append(ops, qop{kind: qX, t: w})
		}
		for _, w := range lay.input {
			ops = append(ops, qop{kind: qH, t: w})
		}
	}
	return ops, bc, lay, nil
}

// qasmRender emits OpenQASM 2.0. measure lists the wires to measure, in
// classical-bit order: measure[i] -> c[i].
func qasmRender(header string, ops []qop, nwires int, measure []int) string {
	var b strings.Builder
	b.WriteString("OPENQASM 2.0;\n")
	b.WriteString("include \"qelib1.inc\";\n")
	for _, line := range strings.Split(strings.TrimRight(header, "\n"), "\n") {
		if line != "" {
			fmt.Fprintf(&b, "// %s\n", line)
		}
	}
	fmt.Fprintf(&b, "qreg q[%d];\n", nwires)
	if len(measure) > 0 {
		fmt.Fprintf(&b, "creg c[%d];\n", len(measure))
	}
	for _, op := range ops {
		switch op.kind {
		case qX:
			fmt.Fprintf(&b, "x q[%d];\n", op.t)
		case qCX:
			fmt.Fprintf(&b, "cx q[%d],q[%d];\n", op.a, op.t)
		case qCCX:
			fmt.Fprintf(&b, "ccx q[%d],q[%d],q[%d];\n", op.a, op.b, op.t)
		case qH:
			fmt.Fprintf(&b, "h q[%d];\n", op.t)
		}
	}
	for i, w := range measure {
		fmt.Fprintf(&b, "measure q[%d] -> c[%d];\n", w, i)
	}
	return b.String()
}

// qasmProgram exports any compilable reversible program as a plain classical
// circuit (no H, all named registers measured).
func qasmProgram(bc *bitCircuit) string {
	names := sortedNames(bc.base)
	var head strings.Builder
	head.WriteString("kraM reversible program as a classical X/CNOT/Toffoli circuit\n")
	var measure []int
	for _, n := range names {
		base := bc.base[n]
		fmt.Fprintf(&head, "%s: q[%d..%d] little-endian (bit i of %s = q[%d+i])\n",
			n, base, base+bc.width-1, n, base)
		for b := 0; b < bc.width; b++ {
			measure = append(measure, base+b)
		}
	}
	return qasmRender(head.String(), bitGateOps(bc.gates), bc.nwires, measure)
}

// qasmGrover exports the full Grover circuit, measuring only the input
// register. Bit i of x lands in c[i] (little-endian); Qiskit prints classical
// registers MSB-first, so int(bitstring, 2) recovers x directly.
func qasmGrover(cond Node, condSrc string, width, iters int) (string, error) {
	ops, bc, lay, err := groverOps(cond, width, iters)
	if err != nil {
		return "", err
	}
	head := fmt.Sprintf(
		"Grover search: %s  (%d-bit %s, %d iterations)\n"+
			"input %s: q[0..%d] little-endian; marker q[%d]; rest ancilla\n"+
			"c[i] = bit i of %s — int(bitstring, 2) in Qiskit yields %s",
		condSrc, width, lay.varName, iters,
		lay.varName, width-1, lay.marker, lay.varName, lay.varName)
	return qasmRender(head, ops, bc.nwires, lay.input), nil
}

func sortedNames(base map[string]int) []string {
	names := make([]string, 0, len(base))
	for n := range base {
		names = append(names, n)
	}
	// order by wire index so the header reads in layout order
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if base[names[j]] < base[names[i]] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	return names
}

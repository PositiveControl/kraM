package main

import (
	"fmt"
	"math/bits"
	"strings"
)

// Bernstein–Vazirani: recover a hidden n-bit string s from the linear oracle
// f(x) = s·x (mod 2) in a single query. The oracle is one CNOT per set bit of
// s into a |-> marker; H layers before and after rotate the phase pattern
// (-1)^(s·x) back into the computational basis, so the measurement yields s
// with probability 1. Classically the same recovery takes n queries.
//
// Layout matches the Grover convention: input x on q[0..n-1] little-endian,
// marker on q[n], c[i] = bit i of s.

// bvOps builds the complete circuit as qops.
func bvOps(width int, s int) []qop {
	marker := width
	var ops []qop
	for w := 0; w < width; w++ {
		ops = append(ops, qop{kind: qH, t: w})
	}
	ops = append(ops, qop{kind: qX, t: marker}, qop{kind: qH, t: marker})
	for w := 0; w < width; w++ {
		if s>>w&1 == 1 {
			ops = append(ops, qop{kind: qCX, a: w, t: marker})
		}
	}
	for w := 0; w < width; w++ {
		ops = append(ops, qop{kind: qH, t: w})
	}
	return ops
}

// bvParse handles the argument form "<bits> <s> [qasm]".
func bvParse(args string) (width, s int, asQasm bool, err error) {
	usage := fmt.Errorf("usage: :bv <bits> <s> [qasm]  (s = hidden string as a decimal, 0 <= s < 2^bits)")
	fields := strings.Fields(args)
	if len(fields) == 3 && fields[2] == "qasm" {
		asQasm = true
		fields = fields[:2]
	}
	if len(fields) != 2 {
		return 0, 0, false, usage
	}
	if _, err := fmt.Sscanf(fields[0]+" "+fields[1], "%d %d", &width, &s); err != nil {
		return 0, 0, false, usage
	}
	if width < 1 || width > bitWidth {
		return 0, 0, false, fmt.Errorf("bits must be 1..%d, got %d", bitWidth, width)
	}
	if s < 0 || s >= 1<<width {
		return 0, 0, false, fmt.Errorf("s=%d does not fit in %d bits", s, width)
	}
	return width, s, asQasm, nil
}

// bvCommand parses the REPL argument form and renders the report or QASM.
func bvCommand(args string) (string, error) {
	width, s, asQasm, err := bvParse(args)
	if err != nil {
		return "", err
	}
	if asQasm {
		return qasmBV(width, s), nil
	}
	return bvReport(width, s), nil
}

// qasmBV exports the full circuit, measuring only the input register.
func qasmBV(width, s int) string {
	head := fmt.Sprintf(
		"Bernstein-Vazirani: recover s=%d (%0*b) in one oracle query\n"+
			"input x: q[0..%d] little-endian; marker q[%d]\n"+
			"c[i] = bit i of s — int(bitstring, 2) in Qiskit yields s",
		s, width, s, width-1, width)
	measure := make([]int, width)
	for i := range measure {
		measure[i] = i
	}
	return qasmRender(head, bvOps(width, s), width+1, measure)
}

// bvReport renders the REPL text. The outcome is deterministic — after the
// final H layer the state is exactly |s> — so the report explains the query
// count rather than charting a distribution.
func bvReport(width, s int) string {
	ops := bvOps(width, s)
	var b strings.Builder
	fmt.Fprintf(&b, "oracle: f(x) = s·x (mod 2), s = %d (%0*b) — %d CNOTs, %d wires (%d input + 1 marker)\n",
		s, width, s, bits.OnesCount(uint(s)), width+1, width)
	fmt.Fprintf(&b, "circuit: %d gates total (H layers + phase-kickback oracle)\n", len(ops))
	fmt.Fprintf(&b, "classical: %d queries (probe f at each power of two)\n", width)
	fmt.Fprintf(&b, "quantum:   1 query — measurement yields s = %d with probability 1\n", s)
	return strings.TrimRight(b.String(), "\n")
}

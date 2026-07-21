package main

import (
	"fmt"
	"strings"
)

// Deutsch–Jozsa: given a promise that f is constant or balanced, one oracle
// query decides which (classically: 2^(n-1)+1 evaluations worst case).
// Circuit: H on inputs, |-> marker, bit-flip oracle, H on inputs, measure.
// Measuring all zeros means constant; anything else means balanced.
//
// The oracle is any compiled kraM condition — same synthesizer as :grover.
// Conditions that are neither constant nor balanced break the promise; the
// circuit still runs, and P(all zeros) = ((N-2M)/N)^2 interpolates between
// the two verdicts, so the report warns instead of refusing.

// djOps builds the complete circuit as qops.
func djOps(cond Node, width int) ([]qop, *bitCircuit, oracleLayout, error) {
	bc, lay, err := compileOracle(cond, width)
	if err != nil {
		return nil, nil, oracleLayout{}, err
	}
	var ops []qop
	for _, w := range lay.input {
		ops = append(ops, qop{kind: qH, t: w})
	}
	ops = append(ops, qop{kind: qX, t: lay.marker}, qop{kind: qH, t: lay.marker})
	ops = append(ops, bitGateOps(bc.gates)...)
	for _, w := range lay.input {
		ops = append(ops, qop{kind: qH, t: w})
	}
	return ops, bc, lay, nil
}

// djCommand parses the REPL argument form "<bits> <cond> [qasm]".
func djCommand(args string) (string, error) {
	fields := strings.SplitN(args, " ", 2)
	var width int
	if len(fields) < 2 || len(fields[0]) == 0 {
		return "", fmt.Errorf("usage: :dj <bits> <condition> [qasm]")
	}
	if _, err := fmt.Sscanf(fields[0], "%d", &width); err != nil {
		return "", fmt.Errorf("usage: :dj <bits> <condition> [qasm]")
	}
	condSrc := strings.TrimSpace(fields[1])
	asQasm := false
	if strings.HasSuffix(condSrc, " qasm") {
		asQasm = true
		condSrc = strings.TrimSpace(strings.TrimSuffix(condSrc, " qasm"))
	}
	cond, warn, err := parseCond(condSrc, width)
	if err != nil {
		return "", err
	}
	if asQasm {
		text, err := qasmDJ(cond, condSrc, width)
		if err != nil {
			return "", err
		}
		if warn != "" {
			text = "// " + warn + "\n" + text
		}
		return text, nil
	}
	return djReport(cond, condSrc, width, warn)
}

// qasmDJ exports the full circuit, measuring only the input register.
func qasmDJ(cond Node, condSrc string, width int) (string, error) {
	ops, bc, lay, err := djOps(cond, width)
	if err != nil {
		return "", err
	}
	head := fmt.Sprintf(
		"Deutsch-Jozsa: is %s constant or balanced over %d-bit %s? One oracle query\n"+
			"input %s: q[0..%d] little-endian; marker q[%d]; rest ancilla\n"+
			"all c bits zero -> constant; anything else -> balanced",
		condSrc, width, lay.varName, lay.varName, width-1, lay.marker)
	return qasmRender(head, ops, bc.nwires, lay.input), nil
}

// djReport classifies the condition from its truth table and reports the
// exact P(all zeros) the circuit produces.
func djReport(cond Node, condSrc string, width int, warn string) (string, error) {
	bc, lay, err := compileOracle(cond, width)
	if err != nil {
		return "", err
	}
	table, err := oracleTruthTable(bc, lay)
	if err != nil {
		return "", err
	}
	n, m := len(table), 0
	for _, hit := range table {
		if hit {
			m++
		}
	}
	bias := float64(n-2*m) / float64(n)
	p0 := bias * bias

	var b strings.Builder
	if warn != "" {
		fmt.Fprintln(&b, warn)
	}
	fmt.Fprintf(&b, "oracle: %s over %d-bit %s — %d gates, %d wires (%d input + 1 marker + %d ancilla)\n",
		condSrc, width, lay.varName, len(bc.gates), bc.nwires, width, bc.nwires-width-1)
	fmt.Fprintf(&b, "truth table: %d of %d inputs true\n", m, n)
	switch {
	case m == 0 || m == n:
		fmt.Fprintln(&b, "verdict: CONSTANT — one query measures all zeros with probability 1")
	case 2*m == n:
		fmt.Fprintln(&b, "verdict: BALANCED — one query measures a nonzero state with probability 1")
	default:
		fmt.Fprintf(&b, "verdict: NEITHER — promise broken; P(all zeros) = %.4f (1 = constant, 0 = balanced)\n", p0)
	}
	fmt.Fprintf(&b, "classical worst case: %d queries\n", n/2+1)
	return strings.TrimRight(b.String(), "\n"), nil
}

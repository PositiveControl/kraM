package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// This file turns a kraM condition into a quantum search demo. compileOracle
// lowers the condition to a garbage-free bit-flip oracle (marker ^= cond(x));
// groverRun then evolves Grover amplitudes over the 2^width input values.
//
// Why simulating only the input register is exact, not an approximation: the
// oracle restores every ancilla to zero for every basis state (checked
// exhaustively by oracleTruthTable), and the marker wire is only ever a gate
// *target*, never a control. So in the real quantum circuit the ancillas
// factor out as |0...0> and the marker as |->, and the amplitude evolution
// over the 2^width inputs is identical to the full statevector's. All Grover
// operators here are real matrices, so float64 amplitudes suffice.

// oracleLayout records the wire roles in a compiled oracle.
type oracleLayout struct {
	varName string
	input   []int // wires 0..width-1, little-endian bits of x
	marker  int   // wire width; ends as cond(x)
}

// compileOracle lowers a bare condition with exactly one free variable to a
// bit oracle. The input register is pinned to wires 0..width-1 and the marker
// to wire width, so downstream consumers (QASM export, charts) have a fixed,
// human-readable layout.
func compileOracle(cond Node, width int) (*bitCircuit, oracleLayout, error) {
	if width < 1 || width > bitWidth {
		return nil, oracleLayout{}, fmt.Errorf("bits must be 1..%d, got %d", bitWidth, width)
	}
	var vars []string
	collectCondVars(cond, func(name string) {
		for _, v := range vars {
			if v == name {
				return
			}
		}
		vars = append(vars, name)
	})
	if len(vars) != 1 {
		return nil, oracleLayout{}, fmt.Errorf("condition must use exactly one variable, got %d (%s)",
			len(vars), strings.Join(vars, ", "))
	}
	c := &bitCircuit{base: map[string]int{}, procs: map[string]ProcDef{}, width: width}
	lay := oracleLayout{varName: vars[0], input: c.reg(vars[0])}
	lay.marker = c.alloc(1)[0]
	if err := c.condToBit(cond, lay.marker); err != nil {
		return nil, oracleLayout{}, err
	}
	return c, lay, nil
}

// oracleTruthTable evaluates the oracle classically for every basis state and
// doubles as an exhaustive cleanliness check: any leftover garbage on a
// non-input, non-marker wire (or a clobbered input) would make the quantum
// evolution diverge from the fast simulation, so it is a hard error.
func oracleTruthTable(bc *bitCircuit, lay oracleLayout) ([]bool, error) {
	n := len(lay.input)
	table := make([]bool, 1<<n)
	for x := 0; x < 1<<n; x++ {
		init := make([]bool, bc.nwires)
		for b := 0; b < n; b++ {
			init[lay.input[b]] = (x>>b)&1 == 1
		}
		out := simulateBits(bc.gates, bc.nwires, init)
		for w := 0; w < bc.nwires; w++ {
			if w == lay.marker {
				continue
			}
			if out[w] != init[w] {
				return nil, fmt.Errorf("oracle not garbage-free: wire %d dirty for x=%d", w, x)
			}
		}
		table[x] = out[lay.marker]
	}
	return table, nil
}

type groverResult struct {
	N, M    int
	Optimal int
	Iters   int
	Marked  []int
	Success []float64 // marked probability after 0..Iters iterations
	Final   []float64 // full distribution after Iters
}

// groverOptimal is ⌊π/(4·asin(√(M/N)))⌋ — the asin form stays correct when M
// is a large fraction of N, where the common √(N/M) approximation overshoots.
func groverOptimal(m, n int) int {
	if m == 0 || 2*m >= n {
		return 0
	}
	theta := math.Asin(math.Sqrt(float64(m) / float64(n)))
	return int(math.Floor(math.Pi / (4 * theta)))
}

// groverRun evolves real amplitudes: sign-flip marked states (phase oracle),
// then invert about the mean (diffusion). iters < 0 means "optimal".
func groverRun(table []bool, iters int) groverResult {
	n := len(table)
	r := groverResult{N: n}
	for x, hit := range table {
		if hit {
			r.Marked = append(r.Marked, x)
			r.M++
		}
	}
	r.Optimal = groverOptimal(r.M, n)
	if iters < 0 {
		iters = r.Optimal
	}
	r.Iters = iters

	amp := make([]float64, n)
	for i := range amp {
		amp[i] = 1 / math.Sqrt(float64(n))
	}
	success := func() float64 {
		p := 0.0
		for _, x := range r.Marked {
			p += amp[x] * amp[x]
		}
		return p
	}
	r.Success = append(r.Success, success())
	for k := 0; k < iters; k++ {
		for _, x := range r.Marked {
			amp[x] = -amp[x]
		}
		mean := 0.0
		for _, a := range amp {
			mean += a
		}
		mean /= float64(n)
		for i := range amp {
			amp[i] = 2*mean - amp[i]
		}
		r.Success = append(r.Success, success())
	}
	r.Final = make([]float64, n)
	for i, a := range amp {
		r.Final[i] = a * a
	}
	return r
}

// parseCond parses src as a single bare condition expression. It also warns
// (second return) about constants that cannot fit in width bits — legal, but
// they silently make the condition unsatisfiable or trivial.
func parseCond(src string, width int) (Node, string, error) {
	ast, err := Parse(src)
	if err != nil {
		return nil, "", err
	}
	cond := ast
	if b, ok := ast.(Block); ok {
		if len(b.Stmts) != 1 {
			return nil, "", fmt.Errorf("expected a single condition, got %d statements", len(b.Stmts))
		}
		cond = b.Stmts[0]
	}
	switch cond.(type) {
	case Binary, Unary:
	default:
		return nil, "", fmt.Errorf("expected a condition (comparison or && / || / ! of comparisons)")
	}
	warn := ""
	var walk func(Node)
	walk = func(n Node) {
		switch v := n.(type) {
		case NumberLit:
			if v.Val < 0 || v.Val >= float64(int64(1)<<width) {
				warn = fmt.Sprintf("warning: constant %g does not fit in %d bits (register is mod 2^%d)",
					v.Val, width, width)
			}
		case Binary:
			walk(v.Left)
			walk(v.Right)
		case Unary:
			walk(v.Right)
		}
	}
	walk(cond)
	return cond, warn, nil
}

// groverPrep compiles + tabulates + runs: the shared front half of the REPL
// report and the WASM bridge.
func groverPrep(condSrc string, width, iters int) (*bitCircuit, oracleLayout, groverResult, string, error) {
	cond, warn, err := parseCond(condSrc, width)
	if err != nil {
		return nil, oracleLayout{}, groverResult{}, "", err
	}
	bc, lay, err := compileOracle(cond, width)
	if err != nil {
		return nil, oracleLayout{}, groverResult{}, "", err
	}
	table, err := oracleTruthTable(bc, lay)
	if err != nil {
		return nil, oracleLayout{}, groverResult{}, "", err
	}
	return bc, lay, groverRun(table, iters), warn, nil
}

// groverCommand parses the REPL argument form "<bits> <cond> [iters=<k>]".
func groverCommand(args string) (string, error) {
	fields := strings.SplitN(args, " ", 2)
	var width int
	if len(fields) < 2 || len(fields[0]) == 0 {
		return "", fmt.Errorf("usage: :grover <bits> <condition> [iters=<k>]")
	}
	if _, err := fmt.Sscanf(fields[0], "%d", &width); err != nil {
		return "", fmt.Errorf("usage: :grover <bits> <condition> [iters=<k>]")
	}
	condSrc := strings.TrimSpace(fields[1])
	iters := -1
	if i := strings.LastIndex(condSrc, "iters="); i >= 0 {
		if _, err := fmt.Sscanf(condSrc[i:], "iters=%d", &iters); err != nil || iters < 0 {
			return "", fmt.Errorf("bad iters= value")
		}
		condSrc = strings.TrimSpace(condSrc[:i])
	}
	return groverReport(condSrc, width, iters)
}

// groverReport renders the REPL text: oracle stats, per-iteration success
// probabilities, and (for small widths) the final distribution's top states.
func groverReport(condSrc string, width, iters int) (string, error) {
	bc, lay, r, warn, err := groverPrep(condSrc, width, iters)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if warn != "" {
		fmt.Fprintln(&b, warn)
	}
	fmt.Fprintf(&b, "oracle: %s over %d-bit %s — %d gates, %d wires (%d input + 1 marker + %d ancilla)\n",
		condSrc, width, lay.varName, len(bc.gates), bc.nwires, width, bc.nwires-width-1)
	fmt.Fprintf(&b, "search space N=%d, solutions M=%d", r.N, r.M)
	if r.M > 0 && r.M <= 8 {
		fmt.Fprintf(&b, " %v", r.Marked)
	}
	fmt.Fprintln(&b)
	switch {
	case r.M == 0:
		fmt.Fprintln(&b, "no solutions — the oracle never fires, amplitudes stay uniform")
	case 2*r.M >= r.N:
		fmt.Fprintf(&b, "M ≥ N/2 — measure immediately: success ≥ %.3f without any iterations\n",
			float64(r.M)/float64(r.N))
	default:
		fmt.Fprintf(&b, "optimal iterations k* = %d\n", r.Optimal)
	}
	for k, p := range r.Success {
		bar := strings.Repeat("█", int(p*40+0.5))
		fmt.Fprintf(&b, "  after %2d iterations: P(marked) = %.4f %s\n", k, p, bar)
	}
	if width <= 8 && r.M > 0 {
		type st struct {
			x int
			p float64
		}
		var top []st
		for x, p := range r.Final {
			top = append(top, st{x, p})
		}
		sort.Slice(top, func(i, j int) bool { return top[i].p > top[j].p })
		fmt.Fprintf(&b, "top states after %d iterations:\n", r.Iters)
		for i := 0; i < len(top) && i < 4; i++ {
			fmt.Fprintf(&b, "  %s=%-4d p=%.4f\n", lay.varName, top[i].x, top[i].p)
		}
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

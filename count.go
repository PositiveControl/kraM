package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Quantum counting: how many x satisfy the condition, without finding them.
// Phase estimation on the Grover operator G — G rotates the 2D span of
// {uniform, marked} by 2θ where sin²θ = M/N, so a t-qubit QPE readout y
// estimates θ ≈ π·y/2^t and therefore M ≈ N·sin²(πy/2^t).
//
// The report computes the exact measurement distribution analytically rather
// than gate-by-gate: the uniform start state has overlap exactly 1/2 with
// each of G's two eigenvectors (e^{±i2θ}), so P(y) is the equal mix of the
// two standard QPE kernels. countCrossCheck in tests validates this against
// a direct unitary simulation (explicit G^k powers + DFT over the counting
// register). No QASM export: controlled-Grover needs gates outside qelib1,
// and QPE depth is far beyond today's hardware — simulator only.

// qpeKernel is the textbook QPE distribution for eigenphase fraction phi:
// P(y) = sin²(2^t·π·δ) / (4^t·sin²(π·δ)), δ = phi - y/2^t.
func qpeKernel(phi float64, t uint, y int) float64 {
	T := float64(int(1) << t)
	delta := phi - float64(y)/T
	// δ an integer means exact phase: all the mass lands here.
	if d := delta - math.Round(delta); math.Abs(d) < 1e-15 {
		return 1
	}
	s := math.Sin(math.Pi * delta)
	num := math.Sin(T * math.Pi * delta)
	return num * num / (T * T * s * s)
}

type countResult struct {
	N, M    int     // true values from the truth table
	T       uint    // counting qubits
	Theta   float64 // asin(sqrt(M/N))
	P       []float64
	Best    int     // most likely y
	BestP   float64
	MHat    float64 // N·sin²(π·best/2^t)
}

// countRun builds the exact QPE outcome distribution over y = 0..2^t-1.
func countRun(table []bool, t uint) countResult {
	n := len(table)
	m := 0
	for _, hit := range table {
		if hit {
			m++
		}
	}
	theta := math.Asin(math.Sqrt(float64(m) / float64(n)))
	phi := theta / math.Pi // eigenphase 2θ as a fraction of 2π
	r := countResult{N: n, M: m, T: t, Theta: theta}
	r.P = make([]float64, 1<<t)
	for y := range r.P {
		r.P[y] = 0.5*qpeKernel(phi, t, y) + 0.5*qpeKernel(-phi, t, y)
		if r.P[y] > r.BestP {
			r.Best, r.BestP = y, r.P[y]
		}
	}
	r.MHat = r.estimate(r.Best)
	return r
}

// estimate maps a readout y to an M estimate. y and 2^t-y encode ±θ, the
// same count.
func (r countResult) estimate(y int) float64 {
	T := 1 << r.T
	if y > T/2 {
		y = T - y
	}
	s := math.Sin(math.Pi * float64(y) / float64(T))
	return float64(r.N) * s * s
}

// countCommand parses the REPL argument form "<bits> <cond> [t=<k>]".
func countCommand(args string) (string, error) {
	fields := strings.SplitN(args, " ", 2)
	var width int
	if len(fields) < 2 || len(fields[0]) == 0 {
		return "", fmt.Errorf("usage: :count <bits> <condition> [t=<k>]")
	}
	if _, err := fmt.Sscanf(fields[0], "%d", &width); err != nil {
		return "", fmt.Errorf("usage: :count <bits> <condition> [t=<k>]")
	}
	condSrc := strings.TrimSpace(fields[1])
	t := width + 2 // resolves every θ at this N well enough to round to the true M
	if i := strings.LastIndex(condSrc, "t="); i >= 0 {
		if _, err := fmt.Sscanf(condSrc[i:], "t=%d", &t); err != nil || t < 1 || t > 20 {
			return "", fmt.Errorf("bad t= value (1..20)")
		}
		condSrc = strings.TrimSpace(condSrc[:i])
	}
	cond, warn, err := parseCond(condSrc, width)
	if err != nil {
		return "", err
	}
	bc, lay, err := compileOracle(cond, width)
	if err != nil {
		return "", err
	}
	table, err := oracleTruthTable(bc, lay)
	if err != nil {
		return "", err
	}
	r := countRun(table, uint(t))
	return countReport(condSrc, width, lay, bc, r, warn), nil
}

// countReport renders the REPL text: the estimate distribution aggregated by
// M̂ (y and 2^t-y fold together), the winner, and the true M it is chasing.
func countReport(condSrc string, width int, lay oracleLayout, bc *bitCircuit, r countResult, warn string) string {
	var b strings.Builder
	if warn != "" {
		fmt.Fprintln(&b, warn)
	}
	fmt.Fprintf(&b, "oracle: %s over %d-bit %s — %d gates, %d wires\n",
		condSrc, width, lay.varName, len(bc.gates), bc.nwires)
	fmt.Fprintf(&b, "counting register: t=%d qubits — %d controlled-Grover applications, phase resolution 1/%d\n",
		r.T, (1<<r.T)-1, 1<<r.T)
	fmt.Fprintf(&b, "true count (from truth table): M=%d of N=%d\n", r.M, r.N)

	// Aggregate P(y) by the rounded estimate each y maps to.
	byM := map[int]float64{}
	for y, p := range r.P {
		byM[int(math.Round(r.estimate(y)))] += p
	}
	type est struct {
		m int
		p float64
	}
	var ests []est
	for m, p := range byM {
		ests = append(ests, est{m, p})
	}
	sort.Slice(ests, func(i, j int) bool { return ests[i].p > ests[j].p })
	fmt.Fprintln(&b, "measurement outcome, aggregated by the M it estimates:")
	for i, e := range ests {
		if i == 4 || e.p < 0.005 {
			break
		}
		bar := strings.Repeat("█", int(e.p*40+0.5))
		fmt.Fprintf(&b, "  M̂=%-4d p=%.4f %s\n", e.m, e.p, bar)
	}
	verdict := "correct"
	if int(math.Round(r.MHat)) != r.M {
		verdict = fmt.Sprintf("off by %d", int(math.Round(r.MHat))-r.M)
	}
	fmt.Fprintf(&b, "most likely readout: y=%d → M̂ = %.2f → %d (%s)",
		r.Best, r.MHat, int(math.Round(r.MHat)), verdict)
	return b.String()
}

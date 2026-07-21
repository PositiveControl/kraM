package main

import (
	"fmt"
	"math/bits"
	"math/rand"
	"strings"
)

// Simon's problem: f satisfies f(x) = f(x⊕s) for a hidden s ≠ 0 (and is
// otherwise 2-to-1). Each circuit run measures a uniformly random y with
// y·s = 0 (mod 2); after enough runs the y's span s's orthogonal complement
// and Gaussian elimination over GF(2) recovers s. Quantum: O(n) queries.
// Classical: Ω(√2^n) (birthday bound on finding a collision).
//
// The oracle is the standard construction f(x) = x ⊕ (x_l = 1 ? s : 0),
// l = lowest set bit of s: a CNOT copy of x into the output register, then
// s XORed in controlled on x_l. Promise check: x_l flips along x → x⊕s, so
// exactly the pairs {x, x⊕s} collide. All CNOTs — shallow enough for real
// hardware, like :bv.
//
// Layout: input x on q[0..n-1] little-endian, output f(x) on q[n..2n-1].
// Only the input register is measured (after the final H layer): c = y.

// simonOps builds the complete circuit as qops.
func simonOps(width, s int) []qop {
	var ops []qop
	for w := 0; w < width; w++ {
		ops = append(ops, qop{kind: qH, t: w})
	}
	for w := 0; w < width; w++ {
		ops = append(ops, qop{kind: qCX, a: w, t: width + w})
	}
	if s != 0 {
		l := bits.TrailingZeros(uint(s))
		for w := 0; w < width; w++ {
			if s>>w&1 == 1 {
				ops = append(ops, qop{kind: qCX, a: l, t: width + w})
			}
		}
	}
	for w := 0; w < width; w++ {
		ops = append(ops, qop{kind: qH, t: w})
	}
	return ops
}

// simonF is the oracle's classical truth function, used for the exhaustive
// promise check in the report.
func simonF(width, s, x int) int {
	f := x
	if s != 0 && x>>bits.TrailingZeros(uint(s))&1 == 1 {
		f ^= s
	}
	return f
}

// gf2Rank row-reduces the equations in place and returns the rank.
func gf2Rank(rows []int, width int) int {
	rank := 0
	for col := width - 1; col >= 0; col-- {
		pivot := -1
		for i := rank; i < len(rows); i++ {
			if rows[i]>>col&1 == 1 {
				pivot = i
				break
			}
		}
		if pivot < 0 {
			continue
		}
		rows[rank], rows[pivot] = rows[pivot], rows[rank]
		for i := range rows {
			if i != rank && rows[i]>>col&1 == 1 {
				rows[i] ^= rows[rank]
			}
		}
		rank++
	}
	return rank
}

// gf2NullVector returns the unique nonzero solution of y·s = 0 for all
// reduced rows, given rank = width-1. Free column = any column without a
// pivot; set it to 1 and back-substitute.
func gf2NullVector(rows []int, width, rank int) int {
	pivotCol := map[int]int{} // column -> row
	for i := 0; i < rank; i++ {
		pivotCol[bits.Len(uint(rows[i]))-1] = i
	}
	free := -1
	for col := 0; col < width; col++ {
		if _, ok := pivotCol[col]; !ok {
			free = col
			break
		}
	}
	s := 1 << free
	for col, i := range pivotCol {
		// pivot row i: y_col = parity of the row's other set bits under s
		if bits.OnesCount(uint(rows[i]&s))&1 == 1 {
			s |= 1 << col
		}
	}
	return s
}

// simonSolve draws measurement outcomes until the equations pin s down.
// Sampling y uniformly from {y : y·s = 0} is exactly what the circuit
// produces — TestSimonStatevector verifies that equivalence gate-for-gate.
func simonSolve(width, s int, rng *rand.Rand) (ys []int, recovered int, err error) {
	var rows []int
	for tries := 0; tries < 64*width; tries++ {
		y := rng.Intn(1 << width)
		if bits.OnesCount(uint(y&s))&1 == 1 {
			continue // never produced by the circuit
		}
		ys = append(ys, y)
		rows = append(rows, y)
		rank := gf2Rank(rows, width)
		rows = rows[:rank]
		if rank == width {
			return ys, 0, nil // only s=0 is orthogonal to everything
		}
		if rank == width-1 {
			// Null space is {0, candidate}. One classical verification query
			// settles which (the textbook algorithm's final step): if s were
			// the candidate, f(0) and f(candidate) must collide. When s = 0
			// the candidate is a false positive — keep sampling toward full
			// rank.
			candidate := gf2NullVector(rows, width, rank)
			if simonF(width, s, 0) == simonF(width, s, candidate) {
				return ys, candidate, nil
			}
		}
	}
	return nil, 0, fmt.Errorf("rank stalled — astronomically unlikely with honest sampling")
}

// simonCommand parses the REPL argument form "<bits> <s> [qasm]".
func simonCommand(args string) (string, error) {
	usage := fmt.Errorf("usage: :simon <bits> <s> [qasm]  (s = hidden period as a decimal, 0 <= s < 2^bits)")
	fields := strings.Fields(args)
	asQasm := false
	if len(fields) == 3 && fields[2] == "qasm" {
		asQasm = true
		fields = fields[:2]
	}
	if len(fields) != 2 {
		return "", usage
	}
	var width, s int
	if _, err := fmt.Sscanf(fields[0]+" "+fields[1], "%d %d", &width, &s); err != nil {
		return "", usage
	}
	if width < 1 || width > bitWidth/2 {
		return "", fmt.Errorf("bits must be 1..%d (circuit uses 2*bits wires), got %d", bitWidth/2, width)
	}
	if s < 0 || s >= 1<<width {
		return "", fmt.Errorf("s=%d does not fit in %d bits", s, width)
	}
	if asQasm {
		return qasmSimon(width, s), nil
	}
	return simonReport(width, s)
}

// qasmSimon exports the full circuit, measuring only the input register.
func qasmSimon(width, s int) string {
	head := fmt.Sprintf(
		"Simon: hidden XOR period s=%d (%0*b); f(x) = x XOR (bit %d of x ? s : 0)\n"+
			"input x: q[0..%d] little-endian; output f(x): q[%d..%d]\n"+
			"every measured y satisfies y.s = 0 (mod 2); ~%d runs + GF(2) elimination recover s",
		s, width, s, bits.TrailingZeros(uint(s|1<<width)), width-1, width, 2*width-1, width+3)
	measure := make([]int, width)
	for i := range measure {
		measure[i] = i
	}
	return qasmRender(head, simonOps(width, s), 2*width, measure)
}

// simonReport verifies the promise exhaustively, then runs the full
// sample-and-eliminate loop and shows every equation it used.
func simonReport(width, s int) (string, error) {
	for x := 0; x < 1<<width; x++ {
		if simonF(width, s, x) != simonF(width, s, x^s) {
			return "", fmt.Errorf("oracle broken: f(%d) != f(%d)", x, x^s)
		}
	}
	ys, recovered, err := simonSolve(width, s, rand.New(rand.NewSource(rand.Int63())))
	if err != nil {
		return "", err
	}
	ops := simonOps(width, s)
	var b strings.Builder
	fmt.Fprintf(&b, "oracle: f(x) = x ⊕ (x·%d ? %d : 0) — %d gates, %d wires (%d input + %d output)\n",
		1<<bits.TrailingZeros(uint(s|1<<width)), s, len(ops), 2*width, width, width)
	fmt.Fprintf(&b, "promise f(x) = f(x⊕s) verified for all %d inputs\n", 1<<width)
	fmt.Fprintf(&b, "circuit runs (each yields a random y with y·s = 0):\n")
	for i, y := range ys {
		fmt.Fprintf(&b, "  run %2d: y = %0*b\n", i+1, width, y)
	}
	if recovered == 0 {
		fmt.Fprintf(&b, "equations span the full space → s = 0 (f is injective)")
	} else {
		verdict := "correct"
		if recovered != s {
			verdict = "WRONG"
		}
		fmt.Fprintf(&b, "GF(2) elimination: null space = {%0*b} → s = %d (%s)\n",
			width, recovered, recovered, verdict)
		fmt.Fprintf(&b, "%d quantum runs vs ~2^%.1f classical queries (birthday bound)",
			len(ys), float64(width)/2)
	}
	return b.String(), nil
}

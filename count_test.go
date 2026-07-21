package main

import (
	"math"
	"math/cmplx"
	"strings"
	"testing"
)

// qpeDirect is the independent check: an explicit unitary simulation of QPE
// on the Grover operator. It applies G^k to the uniform state for every
// counting-register value k, DFTs over k, and sums outcome probabilities —
// no 2D-subspace shortcut, no analytic kernel.
func qpeDirect(table []bool, t uint) []float64 {
	n := len(table)
	K := 1 << t

	applyG := func(v []float64) []float64 {
		out := make([]float64, n)
		mean := 0.0
		for x, a := range v {
			if table[x] {
				a = -a
			}
			out[x] = a
			mean += a
		}
		mean /= float64(n)
		for x := range out {
			out[x] = 2*mean - out[x]
		}
		return out
	}

	powers := make([][]float64, K)
	powers[0] = make([]float64, n)
	for x := range powers[0] {
		powers[0][x] = 1 / math.Sqrt(float64(n))
	}
	for k := 1; k < K; k++ {
		powers[k] = applyG(powers[k-1])
	}

	P := make([]float64, K)
	for y := 0; y < K; y++ {
		for x := 0; x < n; x++ {
			var amp complex128
			for k := 0; k < K; k++ {
				phase := -2 * math.Pi * float64(k) * float64(y) / float64(K)
				amp += cmplx.Exp(complex(0, phase)) * complex(powers[k][x], 0)
			}
			amp /= complex(float64(K), 0)
			P[y] += real(amp)*real(amp) + imag(amp)*imag(amp)
		}
	}
	return P
}

// TestCountMatchesDirectSim: the analytic distribution must match the
// explicit unitary simulation for constant, sparse, and dense oracles.
func TestCountMatchesDirectSim(t *testing.T) {
	cases := []struct {
		cond  string
		width int
		t     uint
	}{
		{"x == 5", 3, 5},
		{"x < 4", 3, 4},
		{"x >= 0", 3, 4},        // M = N
		{"x < 0", 3, 4},         // M = 0
		{"x == 9 || x == 3", 4, 6},
		{"x != 2", 4, 6},
	}
	for _, c := range cases {
		cond, _, err := parseCond(c.cond, c.width)
		if err != nil {
			t.Fatalf("%s: %v", c.cond, err)
		}
		bc, lay, err := compileOracle(cond, c.width)
		if err != nil {
			t.Fatalf("%s: %v", c.cond, err)
		}
		table, err := oracleTruthTable(bc, lay)
		if err != nil {
			t.Fatalf("%s: %v", c.cond, err)
		}
		got := countRun(table, c.t)
		want := qpeDirect(table, c.t)
		sum := 0.0
		for y := range want {
			if math.Abs(got.P[y]-want[y]) > 1e-9 {
				t.Fatalf("%s t=%d: P(%d)=%g, direct sim says %g", c.cond, c.t, y, got.P[y], want[y])
			}
			sum += got.P[y]
		}
		if math.Abs(sum-1) > 1e-9 {
			t.Fatalf("%s: distribution sums to %g", c.cond, sum)
		}
	}
}

// TestCountEstimatesTrueM: at the default resolution the most likely readout
// must round to the exact count.
func TestCountEstimatesTrueM(t *testing.T) {
	cases := []struct {
		cond  string
		width int
	}{
		{"x == 5", 3},
		{"x < 4", 3},
		{"x >= 0", 3},
		{"x < 0", 3},
		{"x == 9 || x == 3", 4},
		{"x >= 3 && x <= 5", 4},
	}
	for _, c := range cases {
		cond, _, err := parseCond(c.cond, c.width)
		if err != nil {
			t.Fatalf("%s: %v", c.cond, err)
		}
		bc, lay, err := compileOracle(cond, c.width)
		if err != nil {
			t.Fatalf("%s: %v", c.cond, err)
		}
		table, err := oracleTruthTable(bc, lay)
		if err != nil {
			t.Fatalf("%s: %v", c.cond, err)
		}
		m := 0
		for _, hit := range table {
			if hit {
				m++
			}
		}
		r := countRun(table, uint(c.width+2))
		if int(math.Round(r.MHat)) != m {
			t.Errorf("%s: estimated %g, true M=%d", c.cond, r.MHat, m)
		}
	}
}

func TestCountCommand(t *testing.T) {
	out, err := countCommand("4 x == 9 || x == 3")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "M=2 of N=16") || !strings.Contains(out, "(correct)") {
		t.Fatalf("unexpected report:\n%s", out)
	}
	if _, err := countCommand("4 x == 3 t=8"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "4", "x == 3", "4 x == 3 t=0", "4 x == 3 t=99"} {
		if _, err := countCommand(bad); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}

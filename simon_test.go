package main

import (
	"math"
	"math/bits"
	"math/rand"
	"strings"
	"testing"
)

// TestSimonStatevector: the input-register marginal of the exact exported
// ops must be uniform over {y : y·s = 0} and zero elsewhere — this is the
// equivalence that lets simonSolve sample instead of simulating.
func TestSimonStatevector(t *testing.T) {
	for width := 1; width <= 5; width++ {
		for s := 0; s < 1<<width; s++ {
			amp := svSim(simonOps(width, s), 2*width)
			pY := make([]float64, 1<<width)
			inputMask := 1<<width - 1
			for state, a := range amp {
				pY[state&inputMask] += a * a
			}
			orth := 1 << width
			if s != 0 {
				orth = 1 << (width - 1)
			}
			for y, p := range pY {
				want := 0.0
				if bits.OnesCount(uint(y&s))&1 == 0 {
					want = 1 / float64(orth)
				}
				if math.Abs(p-want) > 1e-12 {
					t.Fatalf("width=%d s=%d: P(y=%d)=%g, want %g", width, s, y, p, want)
				}
			}
		}
	}
}

// TestSimonSolve: recovery must be exact for every s at several widths.
func TestSimonSolve(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for width := 1; width <= 7; width++ {
		for s := 0; s < 1<<width; s++ {
			ys, got, err := simonSolve(width, s, rng)
			if err != nil {
				t.Fatalf("width=%d s=%d: %v", width, s, err)
			}
			if got != s {
				t.Fatalf("width=%d s=%d: recovered %d", width, s, got)
			}
			for _, y := range ys {
				if bits.OnesCount(uint(y&s))&1 == 1 {
					t.Fatalf("width=%d s=%d: sample y=%d violates y·s=0", width, s, y)
				}
			}
		}
	}
}

func TestSimonCommand(t *testing.T) {
	out, err := simonCommand("5 19")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "s = 19 (correct)") {
		t.Fatalf("unexpected report:\n%s", out)
	}
	if !strings.Contains(out, "promise f(x) = f(x⊕s) verified for all 32 inputs") {
		t.Fatalf("missing promise check:\n%s", out)
	}

	out, err = simonCommand("3 0")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "s = 0 (f is injective)") {
		t.Fatalf("s=0 report wrong:\n%s", out)
	}

	qasm, err := simonCommand("3 5 qasm")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"OPENQASM 2.0;", "qreg q[6];", "creg c[3];",
		"cx q[0],q[3];", "cx q[1],q[4];", "cx q[2],q[5];", // copy
		"cx q[0],q[5];", // s bit 2 from control x_0
		"measure q[2] -> c[2];"} {
		if !strings.Contains(qasm, want) {
			t.Fatalf("QASM missing %q:\n%s", want, qasm)
		}
	}

	for _, bad := range []string{"", "3", "3 8", "3 -1", "0 0", "20 1"} {
		if _, err := simonCommand(bad); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}

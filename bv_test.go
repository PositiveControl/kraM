package main

import (
	"math"
	"strings"
	"testing"
)

// TestBVStatevector: for every width ≤ 6 and every hidden s, simulate the
// exact exported ops and check the input-register marginal is exactly |s>.
func TestBVStatevector(t *testing.T) {
	for width := 1; width <= 6; width++ {
		nwires := width + 1
		for s := 0; s < 1<<width; s++ {
			amp := svSim(bvOps(width, s), nwires)
			for state, a := range amp {
				x := state & (1<<width - 1)
				p := a * a
				want := 0.0
				if x == s {
					want = 0.5 // marker is in |->: half the weight on each marker value
				}
				if math.Abs(p-want) > 1e-12 {
					t.Fatalf("width=%d s=%d: state %b has p=%g, want %g", width, s, state, p, want)
				}
			}
		}
	}
}

func TestBVCommand(t *testing.T) {
	out, err := bvCommand("6 37")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "s = 37 (100101)") || !strings.Contains(out, "probability 1") {
		t.Fatalf("unexpected report:\n%s", out)
	}

	qasm, err := bvCommand("3 5 qasm")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"OPENQASM 2.0;", "qreg q[4];", "creg c[3];",
		"cx q[0],q[3];", "cx q[2],q[3];", "measure q[0] -> c[0];"} {
		if !strings.Contains(qasm, want) {
			t.Fatalf("QASM missing %q:\n%s", want, qasm)
		}
	}
	if strings.Contains(qasm, "cx q[1],q[3];") {
		t.Fatalf("QASM has CNOT for unset bit 1 of s=5:\n%s", qasm)
	}

	for _, bad := range []string{"", "3", "3 8", "3 -1", "0 0", "x y"} {
		if _, err := bvCommand(bad); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}

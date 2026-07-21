package main

import (
	"math"
	"strings"
	"testing"
)

// TestDJStatevector: simulate the exact exported ops; the input-register
// probability of |0> must equal ((N-2M)/N)^2 — 1 for constant, 0 for
// balanced, in between when the promise is broken.
func TestDJStatevector(t *testing.T) {
	cases := []struct {
		cond  string
		width int
	}{
		{"x < 4", 3},   // balanced: M=4 of 8
		{"x >= 0", 3},  // constant true
		{"x < 0", 3},   // constant false (M=0)
		{"x == 5", 3},  // neither: M=1
		{"x != 2", 4},  // neither: M=15
		{"x < 8", 4},   // balanced at width 4
	}
	for _, c := range cases {
		cond, _, err := parseCond(c.cond, c.width)
		if err != nil {
			t.Fatalf("%s: %v", c.cond, err)
		}
		ops, bc, lay, err := djOps(cond, c.width)
		if err != nil {
			t.Fatalf("%s: %v", c.cond, err)
		}
		if bc.nwires > 16 {
			t.Fatalf("%s: %d wires too many for statevector test", c.cond, bc.nwires)
		}
		table, err := oracleTruthTable(bc, lay)
		if err != nil {
			t.Fatalf("%s: %v", c.cond, err)
		}
		n, m := len(table), 0
		for _, hit := range table {
			if hit {
				m++
			}
		}
		bias := float64(n-2*m) / float64(n)
		want := bias * bias

		amp := svSim(ops, bc.nwires)
		p0 := 0.0
		inputMask := 1<<c.width - 1
		for state, a := range amp {
			if state&inputMask == 0 {
				p0 += a * a
			}
		}
		if math.Abs(p0-want) > 1e-9 {
			t.Errorf("%s (width %d, M=%d): P(0)=%g, want %g", c.cond, c.width, m, p0, want)
		}
	}
}

func TestDJCommand(t *testing.T) {
	out, err := djCommand("3 x < 4")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "BALANCED") {
		t.Fatalf("expected BALANCED:\n%s", out)
	}
	out, err = djCommand("3 x >= 0")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "CONSTANT") {
		t.Fatalf("expected CONSTANT:\n%s", out)
	}
	out, err = djCommand("3 x == 5")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "NEITHER") {
		t.Fatalf("expected NEITHER:\n%s", out)
	}

	qasm, err := djCommand("3 x < 4 qasm")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"OPENQASM 2.0;", "creg c[3];", "measure q[0] -> c[0];"} {
		if !strings.Contains(qasm, want) {
			t.Fatalf("QASM missing %q:\n%s", want, qasm)
		}
	}

	for _, bad := range []string{"", "3", "x < 4", "3 x + y"} {
		if _, err := djCommand(bad); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}

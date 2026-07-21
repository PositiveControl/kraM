package main

import (
	"math"
	"regexp"
	"strings"
	"testing"
)

// svSim is a tiny real-amplitude statevector simulator over qops — every gate
// used (X, CX, CCX, H) is a real matrix, so no complex numbers are needed.
// State index bit w = wire w. Exponential in nwires; tests keep nwires ≤ 8.
func svSim(ops []qop, nwires int) []float64 {
	amp := make([]float64, 1<<nwires)
	amp[0] = 1
	inv := 1 / math.Sqrt2
	for _, op := range ops {
		switch op.kind {
		case qX:
			for s := range amp {
				if s&(1<<op.t) == 0 {
					amp[s], amp[s|1<<op.t] = amp[s|1<<op.t], amp[s]
				}
			}
		case qCX:
			for s := range amp {
				if s&(1<<op.a) != 0 && s&(1<<op.t) == 0 {
					amp[s], amp[s|1<<op.t] = amp[s|1<<op.t], amp[s]
				}
			}
		case qCCX:
			for s := range amp {
				if s&(1<<op.a) != 0 && s&(1<<op.b) != 0 && s&(1<<op.t) == 0 {
					amp[s], amp[s|1<<op.t] = amp[s|1<<op.t], amp[s]
				}
			}
		case qH:
			for s := range amp {
				if s&(1<<op.t) == 0 {
					lo, hi := amp[s], amp[s|1<<op.t]
					amp[s], amp[s|1<<op.t] = inv*(lo+hi), inv*(lo-hi)
				}
			}
		}
	}
	return amp
}

// TestGroverQASMStatevector is the keystone: simulate the exact ops that get
// exported and check the input-register marginal matches the fast simulation
// to 1e-9. This validates oracle, |-> kickback, diffusion, ancilla reuse, and
// endianness in one assertion.
func TestGroverQASMStatevector(t *testing.T) {
	cases := []struct {
		cond  string
		width int
	}{
		{"x == 5", 3},
		{"x == 9 || x == 3", 4},
		{"x >= 3 && x <= 5", 3},
		{"!(x == 2)", 2},
	}
	for _, c := range cases {
		cond, _, err := parseCond(c.cond, c.width)
		if err != nil {
			t.Fatalf("parseCond: %v", err)
		}
		_, _, r, _, err := groverPrep(c.cond, c.width, -1)
		if err != nil {
			t.Fatalf("groverPrep: %v", err)
		}
		ops, bc, lay, err := groverOps(cond, c.width, r.Optimal)
		if err != nil {
			t.Fatalf("groverOps: %v", err)
		}
		if bc.nwires > 16 {
			t.Fatalf("%s: %d wires too many for statevector test", c.cond, bc.nwires)
		}
		amp := svSim(ops, bc.nwires)

		// Marginal over the input register (wires 0..width-1).
		marginal := make([]float64, 1<<c.width)
		mask := 1<<c.width - 1
		for s, a := range amp {
			marginal[s&mask] += a * a
		}
		for x := range marginal {
			if math.Abs(marginal[x]-r.Final[x]) > 1e-9 {
				t.Fatalf("%s: marginal[%d]=%.12f fast-sim=%.12f", c.cond, x, marginal[x], r.Final[x])
			}
		}
		if r.M > 0 && 2*r.M < r.N {
			marked := 0.0
			for _, x := range r.Marked {
				marked += marginal[x]
			}
			want := r.Success[len(r.Success)-1]
			if math.Abs(marked-want) > 1e-9 {
				t.Fatalf("%s: marked probability %.6f != fast-sim %.6f", c.cond, marked, want)
			}
			if marked <= float64(r.M)/float64(r.N) {
				t.Fatalf("%s: no amplification: %.4f <= uniform %.4f", c.cond, marked, float64(r.M)/float64(r.N))
			}
		}

		// Ancillas must be disentangled: probability of any non-zero ancilla
		// configuration is 0 (marker aside, which sits in |->).
		for s, a := range amp {
			anc := s &^ mask &^ (1 << lay.marker)
			if anc != 0 && a*a > 1e-18 {
				t.Fatalf("%s: ancilla wires entangled: state %b has p=%.2e", c.cond, s, a*a)
			}
		}
	}
}

// TestQASMGolden pins the exported text for the flagship demo: gate order,
// ancilla reuse, header, and measurement mapping.
func TestQASMGolden(t *testing.T) {
	out, err := groverQasmReport("x == 5", 3, 1)
	if err != nil {
		t.Fatal(err)
	}
	golden := `OPENQASM 2.0;
include "qelib1.inc";
// Grover search: x == 5  (3-bit x, 1 iterations)
// input x: q[0..2] little-endian; marker q[3]; rest ancilla
// c[i] = bit i of x — int(bitstring, 2) in Qiskit yields x
qreg q[6];
creg c[3];
h q[0];
h q[1];
h q[2];
x q[3];
h q[3];
x q[1];
ccx q[0],q[1],q[4];
ccx q[2],q[4],q[5];
cx q[5],q[3];
ccx q[2],q[4],q[5];
ccx q[0],q[1],q[4];
x q[1];
h q[0];
h q[1];
h q[2];
x q[0];
x q[1];
x q[2];
h q[2];
ccx q[0],q[1],q[5];
cx q[5],q[2];
ccx q[0],q[1],q[5];
h q[2];
x q[0];
x q[1];
x q[2];
h q[0];
h q[1];
h q[2];
measure q[0] -> c[0];
measure q[1] -> c[1];
measure q[2] -> c[2];
`
	if out != golden {
		t.Fatalf("golden mismatch:\n--- got ---\n%s\n--- want ---\n%s", out, golden)
	}
}

// TestQASMWellFormed checks every emitted line against the tiny grammar we
// use, and that all wire indices are inside the declared register.
func TestQASMWellFormed(t *testing.T) {
	ip := NewInterp()
	ast, err := Parse("x = 3 y = 5 x += y")
	if err != nil {
		t.Fatal(err)
	}
	bc, err := compileBits(ast, ip)
	if err != nil {
		t.Fatal(err)
	}
	for name, out := range map[string]string{
		"program": qasmProgram(bc),
		"grover":  mustGroverQasm(t, "x == 9 || x == 3", 4, 2),
	} {
		lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
		if lines[0] != "OPENQASM 2.0;" || lines[1] != `include "qelib1.inc";` {
			t.Fatalf("%s: bad prelude", name)
		}
		var nq int
		re := regexp.MustCompile(`^(//.*|OPENQASM 2\.0;|include "qelib1\.inc";|qreg q\[(\d+)\];|creg c\[\d+\];|x q\[\d+\];|h q\[\d+\];|cx q\[\d+\],q\[\d+\];|ccx q\[\d+\],q\[\d+\],q\[\d+\];|measure q\[\d+\] -> c\[\d+\];)$`)
		idx := regexp.MustCompile(`q\[(\d+)\]`)
		for _, line := range lines {
			m := re.FindStringSubmatch(line)
			if m == nil {
				t.Fatalf("%s: malformed line %q", name, line)
			}
			if m[2] != "" {
				nq = atoi(t, m[2])
				continue
			}
			if strings.HasPrefix(line, "//") {
				continue
			}
			for _, w := range idx.FindAllStringSubmatch(line, -1) {
				if i := atoi(t, w[1]); i >= nq {
					t.Fatalf("%s: wire %d out of range (qreg %d) in %q", name, i, nq, line)
				}
			}
		}
	}
}

func mustGroverQasm(t *testing.T, cond string, width, iters int) string {
	t.Helper()
	out, err := groverQasmReport(cond, width, iters)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func atoi(t *testing.T, s string) int {
	t.Helper()
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}

package main

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// genProgram builds a random *reversible* straight-line program over the
// variables a, b, c, plus the init line that binds them. Operands are constants
// or distinct variables (never the target), so every op is structurally
// reversible — the Janus "RHS must not reference LHS" rule.
func genProgram(rng *rand.Rand) (initSrc, progSrc string) {
	vars := []string{"a", "b", "c"}
	initSrc = fmt.Sprintf("a = %d; b = %d; c = %d", rng.Intn(50), rng.Intn(50), rng.Intn(50))
	// pair returns a target index and a distinct operand index.
	pair := func() (int, int) {
		i := rng.Intn(3)
		return i, (i + 1 + rng.Intn(2)) % 3
	}
	n := 3 + rng.Intn(6)
	ops := make([]string, 0, n)
	for k := 0; k < n; k++ {
		switch rng.Intn(6) {
		case 0:
			ops = append(ops, fmt.Sprintf("%s += %d", vars[rng.Intn(3)], rng.Intn(20)))
		case 1:
			ops = append(ops, fmt.Sprintf("%s -= %d", vars[rng.Intn(3)], rng.Intn(20)))
		case 2:
			ops = append(ops, fmt.Sprintf("%s ^= %d", vars[rng.Intn(3)], rng.Intn(64)))
		case 3:
			i, j := pair()
			ops = append(ops, fmt.Sprintf("%s <=> %s", vars[i], vars[j]))
		case 4:
			i, j := pair() // register += register
			ops = append(ops, fmt.Sprintf("%s += %s", vars[i], vars[j]))
		case 5:
			i, j := pair() // register ^= register
			ops = append(ops, fmt.Sprintf("%s ^= %s", vars[i], vars[j]))
		}
	}
	return initSrc, strings.Join(ops, "; ")
}

func snapshot(ip *Interp) map[string]float64 {
	m := map[string]float64{}
	for _, n := range []string{"a", "b", "c"} {
		v, _ := ip.get(n)
		m[n] = v.Num
	}
	return m
}

// TestReversibleRoundTrip: for any reversible program P, running P then its
// inverse must return state to exactly where it started. This is the language's
// core invariant, fuzzed.
func TestReversibleRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 500; i++ {
		initSrc, progSrc := genProgram(rng)
		ip := NewInterp()
		mustRun(t, ip, initSrc)
		before := snapshot(ip)
		mustRun(t, ip, progSrc)
		mustRun(t, ip, "reverse { "+progSrc+" }")
		after := snapshot(ip)
		for _, n := range []string{"a", "b", "c"} {
			if before[n] != after[n] {
				t.Fatalf("round-trip changed %s: %v -> %v\ninit: %s\nprog: %s",
					n, before[n], after[n], initSrc, progSrc)
			}
		}
	}
}

// TestInterpreterMatchesCircuit: the compiled gate netlist, simulated, must
// produce the same result as the tree-walk interpreter — two independent
// implementations cross-checking the lowering.
func TestInterpreterMatchesCircuit(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for i := 0; i < 500; i++ {
		initSrc, progSrc := genProgram(rng)
		ip := NewInterp()
		mustRun(t, ip, initSrc)

		progAst, err := Parse(progSrc)
		if err != nil {
			t.Fatalf("parse %q: %v", progSrc, err)
		}

		// interpreter (on a clone, so the registers snapshot stays the input)
		clone := ip.clone()
		if _, err := Eval(progAst, clone); err != nil {
			t.Fatalf("eval %q: %v", progSrc, err)
		}

		// circuit
		gates, err := lower(progAst)
		if err != nil {
			t.Fatalf("lower %q: %v", progSrc, err)
		}
		simReg, err := simulate(gates, registersFrom(ip))
		if err != nil {
			t.Fatalf("simulate %q: %v", progSrc, err)
		}

		for _, n := range []string{"a", "b", "c"} {
			iv := clone.vars[n].val
			if int64(iv.Num) != simReg[n] {
				t.Fatalf("interp/circuit mismatch on %s: interp=%v circuit=%d\ninit: %s\nprog: %s",
					n, iv.Num, simReg[n], initSrc, progSrc)
			}
		}
	}
}

func maskW(v int64) int64 {
	m := int64(1) << bitWidth
	return ((v % m) + m) % m
}

// bitMatchesInterp compiles progSrc to elementary gates, simulates it from the
// state produced by initSrc, and asserts every register agrees with the
// interpreter (mod 2^bitWidth).
func bitMatchesInterp(t *testing.T, initSrc, progSrc string) {
	t.Helper()
	ip := NewInterp()
	mustRun(t, ip, initSrc)
	initReg := registersFrom(ip)

	progAst, err := Parse(progSrc)
	if err != nil {
		t.Fatalf("parse %q: %v", progSrc, err)
	}
	clone := ip.clone()
	if _, err := Eval(progAst, clone); err != nil {
		t.Fatalf("eval %q: %v", progSrc, err)
	}

	bc, err := compileBits(progAst, ip.procs)
	if err != nil {
		t.Fatalf("compileBits %q: %v", progSrc, err)
	}
	initBits := make([]bool, bc.nwires)
	for name, base := range bc.base {
		val := initReg[name]
		for b := 0; b < bitWidth; b++ {
			initBits[base+b] = (val>>uint(b))&1 == 1
		}
	}
	out := simulateBits(bc.gates, bc.nwires, initBits)

	for name, base := range bc.base {
		var got int64
		for b := 0; b < bitWidth; b++ {
			if out[base+b] {
				got |= 1 << uint(b)
			}
		}
		if want := maskW(int64(clone.vars[name].val.Num)); got != want {
			t.Fatalf("bit-circuit mismatch on %s: gates=%d interp=%d\ninit: %s\nprog: %s",
				name, got, want, initSrc, progSrc)
		}
	}
}

// TestBitCircuitMatchesInterpreter: straight-line programs decomposed to
// {X, CNOT, Toffoli} and bit-simulated must agree with the interpreter —
// validates the gate decomposition, including the Cuccaro adder.
func TestBitCircuitMatchesInterpreter(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	for i := 0; i < 500; i++ {
		initSrc, progSrc := genProgram(rng)
		bitMatchesInterp(t, initSrc, progSrc)
	}
}

// TestIfCircuitMatchesInterpreter: reversible if lowered to controlled gates
// must match the interpreter for both the taken and untaken branch. The exit
// assertion repeats the entry condition, so it always holds.
func TestIfCircuitMatchesInterpreter(t *testing.T) {
	cases := []struct{ init, prog string }{
		// condition true -> then branch
		{"a = 0; flag = 5", "if flag == 5 { a += 7 } else { a += 1 } assert flag == 5"},
		// condition false -> else branch
		{"a = 0; flag = 3", "if flag == 5 { a += 7 } else { a += 1 } assert flag == 5"},
		// branch with multiple ops
		{"a = 2; b = 3; flag = 1", "if flag == 1 { a += b; a <=> b } else { } assert flag == 1"},
		// then-only (no else)
		{"a = 4; flag = 9", "if flag == 9 { a ^= 6 } assert flag == 9"},
		{"a = 4; flag = 0", "if flag == 9 { a ^= 6 } assert flag == 9"},
		// nested register add in the taken branch
		{"a = 10; b = 20; flag = 7", "if flag == 7 { b -= a } else { } assert flag == 7"},
		// comparison conditions, both outcomes
		{"a = 0; x = 3", "if x < 5 { a += 1 } else { a += 2 } assert x < 5"},
		{"a = 0; x = 9", "if x < 5 { a += 1 } else { a += 2 } assert x < 5"},
		{"a = 0; x = 9", "if x > 5 { a += 1 } else { a += 2 } assert x > 5"},
		{"a = 0; x = 3", "if x >= 3 { a += 1 } else { a += 2 } assert x >= 3"},
		{"a = 0; x = 5", "if x <= 5 { a += 1 } else { a += 2 } assert x <= 5"},
		{"a = 0; x = 5", "if x != 4 { a += 1 } else { a += 2 } assert x != 4"},
		// constant on the left
		{"a = 0; x = 2", "if 5 > x { a += 1 } else { a += 2 } assert 5 > x"},
	}
	for _, tc := range cases {
		bitMatchesInterp(t, tc.init, tc.prog)
	}
}

// TestAncillaReuse: ancilla wires are recycled, so a straight-line program's
// wire count is bounded by peak concurrent use, not total length.
func TestAncillaReuse(t *testing.T) {
	compileN := func(n int) int {
		ops := make([]string, n)
		for i := range ops {
			ops[i] = "a += 1"
		}
		ast, err := Parse(strings.Join(ops, "; "))
		if err != nil {
			t.Fatal(err)
		}
		bc, err := compileBits(ast, nil)
		if err != nil {
			t.Fatal(err)
		}
		return bc.nwires
	}
	small, big := compileN(2), compileN(200)
	if big != small {
		t.Errorf("wire count grew with program length: %d ops used %d wires, %d ops used %d wires",
			2, small, 200, big)
	}
}

// TestUncallRoundTrip: call then uncall a procedure restores state.
func TestUncallRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	for i := 0; i < 200; i++ {
		initSrc, progSrc := genProgram(rng)
		ip := NewInterp()
		mustRun(t, ip, initSrc)
		before := snapshot(ip)
		mustRun(t, ip, "proc p { "+progSrc+" }")
		mustRun(t, ip, "call p")
		mustRun(t, ip, "uncall p")
		after := snapshot(ip)
		for _, n := range []string{"a", "b", "c"} {
			if before[n] != after[n] {
				t.Fatalf("call/uncall changed %s: %v -> %v\nprog: %s", n, before[n], after[n], progSrc)
			}
		}
	}
}

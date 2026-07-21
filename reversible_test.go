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
		gates, err := lowerProgram(progAst, ip)
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

	bc, err := compileBits(progAst, ip)
	if err != nil {
		t.Fatalf("compileBits %q: %v", progSrc, err)
	}
	initBits := make([]bool, bc.nwires)
	for name, base := range bc.base {
		val := initReg[name]
		for b := 0; b < bc.width; b++ {
			initBits[base+b] = (val>>uint(b))&1 == 1
		}
	}
	out := simulateBits(bc.gates, bc.nwires, initBits)

	for name, base := range bc.base {
		var got int64
		for b := 0; b < bc.width; b++ {
			if out[base+b] {
				got |= 1 << uint(b)
			}
		}
		want, ok := regWant(clone, name, bc.width)
		if !ok || got != want {
			t.Fatalf("bit-circuit mismatch on %s: gates=%d interp=%d ok=%v\ninit: %s\nprog: %s",
				name, got, want, ok, initSrc, progSrc)
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
		// variable vs variable, both outcomes
		{"r = 0; x = 3; y = 8", "if x < y { r += 1 } else { r += 2 } assert x < y"},
		{"r = 0; x = 8; y = 3", "if x < y { r += 1 } else { r += 2 } assert x < y"},
		{"r = 0; x = 5; y = 5", "if x == y { r += 1 } else { r += 2 } assert x == y"},
		{"r = 0; x = 9; y = 4", "if x >= y { r += 1 } else { r += 2 } assert x >= y"},
		{"r = 0; x = 2; y = 7", "if x > y { r += 1 } else { r += 2 } assert x > y"},
		{"r = 0; x = 6; y = 6", "if x <= y { r += 1 } else { r += 2 } assert x <= y"},
		{"r = 0; x = 6; y = 6", "if x != y { r += 1 } else { r += 2 } assert x != y"},
	}
	for _, tc := range cases {
		bitMatchesInterp(t, tc.init, tc.prog)
	}
}

// TestLoopCircuitMatchesInterpreter: a reversible loop unrolled to gates (using
// the compile-time iteration count) must match the interpreter.
func TestLoopCircuitMatchesInterpreter(t *testing.T) {
	cases := []struct{ init, prog string }{
		// counter loop, empty Rest: Do runs n times
		{"s = 0; i = 0; n = 4", "from i == 0 { s += 2; i += 1 } loop { } until i == n"},
		// non-empty Rest
		{"s = 0; i = 0; n = 3", "from i == 0 { i += 1 } loop { s += 5 } until i == n"},
		// loop that does real arithmetic each step
		{"acc = 1; i = 0; n = 5", "from i == 0 { acc ^= 3; i += 1 } loop { } until i == n"},
		// single iteration
		{"i = 0; x = 0", "from i == 0 { x += 9; i += 1 } loop { } until i == 1"},
	}
	for _, tc := range cases {
		bitMatchesInterp(t, tc.init, tc.prog)
	}
}

// TestArrayCircuitMatchesInterpreter: constant-index (and loop-folded-index)
// array element operations lower to gates matching the interpreter.
func TestArrayCircuitMatchesInterpreter(t *testing.T) {
	cases := []struct{ init, prog string }{
		{"a = [1, 2, 3]", "a[0] += 5"},
		{"a = [1, 2, 3]", "a[2] ^= 6"},
		{"a = [1, 2, 3]", "a[0] <=> a[2]"},
		{"a = [10, 20, 30]; x = 4", "a[1] += x"},
		{"a = [1, 2, 3]", "a[0] += 5; a[1] -= 2; a[2] ^= 3; a[0] <=> a[1]"},
		// loop with a loop-varying index, unrolled (indices fold per iteration)
		{"xs = [1, 2, 3, 4]; i = 0; n = 4", "from i == 0 { } loop { xs[i] += 1; i += 1 } until i == n"},
		// in-place reversal via swaps with a computed index
		{"xs = [1, 2, 3, 4]; i = 0; n = 4", "from i == 0 { } loop { xs[i] <=> xs[n - 1 - i]; i += 1 } until i == 2"},
	}
	for _, tc := range cases {
		bitMatchesInterp(t, tc.init, tc.prog)
	}
}

// TestLocalCircuitMatchesInterpreter: local/delocal lower to ancilla
// alloc/free; the visible registers must still match the interpreter.
func TestLocalCircuitMatchesInterpreter(t *testing.T) {
	cases := []struct{ init, prog string }{
		{"x = 5", "local t = 0; t += x; delocal t = x"},
		{"x = 5; y = 3", "local t = 0; t += x; t += y; delocal t = 8"},
		{"x = 6", "local t = x; t -= 2; delocal t = 4"},
		{"a = 12; b = 10", "local t = a; t ^= b; delocal t = 6"},
	}
	for _, tc := range cases {
		bitMatchesInterp(t, tc.init, tc.prog)
	}
}

// TestCompoundIfCircuitMatchesInterpreter: && / || / ! conditions lower to
// controlled gates matching the interpreter, on both branches.
func TestCompoundIfCircuitMatchesInterpreter(t *testing.T) {
	cases := []struct{ init, prog string }{
		{"x = 5; r = 0", "if x > 0 && x < 10 { r += 1 } else { r += 2 } assert x > 0 && x < 10"},
		{"x = 15; r = 0", "if x > 0 && x < 10 { r += 1 } else { r += 2 } assert x > 0 && x < 10"},
		{"x = 15; r = 0", "if x < 0 || x > 10 { r += 1 } else { r += 2 } assert x < 0 || x > 10"},
		{"x = 5; r = 0", "if x < 0 || x > 10 { r += 1 } else { r += 2 } assert x < 0 || x > 10"},
		{"x = 5; r = 0", "if !(x == 3) { r += 1 } else { r += 2 } assert !(x == 3)"},
		{"x = 5; y = 2; r = 0", "if x > 0 && y < 5 || x == 99 { r += 1 } else { r += 2 } assert x > 0 && y < 5 || x == 99"},
	}
	for _, tc := range cases {
		bitMatchesInterp(t, tc.init, tc.prog)
	}
}

// TestProcCircuitMatchesInterpreter: parameterized procedures, inlined with
// by-reference args, lower to gates matching the interpreter.
func TestProcCircuitMatchesInterpreter(t *testing.T) {
	cases := []struct{ init, prog string }{
		{"a = 3; b = 8", "proc add(d, s) { d += s }; call add(a, b)"},
		{"a = 3; b = 8", "proc add(d, s) { d += s }; call add(a, b); uncall add(a, b)"},
		{"a = 1; b = 2; c = 4", "proc sw(x, y) { x <=> y }; call sw(a, b); call sw(b, c)"},
		{"a = 12; b = 10", "proc x(d, s) { d ^= s }; call x(a, b)"},
	}
	for _, tc := range cases {
		bitMatchesInterp(t, tc.init, tc.prog)
	}
}

// TestArrayRoundTrip: a sequence of reversible array operations, then its
// inverse, restores the array exactly.
func TestArrayRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	const size = 6
	for iter := 0; iter < 300; iter++ {
		ip := NewInterp()
		// random initial array
		elems := make([]string, size)
		for i := range elems {
			elems[i] = fmt.Sprintf("%d", rng.Intn(100))
		}
		mustRun(t, ip, "a = ["+strings.Join(elems, ", ")+"]")
		before, _ := ip.get("a")

		// random reversible ops
		ops := make([]string, 0, 8)
		for k := 0; k < 3+rng.Intn(6); k++ {
			switch rng.Intn(4) {
			case 0:
				ops = append(ops, fmt.Sprintf("a[%d] += %d", rng.Intn(size), rng.Intn(50)))
			case 1:
				ops = append(ops, fmt.Sprintf("a[%d] -= %d", rng.Intn(size), rng.Intn(50)))
			case 2:
				ops = append(ops, fmt.Sprintf("a[%d] ^= %d", rng.Intn(size), rng.Intn(64)))
			case 3:
				i := rng.Intn(size)
				j := (i + 1 + rng.Intn(size-1)) % size
				ops = append(ops, fmt.Sprintf("a[%d] <=> a[%d]", i, j))
			}
		}
		prog := strings.Join(ops, "; ")
		mustRun(t, ip, prog)
		mustRun(t, ip, "reverse { "+prog+" }")

		after, _ := ip.get("a")
		if before.String() != after.String() {
			t.Fatalf("array round-trip changed:\nbefore: %s\nafter:  %s\nprog: %s",
				before, after, prog)
		}
	}
}

// TestReversibleSort: a recorded-trace bubble sort sorts the array, and uncall
// (replaying the trace backward) restores the original exactly.
func TestReversibleSort(t *testing.T) {
	const sortit = `sw = [0,0,0,0,0,0,0,0,0,0]
proc sortit {
  if a[0] > a[1] { a[0] <=> a[1]; sw[0] += 1 } else { } assert sw[0] == 1
  if a[1] > a[2] { a[1] <=> a[2]; sw[1] += 1 } else { } assert sw[1] == 1
  if a[2] > a[3] { a[2] <=> a[3]; sw[2] += 1 } else { } assert sw[2] == 1
  if a[3] > a[4] { a[3] <=> a[4]; sw[3] += 1 } else { } assert sw[3] == 1
  if a[0] > a[1] { a[0] <=> a[1]; sw[4] += 1 } else { } assert sw[4] == 1
  if a[1] > a[2] { a[1] <=> a[2]; sw[5] += 1 } else { } assert sw[5] == 1
  if a[2] > a[3] { a[2] <=> a[3]; sw[6] += 1 } else { } assert sw[6] == 1
  if a[0] > a[1] { a[0] <=> a[1]; sw[7] += 1 } else { } assert sw[7] == 1
  if a[1] > a[2] { a[1] <=> a[2]; sw[8] += 1 } else { } assert sw[8] == 1
  if a[0] > a[1] { a[0] <=> a[1]; sw[9] += 1 } else { } assert sw[9] == 1
}`
	rng := rand.New(rand.NewSource(9))
	for iter := 0; iter < 200; iter++ {
		vals := rng.Perm(5)
		elems := make([]string, 5)
		for i, v := range vals {
			elems[i] = fmt.Sprintf("%d", v)
		}
		ip := NewInterp()
		mustRun(t, ip, "a = ["+strings.Join(elems, ", ")+"]")
		before, _ := ip.get("a")
		mustRun(t, ip, sortit)
		mustRun(t, ip, "call sortit")

		sorted, _ := ip.get("a")
		for i := 1; i < len(sorted.Arr); i++ {
			if sorted.Arr[i-1].Num > sorted.Arr[i].Num {
				t.Fatalf("not sorted: %s (from %s)", sorted, before)
			}
		}
		mustRun(t, ip, "uncall sortit")
		if restored, _ := ip.get("a"); restored.String() != before.String() {
			t.Fatalf("uncall did not restore: %s -> sorted %s -> %s", before, sorted, restored)
		}
	}
}

// TestReversibleGCD: subtraction-based Euclid with a recorded branch trace
// computes the gcd and uncall restores the original inputs.
func TestReversibleGCD(t *testing.T) {
	gcd := func(x, y int) int {
		for y != 0 {
			x, y = y, x%y
		}
		return x
	}
	const proc = `k = 0
t = [0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0]
proc gcd {
  from k == 0 { } loop {
    if a > b { a -= b; t[k] += 1 } else { b -= a } assert t[k] == 1
    k += 1
  } until a == b
}`
	rng := rand.New(rand.NewSource(11))
	for iter := 0; iter < 200; iter++ {
		x := 1 + rng.Intn(20)
		y := 1 + rng.Intn(20)
		ip := NewInterp()
		mustRun(t, ip, fmt.Sprintf("a = %d; b = %d", x, y))
		mustRun(t, ip, proc)
		mustRun(t, ip, "call gcd")

		if got, _ := ip.get("a"); int(got.Num) != gcd(x, y) {
			t.Fatalf("gcd(%d,%d) = %v, want %d", x, y, got.Num, gcd(x, y))
		}
		mustRun(t, ip, "uncall gcd")
		ga, _ := ip.get("a")
		gb, _ := ip.get("b")
		if int(ga.Num) != x || int(gb.Num) != y {
			t.Fatalf("uncall did not restore gcd(%d,%d): got a=%v b=%v", x, y, ga.Num, gb.Num)
		}
	}
}

// TestComputeUncompute: compute-copy-uncompute with a local ancilla computes
// f(x)=3x+1 into the output, leaves the input untouched and the ancilla clean,
// and uncall reverses it.
func TestComputeUncompute(t *testing.T) {
	const proc = `proc f(inp, out) {
  local t = 0
  t += inp
  t += inp
  t += inp
  t += 1
  out += t
  t -= 1
  t -= inp
  t -= inp
  t -= inp
  delocal t = 0
}`
	rng := rand.New(rand.NewSource(13))
	for iter := 0; iter < 200; iter++ {
		x := rng.Intn(1000)
		ip := NewInterp()
		mustRun(t, ip, fmt.Sprintf("x = %d; result = 0", x))
		mustRun(t, ip, proc)
		mustRun(t, ip, "call f(x, result)")

		if r, _ := ip.get("result"); int(r.Num) != 3*x+1 {
			t.Fatalf("f(%d) = %v, want %d", x, r.Num, 3*x+1)
		}
		if xv, _ := ip.get("x"); int(xv.Num) != x {
			t.Fatalf("input changed: x = %v, want %d", xv.Num, x)
		}
		if _, exists := ip.get("t"); exists {
			t.Fatalf("ancilla t leaked (should be delocal'd)")
		}
		mustRun(t, ip, "uncall f(x, result)")
		if r, _ := ip.get("result"); r.Num != 0 {
			t.Fatalf("uncall did not restore result: got %v", r.Num)
		}
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

// TestWithDo: `with x = e { compute } do { body }` desugars to
// local; compute; body; inverse(compute); delocal — compute-copy-uncompute
// as syntax. Checks forward result, clean ancilla, uncall round-trip, and
// that the desugared form lowers to a circuit matching the interpreter.
func TestWithDo(t *testing.T) {
	const proc = `proc f(inp, out) {
  with t = 0 { t += inp; t += inp; t += inp; t += 1 } do { out += t }
}`
	rng := rand.New(rand.NewSource(17))
	for iter := 0; iter < 200; iter++ {
		x := rng.Intn(1000)
		ip := NewInterp()
		mustRun(t, ip, fmt.Sprintf("x = %d; result = 0", x))
		mustRun(t, ip, proc)
		mustRun(t, ip, "call f(x, result)")

		if r, _ := ip.get("result"); int(r.Num) != 3*x+1 {
			t.Fatalf("f(%d) = %v, want %d", x, r.Num, 3*x+1)
		}
		if _, exists := ip.get("t"); exists {
			t.Fatalf("ancilla t leaked (with should uncompute+delocal)")
		}
		mustRun(t, ip, "uncall f(x, result)")
		if r, _ := ip.get("result"); r.Num != 0 {
			t.Fatalf("uncall did not restore result: got %v", r.Num)
		}
	}
}

// TestWithDoCircuit: the desugared with/do lowers to gates that match the
// interpreter.
func TestWithDoCircuit(t *testing.T) {
	const src = `with t = 0 { t += a; t += a; t += 1 } do { b += t }`
	ip := NewInterp()
	mustRun(t, ip, "a = 5; b = 0")

	ast, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	clone := ip.clone()
	if _, err := Eval(ast, clone); err != nil {
		t.Fatal(err)
	}
	gates, err := lowerProgram(ast, ip)
	if err != nil {
		t.Fatal(err)
	}
	simReg, err := simulate(gates, registersFrom(ip))
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"a", "b"} {
		iv := clone.vars[n].val
		if int64(iv.Num) != simReg[n] {
			t.Fatalf("%s: interpreter %v, circuit %d", n, iv.Num, simReg[n])
		}
	}
}

// TestWithDoRejectsIrreversibleCompute: a compute block that can't be
// structurally inverted is a parse error, not a runtime surprise.
func TestWithDoRejectsIrreversibleCompute(t *testing.T) {
	_, err := Parse(`with t = 0 { t = 5 } do { }`)
	if err == nil {
		t.Fatal("expected parse error for irreversible compute block")
	}
	if !strings.Contains(err.Error(), "not reversible") {
		t.Fatalf("unhelpful error: %v", err)
	}
}

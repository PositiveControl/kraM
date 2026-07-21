package main

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

// oracleFor parses and compiles a condition, failing the test on any error.
func oracleFor(t *testing.T, cond string, width int) (*bitCircuit, oracleLayout, []bool) {
	t.Helper()
	node, _, err := parseCond(cond, width)
	if err != nil {
		t.Fatalf("parseCond %q: %v", cond, err)
	}
	bc, lay, err := compileOracle(node, width)
	if err != nil {
		t.Fatalf("compileOracle %q: %v", cond, err)
	}
	table, err := oracleTruthTable(bc, lay)
	if err != nil {
		t.Fatalf("oracleTruthTable %q: %v", cond, err)
	}
	return bc, lay, table
}

// interpCond asks the interpreter for the truth value of cond with x set.
func interpCond(t *testing.T, cond string, x int) bool {
	t.Helper()
	node, _, err := parseCond(cond, bitWidth)
	if err != nil {
		t.Fatalf("parseCond: %v", err)
	}
	ip := NewInterp()
	mustRun(t, ip, fmt.Sprintf("x = %d", x))
	ok, err := evalCond(node, ip, "test condition")
	if err != nil {
		t.Fatalf("evalCond %q x=%d: %v", cond, x, err)
	}
	return ok
}

// TestOracleCleanAndCorrect: for every basis state, the marker matches the
// interpreter's truth value and every other wire is restored (the truth-table
// builder itself errors on garbage — superposition demands the exhaustive
// check, a single input proving clean is not enough).
func TestOracleCleanAndCorrect(t *testing.T) {
	conds := []string{
		"x == 9",
		"x != 3",
		"x < 6",
		"x >= 10",
		"x == 9 || x == 3",
		"x >= 3 && x <= 5",
		"!(x == 7)",
		"!(x < 4) && x != 12",
	}
	for _, cond := range conds {
		for width := 3; width <= 6; width++ {
			// The oracle compares registers mod 2^width; the interpreter sees
			// the full constant. Skip cond/width combos where a constant
			// exceeds the register range — parseCond warns there.
			if constantsExceed(t, cond, width) {
				continue
			}
			_, _, table := oracleFor(t, cond, width)
			for x := 0; x < 1<<width; x++ {
				want := interpCond(t, cond, x)
				if table[x] != want {
					t.Fatalf("%s width=%d x=%d: oracle=%v interp=%v", cond, width, x, table[x], want)
				}
			}
		}
	}
}

func constantsExceed(t *testing.T, cond string, width int) bool {
	t.Helper()
	_, warn, err := parseCond(cond, width)
	if err != nil {
		t.Fatalf("parseCond: %v", err)
	}
	return warn != ""
}

// TestOracleMarkerNeverControl pins the phase-kickback property: with the
// marker only ever a target, preparing it in |-> turns the bit-flip oracle
// into an exact phase oracle, which is what the fast simulation assumes.
func TestOracleMarkerNeverControl(t *testing.T) {
	for _, cond := range []string{"x == 5", "x == 9 || x == 3", "x >= 3 && x <= 5", "!(x < 4)"} {
		bc, lay, _ := oracleFor(t, cond, 4)
		for i, g := range bc.gates {
			if g.A == lay.marker && g.Op != BX || g.Op == BTOFF && g.B == lay.marker {
				t.Fatalf("%s: gate %d (%s) uses marker wire %d as control", cond, i, g, lay.marker)
			}
			if g.Op == BCNOT && g.A == lay.marker {
				t.Fatalf("%s: gate %d (%s) uses marker as CNOT control", cond, i, g)
			}
		}
	}
}

// TestGroverMatchesClosedForm: success after k iterations must equal
// sin²((2k+1)·θ) with θ = asin(√(M/N)) — the textbook closed form.
func TestGroverMatchesClosedForm(t *testing.T) {
	for width := 4; width <= 8; width++ {
		for _, m := range []int{1, 2, 5} {
			n := 1 << width
			table := make([]bool, n)
			for i := 0; i < m; i++ {
				table[(i*7+3)%n] = true // scattered marked states
			}
			marked := 0
			for _, b := range table {
				if b {
					marked++
				}
			}
			if marked != m {
				t.Fatalf("collision in marked-state placement: want %d got %d", m, marked)
			}
			r := groverRun(table, -1)
			theta := math.Asin(math.Sqrt(float64(m) / float64(n)))
			for k, p := range r.Success {
				want := math.Pow(math.Sin(float64(2*k+1)*theta), 2)
				if math.Abs(p-want) > 1e-9 {
					t.Fatalf("width=%d M=%d k=%d: success=%.12f closed-form=%.12f", width, m, k, p, want)
				}
			}
		}
	}
}

func TestGroverEdges(t *testing.T) {
	// M = 0: no iterations, uniform stays uniform.
	r := groverRun(make([]bool, 16), -1)
	if r.Optimal != 0 || r.Success[0] != 0 {
		t.Fatalf("M=0: optimal=%d success0=%f", r.Optimal, r.Success[0])
	}
	// M = N: already certain.
	all := make([]bool, 16)
	for i := range all {
		all[i] = true
	}
	r = groverRun(all, -1)
	if r.Optimal != 0 || math.Abs(r.Success[0]-1) > 1e-12 {
		t.Fatalf("M=N: optimal=%d success0=%f", r.Optimal, r.Success[0])
	}
	// M >= N/2: measure immediately.
	half := make([]bool, 16)
	for i := 0; i < 8; i++ {
		half[i] = true
	}
	r = groverRun(half, -1)
	if r.Optimal != 0 || math.Abs(r.Success[0]-0.5) > 1e-12 {
		t.Fatalf("M=N/2: optimal=%d success0=%f", r.Optimal, r.Success[0])
	}
}

func TestGroverErrors(t *testing.T) {
	cases := []struct {
		args string
		want string
	}{
		{"", "usage:"},
		{"4", "usage:"},
		{"abc x == 1", "usage:"},
		{"0 x == 1", "bits must be"},
		{"17 x == 1", "bits must be"},
		{"4 x == y", "exactly one variable"},
		{"4 x += 1", "expected a condition"},
		{"4 x == 1 iters=-2", "bad iters"},
	}
	for _, c := range cases {
		_, err := groverCommand(c.args)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf(":grover %q: err=%v, want substring %q", c.args, err, c.want)
		}
	}
	// Overflowing constant is a warning, not an error.
	out, err := groverCommand("4 x == 99")
	if err != nil || !strings.Contains(out, "warning: constant 99") {
		t.Fatalf("overflow constant: err=%v out=%q", err, out)
	}
}

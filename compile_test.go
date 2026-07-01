package main

import (
	"fmt"
	"strings"
	"testing"
)

// Regression suite for the compile commands (:gates / :circuit / :verify /
// :energy / :invert). Every behaviour we rely on — both what must compile and
// what must fail (and why) — is pinned here so a future change can't silently
// break or "0-gate" it.
//
// POLICY (moving forward, indefinitely): when a compile bug or gap is found,
// add a row to compileCases BEFORE/with the fix. Successes guard against
// regressions; expected-error rows guard against silent-wrong output (the
// "0 gates" class of bug) and document the language's real limits.
//
// Each command runs against a FRESH interpreter, exactly like the studio's
// one-click buttons: a program carries its own `=` initialisation and lowers
// self-contained, independent of any prior run.

func runCompile(src, cmd string) (string, error) {
	ast, err := Parse(src)
	if err != nil {
		return "", err
	}
	switch cmd {
	case "gates":
		bc, e := compileBits(ast, NewInterp())
		if e != nil {
			return "", e
		}
		return fmt.Sprintf("%d gates", len(bc.gates)), nil
	case "circuit":
		g, e := lowerProgram(ast, NewInterp())
		if e != nil {
			return "", e
		}
		return fmt.Sprintf("%d gates", len(g)), nil
	case "verify":
		return verify(ast, NewInterp())
	case "energy":
		return energyReport(ast, NewInterp())
	case "invert":
		n, e := invertTop(ast)
		if e != nil {
			return "", e
		}
		return format(n), nil
	}
	return "", fmt.Errorf("unknown command %q", cmd)
}

// check interprets an expectation for one command:
//   - ""              → skip (command not exercised for this program)
//   - "OK"            → must succeed with a non-empty, non-zero-gate result
//   - "has:SUBSTR"    → must succeed and the output must contain SUBSTR
//   - "err:SUBSTR"    → must fail with an error containing SUBSTR
func check(t *testing.T, name, cmd, exp string) {
	t.Helper()
	if exp == "" {
		return
	}
	out, err := runCompile(programs[name], cmd)
	switch {
	case exp == "OK":
		if err != nil {
			t.Errorf("%s / :%s — want success, got error: %v", name, cmd, err)
		} else if out == "0 gates" {
			t.Errorf("%s / :%s — compiled to 0 gates (silently empty)", name, cmd)
		}
	case strings.HasPrefix(exp, "has:"):
		want := exp[len("has:"):]
		if err != nil {
			t.Errorf("%s / :%s — want success containing %q, got error: %v", name, cmd, want, err)
		} else if !strings.Contains(out, want) {
			t.Errorf("%s / :%s — output %q does not contain %q", name, cmd, out, want)
		}
	case strings.HasPrefix(exp, "err:"):
		want := exp[len("err:"):]
		if err == nil {
			t.Errorf("%s / :%s — want error containing %q, got success: %q", name, cmd, want, out)
		} else if !strings.Contains(err.Error(), want) {
			t.Errorf("%s / :%s — error %q does not contain %q", name, cmd, err.Error(), want)
		}
	default:
		t.Fatalf("bad expectation %q", exp)
	}
}

// programs: the source under each name (kept separate so cases stay readable).
var programs = map[string]string{
	"fib": `a = 0; b = 1; i = 0; n = 10
proc fibstep(x, y) { x += y; x <=> y }
from i == 0 { } loop { call fibstep(a, b); i += 1 } until i == n
print "fib pair: " + a + ", " + b`,

	"straight-line": `a = 1; b = 2
a += b
a <=> b`,

	"swap-xor": `x = 12; y = 7
x <=> y
x ^= 10`,

	"reverse-block": `a = 0
reverse { a += 5; a += 3 }`,

	"reversible-if": `x = 0; c = 0
if c == 0 { x += 1 } else { } assert x == 1`,

	// print carries no register effect — a program ending in print must still
	// compile (regression: print used to abort the whole lowering).
	"init-and-print": `a = 1
a += 1
print a`,

	// --- programs that MUST fail, with the reason pinned ---

	// array-literal initialisation is not lowered (a literal isn't one register).
	// NOTE: array *element* ops do lower when the array is already in state.
	"array-init": `g = [1, 0, 0, 1]
g[0] <=> g[3]`,

	// nested reversible loops cannot be unrolled (the inner loop differs per
	// outer iteration) — this is the CA "step generation" shape.
	"nested-loops": `a = 0; b = 0
from a == 0 { } loop {
  from b == 0 { } loop { b += 1 } until b == 2
  b -= 2
  a += 1
} until a == 2`,

	// forget is deliberately irreversible: no gate, no inverse.
	"forget": `x = 5
forget x`,

	// '=' introduces a fresh name; re-binding is rejected by the compiler too.
	"reassign": `x = 1
x = 2`,
}

func TestCompileMatrix(t *testing.T) {
	cases := []struct {
		name                                   string
		gates, circuit, verify, energy, invert string
	}{
		// name             gates   circuit  verify        energy          invert
		{"fib", "OK", "OK", "has:MATCH", "has:Landauer", "OK"},
		{"straight-line", "OK", "OK", "has:MATCH", "has:Landauer", "OK"},
		{"swap-xor", "OK", "OK", "has:MATCH", "has:Landauer", "OK"},
		{"reverse-block", "OK", "OK", "has:MATCH", "has:Landauer", "OK"},
		{"reversible-if", "OK", "err:does not lower a reversible if", "has:MATCH", "has:Landauer", ""},
		{"init-and-print", "OK", "OK", "has:MATCH", "has:Landauer", "OK"},

		{"array-init", "err:operand must be a constant", "err:array-literal initialisation is not lowered", "err:operand must be a constant", "err:operand must be a constant", ""},
		{"nested-loops", "err:nested loops cannot be unrolled", "err:nested loops cannot be unrolled", "err:nested loops", "err:nested loops", ""},
		{"forget", "err:irreversible erasure", "err:irreversible erasure", "err:irreversible erasure", "err:irreversible erasure", "err:cannot reverse forget"},
		{"reassign", "err:cannot reassign", "", "", "", ""},
	}
	for _, c := range cases {
		check(t, c.name, "gates", c.gates)
		check(t, c.name, "circuit", c.circuit)
		check(t, c.name, "verify", c.verify)
		check(t, c.name, "energy", c.energy)
		check(t, c.name, "invert", c.invert)
	}
}

// TestInvertRoundTrips pins that :invert of a whole program keeps the setup
// (proc defs + `=` init) and reverses the body — the "run it backward" form.
func TestInvertWholeProgram(t *testing.T) {
	out, err := runCompile(programs["fib"], "invert")
	if err != nil {
		t.Fatalf("invert fib: %v", err)
	}
	for _, want := range []string{
		"a = 0",                // setup preserved
		"proc fibstep",         // proc def preserved
		"from i == n",          // loop entry/exit swapped
		"uncall fibstep(a, b)", // call inverted
		"until i == 0",         // exit is the old entry
	} {
		if !strings.Contains(out, want) {
			t.Errorf("invert fib missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "print") {
		t.Errorf("invert fib should drop print (no inverse), got:\n%s", out)
	}
}

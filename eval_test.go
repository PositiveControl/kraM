package main

import (
	"strings"
	"testing"
)

// eval runs a whole program in a fresh interpreter and returns the result.
func evalSrc(t *testing.T, src string) (Value, error) {
	t.Helper()
	ast, err := Parse(src)
	if err != nil {
		return Value{}, err
	}
	return Eval(ast, NewInterp())
}

func TestEval(t *testing.T) {
	tests := []struct{ src, want string }{
		// arithmetic + precedence
		{"2 + 3 * 4", "14"},
		{"(2 + 3) * 4", "20"},
		{"-5 + 2", "-3"},
		{"10 / 4", "2.5"},
		// comparisons + booleans
		{"3 < 5", "true"},
		{"2 + 3 == 5", "true"},
		{"2 < 3 == true", "true"},
		{"true == 1", "false"},
		{"1 != 2", "true"},
		// strings
		{`"a" + "b"`, `"ab"`},
		{`"n=" + 5`, `"n=5"`},
		{`"x" == "x"`, "true"},
		// variables + statements
		{"x = 5; x + 1", "6"},
		{"a = 1; b = 2; a + b", "3"},
		// reversible updates
		{"x = 5; x += 3", "8"},
		{"x = 5; x -= 1", "4"},
		{"x = 12; x ^= 10", "6"},
		{"a = 1; b = 2; a <=> b; a", "2"},
		// control flow
		{"if 2 > 1 { 10 } else { 20 }", "10"},
		{"if 1 > 2 { 10 } else { 20 }", "20"},
		{"if false { 1 } else if true { 2 } else { 3 }", "2"},
		{"i = 0; while i < 3 { i = i + 1 }; i", "3"},
		{"i = 0; n = 3; from i == 0 { i += 1 } loop { i += 1 } until i == n; i", "3"},
		// reversible if + assert (exit assertion holds)
		{"x = 0; if true { x += 5 } else { x -= 1 } assert x == 5; x", "5"},
		// reverse block round-trips
		{"x = 0; x += 5; reverse { x += 5 }; x", "0"},
		// procedures
		{"x = 1; proc bump { x += 4 }; call bump; x", "5"},
		{"x = 1; proc bump { x += 4 }; call bump; uncall bump; x", "1"},
		// parameterized procedures (by-reference)
		{"x = 3; y = 10; proc add(d, s) { d += s }; call add(x, y); x", "13"},
		{"x = 3; y = 10; proc add(d, s) { d += s }; call add(x, y); uncall add(x, y); x", "3"},
		{"x = 1; y = 2; proc sw(a, b) { a <=> b }; call sw(x, y); x", "2"},
	}
	for _, tc := range tests {
		got, err := evalSrc(t, tc.src)
		if err != nil {
			t.Errorf("%q: unexpected error: %v", tc.src, err)
			continue
		}
		if got.String() != tc.want {
			t.Errorf("%q = %s, want %s", tc.src, got.String(), tc.want)
		}
	}
}

func TestEvalErrors(t *testing.T) {
	tests := []struct{ src, msgContains string }{
		{"1 / 0", "division by zero"},
		{"true + 1", "needs numbers or a string"},
		{"x", "undefined variable"},
		{"if 1 { 2 }", "must be bool"},
		{"x = 1.5; x ^= 1", "whole number"},
		{"_ = 5", "cannot be assigned"},
		{"x = 5; reverse { x = 9 }", "cannot reverse destructive assignment"},
		{"if true { 1 } assert false", "exit assertion violated"},
		{"from x == 0 { x += 1 } loop { x += 1 } until x == 1", "undefined variable"},
		{"proc p { x = 1 }; uncall p", "cannot uncall"},
		{"x = 1; proc add(d, s) { d += s }; call add(x, x)", "aliased argument"},
		{"x = 1; y = 2; proc add(d, s) { d += s }; call add(x)", "takes 2 argument"},
	}
	for _, tc := range tests {
		_, err := evalSrc(t, tc.src)
		if err == nil {
			t.Errorf("%q: expected error containing %q, got nil", tc.src, tc.msgContains)
			continue
		}
		if !strings.Contains(err.Error(), tc.msgContains) {
			t.Errorf("%q: error %q does not contain %q", tc.src, err.Error(), tc.msgContains)
		}
	}
}

func TestInvertFormat(t *testing.T) {
	tests := []struct{ src, want string }{
		{"a += 5", "a -= 5"},
		{"a -= 5", "a += 5"},
		{"a ^= 3", "a ^= 3"},          // self-inverse
		{"a <=> b", "a <=> b"},        // self-inverse
		{"a += 5; b <=> c", "{ b <=> c; a -= 5 }"}, // reversed order
		{"call p", "uncall p"},
		{"if c { x += 1 } else { x -= 1 } assert d", "if d { x -= 1 } else { x += 1 } assert c"},
	}
	for _, tc := range tests {
		ast, err := Parse(tc.src)
		if err != nil {
			t.Errorf("%q: parse error: %v", tc.src, err)
			continue
		}
		inv, err := invert(ast)
		if err != nil {
			t.Errorf("%q: invert error: %v", tc.src, err)
			continue
		}
		if got := format(inv); got != tc.want {
			t.Errorf("invert(%q) = %q, want %q", tc.src, got, tc.want)
		}
	}
}

func TestAtomicRollback(t *testing.T) {
	// A line that errors part-way must leave no mutation behind.
	ip := NewInterp()
	mustRun(t, ip, "a = 1")
	cp := ip.checkpoint()
	ast, _ := Parse("a += 5; a <=> b") // b undefined -> swap errors after a += 5
	if _, err := Eval(ast, ip); err == nil {
		t.Fatal("expected error")
	}
	ip.rollback(cp)
	if v, _ := ip.get("a"); v.Num != 1 {
		t.Errorf("after rollback a = %v, want 1", v.Num)
	}
	if len(ip.past) != 1 { // only the original `a = 1`
		t.Errorf("history len = %d, want 1", len(ip.past))
	}
}

// mustRun evaluates src into ip, failing the test on any error.
func mustRun(t *testing.T, ip *Interp, src string) {
	t.Helper()
	ast, err := Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	if _, err := Eval(ast, ip); err != nil {
		t.Fatalf("eval %q: %v", src, err)
	}
}

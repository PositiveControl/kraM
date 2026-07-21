package main

import (
	"math"
	"math/rand"
	"strings"
	"testing"
)

func TestModArith(t *testing.T) {
	if modPow(7, 4, 15) != 1 {
		t.Fatal("7^4 mod 15")
	}
	if mulOrder(7, 15) != 4 {
		t.Fatal("order of 7 mod 15")
	}
	if mulOrder(2, 21) != 6 {
		t.Fatal("order of 2 mod 21")
	}
	for _, c := range []struct {
		n, b, k int
		ok      bool
	}{{9, 3, 2, true}, {27, 3, 3, true}, {49, 7, 2, true}, {15, 0, 0, false}, {21, 0, 0, false}} {
		b, k, ok := isPerfectPower(c.n)
		if ok != c.ok || (ok && (b != c.b || k != c.k)) {
			t.Fatalf("isPerfectPower(%d) = %d,%d,%v", c.n, b, k, ok)
		}
	}
}

// TestShorDist: normalized, and peaked exactly at round(2^t·k/r).
func TestShorDist(t *testing.T) {
	for _, r := range []int{2, 3, 4, 6, 10} {
		P := shorDist(r, 8)
		sum := 0.0
		for _, p := range P {
			sum += p
		}
		if math.Abs(sum-1) > 1e-9 {
			t.Fatalf("r=%d: sums to %g", r, sum)
		}
		for k := 0; k < r; k++ {
			y := int(math.Round(256*float64(k)/float64(r))) % 256
			if P[y] < 0.4/float64(r) {
				t.Fatalf("r=%d: no peak at y=%d (k=%d), P=%g", r, y, k, P[y])
			}
		}
	}
}

// TestCFDenominator: exact phases recover their denominator.
func TestCFDenominator(t *testing.T) {
	for _, c := range []struct {
		y, T, limit, want int
	}{
		{64, 256, 15, 4},  // 1/4
		{85, 256, 15, 3},  // ≈ 1/3
		{171, 256, 15, 3}, // ≈ 2/3
		{0, 256, 15, 1},
		{128, 256, 15, 2}, // 1/2
	} {
		if got := cfDenominator(c.y, c.T, c.limit); got != c.want {
			t.Fatalf("cf(%d/%d) = %d, want %d", c.y, c.T, got, c.want)
		}
	}
}

// TestShorFactors: every valid a for several toy N either factors N
// correctly or reports the two textbook unlucky cases.
func TestShorFactors(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for _, n := range []int{15, 21, 33, 35, 39} {
		for a := 2; a < n; a++ {
			if gcdInt(a, n) != 1 {
				continue
			}
			out, err := shorReport(n, a, 0, rng)
			if err != nil {
				t.Fatalf("N=%d a=%d: %v", n, a, err)
			}
			r := mulOrder(a, n)
			if r%2 == 1 {
				if !strings.Contains(out, "r is odd") {
					t.Fatalf("N=%d a=%d: expected odd-r report:\n%s", n, a, out)
				}
				continue
			}
			if modPow(a, r/2, n) == n-1 {
				if !strings.Contains(out, "≡ -1 mod N") {
					t.Fatalf("N=%d a=%d: expected -1 report:\n%s", n, a, out)
				}
				continue
			}
			if !strings.Contains(out, "× ") || !strings.Contains(out, "order found: r =") {
				t.Fatalf("N=%d a=%d: expected factors:\n%s", n, a, out)
			}
		}
	}
}

func TestShorCommand(t *testing.T) {
	out, err := shorCommand("15 a=7")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "15 = 3 × 5") && !strings.Contains(out, "15 = 5 × 3") {
		t.Fatalf("15 with a=7 must factor:\n%s", out)
	}
	for arg, want := range map[string]string{
		"16": "even",
		"27": "perfect power",
		"17": "prime",
	} {
		out, err := shorCommand(arg)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, want) {
			t.Fatalf(":shor %s should say %q:\n%s", arg, want, out)
		}
	}
	for _, bad := range []string{"", "abc", "15 a=x", "15 t=0", "5"} {
		if _, err := shorCommand(bad); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}

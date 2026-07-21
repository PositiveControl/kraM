package main

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"
)

// Shor period-finding, toy sizes. Factoring N reduces to finding the
// multiplicative order r of a random a mod N: if r is even and a^(r/2) ≢ -1,
// then gcd(a^(r/2) ± 1, N) are proper factors.
//
// The quantum part is QPE on the permutation U: |x⟩ → |a·x mod N⟩. The start
// state |1⟩ decomposes over U's r eigenvectors (eigenphases k/r) with weight
// exactly 1/r, so the t-qubit readout distribution is a 1/r-mix of QPE
// kernels — the same closed form :count uses, no statevector needed. Each
// sampled readout y ≈ 2^t·k/r; continued fractions on y/2^t recover a
// divisor of r, and the lcm of a few samples reaches r itself.
//
// Simulator-only, like :count. The gate-level story (compiling a·x mod N as
// reversible kraM arithmetic) is future work tracked in the roadmap.

func gcdInt(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

func modPow(a, e, n int) int {
	r := 1
	a %= n
	for ; e > 0; e >>= 1 {
		if e&1 == 1 {
			r = r * a % n
		}
		a = a * a % n
	}
	return r
}

// mulOrder is the multiplicative order of a mod n; a must be coprime to n.
func mulOrder(a, n int) int {
	x, r := a%n, 1
	for x != 1 {
		x = x * a % n
		r++
	}
	return r
}

// isPerfectPower reports n = b^k with k ≥ 2, a case the order trick cannot
// factor (and which Shor's classical preamble handles separately).
func isPerfectPower(n int) (int, int, bool) {
	for k := 2; 1<<k <= n; k++ {
		b := int(math.Round(math.Pow(float64(n), 1/float64(k))))
		for _, c := range []int{b - 1, b, b + 1} {
			if c >= 2 {
				p := 1
				for i := 0; i < k && p <= n; i++ {
					p *= c
				}
				if p == n {
					return c, k, true
				}
			}
		}
	}
	return 0, 0, false
}

// shorDist is the exact QPE readout distribution for order r.
func shorDist(r int, t uint) []float64 {
	P := make([]float64, 1<<t)
	for y := range P {
		for k := 0; k < r; k++ {
			P[y] += qpeKernel(float64(k)/float64(r), t, y) / float64(r)
		}
	}
	return P
}

// cfDenominator returns the best continued-fraction convergent denominator
// of y/2^t below limit — the candidate divisor of r.
func cfDenominator(y, T, limit int) int {
	// convergent denominators via the standard recurrence k_i = a_i·k_{i-1} + k_{i-2}
	num, den := y, T
	prev, cur := 1, 0 // k_{-2}, k_{-1}
	best := 1
	for den != 0 {
		q := num / den
		num, den = den, num%den
		prev, cur = cur, q*cur+prev
		if cur >= limit {
			break
		}
		if cur > 0 {
			best = cur
		}
	}
	return best
}

// shorSample draws a readout from the exact distribution.
func shorSample(P []float64, rng *rand.Rand) int {
	u := rng.Float64()
	acc := 0.0
	for y, p := range P {
		acc += p
		if u <= acc {
			return y
		}
	}
	return len(P) - 1
}

type shorTrace struct {
	y, den int
	r      int // accumulated lcm so far
}

// shorFindOrder samples readouts and accumulates CF denominators by lcm
// until the true order is hit (checked with a^cand ≡ 1, a classical query).
func shorFindOrder(a, n int, P []float64, T int, rng *rand.Rand) ([]shorTrace, int, error) {
	acc := 1
	var trace []shorTrace
	for i := 0; i < 32; i++ {
		y := shorSample(P, rng)
		d := cfDenominator(y, T, n)
		acc = acc / gcdInt(acc, d) * d
		trace = append(trace, shorTrace{y, d, acc})
		if modPow(a, acc, n) == 1 {
			// acc is a multiple of the order; take its smallest divisor that
			// still satisfies a^d ≡ 1 (lcm accumulation can overshoot).
			for d := 1; d <= acc; d++ {
				if acc%d == 0 && modPow(a, d, n) == 1 {
					return trace, d, nil
				}
			}
		}
	}
	return nil, 0, fmt.Errorf("order not found in 32 samples — astronomically unlikely")
}

// shorCommand parses the REPL argument form "<N> [a=<k>] [t=<k>]".
func shorCommand(args string) (string, error) {
	usage := fmt.Errorf("usage: :shor <N> [a=<base>] [t=<counting qubits>]")
	fields := strings.Fields(args)
	if len(fields) < 1 {
		return "", usage
	}
	var n int
	if _, err := fmt.Sscanf(fields[0], "%d", &n); err != nil {
		return "", usage
	}
	a, t := 0, 0
	for _, f := range fields[1:] {
		switch {
		case strings.HasPrefix(f, "a="):
			if _, err := fmt.Sscanf(f, "a=%d", &a); err != nil {
				return "", usage
			}
		case strings.HasPrefix(f, "t="):
			if _, err := fmt.Sscanf(f, "t=%d", &t); err != nil || t < 1 || t > 20 {
				return "", fmt.Errorf("bad t= value (1..20)")
			}
		default:
			return "", usage
		}
	}
	return shorReport(n, a, t, rand.New(rand.NewSource(rand.Int63())))
}

// shorReport runs the classical preamble, the exact QPE sampling loop, and
// the postprocessing, narrating each step.
func shorReport(n, a, t int, rng *rand.Rand) (string, error) {
	if n < 15 || n > 4096 {
		return "", fmt.Errorf("N must be 15..4096 (toy sizes), got %d", n)
	}
	if n%2 == 0 {
		return fmt.Sprintf("N=%d is even — classical preamble: %d = 2 × %d, no quantum needed", n, n, n/2), nil
	}
	if b, k, ok := isPerfectPower(n); ok {
		return fmt.Sprintf("N=%d = %d^%d — perfect power, handled classically in Shor's preamble", n, b, k), nil
	}
	prime := true
	for d := 3; d*d <= n; d += 2 {
		if n%d == 0 {
			prime = false
			break
		}
	}
	if prime {
		return fmt.Sprintf("N=%d is prime — nothing to factor", n), nil
	}

	if a == 0 {
		for {
			a = 2 + rng.Intn(n-3)
			if gcdInt(a, n) == 1 {
				break
			}
			// a shares a factor with N: that IS a factor — but as a random
			// draw it short-circuits the demo, so redraw and mention nothing.
		}
	}
	if a < 2 || a >= n {
		return "", fmt.Errorf("a must be 2..N-1")
	}
	if g := gcdInt(a, n); g > 1 {
		return fmt.Sprintf("lucky pick: gcd(%d, %d) = %d — factor found classically, %d = %d × %d",
			a, n, g, n, g, n/g), nil
	}

	bitsN := 0
	for 1<<bitsN < n {
		bitsN++
	}
	if t == 0 {
		t = 2 * bitsN // standard choice: enough resolution for continued fractions
	}
	T := 1 << t

	r := mulOrder(a, n) // ground truth, used to build the exact distribution
	P := shorDist(r, uint(t))
	trace, found, err := shorFindOrder(a, n, P, T, rng)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "N = %d (%d bits), a = %d\n", n, bitsN, a)
	fmt.Fprintf(&b, "QPE on U: |x⟩ → |%d·x mod %d⟩ — %d counting qubits, %d work qubits, resolution 1/%d\n",
		a, n, t, bitsN, T)
	fmt.Fprintf(&b, "true order (from classical simulation): r = %d\n", r)

	// top readout peaks, labeled with the k/r they encode
	type pk struct {
		y int
		p float64
	}
	var peaks []pk
	for y, p := range P {
		peaks = append(peaks, pk{y, p})
	}
	sort.Slice(peaks, func(i, j int) bool { return peaks[i].p > peaks[j].p })
	fmt.Fprintf(&b, "readout distribution peaks near y = %d·k/%d:\n", T, r)
	shown := 0
	for _, pk := range peaks {
		if shown == 4 || pk.p < 0.005 {
			break
		}
		k := int(math.Round(float64(pk.y) * float64(r) / float64(T)))
		fmt.Fprintf(&b, "  y=%-5d p=%.4f  (≈ %d/%d)\n", pk.y, pk.p, k, r)
		shown++
	}

	fmt.Fprintln(&b, "sampled runs — continued fractions on y/2^t, orders combined by lcm:")
	for i, tr := range trace {
		fmt.Fprintf(&b, "  run %d: y=%-5d → denominator %d → candidate r=%d\n", i+1, tr.y, tr.den, tr.r)
	}
	fmt.Fprintf(&b, "order found: r = %d (verified %d^%d ≡ 1 mod %d)\n", found, a, found, n)

	if found%2 == 1 {
		fmt.Fprintf(&b, "r is odd — unlucky a, no factors this run; retry (or pass a=…)")
		return b.String(), nil
	}
	half := modPow(a, found/2, n)
	if half == n-1 {
		fmt.Fprintf(&b, "a^(r/2) ≡ -1 mod N — unlucky a, no factors this run; retry (or pass a=…)")
		return b.String(), nil
	}
	p, q := gcdInt(half-1, n), gcdInt(half+1, n)
	fmt.Fprintf(&b, "a^(r/2) = %d → gcd(%d±1, %d) → %d = %d × %d", half, half, n, n, p, q)
	if p*q != n {
		return "", fmt.Errorf("internal error: %d × %d != %d", p, q, n)
	}
	return b.String(), nil
}

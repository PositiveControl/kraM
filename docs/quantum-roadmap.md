# Quantum algorithm roadmap

Candidate algorithms for the simulator/REPL, ranked by how well they exploit
kraM's unique trick: classical reversible code compiled to garbage-free
oracles. `:grover` shipped first; this is the queue behind it.

## 1. Bernstein–Vazirani — `:bv` (shipped)

Hidden n-bit string s, oracle computes f(x) = s·x (mod 2). Classically
recovering s takes n queries (probe each bit); the quantum circuit needs
**one**: H layer, phase-kickback oracle (one CNOT per set bit of s), H layer,
measure — the result is s, deterministically.

- Oracle is a bare CNOT chain: shallow, transpiles cleanly, near-ideal
  results on real hardware — a stronger hardware demo than Grover.
- Measured on `ibm_marrakesh` (6 bits, s=37, 4096 shots): **x=37 at 96.3%**,
  transpiled depth 10 vs Grover's 178. The stray ~1% states are single-bit
  readout flips of 37. Sample export: `hardware/bv.qasm`.
- Effort: small. Reuses the qop/QASM/statevector infrastructure.

## 2. Deutsch–Jozsa — `:dj` (shipped)

Promise: f is either constant or balanced. One query decides which;
classically it takes 2^(n-1)+1 evaluations in the worst case. The original
quantum algorithm (1992).

- Strong kraM angle: the user writes *any* condition, the compiler proves the
  oracle garbage-free, DJ classifies it in a single evaluation. Measure all
  zeros → constant, anything else → balanced.
- Measured on `ibm_marrakesh` (`x < 4`, 3 bits, 4096 shots): 84% nonzero →
  correct **balanced** verdict, with the ideal peak x=4 on top at 37.5%. The
  comparator oracle (48 gates, 12 wires) transpiled to depth 446 — deeper
  than Grover — so the noise floor is high even though the verdict is clear.
  Sample export: `hardware/dj.qasm`.
- Shares the single-query harness with `:bv`. Effort: small.

## 3. Quantum counting — `:count` (shipped)

Not "find an x satisfying the condition" but "how many x satisfy it" —
Grover's operator run inside quantum phase estimation; the rotation angle
encodes M/N. Thematic rhyme with `range.kr`, which counts hits classically.

- Shipped as exact measurement statistics: the uniform start state overlaps
  each Grover eigenvector with weight exactly 1/2, so the readout
  distribution is an equal mix of two QPE kernels — computed analytically,
  cross-checked in tests against a direct unitary simulation (explicit G^k
  powers + DFT, no subspace shortcut).
- Default t = bits+2 counting qubits rounds to the exact M on every test
  oracle; `t=K` overrides to show resolution trade-offs.
- Simulator-only, by design: controlled-Grover leaves the qelib1 gate set
  and QPE is far too deep for today's hardware. No QASM export.
- Studio idea still open: visualize the estimated phase converging as
  counting qubits are added.

## 4. Simon's algorithm — `:simon` (shipped)

Hidden XOR period: f(x) = f(x ⊕ s) for all x. Each run yields a random y
with y·s = 0; after ~n runs, solving the linear system over GF(2) recovers s.
Exponential quantum/classical separation; the direct ancestor of Shor.

- Shipped with the standard oracle f(x) = x ⊕ (x_l ? s : 0) (l = lowest set
  bit of s): a CNOT copy plus a controlled XOR of s — the first
  function-into-register oracle in the codebase, and all CNOTs, so it runs
  well on hardware.
- The REPL flow samples y from {y : y·s = 0} — TestSimonStatevector proves
  gate-for-gate that this is exactly the circuit's output distribution —
  then row-reduces over GF(2), showing every equation. The rank-(n-1)
  candidate is settled by the textbook final step, one classical
  verification query f(0) =? f(candidate), which also disambiguates s = 0
  (injective f).
- Promise f(x) = f(x⊕s) is verified exhaustively before running.

## 5. Shor period-finding (toy) — `:shor` (shipped)

Factor 15 or 21 in the simulator. The quantum core is period-finding over
modular multiplication: QPE on U: |x⟩ → |a·x mod N⟩, whose eigenphases are
k/r for the order r of a.

- Shipped as exact measurement statistics, the same closed form as `:count`:
  the start state |1⟩ decomposes over U's r eigenvectors with weight exactly
  1/r, so the readout distribution is a 1/r-mix of QPE kernels. Sampled
  readouts go through continued fractions (lcm-combined across runs, then
  reduced to the smallest verifying divisor) and gcd(a^(r/2) ± 1, N).
- Classical preamble handled: even N, prime N, perfect powers. Unlucky a
  (odd r, or a^(r/2) ≡ -1) reported as the textbook retry cases.
- Tested exhaustively: every coprime a for N ∈ {15, 21, 33, 35, 39}.
- **Still open (the flagship gate-level story):** compile `a*x mod N` as
  reversible kraM arithmetic into an actual controlled-multiplier circuit,
  so the oracle is built by the language rather than analyzed in closed
  form. Tracked here as future work.

## 6. Studio parity — next up

`:bv`, `:dj`, `:count`, `:simon`, `:shor` are REPL-only; the Studio has a
Grover pane only. The WASM bridge (`wasm.go`) already exists — add panes for
the new algorithms so the whole arc is visible in the browser: BV/DJ
single-query circuits with their QASM download, counting's readout
distribution, Simon's equation-by-equation elimination, Shor's peak
spectrum and continued-fraction trace.

## Ruled out (for now)

- **QAOA / VQE** — variational hybrids; no compiled-oracle story, and the
  classical optimizer loop doesn't fit the REPL shape.
- **Teleportation / superdense coding** — nice protocol demos but they never
  touch the compiler.
- **Quantum walks** — cute next to `ca.kr`, but niche until the CA work
  grows.

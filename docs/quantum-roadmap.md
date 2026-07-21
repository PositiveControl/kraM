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
- Effort: small. Reuses the qop/QASM/statevector infrastructure.

## 2. Deutsch–Jozsa — `:dj`

Promise: f is either constant or balanced. One query decides which;
classically it takes 2^(n-1)+1 evaluations in the worst case. The original
quantum algorithm (1992).

- Strong kraM angle: the user writes *any* condition, the compiler proves the
  oracle garbage-free, DJ classifies it in a single evaluation. Measure all
  zeros → constant, anything else → balanced.
- Shares the single-query harness with `:bv`. Effort: small.

## 3. Quantum counting — `:count`

Not "find an x satisfying the condition" but "how many x satisfy it" —
Grover's operator run inside quantum phase estimation; the rotation angle
encodes M/N. Thematic rhyme with `range.kr`, which counts hits classically.

- Direct extension of the existing `:grover` machinery.
- Simulator-only: QPE is far too deep for today's hardware.
- Studio idea: visualize the estimated phase converging as counting qubits
  are added. Effort: medium (needs QFT and controlled-Grover plumbing).

## 4. Simon's algorithm — `:simon`

Hidden XOR period: f(x) = f(x ⊕ s) for all x. Each run yields a random y
with y·s = 0; after ~n runs, solving the linear system over GF(2) recovers s.
Exponential quantum/classical separation; the direct ancestor of Shor.

- Needs a function-into-register oracle (f(x) written to an output register)
  rather than a bit-flip marker — kraM procedures compile to exactly that
  shape.
- Classical post-processing: Gaussian elimination mod 2.
- Effort: medium-large (new oracle shape + linear algebra + multi-shot flow).

## 5. Shor period-finding (toy) — `:shor`

Factor 15 or 21 in the simulator. The quantum core is period-finding over
reversible modular exponentiation — and reversible arithmetic is kraM's whole
thing: `a*x mod N` written as kraM code, compiled to a circuit, would be the
flagship "homemade language factors a number" demo.

- Needs: QFT, controlled modular multiplication, ~10+ qubits.
- Simulator-only, and the largest effort on the list. Long-term flagship.

## Ruled out (for now)

- **QAOA / VQE** — variational hybrids; no compiled-oracle story, and the
  classical optimizer loop doesn't fit the REPL shape.
- **Teleportation / superdense coding** — nice protocol demos but they never
  touch the compiler.
- **Quantum walks** — cute next to `ca.kr`, but niche until the CA work
  grows.

# kraMLang

An experimental **reversible** programming language with a time-travel REPL,
written in Go. Short name: **kraM**.

Every change to state is recorded with its inverse, so you can step backwards
through anything you run. A subset of the language is reversible *by
construction* — programs that can run backward by inverting their own text —
which is the groundwork for the long-term goal: compiling to reversible /
adiabatic circuits.

Dynamically typed, imperative, tree-walk interpreter. A research toy, early days.

## Run

```sh
make build        # builds two binaries: kram and krapl
./krapl           # open the REPL
./kram fib.kr     # run a .kr script
```

(Or without building: `go run . fib.kr`, and `go run .` for the REPL.)

```
> x = 2 + 3 * 4
14
> print "hello, " + "world"
hello, world
```

`krapl` opens the interactive REPL; `kram file.kr` runs a script quietly. They
are the same binary, dispatched on the invoked name.

## The idea: reversibility

`=` **introduces** a name; it never overwrites. Re-binding an existing variable
would destroy its value — the irreversible act — so it is an error, not a warning
(the full-Janus discipline: no destructive assignment):

```
> x = 5
> x = 9
error: cannot reassign "x" with '=' — '=' introduces a fresh name;
       use += / -= / ^= / <=> to change it, or `forget x` to erase it first
```

Reversible updates keep the information and can be undone exactly:

```
x += 3      # inverse: x -= 3
x ^= 10     # XOR — self-inverse
a <=> b     # swap — self-inverse
```

To destroy information on purpose, there is one explicit escape hatch —
`forget x`. It erases the variable (freeing the name to re-introduce). It is the
*only* irreversible act in the language, so it is named and visible rather than
hiding inside an innocent-looking `=`; `reverse` / `uncall` refuse it and it
lowers to no gate:

```
> x = 5
> forget x       # deliberate erasure — the one irreversible operation
> x = 9          # the name is free again
```

So you can run a block backward:

```
> a = 0
> reverse { a += 5; a += 3 }   # runs a -= 3; a -= 5
> print a
0
```

Arrays support the same reversible element operations (`reverse.kr` reverses an
array in place, then undoes it):

```
> xs = [1, 2, 3]
> xs[0] += 10      # element update
> xs[0] <=> xs[2]  # element swap
> xs
[3, 2, 11]
```

Name a reversible block as a procedure (with by-reference parameters), then run
it either direction — `call` forward, `uncall` backward:

```
> proc add(dst, src) { dst += src }
> call add(x, y)     # x += y
> uncall add(x, y)   # x -= y  — the same procedure, reversed
```

Scoped temporaries use `local` / `delocal` (the Janus discipline): `local`
introduces a fresh variable, `delocal` removes it while asserting its value.
They are exact inverses, so a temporary stays reversible — the asserted value is
what lets it be removed without leaving garbage. In circuits a local lowers to
an **ancilla register** (allocated, used, then uncomputed and freed):

```
> local t = 0
> t += x          # use t
> delocal t = x   # remove t, asserting it now equals x
```

The compute-copy-uncompute pattern (Bennett's trick) has dedicated syntax:
`with` runs its compute block, then the body, then the compute block's
*structural inverse*, and delocals the ancilla — the uncompute is derived from
the program text, so it can never drift out of sync with the compute. The
compute block must be reversible; that is checked at parse time:

```
> with t = 0 { t += x; t += x } do { out += t }
# ≡ local t = 0; t += x; t += x; out += t; t -= x; t -= x; delocal t = 0
```

…and step through time:

```
> x = 1; x += 1; x += 1
> :history
> :undo        # walk back one mutation at a time
```

## Demo: reversible Fibonacci

`fib.kr` computes Fibonacci with only reversible updates, then runs the loop
backward to recover the inputs exactly.

```sh
make build
./kram fib.kr
```
```
fib(10) pair: 55, 89
reversed back to: a=0, b=1, i=0
```

Explore it interactively in the REPL:

```sh
./krapl
```
```
a = 0; b = 1; i = 0; n = 10
proc fibstep(x, y) { x += y; x <=> y }
:verify from i == 0 { } loop { call fibstep(a, b); i += 1 } until i == n
from i == 0 { } loop { call fibstep(a, b); i += 1 } until i == n
print "got " + a + ", " + b
:history     # every mutation — all reversible
```

`:verify` unrolls the loop, lowers it to `X / CNOT / Toffoli` gates, simulates
them, and confirms the circuit matches the interpreter. `:gates <same code>`
prints the netlist; `:undo` walks the computation backward one step at a time.

Note: `:verify` / `:gates` / `:circuit` compile against the *current* variables,
so run them while the loop's start condition still holds (here, before running
the loop for real, while `i == 0`).

One demo, the whole language: reversible updates, a parameterized procedure, a
reversible loop, time travel, and verified compilation to a reversible circuit.

## Demos

Each `.kr` demo highlights a different facet. Run with `./kram <file>`; see
[docs/demos.md](docs/demos.md) for a walk-through of each.

| Demo | Use-case — what it demonstrates | Compiles to a circuit? |
|------|----------------------------------|:---:|
| `fib.kr` | reversible arithmetic, a parameterized procedure, a reversible loop — run forward then backward to recover the inputs | ✅ |
| `reverse.kr` | in-place array reversal by element swaps with computed indices | ✅ |
| `compute.kr` | compute → copy → uncompute with a `local` ancilla — the garbage-free technique of reversible computing | ✅ |
| `ca.kr` | a reversible Margolus cellular automaton on a grid — mix a pattern, then scrub the timeline to un-mix it exactly | ✅ (nested loops unroll) |
| `sort.kr` | making an irreversible algorithm (bubble sort) reversible by recording a swap trace | interpreter-only |
| `gcd.kr` | the same for Euclid's GCD by subtraction (branch trace) | interpreter-only |
| `range.kr` | counting elements in `[lo, hi]` — a compound condition with a count trace | interpreter-only |

### Why some demos are interpreter-only

Every demo is **reversible** — `uncall` and the time-travel timeline run it
backward exactly. But not every reversible program compiles to a *fixed gate
circuit*. A circuit is static wiring: the compiler must know at compile time
exactly which gates fire, on which wires, in what order. What defeats that is
**data-dependent control flow** (`sort`, `gcd`, `range`): their reversible `if`
branches on the very values the body is changing — *did this pair swap? which
operand was larger?* — and `gcd`'s loop length depends on its inputs. The gate
sequence would differ from one input to the next, so there is no single wiring;
the branch is decided at *run* time, and its outcome recorded in a trace. That
trace is the whole point — exactly the information the irreversible algorithm
would otherwise destroy, and what lets `uncall` reconstruct the input from the
output.

Everything whose control flow is fixed at compile time *does* lower, even when
it looks complex: the compiler unrolls loops (including **nested** loops, like
`ca`'s grid sweep — each inner pass re-unrolls against the advancing compile-time
state), inlines procedures, prepares array literals element by element, and
folds loop-invariant index/bound expressions to constants. `fib`, `reverse`,
`compute`, and `ca` all lower to `X / CNOT / Toffoli` gates whose circuit is
checked against the interpreter (`:verify`) and costed with Landauer's bound
(`:energy` — 0 J when every `local` ancilla is `delocal`'d back to clean).

Note: the compile commands lower a whole program from cold (its `=`
initialisation included). A demo *file* that runs a computation forward and then
backward (`fib.kr`/`reverse.kr` end with a `reverse { … }` pass) is two passes,
not one circuit — compile the single forward pass, not the whole file.

## Commands

`:undo` `:redo` `:history` `:env` — time travel and inspection
`:load` `:step` — load a program and run it one mutation at a time
`:invert CODE` — print a program's structural inverse
`:circuit CODE` — compile reversible code to a register-level netlist
`:gates CODE` — compile to elementary X / CNOT / Toffoli gates (adds use a Cuccaro adder)
`:verify CODE` — check the compiled circuit matches the interpreter
`:energy CODE` — Landauer energy bound from the circuit's garbage bits
`:grover BITS COND [iters=K] [qasm]` — Grover-search a compiled oracle (see below)
`:bv BITS S [qasm]` — Bernstein–Vazirani: recover hidden S in one oracle query
`:dj BITS COND [qasm]` — Deutsch–Jozsa: COND constant or balanced, in one query
`:count BITS COND [t=K]` — quantum counting: estimate how many x satisfy COND
`:simon BITS S [qasm]` — Simon: recover hidden XOR period S in O(BITS) queries
`:shor N [a=K] [t=K]` — Shor period-finding: factor toy N via QPE on a·x mod N
`:qasm CODE` — export a compiled program as OpenQASM 2.0
`:reset` `:help` — clear state, list commands

Shorthands: `_` = last result, `!!` = last line (e.g. `reverse { !! }`).

## Quantum: Grover search on compiled oracles

kraM's `if`-condition compiler is, accidentally on purpose, a quantum oracle
synthesizer: `condToBit` computes any comparison/`&&`/`||`/`!` condition into a
single marker bit and uncomputes *all* of its scratch — the compute-copy-uncompute
discipline quantum oracles require. `:grover` exploits that:

```
> :grover 4 x == 9 || x == 3
oracle: x == 9 || x == 3 over 4-bit x — 47 gates, 10 wires (4 input + 1 marker + 5 ancilla)
search space N=16, solutions M=2 [3 9]
optimal iterations k* = 2
  after  0 iterations: P(marked) = 0.1250 █████
  after  1 iterations: P(marked) = 0.7812 ███████████████████████████████
  after  2 iterations: P(marked) = 0.9453 ██████████████████████████████████████
```

The simulation is exact, not approximate: the oracle is verified garbage-free
for every basis state and the marker is only ever a gate target, so the
amplitude evolution over the 2^bits inputs equals the full statevector's
(cross-checked in tests by simulating the exported circuit wire-for-wire).

Add `qasm` to export the complete Grover circuit — superposition layer, phase
kickback, diffusion — as OpenQASM 2.0, and run it on real IBM Quantum hardware:
see [hardware/](hardware/). The Studio has a matching pane: type a condition,
watch the amplitude bars converge, download the `.qasm`.

`:bv BITS S` is Bernstein–Vazirani: a hidden string S is recovered from the
linear oracle f(x) = S·x (mod 2) in a single query (classically it takes BITS
queries). The oracle is a bare CNOT chain, so unlike Grover it survives real
hardware nearly intact.

`:dj BITS COND` is Deutsch–Jozsa: one query decides whether COND is constant
or balanced (classically: 2^(BITS-1)+1 evaluations worst case). Measure all
zeros → constant, anything else → balanced. Conditions that are neither break
the promise; the report says so and gives the exact P(all zeros).

`:count BITS COND` is quantum counting: phase estimation on the Grover
operator estimates *how many* x satisfy COND without finding them — the
quantum sibling of `range.kr`'s classical hit-counting. The report shows the
exact QPE readout distribution (cross-checked in tests against a direct
unitary simulation) and the M it rounds to. Simulator-only: controlled-Grover
plus QFT is far beyond today's hardware depth budgets.

`:simon BITS S` is Simon's algorithm: a hidden XOR period (f(x) = f(x⊕S))
recovered in O(BITS) circuit runs plus Gaussian elimination over GF(2), where
classical search needs ~2^(BITS/2) queries — the exponential separation that
led to Shor. Each run yields a random y with y·S = 0; the report shows every
equation and the elimination verdict. The oracle is all CNOTs, so `qasm`
exports run well on real hardware.

`:shor N` factors a toy composite by Shor period-finding: QPE on the
permutation |x⟩ → |a·x mod N⟩ yields readouts near 2^t·k/r, continued
fractions recover the order r, and gcd(a^(r/2) ± 1, N) splits N. The readout
distribution is exact (same closed form as `:count`); the classical preamble
(even, prime, perfect-power N) and the two textbook unlucky-a cases are all
reported honestly. `:shor 15` → 15 = 3 × 5. More in
[docs/quantum-roadmap.md](docs/quantum-roadmap.md).

## Status

Early sketch. `:circuit` is a register-level view (whole ADD / SUB / SWAP /
CNOT blocks); `:gates` decomposes to real elementary gates (X / CNOT / Toffoli,
16-bit registers by default — `:grover`/`:qasm` compile narrower ones so they
fit on real quantum hardware — arithmetic mod 2^width). Both unroll reversible loops and
inline procedures against the current state; `:circuit` doesn't yet lower a
reversible `if` (use `:gates` for that). `:gates` lowers procedures (inlined) and reversible `if`s — conditions can
compare a variable to a constant or to another variable (`== != < > <= >=`),
via an equality check or a subtract-based comparator — and `&& || !`
combinations of those — to controlled gates. Reversible loops are unrolled using the iteration count from the current state
(the circuit is specialised to that count); nested loops unroll too — each inner
pass re-unrolls against the advancing compile-time state. Ancilla wires are recycled, so a circuit's width is bounded by
peak concurrent scratch, not program length. `+=`/`-=` aren't bit-exact in the
*interpreter* (`^=` is); the *gate* circuit is exact mod 2^16. Array element ops
lower to circuits when the index folds to a constant at compile time — including
loop-varying indices like `xs[n-1-i]`, which fold per unrolled iteration (each
element becomes its own register). Genuinely dynamic indexing (a runtime address
the compiler can't fold) would need a reversible address decoder and is not
lowered.

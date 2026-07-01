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
`:reset` `:help` — clear state, list commands

Shorthands: `_` = last result, `!!` = last line (e.g. `reverse { !! }`).

## Status

Early sketch. `:circuit` is a register-level view (whole ADD / SUB / SWAP /
CNOT blocks); `:gates` decomposes to real elementary gates (X / CNOT / Toffoli,
fixed 16-bit registers, arithmetic mod 2^16). Both unroll reversible loops and
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

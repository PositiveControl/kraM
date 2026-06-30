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

Overwriting a variable destroys information, so it warns:

```
> x = 5
> x = 9
⚠ destructive overwrite of "x" — use += / -= / <=> to stay reversible
```

Reversible updates keep the information and can be undone exactly:

```
x += 3      # inverse: x -= 3
x ^= 10     # XOR — self-inverse
a <=> b     # swap — self-inverse
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
what lets it be removed without leaving garbage:

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

`./kram sort.kr` is a deeper one — a reversible bubble sort. Sorting isn't
reversible on its own (many inputs collapse to one sorted output), so each
compare-exchange records whether it swapped into a trace array; that recorded
bit is the reversible-if's exit assertion. `uncall` replays the trace backward
and restores the original order exactly.

`./kram gcd.kr` does the same for Euclid's GCD (by subtraction): each step
records which value it reduced, so `uncall` recovers the two inputs from the
gcd and the trace — even though gcd alone is many-to-one.

See [docs/demos.md](docs/demos.md) for what each of the four demos
(`fib.kr`, `reverse.kr`, `sort.kr`, `gcd.kr`) showcases.

## Commands

`:undo` `:redo` `:history` `:env` — time travel and inspection
`:load` `:step` — load a program and run it one mutation at a time
`:invert CODE` — print a program's structural inverse
`:circuit CODE` — compile reversible code to a register-level netlist
`:gates CODE` — compile to elementary X / CNOT / Toffoli gates (adds use a Cuccaro adder)
`:verify CODE` — check the compiled circuit matches the interpreter
`:reset` `:strict` `:help` — clear state, enforce reversibility, list commands

Shorthands: `_` = last result, `!!` = last line (e.g. `reverse { !! }`).

## Status

Early sketch. `:circuit` is a register-level view; `:gates` decomposes to real
elementary gates (X / CNOT / Toffoli, fixed 16-bit registers, arithmetic mod
2^16). `:gates` lowers procedures (inlined) and reversible `if`s — conditions can
compare a variable to a constant or to another variable (`== != < > <= >=`),
via an equality check or a subtract-based comparator, to controlled gates.
Reversible loops are unrolled using the iteration count from the current state
(the circuit is specialised to that count); nested loops can't be unrolled. Ancilla wires are recycled, so a circuit's width is bounded by
peak concurrent scratch, not program length. `+=`/`-=` aren't bit-exact in the
*interpreter* (`^=` is); the *gate* circuit is exact mod 2^16. Array element ops
lower to circuits when the index folds to a constant at compile time — including
loop-varying indices like `xs[n-1-i]`, which fold per unrolled iteration (each
element becomes its own register). Genuinely dynamic indexing (a runtime address
the compiler can't fold) would need a reversible address decoder and is not
lowered.

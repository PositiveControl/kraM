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
go run .
```

```
> x = 2 + 3 * 4
14
> print "hello, " + "world"
hello, world
```

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

Name a reversible block as a procedure, then run it either direction —
`call` forward, `uncall` backward:

```
> proc cipher { msg ^= key }
> call cipher     # encrypt
> uncall cipher   # decrypt — the same procedure, reversed
```

…and step through time:

```
> x = 1; x += 1; x += 1
> :history
> :undo        # walk back one mutation at a time
```

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
2^16). `:gates` lowers procedures (inlined) and reversible `if`s with
`var == const` conditions (controlled gates); loops can't become a fixed circuit
and are rejected. `+=`/`-=` aren't bit-exact in the *interpreter* (`^=` is); the
*gate* circuit is exact mod 2^16.

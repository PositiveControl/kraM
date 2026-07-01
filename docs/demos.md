# Demos

Four `.kr` programs, each highlighting a different facet of reversible
computing. Run any with `./kram <file>` (after `make build`).

The arc: the first two show reversibility when it's *free* (the operations are
already invertible); the last two show the deeper case — algorithms that are
inherently **irreversible**, made reversible by recording exactly the
information they would otherwise destroy.

| Demo | Shows | Reversible because |
|------|-------|--------------------|
| `fib.kr` | reversible arithmetic + a parameterized procedure | every step `(x,y)→(y,x+y)` is an invertible update |
| `reverse.kr` | reversible array element ops in a loop | swaps are self-inverse |
| `sort.kr` | making an irreversible algorithm reversible | records a swap trace |
| `gcd.kr` | same, with arithmetic | records a branch trace |

---

## `fib.kr` — reversible Fibonacci

Computes Fibonacci numbers, then runs the loop backward to recover the inputs.

```
fib(10) pair: 55, 89
reversed back to: a=0, b=1, i=0
```

**Showcases**
- **Reversible updates** — `x += y` and `x <=> y` carry no information loss, so
  the step `(x, y) → (y, x+y)` is invertible by construction.
- **Parameterized procedures** — `proc fibstep(x, y)` with by-reference args,
  applied each iteration via `call fibstep(a, b)`.
- **Reversible loop** — `from … loop … until …`.
- **`reverse { … }`** — runs the structural inverse of the whole loop.
- **Verified compilation** — `:verify` unrolls the loop, lowers it to
  `X / CNOT / Toffoli` gates, simulates them, and confirms a match with the
  interpreter (run it while `i == 0`).

**Point:** when the operations are individually reversible, the whole program is
reversible for free — no trace needed.

---

## `reverse.kr` — reversible in-place array reversal

Reverses an array by swapping `xs[i]` with `xs[n-1-i]`, then undoes it.

```
before: [1, 2, 3, 4, 5, 6]
after:  [6, 5, 4, 3, 2, 1]
undone: [1, 2, 3, 4, 5, 6]
```

**Showcases**
- **Arrays** — literals, indexed element swap `xs[i] <=> xs[n-1-i]`.
- **Computed indices** — the index `n-1-i` changes each iteration; under
  `:gates`/`:verify` it folds to a concrete element register per unrolled
  iteration.
- **Self-inverse work** — a swap undoes itself, so reversing the array twice is
  the identity; `reverse { … }` restores the original.

**Point:** reversibility extends cleanly to data structures when the element
operations are invertible.

---

## `sort.kr` — reversible sort

A bubble sort that sorts the array **and** keeps a trace of which compares
swapped, so it can be undone.

```
before:   [5, 2, 4, 1, 3]
sorted:   [1, 2, 3, 4, 5]
trace:    [1, 1, 1, 1, 0, 1, 1, 1, 0, 0]
restored: [5, 2, 4, 1, 3]
```

**Showcases**
- **Irreversibility, confronted** — sorting maps many inputs to one sorted
  output, so it *cannot* be undone from the result alone (a counting argument:
  not injective).
- **The trace trick** — each compare-exchange records into `sw[m]` whether it
  swapped. That recorded bit is precisely the **reversible-`if` exit
  assertion**: `assert sw[m] == 1` holds iff the swap branch ran.
- **`call` / `uncall`** — `call sortit` sorts and fills the trace; `uncall
  sortit` replays the inverse, using the trace to decide each un-swap, and
  restores the original order.

**Point:** an irreversible algorithm becomes a bijection — `input ↔ (sorted
output, trace)` — by keeping the information it would otherwise discard. The
trace is the "garbage" reversible computing must carry.

---

## `gcd.kr` — reversible GCD

Euclid's algorithm by repeated subtraction (no modulo needed — just `-=`),
recording each step's branch so the inputs can be recovered from the gcd.

```
inputs:   a=48, b=36
gcd:      12
trace:    [1, 0, 0, …]
restored: a=48, b=36
```

**Showcases**
- **The same principle, in arithmetic** — gcd is many-to-one (countless pairs
  share a gcd), so it's irreversible on its own.
- **Branch trace** — each step does `a -= b` or `b -= a`; the choice is recorded
  in `t[k]`, again serving as the reversible-`if` exit assertion.
- **Reversible loop with a data-dependent length** — the loop runs until
  `a == b`; the trace length records how many steps it took.

**Point:** `(a, b) ↔ (gcd, trace)` is a bijection. `uncall gcd` walks the trace
backward and reconstructs both inputs from the single gcd value.

---

## Note on circuits

`fib.kr` and `reverse.kr` lower to reversible gate circuits (`:gates` /
`:verify`) — straight-line and constant/loop-folded-index code. `sort.kr` and
`gcd.kr` run in the interpreter only: their conditionals branch on the data
being sorted/reduced (the `if` modifies the values its own condition reads), and
`gcd.kr`'s loop length is genuinely data-dependent — neither has a fixed wiring.
The reversibility itself (via `uncall`) holds in all four.

---

## Language features

A reference for what the demos draw on.

**Values** — numbers, booleans, strings, `nil`, and arrays (`[1, 2, 3]`, nested,
indexed `a[i]`, bounds-checked).

**Expressions** — `+ - * /`, comparisons `< > <= >= == !=` (equality across
types), string concat with `+`, and boolean `&&` `||` `!` (short-circuit, so
`i < len && a[i] > 0` is a safe guard).

**Reversible updates** (information-preserving, the building blocks of every
demo):
- `x += e` / `x -= e` — add/subtract
- `x ^= e` — XOR (exact, self-inverse)
- `a <=> b` / `a[i] <=> a[j]` — swap (self-inverse)

**Destructive (irreversible) operations** warn, and error under `:strict`:
- `x = e` overwriting an existing value, `a[i] = e`, `print` (output)

**Control flow**
- `if c { … } else { … }` / `else if`, with strict boolean conditions
- `while c { … }` — classic (irreversible) loop
- `from e1 { … } loop { … } until e2` — reversible Janus loop
- `assert c` — reversible runtime check
- `if c { … } else { … } assert exit` — reversible if (the exit assertion
  records which branch ran, so it inverts without a log)

**Procedures** — `proc name(params) { … }`, called `call name(args)` /
`uncall name(args)`; parameters are by-reference. `uncall` runs the inverse.

**Local scope** — `local x = e` introduces a scoped temporary, `delocal x = e`
removes it (asserting its value). Exact inverses, so a temporary stays
reversible; in circuits a local is an ancilla register.

**Reverse / invert** — `reverse { … }` runs a block's structural inverse;
`:invert CODE` prints it.

**REPL & tooling** — `:undo` `:redo` `:history` `:step` (time travel), `:env`
`:output`, `:circuit` / `:gates` / `:verify` (compile to and check reversible
gate circuits), `:strict`, `:reset`. Shorthands `_` (last result) and `!!`
(last line).

**Circuit lowering** reaches: reversible updates, swaps, constant- and
loop-folded-index array elements, procedures (inlined), `local` (→ ancilla),
unrolled loops, and reversible `if`s whose condition is a comparison or any
`&& || !` combination of comparisons. Not lowered: genuinely dynamic array
indices, data-dependent loop lengths, and branches that modify their own
condition variable.

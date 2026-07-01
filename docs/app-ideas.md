# Flagship application — idea menu

Exploration for a state-of-the-art, engaging, technically impressive, generally
useful application built on **kraM** (the reversible language). This is a
decision doc, not a plan yet — one path gets chosen, then planned deep.

## What kraM already gives us to build on

- Reversible-by-construction updates (`+= -= ^= <=>`), destructive-write warnings, `:strict`
- `reverse {}` blocks, `proc` + `call`/`uncall`, `local`/`delocal` (Janus ancilla discipline)
- Reversible `if` (with `&& || !`) and reversible loops (`from…loop…until`)
- **Time-travel REPL** — `:undo :redo :history :step`; every mutation + its inverse recorded
- **Verified compilation to gate circuits** — `:circuit` (register), `:gates` (X/CNOT/Toffoli,
  Cuccaro adder, 16-bit mod 2^16), `:verify` (simulate gates, confirm match with interpreter)
- Trace trick: irreversible algorithms (sort, gcd) made bijective by recording discarded info

The moat: structural reversibility **+** time-travel debugging **+** verified lowering to
quantum-style gate circuits. Few/no languages have all three. Weak leg today = "generally
useful" (it's a demo, not a tool with a job).

---

## The full menu

Tags: **fit** (how naturally kraM's features earn it) / **lift** (build effort) / **wow**
(impressiveness + engagement). ★ out of 5.

### A. Quantum / gate circuits — leans on `:gates`, ancillas, verify

- **A1 — Quantum oracle synthesizer.** Write a classical predicate `f(x)`; kraM compiles it to
  a verified, garbage-free Toffoli oracle and exports to OpenQASM/Qiskit. `local`/`delocal` is
  exactly the uncompute-ancilla discipline quantum oracles require. *fit ★★★★★ / lift high / wow ★★★★★*
  - (Oracle = the reversible "recognizer" circuit a quantum algorithm queries — flips a marker
    bit when `f(x)` is true. Writing them by hand is tedious and easy to leave garbage in.)
- **A2 — Grover end-to-end.** A1 plus actually run Grover's search on the synthesized oracle
  (simulate), showing amplitude amplification. *fit ★★★★ / lift high / wow ★★★★★*
- **A3 — Landauer energy analyzer.** Count irreversible bit-erasures in any program → show
  `kT ln2` heat dissipated; reversible version ≈ 0. Directly serves the adiabatic-computing
  goal. *fit ★★★★★ / lift low / wow ★★★★*
- **A4 — Animated circuit diagram.** Render `:gates` output as a real quantum-circuit diagram
  (wires, X/⊕/Toffoli dots), step-highlight per gate. *fit ★★★★ / lift med / wow ★★★★*

### B. Crypto / codecs — bijection = one procedure runs both ways

- **B1 — Feistel block cipher.** `encrypt = call enc`, `decrypt = uncall enc` — one procedure,
  no separate decryptor. Compiles to gates. *fit ★★★★★ / lift low / wow ★★★★*
- **B2 — Bijective codec / transcoder.** Lossless format transform with a *proven* round-trip;
  bijection means data loss is mathematically impossible. *fit ★★★★ / lift med / wow ★★★*
- **B3 — Reversible entropy coder.** rANS / range coder — encode/decode are structural
  inverses. *fit ★★★ / lift high / wow ★★★*

### C. Dev tooling / UX — leans on time-travel

- **C1 — kraM Studio (web playground).** Go→WASM. Live editor + scrubbable time-travel timeline
  + circuit pane + verify badge. The showcase shell everything else can live in.
  *fit ★★★★ / lift high / wow ★★★★★*
- **C2 — Omniscient debugger.** Run to crash, scrub backward through every mutation, inspect any
  past state without re-running. *fit ★★★★★ / lift med / wow ★★★★*
- **C3 — Reversibility fuzzer.** Property tester: auto-assert `uncall(call(x)) == x` over random
  inputs for any proc. Cheap, genuinely useful QA. *fit ★★★★ / lift low / wow ★★★*

### D. Simulation / physics — leans on exact reversibility (no float drift)

- **D1 — Reversible cellular automata.** Margolus / billiard-ball CA — universal reversible
  computing, gorgeous, runs backward exactly. Deep theory + very visual.
  *fit ★★★★★ / lift med / wow ★★★★★*
- **D2 — Bit-exact rewindable sim.** Fixed-point n-body / lattice that runs backward to the
  exact start state (floats can't; kraM can). *fit ★★★★ / lift med / wow ★★★*

### E. Bigger swings

- **E1 — Time-rewind puzzle game.** Braid-like mechanic where rewind is *real* execution
  reversal; levels authored in kraM. *fit ★★★ / lift high / wow ★★★★★*
- **E2 — Reversible notebook.** Jupyter-style literate env; every cell undoable, inline circuit
  + timeline per cell. *fit ★★★ / lift high / wow ★★★★*
- **E3 — Perfect-undo datastore.** Event store where the log *is* the inverse — infinite exact
  undo, no snapshots. *fit ★★★ / lift med / wow ★★★*

---

## The two axes to choose on

**Fun to me** (builder enjoyment, curiosity, no external audience needed) vs **Impresses
others** (portfolio, "state of the art", needs a viewer to land).

```
                 IMPRESSES OTHERS  ↑ (needs an audience to land)
                                   │
        A2 Grover        A1 Oracle │  C1 Studio       E1 Rewind game
        A4 Circuit diag            │  D1 Reversible CA
                                   │
   ────────────────────────────────┼──────────────────────────────→  FUN TO ME
        A3 Landauer                │  D1 Reversible CA   (self-driven,
        B2 Codec   E3 Datastore    │  C2 Debugger         no audience
        B3 Entropy coder           │  B1 Feistel cipher   needed)
                                   │  C3 Fuzzer  D2 Rewindable sim
                                   ↓
```

Rough placement (a given idea can shift with how it's framed):

- **High impress, needs audience:** A1, A2, A4 (quantum-literate viewers), C1, E1.
- **High fun, self-contained:** C2, C3, B1, D2, D1 (D1 straddles — impresses *and* fun).
- **Both (the sweet spot):** **D1 reversible CA**, **C1 studio**. D1 is visually striking for
  low conceptual overhead and needs no quantum knowledge; C1 is the shell that makes any other
  idea shareable.
- **Quiet-useful, low glamour:** A3, B2, B3, C3, E3 — genuinely useful, less of a "wow".

### Reading the axes

- Want to **enjoy building** and not depend on a niche audience → **D1 (reversible CA)** or
  **C2 (debugger)** or **B1 (Feistel cipher)**. All fun, all self-contained, none need quantum
  background.
- Want to **impress** (portfolio / "state of the art") → **A1 (oracle synthesizer)** or the
  **C1 studio** as a shell around it. Highest ceiling, highest lift, needs a viewer who values
  quantum tooling.
- Want **both with least pain** → **D1 inside C1**: reversible CA rendered live in a web studio
  with the time-travel scrubber. Visual, deep, shareable, no quantum prerequisite.

---

## Open decision

Pick one of:
1. A single idea (e.g. D1).
2. A fusion (e.g. A1 + C1, or D1 + C1).
3. An axis to optimize ("maximize fun", "maximize impress", "both") and let the recommendation
   above pick.

Then this doc's chosen path gets promoted into a real implementation plan.

---

## CHOSEN: D1 + C1 — reversible CA inside kraM Studio

**Build order: C1 first (the shell), then D1 (the content).** The studio makes
every kraM feature visible and shareable; the reversible CA is the flagship demo
that lives inside it.

### Architecture

Single Go package compiled two ways:
- Native (`!js` build tag on `main.go`) → the existing `kram`/`krapl` CLI.
- WASM (`wasm.go`, `//go:build js`) → a browser bridge. Replaces the stdin
  `main()` with one that installs JS-callable functions over the same `Interp`
  and blocks. Everything crosses the boundary as JSON strings (one shape, in
  `marshal`/`valueJSON`).

Studio front end = one static page (`studio/index.html`), plain JS, no framework,
no CDN. `make studio` builds `kram.wasm` + copies `wasm_exec.js`; `make serve`
hosts it (WASM needs http, not `file://`). Build artifacts gitignored.

JS bridge API (all return JSON): `kramEval(src)`, `kramUndo()`, `kramRedo()`,
`kramGoto(n)` (timeline scrub engine — steps undo/redo to reach n applied ops),
`kramReset()`, `kramState()`, `kramGates/Circuit/Verify/Invert(src)`.

### Phases

- **Phase 1 — pipe + shell. ✅ DONE, verified in browser.** WASM bridge, static
  page with editor + Run/Undo/Redo/Reset, live state inspector, output pane,
  **time-travel timeline scrubber** (drag to rewind state to any past mutation),
  circuit/gates/verify/invert buttons, examples menu. Confirmed: fib runs
  (a=55,b=89), scrubbing to step 13 rewinds state to a=2,b=3,i=3 and retracts
  output, circuit pane renders success + error.
- **Phase 2 — reversible CA on a flat grid. ✅ DONE, verified in browser.**
  Chose the flat-1D-grid path (below). `ca.kr` is a Margolus block-rotation CA:
  each generation rotates every 2×2 block at partition offset `o` (bounds `w-o`/
  `h-o`, no wrap, no modulo), then `o ^= 1`. Block counters are `local`/`delocal`
  (Janus scoped temporaries) — fresh each pass, no destructive-overwrite warnings,
  and `reverse{gen}` is verified to be its exact inverse (also confirms `invert`
  handles nested reversible loops + local/delocal). Studio renders a flat numeric
  array `g` as a `w`-wide grid; two examples added (seed grid, step generation).
  Verified: seed + 5 generations = 558 mutations mixes the grid; scrubbing the
  timeline back to step 4 un-mixes it to the exact seed.
- **Phase 3 — animation + generation-level stepping.** Add a play/pause loop that
  auto-advances generations (one `kramEval(gen)` per tick) and a coarse
  "step generation" control, so the mix→un-mix is watchable without 558 fine
  scrub steps. Optionally group history by generation on the timeline.
- **Phase 4 — polish.** Circuit-diagram rendering (A4), syntax highlight, share,
  richer CA rules (Critters / billiard-ball) once conditionals-on-blocks are wanted.

### Language findings (from building the CA)

Probed and confirmed in the interpreter:
- ✅ Computed-index element swap in a reversible loop: `g[i] <=> g[i+1]`.
- ✅ `var*var + var` index expressions: `g[by*w + bx]`.
- ✅ Nested reversible loops, with the `counter -= n` reset-on-exit pattern.
- ✅ `local`/`delocal` counters inside loops; `reverse{}` inverts the whole thing.
- ❌ No modulo `%` and no integer division/floor — so grid math avoids both
  (bounds `w-o`, non-wrapping partitions).
- ❌ Procedures reject array-element arguments (`call rot(g[0], g[1])` →
  `parse error: expected a variable name`). Procs are by-name only, so the block
  rule is inline element swaps, not a `proc`.
- ❌ Nested/2D index on a reversible-op LHS (`g[0][0] <=> g[1][1]`) doesn't parse.
  Sidestepped by the flat-1D-grid representation — the lazier path, and it lowers
  to circuits more readily. (Left as a possible future language feature.)

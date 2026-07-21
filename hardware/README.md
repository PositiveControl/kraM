# Running kraM circuits on real quantum hardware

kraM's `:grover ... qasm` export is plain OpenQASM 2.0 over `x`/`cx`/`ccx`/`h` —
exactly the gate set IBM's transpiler eats for breakfast. This directory holds
the submission script.

## Setup (once)

```sh
pip install qiskit qiskit-ibm-runtime matplotlib
```

Create a free account at https://quantum.cloud.ibm.com, create an instance,
then copy your API key and the instance CRN (the long
`crn:v1:bluemix:public:quantum-computing:...` string on the instance page):

```sh
python -c "from qiskit_ibm_runtime import QiskitRuntimeService; \
           QiskitRuntimeService.save_account(channel='ibm_quantum_platform', \
                                             token='YOUR_API_KEY', instance='YOUR_CRN')"
```

The token is stored in `~/.qiskit/` — never commit it.

## Run

```sh
# in the REPL (or the Studio "⬇ QASM" button):
#   :grover 3 x == 5 iters=1 qasm     → save as grover.qasm
python run_ibm.py --qasm grover.qasm --shots 4096
```

`grover.qasm` in this directory is exactly that export, kept as a sample.

## What to expect

The simulator shows the ideal, noiseless evolution. Real hardware is noisy, and
every Toffoli transpiles into a deep chain of native two-qubit gates — errors
compound fast. Set expectations accordingly:

- **Keep it small.** 3 bits, a single equality, and force `iters=1` — the
  default export uses the optimal count (2 at 3 bits), which doubles the depth
  for little hardware gain. One iteration is ~6 qubits and depth ~180 after
  transpilation.
- **Success looks like a clear peak, not certainty.** Ideal P(x=5) after one
  iteration at 3 bits is ~78%. On hardware, expect the marked state to *dominate
  the histogram* — visibly taller than the 7 losers — rather than hit the ideal
  number. (Measured on `ibm_marrakesh`, 4096 shots: x=5 at 53.9%, next best 9.3%.)
- **Bigger oracles decay quickly.** Range conditions (`x >= 3 && x <= 5`)
  compile to hundreds of gates; after transpilation the signal mostly drowns.
  Run those in the Studio simulator instead.

The point of the exercise isn't precision — it's that a homemade reversible
language compiles, verifiably garbage-free, to gates a physical quantum
computer executes.

## What actually happens, step by step

Tracing `grover.qasm` from text file to histogram:

### 1. Local: parse

`qasm2.load()` turns the text into a Qiskit circuit object: 6 qubits, 3
classical bits, depth 16, only `h`/`x`/`cx`/`ccx` gates. Nothing quantum yet —
pure data structure.

### 2. Local: transpile (depth 16 → ~178)

The chip can't run a Toffoli. IBM's Heron-class backends natively execute only
`rz`, `sx`, `x`, and `cz`. Transpiling does three things:

- **Decompose**: each `ccx` becomes ~6 two-qubit gates plus single-qubit
  rotations; each `h`/`cx` gets rewritten in the native basis.
- **Route**: physical qubits sit on a heavy-hex lattice where each qubit
  touches only 2–3 neighbors. The circuit wants interactions between
  non-adjacent qubits, so SWAPs get inserted to shuttle states around.
- **Layout**: of the chip's ~156 qubits, pick 6 with the best current
  calibration (lowest error, longest coherence).

That's why depth explodes 16 → ~178, and why circuits must stay small: every
layer of depth is another chance for noise.

### 3. Submit and queue

`SamplerV2.run()` serializes the transpiled circuit to IBM's runtime, billed
against your instance. The job waits in queue, then gets scheduled.

### 4. On the machine: gates become microwave pulses

The qubits are superconducting transmons at ~15 millikelvin in a dilution
refrigerator. Each native gate is a pre-calibrated microwave pulse at the
qubit's frequency (~4–5 GHz): an `sx` is a specific pulse shape and duration,
a `cz` is a tuned interaction between two neighboring transmons. The
178-layer circuit compiles into a pulse schedule a few microseconds long.

### 5. One shot, physically

Each of the 4096 shots is the same sequence:

1. **Reset** all 6 qubits to |0⟩.
2. **H layer** on q0–q2: the 3-bit register enters equal superposition — all
   8 values of x simultaneously, amplitude 1/√8 each.
3. **Marker prep**: q3 goes to the |−⟩ state, so writing into it kicks a
   *phase* back instead of a bit (phase kickback).
4. **Oracle**: the Toffoli chain reversibly computes `x == 5` into the marker.
   Only the |101⟩ branch flips it — that branch's amplitude gets its sign
   flipped. Then the same Toffolis run in reverse, returning ancillas q4–q5
   to |0⟩. This uncompute step is exactly kraM's garbage-free reversibility
   requirement — leftover garbage in ancillas would entangle with x and
   destroy the interference the next step needs.
5. **Diffusion**: inverts every amplitude about the mean. Seven positive
   amplitudes and one negative one → the negative one overshoots. After 1
   iteration, |101⟩'s amplitude goes from 0.35 to ~0.88, i.e. P(x=5) ≈ 78%.
6. **Measure** q0–q2: readout resonators coupled to each qubit are probed
   with a microwave tone; the reflected signal's phase differs depending on
   qubit state (dispersive readout). Superposition collapses to one definite
   3-bit string, recorded into c[0..2].

### 6. Repeat 4096×, histogram

Each shot yields one number 0–7. Ideal distribution: 78% fives, ~3% each for
the rest. Measured on `ibm_marrakesh`: 53.9% fives.

### 7. Where the missing 24% went

- **Gate error**: ~0.1–0.3% per two-qubit gate, compounded over ~178 layers.
- **Decoherence**: qubits hold state for hundreds of microseconds (T1/T2);
  the circuit takes a few microseconds — small ratio, but it leaks.
- **Readout error**: ~1% chance per qubit of reading the wrong bit.

Noise pushes the distribution toward uniform (12.5% each). Landing at 54%
against that floor means the interference — superposition, phase flip,
uncompute, diffusion — genuinely happened on physical hardware before noise
smeared it.

The kraM-specific point is step 5.4: the language guarantees the oracle
uncomputes its ancillas, and that guarantee is what makes the amplitude
arithmetic in 5.5 valid at all.

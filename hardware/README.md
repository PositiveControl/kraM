# Running kraM circuits on real quantum hardware

kraM's `:grover ... qasm` export is plain OpenQASM 2.0 over `x`/`cx`/`ccx`/`h` —
exactly the gate set IBM's transpiler eats for breakfast. This directory holds
the submission script.

## Setup (once)

```sh
pip install qiskit qiskit-ibm-runtime matplotlib
```

Create a free account at https://quantum.cloud.ibm.com, copy your API token, then:

```sh
python -c "from qiskit_ibm_runtime import QiskitRuntimeService; \
           QiskitRuntimeService.save_account(channel='ibm_quantum_platform', token='YOUR_TOKEN')"
```

The token is stored in `~/.qiskit/` — never commit it.

## Run

```sh
# in the REPL (or the Studio "⬇ QASM" button):
#   :grover 3 x == 5 qasm     → save as grover.qasm
python run_ibm.py --qasm grover.qasm --shots 4096
```

## What to expect

The simulator shows the ideal, noiseless evolution. Real hardware is noisy, and
every Toffoli transpiles into a deep chain of native two-qubit gates — errors
compound fast. Set expectations accordingly:

- **Keep it small.** 3 bits, a single equality, 1 Grover iteration (the default
  export). That's ~6 qubits and a manageable depth.
- **Success looks like a clear peak, not certainty.** Ideal P(x=5) after one
  iteration at 3 bits is ~78%. On hardware, expect the marked state to *dominate
  the histogram* — visibly taller than the 7 losers — rather than hit the ideal
  number.
- **Bigger oracles decay quickly.** Range conditions (`x >= 3 && x <= 5`)
  compile to hundreds of gates; after transpilation the signal mostly drowns.
  Run those in the Studio simulator instead.

The point of the exercise isn't precision — it's that a homemade reversible
language compiles, verifiably garbage-free, to gates a physical quantum
computer executes.

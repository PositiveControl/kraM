#!/usr/bin/env python3
"""Run a kraM-exported Grover circuit on real IBM Quantum hardware.

Usage:
    pip install qiskit qiskit-ibm-runtime matplotlib
    python -c "from qiskit_ibm_runtime import QiskitRuntimeService; \
               QiskitRuntimeService.save_account(channel='ibm_quantum_platform', token='YOUR_TOKEN')"
    python run_ibm.py --qasm grover.qasm [--backend ibm_brisbane] [--shots 4096]

Export the circuit first:  :grover 3 x == 5 qasm  (REPL) or the Studio's
"⬇ QASM" button. Keep it small — 3 bits, one equality, 1 iteration — so the
transpiled Toffoli chains stay shallow enough for today's noise.
"""

import argparse
from collections import Counter

from qiskit import qasm2, transpile
from qiskit_ibm_runtime import QiskitRuntimeService, SamplerV2


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--qasm", required=True, help="path to the exported .qasm file")
    ap.add_argument("--backend", default=None, help="IBM backend name (default: least busy)")
    ap.add_argument("--shots", type=int, default=4096)
    args = ap.parse_args()

    circuit = qasm2.load(args.qasm)
    print(f"circuit: {circuit.num_qubits} qubits, depth {circuit.depth()}")

    service = QiskitRuntimeService()
    if args.backend:
        backend = service.backend(args.backend)
    else:
        backend = service.least_busy(operational=True, simulator=False,
                                     min_num_qubits=circuit.num_qubits)
    print(f"backend: {backend.name}")

    isa = transpile(circuit, backend=backend, optimization_level=3)
    print(f"transpiled depth: {isa.depth()}")

    job = SamplerV2(mode=backend).run([isa], shots=args.shots)
    print(f"job {job.job_id()} submitted, waiting…")
    counts = job.result()[0].data.c.get_counts()

    # kraM measures little-endian into c[i]; Qiskit prints the creg MSB-first,
    # so int(bitstring, 2) is the value of x directly.
    by_x = Counter({int(bits, 2): n for bits, n in counts.items()})
    total = sum(by_x.values())
    print(f"\nresults ({total} shots):")
    for x, n in by_x.most_common():
        bar = "█" * round(40 * n / total)
        print(f"  x={x:<4d} {n:>6d}  {n/total:6.1%}  {bar}")


if __name__ == "__main__":
    main()

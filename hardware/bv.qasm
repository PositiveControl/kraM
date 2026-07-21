OPENQASM 2.0;
include "qelib1.inc";
// Bernstein-Vazirani: recover s=37 (100101) in one oracle query
// input x: q[0..5] little-endian; marker q[6]
// c[i] = bit i of s — int(bitstring, 2) in Qiskit yields s
qreg q[7];
creg c[6];
h q[0];
h q[1];
h q[2];
h q[3];
h q[4];
h q[5];
x q[6];
h q[6];
cx q[0],q[6];
cx q[2],q[6];
cx q[5],q[6];
h q[0];
h q[1];
h q[2];
h q[3];
h q[4];
h q[5];
measure q[0] -> c[0];
measure q[1] -> c[1];
measure q[2] -> c[2];
measure q[3] -> c[3];
measure q[4] -> c[4];
measure q[5] -> c[5];



OPENQASM 2.0;
include "qelib1.inc";
// Grover search: x == 5  (3-bit x, 1 iterations)
// input x: q[0..2] little-endian; marker q[3]; rest ancilla
// c[i] = bit i of x — int(bitstring, 2) in Qiskit yields x
qreg q[6];
creg c[3];
h q[0];
h q[1];
h q[2];
x q[3];
h q[3];
x q[1];
ccx q[0],q[1],q[4];
ccx q[2],q[4],q[5];
cx q[5],q[3];
ccx q[2],q[4],q[5];
ccx q[0],q[1],q[4];
x q[1];
h q[0];
h q[1];
h q[2];
x q[0];
x q[1];
x q[2];
h q[2];
ccx q[0],q[1],q[5];
cx q[5],q[2];
ccx q[0],q[1],q[5];
h q[2];
x q[0];
x q[1];
x q[2];
h q[0];
h q[1];
h q[2];
measure q[0] -> c[0];
measure q[1] -> c[1];
measure q[2] -> c[2];



OPENQASM 2.0;
include "qelib1.inc";
// Simon: hidden XOR period s=19 (10011); f(x) = x XOR (bit 0 of x ? s : 0)
// input x: q[0..4] little-endian; output f(x): q[5..9]
// every measured y satisfies y.s = 0 (mod 2); ~8 runs + GF(2) elimination recover s
qreg q[10];
creg c[5];
h q[0];
h q[1];
h q[2];
h q[3];
h q[4];
cx q[0],q[5];
cx q[1],q[6];
cx q[2],q[7];
cx q[3],q[8];
cx q[4],q[9];
cx q[0],q[5];
cx q[0],q[6];
cx q[0],q[9];
h q[0];
h q[1];
h q[2];
h q[3];
h q[4];
measure q[0] -> c[0];
measure q[1] -> c[1];
measure q[2] -> c[2];
measure q[3] -> c[3];
measure q[4] -> c[4];



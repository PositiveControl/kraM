# kraMLang reversible Fibonacci.
# The loop (a, b) -> (b, a+b) computes Fibonacci numbers with only reversible
# updates. Run it backward (reverse / uncall) and the inputs come back exactly —
# no information is lost.

a = 0
b = 1
i = 0
n = 10

proc step { a += b; a <=> b; i += 1 }

from i == 0 { } loop { call step } until i == n
print "fib(" + n + ") pair: " + a + ", " + b

reverse { from i == 0 { } loop { call step } until i == n }
print "reversed back to: a=" + a + ", b=" + b + ", i=" + i

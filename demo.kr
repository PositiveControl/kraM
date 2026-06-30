# kraMLang reversible cipher — one procedure, run forward to encrypt and
# backward (uncall) to decrypt. XOR loses no information, so no separate
# decrypt code is needed.

key = 9999
msg = 1337
proc cipher { msg ^= key }

print "message:   " + msg

call cipher
print "encrypted: " + msg

uncall cipher
print "decrypted: " + msg

print "encrypt and decrypt are the same procedure, run in opposite directions."

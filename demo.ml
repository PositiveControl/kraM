# mlang reversible cipher — encrypt and decrypt with the SAME operation.
# XOR is its own inverse, so no separate "decrypt" code is needed, and no
# information is ever lost.

key = 9999
msg = 1337
print "message:   " + msg

# encrypt
msg ^= key
print "encrypted: " + msg

# decrypt — literally the same line again
msg ^= key
print "decrypted: " + msg

print "recovered the original with the identical operation."

package main

import "fmt"

// invert returns the structural inverse of a statement: the program that
// undoes it when run forward, derived purely from the program text (no undo
// log). It succeeds only on the reversible subset — and the error it returns
// on anything else names exactly why that construct can't be reversed, which
// makes invert() a checker for "is this program reversible?".
//
// Expressions inside statements (deltas, conditions, assertions) are pure reads
// and are reused unchanged; only the statements that mutate state are inverted.
func invert(n Node) (Node, error) {
	switch v := n.(type) {
	case Block:
		// Inverse of a sequence is the inverses in reverse order.
		out := make([]Node, len(v.Stmts))
		for i, s := range v.Stmts {
			inv, err := invert(s)
			if err != nil {
				return nil, err
			}
			out[len(v.Stmts)-1-i] = inv
		}
		return Block{Stmts: out}, nil

	case CompoundAssign:
		op := MINUS
		if v.Op == MINUS {
			op = PLUS
		}
		return CompoundAssign{Name: v.Name, Op: op, Value: v.Value}, nil

	case Swap:
		return v, nil // self-inverse

	case Assert:
		return v, nil // self-inverse: the check is the same backward

	case If:
		// Reversible iff it carries an exit assertion. Inverting swaps the
		// entry condition and the exit assertion (so the exit now selects the
		// branch on the way back) and inverts both branch bodies.
		if v.Exit == nil {
			return nil, fmt.Errorf("if is not reversible: add an 'assert' exit condition")
		}
		then, err := invert(v.Then)
		if err != nil {
			return nil, err
		}
		var els Node
		if v.Else != nil {
			if els, err = invert(v.Else); err != nil {
				return nil, err
			}
		}
		return If{Cond: v.Exit, Then: then, Else: els, Exit: v.Cond}, nil

	case ReversibleLoop:
		// Swap entry and exit conditions, invert both bodies.
		do, err := invert(v.Do)
		if err != nil {
			return nil, err
		}
		rest, err := invert(v.Rest)
		if err != nil {
			return nil, err
		}
		return ReversibleLoop{Entry: v.Exit, Do: do, Rest: rest, Exit: v.Entry}, nil

	case Assign:
		return nil, fmt.Errorf("cannot reverse destructive assignment of %q; use += / -= / <=>", v.Name)
	case Print:
		return nil, fmt.Errorf("cannot reverse print (irreversible output)")
	case While:
		return nil, fmt.Errorf("classic while is not reversible; use from/loop/until")
	}
	return nil, fmt.Errorf("cannot reverse %T", n)
}

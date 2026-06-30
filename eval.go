package main

import "fmt"

// Eval walks the AST and returns a Value. Types are checked here at runtime —
// that runtime check IS what "dynamic typing" means. All state lives in the
// Interp, and every mutation it performs is reversible.
func Eval(n Node, ip *Interp) (Value, error) {
	switch v := n.(type) {
	case NumberLit:
		return numVal(v.Val), nil
	case BoolLit:
		return boolVal(v.Val), nil
	case Var:
		val, ok := ip.get(v.Name)
		if !ok {
			return Value{}, fmt.Errorf("undefined variable %q", v.Name)
		}
		return val, nil
	case Assign:
		val, err := Eval(v.Value, ip)
		if err != nil {
			return Value{}, err
		}
		ip.set(v.Name, val) // records the inverse for time travel
		return val, nil
	case Print:
		val, err := Eval(v.Value, ip)
		if err != nil {
			return Value{}, err
		}
		ip.print(val) // output is reversible state, not a raw side effect
		return val, nil
	case Unary:
		r, err := Eval(v.Right, ip)
		if err != nil {
			return Value{}, err
		}
		if r.Kind != NumKind {
			return Value{}, fmt.Errorf("cannot negate %s", r.typeName())
		}
		return numVal(-r.Num), nil
	case Binary:
		return evalBinary(v, ip)
	}
	return Value{}, fmt.Errorf("cannot evaluate %T", n)
}

func evalBinary(b Binary, ip *Interp) (Value, error) {
	l, err := Eval(b.Left, ip)
	if err != nil {
		return Value{}, err
	}
	r, err := Eval(b.Right, ip)
	if err != nil {
		return Value{}, err
	}

	// == and != work across any kinds (different kinds are simply unequal).
	switch b.Op {
	case EQ:
		return boolVal(valEqual(l, r)), nil
	case NE:
		return boolVal(!valEqual(l, r)), nil
	}

	// Remaining operators are numeric. Reject non-numbers.
	if l.Kind != NumKind || r.Kind != NumKind {
		return Value{}, fmt.Errorf("operator %s needs numbers, got %s and %s",
			opSym(b.Op), l.typeName(), r.typeName())
	}
	switch b.Op {
	case PLUS:
		return numVal(l.Num + r.Num), nil
	case MINUS:
		return numVal(l.Num - r.Num), nil
	case STAR:
		return numVal(l.Num * r.Num), nil
	case SLASH:
		if r.Num == 0 {
			return Value{}, fmt.Errorf("division by zero")
		}
		return numVal(l.Num / r.Num), nil
	case LT:
		return boolVal(l.Num < r.Num), nil
	case GT:
		return boolVal(l.Num > r.Num), nil
	case LE:
		return boolVal(l.Num <= r.Num), nil
	case GE:
		return boolVal(l.Num >= r.Num), nil
	}
	return Value{}, fmt.Errorf("unknown operator %s", opSym(b.Op))
}

// valEqual: equal only if same kind and same payload.
func valEqual(a, b Value) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case NumKind:
		return a.Num == b.Num
	case BoolKind:
		return a.Bool == b.Bool
	}
	return false
}

func opSym(k TokKind) string {
	for sym, kind := range map[string]TokKind{
		"+": PLUS, "-": MINUS, "*": STAR, "/": SLASH,
		"<": LT, ">": GT, "<=": LE, ">=": GE, "==": EQ, "!=": NE,
	} {
		if kind == k {
			return sym
		}
	}
	return kindName(k)
}

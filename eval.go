package main

import (
	"fmt"
	"math"
)

// asInt requires a number with no fractional part, exactly representable as an
// integer (float64 is exact for integers up to 2^53). Used by bitwise ^=.
// ponytail: 2^53 ceiling — add a real int64 type if exact bits beyond that matter.
func asInt(v Value, what string) (int64, error) {
	if v.Kind != NumKind {
		return 0, fmt.Errorf("%s must be an integer, got %s", what, v.typeName())
	}
	if v.Num != math.Trunc(v.Num) {
		return 0, fmt.Errorf("%s must be a whole number, got %g", what, v.Num)
	}
	if math.Abs(v.Num) > 1<<53 {
		return 0, fmt.Errorf("%s exceeds exact integer range (2^53)", what)
	}
	return int64(v.Num), nil
}

// Eval walks the AST and returns a Value. Types are checked here at runtime —
// that runtime check IS what "dynamic typing" means. All state lives in the
// Interp, and every mutation it performs is reversible.
func Eval(n Node, ip *Interp) (Value, error) {
	switch v := n.(type) {
	case NumberLit:
		return numVal(v.Val), nil
	case BoolLit:
		return boolVal(v.Val), nil
	case StrLit:
		return strVal(v.Val), nil
	case Var:
		if v.Name == "_" { // bare '_' is the last result (REPL convenience)
			return ip.last, nil
		}
		val, ok := ip.get(v.Name)
		if !ok {
			return Value{}, fmt.Errorf("undefined variable %q", v.Name)
		}
		return val, nil
	case Assign:
		if v.Name == "_" {
			return Value{}, fmt.Errorf("'_' is the last-result reference and cannot be assigned")
		}
		// First binding is reversible (undo just unsets it). Overwriting an
		// existing value destroys information — the irreversible act — so warn
		// and nudge toward the reversible updates (+= / -= / <=>).
		if old, exists := ip.get(v.Name); exists {
			msg := fmt.Sprintf("destructive overwrite of %q (was %s) — irreversible; use += / -= / <=> to stay reversible", v.Name, old)
			if ip.strict {
				return Value{}, fmt.Errorf("strict mode: %s", msg)
			}
			ip.warn(msg)
		}
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
		return nilVal(), nil
	case CompoundAssign:
		cur, ok := ip.get(v.Name)
		if !ok {
			return Value{}, fmt.Errorf("cannot update undefined variable %q", v.Name)
		}
		if cur.Kind != NumKind {
			return Value{}, fmt.Errorf("reversible update needs a number, %q is %s", v.Name, cur.typeName())
		}
		rhs, err := Eval(v.Value, ip)
		if err != nil {
			return Value{}, err
		}
		if rhs.Kind != NumKind {
			return Value{}, fmt.Errorf("reversible update needs a number, got %s", rhs.typeName())
		}
		delta := rhs.Num
		if v.Op == MINUS {
			delta = -delta
		}
		ip.incr(v.Name, delta)
		return numVal(cur.Num + delta), nil
	case XorAssign:
		cur, ok := ip.get(v.Name)
		if !ok {
			return Value{}, fmt.Errorf("cannot update undefined variable %q", v.Name)
		}
		lhs, err := asInt(cur, fmt.Sprintf("variable %q", v.Name))
		if err != nil {
			return Value{}, err
		}
		rhs, err := Eval(v.Value, ip)
		if err != nil {
			return Value{}, err
		}
		mask, err := asInt(rhs, "^= operand")
		if err != nil {
			return Value{}, err
		}
		ip.xor(v.Name, mask)
		return numVal(float64(lhs ^ mask)), nil
	case Swap:
		if _, ok := ip.get(v.A); !ok {
			return Value{}, fmt.Errorf("cannot swap undefined variable %q", v.A)
		}
		if _, ok := ip.get(v.B); !ok {
			return Value{}, fmt.Errorf("cannot swap undefined variable %q", v.B)
		}
		ip.swap(v.A, v.B)
		return nilVal(), nil
	case Block:
		return evalBlock(v, ip)
	case If:
		return evalIf(v, ip)
	case Assert:
		ok, err := evalCond(v.Cond, ip, "assert")
		if err != nil {
			return Value{}, err
		}
		if !ok {
			return Value{}, fmt.Errorf("assertion failed")
		}
		return nilVal(), nil
	case Reverse:
		inv, err := invert(v.Body)
		if err != nil {
			return Value{}, err
		}
		return Eval(inv, ip)
	case ProcDef:
		ip.procs[v.Name] = v // a definition, not state — not logged
		return nilVal(), nil
	case Call:
		body, err := procBody(ip, v.Name, v.Args)
		if err != nil {
			return Value{}, err
		}
		return Eval(body, ip)
	case Uncall:
		body, err := procBody(ip, v.Name, v.Args)
		if err != nil {
			return Value{}, err
		}
		inv, err := invert(body)
		if err != nil {
			return Value{}, fmt.Errorf("cannot uncall %q: %w", v.Name, err)
		}
		return Eval(inv, ip)
	case ReversibleLoop:
		return evalReversibleLoop(v, ip)
	case While:
		return evalWhile(v, ip)
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

// evalIf runs an if, enforcing the optional reversible exit assertion, and
// emits a top-level control-flow note.
func evalIf(v If, ip *Interp) (Value, error) {
	ip.cfDepth++
	defer func() { ip.cfDepth-- }()

	taken, err := evalCond(v.Cond, ip, "if condition")
	if err != nil {
		return Value{}, err
	}
	var out Value
	if taken {
		out, err = Eval(v.Then, ip)
	} else if v.Else != nil {
		out, err = Eval(v.Else, ip)
	}
	if err != nil {
		return Value{}, err
	}
	// Reversible if: the exit assertion must equal which branch ran, so
	// backward execution can recover the branch without a log.
	if v.Exit != nil {
		exit, err := evalCond(v.Exit, ip, "if exit assertion")
		if err != nil {
			return Value{}, err
		}
		if exit != taken {
			return Value{}, fmt.Errorf("if exit assertion violated: %s-branch ran but exit is %v",
				branchName(taken), exit)
		}
	}
	if ip.cfDepth == 1 { // top-level statement
		ip.note(fmt.Sprintf("if → %s branch", branchName(taken)))
	}
	return out, nil
}

// evalWhile runs the classic loop and notes its iteration count.
func evalWhile(v While, ip *Interp) (Value, error) {
	ip.cfDepth++
	defer func() { ip.cfDepth-- }()

	// ponytail: hard iteration cap so a runaway loop can't fill the undo
	// history unbounded. Raise it / make it configurable if real programs hit it.
	const maxIter = 1_000_000
	count := 0
	for {
		cond, err := evalCond(v.Cond, ip, "while condition")
		if err != nil {
			return Value{}, err
		}
		if !cond {
			break
		}
		if count >= maxIter {
			return Value{}, fmt.Errorf("while exceeded %d iterations", maxIter)
		}
		if _, err := Eval(v.Body, ip); err != nil {
			return Value{}, err
		}
		count++
	}
	if ip.cfDepth == 1 {
		ip.note(fmt.Sprintf("while: %d iteration(s)", count))
	}
	return nilVal(), nil
}

// evalReversibleLoop runs `from Entry { Do } loop { Rest } until Exit`. Entry
// must hold on first entry and must fail on every re-entry; the loop ends when
// Exit holds. Those assertions are what let the loop run backward without a
// log — the inverse just swaps Entry and Exit.
func evalReversibleLoop(v ReversibleLoop, ip *Interp) (Value, error) {
	ip.cfDepth++
	defer func() { ip.cfDepth-- }()

	const maxIter = 1_000_000
	entry, err := evalCond(v.Entry, ip, "loop entry assertion")
	if err != nil {
		return Value{}, err
	}
	if !entry {
		return Value{}, fmt.Errorf("loop entry assertion failed")
	}
	if _, err := Eval(v.Do, ip); err != nil {
		return Value{}, err
	}
	count := 1 // the initial Do counts as one pass
	for {
		exit, err := evalCond(v.Exit, ip, "loop exit assertion")
		if err != nil {
			return Value{}, err
		}
		if exit {
			break
		}
		if count >= maxIter {
			return Value{}, fmt.Errorf("reversible loop exceeded %d iterations", maxIter)
		}
		if _, err := Eval(v.Rest, ip); err != nil {
			return Value{}, err
		}
		reentry, err := evalCond(v.Entry, ip, "loop re-entry assertion")
		if err != nil {
			return Value{}, err
		}
		if reentry {
			return Value{}, fmt.Errorf("loop re-entry assertion violated: entry condition held again")
		}
		if _, err := Eval(v.Do, ip); err != nil {
			return Value{}, err
		}
		count++
	}
	if ip.cfDepth == 1 {
		ip.note(fmt.Sprintf("loop: %d iteration(s)", count))
	}
	return nilVal(), nil
}

// evalCond evaluates a node that must be a bool, naming the context on error.
func evalCond(n Node, ip *Interp, ctx string) (bool, error) {
	v, err := Eval(n, ip)
	if err != nil {
		return false, err
	}
	if v.Kind != BoolKind {
		return false, fmt.Errorf("%s must be bool, got %s", ctx, v.typeName())
	}
	return v.Bool, nil
}

func branchName(taken bool) string {
	if taken {
		return "then"
	}
	return "else"
}

// procBody looks up a procedure and returns its body with parameters bound
// by-reference to the call's argument variables.
func procBody(ip *Interp, name string, args []string) (Node, error) {
	return bindProcBody(ip.procs, name, args)
}

// evalBlock runs statements in order and yields the last value (nil if empty).
func evalBlock(b Block, ip *Interp) (Value, error) {
	last := nilVal()
	for _, s := range b.Stmts {
		v, err := Eval(s, ip)
		if err != nil {
			return Value{}, err
		}
		last = v
	}
	return last, nil
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

	// + is overloaded: numeric add, or concat if either side is a string
	// (the non-string side is coerced to its display form).
	if b.Op == PLUS {
		switch {
		case l.Kind == NumKind && r.Kind == NumKind:
			return numVal(l.Num + r.Num), nil
		case l.Kind == StrKind || r.Kind == StrKind:
			return strVal(l.Raw() + r.Raw()), nil
		default:
			return Value{}, fmt.Errorf("operator + needs numbers or a string, got %s and %s",
				l.typeName(), r.typeName())
		}
	}

	// Remaining operators are numeric. Reject non-numbers.
	if l.Kind != NumKind || r.Kind != NumKind {
		return Value{}, fmt.Errorf("operator %s needs numbers, got %s and %s",
			opSym(b.Op), l.typeName(), r.typeName())
	}
	switch b.Op {
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
	case StrKind:
		return a.Str == b.Str
	case NilKind:
		return true // nil == nil
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

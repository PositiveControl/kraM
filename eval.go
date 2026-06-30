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
	case Local:
		if _, exists := ip.get(v.Name); exists {
			return Value{}, fmt.Errorf("local %q already exists — local must introduce a fresh name", v.Name)
		}
		val, err := Eval(v.Value, ip)
		if err != nil {
			return Value{}, err
		}
		ip.set(v.Name, val)
		return nilVal(), nil
	case Delocal:
		cur, exists := ip.get(v.Name)
		if !exists {
			return Value{}, fmt.Errorf("delocal %q: variable does not exist", v.Name)
		}
		want, err := Eval(v.Value, ip)
		if err != nil {
			return Value{}, err
		}
		if !valEqual(cur, want) {
			return Value{}, fmt.Errorf("delocal %q: value is %s, expected %s", v.Name, cur, want)
		}
		ip.unset(v.Name)
		return nilVal(), nil
	case Swap:
		return evalSwap(v, ip)
	case ArrayLit:
		elems := make([]Value, len(v.Elems))
		for i, e := range v.Elems {
			val, err := Eval(e, ip)
			if err != nil {
				return Value{}, err
			}
			elems[i] = val
		}
		return arrVal(elems), nil
	case Index:
		arr, idx, err := evalIndex(v.Arr, v.Idx, ip)
		if err != nil {
			return Value{}, err
		}
		return arr.Arr[idx], nil
	case IdxAssign:
		arr, idx, err := evalIndexVar(ip, v.Name, v.Idx)
		if err != nil {
			return Value{}, err
		}
		val, err := Eval(v.Value, ip)
		if err != nil {
			return Value{}, err
		}
		msg := fmt.Sprintf("destructive overwrite of %s[%d] (was %s) — use += / -= / ^= / <=> to stay reversible", v.Name, idx, arr.Arr[idx])
		if ip.strict {
			return Value{}, fmt.Errorf("strict mode: %s", msg)
		}
		ip.warn(msg)
		ip.set(v.Name, withElem(arr, idx, val))
		return val, nil
	case IdxUpdate:
		arr, idx, err := evalIndexVar(ip, v.Name, v.Idx)
		if err != nil {
			return Value{}, err
		}
		cur := arr.Arr[idx]
		newElem, err := applyUpdate(cur, v.Op, v.Value, ip)
		if err != nil {
			return Value{}, err
		}
		ip.set(v.Name, withElem(arr, idx, newElem))
		return newElem, nil
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
		if v.Op == NOT {
			if r.Kind != BoolKind {
				return Value{}, fmt.Errorf("'!' needs a bool, got %s", r.typeName())
			}
			return boolVal(!r.Bool), nil
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

// arrIndex evaluates an index expression to a valid array offset.
func arrIndex(idxNode Node, ip *Interp, length int) (int, error) {
	iv, err := Eval(idxNode, ip)
	if err != nil {
		return 0, err
	}
	n, err := asInt(iv, "array index")
	if err != nil {
		return 0, err
	}
	if n < 0 || int(n) >= length {
		return 0, fmt.Errorf("array index %d out of range [0, %d)", n, length)
	}
	return int(n), nil
}

// evalIndex evaluates an array expression and an index, returning the array
// value and the bounds-checked offset.
func evalIndex(arrNode, idxNode Node, ip *Interp) (Value, int, error) {
	arr, err := Eval(arrNode, ip)
	if err != nil {
		return Value{}, 0, err
	}
	if arr.Kind != ArrKind {
		return Value{}, 0, fmt.Errorf("cannot index %s", arr.typeName())
	}
	idx, err := arrIndex(idxNode, ip, len(arr.Arr))
	return arr, idx, err
}

// evalIndexVar resolves an array variable and an index for an in-place update.
func evalIndexVar(ip *Interp, name string, idxNode Node) (Value, int, error) {
	arr, ok := ip.get(name)
	if !ok {
		return Value{}, 0, fmt.Errorf("undefined variable %q", name)
	}
	if arr.Kind != ArrKind {
		return Value{}, 0, fmt.Errorf("%q is %s, not an array", name, arr.typeName())
	}
	idx, err := arrIndex(idxNode, ip, len(arr.Arr))
	return arr, idx, err
}

// withElem returns a copy of arr with element i replaced — a fresh slice, so
// the undo log's stored "before" array is never mutated.
func withElem(arr Value, i int, v Value) Value {
	cp := make([]Value, len(arr.Arr))
	copy(cp, arr.Arr)
	cp[i] = v
	return arrVal(cp)
}

// applyUpdate computes the new element value for `cur <op> rhs`.
func applyUpdate(cur Value, op TokKind, rhsNode Node, ip *Interp) (Value, error) {
	if cur.Kind != NumKind {
		return Value{}, fmt.Errorf("reversible update needs a number, element is %s", cur.typeName())
	}
	rhs, err := Eval(rhsNode, ip)
	if err != nil {
		return Value{}, err
	}
	switch op {
	case PLUSEQ:
		if rhs.Kind != NumKind {
			return Value{}, fmt.Errorf("+= needs a number, got %s", rhs.typeName())
		}
		return numVal(cur.Num + rhs.Num), nil
	case MINUSEQ:
		if rhs.Kind != NumKind {
			return Value{}, fmt.Errorf("-= needs a number, got %s", rhs.typeName())
		}
		return numVal(cur.Num - rhs.Num), nil
	case CARETEQ:
		l, err := asInt(cur, "^= target")
		if err != nil {
			return Value{}, err
		}
		r, err := asInt(rhs, "^= operand")
		if err != nil {
			return Value{}, err
		}
		return numVal(float64(l ^ r)), nil
	}
	return Value{}, fmt.Errorf("unknown update operator")
}

// evalSwap exchanges two lvalues (variables or array elements). Plain var-var
// swaps use the dedicated reversible swap op; anything indexed reads both
// locations and writes them back crossed.
func evalSwap(v Swap, ip *Interp) (Value, error) {
	if v.AI == nil && v.BI == nil {
		if _, ok := ip.get(v.A); !ok {
			return Value{}, fmt.Errorf("cannot swap undefined variable %q", v.A)
		}
		if _, ok := ip.get(v.B); !ok {
			return Value{}, fmt.Errorf("cannot swap undefined variable %q", v.B)
		}
		ip.swap(v.A, v.B)
		return nilVal(), nil
	}
	va, err := readLoc(ip, v.A, v.AI)
	if err != nil {
		return Value{}, err
	}
	vb, err := readLoc(ip, v.B, v.BI)
	if err != nil {
		return Value{}, err
	}
	if err := writeLoc(ip, v.A, v.AI, vb); err != nil {
		return Value{}, err
	}
	if err := writeLoc(ip, v.B, v.BI, va); err != nil {
		return Value{}, err
	}
	return nilVal(), nil
}

func readLoc(ip *Interp, name string, idx Node) (Value, error) {
	if idx == nil {
		val, ok := ip.get(name)
		if !ok {
			return Value{}, fmt.Errorf("undefined variable %q", name)
		}
		return val, nil
	}
	arr, i, err := evalIndexVar(ip, name, idx)
	if err != nil {
		return Value{}, err
	}
	return arr.Arr[i], nil
}

func writeLoc(ip *Interp, name string, idx Node, val Value) error {
	if idx == nil {
		ip.set(name, val)
		return nil
	}
	arr, i, err := evalIndexVar(ip, name, idx)
	if err != nil {
		return err
	}
	ip.set(name, withElem(arr, i, val))
	return nil
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
	// && and || short-circuit, so a guard like `i < len && a[i] > 0` is safe.
	if b.Op == AND || b.Op == OR {
		left, err := evalCond(b.Left, ip, "'&&'/'||' operand")
		if err != nil {
			return Value{}, err
		}
		if b.Op == AND && !left {
			return boolVal(false), nil
		}
		if b.Op == OR && left {
			return boolVal(true), nil
		}
		right, err := evalCond(b.Right, ip, "'&&'/'||' operand")
		if err != nil {
			return Value{}, err
		}
		return boolVal(right), nil
	}

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
		"&&": AND, "||": OR,
	} {
		if kind == k {
			return sym
		}
	}
	return kindName(k)
}

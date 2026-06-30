package main

import "fmt"

func errArity(name string, want, got int) error {
	return fmt.Errorf("procedure %q takes %d argument(s), got %d", name, want, got)
}
func errAlias(name, arg string) error {
	return fmt.Errorf("procedure %q called with aliased argument %q (arguments must be distinct)", name, arg)
}

// substitute returns a copy of n with every variable name remapped through m
// (names not in m are unchanged). This implements by-reference parameter
// binding: a procedure body is rewritten so its parameters refer to the
// caller's argument variables before it is evaluated, inverted, or compiled.
func substitute(n Node, m map[string]string) Node {
	r := func(name string) string {
		if to, ok := m[name]; ok {
			return to
		}
		return name
	}
	switch v := n.(type) {
	case NumberLit, StrLit, BoolLit:
		return v
	case Var:
		return Var{Name: r(v.Name)}
	case Unary:
		return Unary{Op: v.Op, Right: substitute(v.Right, m)}
	case Binary:
		return Binary{Op: v.Op, Left: substitute(v.Left, m), Right: substitute(v.Right, m)}
	case Assign:
		return Assign{Name: r(v.Name), Value: substitute(v.Value, m)}
	case CompoundAssign:
		return CompoundAssign{Name: r(v.Name), Op: v.Op, Value: substitute(v.Value, m)}
	case XorAssign:
		return XorAssign{Name: r(v.Name), Value: substitute(v.Value, m)}
	case Swap:
		return Swap{A: r(v.A), AI: substNil(v.AI, m), B: r(v.B), BI: substNil(v.BI, m)}
	case ArrayLit:
		elems := make([]Node, len(v.Elems))
		for i, e := range v.Elems {
			elems[i] = substitute(e, m)
		}
		return ArrayLit{Elems: elems}
	case Index:
		return Index{Arr: substitute(v.Arr, m), Idx: substitute(v.Idx, m)}
	case IdxAssign:
		return IdxAssign{Name: r(v.Name), Idx: substitute(v.Idx, m), Value: substitute(v.Value, m)}
	case IdxUpdate:
		return IdxUpdate{Name: r(v.Name), Idx: substitute(v.Idx, m), Op: v.Op, Value: substitute(v.Value, m)}
	case Print:
		return Print{Value: substitute(v.Value, m)}
	case Assert:
		return Assert{Cond: substitute(v.Cond, m)}
	case Block:
		stmts := make([]Node, len(v.Stmts))
		for i, s := range v.Stmts {
			stmts[i] = substitute(s, m)
		}
		return Block{Stmts: stmts}
	case If:
		out := If{Cond: substitute(v.Cond, m), Then: substitute(v.Then, m)}
		if v.Else != nil {
			out.Else = substitute(v.Else, m)
		}
		if v.Exit != nil {
			out.Exit = substitute(v.Exit, m)
		}
		return out
	case While:
		return While{Cond: substitute(v.Cond, m), Body: substitute(v.Body, m)}
	case ReversibleLoop:
		return ReversibleLoop{
			Entry: substitute(v.Entry, m), Do: substitute(v.Do, m),
			Rest: substitute(v.Rest, m), Exit: substitute(v.Exit, m),
		}
	case Reverse:
		return Reverse{Body: substitute(v.Body, m)}
	case Call:
		return Call{Name: v.Name, Args: rename(v.Args, r)}
	case Uncall:
		return Uncall{Name: v.Name, Args: rename(v.Args, r)}
	}
	return n
}

// substNil substitutes an optional node (used for swap index operands).
func substNil(n Node, m map[string]string) Node {
	if n == nil {
		return nil
	}
	return substitute(n, m)
}

func rename(names []string, r func(string) string) []string {
	if names == nil {
		return nil
	}
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = r(n)
	}
	return out
}

// bindProcBody looks up a procedure and returns its body with parameters bound
// by-reference to the call arguments.
func bindProcBody(procs map[string]ProcDef, name string, args []string) (Node, error) {
	def, ok := procs[name]
	if !ok {
		return nil, fmt.Errorf("undefined procedure %q", name)
	}
	m, err := bindArgs(name, def.Params, args)
	if err != nil {
		return nil, err
	}
	return substitute(def.Body, m), nil
}

// bindArgs builds the parameter->argument name map for a call, validating arity
// and rejecting aliased (duplicate) arguments.
func bindArgs(name string, params, args []string) (map[string]string, error) {
	if len(params) != len(args) {
		return nil, errArity(name, len(params), len(args))
	}
	seen := map[string]bool{}
	m := map[string]string{}
	for i, p := range params {
		if seen[args[i]] {
			return nil, errAlias(name, args[i])
		}
		seen[args[i]] = true
		m[p] = args[i]
	}
	return m, nil
}

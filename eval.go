package main

import "fmt"

// Env holds variable bindings. One scope for now.
type Env map[string]float64

// Eval walks the AST and returns a float64. Dynamic typing: one value kind for now.
func Eval(n Node, env Env) (float64, error) {
	switch v := n.(type) {
	case NumberLit:
		return v.Val, nil
	case Var:
		val, ok := env[v.Name]
		if !ok {
			return 0, fmt.Errorf("undefined variable %q", v.Name)
		}
		return val, nil
	case Assign:
		val, err := Eval(v.Value, env)
		if err != nil {
			return 0, err
		}
		env[v.Name] = val
		return val, nil
	case Unary:
		r, err := Eval(v.Right, env)
		if err != nil {
			return 0, err
		}
		return -r, nil // only MINUS exists as a prefix today
	case Binary:
		l, err := Eval(v.Left, env)
		if err != nil {
			return 0, err
		}
		r, err := Eval(v.Right, env)
		if err != nil {
			return 0, err
		}
		switch v.Op {
		case PLUS:
			return l + r, nil
		case MINUS:
			return l - r, nil
		case STAR:
			return l * r, nil
		case SLASH:
			if r == 0 {
				return 0, fmt.Errorf("division by zero")
			}
			return l / r, nil
		}
	}
	return 0, fmt.Errorf("cannot evaluate %T", n)
}

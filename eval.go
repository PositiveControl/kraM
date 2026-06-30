package main

import "fmt"

// Eval walks the AST and returns a float64. Dynamic typing: one value kind for now.
func Eval(n Node) (float64, error) {
	switch v := n.(type) {
	case NumberLit:
		return v.Val, nil
	case Unary:
		r, err := Eval(v.Right)
		if err != nil {
			return 0, err
		}
		return -r, nil // only MINUS exists as a prefix today
	case Binary:
		l, err := Eval(v.Left)
		if err != nil {
			return 0, err
		}
		r, err := Eval(v.Right)
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

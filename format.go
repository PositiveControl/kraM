package main

import (
	"strconv"
	"strings"
)

// format renders an AST node back to kraM source. It is a display tool (for
// :invert, errors, traces), not a strict serializer — expressions are printed
// flat, relying on precedence rather than wrapping everything in parens.
func format(n Node) string {
	switch v := n.(type) {
	case NumberLit:
		return strconv.FormatFloat(v.Val, 'g', -1, 64)
	case StrLit:
		return strconv.Quote(v.Val)
	case BoolLit:
		return strconv.FormatBool(v.Val)
	case Var:
		return v.Name
	case Unary:
		return "-" + format(v.Right)
	case Binary:
		return format(v.Left) + " " + opSym(v.Op) + " " + format(v.Right)

	case Assign:
		return v.Name + " = " + format(v.Value)
	case CompoundAssign:
		op := "+="
		if v.Op == MINUS {
			op = "-="
		}
		return v.Name + " " + op + " " + format(v.Value)
	case XorAssign:
		return v.Name + " ^= " + format(v.Value)
	case Swap:
		return v.A + " <=> " + v.B
	case Print:
		return "print " + format(v.Value)
	case Assert:
		return "assert " + format(v.Cond)

	case Block:
		parts := make([]string, len(v.Stmts))
		for i, s := range v.Stmts {
			parts[i] = format(s)
		}
		return "{ " + strings.Join(parts, "; ") + " }"

	case If:
		s := "if " + format(v.Cond) + " " + format(v.Then)
		if v.Else != nil {
			s += " else " + format(v.Else)
		}
		if v.Exit != nil {
			s += " assert " + format(v.Exit)
		}
		return s
	case While:
		return "while " + format(v.Cond) + " " + format(v.Body)
	case ReversibleLoop:
		return "from " + format(v.Entry) + " " + format(v.Do) +
			" loop " + format(v.Rest) + " until " + format(v.Exit)
	case Reverse:
		return "reverse " + format(v.Body)
	case ProcDef:
		return "proc " + v.Name + nameList(v.Params) + " " + format(v.Body)
	case Call:
		return "call " + v.Name + nameList(v.Args)
	case Uncall:
		return "uncall " + v.Name + nameList(v.Args)
	}
	return "<?>"
}

// nameList renders "(a, b, c)", or "" when empty.
func nameList(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return "(" + strings.Join(names, ", ") + ")"
}

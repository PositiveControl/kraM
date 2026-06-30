package main

import "strconv"

// ValueKind tags a Value's type. Dynamic typing: the tag is checked at runtime.
type ValueKind int

const (
	NilKind ValueKind = iota // zero value of Value is nil — handy
	NumKind
	BoolKind
)

// Value is mlang's universal value. Tagged union — add a field + kind to grow
// (e.g. Str string for the next value type). Cheap to copy, no heap games.
type Value struct {
	Kind ValueKind
	Num  float64
	Bool bool
}

func numVal(f float64) Value { return Value{Kind: NumKind, Num: f} }
func boolVal(b bool) Value   { return Value{Kind: BoolKind, Bool: b} }
func nilVal() Value          { return Value{Kind: NilKind} }

func (v Value) typeName() string {
	switch v.Kind {
	case NilKind:
		return "nil"
	case NumKind:
		return "number"
	case BoolKind:
		return "bool"
	}
	return "unknown"
}

func (v Value) String() string {
	switch v.Kind {
	case NilKind:
		return "nil"
	case NumKind:
		return strconv.FormatFloat(v.Num, 'g', -1, 64)
	case BoolKind:
		return strconv.FormatBool(v.Bool)
	}
	return "<?>"
}

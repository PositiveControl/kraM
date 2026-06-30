package main

import "strconv"

// ValueKind tags a Value's type. Dynamic typing: the tag is checked at runtime.
type ValueKind int

const (
	NilKind ValueKind = iota // zero value of Value is nil — handy
	NumKind
	BoolKind
	StrKind
)

// Value is mlang's universal value. Tagged union — add a field + kind to grow.
// Cheap to copy, no heap games.
type Value struct {
	Kind ValueKind
	Num  float64
	Bool bool
	Str  string
}

func numVal(f float64) Value { return Value{Kind: NumKind, Num: f} }
func boolVal(b bool) Value   { return Value{Kind: BoolKind, Bool: b} }
func strVal(s string) Value  { return Value{Kind: StrKind, Str: s} }
func nilVal() Value          { return Value{Kind: NilKind} }

func (v Value) typeName() string {
	switch v.Kind {
	case NilKind:
		return "nil"
	case NumKind:
		return "number"
	case BoolKind:
		return "bool"
	case StrKind:
		return "string"
	}
	return "unknown"
}

// String is the debugging/echo form — strings are quoted so the type is
// unambiguous at the REPL and in :env / :history.
func (v Value) String() string {
	switch v.Kind {
	case NilKind:
		return "nil"
	case NumKind:
		return strconv.FormatFloat(v.Num, 'g', -1, 64)
	case BoolKind:
		return strconv.FormatBool(v.Bool)
	case StrKind:
		return strconv.Quote(v.Str)
	}
	return "<?>"
}

// Raw is the user-facing form used by print — a string prints without quotes.
func (v Value) Raw() string {
	if v.Kind == StrKind {
		return v.Str
	}
	return v.String()
}

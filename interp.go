package main

import (
	"fmt"
	"sort"
	"strings"
)

// binding is a variable's value plus whether it exists at all. The `exists`
// flag lets undo distinguish "restore old value" from "remove again" (a
// variable's first assignment had no prior value to restore).
type binding struct {
	val    Value
	exists bool
}

// reversible is one undoable operation. Every mutation in the language
// implements this: redo walks state forward, undo walks it back, label
// describes it for :history. Add a new mutation kind → implement reversible
// and route it through Interp.do(), and time travel works for free.
type reversible interface {
	redo(*Interp)
	undo(*Interp)
	label() string
}

// varEdit captures a single variable's before/after. The symmetry (undo
// applies before, redo applies after) is the whole trick.
type varEdit struct {
	name          string
	before, after binding
}

func (e varEdit) redo(ip *Interp) { ip.applyBinding(e.name, e.after) }
func (e varEdit) undo(ip *Interp) { ip.applyBinding(e.name, e.before) }
func (e varEdit) label() string {
	if !e.before.exists {
		return fmt.Sprintf("%s = %s  (was unset)", e.name, e.after.val)
	}
	return fmt.Sprintf("%s = %s  (was %s)", e.name, e.after.val, e.before.val)
}

// incrEdit is a *structurally* reversible mutation: x += delta. Unlike varEdit
// it stores no prior value — the inverse is computed by negating the delta,
// the way a reversible adder gate inverts without recording its input. This is
// the adiabatic-friendly mutation class; destructive '=' (varEdit) is not.
// ponytail: float add/sub isn't bit-exact, so undo isn't perfectly lossless;
// switch to integers/fixed-point when a real reversible backend needs exactness.
type incrEdit struct {
	name  string
	delta float64
}

func (e incrEdit) redo(ip *Interp) { b := ip.vars[e.name]; b.val.Num += e.delta; ip.vars[e.name] = b }
func (e incrEdit) undo(ip *Interp) { b := ip.vars[e.name]; b.val.Num -= e.delta; ip.vars[e.name] = b }
func (e incrEdit) label() string {
	if e.delta < 0 {
		return fmt.Sprintf("%s -= %g", e.name, -e.delta)
	}
	return fmt.Sprintf("%s += %g", e.name, e.delta)
}

// swapEdit exchanges two variables. It is self-inverse — redo and undo are the
// same operation — like a Fredkin/controlled-swap gate. Stores only the names.
type swapEdit struct{ a, b string }

func (e swapEdit) redo(ip *Interp) { ip.vars[e.a], ip.vars[e.b] = ip.vars[e.b], ip.vars[e.a] }
func (e swapEdit) undo(ip *Interp) { e.redo(ip) }
func (e swapEdit) label() string   { return fmt.Sprintf("%s <=> %s", e.a, e.b) }

// printEdit makes output a reversible thing: redo appends to the buffer, undo
// pops it. The physical terminal is append-only, but this buffer is the model
// of truth — :output renders it at the current point in time.
type printEdit struct{ val Value }

func (e printEdit) redo(ip *Interp) { ip.output = append(ip.output, e.val) }
func (e printEdit) undo(ip *Interp) { ip.output = ip.output[:len(ip.output)-1] }
func (e printEdit) label() string   { return fmt.Sprintf("print %s", e.val) }

// Interp holds all mutable program state plus the time-travel machinery.
// Reversibility lives HERE and only here: every mutation goes through do().
type Interp struct {
	vars   map[string]binding
	output []Value      // reversible output buffer; terminal is just a view
	past   []reversible // applied ops, newest last — the undo stack
	fut    []reversible // undone ops, newest last — the redo stack

	// Stepping machinery (see stepper.go). When stepping, do() pauses before
	// each mutation until the controller grants the next step.
	stepping       bool
	gateIn         chan string   // eval -> controller: label of the op about to run
	gateOut        chan struct{} // controller -> eval: permission to run it
	stepDone       chan error    // eval -> controller: program finished
	stepPending    string        // next op the parked evaluator will run
	stepHasPending bool          // false once the program has drained
	stepErr        error         // terminal error, if any

	warnings []string // advisory messages from the last evaluation (not state)
}

func NewInterp() *Interp {
	return &Interp{vars: map[string]binding{}}
}

// do applies an operation forward and records it. A fresh mutation invalidates
// the redo stack — you cannot redo into a future you have diverged from.
func (ip *Interp) do(r reversible) {
	ip.gate(r.label()) // when stepping, pause here until the next :step
	r.redo(ip)
	ip.past = append(ip.past, r)
	ip.fut = nil
}

func (ip *Interp) get(name string) (Value, bool) {
	b, ok := ip.vars[name]
	if !ok || !b.exists {
		return Value{}, false
	}
	return b.val, true
}

func (ip *Interp) set(name string, val Value) {
	before := ip.vars[name] // zero binding => {exists:false}, correct for new vars
	ip.do(varEdit{name: name, before: before, after: binding{val: val, exists: true}})
}

func (ip *Interp) print(val Value) {
	ip.do(printEdit{val: val})
}

// warn records an advisory message (e.g. a destructive overwrite). Warnings
// are not program state — they are not reversible and not part of history; the
// controller drains and prints them after each evaluation.
func (ip *Interp) warn(msg string) { ip.warnings = append(ip.warnings, msg) }

// DrainWarnings returns and clears pending advisories.
func (ip *Interp) DrainWarnings() []string {
	w := ip.warnings
	ip.warnings = nil
	return w
}

// incr applies a reversible `x += delta`. The caller guarantees name exists
// and holds a number.
func (ip *Interp) incr(name string, delta float64) {
	ip.do(incrEdit{name: name, delta: delta})
}

// swap exchanges two existing variables reversibly.
func (ip *Interp) swap(a, b string) {
	ip.do(swapEdit{a: a, b: b})
}

// Undo reverses the most recent operation. Returns false if there is no history.
func (ip *Interp) Undo() (reversible, bool) {
	if len(ip.past) == 0 {
		return nil, false
	}
	r := ip.past[len(ip.past)-1]
	ip.past = ip.past[:len(ip.past)-1]
	r.undo(ip)
	ip.fut = append(ip.fut, r)
	return r, true
}

// Redo re-applies the most recently undone operation.
func (ip *Interp) Redo() (reversible, bool) {
	if len(ip.fut) == 0 {
		return nil, false
	}
	r := ip.fut[len(ip.fut)-1]
	ip.fut = ip.fut[:len(ip.fut)-1]
	r.redo(ip)
	ip.past = append(ip.past, r)
	return r, true
}

// applyBinding forces a variable to a binding without logging (undo/redo move
// ops between stacks themselves; they must not record new history).
func (ip *Interp) applyBinding(name string, b binding) {
	if b.exists {
		ip.vars[name] = b
	} else {
		delete(ip.vars, name)
	}
}

// HistoryString renders the timeline, oldest first, marking the cursor.
func (ip *Interp) HistoryString() string {
	if len(ip.past) == 0 && len(ip.fut) == 0 {
		return "(no history)"
	}
	var b strings.Builder
	for i, r := range ip.past {
		fmt.Fprintf(&b, "%2d  %s\n", i+1, r.label())
	}
	b.WriteString("    --- now ---\n")
	for i := len(ip.fut) - 1; i >= 0; i-- { // fut newest-last; show next redo first
		fmt.Fprintf(&b, "    %s  (undone)\n", ip.fut[i].label())
	}
	return strings.TrimRight(b.String(), "\n")
}

// EnvString lists current bindings, sorted for stable output.
func (ip *Interp) EnvString() string {
	names := make([]string, 0, len(ip.vars))
	for n, b := range ip.vars {
		if b.exists {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return "(empty)"
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		fmt.Fprintf(&b, "%s = %s\n", n, ip.vars[n].val)
	}
	return strings.TrimRight(b.String(), "\n")
}

// OutputString renders the output buffer as it stands now — the time-traveled
// truth, regardless of what physically scrolled past in the terminal.
func (ip *Interp) OutputString() string {
	if len(ip.output) == 0 {
		return "(no output)"
	}
	var b strings.Builder
	for _, v := range ip.output {
		fmt.Fprintln(&b, v.Raw())
	}
	return strings.TrimRight(b.String(), "\n")
}

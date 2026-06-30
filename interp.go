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

// edit is one reversible mutation: how a single variable looked before and
// after. Undo applies `before`; redo applies `after`. This symmetry is the
// whole trick — every state change knows how to walk itself in either
// direction.
type edit struct {
	name   string
	before binding
	after  binding
}

// Interp holds all mutable program state plus the time-travel machinery.
// Reversibility lives HERE and only here: every mutation goes through set(),
// which logs its inverse. Add a new mutation kind later → route it through
// set() and it is reversible for free.
type Interp struct {
	vars map[string]binding
	past []edit // applied edits, newest last — the undo stack
	fut  []edit // undone edits, newest last — the redo stack
}

func NewInterp() *Interp {
	return &Interp{vars: map[string]binding{}}
}

func (ip *Interp) get(name string) (Value, bool) {
	b, ok := ip.vars[name]
	if !ok || !b.exists {
		return Value{}, false
	}
	return b.val, true
}

// set mutates a variable and records the inverse so it can be undone. A fresh
// mutation invalidates the redo stack — you cannot redo into a future you have
// diverged from.
func (ip *Interp) set(name string, val Value) {
	before := ip.vars[name] // zero binding => {exists:false}, correct for new vars
	after := binding{val: val, exists: true}
	ip.vars[name] = after
	ip.past = append(ip.past, edit{name: name, before: before, after: after})
	ip.fut = nil
}

// Undo reverses the most recent mutation. Returns false if there is no history.
func (ip *Interp) Undo() (edit, bool) {
	if len(ip.past) == 0 {
		return edit{}, false
	}
	e := ip.past[len(ip.past)-1]
	ip.past = ip.past[:len(ip.past)-1]
	ip.apply(e.name, e.before)
	ip.fut = append(ip.fut, e)
	return e, true
}

// Redo re-applies the most recently undone mutation.
func (ip *Interp) Redo() (edit, bool) {
	if len(ip.fut) == 0 {
		return edit{}, false
	}
	e := ip.fut[len(ip.fut)-1]
	ip.fut = ip.fut[:len(ip.fut)-1]
	ip.apply(e.name, e.after)
	ip.past = append(ip.past, e)
	return e, true
}

// apply forces a variable to a given binding without logging (undo/redo move
// edits between stacks themselves; they must not record new history).
func (ip *Interp) apply(name string, b binding) {
	if b.exists {
		ip.vars[name] = b
	} else {
		delete(ip.vars, name)
	}
}

// HistoryString renders the undo timeline, oldest first, marking the cursor
// (everything above the line is undoable, everything below is redoable).
func (ip *Interp) HistoryString() string {
	if len(ip.past) == 0 && len(ip.fut) == 0 {
		return "(no history)"
	}
	var b strings.Builder
	for i, e := range ip.past {
		fmt.Fprintf(&b, "%2d  %s\n", i+1, describe(e))
	}
	b.WriteString("    --- now ---\n")
	// fut is newest-last; show the next redo first
	for i := len(ip.fut) - 1; i >= 0; i-- {
		fmt.Fprintf(&b, "    %s  (undone)\n", describe(ip.fut[i]))
	}
	return strings.TrimRight(b.String(), "\n")
}

func describe(e edit) string {
	if !e.before.exists {
		return fmt.Sprintf("%s = %s  (was unset)", e.name, e.after.val)
	}
	return fmt.Sprintf("%s = %s  (was %s)", e.name, e.after.val, e.before.val)
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

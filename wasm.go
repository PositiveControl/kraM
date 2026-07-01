//go:build js

package main

import (
	"encoding/json"
	"fmt"
	"syscall/js"
)

// The browser bridge for kraM Studio. Builds only under GOOS=js: it replaces
// the CLI main() (see main.go's !js tag) with one that installs a handful of
// JS-callable functions and then blocks forever, keeping the Go runtime and its
// single Interp alive for the page to drive.
//
// Every exported function takes/returns JSON strings — the simplest bridge that
// works, and it keeps the value/history shapes in one place (marshalJSON) rather
// than fiddling with js.ValueOf per field.

var studio = NewInterp()

func main() {
	reg := func(name string, fn func(args []js.Value) any) {
		js.Global().Set(name, js.FuncOf(func(_ js.Value, args []js.Value) any { return fn(args) }))
	}
	reg("kramEval", func(a []js.Value) any { return kramEval(a[0].String()) })
	reg("kramUndo", func([]js.Value) any { return kramUndo() })
	reg("kramRedo", func([]js.Value) any { return kramRedo() })
	reg("kramGoto", func(a []js.Value) any { return kramGoto(a[0].Int()) })
	reg("kramReset", func([]js.Value) any { studio.Reset(); return kramState() })
	reg("kramState", func([]js.Value) any { return kramState() })
	reg("kramCircuit", func(a []js.Value) any { return kramCompile(a[0].String(), "circuit") })
	reg("kramGates", func(a []js.Value) any { return kramCompile(a[0].String(), "gates") })
	reg("kramInvert", func(a []js.Value) any { return kramCompile(a[0].String(), "invert") })
	reg("kramVerify", func(a []js.Value) any { return kramCompile(a[0].String(), "verify") })
	select {} // keep the instance alive for callbacks
}

func marshal(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `{"error":"marshal failed"}`
	}
	return string(b)
}

// valueJSON turns a kraM Value into a plain JS-native value: numbers/bools/
// strings pass through, arrays nest (so a 2D grid arrives as nested arrays,
// ready for the CA renderer). Kind is dropped — the display string carries type.
func valueJSON(v Value) any {
	switch v.Kind {
	case NumKind:
		return v.Num
	case BoolKind:
		return v.Bool
	case StrKind:
		return v.Str
	case ArrKind:
		out := make([]any, len(v.Arr))
		for i, e := range v.Arr {
			out[i] = valueJSON(e)
		}
		return out
	}
	return nil
}

// kramState is the whole snapshot the UI renders: variables (native value +
// display repr), the output buffer, and the timeline cursor. `cursor` is how
// many ops are applied; `total` is cursor + undone, so a slider spans [0,total].
func kramState() string {
	vars := map[string]any{}
	for name, b := range studio.vars {
		if b.exists {
			vars[name] = map[string]any{"val": valueJSON(b.val), "repr": b.val.String()}
		}
	}
	out := make([]string, len(studio.output))
	for i, v := range studio.output {
		out[i] = v.Raw()
	}
	hist := make([]string, 0, len(studio.past)+len(studio.fut))
	for _, r := range studio.past {
		hist = append(hist, r.label())
	}
	cursor := len(studio.past)
	for i := len(studio.fut) - 1; i >= 0; i-- { // fut newest-last; timeline order
		hist = append(hist, studio.fut[i].label())
	}
	return marshal(map[string]any{
		"vars": vars, "output": out, "history": hist,
		"cursor": cursor, "total": cursor + len(studio.fut),
	})
}

func kramEval(src string) string {
	ast, err := Parse(src)
	if err != nil {
		return marshal(map[string]any{"ok": false, "error": "parse error: " + err.Error()})
	}
	cp := studio.checkpoint()
	val, err := Eval(ast, studio)
	if err != nil {
		studio.rollback(cp) // atomic: a failed program leaves no partial mutations
		return marshal(map[string]any{"ok": false, "error": err.Error(),
			"warnings": studio.DrainWarnings(), "state": kramStateRaw()})
	}
	res := ""
	if val.Kind != NilKind {
		studio.last = val
		res = val.String()
	}
	return marshal(map[string]any{"ok": true, "result": res,
		"warnings": studio.DrainWarnings(), "notes": studio.DrainNotes(),
		"state": kramStateRaw()})
}

// kramStateRaw returns the snapshot as a value (not a JSON string) so it can be
// nested inside another response without double-encoding.
func kramStateRaw() any {
	var s any
	json.Unmarshal([]byte(kramState()), &s)
	return s
}

func kramUndo() string {
	if r, ok := studio.Undo(); ok {
		return marshal(map[string]any{"ok": true, "label": r.label(), "state": kramStateRaw()})
	}
	return marshal(map[string]any{"ok": false, "state": kramStateRaw()})
}

func kramRedo() string {
	if r, ok := studio.Redo(); ok {
		return marshal(map[string]any{"ok": true, "label": r.label(), "state": kramStateRaw()})
	}
	return marshal(map[string]any{"ok": false, "state": kramStateRaw()})
}

// kramGoto scrubs the timeline to exactly n applied ops by stepping undo/redo —
// the time-travel slider's engine. Cheap: each step is one map write.
func kramGoto(n int) string {
	for len(studio.past) > n {
		if _, ok := studio.Undo(); !ok {
			break
		}
	}
	for len(studio.past) < n {
		if _, ok := studio.Redo(); !ok {
			break
		}
	}
	return kramState()
}

func kramCompile(src, mode string) string {
	ast, err := Parse(src)
	if err != nil {
		return marshal(map[string]any{"ok": false, "error": "parse error: " + err.Error()})
	}
	// Compile against a fresh interpreter, not the live studio state: a program
	// now carries its own `=` initialisation, so it lowers self-contained. This
	// makes the compile buttons one-click regardless of what has been run/undone.
	env := NewInterp()
	var text string
	switch mode {
	case "circuit":
		gates, e := lower(ast)
		if e != nil {
			return marshal(map[string]any{"ok": false, "error": e.Error()})
		}
		for i, g := range gates {
			text += fmt.Sprintf("%d  %s\n", i+1, g)
		}
	case "gates":
		bc, e := compileBits(ast, env)
		if e != nil {
			return marshal(map[string]any{"ok": false, "error": e.Error()})
		}
		for i, g := range bc.gates {
			text += fmt.Sprintf("%d  %s\n", i+1, g)
		}
	case "invert":
		inv, e := invert(ast)
		if e != nil {
			return marshal(map[string]any{"ok": false, "error": e.Error()})
		}
		text = format(inv)
	case "verify":
		rep, e := verify(ast, env)
		if e != nil {
			return marshal(map[string]any{"ok": false, "error": e.Error()})
		}
		text = rep
	}
	return marshal(map[string]any{"ok": true, "text": text})
}

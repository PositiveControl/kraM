package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func main() {
	in := bufio.NewScanner(os.Stdin)
	ip := NewInterp()
	shown := 0 // output buffer entries already rendered to the terminal
	fmt.Println("mlang — reversible REPL. :help for commands, Ctrl-D to exit.")
	for {
		fmt.Print("> ")
		if !in.Scan() {
			fmt.Println()
			return
		}
		line := strings.TrimSpace(in.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, ":") {
			runMeta(line, ip)
			reconcileOutput(ip, &shown) // undo/redo may grow or shrink output
			continue
		}
		ast, err := Parse(line)
		if err != nil {
			fmt.Println("parse error:", err)
			continue
		}
		val, err := Eval(ast, ip)
		if err != nil {
			fmt.Println("error:", err)
			continue
		}
		reconcileOutput(ip, &shown)
		if val.Kind != NilKind {
			fmt.Println(val) // echo real results; print/empty-if produce nil and stay quiet
		}
	}
}

// reconcileOutput renders the gap between the output buffer and what the
// terminal has shown. Growth = new prints (emit them). Shrink = prints undone
// (the terminal can't erase, so announce the retraction). The buffer is truth;
// the terminal is its append-only transcript.
func reconcileOutput(ip *Interp, shown *int) {
	switch {
	case len(ip.output) > *shown:
		for _, v := range ip.output[*shown:] {
			fmt.Println(v)
		}
	case len(ip.output) < *shown:
		fmt.Printf("↩ retracted %d output line(s) — :output shows current\n", *shown-len(ip.output))
	}
	*shown = len(ip.output)
}

// runMeta handles REPL-only commands (prefixed with ':') that drive time
// travel and inspection. Not part of the language — kept outside it so the
// language namespace stays clean.
func runMeta(line string, ip *Interp) {
	switch strings.Fields(line)[0] {
	case ":undo":
		if r, ok := ip.Undo(); ok {
			fmt.Printf("undid: %s\n", r.label())
		} else {
			fmt.Println("nothing to undo")
		}
	case ":redo":
		if r, ok := ip.Redo(); ok {
			fmt.Printf("redid: %s\n", r.label())
		} else {
			fmt.Println("nothing to redo")
		}
	case ":history":
		fmt.Println(ip.HistoryString())
	case ":env":
		fmt.Println(ip.EnvString())
	case ":output":
		fmt.Println(ip.OutputString())
	case ":help":
		fmt.Println(":undo    step back one mutation")
		fmt.Println(":redo    step forward one mutation")
		fmt.Println(":history show the timeline")
		fmt.Println(":env     show current variables")
		fmt.Println(":output  show the output buffer at the current time")
		fmt.Println(":help    this list")
	default:
		fmt.Printf("unknown command %q (try :help)\n", line)
	}
}

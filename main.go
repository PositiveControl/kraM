//go:build !js

package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	ip := NewInterp()

	// Behaviour depends on how the binary was invoked: as `krapl` it always
	// opens the REPL; as `kram file.kr` it runs a file quietly (only print
	// output, no prompts), then exits; `kram` with no file opens the REPL too.
	isREPL := strings.Contains(filepath.Base(os.Args[0]), "krapl")
	if !isREPL && len(os.Args) > 1 {
		runFile(os.Args[1], ip)
		return
	}

	in := bufio.NewScanner(os.Stdin)
	shown := 0        // output buffer entries already rendered to the terminal
	stepping := false // a :load'ed program is mid-flight, driven by :step
	lastCmd := ""     // last code line, for '!!' substitution
	fmt.Println("kraMLang — reversible REPL. :help for commands, Ctrl-D to exit.")
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
		if w := firstWord(line); w == ":exit" || w == ":quit" {
			return
		}

		// '!!' expands to the last code line (shell-style), so e.g.
		// `reverse { !! }` runs the inverse of whatever you just typed.
		// ponytail: blunt text replace — also expands a literal "!!" inside a
		// string. Make it lexer-aware (skip string tokens) if that ever bites.
		if strings.Contains(line, "!!") {
			if lastCmd == "" {
				fmt.Println("no previous command for !!")
				continue
			}
			line = strings.ReplaceAll(line, "!!", lastCmd)
			fmt.Println("»", line)
		}

		// While a stepped program is in flight, only stepping/inspection is
		// allowed — running new code or time-traveling would desync the parked
		// evaluator from the history it is still writing.
		if stepping {
			switch firstWord(line) {
			case ":step":
				label, finished, err := ip.Step()
				if finished {
					stepping = false
					if err != nil {
						fmt.Println("program halted:", err)
					} else {
						fmt.Println("program finished")
					}
				} else {
					fmt.Println("step:", label)
				}
				reconcileOutput(ip, &shown)
				printMessages(ip)
			case ":env", ":output", ":history", ":help":
				runMeta(line, ip)
			default:
				fmt.Println("stepping in progress — use :step, :env, :output, :history")
			}
			continue
		}

		if firstWord(line) == ":reset" {
			ip.Reset()
			shown = 0
			fmt.Println("state cleared")
			continue
		}

		if strings.HasPrefix(line, ":grover") {
			args := strings.TrimSpace(strings.TrimPrefix(line, ":grover"))
			report, err := groverCommand(args)
			if err != nil {
				fmt.Println(err)
				continue
			}
			fmt.Println(report)
			continue
		}

		if strings.HasPrefix(line, ":verify") {
			code := strings.TrimSpace(strings.TrimPrefix(line, ":verify"))
			ast, err := Parse(code)
			if err != nil {
				fmt.Println("parse error:", err)
				continue
			}
			report, err := verify(ast, ip)
			if err != nil {
				fmt.Println(err)
				continue
			}
			fmt.Println(report)
			continue
		}

		if strings.HasPrefix(line, ":energy") {
			code := strings.TrimSpace(strings.TrimPrefix(line, ":energy"))
			ast, err := Parse(code)
			if err != nil {
				fmt.Println("parse error:", err)
				continue
			}
			report, err := energyReport(ast, ip)
			if err != nil {
				fmt.Println(err)
				continue
			}
			fmt.Println(report)
			continue
		}

		if strings.HasPrefix(line, ":gates") {
			code := strings.TrimSpace(strings.TrimPrefix(line, ":gates"))
			ast, err := Parse(code)
			if err != nil {
				fmt.Println("parse error:", err)
				continue
			}
			bc, err := compileBits(ast, ip)
			if err != nil {
				fmt.Println("cannot compile to gates:", err)
				continue
			}
			fmt.Printf("elementary gates (%d-bit registers, %d wires):\n", bc.width, bc.nwires)
			for i, g := range bc.gates {
				fmt.Printf("  %3d  %s\n", i+1, g)
			}
			fmt.Printf("%d gates (X/CNOT/Toffoli)\n", len(bc.gates))
			continue
		}

		if strings.HasPrefix(line, ":circuit") {
			code := strings.TrimSpace(strings.TrimPrefix(line, ":circuit"))
			ast, err := Parse(code)
			if err != nil {
				fmt.Println("parse error:", err)
				continue
			}
			gates, err := lowerProgram(ast, ip)
			if err != nil {
				fmt.Println("cannot compile to circuit:", err)
				continue
			}
			fmt.Println("reversible circuit:")
			for i, g := range gates {
				fmt.Printf("  %2d  %s\n", i+1, g)
			}
			fmt.Printf("%d gate(s)\n", len(gates))
			continue
		}

		if strings.HasPrefix(line, ":invert") {
			code := strings.TrimSpace(strings.TrimPrefix(line, ":invert"))
			ast, err := Parse(code)
			if err != nil {
				fmt.Println("parse error:", err)
				continue
			}
			inv, err := invertTop(ast)
			if err != nil {
				fmt.Println("not reversible:", err)
				continue
			}
			fmt.Println(format(inv))
			continue
		}

		if strings.HasPrefix(line, ":load") {
			code := strings.TrimSpace(strings.TrimPrefix(line, ":load"))
			ast, err := Parse(code)
			if err != nil {
				fmt.Println("parse error:", err)
				continue
			}
			ip.StartStep(ast)
			stepping = true
			fmt.Println("loaded — :step to advance one mutation at a time")
			continue
		}

		if strings.HasPrefix(line, ":") {
			runMeta(line, ip)
			reconcileOutput(ip, &shown) // undo/redo may grow or shrink output
			continue
		}

		lastCmd = line // remember for '!!' (even if it errors, shell-style)
		ast, err := Parse(line)
		if err != nil {
			fmt.Println("parse error:", err)
			continue
		}
		cp := ip.checkpoint()
		val, err := Eval(ast, ip)
		if err != nil {
			ip.rollback(cp) // atomic: a failed line leaves no partial mutations
			fmt.Println("error:", err)
			continue
		}
		reconcileOutput(ip, &shown)
		printMessages(ip)
		if val.Kind != NilKind {
			ip.last = val    // '_' references this next line
			fmt.Println(val) // echo real results; print/empty-if produce nil and stay quiet
		}
	}
}

func firstWord(line string) string { return strings.Fields(line)[0] }

// runFile executes a whole source file with no REPL chrome — only print output
// and warnings reach stdout/stderr.
func runFile(path string, ip *Interp) {
	src, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ast, perr := Parse(string(src))
	if perr != nil {
		fmt.Fprintln(os.Stderr, "parse error:", perr)
		os.Exit(1)
	}
	_, eerr := Eval(ast, ip)
	for _, v := range ip.output { // flush whatever ran (even up to an error)
		fmt.Println(v.Raw())
	}
	for _, w := range ip.DrainWarnings() {
		fmt.Fprintln(os.Stderr, "⚠", w)
	}
	if eerr != nil {
		fmt.Fprintln(os.Stderr, "error:", eerr)
		os.Exit(1)
	}
}

func printMessages(ip *Interp) {
	for _, w := range ip.DrainWarnings() {
		fmt.Println("⚠", w)
	}
	for _, n := range ip.DrainNotes() {
		fmt.Println("·", n)
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
			fmt.Println(v.Raw())
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
	switch firstWord(line) {
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
		fmt.Println(":invert CODE  print the structural inverse of a program")
		fmt.Println(":circuit CODE compile reversible code to a register-level netlist")
		fmt.Println(":gates CODE   compile to elementary X/CNOT/Toffoli gates")
		fmt.Println(":verify CODE  check the circuit matches the interpreter")
		fmt.Println(":energy CODE  Landauer energy bound from garbage bits")
		fmt.Println(":grover BITS COND [iters=K]  Grover-search a compiled oracle for COND")
		fmt.Println(":load CODE  load a program to step through")
		fmt.Println(":step       run the next single mutation")
		fmt.Println(":undo       step back one mutation")
		fmt.Println(":redo       step forward one mutation")
		fmt.Println(":history    show the timeline")
		fmt.Println(":env        show current variables")
		fmt.Println(":output     show the output buffer at the current time")
		fmt.Println(":reset      clear all state and history")
		fmt.Println(":exit       quit (also :quit, or Ctrl-D)")
		fmt.Println(":help       this list")
	default:
		fmt.Printf("unknown command %q (try :help)\n", line)
	}
}

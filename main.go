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
		fmt.Println(val)
	}
}

// runMeta handles REPL-only commands (prefixed with ':') that drive time
// travel and inspection. These are not part of the language — they live
// outside it so the language namespace stays clean.
func runMeta(line string, ip *Interp) {
	switch strings.Fields(line)[0] {
	case ":undo":
		if e, ok := ip.Undo(); ok {
			fmt.Printf("undid: %s\n", describe(e))
		} else {
			fmt.Println("nothing to undo")
		}
	case ":redo":
		if e, ok := ip.Redo(); ok {
			fmt.Printf("redid: %s\n", describe(e))
		} else {
			fmt.Println("nothing to redo")
		}
	case ":history":
		fmt.Println(ip.HistoryString())
	case ":env":
		fmt.Println(ip.EnvString())
	case ":help":
		fmt.Println(":undo    step back one mutation")
		fmt.Println(":redo    step forward one mutation")
		fmt.Println(":history show the timeline")
		fmt.Println(":env     show current variables")
		fmt.Println(":help    this list")
	default:
		fmt.Printf("unknown command %q (try :help)\n", line)
	}
}

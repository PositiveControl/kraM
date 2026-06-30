package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func main() {
	in := bufio.NewScanner(os.Stdin)
	env := Env{}
	fmt.Println("mlang v0 — arithmetic REPL. Ctrl-D to exit.")
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
		ast, err := Parse(line)
		if err != nil {
			fmt.Println("parse error:", err)
			continue
		}
		val, err := Eval(ast, env)
		if err != nil {
			fmt.Println("error:", err)
			continue
		}
		fmt.Println(val)
	}
}

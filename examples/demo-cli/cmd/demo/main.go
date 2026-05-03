// Demo is a tiny CLI used to exercise the gotit runner in examples/demo-suite.
// Subcommands:
//
//	demo version           — print version+JSON, exit 0.
//	demo add A B           — print sum, exit 0.
//	demo crash             — print message to stderr, exit 1.
//	demo greet --name=foo  — print greeting, exit 0.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
)

const version = "0.1.0"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "demo:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: demo <version|add|crash|greet>")
	}
	switch args[0] {
	case "version":
		out := map[string]any{"name": "demo", "version": version}
		b, _ := json.Marshal(out)
		fmt.Println(string(b))
		return nil
	case "add":
		if len(args) != 3 {
			return errors.New("usage: demo add A B")
		}
		a, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("A: %w", err)
		}
		b, err := strconv.Atoi(args[2])
		if err != nil {
			return fmt.Errorf("B: %w", err)
		}
		fmt.Println(a + b)
		return nil
	case "crash":
		return errors.New("intentional crash")
	case "greet":
		fs := flag.NewFlagSet("greet", flag.ContinueOnError)
		name := fs.String("name", "world", "name to greet")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		fmt.Printf("Hello, %s!\n", *name)
		return nil
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

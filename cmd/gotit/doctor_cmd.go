package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/shivamstaq/gotit/tui"
)

// runDoctor prints an environment-detection report. Exits non-zero if any
// hard requirement fails. Soft warnings (no PTY, glamour fallback) are
// reported but don't change the exit code.
func runDoctor(args []string) int {
	fs := flag.NewFlagSet("gotit doctor", flag.ContinueOnError)
	silenceFlagOutput(fs)
	if err := fs.Parse(args); err != nil {
		return usageError("%v", err)
	}

	checks := tui.CheckEnvironment()
	fmt.Print(tui.FormatReport(checks))

	// Project-level checks: go.mod exists, gotit.yaml resolves.
	root, err := findProjectRoot()
	if err != nil {
		fmt.Printf("✗ %-30s %s\n", "Go module root", err)
		fmt.Println("\n(some project checks skipped)")
		if tui.HasHardFailure(checks) {
			return 1
		}
		return 0
	}
	fmt.Printf("✓ %-30s %s\n", "Go module root", root)

	if cfg, err := loadGotitConfig(root); err != nil {
		fmt.Printf("✗ %-30s %v\n", "tests/e2e/gotit.yaml", err)
		fmt.Fprintln(os.Stderr, "\nrun `gotit init` to scaffold the project.")
		return 1
	} else {
		fmt.Printf("✓ %-30s binary=%s prefix=%s\n",
			"tests/e2e/gotit.yaml", cfg.BinaryName, cfg.EnvPrefix)
	}

	if tui.HasHardFailure(checks) {
		return 1
	}
	return 0
}

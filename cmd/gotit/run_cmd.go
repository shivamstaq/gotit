package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// runHeadless is the CI-friendly invocation: shells to `go test` with the
// supplied -run pattern, streams output to stdout/stderr, and exits with the
// `go test` exit code.
//
// Examples:
//
//	gotit run                       → all specs
//	gotit run wave1                 → -run TestE2E/wave1
//	gotit run wave3/risk-class      → -run TestE2E/wave3/risk-class
//	gotit run -- -count=1 -race ... → forwards flags to `go test`
func runHeadless(args []string) int {
	fs := flag.NewFlagSet("gotit run", flag.ContinueOnError)
	silenceFlagOutput(fs)
	if err := fs.Parse(args); err != nil {
		return usageError("%v", err)
	}

	pattern := ""
	extraFlags := fs.Args()
	if len(extraFlags) > 0 {
		pattern = extraFlags[0]
		extraFlags = extraFlags[1:]
	}

	projectRoot, err := findProjectRoot()
	if err != nil {
		return usageError("%v", err)
	}
	cfg, err := loadGotitConfig(projectRoot)
	if err != nil {
		return usageError("%v", err)
	}

	testPkg := "./" + filepath.ToSlash(filepath.Dir(cfg.SpecsDir))
	if testPkg == "./." {
		testPkg = "./tests/e2e"
	}

	runFilter := "TestE2E"
	if pattern != "" {
		runFilter = "TestE2E/" + pattern
	}

	goArgs := []string{"test", testPkg, "-run", runFilter, "-count=1", "-v"}
	goArgs = append(goArgs, extraFlags...)

	cmd := exec.Command("go", goArgs...)
	cmd.Dir = projectRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = os.Environ()

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "gotit run:", err)
		return 1
	}
	return 0
}

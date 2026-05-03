// Command gotit is the headline interactive surface for the gotit kit.
//
// Usage:
//
//	gotit                    # interactive TUI in the current Go module
//	gotit init               # scaffold tests/e2e/ in this project
//	gotit run [pattern]      # headless `go test` wrapper, CI-friendly
//	gotit doctor             # environment-detection report
//	gotit version            # print version and exit
//
// Bare `gotit` requires a TTY and a Go module rooted at or above CWD.
// `gotit run` works headless. See SPEC.md §17 for the full surface contract.
package main

import (
	"flag"
	"fmt"
	"os"
)

const usageText = `gotit — interactive E2E test runner for Go CLIs

Usage:
  gotit                  Launch the interactive TUI.
  gotit init             Scaffold tests/e2e/ for this project.
  gotit run [pattern]    Run specs headlessly; pattern feeds -run regex.
  gotit doctor           Report environment readiness for the TUI.
  gotit version          Print version and exit.
  gotit help             This message.

The bare TUI walks up to the nearest go.mod and reads tests/e2e/gotit.yaml
(or the GOTIT_BINARY / GOTIT_BUILD_PATH / GOTIT_ENV_PREFIX env vars) to
learn the project's wiring. See https://github.com/shivamstaq/gotit/blob/main/SPEC.md.
`

// Version is set via -ldflags at build time, falling back to "dev" otherwise.
var Version = "dev"

func main() {
	if len(os.Args) < 2 {
		os.Exit(runTUI(nil))
	}
	switch os.Args[1] {
	case "init":
		os.Exit(runInit(os.Args[2:]))
	case "run":
		os.Exit(runHeadless(os.Args[2:]))
	case "doctor":
		os.Exit(runDoctor(os.Args[2:]))
	case "version", "--version", "-v":
		fmt.Println("gotit", Version)
		return
	case "help", "--help", "-h":
		fmt.Print(usageText)
		return
	default:
		// Treat unknown args as flags for the bare TUI (e.g. `gotit -nosplash`).
		// Re-parse from index 1 so the TUI can take its own flags later.
		os.Exit(runTUI(os.Args[1:]))
	}
}

// usageError prints a short error and the usage hint, then exits non-zero.
func usageError(format string, args ...any) int {
	fmt.Fprintf(os.Stderr, "gotit: "+format+"\n", args...)
	fmt.Fprintln(os.Stderr, "run `gotit help` for usage.")
	return 2
}

// silenceFlagOutput tells the flag package not to dump errors twice.
func silenceFlagOutput(fs *flag.FlagSet) {
	fs.SetOutput(os.Stderr)
}

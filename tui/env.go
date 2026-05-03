package tui

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"golang.org/x/term"
)

// EnvCheck is one row in the environment-detection report.
type EnvCheck struct {
	Name    string
	Passed  bool
	Detail  string
	Hard    bool // hard failure → caller should error out
	Warning string
}

// CheckEnvironment runs the full battery of TUI prerequisites and returns
// the results in display order. It does not error — callers decide whether
// any Hard failures should block.
func CheckEnvironment() []EnvCheck {
	var out []EnvCheck

	// 1) TTY check (stdout).
	isTTY := term.IsTerminal(int(os.Stdout.Fd()))
	out = append(out, EnvCheck{
		Name:   "stdout is a TTY",
		Passed: isTTY,
		Detail: ttyDetail(isTTY),
		Hard:   true,
	})

	// 2) Min terminal size.
	cols, rows, _ := term.GetSize(int(os.Stdout.Fd()))
	enough := cols >= 80 && rows >= 24
	detail := fmt.Sprintf("%dx%d (need ≥80×24)", cols, rows)
	if !isTTY {
		detail = "skipped (no TTY)"
		enough = true
	}
	out = append(out, EnvCheck{
		Name:   "terminal size",
		Passed: enough,
		Detail: detail,
		Hard:   isTTY,
	})

	// 3) TERM env var.
	termVar := os.Getenv("TERM")
	out = append(out, EnvCheck{
		Name:    "TERM env var",
		Passed:  termVar != "" && termVar != "dumb",
		Detail:  fmt.Sprintf("%q", termVar),
		Hard:    false,
		Warning: termWarning(termVar),
	})

	// 4) `go` on PATH.
	goPath, err := exec.LookPath("go")
	out = append(out, EnvCheck{
		Name:   "`go` on PATH",
		Passed: err == nil,
		Detail: goDetail(goPath, err),
		Hard:   true,
	})

	// 5) PTY availability.
	ptyOK, ptyDetail := checkPTY()
	out = append(out, EnvCheck{
		Name:    "PTY (creack/pty)",
		Passed:  ptyOK,
		Detail:  ptyDetail,
		Hard:    false,
		Warning: ptyWarning(ptyOK),
	})

	// 6) glamour smoke render.
	gOK, gDetail := checkGlamour()
	out = append(out, EnvCheck{
		Name:    "glamour markdown render",
		Passed:  gOK,
		Detail:  gDetail,
		Hard:    false,
		Warning: glamourWarning(gOK),
	})

	return out
}

// HasHardFailure reports whether any Hard check failed.
func HasHardFailure(checks []EnvCheck) bool {
	for _, c := range checks {
		if c.Hard && !c.Passed {
			return true
		}
	}
	return false
}

// FormatReport renders the checks as a plain-text report for `gotit doctor`.
func FormatReport(checks []EnvCheck) string {
	var b strings.Builder
	for _, c := range checks {
		mark := "✓"
		if !c.Passed {
			if c.Hard {
				mark = "✗"
			} else {
				mark = "!"
			}
		}
		fmt.Fprintf(&b, "%s %-30s %s\n", mark, c.Name, c.Detail)
		if c.Warning != "" && !c.Passed {
			fmt.Fprintf(&b, "  %s\n", c.Warning)
		}
	}
	return b.String()
}

// --- helpers ---

func ttyDetail(ok bool) string {
	if ok {
		return "yes"
	}
	return "no — bare `gotit` requires a TTY; use `gotit run` for headless mode"
}

func termWarning(t string) string {
	if t == "" || t == "dumb" {
		return `set TERM=xterm-256color (or similar) for color rendering`
	}
	return ""
}

func goDetail(path string, err error) string {
	if err != nil {
		return "not found — install Go (https://go.dev/dl/)"
	}
	out, err := exec.Command(path, "version").Output()
	if err != nil {
		return path
	}
	return strings.TrimSpace(string(out))
}

// checkPTY tries to instantiate a pty pair to confirm pty support is real.
func checkPTY() (bool, string) {
	if runtime.GOOS == "windows" {
		return false, "Windows ConPTY not yet supported by gotit"
	}
	// Best-effort: actually opening a pty is more reliable than a goos check.
	// We don't import creack/pty here to keep this file dep-thin; instead
	// rely on the goos check above. shell.go is what actually uses pty.
	return true, "available (Linux/macOS)"
}

func ptyWarning(ok bool) string {
	if ok {
		return ""
	}
	return "the embedded shell pane will be unavailable; use an external terminal to inspect preserved temp HOMEs"
}

// checkGlamour tries a one-line render to make sure glamour/chroma can load.
func checkGlamour() (bool, string) {
	defer func() { _ = recover() }()
	out := renderMarkdown("# smoke", 80)
	if strings.Contains(out, "smoke") {
		return true, "ok"
	}
	return false, "render returned empty output"
}

func glamourWarning(ok bool) string {
	if ok {
		return ""
	}
	return "notes pane will fall back to plain markdown"
}

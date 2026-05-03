// Package tui implements the interactive terminal UI for the gotit kit.
//
// The TUI is the headline interactive surface for the E2E test suite. It is
// purposely decoupled from runner internals: it reads YAML specs (Layer 1),
// invokes `go test` to execute them via the runner (Layer 2), and tails the
// JSONL log file the runner writes to surface live events. See SPEC.md §17.
package tui

import (
	"context"
	"sync"

	"github.com/shivamstaq/gotit/runner"
)

// TestState is the current execution state of a spec in the TUI.
type TestState int

const (
	StatePending TestState = iota
	StateQueued
	StateRunning
	StatePassed
	StateFailed
	StateSkipped // requirements not met
)

func (s TestState) String() string {
	switch s {
	case StatePending:
		return "pending"
	case StateQueued:
		return "queued"
	case StateRunning:
		return "running"
	case StatePassed:
		return "passed"
	case StateFailed:
		return "failed"
	case StateSkipped:
		return "skipped"
	default:
		return "unknown"
	}
}

// TestEntry holds the full TUI state for a single spec.
type TestEntry struct {
	Spec        *runner.Spec
	State       TestState
	Steps       []*StepResult
	CurrentStep int // index of currently executing step (-1 = none)
	WorkDir     string
	HomeDir     string
	BinDir      string
	Env         map[string]string
	DurationMs  int64
	Error       string // runner / `go test` error message
	ReqError    string // requirements check failure

	// Cancellation
	cancel context.CancelFunc
	mu     sync.Mutex
}

// SetCancel stores a cancel function for an in-flight run. Thread-safe.
func (te *TestEntry) SetCancel(c context.CancelFunc) {
	te.mu.Lock()
	defer te.mu.Unlock()
	te.cancel = c
}

// Cancel cancels a running test if one is in flight.
func (te *TestEntry) Cancel() {
	te.mu.Lock()
	defer te.mu.Unlock()
	if te.cancel != nil {
		te.cancel()
	}
}

// StepResult holds the outcome of one step for TUI display.
type StepResult struct {
	Phase      string // "setup", "test", "cleanup"
	Name       string
	Command    string
	ExitCode   int
	DurationMs int64
	Stdout     string
	Stderr     string
	Assertions []runner.AssertionResult
	Passed     bool
}

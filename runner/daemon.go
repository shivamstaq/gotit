package runner

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// DaemonDefaults captures the lifecycle defaults applied when a Daemon /
// background Step leaves fields unset.
var DaemonDefaults = struct {
	StartTimeout    time.Duration
	ReadyTimeout    time.Duration
	ReadyInterval   time.Duration
	StopSignal      syscall.Signal
	StopGrace       time.Duration
	LogBufferCap    int // bytes retained in the ring buffer
	StopSignalLabel string
}{
	StartTimeout:    10 * time.Second,
	ReadyTimeout:    5 * time.Second,
	ReadyInterval:   100 * time.Millisecond,
	StopSignal:      syscall.SIGTERM,
	StopGrace:       5 * time.Second,
	LogBufferCap:    64 * 1024,
	StopSignalLabel: "TERM",
}

// DaemonHandle is the runner-side handle to a running daemon.
type DaemonHandle struct {
	Name      string
	Command   string // resolved command line (post-template, post-binary-substitution)
	Cmd       *exec.Cmd
	PID       int
	PGID      int
	LogBuf    *ringBuffer
	LogFile   string // absolute path to the on-disk log tee
	StartedAt time.Time
	ReadyAt   time.Time

	// ExpectExit is true for background daemons designed to exit on
	// their own (e.g. a subscriber bound to `--max-events 1`). When
	// set, PollDeaths skips this daemon and teardown tolerates an
	// already-dead process exiting with code 0.
	ExpectExit bool

	exited   chan struct{}
	exitErr  error
	exitCode int

	stopOnce sync.Once
}

// DaemonResult is the teardown record for one daemon.
type DaemonResult struct {
	Name           string
	PID            int
	Signal         string // signal sent to initiate stop
	Grace          time.Duration
	ExitCode       int
	Duration       time.Duration // wall clock from start to exit
	CleanExit      bool          // false when SIGKILL was needed
	ExitMismatch   bool          // exit code differed from expected
	ExpectedExit   *int
	LogTail        string
	LogAssertions  []AssertionResult
	UnexpectedDeath bool // exited before Stop() was called
}

// DaemonRegistry tracks daemons started during a spec.
type DaemonRegistry struct {
	mu      sync.Mutex
	handles []*DaemonHandle // ordered by start time
}

// All returns the registered daemon handles in start order.
func (r *DaemonRegistry) All() []*DaemonHandle {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*DaemonHandle, len(r.handles))
	copy(out, r.handles)
	return out
}

// add appends a handle to the registry.
func (r *DaemonRegistry) add(h *DaemonHandle) {
	r.mu.Lock()
	r.handles = append(r.handles, h)
	r.mu.Unlock()
}

// PollDeaths returns the first handle whose process has exited
// unexpectedly (Stop hasn't been called). It does not block.
//
// Handles with ExpectExit=true are skipped: those are background
// daemons designed to exit on their own (e.g. a subscriber with
// `--max-events 1`). The teardown phase still records their final
// exit code via the regular Stop path.
func (r *DaemonRegistry) PollDeaths() *DaemonHandle {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, h := range r.handles {
		if h.ExpectExit {
			continue
		}
		select {
		case <-h.exited:
			return h
		default:
		}
	}
	return nil
}

// StartDaemon spawns a daemon and waits for its readiness check to pass (if
// any). On error, the process group is killed before returning.
//
// command, env, ready, stop, capture, logAssert, startTimeout come from the
// caller — the same fields are populated identically whether the daemon was
// declared at top-level or via a background:true step.
func StartDaemon(
	ctx context.Context,
	cfg Config,
	name string,
	command string,
	stepEnv map[string]string,
	specEnv []string,
	workDir string,
	homeDir string,
	vars map[string]string,
	binPath string,
	ready *ReadyCheck,
	startTimeoutRaw string,
) (*DaemonHandle, error) {
	parts, cmdLine, err := PrepareCommand(cfg, command, vars, binPath)
	if err != nil {
		return nil, fmt.Errorf("daemon %q: %w", name, err)
	}

	logDir := filepath.Join(homeDir, ".gotit-daemons")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, fmt.Errorf("daemon %q: create log dir: %w", name, err)
	}
	logPath := filepath.Join(logDir, name+".log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return nil, fmt.Errorf("daemon %q: open log file: %w", name, err)
	}

	buf := newRingBuffer(DaemonDefaults.LogBufferCap)
	out := io.MultiWriter(buf, logFile)

	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Dir = workDir
	cmd.Env = append(append([]string{}, specEnv...), EnvMapToSlice(stepEnv)...)
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	startedAt := time.Now()
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return nil, fmt.Errorf("daemon %q: start: %w", name, err)
	}

	pid := cmd.Process.Pid
	pgid, _ := syscall.Getpgid(pid)
	h := &DaemonHandle{
		Name:      name,
		Command:   cmdLine,
		Cmd:       cmd,
		PID:       pid,
		PGID:      pgid,
		LogBuf:    buf,
		LogFile:   logPath,
		StartedAt: startedAt,
		exited:    make(chan struct{}),
	}

	// Death watcher: closes exited once Wait() returns.
	go func() {
		waitErr := cmd.Wait()
		_ = logFile.Close()
		h.exitErr = waitErr
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			h.exitCode = exitErr.ExitCode()
		} else if waitErr != nil {
			h.exitCode = -1
		}
		close(h.exited)
	}()

	startTimeout := DaemonDefaults.StartTimeout
	if startTimeoutRaw != "" {
		if d, perr := time.ParseDuration(startTimeoutRaw); perr == nil {
			startTimeout = d
		}
	}

	if err := h.WaitReady(ctx, ready, startTimeout); err != nil {
		// Clean up before returning the failure.
		h.stopForFailure()
		return nil, fmt.Errorf("daemon %q: readiness: %w", name, err)
	}
	h.ReadyAt = time.Now()
	return h, nil
}

// WaitReady blocks until the configured readiness check passes, or the
// timeout expires, or the daemon dies. nil check means "started == ready".
func (h *DaemonHandle) WaitReady(ctx context.Context, check *ReadyCheck, fallbackTimeout time.Duration) error {
	if check == nil {
		// No explicit ready check — succeed immediately, but reject if the
		// process has already died.
		select {
		case <-h.exited:
			return fmt.Errorf("daemon exited immediately (exit=%d): %s",
				h.exitCode, snippetLine(h.LogBuf.String(), 200))
		default:
			return nil
		}
	}
	timeout := fallbackTimeout
	if check.Timeout != "" {
		if d, err := time.ParseDuration(check.Timeout); err == nil {
			timeout = d
		}
	}
	interval := DaemonDefaults.ReadyInterval
	if check.Interval != "" {
		if d, err := time.ParseDuration(check.Interval); err == nil {
			interval = d
		}
	}

	deadline := time.Now().Add(timeout)
	probe := readinessProbe(check)
	for {
		if probe(h) {
			return nil
		}
		// Did the daemon die under us?
		select {
		case <-h.exited:
			return fmt.Errorf("daemon exited before ready (exit=%d): %s",
				h.exitCode, snippetLine(h.LogBuf.String(), 200))
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("ready check %s timed out after %s: %s",
				describeReady(check), timeout, snippetLine(h.LogBuf.String(), 200))
		}
		select {
		case <-time.After(interval):
		case <-h.exited:
			return fmt.Errorf("daemon exited before ready (exit=%d): %s",
				h.exitCode, snippetLine(h.LogBuf.String(), 200))
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Stop initiates graceful shutdown and returns the teardown record. Idempotent.
func (h *DaemonHandle) Stop(stop *StopSpec) DaemonResult {
	res := DaemonResult{Name: h.Name, PID: h.PID}
	h.stopOnce.Do(func() {
		signal := DaemonDefaults.StopSignal
		signalLabel := DaemonDefaults.StopSignalLabel
		grace := DaemonDefaults.StopGrace
		var expectedExit *int
		if stop != nil {
			if stop.Signal != "" {
				signal, signalLabel = parseSignal(stop.Signal)
			}
			if stop.Grace != "" {
				if d, err := time.ParseDuration(stop.Grace); err == nil {
					grace = d
				}
			}
			expectedExit = stop.ExpectedExit
		}
		res.Signal = signalLabel
		res.Grace = grace
		res.ExpectedExit = expectedExit

		// Already dead? Capture state and return. For ExpectExit
		// daemons (e.g. `--max-events 1` subscribers) the natural
		// exit is the design, not a fault — don't flag it as
		// UnexpectedDeath.
		select {
		case <-h.exited:
			res.CleanExit = true
			res.UnexpectedDeath = !h.ExpectExit
		default:
			// Send signal to the whole process group so forked children die too.
			killGroup(h.PGID, h.PID, signal)
			select {
			case <-h.exited:
				res.CleanExit = true
			case <-time.After(grace):
				killGroup(h.PGID, h.PID, syscall.SIGKILL)
				<-h.exited
				res.CleanExit = false
			}
		}
		res.ExitCode = h.exitCode
		res.Duration = time.Since(h.StartedAt)
		if expectedExit != nil && *expectedExit != h.exitCode {
			res.ExitMismatch = true
		}
		res.LogTail = tail(h.LogBuf.String(), 4*1024)
	})
	return res
}

// stopForFailure is the internal "best-effort kill" used when readiness fails;
// it does not populate a DaemonResult.
func (h *DaemonHandle) stopForFailure() {
	h.stopOnce.Do(func() {
		select {
		case <-h.exited:
			return
		default:
		}
		killGroup(h.PGID, h.PID, syscall.SIGTERM)
		select {
		case <-h.exited:
		case <-time.After(2 * time.Second):
			killGroup(h.PGID, h.PID, syscall.SIGKILL)
			<-h.exited
		}
	})
}

// killGroup sends sig to the process group. Falls back to killing just the
// leader if the pgid lookup failed earlier.
func killGroup(pgid, pid int, sig syscall.Signal) {
	if pgid > 0 {
		if err := syscall.Kill(-pgid, sig); err == nil {
			return
		}
	}
	_ = syscall.Kill(pid, sig)
}

// parseSignal maps the YAML-friendly signal name to a syscall.Signal.
// Validation in spec.go ensures the input is one of TERM/INT/HUP/QUIT.
func parseSignal(name string) (syscall.Signal, string) {
	switch strings.ToUpper(name) {
	case "INT":
		return syscall.SIGINT, "INT"
	case "HUP":
		return syscall.SIGHUP, "HUP"
	case "QUIT":
		return syscall.SIGQUIT, "QUIT"
	default:
		return syscall.SIGTERM, "TERM"
	}
}

// readinessProbe returns a function that checks ready at a point in time.
func readinessProbe(c *ReadyCheck) func(*DaemonHandle) bool {
	switch {
	case c.TCP != "":
		addr := c.TCP
		return func(*DaemonHandle) bool {
			conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
			if err != nil {
				return false
			}
			_ = conn.Close()
			return true
		}
	case c.LogContains != "":
		needle := c.LogContains
		return func(h *DaemonHandle) bool {
			return strings.Contains(h.LogBuf.String(), needle)
		}
	case c.Command != "":
		cmdLine := c.Command
		return func(h *DaemonHandle) bool {
			parts := SplitCommand(cmdLine)
			if len(parts) == 0 {
				return false
			}
			cmd := exec.Command(parts[0], parts[1:]...)
			// Inherit the daemon's env so the probe shell expands
			// $HOME / $XDG_CONFIG_HOME / $PATH the same way the
			// daemon does. Otherwise the probe runs with gotit's
			// own $HOME and a spec like `grep -q foo $HOME/x` looks
			// at the wrong file. Fixed in v0.3.1.
			if h != nil && h.Cmd != nil && len(h.Cmd.Env) > 0 {
				cmd.Env = h.Cmd.Env
				cmd.Dir = h.Cmd.Dir
			}
			return cmd.Run() == nil
		}
	case c.PIDFile != "":
		path := c.PIDFile
		return func(*DaemonHandle) bool {
			info, err := os.Stat(path)
			return err == nil && info.Size() > 0
		}
	}
	return func(*DaemonHandle) bool { return true }
}

func describeReady(c *ReadyCheck) string {
	if c == nil {
		return "(none)"
	}
	switch {
	case c.TCP != "":
		return "tcp=" + c.TCP
	case c.LogContains != "":
		return "log_contains=" + c.LogContains
	case c.Command != "":
		return "command=" + c.Command
	case c.PIDFile != "":
		return "pid_file=" + c.PIDFile
	}
	return "(none)"
}

// ApplyCapture runs the daemon's Capture map against the current log buffer
// and writes results into vars. Capture failures are silent (consistent with
// Step capture in runner.go) — a missing var produces a clear failure
// downstream when the literal {{ name }} survives template substitution.
func (h *DaemonHandle) ApplyCapture(capture map[string]string, vars map[string]string) {
	if len(capture) == 0 {
		return
	}
	src := h.LogBuf.String()
	for name, expr := range capture {
		resolved := ResolveTemplates(expr, vars)
		val, err := ExtractValue(src, resolved)
		if err != nil {
			continue
		}
		vars[name] = val
	}
}

// RunLogAssertions evaluates assertions against the daemon's captured log.
// The log is treated as Stdout in the synthetic StepResult so existing
// contains / not_contains / regex / stderr_contains assertions all reuse the
// same dispatcher. (stderr_contains is intentionally also checked against the
// merged log — daemons capture stdout+stderr together.)
func RunLogAssertions(asserts []Assertion, logTail string, vars map[string]string, custom map[string]AssertionFunc) []AssertionResult {
	if len(asserts) == 0 {
		return nil
	}
	synthetic := StepResult{Stdout: logTail, Stderr: logTail}
	out := make([]AssertionResult, 0, len(asserts))
	for _, a := range asserts {
		r := RunAssertion(a, synthetic, vars, custom)
		r.Type = a.Type
		out = append(out, r)
	}
	return out
}

// ringBuffer is a bounded byte buffer; writes beyond cap drop the oldest data.
type ringBuffer struct {
	mu  sync.Mutex
	buf []byte
	cap int
}

func newRingBuffer(cap int) *ringBuffer {
	return &ringBuffer{cap: cap}
}

func (r *ringBuffer) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, p...)
	if len(r.buf) > r.cap {
		keep := r.cap
		drop := len(r.buf) - keep
		r.buf = append(r.buf[:0], r.buf[drop:]...)
	}
	return len(p), nil
}

func (r *ringBuffer) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.buf)
}

// tail returns the last n bytes of s, prefixed with "..." when truncated.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}

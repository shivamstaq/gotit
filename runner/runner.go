package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// SpecEvent is the typed message the runner emits to consumers. Consumers
// receive: spec_start → step_start/step_done* → daemon_*? → spec_end. The
// events channel is closed by RunSpec when the spec finishes.
//
// Daemon events (daemon_start, daemon_ready, daemon_died, daemon_stop) carry
// the daemon's name in Daemon, its OS pid in PID, and (for daemon_stop) the
// signal that was sent and the log tail in Stdout for post-mortem.
type SpecEvent struct {
	Type       string // spec_start, step_start, step_done, spec_end,
	                  // daemon_start, daemon_ready, daemon_died, daemon_stop
	Wave       string
	Spec       string
	Phase      string // setup, test, cleanup, daemon
	Step       string
	Command    string
	Stdout     string
	Stderr     string
	ExitCode   int
	DurationMs int64
	Assertions []AssertionResult
	Passed     bool
	Skipped    bool // spec_end only: true when the testdriver skipped on unmet requirements
	WorkDir    string
	HomeDir    string
	BinDir     string
	Env        map[string]string
	Error      string

	// Daemon-specific fields. Empty/zero on non-daemon events.
	Daemon string
	PID    int
	Signal string
	Ready  string // which readiness mode fired (e.g. "tcp", "log_contains", "(none)")
}

// Runner orchestrates spec execution.
type Runner struct {
	Config      Config
	BinDir      string       // directory containing the built binary
	ProjectRoot string       // absolute path to the consumer's repo root
	Logger      *JSONLLogger // optional; nil disables file logging

	// teardown stores per-daemon Stop/LogAssert config keyed by
	// teardownKey(spec, daemonName) so the stop phase can look them up
	// without re-walking the spec.
	teardownMu sync.Mutex
	teardown   map[string]daemonTeardown
}

// New constructs a Runner from a Config, applying defaults and validating.
func New(cfg Config, binDir, projectRoot string) (*Runner, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &Runner{
		Config:      cfg.withDefaults(),
		BinDir:      binDir,
		ProjectRoot: projectRoot,
	}, nil
}

// BuildBinary compiles the consumer's CLI into BinDir/<BinaryName>.
func (r *Runner) BuildBinary(ctx context.Context) error {
	if err := os.MkdirAll(r.BinDir, 0o755); err != nil {
		return fmt.Errorf("create bin dir: %w", err)
	}
	binPath := filepath.Join(r.BinDir, r.Config.BinaryName)
	cmd := exec.CommandContext(ctx, "go", "build", "-o", binPath, r.Config.BuildPath)
	cmd.Dir = r.ProjectRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("build %s: %w\n%s", r.Config.BinaryName, err, out)
	}
	return nil
}

// RunSpec executes one spec end-to-end, streaming events. The channel is
// closed when the spec finishes. The caller is responsible for cleaning up
// the temp HOME directory (read from the spec_start event's HomeDir field).
//
// Daemon lifecycle: pre_setup daemons start after provisioning; post_setup
// daemons (the default) start after setup; background:true steps start
// in-line during the test phase. All are stopped in reverse start order
// before cleanup assertions run. See SPEC.md §6.5.
func (r *Runner) RunSpec(ctx context.Context, spec *Spec, events chan<- SpecEvent) error {
	defer close(events)

	homeDir, err := os.MkdirTemp("", "gotit-"+spec.Name+"-*")
	if err != nil {
		return fmt.Errorf("create home dir: %w", err)
	}
	workDir := filepath.Join(homeDir, "work")

	binPath := filepath.Join(r.BinDir, r.Config.BinaryName)
	env := BuildEnv(r.Config, homeDir, r.BinDir, r.ProjectRoot)
	envMap := EnvSliceToMap(env)

	events <- SpecEvent{
		Type: "spec_start", Wave: spec.Wave, Spec: spec.Name,
		WorkDir: workDir, HomeDir: homeDir, BinDir: r.BinDir, Env: envMap,
	}
	r.logSpecStart(spec, workDir, homeDir, envMap)

	specStart := time.Now()
	specPassed := true
	registry := &DaemonRegistry{}
	vars := map[string]string{}

	// stopAllDaemons is invoked on every exit path (success or failure) before
	// spec_end is emitted, so daemon teardown results and log assertions are
	// always reported and the process group is always reaped.
	stopAllDaemons := func() bool {
		ok := true
		handles := registry.All()
		for i := len(handles) - 1; i >= 0; i-- {
			h := handles[i]
			stopSpec, logAsserts := r.daemonStopConfig(spec, h.Name)
			res := h.Stop(stopSpec)
			res.LogAssertions = RunLogAssertions(logAsserts, h.LogBuf.String(), vars, r.Config.Assertions)
			passed := res.CleanExit && !res.ExitMismatch
			for _, a := range res.LogAssertions {
				if !a.Passed {
					passed = false
				}
			}
			if !passed {
				ok = false
			}
			r.emitDaemonStop(events, spec, h, res, passed)
		}
		return ok
	}

	finish := func(passed bool, errMsg string) {
		daemonsOK := stopAllDaemons()
		r.emitSpecEnd(events, spec, passed && daemonsOK, specStart, errMsg)
	}

	// Provision the repo.
	fixturesDir := r.Config.FixturesDir
	if !filepath.IsAbs(fixturesDir) {
		fixturesDir = filepath.Join(r.ProjectRoot, fixturesDir)
	}
	switch {
	case spec.Repo.Fixture != "":
		if err := PrepareFixtureRepo(fixturesDir, spec.Repo.Fixture, workDir); err != nil {
			finish(false, err.Error())
			return fmt.Errorf("prepare fixture: %w", err)
		}
	case spec.Repo.Helper != "":
		if err := CallRepoHelper(r.Config.RepoHelpers, r.ProjectRoot, spec.Repo.Helper, spec.Repo.HelperArgs, workDir); err != nil {
			finish(false, err.Error())
			return fmt.Errorf("repo helper: %w", err)
		}
	default:
		if err := os.MkdirAll(workDir, 0o755); err != nil {
			finish(false, err.Error())
			return fmt.Errorf("create work dir: %w", err)
		}
	}

	// pre_setup daemons.
	if err := r.startDaemonsForPhase(ctx, spec, DaemonPhasePreSetup, registry, env, workDir, homeDir, vars, binPath, events); err != nil {
		finish(false, err.Error())
		return err
	}

	// Setup phase: any failure aborts the spec. Daemon deaths between setup
	// steps also abort.
	for _, step := range spec.Setup {
		if ctx.Err() != nil {
			finish(false, "cancelled")
			return ctx.Err()
		}
		if dead := registry.PollDeaths(); dead != nil {
			r.emitDaemonDied(events, spec, dead)
			finish(false, fmt.Sprintf("daemon %q died during setup", dead.Name))
			return fmt.Errorf("daemon %q died unexpectedly during setup", dead.Name)
		}
		events <- SpecEvent{Type: "step_start", Wave: spec.Wave, Spec: spec.Name, Phase: "setup", Step: step.Name, Command: ResolveTemplates(step.Command, vars)}
		result := ExecuteStep(ctx, r.Config, step, workDir, env, vars, binPath)
		stepPassed := result.ExitCode == 0
		evt := stepDoneEvent(spec, "setup", step, result, nil, stepPassed)
		events <- evt
		r.logStep(spec, "setup", step, result, nil, stepPassed)
		if !stepPassed {
			finish(false, fmt.Sprintf("setup step %q failed", step.Name))
			return fmt.Errorf("setup step %q failed (exit %d): %s", step.Name, result.ExitCode, result.Stderr)
		}
	}

	// post_setup daemons (default phase).
	if err := r.startDaemonsForPhase(ctx, spec, DaemonPhasePostSetup, registry, env, workDir, homeDir, vars, binPath, events); err != nil {
		finish(false, err.Error())
		return err
	}

	// Test phase: assertions are evaluated; failures recorded but the loop
	// continues. Daemon deaths between steps abort the loop (fail-fast).
	for _, step := range spec.Steps {
		if ctx.Err() != nil {
			finish(false, "cancelled")
			return ctx.Err()
		}
		if dead := registry.PollDeaths(); dead != nil {
			r.emitDaemonDied(events, spec, dead)
			specPassed = false
			break
		}
		if step.Background {
			if err := r.startBackgroundStep(ctx, spec, step, registry, env, workDir, homeDir, vars, binPath, events); err != nil {
				specPassed = false
				break
			}
			continue
		}
		events <- SpecEvent{Type: "step_start", Wave: spec.Wave, Spec: spec.Name, Phase: "test", Step: step.Name, Command: ResolveTemplates(step.Command, vars)}

		result := ExecuteStep(ctx, r.Config, step, workDir, env, vars, binPath)

		// Capture variables for downstream {{var}} substitution.
		for name, expr := range step.Capture {
			resolved := ResolveTemplates(expr, vars)
			val, err := ExtractValue(result.Stdout, resolved)
			if err != nil {
				continue
			}
			vars[name] = val
		}

		stepPassed := true
		var assertions []AssertionResult
		for _, a := range step.Assert {
			ar := RunAssertion(a, result, vars, r.Config.Assertions)
			ar.Type = a.Type
			if !ar.Passed {
				stepPassed = false
			}
			assertions = append(assertions, ar)
		}
		if !stepPassed {
			specPassed = false
		}

		events <- stepDoneEvent(spec, "test", step, result, assertions, stepPassed)
		r.logStep(spec, "test", step, result, assertions, stepPassed)
	}

	// Stop daemons before cleanup assertions run, so clean-shutdown failures
	// surface alongside the test results.
	daemonsOK := stopAllDaemons()
	if !daemonsOK {
		specPassed = false
	}

	// Cleanup phase: post-condition assertions only. Temp HOME removal is
	// the consumer's t.Cleanup responsibility.
	for _, check := range spec.Cleanup {
		cleanupPassed := true
		var errs []string
		for _, path := range check.AssertFilesRemoved {
			fullPath := filepath.Join(workDir, path)
			if _, err := os.Stat(fullPath); err == nil {
				cleanupPassed = false
				errs = append(errs, fmt.Sprintf("file %q should have been removed but still exists", path))
			}
		}
		if !cleanupPassed {
			specPassed = false
		}
		evt := SpecEvent{
			Type: "step_done", Wave: spec.Wave, Spec: spec.Name,
			Phase: "cleanup", Step: "cleanup_verification",
			Stdout: strings.Join(errs, "\n"), Passed: cleanupPassed,
		}
		events <- evt
		r.logStep(spec, "cleanup", Step{Name: "cleanup_verification"}, StepResult{Stdout: strings.Join(errs, "\n")}, nil, cleanupPassed)
	}

	r.emitSpecEnd(events, spec, specPassed, specStart, "")
	return nil
}

// startDaemonsForPhase starts every Daemon whose Phase matches and registers
// it. Returns the first start error; on error, no further daemons in the
// phase are launched (the registry still holds the ones that did start, so
// the caller's teardown path can reap them).
func (r *Runner) startDaemonsForPhase(
	ctx context.Context,
	spec *Spec,
	phase string,
	registry *DaemonRegistry,
	env []string,
	workDir, homeDir string,
	vars map[string]string,
	binPath string,
	events chan<- SpecEvent,
) error {
	for _, d := range spec.Daemons {
		dphase := d.Phase
		if dphase == "" {
			dphase = DaemonPhasePostSetup
		}
		if dphase != phase {
			continue
		}
		if err := r.spawnDaemon(ctx, spec, d.Name, d.Command, d.Env, d.Ready, d.Stop, d.Capture, d.LogAssert, d.StartTimeout, registry, env, workDir, homeDir, vars, binPath, events); err != nil {
			return err
		}
	}
	return nil
}

// startBackgroundStep launches a background:true step as a daemon and emits
// the same start/ready events. The step's name doubles as the daemon name.
func (r *Runner) startBackgroundStep(
	ctx context.Context,
	spec *Spec,
	step Step,
	registry *DaemonRegistry,
	env []string,
	workDir, homeDir string,
	vars map[string]string,
	binPath string,
	events chan<- SpecEvent,
) error {
	// Emit a step_start so the TUI shows the step entry in the timeline.
	events <- SpecEvent{
		Type: "step_start", Wave: spec.Wave, Spec: spec.Name,
		Phase: "test", Step: step.Name,
		Command: ResolveTemplates(step.Command, vars),
	}
	if err := r.spawnDaemon(ctx, spec, step.Name, step.Command, step.Env, step.Ready, step.Stop, nil, step.LogAssert, step.Timeout, registry, env, workDir, homeDir, vars, binPath, events); err != nil {
		// Surface the failure as a step_done so the TUI marks it failed.
		events <- SpecEvent{
			Type: "step_done", Wave: spec.Wave, Spec: spec.Name,
			Phase: "test", Step: step.Name,
			Command: ResolveTemplates(step.Command, vars),
			Passed:  false,
			Error:   err.Error(),
		}
		return err
	}
	// Background-step success: emit a step_done so the timeline closes out.
	events <- SpecEvent{
		Type: "step_done", Wave: spec.Wave, Spec: spec.Name,
		Phase: "test", Step: step.Name,
		Command: ResolveTemplates(step.Command, vars),
		Passed:  true,
	}
	return nil
}

func (r *Runner) spawnDaemon(
	ctx context.Context,
	spec *Spec,
	name, command string,
	dEnv map[string]string,
	ready *ReadyCheck,
	stop *StopSpec,
	capture map[string]string,
	logAssert []Assertion,
	startTimeout string,
	registry *DaemonRegistry,
	env []string,
	workDir, homeDir string,
	vars map[string]string,
	binPath string,
	events chan<- SpecEvent,
) error {
	events <- SpecEvent{
		Type: "daemon_start", Wave: spec.Wave, Spec: spec.Name, Phase: "daemon",
		Daemon: name, Command: ResolveTemplates(command, vars), Ready: describeReady(ready),
	}
	h, err := StartDaemon(ctx, r.Config, name, command, dEnv, env, workDir, homeDir, vars, binPath, ready, startTimeout)
	if err != nil {
		r.logDaemonEvent(DaemonEventRecord{
			Type: RecordTypeDaemonStart, Wave: spec.Wave, Spec: spec.Name,
			Daemon: name, Passed: false, Error: err.Error(),
		})
		return err
	}
	h.ApplyCapture(capture, vars)
	// Remember stop/log_assert by daemon name for the teardown phase.
	r.recordDaemonTeardownConfig(spec, name, stop, logAssert)
	registry.add(h)
	r.logDaemonEvent(DaemonEventRecord{
		Type: RecordTypeDaemonStart, Wave: spec.Wave, Spec: spec.Name,
		Daemon: name, PID: h.PID, Command: h.Command, Passed: true,
	})
	events <- SpecEvent{
		Type: "daemon_ready", Wave: spec.Wave, Spec: spec.Name, Phase: "daemon",
		Daemon: name, PID: h.PID, Ready: describeReady(ready),
		DurationMs: time.Since(h.StartedAt).Milliseconds(),
	}
	r.logDaemonEvent(DaemonEventRecord{
		Type: RecordTypeDaemonReady, Wave: spec.Wave, Spec: spec.Name,
		Daemon: name, PID: h.PID, Ready: describeReady(ready),
		DurationMs: time.Since(h.StartedAt).Milliseconds(),
		Passed:     true,
	})
	return nil
}

// daemonStopConfig and recordDaemonTeardownConfig persist per-daemon
// teardown configuration so spawnDaemon and stopAllDaemons can stay
// decoupled. Per-spec storage uses a small map on the runner; the runner is
// shared across specs so the map must be keyed by (spec, daemon).
func (r *Runner) recordDaemonTeardownConfig(spec *Spec, name string, stop *StopSpec, logAssert []Assertion) {
	r.teardownMu.Lock()
	if r.teardown == nil {
		r.teardown = map[string]daemonTeardown{}
	}
	r.teardown[teardownKey(spec, name)] = daemonTeardown{stop: stop, logAssert: logAssert}
	r.teardownMu.Unlock()
}

func (r *Runner) daemonStopConfig(spec *Spec, name string) (*StopSpec, []Assertion) {
	r.teardownMu.Lock()
	defer r.teardownMu.Unlock()
	if r.teardown == nil {
		return nil, nil
	}
	td := r.teardown[teardownKey(spec, name)]
	return td.stop, td.logAssert
}

func teardownKey(spec *Spec, name string) string {
	return spec.Wave + "/" + spec.Name + "/" + name
}

type daemonTeardown struct {
	stop      *StopSpec
	logAssert []Assertion
}

func (r *Runner) emitDaemonDied(events chan<- SpecEvent, spec *Spec, h *DaemonHandle) {
	events <- SpecEvent{
		Type: "daemon_died", Wave: spec.Wave, Spec: spec.Name, Phase: "daemon",
		Daemon: h.Name, PID: h.PID, ExitCode: h.exitCode,
		Stdout: tail(h.LogBuf.String(), 2*1024),
		Error:  fmt.Sprintf("daemon %q exited unexpectedly (exit=%d)", h.Name, h.exitCode),
	}
	r.logDaemonEvent(DaemonEventRecord{
		Type: RecordTypeDaemonDied, Wave: spec.Wave, Spec: spec.Name,
		Daemon: h.Name, PID: h.PID, ExitCode: h.exitCode,
		LogTail: tail(h.LogBuf.String(), 2*1024),
		Passed:  false,
		Error:   fmt.Sprintf("daemon %q exited unexpectedly", h.Name),
	})
}

func (r *Runner) emitDaemonStop(events chan<- SpecEvent, spec *Spec, h *DaemonHandle, res DaemonResult, passed bool) {
	errMsg := ""
	if !res.CleanExit {
		errMsg = fmt.Sprintf("daemon %q did not exit within %s of SIG%s; SIGKILL escalated", res.Name, res.Grace, res.Signal)
	} else if res.ExitMismatch {
		errMsg = fmt.Sprintf("daemon %q exited %d, expected %d", res.Name, res.ExitCode, *res.ExpectedExit)
	}
	events <- SpecEvent{
		Type: "daemon_stop", Wave: spec.Wave, Spec: spec.Name, Phase: "daemon",
		Daemon: res.Name, PID: res.PID, Signal: res.Signal,
		ExitCode: res.ExitCode, DurationMs: res.Duration.Milliseconds(),
		Stdout: res.LogTail, Assertions: res.LogAssertions,
		Passed: passed, Error: errMsg,
	}
	r.logDaemonEvent(DaemonEventRecord{
		Type: RecordTypeDaemonStop, Wave: spec.Wave, Spec: spec.Name,
		Daemon: res.Name, PID: res.PID, Signal: res.Signal,
		GraceMs:      res.Grace.Milliseconds(),
		ExitCode:     res.ExitCode,
		DurationMs:   res.Duration.Milliseconds(),
		CleanExit:    res.CleanExit,
		ExitMismatch: res.ExitMismatch,
		ExpectedExit: res.ExpectedExit,
		LogTail:      res.LogTail,
		LogAssertions: res.LogAssertions,
		Passed:       passed,
		Error:        errMsg,
	})
}

func (r *Runner) logDaemonEvent(rec DaemonEventRecord) {
	if r.Logger == nil {
		return
	}
	rec.Timestamp = time.Now()
	r.Logger.LogAny(rec)
}

// stepDoneEvent assembles a step_done SpecEvent from a result.
func stepDoneEvent(spec *Spec, phase string, step Step, result StepResult, assertions []AssertionResult, passed bool) SpecEvent {
	return SpecEvent{
		Type:       "step_done",
		Wave:       spec.Wave,
		Spec:       spec.Name,
		Phase:      phase,
		Step:       step.Name,
		Command:    result.Command,
		Stdout:     result.Stdout,
		Stderr:     result.Stderr,
		ExitCode:   result.ExitCode,
		DurationMs: result.Duration,
		Assertions: assertions,
		Passed:     passed,
	}
}

func (r *Runner) emitSpecEnd(events chan<- SpecEvent, spec *Spec, passed bool, start time.Time, errMsg string) {
	evt := SpecEvent{
		Type: "spec_end", Wave: spec.Wave, Spec: spec.Name,
		Passed: passed, DurationMs: time.Since(start).Milliseconds(), Error: errMsg,
	}
	events <- evt
	if r.Logger != nil {
		r.Logger.LogAny(SpecEndRecord{
			Type: RecordTypeSpecEnd, Wave: spec.Wave, Spec: spec.Name,
			Passed: passed, DurationMs: evt.DurationMs,
		})
	}
}

func (r *Runner) logSpecStart(spec *Spec, workDir, homeDir string, env map[string]string) {
	if r.Logger == nil {
		return
	}
	r.Logger.LogAny(SpecStartRecord{
		Type: RecordTypeSpecStart, Wave: spec.Wave, Spec: spec.Name,
		WorkDir: workDir, HomeDir: homeDir, BinDir: r.BinDir, Env: env,
	})
}

func (r *Runner) logStep(spec *Spec, phase string, step Step, result StepResult, assertions []AssertionResult, passed bool) {
	if r.Logger == nil {
		return
	}
	r.Logger.Log(StepLog{
		Phase:      phase,
		Timestamp:  time.Now(),
		Wave:       spec.Wave,
		Spec:       spec.Name,
		Step:       step.Name,
		Command:    result.Command,
		ExitCode:   result.ExitCode,
		DurationMs: result.Duration,
		Stdout:     result.Stdout,
		Stderr:     result.Stderr,
		Assertions: assertions,
		Passed:     passed,
	})
}

package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// SpecEvent is the typed message the runner emits to consumers. Consumers
// receive: spec_start → step_start/step_done* → spec_end. The events channel
// is closed by RunSpec when the spec finishes.
type SpecEvent struct {
	Type       string // spec_start, step_start, step_done, spec_end
	Wave       string
	Spec       string
	Phase      string // setup, test, cleanup
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
}

// Runner orchestrates spec execution.
type Runner struct {
	Config      Config
	BinDir      string       // directory containing the built binary
	ProjectRoot string       // absolute path to the consumer's repo root
	Logger      *JSONLLogger // optional; nil disables file logging
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

	// Provision the repo.
	fixturesDir := r.Config.FixturesDir
	if !filepath.IsAbs(fixturesDir) {
		fixturesDir = filepath.Join(r.ProjectRoot, fixturesDir)
	}
	switch {
	case spec.Repo.Fixture != "":
		if err := PrepareFixtureRepo(fixturesDir, spec.Repo.Fixture, workDir); err != nil {
			r.emitSpecEnd(events, spec, false, specStart, err.Error())
			return fmt.Errorf("prepare fixture: %w", err)
		}
	case spec.Repo.Helper != "":
		if err := CallRepoHelper(r.Config.RepoHelpers, r.ProjectRoot, spec.Repo.Helper, spec.Repo.HelperArgs, workDir); err != nil {
			r.emitSpecEnd(events, spec, false, specStart, err.Error())
			return fmt.Errorf("repo helper: %w", err)
		}
	default:
		if err := os.MkdirAll(workDir, 0o755); err != nil {
			r.emitSpecEnd(events, spec, false, specStart, err.Error())
			return fmt.Errorf("create work dir: %w", err)
		}
	}

	vars := map[string]string{}

	// Setup phase: any failure aborts the spec.
	for _, step := range spec.Setup {
		if ctx.Err() != nil {
			r.emitSpecEnd(events, spec, false, specStart, "cancelled")
			return ctx.Err()
		}
		events <- SpecEvent{Type: "step_start", Wave: spec.Wave, Spec: spec.Name, Phase: "setup", Step: step.Name, Command: ResolveTemplates(step.Command, vars)}
		result := ExecuteStep(ctx, r.Config, step, workDir, env, vars, binPath)
		stepPassed := result.ExitCode == 0
		evt := stepDoneEvent(spec, "setup", step, result, nil, stepPassed)
		events <- evt
		r.logStep(spec, "setup", step, result, nil, stepPassed)
		if !stepPassed {
			r.emitSpecEnd(events, spec, false, specStart, fmt.Sprintf("setup step %q failed", step.Name))
			return fmt.Errorf("setup step %q failed (exit %d): %s", step.Name, result.ExitCode, result.Stderr)
		}
	}

	// Test phase: assertions are evaluated; failures recorded but the loop continues.
	for _, step := range spec.Steps {
		if ctx.Err() != nil {
			r.emitSpecEnd(events, spec, false, specStart, "cancelled")
			return ctx.Err()
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

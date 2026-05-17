package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// SupportedSpecVersion is the major version of the spec format this runner
// understands. Specs declaring a higher major are refused with a clear error.
const SupportedSpecVersion = 1

// Spec is one E2E scenario loaded from a YAML file.
type Spec struct {
	SpecVersion int            `yaml:"spec_version"`
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Tags        []string       `yaml:"tags"`
	Requires    []string       `yaml:"requires"`
	Repo        RepoSpec       `yaml:"repo"`
	Setup       []Step         `yaml:"setup"`
	Daemons     []Daemon       `yaml:"daemons"`
	Steps       []Step         `yaml:"steps"`
	Cleanup     []CleanupCheck `yaml:"cleanup"`

	// NotesFile optionally overrides the default sidecar notes path.
	// Default: <spec-file-without-ext>.md.
	NotesFile string `yaml:"notes_file"`

	// Order controls sort position within a wave; lower values come first.
	// Unset (0) sorts after all ordered specs, then alphabetically by name.
	Order int `yaml:"order,omitempty"`

	// Set by the loader.
	Wave      string `yaml:"-"`
	FilePath  string `yaml:"-"`
	NotesPath string `yaml:"-"`
}

// RepoSpec selects the repository the spec runs against.
type RepoSpec struct {
	Fixture    string         `yaml:"fixture"`
	Helper     string         `yaml:"helper"`
	HelperArgs map[string]any `yaml:"helper_args"`
}

// Step is one CLI invocation with optional capture and assertions.
//
// When Background is true, the step starts a long-running process under the
// spec's daemon registry and returns immediately after the optional Ready
// check passes. Background steps cannot use Assert/Capture (no stdout at
// point-of-use); they may declare Ready/Stop/LogAssert to control the
// daemon's lifecycle and teardown verification.
type Step struct {
	Name    string            `yaml:"name"`
	Command string            `yaml:"command"`
	Env     map[string]string `yaml:"env"`
	Timeout string            `yaml:"timeout"`
	Capture map[string]string `yaml:"capture"`
	Assert  []Assertion       `yaml:"assert"`

	Background bool        `yaml:"background,omitempty"`
	Ready      *ReadyCheck `yaml:"ready,omitempty"`
	Stop       *StopSpec   `yaml:"stop,omitempty"`
	LogAssert  []Assertion `yaml:"log_assert,omitempty"`
}

// Daemon is a long-running process spawned by the runner and torn down at end
// of spec. Daemons live in their own top-level block (named lifecycle) or
// inline via Step.Background=true.
//
// Phase is one of:
//   - "pre_setup"  — start after repo provisioning, before setup steps.
//   - "post_setup" — (default) start after setup, before test steps.
//
// Ready (optional) gates the runner: it blocks step execution until the check
// passes or the readiness timeout expires. nil Ready means "started == ready".
//
// Stop (optional) controls teardown semantics. Default: signal=TERM, grace=5s,
// expected_exit=0. After grace, the runner SIGKILLs the process group and
// fails the spec with "did not exit within grace".
//
// LogAssert (optional) evaluates assertions against the captured stdout+stderr
// log at teardown — useful for "logged graceful shutdown" style checks.
//
// Capture (optional) extracts a one-shot value from the captured log once the
// daemon is ready, so subsequent steps can reference it via {{ var }}.
type Daemon struct {
	Name         string            `yaml:"name"`
	Command      string            `yaml:"command"`
	Env          map[string]string `yaml:"env,omitempty"`
	Phase        string            `yaml:"phase,omitempty"`
	StartTimeout string            `yaml:"start_timeout,omitempty"`
	Ready        *ReadyCheck       `yaml:"ready,omitempty"`
	Stop         *StopSpec         `yaml:"stop,omitempty"`
	Capture      map[string]string `yaml:"capture,omitempty"`
	LogAssert    []Assertion       `yaml:"log_assert,omitempty"`
}

// ReadyCheck gates daemon startup. Exactly one of TCP / LogContains / Command
// / PIDFile must be set; an empty ReadyCheck means "started == ready".
type ReadyCheck struct {
	TCP         string `yaml:"tcp,omitempty"`
	LogContains string `yaml:"log_contains,omitempty"`
	Command     string `yaml:"command,omitempty"`
	PIDFile     string `yaml:"pid_file,omitempty"`
	Timeout     string `yaml:"timeout,omitempty"`
	Interval    string `yaml:"interval,omitempty"`
}

// StopSpec controls daemon teardown. Zero-valued StopSpec means default:
// signal=TERM, grace=5s, expected_exit=0.
type StopSpec struct {
	Signal       string `yaml:"signal,omitempty"`
	Grace        string `yaml:"grace,omitempty"`
	ExpectedExit *int   `yaml:"expected_exit,omitempty"`
}

// DaemonPhase constants.
const (
	DaemonPhasePreSetup  = "pre_setup"
	DaemonPhasePostSetup = "post_setup"
)

// Assertion is one check on a step's result.
type Assertion struct {
	Type     string `yaml:"type"`
	Path     string `yaml:"path"`
	Expected any    `yaml:"expected"`
	Op       string `yaml:"op"`
	Value    string `yaml:"value"`
	Pattern  string `yaml:"pattern"`
}

// CleanupCheck is a post-test verification.
type CleanupCheck struct {
	AssertNoResidue    bool     `yaml:"assert_no_residue"`
	AssertFilesRemoved []string `yaml:"assert_files_removed"`
}

// StepResult is the outcome of executing one step.
type StepResult struct {
	Name     string
	Command  string
	ExitCode int
	Stdout   string
	Stderr   string
	Duration int64 // milliseconds
	Passed   bool
	Errors   []string
}

// LoadSpec parses one YAML spec file.
func LoadSpec(path string) (*Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read spec %s: %w", path, err)
	}
	var spec Spec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("parse spec %s: %w", path, err)
	}
	if spec.SpecVersion == 0 {
		spec.SpecVersion = SupportedSpecVersion // legacy spec — apply default
	}
	if spec.SpecVersion > SupportedSpecVersion {
		return nil, fmt.Errorf("spec %s: spec_version %d is newer than this runner supports (max %d); upgrade the gotit runner",
			path, spec.SpecVersion, SupportedSpecVersion)
	}
	spec.FilePath = path
	spec.NotesPath = resolveNotesPath(path, spec.NotesFile)
	if err := spec.Validate(); err != nil {
		return nil, fmt.Errorf("spec %s: %w", path, err)
	}
	return &spec, nil
}

// Validate enforces spec-wide invariants that the JSON schema cannot express:
// daemon-name uniqueness, ready-check exactly-one, phase enum, signal enum,
// background-step exclusions. Loaders call this; callers that build specs
// programmatically should call it too.
func (s *Spec) Validate() error {
	names := map[string]string{} // name -> source ("daemon" or "step")
	for i := range s.Daemons {
		d := &s.Daemons[i]
		if d.Name == "" {
			return fmt.Errorf("daemon at index %d: name is required", i)
		}
		if d.Command == "" {
			return fmt.Errorf("daemon %q: command is required", d.Name)
		}
		if prev, ok := names[d.Name]; ok {
			return fmt.Errorf("daemon name %q already used by %s", d.Name, prev)
		}
		names[d.Name] = "daemon"
		switch d.Phase {
		case "", DaemonPhasePreSetup, DaemonPhasePostSetup:
		default:
			return fmt.Errorf("daemon %q: invalid phase %q (must be pre_setup or post_setup)", d.Name, d.Phase)
		}
		if err := validateReadyCheck(d.Ready, "daemon "+d.Name); err != nil {
			return err
		}
		if err := validateStopSpec(d.Stop, "daemon "+d.Name); err != nil {
			return err
		}
	}
	for i := range s.Steps {
		st := &s.Steps[i]
		if !st.Background {
			if st.Ready != nil || st.Stop != nil || len(st.LogAssert) > 0 {
				return fmt.Errorf("step %q: ready/stop/log_assert require background: true", st.Name)
			}
			continue
		}
		if len(st.Assert) > 0 {
			return fmt.Errorf("step %q: background:true forbids assert (stdout is not captured at point-of-use)", st.Name)
		}
		if len(st.Capture) > 0 {
			return fmt.Errorf("step %q: background:true forbids capture (use the daemon block for one-shot capture)", st.Name)
		}
		if st.Name == "" {
			return fmt.Errorf("background step at index %d: name is required", i)
		}
		if prev, ok := names[st.Name]; ok {
			return fmt.Errorf("background step name %q already used by %s", st.Name, prev)
		}
		names[st.Name] = "background step"
		if err := validateReadyCheck(st.Ready, "step "+st.Name); err != nil {
			return err
		}
		if err := validateStopSpec(st.Stop, "step "+st.Name); err != nil {
			return err
		}
	}
	return nil
}

func validateReadyCheck(r *ReadyCheck, ctx string) error {
	if r == nil {
		return nil
	}
	set := 0
	if r.TCP != "" {
		set++
	}
	if r.LogContains != "" {
		set++
	}
	if r.Command != "" {
		set++
	}
	if r.PIDFile != "" {
		set++
	}
	if set > 1 {
		return fmt.Errorf("%s: ready: exactly one of tcp/log_contains/command/pid_file may be set", ctx)
	}
	return nil
}

func validateStopSpec(s *StopSpec, ctx string) error {
	if s == nil || s.Signal == "" {
		return nil
	}
	switch strings.ToUpper(s.Signal) {
	case "TERM", "INT", "HUP", "QUIT":
		return nil
	}
	return fmt.Errorf("%s: stop.signal %q is not one of TERM/INT/HUP/QUIT", ctx, s.Signal)
}

// LoadSpecs walks specsDir and returns every .yaml spec, sorted by
// (wave, order, name). The wave is the first path segment under specsDir.
func LoadSpecs(specsDir string) ([]*Spec, error) {
	var specs []*Spec
	err := filepath.Walk(specsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return nil
		}
		spec, err := LoadSpec(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(specsDir, path)
		parts := strings.SplitN(rel, string(filepath.Separator), 2)
		if len(parts) > 0 {
			spec.Wave = parts[0]
		}
		specs = append(specs, spec)
		return nil
	})
	if err != nil {
		return specs, err
	}
	sort.SliceStable(specs, func(i, j int) bool {
		if specs[i].Wave != specs[j].Wave {
			return specs[i].Wave < specs[j].Wave
		}
		oi, oj := orderKey(specs[i].Order), orderKey(specs[j].Order)
		if oi != oj {
			return oi < oj
		}
		return specs[i].Name < specs[j].Name
	})
	return specs, nil
}

func orderKey(o int) int {
	if o == 0 {
		const maxInt = int(^uint(0) >> 1)
		return maxInt
	}
	return o
}

func resolveNotesPath(specPath, notesFile string) string {
	if notesFile != "" {
		if filepath.IsAbs(notesFile) {
			return notesFile
		}
		return filepath.Join(filepath.Dir(specPath), notesFile)
	}
	ext := filepath.Ext(specPath)
	return strings.TrimSuffix(specPath, ext) + ".md"
}

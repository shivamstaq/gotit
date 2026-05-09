package main

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"gopkg.in/yaml.v3"

	"github.com/shivamstaq/gotit/runner"
	"github.com/shivamstaq/gotit/tui"
)

// runTUI launches the interactive TUI in the current Go module. Walks up
// from CWD to find go.mod, loads tests/e2e/gotit.yaml (or env-var
// overrides), discovers specs, and runs the bubbletea program.
func runTUI(_ []string) int {
	checks := tui.CheckEnvironment()
	if tui.HasHardFailure(checks) {
		fmt.Fprintln(os.Stderr, "gotit: environment check failed:")
		fmt.Fprint(os.Stderr, tui.FormatReport(checks))
		fmt.Fprintln(os.Stderr, "run `gotit doctor` for full details.")
		return 1
	}

	projectRoot, err := findProjectRoot()
	if err != nil {
		return usageError("%v", err)
	}

	cfg, err := loadGotitConfig(projectRoot)
	if err != nil {
		return usageError("%v", err)
	}

	specsDir := cfg.SpecsDir
	if !filepath.IsAbs(specsDir) {
		specsDir = filepath.Join(projectRoot, specsDir)
	}
	specs, err := runner.LoadSpecs(specsDir)
	if err != nil {
		return usageError("load specs from %s: %v", specsDir, err)
	}
	if len(specs) == 0 {
		return usageError("no specs found in %s — run `gotit init` to scaffold one", specsDir)
	}

	featuresFile := cfg.FeaturesFile
	if !filepath.IsAbs(featuresFile) {
		featuresFile = filepath.Join(projectRoot, featuresFile)
	}

	entries := make([]*tui.TestEntry, 0, len(specs))
	for _, s := range specs {
		entry := &tui.TestEntry{Spec: s, State: tui.StatePending, CurrentStep: -1}
		// Pre-flight: evaluate the requirement types the TUI can check itself
		// (feature:* and git). Custom requirements stay unknown until the
		// testdriver runs them — the testdriver emits a skip event in that
		// case, so the spec still reaches a terminal state.
		if reqErr := runner.CheckBuiltinRequirements(s.Requires, projectRoot, featuresFile, cfg.EnvPrefix); reqErr != nil {
			entry.State = tui.StateSkipped
			entry.ReqError = reqErr.Error()
		}
		entries = append(entries, entry)
	}

	model := tui.NewModel(entries, cfg, projectRoot)
	prog := tea.NewProgram(&model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	model.SetProgram(prog)

	defer model.Cleanup()

	if _, err := prog.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "gotit:", err)
		return 1
	}
	return 0
}

// findProjectRoot walks up from CWD until a go.mod is found.
func findProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found in %s or any parent", dir)
		}
		dir = parent
	}
}

// loadGotitConfig reads gotit.yaml (or env-var overrides) and returns a
// tui.GotitConfig. Search order:
//  1. <cwd>/gotit.yaml          (a suite living in its own subdirectory)
//  2. <projectRoot>/tests/e2e/gotit.yaml  (the conventional location)
// Errors when required fields are missing.
func loadGotitConfig(projectRoot string) (tui.GotitConfig, error) {
	cfg := tui.GotitConfig{}
	cwd, _ := os.Getwd()

	cfgPath := filepath.Join(cwd, "gotit.yaml")
	if _, err := os.Stat(cfgPath); err != nil {
		cfgPath = filepath.Join(projectRoot, "tests", "e2e", "gotit.yaml")
	}
	if data, err := os.ReadFile(cfgPath); err == nil {
		raw := struct {
			BinaryName   string `yaml:"binary_name"`
			BuildPath    string `yaml:"build_path"`
			EnvPrefix    string `yaml:"env_prefix"`
			SpecsDir     string `yaml:"specs_dir"`
			FixturesDir  string `yaml:"fixtures_dir"`
			FeaturesFile string `yaml:"features_file"`
			ResultsDir   string `yaml:"results_dir"`
		}{}
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return cfg, fmt.Errorf("parse %s: %w", cfgPath, err)
		}
		cfg.BinaryName = raw.BinaryName
		cfg.BuildPath = raw.BuildPath
		cfg.EnvPrefix = raw.EnvPrefix
		cfg.SpecsDir = raw.SpecsDir
		cfg.FixturesDir = raw.FixturesDir
		cfg.FeaturesFile = raw.FeaturesFile
		cfg.ResultsDir = raw.ResultsDir
	} else if !os.IsNotExist(err) {
		return cfg, fmt.Errorf("read %s: %w", cfgPath, err)
	}

	tui.LoadConfigFromEnv(&cfg)
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return cfg, fmt.Errorf("%w (set fields in %s or via GOTIT_BINARY/GOTIT_BUILD_PATH/GOTIT_ENV_PREFIX)", err, cfgPath)
	}
	return cfg, nil
}

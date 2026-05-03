// Package testdriver is the drop-in `go test` driver for gotit.
//
// In a consumer's tests/e2e/runner_test.go, a single call to Run wires the
// runner into Go's testing framework: it builds the CLI binary in TestMain,
// walks the specs directory, dispatches each spec as a parallel subtest,
// drains events into t.Logf/t.Errorf, and registers temp-dir cleanup.
package testdriver

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/shivamstaq/gotit/runner"
)

// Run is the all-in-one entry point. Call it from a single TestE2E function:
//
//	func TestE2E(t *testing.T) {
//	    testdriver.Run(t, runner.Config{ BinaryName: "kubectl", BuildPath: "./cmd/kubectl", EnvPrefix: "KUBE", … })
//	}
//
// Run builds the CLI binary once (shared across subtests), walks the configured
// specs directory, and dispatches each spec via t.Run + t.Parallel. Failures
// are reported through t.Errorf with full assertion context; passing specs log
// each step's command and duration via t.Logf.
func Run(t *testing.T, cfg runner.Config) {
	t.Helper()

	projectRoot, err := findProjectRoot()
	if err != nil {
		t.Fatalf("find project root: %v", err)
	}
	// Expose to assertions that resolve project-relative paths (golden_file).
	prefix := cfg.EnvPrefix
	if prefix == "" {
		t.Fatal("Config.EnvPrefix is required")
	}
	rootKey := prefix + "_E2E_PROJECT_ROOT"
	t.Setenv(rootKey, projectRoot)

	binDir, err := os.MkdirTemp("", "gotit-bin-*")
	if err != nil {
		t.Fatalf("create bin dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(binDir) })

	r, err := runner.New(cfg, binDir, projectRoot)
	if err != nil {
		t.Fatalf("runner: %v", err)
	}

	if err := r.BuildBinary(t.Context()); err != nil {
		t.Fatalf("build binary: %v", err)
	}

	resultsDir := cfg.ResultsDir
	if !filepath.IsAbs(resultsDir) {
		resultsDir = filepath.Join(projectRoot, r.Config.ResultsDir)
	}
	logger, err := runner.NewJSONLLogger(resultsDir)
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	r.Logger = logger

	specsDir := cfg.SpecsDir
	if !filepath.IsAbs(specsDir) {
		specsDir = filepath.Join(projectRoot, r.Config.SpecsDir)
	}
	specs, err := runner.LoadSpecs(specsDir)
	if err != nil {
		t.Fatalf("load specs: %v", err)
	}
	if len(specs) == 0 {
		t.Fatalf("no specs found in %s", specsDir)
	}

	featuresFile := r.Config.FeaturesFile
	if !filepath.IsAbs(featuresFile) {
		featuresFile = filepath.Join(projectRoot, r.Config.FeaturesFile)
	}

	for _, spec := range specs {
		spec := spec
		testName := spec.Wave + "/" + spec.Name
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			if err := runner.CheckRequirements(spec.Requires, projectRoot, r.Config.RequirementCheckers, featuresFile, prefix); err != nil {
				t.Skip(err)
			}

			events := make(chan runner.SpecEvent, 100)
			var homeDir string

			done := make(chan struct{})
			go func() {
				defer close(done)
				for ev := range events {
					switch ev.Type {
					case "spec_start":
						homeDir = ev.HomeDir
					case "step_done":
						t.Logf("[%s] %s (exit=%d, %dms)", ev.Step, ev.Command, ev.ExitCode, ev.DurationMs)
						if !ev.Passed {
							for _, a := range ev.Assertions {
								if !a.Passed {
									t.Errorf("[%s] assertion (%s): %s", ev.Step, a.Type, a.Error)
								}
							}
						}
					}
				}
			}()

			err := r.RunSpec(t.Context(), spec, events)
			<-done

			// Honor GOTIT_KEEP_HOMEDIR=1 to preserve temp HOME after test exit.
			// The TUI sets this so it can shell into the spec's work dir; the
			// TUI is responsible for reaping the dir when the user is done.
			if homeDir != "" && os.Getenv("GOTIT_KEEP_HOMEDIR") != "1" {
				t.Cleanup(func() { _ = os.RemoveAll(homeDir) })
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

// findProjectRoot walks up from the caller until a go.mod is found.
func findProjectRoot() (string, error) {
	_, filename, _, ok := runtime.Caller(1)
	if !ok {
		return "", fmt.Errorf("cannot determine caller location")
	}
	dir := filepath.Dir(filename)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found in any parent of %s", filename)
		}
		dir = parent
	}
}


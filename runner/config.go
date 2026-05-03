// Package runner is the gotit E2E test harness for Go CLI binaries.
//
// Consumers configure the runner with their CLI's binary name and build path,
// register repo helpers and requirement checkers, and dispatch via the
// testdriver subpackage. See https://github.com/shivamstaq/gotit/blob/main/SPEC.md.
package runner

import (
	"strings"
	"time"
)

// Config wires the runner to a target Go CLI project. All fields are optional
// except BinaryName, BuildPath, and EnvPrefix; defaults apply to the rest.
type Config struct {
	// BinaryName is the bare name of the CLI under test (e.g. "kubectl").
	// In specs, the first command token matching this name is rewritten to
	// the absolute path of the binary built by the runner.
	BinaryName string

	// BuildPath is the Go package path passed to `go build -o <bin> <BuildPath>`.
	// Typically "./cmd/<binary>".
	BuildPath string

	// EnvPrefix is the uppercase prefix used to derive isolation env vars:
	//   <PREFIX>_HOME             — overrides the CLI's home dir.
	//   <PREFIX>_E2E_PROJECT_ROOT — absolute path to the consumer's repo root.
	//   <PREFIX>_E2E_FEATURES     — comma-separated feature-flag override.
	// e.g. EnvPrefix "KUBE" yields KUBE_HOME, KUBE_E2E_PROJECT_ROOT, KUBE_E2E_FEATURES.
	EnvPrefix string

	// SpecsDir is the directory tree containing YAML spec files, relative to
	// the project root. Default: "tests/e2e/specs".
	SpecsDir string

	// FixturesDir is the directory holding static repo fixtures, relative to
	// the project root. Default: "tests/e2e/testdata/repos".
	FixturesDir string

	// FeaturesFile is the path to the feature-flag allowlist YAML, relative
	// to the project root. Default: "tests/e2e/features.yaml".
	FeaturesFile string

	// ResultsDir is the directory where JSONL run logs are written, relative
	// to the project root. Default: "tests/e2e/results".
	ResultsDir string

	// DefaultTimeout caps each step's runtime when the spec doesn't specify.
	// Default: 30 seconds.
	DefaultTimeout time.Duration

	// RepoHelpers maps spec `repo.helper:` names to programmatic builders.
	RepoHelpers map[string]RepoHelper

	// RequirementCheckers maps spec `requires:` entries (other than feature:* and "git")
	// to runtime probes. Returning nil means the requirement is satisfied; returning
	// an error causes the spec to be skipped with that error as the message.
	RequirementCheckers map[string]RequirementChecker

	// Assertions extends the built-in assertion type set with project-local types.
	// Custom type names must use the "x-" prefix; this is enforced by the
	// JSON schema and by the runtime when a spec is loaded.
	Assertions map[string]AssertionFunc
}

// RepoHelper builds a target repository under workDir for a single spec.
// projectRoot is the consumer's repo root; args carries the spec's helper_args.
type RepoHelper func(projectRoot, workDir string, args map[string]any) error

// RequirementChecker probes whether a runtime prerequisite is available.
// Return nil to satisfy; return an error to skip dependent specs.
type RequirementChecker func(projectRoot string) error

// AssertionFunc evaluates one assertion against a step's output.
type AssertionFunc func(a Assertion, r StepResult, vars map[string]string) error

// withDefaults returns a copy of cfg with empty fields filled in.
func (c Config) withDefaults() Config {
	if c.SpecsDir == "" {
		c.SpecsDir = "tests/e2e/specs"
	}
	if c.FixturesDir == "" {
		c.FixturesDir = "tests/e2e/testdata/repos"
	}
	if c.FeaturesFile == "" {
		c.FeaturesFile = "tests/e2e/features.yaml"
	}
	if c.ResultsDir == "" {
		c.ResultsDir = "tests/e2e/results"
	}
	if c.DefaultTimeout == 0 {
		c.DefaultTimeout = 30 * time.Second
	}
	c.EnvPrefix = strings.ToUpper(c.EnvPrefix)
	return c
}

// validate reports configuration errors that would prevent the runner from
// starting (missing required fields).
func (c Config) validate() error {
	var missing []string
	if c.BinaryName == "" {
		missing = append(missing, "BinaryName")
	}
	if c.BuildPath == "" {
		missing = append(missing, "BuildPath")
	}
	if c.EnvPrefix == "" {
		missing = append(missing, "EnvPrefix")
	}
	if len(missing) > 0 {
		return &ConfigError{Missing: missing}
	}
	return nil
}

// ConfigError reports missing required Config fields.
type ConfigError struct{ Missing []string }

func (e *ConfigError) Error() string {
	return "gotit: Config missing required fields: " + strings.Join(e.Missing, ", ")
}

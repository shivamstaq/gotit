// Package e2e is the gotit kit's self-test consumer.
//
// It targets the tiny CLI under examples/demo-cli/cmd/demo and exercises every
// runner code path — passing/failing/feature-gated specs, a static fixture,
// a programmatic helper, a custom requirement checker, and a custom assertion
// type registered via Config.
package e2e

import (
	"fmt"
	"strings"
	"testing"

	"github.com/shivamstaq/gotit/runner"
	"github.com/shivamstaq/gotit/runner/testdriver"
	"github.com/shivamstaq/gotit/examples/demo-suite/helpers"
)

func TestE2E(t *testing.T) {
	testdriver.Run(t, runner.Config{
		BinaryName:   "demo",
		BuildPath:    "./examples/demo-cli/cmd/demo",
		EnvPrefix:    "DEMO",
		SpecsDir:     "examples/demo-suite/specs",
		FixturesDir:  "examples/demo-suite/testdata/repos",
		FeaturesFile: "examples/demo-suite/features.yaml",
		ResultsDir:   "examples/demo-suite/results",

		RepoHelpers: map[string]runner.RepoHelper{
			"two-commit-rename": helpers.TwoCommitRename,
		},
		RequirementCheckers: map[string]runner.RequirementChecker{
			"demo-helper-marker": helpers.CheckDemoHelperMarker,
			"curl":               helpers.CheckCurl,
		},
		Assertions: map[string]runner.AssertionFunc{
			"x-output-lines-gte": assertOutputLinesGte,
		},
	})
}

// assertOutputLinesGte demonstrates a project-local assertion type. It passes
// when stdout has at least `expected` non-empty lines.
func assertOutputLinesGte(a runner.Assertion, r runner.StepResult, _ map[string]string) runner.AssertionResult {
	expected, ok := a.Expected.(int)
	if !ok {
		// YAML decodes integers as int by default but allow int64/float64 too.
		switch v := a.Expected.(type) {
		case int64:
			expected = int(v)
		case float64:
			expected = int(v)
		default:
			return runner.AssertionResult{
				Passed:  false,
				Summary: "x-output-lines-gte",
				Error:   fmt.Sprintf("x-output-lines-gte: expected must be an integer, got %T", a.Expected),
			}
		}
	}
	count := 0
	for _, line := range strings.Split(r.Stdout, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	summary := fmt.Sprintf("non-empty lines >= %d", expected)
	got := fmt.Sprintf("%d", count)
	want := fmt.Sprintf("%d", expected)
	if count < expected {
		return runner.AssertionResult{
			Passed:  false,
			Summary: summary,
			Want:    want,
			Got:     got,
			Error:   fmt.Sprintf("x-output-lines-gte: got %d non-empty lines, want >= %d", count, expected),
		}
	}
	return runner.AssertionResult{Passed: true, Summary: summary, Want: want, Got: got}
}

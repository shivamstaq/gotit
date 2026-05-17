// Package helpers shows how a consumer registers project-local repo builders
// and requirement checkers. The two functions here are imported by the
// runner_test.go in the parent directory; in your own project the equivalent
// would live under tests/e2e/helpers/.
package helpers

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/shivamstaq/gotit/runner"
)

// TwoCommitRename builds a repo with two commits where a file is renamed in
// the second commit. Used by specs that exercise rename-aware behavior.
func TwoCommitRename(_, workDir string, _ map[string]any) error {
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return err
	}
	if err := runner.GitInit(workDir); err != nil {
		return err
	}
	if err := runner.WriteFile(filepath.Join(workDir, "old_name.txt"), "hello\n"); err != nil {
		return err
	}
	if err := runner.GitCommitAll(workDir, "initial: old_name.txt"); err != nil {
		return err
	}
	// Rename via git mv to preserve history.
	if err := runner.RunGit(workDir, "mv", "old_name.txt", "new_name.txt"); err != nil {
		return err
	}
	return runner.GitCommitAll(workDir, "rename: old_name.txt -> new_name.txt")
}

// CheckDemoHelperMarker is a placeholder requirement checker. In a real
// project this would verify a binary on PATH, an env var, an API key, etc.
// Here it simply checks that the projectRoot exists, so it always passes —
// the goal is to demonstrate registration, not gating.
func CheckDemoHelperMarker(projectRoot string) error {
	if _, err := os.Stat(projectRoot); err != nil {
		return fmt.Errorf("project root %q not accessible: %w", projectRoot, err)
	}
	return nil
}

// CheckCurl ensures curl is on PATH; daemon specs use it as their HTTP client.
func CheckCurl(_ string) error {
	if _, err := exec.LookPath("curl"); err != nil {
		return fmt.Errorf("curl not found on PATH: %w", err)
	}
	return nil
}

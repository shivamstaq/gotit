package runner

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// PrepareFixtureRepo copies a fixture from fixturesDir/<fixture>/ into
// workDir, runs `git init -b main`, and creates the initial commit.
func PrepareFixtureRepo(fixturesDir, fixture, workDir string) error {
	src := filepath.Join(fixturesDir, fixture)
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("fixture %q not found at %s", fixture, src)
	}
	if err := CopyDir(src, workDir); err != nil {
		return err
	}
	if err := GitInit(workDir); err != nil {
		return err
	}
	return GitCommitAll(workDir, "initial fixture commit")
}

// CallRepoHelper dispatches by name into the registered helper map.
func CallRepoHelper(helpers map[string]RepoHelper, projectRoot, name string, args map[string]any, workDir string) error {
	helper, ok := helpers[name]
	if !ok {
		return fmt.Errorf("unknown repo helper %q (available: %v)", name, repoHelperNames(helpers))
	}
	return helper(projectRoot, workDir, args)
}

func repoHelperNames(helpers map[string]RepoHelper) []string {
	names := make([]string, 0, len(helpers))
	for k := range helpers {
		names = append(names, k)
	}
	return names
}

// CopyDir copies src to dst recursively. Uses `cp -a` for permissions/links.
func CopyDir(src, dst string) error {
	cmd := exec.Command("cp", "-a", src, dst)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("copy %s -> %s: %w\n%s", src, dst, err, out)
	}
	return nil
}

// GitInit initializes a git repository on branch main.
func GitInit(dir string) error {
	return RunGit(dir, "init", "-b", "main")
}

// GitCommitAll stages everything and commits. Allows empty commits so callers
// can mark milestones in repos with no working changes.
func GitCommitAll(dir, msg string) error {
	if err := RunGit(dir, "add", "-A"); err != nil {
		return err
	}
	return RunGit(dir, "commit", "-m", msg, "--allow-empty")
}

// RunGit executes a git subcommand in dir with deterministic env.
func RunGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=gotit Test",
		"GIT_AUTHOR_EMAIL=test@gotit.dev",
		"GIT_COMMITTER_NAME=gotit Test",
		"GIT_COMMITTER_EMAIL=test@gotit.dev",
		"GIT_CONFIG_NOSYSTEM=1",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s in %s: %w\n%s", strings.Join(args, " "), dir, err, out)
	}
	return nil
}

// WriteFile writes content to path, creating parent directories as needed.
func WriteFile(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

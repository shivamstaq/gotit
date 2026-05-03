package runner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ExecuteStep runs one CLI command and returns the StepResult.
// Templates in step.Command are resolved against vars before execution.
// If the first command token equals cfg.BinaryName, it is rewritten to binPath.
func ExecuteStep(ctx context.Context, cfg Config, step Step, workDir string, env []string, vars map[string]string, binPath string) StepResult {
	cmdLine := ResolveTemplates(step.Command, vars)
	parts := SplitCommand(cmdLine)
	if len(parts) == 0 {
		return StepResult{Name: step.Name, ExitCode: -1, Errors: []string{"empty command"}}
	}

	if parts[0] == cfg.BinaryName {
		parts[0] = binPath
	}

	timeout := cfg.DefaultTimeout
	if step.Timeout != "" {
		if d, err := time.ParseDuration(step.Timeout); err == nil {
			timeout = d
		}
	}

	stepCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(stepCtx, parts[0], parts[1:]...)
	cmd.Dir = workDir
	cmd.Env = append(env, EnvMapToSlice(step.Env)...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start).Milliseconds()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	return StepResult{
		Name:     step.Name,
		Command:  cmdLine,
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: duration,
		Passed:   true,
	}
}

// BuildEnv assembles the isolation environment for a single spec.
//
// Variables exported:
//   HOME, XDG_CONFIG_HOME (sandboxed)
//   <PREFIX>_HOME, <PREFIX>_E2E_PROJECT_ROOT
//   PATH (binDir prepended)
//   GIT_AUTHOR_*, GIT_COMMITTER_*, GIT_CONFIG_NOSYSTEM=1 (deterministic git)
//   TERM=dumb (disable color/paging)
func BuildEnv(cfg Config, homeDir, binDir, projectRoot string) []string {
	prefix := strings.ToUpper(cfg.EnvPrefix)
	homeKey := prefix + "_HOME"
	rootKey := prefix + "_E2E_PROJECT_ROOT"
	return []string{
		"HOME=" + homeDir,
		homeKey + "=" + filepath.Join(homeDir, "."+strings.ToLower(cfg.BinaryName)),
		"XDG_CONFIG_HOME=" + filepath.Join(homeDir, ".config"),
		"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		rootKey + "=" + projectRoot,
		"GIT_AUTHOR_NAME=gotit Test",
		"GIT_AUTHOR_EMAIL=test@gotit.dev",
		"GIT_COMMITTER_NAME=gotit Test",
		"GIT_COMMITTER_EMAIL=test@gotit.dev",
		"GIT_CONFIG_NOSYSTEM=1",
		"TERM=dumb",
	}
}

// SplitCommand tokenizes a command line, respecting single and double quotes.
// Quoted empty strings are preserved as separate arguments.
func SplitCommand(cmdLine string) []string {
	var parts []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	wasQuoted := false

	for _, r := range cmdLine {
		switch {
		case r == '\'' && !inDouble:
			inSingle = !inSingle
			wasQuoted = true
		case r == '"' && !inSingle:
			inDouble = !inDouble
			wasQuoted = true
		case r == ' ' && !inSingle && !inDouble:
			if current.Len() > 0 || wasQuoted {
				parts = append(parts, current.String())
				current.Reset()
				wasQuoted = false
			}
		default:
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 || wasQuoted {
		parts = append(parts, current.String())
	}
	return parts
}

// EnvMapToSlice converts a map to "KEY=VALUE" strings.
func EnvMapToSlice(m map[string]string) []string {
	s := make([]string, 0, len(m))
	for k, v := range m {
		s = append(s, k+"="+v)
	}
	return s
}

// EnvSliceToMap converts "KEY=VALUE" strings to a map.
func EnvSliceToMap(s []string) map[string]string {
	m := make(map[string]string, len(s))
	for _, entry := range s {
		if k, v, ok := strings.Cut(entry, "="); ok {
			m[k] = v
		}
	}
	return m
}

// findFirstFile searches for path under several candidate roots and returns
// the first that exists. Used by golden-file resolution.
func findFirstFile(path string, roots ...string) (string, error) {
	if filepath.IsAbs(path) {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
		return "", fmt.Errorf("absolute path %q not found", path)
	}
	for _, root := range roots {
		if root == "" {
			continue
		}
		candidate := filepath.Join(root, path)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("relative path %q not found under any root", path)
}

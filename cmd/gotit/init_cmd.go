package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shivamstaq/gotit/tui"
)

// runInit scaffolds tests/e2e/ in the current Go module — the human-friendly
// counterpart of the gotit-bootstrap skill. It writes runner_test.go,
// features.yaml, gotit.yaml, a smoke spec, .gitignore lines, and an
// AGENTS.md snippet. Idempotent: existing files are left untouched.
func runInit(args []string) int {
	fs := flag.NewFlagSet("gotit init", flag.ContinueOnError)
	silenceFlagOutput(fs)
	binaryName := fs.String("binary", "", "binary name (default: detected from cmd/<name>/)")
	envPrefix := fs.String("prefix", "", "env prefix for isolation vars (default: derived from binary name)")
	buildPath := fs.String("build", "", "go build path (default: ./cmd/<binary>)")
	if err := fs.Parse(args); err != nil {
		return usageError("%v", err)
	}

	projectRoot, err := findProjectRoot()
	if err != nil {
		return usageError("%v", err)
	}

	bin, build, prefix, err := resolveScaffoldingDefaults(projectRoot, *binaryName, *buildPath, *envPrefix)
	if err != nil {
		return usageError("%v", err)
	}

	cfg := tui.GotitConfig{
		BinaryName: bin,
		BuildPath:  build,
		EnvPrefix:  prefix,
	}
	cfg.ApplyDefaults()

	fmt.Println("Scaffolding gotit E2E suite:")
	fmt.Println("  binary_name:", cfg.BinaryName)
	fmt.Println("  build_path: ", cfg.BuildPath)
	fmt.Println("  env_prefix: ", cfg.EnvPrefix)
	fmt.Println("  project root:", projectRoot)
	fmt.Println()

	created, skipped := 0, 0
	mark := func(path string, wrote bool) {
		if wrote {
			fmt.Println("  +", path)
			created++
		} else {
			fmt.Println("  =", path, "(exists)")
			skipped++
		}
	}

	if w, err := writeIfMissing(filepath.Join(projectRoot, "tests/e2e/gotit.yaml"), gotitYamlContent(cfg)); err != nil {
		return errorExit(err)
	} else {
		mark("tests/e2e/gotit.yaml", w)
	}

	if w, err := writeIfMissing(filepath.Join(projectRoot, "tests/e2e/runner_test.go"), runnerTestGoContent(cfg)); err != nil {
		return errorExit(err)
	} else {
		mark("tests/e2e/runner_test.go", w)
	}

	if w, err := writeIfMissing(filepath.Join(projectRoot, "tests/e2e/features.yaml"), featuresYamlContent(cfg)); err != nil {
		return errorExit(err)
	} else {
		mark("tests/e2e/features.yaml", w)
	}

	if w, err := writeIfMissing(filepath.Join(projectRoot, "tests/e2e/specs/wave1/smoke.yaml"), smokeSpecContent(cfg)); err != nil {
		return errorExit(err)
	} else {
		mark("tests/e2e/specs/wave1/smoke.yaml", w)
	}

	if appended, err := appendOnce(filepath.Join(projectRoot, ".gitignore"), "tests/e2e/results/", "# gotit run logs\ntests/e2e/results/\n"); err != nil {
		return errorExit(err)
	} else if appended {
		mark(".gitignore", true)
	} else {
		mark(".gitignore", false)
	}

	fmt.Printf("\n%d created, %d already existed.\n\n", created, skipped)
	fmt.Println("Next steps:")
	fmt.Println("  1) `go get github.com/shivamstaq/gotit@latest`  (if not already a dependency)")
	fmt.Println("  2) `gotit run wave1`  (or just `gotit` for the TUI)")
	return 0
}

func errorExit(err error) int {
	fmt.Fprintln(os.Stderr, "gotit init:", err)
	return 1
}

// resolveScaffoldingDefaults picks sensible defaults when flags are unset.
// BinaryName: first directory under <root>/cmd/. BuildPath: ./cmd/<binary>.
// EnvPrefix: uppercase binary name with non-alnum mapped to _.
func resolveScaffoldingDefaults(projectRoot, binary, build, prefix string) (string, string, string, error) {
	if binary == "" {
		entries, err := os.ReadDir(filepath.Join(projectRoot, "cmd"))
		if err != nil || len(entries) == 0 {
			return "", "", "", fmt.Errorf("no cmd/ directory found and -binary not given; pass -binary=<name>")
		}
		for _, e := range entries {
			if e.IsDir() {
				binary = e.Name()
				break
			}
		}
		if binary == "" {
			return "", "", "", fmt.Errorf("cmd/ exists but is empty; pass -binary=<name>")
		}
	}
	if build == "" {
		build = "./cmd/" + binary
	}
	if prefix == "" {
		prefix = derivePrefix(binary)
	}
	return binary, build, prefix, nil
}

func derivePrefix(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 32)
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// writeIfMissing creates a file with the given content if and only if it
// doesn't already exist. Returns whether a write happened.
func writeIfMissing(path, content string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return true, nil
}

// appendOnce appends the snippet to path only if marker is not already
// present in the file. Returns whether an append happened.
func appendOnce(path, marker, snippet string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if strings.Contains(string(data), marker) {
		return false, nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return false, err
	}
	defer f.Close()
	prefix := ""
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		prefix = "\n"
	}
	_, err = f.WriteString(prefix + snippet)
	return err == nil, err
}

// --- file content templates (kept inline so cmd/gotit has no embed deps) ---

func gotitYamlContent(cfg tui.GotitConfig) string {
	return fmt.Sprintf(`# gotit TUI per-project config.
# Schema: https://github.com/shivamstaq/gotit/raw/main/schema/gotit-config.schema.json

binary_name: %s
build_path: %s
env_prefix: %s

# Optional — defaults shown.
# specs_dir: tests/e2e/specs
# fixtures_dir: tests/e2e/testdata/repos
# features_file: tests/e2e/features.yaml
# results_dir: tests/e2e/results
`, cfg.BinaryName, cfg.BuildPath, cfg.EnvPrefix)
}

func runnerTestGoContent(cfg tui.GotitConfig) string {
	return fmt.Sprintf(`package e2e

import (
	"testing"

	"github.com/shivamstaq/gotit/runner"
	"github.com/shivamstaq/gotit/runner/testdriver"
)

// TestE2E runs every YAML spec under tests/e2e/specs as a parallel subtest.
// Filter at the CLI: ` + "`go test ./tests/e2e/ -run \"TestE2E/wave1\"`" + ` or use ` + "`gotit run wave1`" + `.
func TestE2E(t *testing.T) {
	testdriver.Run(t, runner.Config{
		BinaryName: %q,
		BuildPath:  %q,
		EnvPrefix:  %q,

		// Register repo helpers, requirement checkers, and custom assertions here:
		// RepoHelpers:         map[string]runner.RepoHelper{ "my-helper": helpers.MyHelper },
		// RequirementCheckers: map[string]runner.RequirementChecker{ "my-tool": helpers.CheckMyTool },
		// Assertions:          map[string]runner.AssertionFunc{ "x-my-assert": helpers.MyAssertion },
	})
}
`, cfg.BinaryName, cfg.BuildPath, cfg.EnvPrefix)
}

func featuresYamlContent(cfg tui.GotitConfig) string {
	return fmt.Sprintf(`# gotit feature-flag allowlist.
#
# Specs that declare ` + "`requires: [feature:<name>]`" + ` are skipped unless the flag
# is listed here as ` + "`true`" + ` (or enabled at run time via %s_E2E_FEATURES).
#
# Schema: https://github.com/shivamstaq/gotit/blob/main/schema/features.schema.json

enabled:
  # example-feature: false
`, cfg.EnvPrefix)
}

func smokeSpecContent(cfg tui.GotitConfig) string {
	return fmt.Sprintf(`# yaml-language-server: $schema=https://github.com/shivamstaq/gotit/raw/main/schema/spec.schema.json
spec_version: 1
name: smoke-version
description: %s prints something on bare invocation
tags: [wave1, smoke]
order: 10

steps:
  - name: smoke
    command: %s
    assert:
      - type: exit_code
        expected: 0
`, cfg.BinaryName, cfg.BinaryName)
}

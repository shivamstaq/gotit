package testdriver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shivamstaq/gotit/runner"
)

// TestFindProjectRootFromCwd is the regression test for the "imported as a
// module" bug: findProjectRoot must anchor on cwd, not on this file's
// runtime.Caller path (which lives in $GOMODCACHE for external consumers).
func TestFindProjectRootFromCwd(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	nested := filepath.Join(root, "pkg", "deep")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	// Resolve any symlinks (e.g. /tmp → /private/tmp on macOS) so the
	// comparison below uses the canonical form findProjectRoot returns.
	wantRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("evalsymlinks root: %v", err)
	}

	for _, start := range []string{root, nested} {
		t.Run(start, func(t *testing.T) {
			t.Chdir(start)
			got, err := findProjectRoot()
			if err != nil {
				t.Fatalf("findProjectRoot: %v", err)
			}
			gotResolved, err := filepath.EvalSymlinks(got)
			if err != nil {
				t.Fatalf("evalsymlinks got: %v", err)
			}
			if gotResolved != wantRoot {
				t.Errorf("findProjectRoot = %q, want %q", gotResolved, wantRoot)
			}
		})
	}
}

// TestFindProjectRootNoGoMod confirms the error mentions the cwd path so a
// failing consumer can debug from the message alone.
func TestFindProjectRootNoGoMod(t *testing.T) {
	dir := t.TempDir()
	// Walk up from dir; if any ancestor already has a go.mod (rare on /tmp,
	// but possible in non-standard CI sandboxes) skip — the test premise
	// doesn't hold.
	for d := dir; ; {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			t.Skipf("ancestor %s already contains go.mod; cannot exercise no-go.mod path", d)
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}

	t.Chdir(dir)
	_, err := findProjectRoot()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "go.mod not found") {
		t.Errorf("error = %q, want containing %q", err.Error(), "go.mod not found")
	}
}

// TestRunSkipsMissingSpecsDir is the regression test for the bootstrapping
// gap: a missing specs dir must not be a fatal — it's "no work to do",
// same shape as `go test` finding no _test.go files.
func TestRunSkipsMissingSpecsDir(t *testing.T) {
	root := makeMinimalProject(t)
	cfg := runner.Config{
		BinaryName:  "tinybin",
		BuildPath:   "./cmd/tinybin",
		EnvPrefix:   "TINY",
		ProjectRoot: root,
		SpecsDir:    filepath.Join(root, "tests", "e2e", "does-not-exist"),
		ResultsDir:  filepath.Join(root, "results"),
	}
	Run(t, cfg)
	if t.Failed() {
		t.Fatal("Run failed on missing specs dir; expected soft skip")
	}
}

// TestRunSkipsEmptySpecsDir mirrors the above for the existed-but-empty case.
func TestRunSkipsEmptySpecsDir(t *testing.T) {
	root := makeMinimalProject(t)
	emptySpecs := filepath.Join(root, "tests", "e2e", "specs")
	if err := os.MkdirAll(emptySpecs, 0o755); err != nil {
		t.Fatalf("mkdir empty specs: %v", err)
	}
	cfg := runner.Config{
		BinaryName:  "tinybin",
		BuildPath:   "./cmd/tinybin",
		EnvPrefix:   "TINY",
		ProjectRoot: root,
		SpecsDir:    emptySpecs,
		ResultsDir:  filepath.Join(root, "results"),
	}
	Run(t, cfg)
	if t.Failed() {
		t.Fatal("Run failed on empty specs dir; expected soft skip")
	}
}

// makeMinimalProject writes a self-contained Go module with a buildable
// binary at ./cmd/tinybin into a temp dir, returning the dir.
func makeMinimalProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module tiny.test\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	cmdDir := filepath.Join(root, "cmd", "tinybin")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatalf("mkdir cmd: %v", err)
	}
	main := "package main\n\nfunc main() {}\n"
	if err := os.WriteFile(filepath.Join(cmdDir, "main.go"), []byte(main), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	return root
}

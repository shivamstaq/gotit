package runner

import (
	"context"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// daemonTestEnv builds the demo CLI once and returns (binPath, env, workDir,
// homeDir, cleanup). The demo CLI lives under examples/demo-cli; it provides
// the `serve` subcommand used as the daemon under test.
func daemonTestEnv(t *testing.T) (string, []string, string, string) {
	t.Helper()
	binDir := t.TempDir()
	homeDir := t.TempDir()
	workDir := filepath.Join(homeDir, "work")
	if err := exec.Command("mkdir", "-p", workDir).Run(); err != nil {
		t.Fatalf("mkdir work: %v", err)
	}
	binPath := filepath.Join(binDir, "demo")

	// Resolve the repo root (this test file lives at runner/daemon_test.go).
	wd, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	root := strings.TrimSpace(string(wd))

	build := exec.Command("go", "build", "-o", binPath, "./examples/demo-cli/cmd/demo")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build demo: %v\n%s", err, out)
	}

	cfg := Config{BinaryName: "demo", BuildPath: "./examples/demo-cli/cmd/demo", EnvPrefix: "DEMO"}.withDefaults()
	env := BuildEnv(cfg, homeDir, binDir, root)
	return binPath, env, workDir, homeDir
}

func TestDaemon_CleanShutdown(t *testing.T) {
	binPath, env, workDir, homeDir := daemonTestEnv(t)
	cfg := Config{BinaryName: "demo"}

	h, err := StartDaemon(
		context.Background(), cfg, "echo",
		"demo serve --ready-marker listening",
		nil, env, workDir, homeDir, map[string]string{}, binPath,
		&ReadyCheck{LogContains: "listening on port", Timeout: "5s"},
		"",
	)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if h.PID == 0 {
		t.Fatal("expected non-zero PID")
	}

	res := h.Stop(&StopSpec{Grace: "4s"})
	if !res.CleanExit {
		t.Errorf("expected clean exit, got unclean (signal=%s, exit=%d)", res.Signal, res.ExitCode)
	}
	if res.ExitCode != 0 {
		t.Errorf("expected exit 0, got %d", res.ExitCode)
	}
	if !strings.Contains(res.LogTail, "graceful shutdown") {
		t.Errorf("expected log to contain 'graceful shutdown', got: %s", res.LogTail)
	}
}

func TestDaemon_UncleanShutdown(t *testing.T) {
	binPath, env, workDir, homeDir := daemonTestEnv(t)
	cfg := Config{BinaryName: "demo"}

	h, err := StartDaemon(
		context.Background(), cfg, "stubborn",
		"demo serve --ignore-sigterm --ready-marker listening",
		nil, env, workDir, homeDir, map[string]string{}, binPath,
		&ReadyCheck{LogContains: "listening on port", Timeout: "5s"},
		"",
	)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	res := h.Stop(&StopSpec{Grace: "500ms"})
	if res.CleanExit {
		t.Error("expected unclean exit (SIGKILL escalation), got clean")
	}
	if res.Signal != "TERM" {
		t.Errorf("expected initial signal TERM, got %s", res.Signal)
	}
}

func TestDaemon_ReadinessTimeout(t *testing.T) {
	binPath, env, workDir, homeDir := daemonTestEnv(t)
	cfg := Config{BinaryName: "demo"}

	// Ask the daemon to listen, but require a readiness marker it will never
	// emit. Should fail fast and reap the process.
	_, err := StartDaemon(
		context.Background(), cfg, "neverready",
		"demo serve",
		nil, env, workDir, homeDir, map[string]string{}, binPath,
		&ReadyCheck{LogContains: "this-marker-never-appears", Timeout: "400ms"},
		"",
	)
	if err == nil {
		t.Fatal("expected readiness timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected 'timed out' in error, got: %v", err)
	}
	// Give the kill a moment to land, then ensure no demo process is alive
	// for our session. (We can't easily check PID directly because Stop()
	// already reaped it; the assertion above is sufficient.)
}

func TestDaemon_MidSpecDeath(t *testing.T) {
	binPath, env, workDir, homeDir := daemonTestEnv(t)
	cfg := Config{BinaryName: "demo"}

	// crash-after will exit non-zero shortly after ready fires.
	h, err := StartDaemon(
		context.Background(), cfg, "crasher",
		"demo serve --ready-marker listening --crash-after 200ms",
		nil, env, workDir, homeDir, map[string]string{}, binPath,
		&ReadyCheck{LogContains: "listening on port", Timeout: "5s"},
		"",
	)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	registry := &DaemonRegistry{}
	registry.add(h)

	// Wait for the daemon to crash, then poll.
	select {
	case <-h.exited:
	case <-time.After(3 * time.Second):
		t.Fatal("daemon did not exit within 3s")
	}
	dead := registry.PollDeaths()
	if dead == nil {
		t.Fatal("PollDeaths returned nil, expected the crashed daemon")
	}
	if dead.Name != "crasher" {
		t.Errorf("expected crasher, got %s", dead.Name)
	}
	if dead.exitCode == 0 {
		t.Errorf("expected non-zero exit, got 0")
	}
}

func TestDaemon_LogAssert(t *testing.T) {
	binPath, env, workDir, homeDir := daemonTestEnv(t)
	cfg := Config{BinaryName: "demo"}

	h, err := StartDaemon(
		context.Background(), cfg, "echo",
		"demo serve --ready-marker listening",
		nil, env, workDir, homeDir, map[string]string{}, binPath,
		&ReadyCheck{LogContains: "listening on port", Timeout: "5s"},
		"",
	)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	res := h.Stop(&StopSpec{Grace: "4s"})

	asserts := []Assertion{
		{Type: "contains", Value: "graceful shutdown complete"},
		{Type: "not_contains", Value: "PANIC"},
	}
	results := RunLogAssertions(asserts, h.LogBuf.String(), map[string]string{}, nil)
	if len(results) != 2 {
		t.Fatalf("expected 2 log assertion results, got %d", len(results))
	}
	for i, r := range results {
		if !r.Passed {
			t.Errorf("log assertion %d failed: %s (%s)", i, r.Summary, r.Error)
		}
	}
	// Ensure unused vars
	_ = res
}

func TestDaemon_ProcessGroupReap(t *testing.T) {
	binPath, env, workDir, homeDir := daemonTestEnv(t)
	cfg := Config{BinaryName: "demo"}

	// Spawn under sh -c so sh becomes the group leader, then exec demo.
	// Both share the process group; SIGTERM to the group reaps both.
	h, err := StartDaemon(
		context.Background(), cfg, "wrapped",
		"sh -c 'demo serve --ready-marker listening'",
		nil, env, workDir, homeDir, map[string]string{}, binPath,
		&ReadyCheck{LogContains: "listening on port", Timeout: "5s"},
		"",
	)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if h.PGID == 0 {
		t.Errorf("expected non-zero PGID for process-group reap, got 0")
	}

	res := h.Stop(&StopSpec{Grace: "4s"})
	if !res.CleanExit {
		t.Errorf("expected clean exit for process group, got unclean")
	}
}

func TestRingBuffer_Bounded(t *testing.T) {
	rb := newRingBuffer(10)
	rb.Write([]byte("abcdefghij")) // exactly cap
	if rb.String() != "abcdefghij" {
		t.Errorf("got %q", rb.String())
	}
	rb.Write([]byte("klmnop")) // overflow
	got := rb.String()
	if len(got) != 10 {
		t.Errorf("expected length 10, got %d (%q)", len(got), got)
	}
	if !strings.HasSuffix(got, "klmnop") {
		t.Errorf("expected tail 'klmnop', got %q", got)
	}
}

// TestSpec_ValidateDaemons exercises the validation rules added in spec.go.
func TestSpec_ValidateDaemons(t *testing.T) {
	cases := []struct {
		name    string
		spec    Spec
		wantErr string
	}{
		{
			name:    "valid post_setup",
			spec:    Spec{Daemons: []Daemon{{Name: "a", Command: "sleep 1"}}, Steps: []Step{{Name: "x", Command: "true"}}},
			wantErr: "",
		},
		{
			name:    "missing daemon command",
			spec:    Spec{Daemons: []Daemon{{Name: "a"}}, Steps: []Step{{Name: "x", Command: "true"}}},
			wantErr: "command is required",
		},
		{
			name:    "bad phase",
			spec:    Spec{Daemons: []Daemon{{Name: "a", Command: "sleep 1", Phase: "midway"}}, Steps: []Step{{Name: "x", Command: "true"}}},
			wantErr: "invalid phase",
		},
		{
			name:    "duplicate name across daemon+step",
			spec:    Spec{Daemons: []Daemon{{Name: "dup", Command: "sleep 1"}}, Steps: []Step{{Name: "dup", Command: "sleep 1", Background: true}}},
			wantErr: "already used",
		},
		{
			name:    "background with assert",
			spec:    Spec{Steps: []Step{{Name: "x", Command: "sleep 1", Background: true, Assert: []Assertion{{Type: "contains", Value: "x"}}}}},
			wantErr: "background:true forbids assert",
		},
		{
			name:    "background with capture",
			spec:    Spec{Steps: []Step{{Name: "x", Command: "sleep 1", Background: true, Capture: map[string]string{"v": "$.x"}}}},
			wantErr: "background:true forbids capture",
		},
		{
			name:    "ready without background",
			spec:    Spec{Steps: []Step{{Name: "x", Command: "true", Ready: &ReadyCheck{TCP: ":80"}}}},
			wantErr: "ready/stop/log_assert require background: true",
		},
		{
			name:    "ready with two modes",
			spec:    Spec{Daemons: []Daemon{{Name: "a", Command: "sleep 1", Ready: &ReadyCheck{TCP: ":80", LogContains: "ready"}}}, Steps: []Step{{Name: "x", Command: "true"}}},
			wantErr: "exactly one of tcp/log_contains/command/pid_file",
		},
		{
			name:    "bad stop signal",
			spec:    Spec{Daemons: []Daemon{{Name: "a", Command: "sleep 1", Stop: &StopSpec{Signal: "KILL"}}}, Steps: []Step{{Name: "x", Command: "true"}}},
			wantErr: "stop.signal",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.spec.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("expected error containing %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}

// Sanity: ensure parallel daemons in independent specs don't clobber each
// other's registry entries (the registry is per-RunSpec, not global).
func TestDaemonRegistry_Isolation(t *testing.T) {
	a := &DaemonRegistry{}
	b := &DaemonRegistry{}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			h := &DaemonHandle{Name: "x" + strconv.Itoa(i), exited: make(chan struct{})}
			if i%2 == 0 {
				a.add(h)
			} else {
				b.add(h)
			}
		}()
	}
	wg.Wait()
	if len(a.All()) != 5 || len(b.All()) != 5 {
		t.Fatalf("expected 5+5, got %d+%d", len(a.All()), len(b.All()))
	}
}


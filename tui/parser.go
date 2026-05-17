package tui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/shivamstaq/gotit/runner"
)

func itoa(n int) string  { return strconv.Itoa(n) }
func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// FindLatestJSONL returns the most recent .jsonl file in dir. Used by the
// post-mortem path (when the user opens the TUI after a CI run).
func FindLatestJSONL(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read results dir %s: %w", dir, err)
	}
	var latest string
	var latestTime time.Time
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".jsonl" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(latestTime) {
			latestTime = info.ModTime()
			latest = filepath.Join(dir, e.Name())
		}
	}
	if latest == "" {
		return "", fmt.Errorf("no .jsonl files found in %s", dir)
	}
	return latest, nil
}

// recordType identifies which record shape to decode.
type recordTypeOnly struct {
	Type string `json:"type"`
}

// JSONLEvent is the union of records the runner writes — one per JSON line.
// Tail consumers receive these as they land.
type JSONLEvent struct {
	Type       string                   `json:"type"`
	Phase      string                   `json:"phase,omitempty"`
	Wave       string                   `json:"wave"`
	Spec       string                   `json:"spec"`
	Step       string                   `json:"step,omitempty"`
	Command    string                   `json:"command,omitempty"`
	ExitCode   int                      `json:"exit_code,omitempty"`
	DurationMs int64                    `json:"duration_ms,omitempty"`
	Stdout     string                   `json:"stdout,omitempty"`
	Stderr     string                   `json:"stderr,omitempty"`
	Assertions []runner.AssertionResult `json:"assertions,omitempty"`
	Passed     bool                     `json:"passed,omitempty"`
	Skipped    bool                     `json:"skipped,omitempty"`
	Error      string                   `json:"error,omitempty"`
	WorkDir    string                   `json:"work_dir,omitempty"`
	HomeDir    string                   `json:"home_dir,omitempty"`
	BinDir     string                   `json:"bin_dir,omitempty"`
	Env        map[string]string        `json:"env,omitempty"`

	// Daemon-event fields (populated only on daemon_* record types).
	Daemon        string                   `json:"daemon,omitempty"`
	PID           int                      `json:"pid,omitempty"`
	Signal        string                   `json:"signal,omitempty"`
	Ready         string                   `json:"ready,omitempty"`
	CleanExit     bool                     `json:"clean_exit,omitempty"`
	LogTail       string                   `json:"log_tail,omitempty"`
	LogAssertions []runner.AssertionResult `json:"log_assertions,omitempty"`
}

// TailJSONL streams every appended line of a JSONL log file to ch until ctx
// is cancelled or the file is closed by the writer. Existing lines are
// emitted first, then new lines as they arrive.
//
// The function blocks; run in a goroutine. On ctx cancellation it returns nil.
func TailJSONL(ctx context.Context, path string, ch chan<- JSONLEvent) error {
	// Wait briefly for the file to be created if it isn't yet — `go test`
	// takes ~100ms to open the runner's logger.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for %s", path)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(50 * time.Millisecond):
		}
	}

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	reader := bufio.NewReader(f)
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		line, err := reader.ReadString('\n')
		if err == io.EOF {
			// No new data yet; sleep briefly and retry. We rely on the spec
			// runner's spec_end record to signal completion at a higher level.
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(50 * time.Millisecond):
				continue
			}
		}
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		if line == "" || line == "\n" {
			continue
		}

		var probe recordTypeOnly
		if err := json.Unmarshal([]byte(line), &probe); err != nil {
			continue // malformed line; skip
		}
		var ev JSONLEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		ch <- ev
	}
}

// ToSpecEvent converts a JSONLEvent into the runner.SpecEvent shape the TUI
// already knows how to render. Daemon lifecycle records are surfaced as
// synthetic step_done events with phase="daemon" so they appear in the step
// timeline; this keeps the renderer unchanged while making daemon activity
// visible without a dedicated panel.
func (e JSONLEvent) ToSpecEvent() runner.SpecEvent {
	t := e.Type
	command := e.Command
	stdout := e.Stdout
	passed := e.Passed
	step := e.Step
	phase := e.Phase
	assertions := e.Assertions

	switch e.Type {
	case runner.RecordTypeSpecStart:
		t = "spec_start"
	case runner.RecordTypeStep:
		t = "step_done"
	case runner.RecordTypeSpecEnd:
		t = "spec_end"
	case runner.RecordTypeDaemonStart:
		t = "step_done"
		phase = "daemon"
		step = "[daemon] " + e.Daemon + " starting"
		passed = e.Error == ""
		if e.Error != "" {
			stdout = e.Error
		}
	case runner.RecordTypeDaemonReady:
		t = "step_done"
		phase = "daemon"
		step = "[daemon] " + e.Daemon + " ready"
		passed = true
		stdout = "ready via " + e.Ready
	case runner.RecordTypeDaemonDied:
		t = "step_done"
		phase = "daemon"
		step = "[daemon] " + e.Daemon + " died"
		passed = false
		stdout = "exit_code=" + itoa(e.ExitCode) + "\n" + e.LogTail
	case runner.RecordTypeDaemonStop:
		t = "step_done"
		phase = "daemon"
		step = "[daemon] " + e.Daemon + " stopped"
		passed = e.Passed
		signalLabel := e.Signal
		if signalLabel != "" {
			signalLabel = "SIG" + signalLabel
		}
		stdout = signalLabel + ", exit=" + itoa(e.ExitCode) + ", clean=" + boolStr(e.CleanExit) + "\n" + e.LogTail
		// Merge log_assertions into assertions so the renderer shows them.
		if len(e.LogAssertions) > 0 {
			assertions = append(assertions, e.LogAssertions...)
		}
	}
	return runner.SpecEvent{
		Type:       t,
		Wave:       e.Wave,
		Spec:       e.Spec,
		Phase:      phase,
		Step:       step,
		Command:    command,
		Stdout:     stdout,
		Stderr:     e.Stderr,
		ExitCode:   e.ExitCode,
		DurationMs: e.DurationMs,
		Assertions: assertions,
		Passed:     passed,
		Skipped:    e.Skipped,
		Error:      e.Error,
		WorkDir:    e.WorkDir,
		HomeDir:    e.HomeDir,
		BinDir:     e.BinDir,
		Env:        e.Env,
		Daemon:     e.Daemon,
		PID:        e.PID,
		Signal:     e.Signal,
		Ready:      e.Ready,
	}
}

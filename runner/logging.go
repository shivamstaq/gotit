package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Record type constants for JSONL entries.
const (
	RecordTypeSpecStart = "spec_start"
	RecordTypeStep      = "step"
	RecordTypeSpecEnd   = "spec_end"
)

// SpecStartRecord is the first JSONL line for a spec.
type SpecStartRecord struct {
	Type    string            `json:"type"`
	Wave    string            `json:"wave"`
	Spec    string            `json:"spec"`
	WorkDir string            `json:"work_dir"`
	HomeDir string            `json:"home_dir"`
	BinDir  string            `json:"bin_dir"`
	Env     map[string]string `json:"env"`
}

// SpecEndRecord is the last JSONL line for a spec.
//
// Skipped distinguishes a requirements skip (e.g. unmet feature flag) from a
// genuine failure. When Skipped is true, Passed is false and Error carries the
// reason — the TUI uses this to land the spec on StateSkipped rather than
// StateFailed and to surface the reason in its right panel.
type SpecEndRecord struct {
	Type       string `json:"type"`
	Wave       string `json:"wave"`
	Spec       string `json:"spec"`
	Passed     bool   `json:"passed"`
	Skipped    bool   `json:"skipped,omitempty"`
	Error      string `json:"error,omitempty"`
	DurationMs int64  `json:"duration_ms"`
}

// StepLog is one JSONL line per step.
type StepLog struct {
	Type       string            `json:"type"`
	Phase      string            `json:"phase,omitempty"`
	Timestamp  time.Time         `json:"timestamp"`
	Wave       string            `json:"wave"`
	Spec       string            `json:"spec"`
	Step       string            `json:"step"`
	Command    string            `json:"command"`
	ExitCode   int               `json:"exit_code"`
	DurationMs int64             `json:"duration_ms"`
	Stdout     string            `json:"stdout"`
	Stderr     string            `json:"stderr"`
	Assertions []AssertionResult `json:"assertions"`
	Passed     bool              `json:"passed"`
}

// AssertionResult records one assertion's outcome.
//
// Summary, Want, and Got give the TUI (and the test driver's t.Errorf output)
// enough context to show what was checked without rereading the full step
// stdout. Error is kept for back-compat with older logs and for the testdriver
// fallback when richer fields are absent.
type AssertionResult struct {
	Type    string `json:"type"`
	Passed  bool   `json:"passed"`
	Summary string `json:"summary,omitempty"`
	Want    string `json:"want,omitempty"`
	Got     string `json:"got,omitempty"`
	Error   string `json:"error,omitempty"`
}

// JSONLLogger writes one record per line. Safe for concurrent use.
type JSONLLogger struct {
	mu   sync.Mutex
	file *os.File
}

// NewJSONLLogger opens results/run-<timestamp>.jsonl under resultsDir.
//
// When the environment variable GOTIT_RESULTS_FILE is set to an absolute path,
// the logger writes to that exact file instead of generating a timestamped name
// under resultsDir. The TUI uses this to pre-compute the path it tails for live
// events.
func NewJSONLLogger(resultsDir string) (*JSONLLogger, error) {
	if forced := os.Getenv("GOTIT_RESULTS_FILE"); forced != "" {
		if err := os.MkdirAll(filepath.Dir(forced), 0o755); err != nil {
			return nil, fmt.Errorf("create results dir for %s: %w", forced, err)
		}
		f, err := os.Create(forced)
		if err != nil {
			return nil, fmt.Errorf("create log file %s: %w", forced, err)
		}
		return &JSONLLogger{file: f}, nil
	}
	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		return nil, fmt.Errorf("create results dir: %w", err)
	}
	name := fmt.Sprintf("run-%s.jsonl", time.Now().Format("2006-01-02T15-04-05"))
	path := filepath.Join(resultsDir, name)
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create log file %s: %w", path, err)
	}
	return &JSONLLogger{file: f}, nil
}

// Log writes a step record.
func (l *JSONLLogger) Log(entry StepLog) {
	entry.Type = RecordTypeStep
	l.LogAny(entry)
}

// LogAny writes any JSON-serializable record as one JSON line.
func (l *JSONLLogger) LogAny(v any) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	data, _ := json.Marshal(v)
	data = append(data, '\n')
	_, _ = l.file.Write(data)
}

// Close flushes and closes the log file.
func (l *JSONLLogger) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	return l.file.Close()
}

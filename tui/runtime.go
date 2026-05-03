package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/shivamstaq/gotit/runner"
)

// jsonlSeq is incremented per spawn to give each `go test` invocation a
// unique JSONL output file the TUI can tail.
var jsonlSeq atomic.Int64

// SpawnGoTest runs `go test ./<specsDir>/.. -run "TestE2E/<wave>/<name>"` for
// the given spec, with env vars that make the runner write JSONL to a
// known path and preserve the temp HOME on exit.
//
// Events parsed from the JSONL file are emitted on `events` as runner.SpecEvents.
// When the test process exits, the function closes `events` (after one final
// drain) and returns. Cancelling ctx terminates the test process and the tail.
//
// Returns the path to the JSONL log so the caller can re-read it later if needed.
func SpawnGoTest(ctx context.Context, projectRoot, specsDir, wave, specName string, events chan<- runner.SpecEvent) (string, error) {
	defer close(events)

	// Compute a unique JSONL path. /tmp on Linux/macOS — TempDir handles others.
	seq := jsonlSeq.Add(1)
	jsonlPath := filepath.Join(os.TempDir(),
		fmt.Sprintf("gotit-run-%d-%d.jsonl", time.Now().Unix(), seq))

	// Determine the package import path: go test wants ./<specsDir>/.. or
	// the package containing the runner_test.go. We use ./<specsDir>/..
	// which resolves to e.g. ./tests/e2e — the right level for `runner_test.go`.
	testPkg := "./" + filepath.ToSlash(filepath.Dir(specsDir))
	if testPkg == "./." {
		testPkg = "./tests/e2e"
	}

	runFilter := "TestE2E/" + wave + "/" + specName

	cmd := exec.CommandContext(ctx, "go", "test", testPkg,
		"-run", runFilter, "-count=1", "-v", "-timeout=300s")
	cmd.Dir = projectRoot
	cmd.Env = append(os.Environ(),
		"GOTIT_RESULTS_FILE="+jsonlPath,
		"GOTIT_KEEP_HOMEDIR=1",
	)

	// Run `go test` in a goroutine so the JSONL tail can run concurrently.
	type runResult struct {
		err error
	}
	doneCh := make(chan runResult, 1)
	go func() {
		_ = cmd.Start()
		doneCh <- runResult{err: cmd.Wait()}
	}()

	// Tail the JSONL file. The tailer waits for the file to appear, then
	// streams every line until ctx is cancelled.
	tailCtx, cancelTail := context.WithCancel(ctx)
	tailEvents := make(chan JSONLEvent, 64)
	tailErrCh := make(chan error, 1)
	go func() {
		tailErrCh <- TailJSONL(tailCtx, jsonlPath, tailEvents)
	}()

	// Pump tailEvents → events as runner.SpecEvent until either the process
	// exits or ctx is cancelled. After process exit, drain remaining events
	// for ~250ms (the tailer may still be flushing the last few lines), then
	// stop.
	for {
		select {
		case <-ctx.Done():
			cancelTail()
			return jsonlPath, ctx.Err()

		case ev := <-tailEvents:
			events <- ev.ToSpecEvent()

		case res := <-doneCh:
			// Process exited. Drain stragglers from the tail before closing.
			drainDeadline := time.After(250 * time.Millisecond)
		drain:
			for {
				select {
				case ev := <-tailEvents:
					events <- ev.ToSpecEvent()
				case <-drainDeadline:
					break drain
				}
			}
			cancelTail()
			if res.err != nil {
				// `go test` non-zero is usually expected (a spec failed).
				// Return the error so the TUI can record it but don't
				// stop the run loop.
				return jsonlPath, res.err
			}
			return jsonlPath, nil
		}
	}
}

// ParseGotitConfig loads tests/e2e/gotit.yaml (or env-var overrides) into a
// runner.Config-shaped value the TUI can use to know binary name, build
// path, env prefix, and dirs. The TUI doesn't run RunSpec directly so most
// of the registries (RepoHelpers, RequirementCheckers, Assertions) aren't
// needed — that's the consumer's runner_test.go's job.
type GotitConfig struct {
	BinaryName   string
	BuildPath    string
	EnvPrefix    string
	SpecsDir     string
	FixturesDir  string
	FeaturesFile string
	ResultsDir   string
}

// ApplyDefaults fills empty fields with the same defaults the runner uses.
func (c *GotitConfig) ApplyDefaults() {
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
	c.EnvPrefix = strings.ToUpper(c.EnvPrefix)
}

// Validate reports missing required fields.
func (c GotitConfig) Validate() error {
	var missing []string
	if c.BinaryName == "" {
		missing = append(missing, "binary_name")
	}
	if c.BuildPath == "" {
		missing = append(missing, "build_path")
	}
	if c.EnvPrefix == "" {
		missing = append(missing, "env_prefix")
	}
	if len(missing) > 0 {
		return fmt.Errorf("gotit.yaml missing required fields: %s", strings.Join(missing, ", "))
	}
	return nil
}

// LoadConfigFromEnv overlays GOTIT_BINARY / GOTIT_BUILD_PATH / GOTIT_ENV_PREFIX
// onto an existing config. Set non-empty values win.
func LoadConfigFromEnv(c *GotitConfig) {
	if v := os.Getenv("GOTIT_BINARY"); v != "" {
		c.BinaryName = v
	}
	if v := os.Getenv("GOTIT_BUILD_PATH"); v != "" {
		c.BuildPath = v
	}
	if v := os.Getenv("GOTIT_ENV_PREFIX"); v != "" {
		c.EnvPrefix = v
	}
}

// SeqString is a tiny helper that exposes the spawn counter for tests/logs.
func SeqString() string { return strconv.FormatInt(jsonlSeq.Load(), 10) }

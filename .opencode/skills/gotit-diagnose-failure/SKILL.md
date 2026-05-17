---
name: gotit-diagnose-failure
description: Read the most recent JSONL log under tests/e2e/results/, identify the failed step, classify the failure, and propose a fix or open a tracking issue. Invoke when a test fails locally or in CI and the user wants to debug it.
version: 1
---

# gotit-diagnose-failure

Find the failed step, classify the failure, and propose a fix.

## Path A — interactive (preferred when `gotit` is installed)

If the user has the `gotit` binary, this is dramatically faster than parsing JSONL by hand:

1. Tell the user: `cd <project-root> && gotit`
2. In the TUI:
   - Find the failed spec (red `✗` in the test list; `/`-search if many specs).
   - Press `⏎` to open the step viewer.
   - Press `→` until you reach the failed step. The full stdout, stderr, and assertion errors are shown.
   - Press `⏎` again to open the **embedded shell** in the spec's preserved temp HOME — the same env, same cwd, same `<PREFIX>_HOME` the test ran under. Reproduce by hand: re-run the failing CLI invocation, inspect intermediate state, etc.
   - Press `n` to read the spec's sidecar notes (often documents the why behind the assertion).
3. Once you understand the cause, propose a fix.

The TUI sets `GOTIT_KEEP_HOMEDIR=1` while it's open so the temp dir survives test exit. It cleans up when the user quits or presses `R`.

## Path B — JSONL parsing (when `gotit` isn't available, or in CI artifacts)

Walk a JSONL run log to find the failure(s) and produce a focused diagnosis.

### Inputs

- A JSONL log path (defaults to the most recent under `tests/e2e/results/`).
- Optionally a spec name to filter to.

### Steps

1. **Locate the log**. If the user didn't specify, list `tests/e2e/results/*.jsonl` and pick the most recent (`ls -t | head -1`).

2. **Filter to failed records** with `jq` (preferred) or `grep`:
   ```
   jq -c 'select(.passed == false)' tests/e2e/results/run-*.jsonl
   ```
   You're looking for records of `type: step` with `passed: false`, plus the surrounding `spec_start` so you have the work dir, env, and command.

3. **Classify the failure**:
   - **Crash** — `exit_code` non-zero where 0 expected. Read `stderr`, then read the source file the stderr trace points at.
   - **Wrong output** — `contains` / `regex` / `json_path` / `golden_file` failed. The `assertions[].error` field has the expected vs got.
   - **Capture miss** — earlier step's capture didn't match, so a later command runs with literal `{{ var }}` in it. Find the step where the capture expression first failed.
   - **Setup failure** — `phase: "setup"` step exited non-zero. The spec aborted before reaching test steps; fix the setup, not the assertion.
   - **Timeout** — exit code 0 but duration ≥ step's `timeout`. Increase, or split the step.
   - **Skip** — appears in `go test` output but not in the JSONL. Means a `requires:` prerequisite was unmet; check the test's `Requires` against `features.yaml` and `RequirementCheckers`.
   - **Daemon failure** — see the daemon-classification table below.

### Daemon failures (records with `"type":"daemon_*"`)

When a spec uses `daemons:` or `background: true`, four extra record types appear: `daemon_start`, `daemon_ready`, `daemon_died`, `daemon_stop`. Classify by which one shows `passed: false`:

| Record (where it fails) | Meaning | Diagnosis |
|---|---|---|
| `daemon_start` with `error` | Spawn failed. | Read `error`. Common: binary not on PATH, command parse error, work dir missing. |
| `daemon_start` succeeded but no `daemon_ready` appears, followed by error | Readiness timed out. | Read the next `error`-bearing record's message — it has the captured log tail and which `ready:` mode it was waiting on. Increase `ready.timeout`, or fix the daemon to actually reach the ready state (port not binding, log line never printed, pid file never written). |
| `daemon_died` mid-spec | Process exited before stop was called. | The record carries `exit_code` and `log_tail`. Match the tail against your daemon's known fatal-error markers; if it's a crash, fix the daemon. If it's a graceful exit (`exit_code: 0`), the daemon thinks it's done — maybe it received an unintended signal, or a config option made it exit early. |
| `daemon_stop` with `clean_exit: false` | SIGTERM didn't drop the daemon within `stop.grace`; SIGKILL was needed. | The daemon's shutdown handler is broken: ignoring signals, blocked in a long operation, or stuck on uncancellable I/O. Fix the daemon; bumping `grace` only masks the bug. Examine `log_tail` for what the daemon was last doing. |
| `daemon_stop` with `exit_mismatch: true` | Process exited with `code != expected_exit`. | Either the daemon doesn't exit cleanly under the configured `stop.signal`, or `expected_exit` was set wrong. Cross-check the daemon's documented exit codes. |
| `daemon_stop.log_assertions[].passed: false` | A `log_assert:` check failed. | The daemon ran and exited, but its captured stdout+stderr didn't contain the expected marker (or contained a forbidden one). `log_tail` is included in the same record. |

Useful `jq` recipes for daemon triage:

```sh
# Every non-passing daemon record, last run
jq -c 'select(.type|startswith("daemon_")) | select(.passed == false)' \
    $(ls -t tests/e2e/results/run-*.jsonl | head -1)

# Full daemon timeline for one spec
jq -c 'select(.spec == "<name>") | select(.type|startswith("daemon_"))' \
    $(ls -t tests/e2e/results/run-*.jsonl | head -1)

# Pull just the log tail of the most recent unclean stop
jq -r 'select(.type=="daemon_stop") | select(.clean_exit == false) | .log_tail' \
    $(ls -t tests/e2e/results/run-*.jsonl | head -1) | tail -1
```

The daemon's full (unbounded) log lives at `$HOME/.gotit-daemons/<name>.log` under the spec's temp HOME — wiped at end-of-spec unless `GOTIT_KEEP_HOMEDIR=1` is set (the TUI does this automatically).

4. **Reproduce locally before proposing a fix**:
   - The `spec_start` record carries `home_dir`. That dir is gone after `t.Cleanup`, so re-running the test recreates it. Use `go test ./tests/e2e/ -run "TestE2E/<wave>/<spec>" -v` to reproduce.
   - For interactive inspection, the gotit TUI (`just e2e`, if the project ships it) opens a shell inside the test's temp dir.

5. **Propose a fix** based on the classification:
   - Crash → fix the CLI.
   - Wrong output → fix the CLI, or update the assertion if the expected value was wrong.
   - Capture miss → fix the capture expression (the JSONPath or regex).
   - Setup failure → fix the setup step (often a missing prerequisite).
   - Timeout → fix the slow path, then *consider* raising the timeout.
   - Skip → either flip the feature flag or register the missing requirement.

6. **Never** mark a test as `t.Skip` in the runner_test.go to silence a real failure. If a feature regressed, the spec should fail.

## Anti-patterns

- "Diagnose" by guessing without reading the JSONL. The actual stderr/error is in the file; read it.
- Bisecting via removing assertions. Add `-v` and read the full step trace.
- Editing the YAML to make the test pass. Fix the CLI, or document why the expected value changed.

## References

- JSONL record shapes: https://github.com/shivamstaq/gotit/blob/main/runner/logging.go
- Event semantics: https://github.com/shivamstaq/gotit/blob/main/SPEC.md#11-event-stream--logging

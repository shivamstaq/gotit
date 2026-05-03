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

---
name: gotit-run-suite
description: Run the gotit E2E suite (full or filtered) via `go test -run`, with feature-flag and verbosity overrides, and summarize the result. Invoke when the user asks to "run the e2e tests", "run wave2 only", or to verify a single spec after editing.
version: 1
---

# gotit-run-suite

Run the suite or a slice of it. Three execution modes are supported.

## Interactive — `gotit` (TUI)

If the `gotit` binary is installed (`go install github.com/shivamstaq/gotit/cmd/gotit@latest`), prefer the TUI for local development:

```
cd <project-root>
gotit
```

Use `r` to run one spec, `a` to run all (or all matching the active filter), `R` to reset state, `s` to stop a runaway. Live step output streams into the right pane. See the `gotit-tui` skill for the full keybinding table.

## Headless — `gotit run`

For CI or scripts where the TUI isn't appropriate:

```
gotit run             # all specs
gotit run wave1       # one wave
gotit run wave3/risk  # one spec
gotit run wave3 -- -count=1 -race -v   # forward extra flags to `go test`
```

`gotit run` shells to `go test` under the hood with the right `-run` filter.

## Plain `go test`

The original CI surface still works exactly as before:

## Common invocations

| Goal | Command |
|---|---|
| Full suite | `go test ./tests/e2e/ -v` |
| One wave | `go test ./tests/e2e/ -run 'TestE2E/wave2' -v` |
| One spec | `go test ./tests/e2e/ -run 'TestE2E/wave2/journey-foo' -v` |
| Specs matching a pattern | `go test ./tests/e2e/ -run 'TestE2E/.*/journey-.*' -v` |
| Force-enable feature flags | `<PREFIX>_E2E_FEATURES=flag1,flag2 go test ./tests/e2e/ -v` |
| Force-enable everything | `<PREFIX>_E2E_FEATURES=all go test ./tests/e2e/ -v` |
| Tighten parallelism | `go test ./tests/e2e/ -parallel 2 -v` |
| With race detector | `go test ./tests/e2e/ -race -v` |

The subtest naming is always `TestE2E/<wave>/<name>`. There is no tag-based filtering built in; if you need it, write a one-off helper that reads YAML and prints names matching, then feed those names into `-run`.

## Steps

1. **Confirm the project has a suite**. If `tests/e2e/runner_test.go` doesn't exist, point the user at `gotit-bootstrap` first.

2. **Pick the right scope**. Default to the smallest scope that answers the user's question:
   - "Did my edit break anything?" → just the spec(s) for the feature you edited.
   - "Is the wave green?" → that wave only.
   - "Are we ready to merge?" → the full suite.

3. **Run with `-v`**. The verbose output (one line per step) is what `gotit-diagnose-failure` consumes if anything fails.

4. **Summarize**. Report:
   - Pass count, fail count, skip count.
   - For failures: the spec name and the first assertion error (verbatim).
   - For skips: the requirement that wasn't met (often a feature flag).

5. **If everything passed**, mention the JSONL log path so the user has it for future debugging.

## Tips

- The suite runs in parallel by default. If you see flakes that disappear with `-parallel 1`, there's a real isolation bug — investigate, don't paper over.
- `-count=1` defeats Go's test cache. The suite is fully I/O-bound (it execs binaries), so caching doesn't usually help; if a spec is "passing" but the CLI's been edited, the fresh binary build in `TestMain` ensures the test actually exercises the new code.
- Don't pass `-failfast` for the full suite; the parallel subtest model means you'll lose useful information about other failures.

### Daemon specs

Specs that use `daemons:` or `background: true` add real-time overhead: readiness polling (default ~5s upper bound), `stop.grace` delay (default 5s) at teardown, and any `start_timeout` budget. A wave with five daemon specs running in parallel can take 15-30s. Either:

- Pass `-timeout 120s` (or higher) explicitly when running daemon-heavy waves.
- Or drop `start_timeout` / `stop.grace` in specs that don't need them — but only when those tighter bounds reflect *real* requirements; don't tune for speed at the cost of masking shutdown bugs.

If a daemon spec hangs, it's almost always the daemon under test failing to shut down. The test harness will SIGKILL after `stop.grace` and surface the failure; the wait is bounded.

## References

- Discovery + filtering: https://github.com/shivamstaq/gotit/blob/main/SPEC.md#13-test-discovery--filtering

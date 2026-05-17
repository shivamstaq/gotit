---
name: gotit-port-from-bats
description: Convert an existing CLI test suite (bats, expect, pytest+subprocess, Makefile-driven shell scripts) into gotit YAML specs. Invoke when the user is migrating an established test suite to gotit.
version: 1
---

# gotit-port-from-bats

Translate one source-suite scenario into one gotit spec at a time. The mapping is mostly mechanical; this skill captures the gotchas.

## Mapping table

| Source pattern | gotit equivalent |
|---|---|
| `@test "name" { ... }` (bats) | One YAML spec, `name:` from the test title (kebab-case). |
| `setup() { ... }` | `setup:` step list. |
| `teardown() { ... }` | `cleanup:` checks (declarative — temp dir wipe is automatic). |
| `[ "$status" -eq 0 ]` | `assert: [{type: exit_code, expected: 0}]`. |
| `[[ "$output" =~ "foo" ]]` | `assert: [{type: contains, value: "foo"}]`. |
| `run cmd \| jq -r .foo` | Use `command: cmd --json` directly + a `json_path` assertion or a `capture:` block. |
| `expect_eq` / `expect_match` | `json_path` with `op: ==` / `regex`. |
| Multi-step shell pipelines | Separate `steps:`, with `capture:` to thread values forward. |
| `pytest`'s `subprocess.run` + `assert .returncode` | Same as bats — exit code + output assertions. |
| `tmp_path` / `tmpdir` fixtures | Built into the runner; the spec runs in an isolated temp dir already. |
| Custom Python helpers building a repo | `runner.RepoHelper` Go function — see `gotit-add-helper`. |
| `mydaemon &` then `wait` / `kill $!` / `kill $(cat pid)` | `daemons:` block or `background: true` step — see daemon section below. |
| `wait_for_port 8080` / `wait_for_log "ready"` polling loops | `ready: { tcp: ":8080" }` / `ready: { log_contains: "ready" }`. |
| `trap "kill ..." EXIT` cleanup | Automatic — the runner reaps the process group at end of spec. |
| Assertion on captured server log after kill | `daemons[].log_assert:` with `contains` / `regex` / `not_contains`. |

## Steps

1. **Inventory the source suite**: list test names, group by scenario. Each named test usually maps to one spec.

2. **Pick a starter test** — the simplest one that exercises one CLI feature with one assertion. Port that first to confirm the pipeline works; iterate from there.

3. **For each test**:
   a. Create `tests/e2e/specs/<wave>/<test-name>.yaml`.
   b. Translate setup → `setup:` (commands; use `assert: [{type: exit_code, expected: 0}]` so failures abort cleanly).
   c. Translate the body → `steps:` with assertions.
   d. Translate teardown — usually nothing; the temp dir wipe handles cleanup. Only specify `cleanup:` if the CLI is supposed to remove specific files.

4. **Repo provisioning**: if the source suite copies a fixture from `tests/fixtures/foo` into a tmp dir, move that tree into `tests/e2e/testdata/repos/foo/` and reference with `repo.fixture: foo`.

5. **Programmatic setup**: if the source uses `git init && git commit … && git mv …`, write a Go helper (`gotit-add-helper`) and reference with `repo.helper:`.

6. **Run as you go**: `go test ./tests/e2e/ -run "TestE2E/<wave>/<spec>" -v`. Each port that passes gives you a green data point; debugging fresh-port failures is much harder when 30 specs land in one PR.

7. **Delete the old test** when the gotit spec is green and reviewed. Two suites covering the same surface drift; pick one.

## Daemon-style tests (servers, agents, watchers)

A common bats / expect / pytest pattern:

```bash
@test "echo server roundtrip" {
  mydaemon --port 8080 &
  PID=$!
  trap "kill $PID" EXIT
  # poll until ready
  for i in $(seq 1 50); do
    nc -z localhost 8080 && break
    sleep 0.1
  done
  run curl -sf http://localhost:8080/echo?msg=hi
  [ "$status" -eq 0 ]
  [[ "$output" == *"hi"* ]]
}
```

Port to gotit by **moving the lifecycle out of the test body** into the spec metadata:

```yaml
daemons:
  - name: echo
    command: sh -c 'mydaemon --port 8080'
    ready:
      tcp: "localhost:8080"
      timeout: 5s
    stop:
      grace: 5s

steps:
  - name: roundtrip
    command: curl -sf "http://localhost:8080/echo?msg=hi"
    assert:
      - { type: exit_code, expected: 0 }
      - { type: contains, value: "hi" }
```

The runner owns spawning, readiness polling, signaling, and process-group reaping. The `trap "kill"`, the polling loop, and the `&` all disappear. Use the `gotit-add-daemon-spec` skill for the full decision tree (top-level vs background, readiness mode, stop semantics).

Hand-rolled retry loops in the original (e.g. "try curl 10 times until it works") often *only* exist because the test had no readiness gate. Once gotit's `ready:` is in place, the loop is dead code — port the body straight as a single step.

## Translation gotchas

- **Shell quoting**: bats commands run through bash. gotit does not invoke a shell — argv is split on whitespace with quote-awareness. If the source uses pipes (`a | b`), explicit shell (`sh -c '...'`), or env interpolation (`$VAR`), wrap in `sh -c '...'` in the YAML.
- **Multi-line commands**: gotit `command:` is one line. Long commands → put logic in a helper or an explicit `sh -c '...'`.
- **`stdin` redirection**: gotit doesn't support stdin redirection in the YAML. Wrap with `sh -c 'cmd < file'`.
- **Captured stderr**: the source may have parsed `$stderr`; gotit asserts on `r.Stderr` via `stderr_contains` only. For more, `capture:` doesn't read stderr — use a regex on stdout where possible, or split into two steps.
- **Skips**: the source's "skip if X is missing" → `requires: [<runtime>]` registered in `RequirementCheckers`. Don't replicate skip logic inside the spec.
- **Test isolation**: the source may have leaned on `cd $tmpdir`. gotit always runs in an isolated `workDir`; that's the implicit cwd. Drop the explicit `cd`.

## References

- Spec format: https://github.com/shivamstaq/gotit/blob/main/SPEC.md#5-the-spec-format
- Anti-patterns: https://github.com/shivamstaq/gotit/blob/main/SPEC.md#18-anti-patterns

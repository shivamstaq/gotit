---
name: gotit-add-spec
description: Author one YAML spec under tests/e2e/specs/<wave>/ for one CLI feature, with appropriate assertions and an auto-rendered sidecar notes file. Invoke when the user asks to "write an e2e test for X", "add a spec for the Y subcommand", or to verify a CLI feature end-to-end.
version: 1
---

# gotit-add-spec

Add one new YAML spec to the project's gotit suite.

## Prerequisites

- The suite is already bootstrapped (`tests/e2e/runner_test.go` exists).
- Identify the wave: ask the user, or infer from existing `tests/e2e/specs/wave*/` directories. Use the highest-numbered wave by default.

## Inputs

- **Feature name** (kebab-case, the spec's `name:` field).
- **CLI invocation** to test (the `command:` field).
- **What to assert**: exit code? specific stdout substring? JSON path equality? Pick the minimum that proves the feature is working.
- **Repo state**: does the test need a fixture? a multi-commit history? Default: no `repo:` block.

## Steps

1. **Pick the wave directory**. If unsure, list `tests/e2e/specs/` and ask. Create `tests/e2e/specs/<wave>/` if it doesn't exist.

2. **Determine the next `order` value**. Read existing `*.yaml` files in the wave; pick the highest `order` and add 10. If none, start at 10.

3. **Write `tests/e2e/specs/<wave>/<name-with-underscores>.yaml`**. Use this skeleton:

   ```yaml
   # yaml-language-server: $schema=https://github.com/shivamstaq/gotit/raw/main/schema/spec.schema.json
   spec_version: 1
   name: <kebab-case-name>
   description: <one-line summary>
   tags: [<wave>, <category>]
   order: <next>
   # repo: { fixture: <name> }   # uncomment if needed
   # repo: { helper: <name> }    # uncomment if a helper is registered
   # requires: [feature:<flag>]  # uncomment to gate on an unshipped feature

   steps:
     - name: <step_name>
       command: <binary> <subcommand> <args>
       assert:
         - type: exit_code
           expected: 0
   ```

4. **Pick assertions** from the built-in 8:
   - `exit_code` — every step should have one.
   - `contains` / `not_contains` — substring match on stdout.
   - `stderr_contains` — for error-path tests.
   - `json_path` — when the CLI emits JSON, prefer this over `contains`. Use `op: ==` for equality, `op: contains_any` with a list for membership, `op: length_gte` for array minimums.
   - `count` — array length on a JSONPath.
   - `regex` — when the value is structural but not JSON (timestamps, semver).
   - `golden_file` — for stable canonical output. The path is project-relative.

5. **Add capture variables** if a later step depends on output from an earlier one:
   ```yaml
   capture:
     entity_id: "$.id"            # JSONPath
     # entity_id: "regex:ID (\\S+)"  # regex first capture group
   ```
   Reference with `{{ entity_id }}` in subsequent commands or assertion values.

6. **Verify the YAML parses and the assertions hold**:
   ```
   go test ./tests/e2e/ -run "TestE2E/<wave>/<name>" -v
   ```

7. **Sidecar notes are auto-rendered** the first time the gotit TUI opens them, but the YAML lives next to a `<name>.md` you can fill in immediately.

## Anti-patterns to avoid

- Asserting on full stdout via a single `contains` of the entire payload — brittle. Use `json_path` or break into multiple narrow `contains`.
- One spec covering five unrelated features. Split into focused specs.
- Inline shell pipelines in `command:` — the runner does not invoke a shell. If you need pipes, write `sh -c '...'` explicitly.
- Hardcoding paths under the work dir. The test runs in an isolated temp dir; refer to fixture files by relative path.
- Using `sh -c 'mydaemon &'` to background a process. The runner does not reap detached children. Use the `daemons:` block or `background: true` step — see below.

## Daemons / long-running processes

If the feature under test is a server, watcher, agent, or any process that must be running while the test exercises it: **switch to the `gotit-add-daemon-spec` skill** instead of forcing a daemon into the synchronous step model.

Quick recognition cues — the request mentions any of:

- "the server", "start the daemon", "spin up X"
- "while the worker is running, do Y"
- a `serve` / `daemon` / `watch` subcommand
- a port number, socket path, or readiness signal
- "test graceful shutdown" or "verify SIGTERM behavior"

Two spec-level surfaces are available; `gotit-add-daemon-spec` handles the choice between them, but in brief:

- `daemons:` top-level block — named daemon with `phase: pre_setup | post_setup`, `ready:`, `stop:`, `capture:`, `log_assert:`. Use when the daemon is part of the spec's *environment*.
- `background: true` on a step — inline daemon that starts at its position in the steps array. Use when the daemon naturally starts mid-sequence.

See SPEC.md §6.5 for the full contract.

## References

- Assertion semantics: https://github.com/shivamstaq/gotit/blob/main/SPEC.md#8-assertion-types
- Capture syntax: https://github.com/shivamstaq/gotit/blob/main/SPEC.md#7-capture--templating
- Daemon lifecycle: https://github.com/shivamstaq/gotit/blob/main/SPEC.md#65-daemon-lifecycle

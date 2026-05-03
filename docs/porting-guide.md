# Porting Guide

Migrate an existing CLI test suite to gotit. The mapping is mechanical for most cases.

## Source frameworks covered

- bats-core (bash)
- expect (tcl)
- pytest with `subprocess.run`
- Go subtests calling `cmd.Execute()` in-process
- Custom Makefile-driven shell scripts

If your suite is none of these, the sections below should still cover the general translation patterns.

## Translation table

| Source | gotit equivalent |
|---|---|
| One named test (`@test "foo" { ... }`) | One YAML spec under `tests/e2e/specs/<wave>/<name>.yaml`. |
| `setup()` / fixtures created in `setup` | `setup:` step list. Failure aborts the spec. |
| `teardown()` | Usually empty (temp HOME wipe is automatic). Use `cleanup:` only for "did the CLI remove what it promised" assertions. |
| `[ "$status" -eq 0 ]` | `assert: [{type: exit_code, expected: 0}]`. |
| `[[ "$output" =~ "foo" ]]` | `assert: [{type: contains, value: "foo"}]`. |
| `expect "..." { send "..." }` | gotit doesn't drive interactive sessions. Use a `--non-interactive` flag in your CLI, or fall back to `command: expect ...`. |
| `subprocess.run([...]).returncode == 0` | `assert: [{type: exit_code, expected: 0}]`. |
| `assert "foo" in result.stdout` | `assert: [{type: contains, value: "foo"}]`. |
| `assert json.loads(stdout)["x"] == 1` | `assert: [{type: json_path, path: "$.x", expected: 1}]`. |
| `tmp_path` / `tmpdir` fixtures | Built in. The spec runs in an isolated temp dir already. |
| Custom Python helpers building a repo | Translate to a Go function under `tests/e2e/helpers/`, register in `Config.RepoHelpers`. |
| Skipping if a binary is missing | `requires: [<name>]` + register a `RequirementChecker`. |

## Step-by-step

1. **Bootstrap** the gotit suite (run `gotit-bootstrap`). You'll have a working `runner_test.go` and a smoke spec; the CI signal flips green before you start porting.

2. **Inventory the source**. List every named test. Group by feature area — those become waves.

3. **Pick a starter test** — the simplest scenario in the source. Translate it. Run it. Pass. Repeat.

4. **For each test**:
   - Create `tests/e2e/specs/<wave>/<name>.yaml`.
   - Translate setup → `setup:` block.
   - Translate body → `steps:` with assertions.
   - Translate fixture/tempdir setup → `repo:` block (fixture or helper).

5. **Migrate fixtures**. Anything the source copies into a temp dir as a starting state moves to `tests/e2e/testdata/repos/<name>/`.

6. **Migrate programmatic setup**. Anything that runs `git init && git commit && git mv` becomes a Go helper. See [`gotit-add-helper` SKILL.md](../.claude/skills/gotit-add-helper/SKILL.md).

7. **Run as you go**. `go test ./tests/e2e/ -run "TestE2E/<wave>/<spec>" -v` after each port. Don't stack 30 ports into one PR — debugging is harder.

8. **Delete the source test** when its gotit replacement is green and reviewed. Two suites covering the same surface drift; pick one.

## Common gotchas

### Shell quoting

bats commands run through bash. gotit splits argv on whitespace with quote-awareness — no shell. If the source uses pipes or env interpolation, wrap explicitly:

```yaml
- command: sh -c 'mytool list | grep foo'
```

### Multi-line commands

`command:` is one line. Long invocations:

- If the logic is genuinely complex, push it into a helper.
- If you need a chain, use `sh -c '...'`.
- If you can split into multiple steps with `capture:` between them, prefer that — better diagnostics on failure.

### stdin redirection

gotit doesn't support stdin redirection at the YAML level. Use `sh -c 'cmd < file'`.

### Environment variables in commands

The runner doesn't expand `$VAR` in `command:`. To use a captured value, use the `{{ var }}` template syntax (resolved by gotit, not the shell).

```yaml
- name: capture_id
  command: mytool create --json
  capture:
    id: "$.id"
- name: use_id
  command: mytool show {{ id }}
```

For env-from-process (e.g. `$HOME`), the runner has already sandboxed it; specs shouldn't need it.

### Stderr access

The 8 built-in assertions only read stderr via `stderr_contains`. If you need richer stderr inspection:

- Prefer making the CLI emit machine-readable errors to stdout (with a non-zero exit) — easier to assert on, easier for users to consume.
- Or add a custom `x-stderr-json` assertion type (see [`gotit-add-assertion` SKILL.md](../.claude/skills/gotit-add-assertion/SKILL.md)).

### Implicit `cd`

Source suites often `cd $tmpdir` before running commands. gotit always runs in an isolated `workDir`; that's the implicit cwd. Drop the explicit `cd`.

### Skips

Source: `if ! command -v jq; then skip; fi` translates to:

- Add `requires: [jq]` to the YAML.
- Register a `RequirementChecker` named `jq` that probes `exec.LookPath("jq")`.

The skip is now machine-readable, surfaces in CI, and skips deterministically.

## After porting

- The full suite should be one `go test ./tests/e2e/ -v` invocation — no Makefile glue.
- The JSONL log under `tests/e2e/results/` becomes your CI artifact for failure analysis.
- Feature-flag any specs gating unimplemented features so the suite stays green.
- Retire the source suite from CI; remove its dependencies.

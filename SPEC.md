# gotit — E2E Test Suite Spec for Go CLIs

A portable specification for an end-to-end test suite that exercises a Go CLI binary against real repositories. Defines the architecture, contracts, and design decisions; concrete struct layouts, package boundaries, and helper sets are provided by the reference Go module under `runner/`.

This document is the cross-tool contract. Any AI agent that can read Markdown can author conforming specs; any Go runtime that conforms to this spec can execute them.

---

## 1. Purpose & Audience

**Audiences served**

- **Humans** authoring or reviewing E2E specs for a Go CLI.
- **AI coding agents** (Claude Code, OpenAI Codex, OpenCode, Cursor, Aider, others) that are asked to add, run, or diagnose specs.
- **Implementors** of alternative runtimes (e.g. a non-Go consumer of this contract).

**Goals**

- Exercise the **shipping CLI binary** (not in-process `func main()`), so packaging, flag parsing, env handling, and exit codes are covered.
- Drive scenarios **declaratively** via YAML so non-Go contributors can read and add tests, and so test intent is decoupled from harness internals.
- Run scenarios **in full isolation** — temp `HOME`, temp work dir, fresh git repo, scoped `PATH` — with no leakage between specs or into the developer's real environment.
- Produce **dual outputs**: human-readable terminal output for `go test -v`, and structured JSONL for post-mortem tooling and TUIs.
- Support **feature-gated specs**: write tests for unimplemented features, skip them deterministically until the feature ships.
- Integrate with `go test` so CI, parallelism, `-run` filtering, and IDE integrations work for free.

**Non-Goals**

- Replacing unit tests, golden-file tests, or property tests.
- Asserting visual layout of TUI rendering.
- Cross-platform GUI automation, browser drivers, mock HTTP fixtures — those are workload-specific concerns, not the suite's contract.

---

## 2. Goals & Non-Goals

Same as §1's `Goals` / `Non-Goals` blocks. (Kept as a separate section header for spec stability — agents that quote §2 always find them here.)

---

## 3. Spec Version & Stability

Every YAML spec **must** declare `spec_version` at top level:

```yaml
spec_version: 1
name: …
```

- The runtime refuses unknown majors with a clear migration message.
- Specs missing `spec_version` are accepted as v1 with a deprecation warning during the v1 → v2 transition (no transition is currently planned).
- The JSON Schema at `schema/spec.schema.json` (`$id`: `https://github.com/shivamstaq/gotit/schemas/spec.v1.json`) is the source of truth for the format. Editor tooling picks it up via the YAML language-server header:

  ```yaml
  # yaml-language-server: $schema=https://github.com/shivamstaq/gotit/raw/main/schema/spec.schema.json
  ```

- Adding new built-in assertion types or capture syntax bumps the runtime's minor version. Removing or renaming any field bumps the major.
- `docs/changelog.md` records every format change.

---

## 4. Top-Level Architecture

```
    ┌──────────────────────────────────────────────────────────┐
    │  go test ./tests/e2e/                                    │
    │                                                          │
    │  TestE2E (single function, calls testdriver.Run)         │
    │   ├─ locate project root (walk up to go.mod)             │
    │   ├─ go build -o <tmp>/<binary> <BuildPath>              │
    │   ├─ open JSONL log                                      │
    │   ├─ LoadSpecs(specs/)         (YAML walk + sort)        │
    │   └─ for each spec:                                      │
    │        t.Run(wave/name, func) { t.Parallel()             │
    │            CheckRequirements()  → t.Skip on miss         │
    │            Runner.RunSpec(ctx, spec, eventsCh)           │
    │            drain events → t.Logf / t.Errorf              │
    │            t.Cleanup(rm temp HOME)                       │
    │        }                                                 │
    └──────────────────────────────────────────────────────────┘
                       │
                       ▼
    ┌──────────────────────────────────────────────────────────┐
    │  Runner.RunSpec  (per-spec, isolated)                    │
    │                                                          │
    │   1. mkdtemp HOME, derive workDir = HOME/work            │
    │   2. provision repo  (fixture copy | helper builder)     │
    │   3. emit spec_start event                               │
    │   4. for each step in setup:    ExecuteStep              │
    │      (any failure aborts the spec)                       │
    │   5. for each step in steps:    ExecuteStep              │
    │      ├─ resolve {{ vars }}                               │
    │      ├─ exec.CommandContext(binPath, args...)            │
    │      ├─ capture stdout/stderr/exit/duration              │
    │      ├─ run captures  (JSONPath | regex)  → vars         │
    │      ├─ run assertions                                   │
    │      └─ emit step_done event                             │
    │   6. for each cleanup check: assert post-conditions      │
    │   7. emit spec_end event                                 │
    └──────────────────────────────────────────────────────────┘
```

The flow is **linear within a spec**, **parallel across specs**. The runner is a pure event source; consumers (the `go test` driver, an interactive TUI, JSONL writers) all subscribe to the same event channel.

---

## 5. The Spec Format

```yaml
spec_version: 1
name:         <stable-id, kebab-case>
description:  <one-line summary>
tags:         [<wave>, <category>, <feature>]
requires:     [<runtime-tool>, feature:<flag-name>]
order:        <int>          # optional, sorts within wave
notes_file:   <path>         # optional, default <basename>.md

repo:
  fixture: <name>            # OR
  helper:  <name>
  helper_args: { ... }

setup:                       # optional pre-conditions; failure aborts spec
  - { name, command, env, timeout, capture, assert }

steps:                       # the actual test
  - name:    <step-id>
    command: <CLI invocation, may use {{ vars }}>
    env:     { KEY: VALUE }
    timeout: 30s
    capture:
      var_name: "$.json.path"     # or "regex:(...)"
    assert:
      - { type, ... }

cleanup:
  - assert_no_residue: true
  - assert_files_removed: [<path-relative-to-workDir>]
```

### Design decisions baked into this format

- **`name` is the canonical id** — used in subtest names, logs, and TUI selection. Filenames are descriptive but not load-bearing.
- **`tags` are free-form**; the runner does not interpret them. Filtering by tag is a `go test -run` regex problem (subtest name = `wave/name`, so wave-filtering is trivial; tag-filtering is a one-off helper script).
- **`requires` is structurally simple** — a flat list of opaque strings. The runner has a small switch for `git` and `feature:<name>`; all other names are looked up in the consumer's `RequirementCheckers` map. New runtime requirements are project-local — the kit does not grow a hardcoded list.
- **`order` is optional**: unset specs sort alphabetically after ordered ones within their wave. This matters only for TUI presentation and `-v` output; correctness must never depend on order across specs.
- **`setup` vs `steps` distinction**: setup steps fail-fast (any non-zero exit aborts the spec) and have no assertions worth recording — they're for "get the system into a known state". Test steps are where assertions live and where partial failures still produce useful output.
- **`cleanup` is post-condition assertion**, not teardown. The temp `HOME` is always removed by `t.Cleanup()` regardless. Cleanup checks verify that the CLI itself didn't leak files outside the work dir or failed to remove what it promised to.

---

## 6. Step Execution Contract

```
ExecuteStep(ctx, cfg, step, workDir, env, vars, binPath) → StepResult
```

**Mandatory behaviors:**

1. **Resolve templates** in `command` and assertion fields against `vars` *before* splitting/executing.
2. **Split the command** with quote-aware tokenization (single + double quotes). Do not invoke a shell for the test command itself.
3. **Substitute the binary path**: if the first token equals `cfg.BinaryName`, replace it with the absolute path of the binary built once at suite startup. Authors can still escape into a shell explicitly with `sh -c '...'` when they need pipes or chained commands.
4. **Apply per-step timeout** via `context.WithTimeout`, defaulting to `cfg.DefaultTimeout` (30s out of the box).
5. **Capture stdout and stderr separately** into bounded buffers; return both in the result.
6. **Capture exit code** by unwrapping `*exec.ExitError`, falling back to `-1` for "couldn't even spawn".
7. **Record duration** in milliseconds.
8. **Never panic**: a bad command, parse error, or missing capture must surface as a failed assertion or a typed error, not crash the test process.

**The step environment** — assembled once per spec by the runner — must include:

| Variable | Why |
|---|---|
| `HOME=<temp>` | Forces config/cache writes into the sandbox. |
| `<PREFIX>_HOME=<temp>/.<binary>` | If the CLI honors a tool-specific home var, set it explicitly. Belt and braces. |
| `XDG_CONFIG_HOME=<temp>/.config` | Many tools read XDG. |
| `PATH=<binDir>:<old PATH>` | Ensures the just-built binary wins resolution. |
| `<PREFIX>_E2E_PROJECT_ROOT=<repo>` | Lets specs invoke project-relative tooling without hardcoding paths. |
| `GIT_AUTHOR_*`, `GIT_COMMITTER_*` | Deterministic commits. |
| `GIT_CONFIG_NOSYSTEM=1` | Prevents the developer's `/etc/gitconfig` from leaking in. |
| `TERM=dumb` | Disables CLI auto-color/auto-paging that would noise up assertions. |

`<PREFIX>` is the consumer's configured `EnvPrefix` (uppercase). Steps may add to this with their own `env:` block; the per-step env appends to the spec env.

---

## 7. Capture & Templating

The capture-and-template loop is what turns a list of independent commands into a coherent scenario.

- **JSONPath capture** (e.g. `$[0].id`, `$.entity.kind`): parse the step's stdout as JSON, evaluate the path, store the first result as a string.
- **Regex capture** (`regex:<pattern>`): match against stdout; if there is at least one capture group, store the first group; otherwise store the full match.
- **Template substitution**: `{{ var_name }}` and `{{var_name}}` (with and without spaces) are both replaced. Substitution happens **before** command splitting and before assertion evaluation, on `command`, `env` values, and string-typed assertion fields (`value`, `pattern`, `path`, and `expected` when it's a string).
- **Failures during capture do not fail the step**. A missing capture leaves the variable undefined, which usually causes the *next* step to fail with a clear "literal `{{ x }}` ended up in the command" error. This is preferable to silently masking an upstream regression.

`vars` is per-spec, not global. There is no cross-spec variable sharing — that would defeat isolation.

---

## 8. Assertion Types

Eight built-in types cover the bulk of CLI verification needs:

| Type | Required fields | Meaning |
|---|---|---|
| `exit_code` | `expected` | Exact exit code match. |
| `contains` | `value` | Stdout substring match. |
| `not_contains` | `value` | Stdout substring absence. |
| `stderr_contains` | `value` | Stderr substring match. |
| `regex` | `pattern` | Stdout regex match. |
| `json_path` | `path`, `expected`, `op` | Parse stdout as JSON, evaluate path, compare. Operators: `==`, `!=`, `>`, `<`, `>=`, `<=`, `contains`, `contains_any`, `length_gte`. |
| `count` | `path`, `op`, `expected` | Length of a JSON array at `path`, integer comparison. |
| `golden_file` | `path` | Trim-compare stdout against a checked-in file (resolved against `<PREFIX>_E2E_PROJECT_ROOT` for relative paths). |

**Custom types**: project-local types use the `x-` prefix (e.g. `x-output-lines-gte`) and must be registered via the runtime's `Config.Assertions` map. The schema enforces the prefix.

**Design rules:**

- Every assertion must report a **diagnostic message that is useful in isolation** — include the path/pattern/value involved and a truncated slice of the actual output (~500 chars). The reader of the JSONL log will not have the YAML in front of them.
- Numeric and string comparison are lenient about input types (a number from JSON and a string from YAML compare equal if their canonical forms match).
- The set should grow slowly; each new type is a maintenance cost for every reader.

---

## 9. Repository Provisioning

Two mechanisms, mutually exclusive per spec:

**Fixture copy** — for a static, file-based scenario:
- Source lives under `<FixturesDir>/<name>/`.
- Provisioning copies the tree into the work dir, runs `git init -b main`, then `git add -A && git commit -m "initial fixture commit"`.
- Cheap, reproducible, diff-able.

**Helper function** — for scenarios that require commit history, renames, branch state, conflicts, etc.:
- A named function registered in `Config.RepoHelpers`, with signature `func(projectRoot, workDir string, args map[string]any) error`.
- Specs invoke by name with `helper:` and an optional `helper_args` map.
- Helpers are responsible for `git init`, writing files, and committing in the right order. The runtime ships `GitInit`, `GitCommitAll`, `RunGit`, `WriteFile`, `CopyDir` to keep the boilerplate small.

**Why both:** fixtures cover most cases and are readable as plain trees; helpers handle the long tail that fixtures cannot express. Anything more complex than "copy and commit" should be a helper.

---

## 10. Requirements & Feature Gating

Two classes of `requires:` entries:

**Runtime requirements** — checked by name:
- `git` is built in; the runtime assumes git is on PATH.
- Everything else must be registered in `Config.RequirementCheckers`. A miss returns a typed error; the test driver translates it into `t.Skip(...)`.

**Feature flags** — `requires: [feature:<name>]`:
- Resolved against an allowlist file checked into the repo (default `tests/e2e/features.yaml`):
  ```yaml
  enabled:
    fast-search:    true
    rename-detect:  false   # spec is committed, will activate when this flips
  ```
- Overridable per-run via env: `<PREFIX>_E2E_FEATURES=fast-search,rename-detect` or `=all`.
- Unknown flags evaluate to `false` (skip), so specs can land before their flag does.
- The flag flip — not a YAML edit — is the moment a pending spec activates. PRs that ship a feature flip its flag in the same commit.

This is the mechanism that makes it safe to write specs *ahead of* the implementation. Without it, every aspirational spec is either a known-failing test (CI noise) or a comment in a backlog (rot).

---

## 11. Isolation & Cleanup

**Per-spec isolation invariants:**
1. The spec's `HOME` is a fresh `mkdtemp` directory.
2. The work dir lives under that `HOME` (`<HOME>/work`), so a single `RemoveAll(HOME)` cleans everything.
3. `PATH` is rewritten to put the test bin dir first.
4. Git config is detached from the developer's system (`GIT_CONFIG_NOSYSTEM=1`, explicit author/committer envs).
5. Color/paging is disabled (`TERM=dumb`).

**Cleanup is registered with `t.Cleanup`** so it runs even if the test panics — but only **after** the events have been drained, since a TUI may still want to inspect the work dir.

**Cleanup assertions** are about the CLI's own promises:
- `assert_no_residue: true` — declarative tag for "the suite should verify no files leaked outside the work dir". Implementation can scan or no-op; the field reserves the contract.
- `assert_files_removed: [path, ...]` — explicit per-path checks, e.g. that `clean` actually removed a config file.

**What we explicitly do not do:** cleanup does not run on spec failure if the work dir is interesting for debugging. The TUI keeps it alive; a CI run removes it at the end. This trade-off favors developer ergonomics.

---

## 12. Event Stream

The runner is a pure event producer. Consumers receive a stream of:

```
spec_start  { wave, spec, workDir, homeDir, binDir, env }
step_start  { wave, spec, phase, step, command }
step_done   { wave, spec, phase, step, command, stdout, stderr,
              exitCode, durationMs, assertions[], passed }
spec_end    { wave, spec, passed, durationMs, error? }
```

**Two event consumers in practice:**

1. **The `go test` driver** in `testdriver.Run` — drains the channel, calls `t.Logf` for each step, `t.Errorf` for each failed assertion, `t.Fatal` if `RunSpec` returned an error.
2. **An interactive TUI** — same channel, real-time list updates, focused step viewer, sidecar shell. (Not shipped by the kit; consumers can build their own.)

**The channel is closed by the runner when the spec finishes.** Consumers must drain to completion before reading sentinel state (e.g. `homeDir` for cleanup registration). A buffered channel keeps the runner from blocking on a slow consumer.

---

## 13. Test Discovery & Filtering

**Discovery**: `LoadSpecs(specsDir)` walks the directory tree, picks up every `*.yaml`, derives the wave from the first path segment under `specsDir`, parses the file, returns a stable-sorted slice.

**Sort order** within the global list:
1. Wave (string compare on directory name).
2. `Order` (unset = max-int, so ordered specs come first).
3. Name (alphabetic tiebreak).

**Filtering** is delegated to `go test -run`. Subtest names are `<wave>/<name>`:

```
go test ./tests/e2e/ -run 'TestE2E/wave3'                   # one wave
go test ./tests/e2e/ -run 'TestE2E/.*/journey-'             # naming convention
go test ./tests/e2e/ -run 'TestE2E/wave3/risk-classification'  # one spec
```

**Tag-based filtering** is intentionally not built into the harness. If needed, layer a small filter binary that reads the YAML and prints names matching, then feeds them back to `-run`.

---

## 14. Parallelism

- Each spec calls `t.Parallel()` immediately after the requirement check.
- `go test` caps parallel subtests at `GOMAXPROCS`; override with `-parallel N`.
- Each spec is independent (separate `HOME`, fresh repo, no shared DB), so true parallelism is safe.
- The fixture copy is per-spec and dirt-cheap (`cp -a` on small trees).

---

## 15. Multi-Agent Ingestion

The kit ships a single canonical surface for AI agents: a directory of skills under `.claude/skills/<name>/SKILL.md` (the [Agent Skills](https://agentskills.io) standard format — Markdown with YAML frontmatter declaring `name` and `description`). Vercel Labs' [`npx skills`](https://github.com/vercel-labs/skills) installs these into target projects across 40+ AI agents from one command.

| Agent | Surface in target project | Behavior |
|---|---|---|
| **Claude Code** | `.claude/skills/<name>/SKILL.md` | Native skill discovery; user invokes by name or via natural-language match against the `description`. |
| **OpenCode** | `.opencode/skills/<name>/SKILL.md` (mirrored from `.claude/`) | Native skill discovery. OpenCode also reads `.claude/skills/` directly as a fallback. |
| **OpenAI Codex CLI** | `AGENTS.md` (project root) | Codex auto-loads `AGENTS.md`; the kit appends a paragraph naming the kit, pointing at the skills directory and at this SPEC.md. No skill primitive — the agent reads the SKILL.md procedurally. |
| **Cursor** | `.cursor/rules/gotit.mdc` (optional, opt-in via bootstrap prompt) | Auto-attaches when `tests/e2e/**` files are edited. Provides authoring conventions, not commands. |
| **Aider** | `CONVENTIONS.md` (optional, opt-in) | A snippet appended to CONVENTIONS.md with the spec contract and assertion vocabulary. |
| **Any other** | `SPEC.md` + `AGENTS.md` | Any agent that reads Markdown can author conforming specs by reading this file. |

**Authoring decision flow** (what an agent does when asked to write a new spec):

1. Read `tests/e2e/runner_test.go` to learn the project's `EnvPrefix`, registered helpers, registered requirements, and registered custom assertions.
2. Read `tests/e2e/features.yaml` to learn what flags exist.
3. Pick the wave (existing or new).
4. Author the YAML following §5; pick assertions from §8; capture per §7.
5. Run only the new spec to confirm it passes (or skips on a documented flag).
6. Render or update the sidecar `.md` notes file.

When uncertain about format details, agents should read this SPEC.md directly rather than guessing from training data.

---

## 16. Extension Points

When the suite grows, these are the seams:

| Extension | Where it goes |
|---|---|
| New assertion type | One entry in `Config.Assertions` (project-local, `x-` prefix). |
| New requirement type | One entry in `Config.RequirementCheckers`. |
| New repo helper | One function + entry in `Config.RepoHelpers`. |
| New event consumer | Subscribe to the same channel; do not modify the runner. |
| New output format | New consumer that drains the same channel. The JSONL writer is exactly this. |
| New feature flag | One line in `features.yaml`; specs gate themselves with `feature:<name>`. |

Things that resist being made pluggable: the spec schema, isolation invariants, the binary-build step. Plugins on these turn the suite into a framework; the value of the suite comes from being opinionated.

---

## 17. TUI Surface Contract

The kit ships a reference TUI binary (`gotit`) that consumes Layer 1 (specs) and Layer 2 (runner) through stable, well-known contracts. Alternative TUIs (web UIs, IDE plugins, vim integrations) can plug in by reading the same contracts.

### How `gotit` finds a project

1. Walks up from CWD until it finds `go.mod` — that's the project root.
2. Reads `<cwd>/gotit.yaml` if present; otherwise `<projectRoot>/tests/e2e/gotit.yaml`. Schema: `schema/gotit-config.schema.json`.
3. Falls back to env vars `GOTIT_BINARY` / `GOTIT_BUILD_PATH` / `GOTIT_ENV_PREFIX` if no config file is found.

### Subcommands

| Command | Behavior |
|---|---|
| `gotit` | Interactive TUI in the current Go module. Requires a TTY. |
| `gotit init` | Scaffold `tests/e2e/` (the human counterpart of the `gotit-bootstrap` skill). |
| `gotit run [pattern]` | Headless `go test ./tests/e2e/ -run "TestE2E/<pattern>"`. CI-friendly. |
| `gotit doctor` | Environment-detection report. |

### Execution path

The TUI does not import the consumer's helpers, requirement checkers, or custom assertions — those are registered in the consumer's `runner_test.go`. Execution flows through `go test`:

1. The TUI invokes `go test ./tests/e2e/ -run "TestE2E/<wave>/<name>" -v` in the project root.
2. It sets two env vars:
   - `GOTIT_RESULTS_FILE=<absolute-path>` — the runner's `JSONLLogger` writes to that exact path instead of generating a timestamped name.
   - `GOTIT_KEEP_HOMEDIR=1` — the runner's testdriver skips temp-HOME cleanup so the TUI can shell into the spec's preserved directory.
3. The TUI tails the JSONL file as the test runs, parsing each line into a `runner.SpecEvent`.
4. On TUI exit, the TUI reaps every preserved temp HOME.

The runner honors both env vars conditionally: when unset, behavior is identical to v0.1 (timestamped JSONL, normal cleanup). The CI surface is unaffected.

### Environment detection

The TUI degrades gracefully where it can:

- **No TTY** → bare `gotit` errors with a hint to use `gotit run`. Hard requirement.
- **Terminal < 80×24** → error with a resize hint. Hard requirement when TTY is present.
- **No `go` on PATH** → hard error.
- **PTY unavailable** (e.g. Windows without ConPTY) → embedded shell pane is hidden; the step viewer shows a "use external terminal" hint. Soft.
- **glamour render fails** → notes pane falls back to plain markdown. Soft.

`gotit doctor` reports each as ✓ / ✗ / !.

### What this contract guarantees

- **Spec-major-version stability**: the env-var names, JSONL line shapes, and config-file schema follow `spec_version`. A v1 TUI works against any v1 runner.
- **Alternative TUIs are first-class**: anything that can spawn `go test`, set the two env vars, and tail JSONL can be a TUI. The reference `gotit` binary is one implementation, not the only one.

---

## 18. Anti-Patterns

- **In-process testing of `func main()`.** Misses the binary build, flag parsing edge cases, and panic-on-startup bugs. Always exec the built binary.
- **Shared databases or fixtures across specs.** Even if it's "read-only", the next person will mutate it. Pay the copy cost.
- **Asserting on full stdout via `==`.** Brittle to formatting tweaks. Prefer `contains` / `json_path` / `regex`. `golden_file` is acceptable when the output is intentionally stable and the file is checked in.
- **Hardcoding `/tmp` or absolute paths.** Use the runner's temp dirs; resolve the project root by walking up to `go.mod`.
- **Cleanup steps that mutate state.** Cleanup is for **assertion**, not for "fixing" the spec's mess. The temp HOME wipe handles that.
- **YAML anchors and includes for spec reuse.** They obscure what the test actually does. Prefer one self-contained spec per file; if two specs share enough setup to feel duplicative, lift the setup into a repo helper.
- **Tag-matrix explosions.** Tags are search aids, not test parameters.
- **Conditional skips inside spec steps.** If a step needs a precondition, it belongs in `requires:` or `setup:`.
- **Forking the runner.** If you need new behavior, register an extension. If the extension surface is insufficient, file an issue against the kit; don't drift.

---

## 19. Glossary

- **Kit** — the gotit repo, providing the spec, runtime module, and skills.
- **Target project** — the Go CLI repo that consumes the kit.
- **Spec** — a YAML file describing one scenario.
- **Step** — one CLI invocation inside a spec.
- **Phase** — `setup`, `test`, or `cleanup`. Distinguishes setup-failures (abort) from assertion-failures (continue and report).
- **Wave** — a directory of specs grouped by release scope.
- **Fixture** — a static repo tree under `<FixturesDir>/`.
- **Helper** — a Go function that programmatically builds a repo.
- **Requirement** — an opaque tag the harness checks before running a spec; either `git`, a feature flag, or a name registered in `RequirementCheckers`.
- **Capture** — extracting a value from step stdout into the spec's variable map.
- **Notes** — sidecar markdown documenting the *why* of a spec.
- **Skill** — a Markdown procedure under `.claude/skills/` consumed by AI agents.
- **Adapter** — a per-agent integration file (e.g. `.cursor/rules/gotit.mdc`) that points the agent at the canonical skills.

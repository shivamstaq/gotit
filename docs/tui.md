# `gotit` — Interactive TUI

The headline interactive surface of the gotit kit. Browses, runs, and inspects every spec in a project's `tests/e2e/` suite. Uses `go test` under the hood for execution, so it picks up every helper, requirement checker, and custom assertion the consumer has registered in their `runner_test.go`.

For the YAML spec format, assertion types, and capture syntax, see [SPEC.md](../SPEC.md). For installation in agent ecosystems, see [gotit-tui SKILL.md](../.claude/skills/gotit-tui/SKILL.md).

---

## Installation

```bash
go install github.com/shivamstaq/gotit/cmd/gotit@latest
```

One install per developer; works across every Go project that has gotit set up.

Confirm:

```bash
gotit version
gotit doctor
```

`gotit doctor` reports each environment prerequisite as `✓` / `✗` / `!`.

---

## Subcommands

| Command | Purpose |
|---|---|
| `gotit` | Interactive TUI in the current Go module. Requires a TTY. |
| `gotit init` | Scaffold `tests/e2e/` for this project. |
| `gotit run [pattern]` | Headless `go test ./tests/e2e/ -run "TestE2E/<pattern>"`. CI-friendly. Forwards trailing flags after `--` to `go test`. |
| `gotit doctor` | Environment-detection report. |
| `gotit version` | Print version. |
| `gotit help` | Usage. |

---

## Layout

```
┌─────────────────┬───────────────────────────────────────────────────┐
│  Test list      │  Right pane (one of three at a time)              │
│  (left)         │                                                   │
│                 │  ┌ wave3/risk-class ─────────────────────────────┐│
│  wave1          │  │ Description: ...                              ││
│  ⊘ smoke-…     │  │ Tags: wave3, risk                             ││
│                 │  │ Steps: 4                                      ││
│  wave3          │  │                                               ││
│  ✓ risk-class…  │  │ [setup] index ✓ PASSED 36ms                   ││
│  ✗ flaky-…      │  │ [test]  search ✓ PASSED  7ms                  ││
│  ● running…     │  │ [test]  validate ✗ FAILED — assertion (...)   ││
│  ◎ queued       │  │ ...                                           ││
│                 │  └───────────────────────────────────────────────┘│
├─────────────────┴───────────────────────────────────────────────────┤
│ ✓ 22  ✗ 1  (23/100 matching)     ↑↓ nav  ⏎ view  r run  n notes  q  │
└─────────────────────────────────────────────────────────────────────┘
```

The right pane shows one of three views depending on focus: the **step viewer**, the **embedded PTY shell** (green border), or the **notes viewer** (green border).

**Status markers**: `○` pending · `◎` queued · `●` running (spinner) · `✓` passed · `✗` failed · `⊘` skipped (requirement not met).

---

## Keybindings

### Test list (default focus)

| Key | Action |
|---|---|
| `↑` / `k` | Move cursor up |
| `↓` / `j` | Move cursor down |
| `⏎` | View selected test in step viewer |
| `tab` | Toggle focus to step viewer |
| `r` | Run selected test |
| `a` | Run all (or filtered subset) |
| `s` | Stop the running test |
| `R` | Reset every test to pending |
| `/` | Open search/filter |
| `n` | Open sidecar notes for selected spec |
| `q` / `ctrl+c` | Quit |

### Step review

| Key | Action |
|---|---|
| `←` / `h` | Previous step |
| `→` / `l` | Next step |
| `↑` / `k` | Scroll output up |
| `↓` / `j` | Scroll output down |
| `⏎` | Open embedded shell in the spec's temp work dir |
| `r` | Re-run this test |
| `n` | Open notes |
| `esc` | Back to test list |
| `q` | Quit |

### Embedded shell

| Key | Action |
|---|---|
| `ctrl+\` | Exit shell, return to step review |
| (other) | Forwarded to the shell |

The shell launches `$SHELL` inside the spec's preserved temp HOME. The runner sets `GOTIT_KEEP_HOMEDIR=1` while the TUI is open, so the directory survives test exit. The TUI cleans it up when you quit or run `R`.

### Notes viewer

| Key | Action |
|---|---|
| `n` | Toggle closed |
| `↑` `↓` `PageUp` `PageDown` `Home` `End` | Scroll |

Sidecar `.md` files render through glamour. Set `GOTIT_TUI_NOTES_PLAIN=1` to bypass glamour, or `GOTIT_TUI_NOTES_STYLE=<style>` (default `dark`) to change the theme.

### Search

Type `/` in the test list to open a substring filter that matches against spec name, description, tags, and wave.

| Key | Action |
|---|---|
| `⏎` | Apply filter (stays active) |
| `esc` | Clear filter and close |
| (other) | Edit the query |

---

## Per-project config — `gotit.yaml`

The TUI looks for `gotit.yaml` in this order:

1. `<cwd>/gotit.yaml` (a suite living in its own subdirectory).
2. `<projectRoot>/tests/e2e/gotit.yaml` (the conventional location).

Schema: [`schema/gotit-config.schema.json`](../schema/gotit-config.schema.json). Required fields:

```yaml
binary_name: mytool
build_path: ./cmd/mytool
env_prefix: MYTOOL
```

Optional (defaults shown):

```yaml
specs_dir: tests/e2e/specs
fixtures_dir: tests/e2e/testdata/repos
features_file: tests/e2e/features.yaml
results_dir: tests/e2e/results
```

If no config file is present, the TUI accepts `GOTIT_BINARY` / `GOTIT_BUILD_PATH` / `GOTIT_ENV_PREFIX` env vars instead.

---

## Architecture

The TUI does not import the consumer's helpers, requirement checkers, or custom assertions — those are registered in the consumer's `runner_test.go`, and the `gotit` binary is a global tool installed once per developer.

Execution flows through `go test`:

1. The TUI invokes `go test ./tests/e2e/ -run "TestE2E/<wave>/<name>" -v` in the project root.
2. It sets two env vars:
   - `GOTIT_RESULTS_FILE=<absolute-path>` — the runner's `JSONLLogger` writes to that exact path.
   - `GOTIT_KEEP_HOMEDIR=1` — the runner's `testdriver` skips temp-HOME cleanup.
3. The TUI tails the JSONL file as the test runs, parsing each line into a `runner.SpecEvent`.
4. On TUI exit, it reaps every preserved temp HOME.

When the env vars are unset (i.e. plain `go test`), the runner's behavior is identical to v0.1: timestamped JSONL under `tests/e2e/results/`, normal `t.Cleanup` removal of temp HOMEs.

---

## Environment variables

| Variable | Default | Effect |
|---|---|---|
| `GOTIT_BINARY` | _(unset)_ | Override `binary_name` from `gotit.yaml`. |
| `GOTIT_BUILD_PATH` | _(unset)_ | Override `build_path`. |
| `GOTIT_ENV_PREFIX` | _(unset)_ | Override `env_prefix`. |
| `GOTIT_KEEP_HOMEDIR` | _(unset)_ | When `=1`, `testdriver.Run` skips temp-HOME cleanup. The TUI sets this. |
| `GOTIT_RESULTS_FILE` | _(unset)_ | When set, `NewJSONLLogger` writes to that exact path instead of generating a name. |
| `GOTIT_TUI_NOTES_STYLE` | `dark` | glamour style for the notes viewer (`dark`, `light`, `notty`, `dracula`, `pink`, or an absolute path to a `.json` style file). |
| `GOTIT_TUI_NOTES_PLAIN` | _(unset)_ | When `=1`, bypass glamour and show raw markdown. |
| `SHELL` | `/bin/bash` | Shell binary used by the embedded shell. |

---

## Troubleshooting

**`environment check failed`** — `gotit doctor` will tell you which check failed.
- *stdout is a TTY: no* → don't pipe `gotit` to a file; use `gotit run` for headless mode.
- *terminal size: too small* → resize to ≥ 80×24.
- *`go` on PATH: not found* → install Go (https://go.dev/dl/).

**Notes pane appears empty** — first open of any notes triggers glamour/chroma lazy-loading (~100 ms, warmed at startup). If it persists, try `GOTIT_TUI_NOTES_PLAIN=1`. If your terminal is unusual, try `GOTIT_TUI_NOTES_STYLE=notty`.

**Shell exits immediately** — the spec's temp HOME was already cleaned. Re-run the test (`r`) first.

**TUI doesn't launch / "no go.mod found"** — make sure CWD is inside a Go module. The binary searches upward for `go.mod` and fails if none is found.

**Windows users** — the embedded shell pane is unavailable (no ConPTY support yet). The TUI hides it and shows guidance to use an external terminal: `cd <work-dir>` (path shown in the JSONL `spec_start` record under `<results-dir>/`). Everything else works.

---

## Related

- [SPEC.md §17](../SPEC.md#17-tui-surface-contract) — the cross-tool TUI contract.
- [gotit-tui SKILL.md](../.claude/skills/gotit-tui/SKILL.md) — agent-facing instructions.
- [gotit-diagnose-failure SKILL.md](../.claude/skills/gotit-diagnose-failure/SKILL.md) — TUI-driven failure debugging.

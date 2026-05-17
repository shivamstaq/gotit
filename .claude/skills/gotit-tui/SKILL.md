---
name: gotit-tui
description: Install and use the gotit interactive TUI — the headline surface for browsing, running, and debugging gotit E2E specs. Invoke when the user wants to "open the test runner", "run the e2e tests interactively", "debug a failed spec", or asks how to install the gotit binary.
version: 1
---

# gotit-tui

The `gotit` binary is the interactive test runner — the daily-driver counterpart of plain `go test`. It walks tests/e2e/, lists every spec by wave, runs them on demand, streams step output live, and opens an embedded shell into the spec's preserved temp HOME for debugging.

## Installation

```
go install github.com/shivamstaq/gotit/cmd/gotit@latest
```

Confirm:

```
gotit version
gotit help
```

The binary works inside any Go project that has gotit set up (a `tests/e2e/runner_test.go` plus a `tests/e2e/gotit.yaml` config). One installation per developer; works across all their projects.

## Subcommands

| Command | What it does |
|---|---|
| `gotit` | Launch the interactive TUI. Requires a TTY. |
| `gotit init` | Scaffold `tests/e2e/` in the current Go module (human path of `gotit-bootstrap`). |
| `gotit run [pattern]` | Headless `go test -run TestE2E/<pattern>` wrapper. CI-friendly. |
| `gotit doctor` | Environment-detection report (TTY, terminal size, PTY support, `go`, glamour). |
| `gotit version` | Print the installed version. |
| `gotit help` | Usage. |

## When to use what

- **`gotit`** — local development, debugging a failing spec. The TUI is the fastest way to inspect step-by-step output and shell into a preserved work dir.
- **`gotit run`** — CI, scripts, or any non-interactive context. Forwards extra flags to `go test`: `gotit run wave1 -- -count=1 -race`.
- **`gotit doctor`** — first run on a new machine, or when the TUI misbehaves. Reports every prerequisite as ✓/✗/!.
- **plain `go test ./tests/e2e/`** — also still works. The TUI doesn't replace it; it's a richer interface.

## TUI keybindings

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

**Daemon entries** appear inline in the timeline with phase `daemon` and step names like `[daemon] echo starting`, `[daemon] echo ready`, `[daemon] echo stopped`. They carry the daemon PID, signal sent at teardown, and the last ~4KB of captured stdout+stderr. Failed clean shutdown shows up as a `[daemon] X stopped` entry with `passed=false`; failed log assertions appear in the same entry's assertion list. Mid-spec crashes appear as `[daemon] X died` and short-circuit the remaining test steps.

For the daemon's full unbounded log, open the embedded shell and read `$HOME/.gotit-daemons/<name>.log` — the TUI sets `GOTIT_KEEP_HOMEDIR=1` so the file survives test exit.

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

## Per-project config — `tests/e2e/gotit.yaml`

```yaml
binary_name: mytool          # required
build_path: ./cmd/mytool     # required
env_prefix: MYTOOL           # required (uppercase)
# specs_dir: tests/e2e/specs                # optional
# fixtures_dir: tests/e2e/testdata/repos    # optional
# features_file: tests/e2e/features.yaml    # optional
# results_dir: tests/e2e/results            # optional
```

`gotit init` writes this file with sensible defaults. The TUI also accepts `GOTIT_BINARY` / `GOTIT_BUILD_PATH` / `GOTIT_ENV_PREFIX` env vars when the file is absent.

## Troubleshooting

- **"environment check failed"** — `gotit doctor` will tell you which check failed. Common ones:
  - `stdout is a TTY: no` → don't pipe `gotit` to a file; use `gotit run` for headless mode.
  - `terminal size: too small` → resize to ≥ 80×24.
  - `go on PATH: not found` → install Go.
- **"no specs found"** — there's no YAML under `tests/e2e/specs/`. Run `gotit-add-spec` or `gotit init` to scaffold the first one.
- **Notes pane empty** — the sidecar `.md` doesn't exist yet. The TUI auto-creates it from the template on first open.
- **Shell exits immediately** — the test's temp HOME was already cleaned. Re-run the test (`r`) first; the shell only works on a freshly-completed run.
- **Windows users**: the embedded shell pane isn't supported yet (no ConPTY). Everything else works. To inspect a spec's preserved HOME, open a separate terminal: `cd <work-dir>` (path shown in the spec's spec_start record under `tests/e2e/results/`).

## Architecture (for the curious)

The TUI does not import the consumer's helpers/checkers/custom assertions — those are registered in the project's `runner_test.go`. Instead, "run this spec" → `go test -run TestE2E/<wave>/<name>` is invoked under the hood with `GOTIT_RESULTS_FILE=<tmp>.jsonl` and `GOTIT_KEEP_HOMEDIR=1`. The runner inside that process writes JSONL events to the file; the TUI tails it. See [SPEC.md §17](https://github.com/shivamstaq/gotit/blob/main/SPEC.md#17-tui-surface-contract).

## References

- TUI doc: https://github.com/shivamstaq/gotit/blob/main/docs/tui.md
- Spec contract: https://github.com/shivamstaq/gotit/blob/main/SPEC.md#17-tui-surface-contract
- Config schema: https://github.com/shivamstaq/gotit/blob/main/schema/gotit-config.schema.json

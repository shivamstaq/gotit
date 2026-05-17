---
name: gotit-add-daemon-spec
description: Author one YAML spec under tests/e2e/specs/<wave>/ that exercises a daemon/server/long-running process — with readiness polling, clean-shutdown verification, and optional log assertions. Invoke when the user asks to "write an e2e test for the server", "spec the daemon", "test the watch subcommand", "verify graceful shutdown", or any test that needs a process running in the background while other steps run.
version: 1
---

# gotit-add-daemon-spec

Author a spec that uses gotit's daemon lifecycle. The runner spawns the process, polls until it's ready, exercises it via normal steps, then sends SIGTERM and verifies clean exit — all without `sh -c '... &'` hacks or leaked PIDs.

> When the test is purely a synchronous CLI invocation, use `gotit-add-spec` instead. This skill is for processes that must be **running** while the test acts on them.

## Prerequisites

- The suite is already bootstrapped (`tests/e2e/runner_test.go` exists).
- The CLI under test has a subcommand that runs in the foreground until signaled (e.g. `myapp serve`, `myapp watch`, `myapp daemon`). Daemons that double-fork themselves into the background require the parent to exit promptly and write a PID file — handle with `ready: { pid_file: ... }` and `stop.signal: TERM` to that PID.

## Decision: top-level `daemons:` vs `background: true`

Both surfaces share the same lifecycle code. Pick by what reads more naturally:

| Use top-level `daemons:` block when | Use `background: true` step when |
|---|---|
| The daemon is part of the spec's environment, named like "the server" or "the worker". | The daemon naturally starts mid-sequence ("do A → start worker → do B"). |
| You want explicit `phase: pre_setup` so setup steps can talk to it. | The daemon starts after some prior step writes its config. |
| You'll reference the daemon's captured stdout via `capture:` to extract a port or token. | One-off; nothing later needs the daemon's own captures. |
| Multiple steps interact with it; lifecycle is conceptually separate from the test actions. | The "start" itself is one of the test actions. |

When in doubt, prefer top-level `daemons:`. It scales better as the spec grows.

## Decision: readiness mode

Pick **one** of `tcp`, `log_contains`, `command`, `pid_file`. Empty `ready:` means "started == ready" (no wait) — only use this for fire-and-forget processes.

| Mode | When to pick |
|---|---|
| `tcp: "host:port"` | The daemon binds a known port. Fastest, lowest-friction. Use `127.0.0.1:<port>` for local binds. |
| `log_contains: "<substring>"` | The daemon prints a stable startup marker (e.g. "listening on", "ready"). Robust to dynamic ports — pair with `capture:` to extract the port from the same line. |
| `command: "<probe>"` | The daemon ships its own health subcommand or there's a natural CLI probe (e.g. `myapp status`, `pg_isready`). Probe exits 0 when ready. |
| `pid_file: "<path>"` | The daemon writes its PID once it's done initializing. Common for double-forking daemons. |

If the daemon is auto-port (`--port 0`), prefer `log_contains` so you can capture the port from the same line that announces readiness.

## Decision: stop semantics

Defaults are usually right. Override only when:

- **`signal:`** — most daemons handle TERM. Use `INT` for daemons that explicitly document SIGINT shutdown. Use `HUP` only for reload-not-stop tests (rare). Don't use KILL — that's the runner's escalation, not your initial signal.
- **`grace:`** — default 5s. Bump if the daemon does slow cleanup (flush a queue, close DB connections). Drop to 1-2s if you want fast feedback that shutdown is broken.
- **`expected_exit:`** — default 0. Set to a specific non-zero only when the daemon documents non-zero clean exit (rare; usually a smell).

## Decision: log assertions

Add `log_assert:` when there's a daemon-side behavior you want to verify that doesn't surface in step output:

- **"Logged graceful shutdown"** — `{ type: contains, value: "<marker>" }`. Proves the daemon ran its cleanup path, not just that it exited 0.
- **"Never logged PANIC / ERROR"** — `{ type: not_contains, value: "PANIC" }`. Catches in-flight errors that don't fail the daemon process.
- **"Connection count matches"** — `{ type: regex, pattern: "served \\d+ requests" }`. Validates throughput-side behavior.

Skip `log_assert:` if the daemon's stop semantics + step assertions already cover everything.

## Steps

1. **Pick wave + name** (same as `gotit-add-spec`).

2. **Choose the surface** using the matrix above.

3. **Write the spec**. Templates below.

### Template A — top-level `daemons:` block (recommended default)

```yaml
# yaml-language-server: $schema=https://github.com/shivamstaq/gotit/raw/main/schema/spec.schema.json
spec_version: 1
name: <kebab-case>
description: <one-line summary including "the daemon" or equivalent>
tags: [<wave>, daemon]
order: <next>
requires: [git]  # add 'curl' if you use curl as the client; register a checker in helpers/

# repo: { fixture: <name> }   # if the daemon reads from a repo

daemons:
  - name: server
    # Use sh -c '...' if the command references $HOME or other env that needs shell expansion.
    # exec resolves the binary directly — $VAR substitution happens in shell only.
    command: sh -c '<binary> serve --port-file $HOME/port'
    phase: post_setup           # or pre_setup if setup needs the daemon up
    start_timeout: 10s
    ready:
      log_contains: "listening on"
      timeout: 5s
    capture:
      port: "regex:listening on port (\\d+)"
    stop:
      signal: TERM
      grace: 5s
      expected_exit: 0
    log_assert:
      - { type: contains,     value: "graceful shutdown" }
      - { type: not_contains, value: "PANIC" }

steps:
  - name: ping
    command: curl -sf http://127.0.0.1:{{ port }}/health
    assert:
      - { type: exit_code, expected: 0 }
      - { type: contains, value: "ok" }
```

### Template B — `background: true` inline step

```yaml
spec_version: 1
name: <kebab-case>
description: <one-line summary>
tags: [<wave>, daemon, background]
order: <next>
requires: [git]

steps:
  - name: seed
    command: <binary> seed-data

  - name: start-worker
    background: true
    command: sh -c '<binary> worker --queue jobs'
    ready:
      log_contains: "worker ready"
      timeout: 5s
    stop:
      signal: TERM
      grace: 3s
    # log_assert: optional; same semantics as the top-level block
    # capture / assert: FORBIDDEN on background steps

  - name: enqueue
    command: <binary> enqueue task-1
    assert:
      - { type: exit_code, expected: 0 }
```

## Common pitfalls

- **`$HOME` doesn't expand** — `exec` invokes the binary directly. Wrap with `sh -c '...'` when you need shell variables (HOME, dynamic paths).
- **`assert:` and `capture:` on background steps** — schema rejects this. Move the daemon to a top-level `daemons:` block where `capture:` is supported, or capture from a separate non-background step that reads the port file / curl's a status endpoint.
- **Daemon that double-forks** — gotit tracks the immediate child via process group. If the daemon's main exits and a fork lives on, the process group survives and SIGTERM still reaches it. But the parent's exit will trigger `daemon_died` (parent exited unexpectedly). Solution: pass a `--no-fork` / `--foreground` flag if the CLI has one; otherwise use `ready: { pid_file: ... }` and accept that the daemon process tree is decoupled from its starting PID.
- **Forgetting `expected_exit: 0`** — it's the default, but be explicit if the test's whole point is "shutdown is clean". Makes the intent legible to the reader.
- **Setting `ready.timeout` too tight** — readiness polling adds overhead; daemons may take 1-2s to bind ports under load. Default 5s is forgiving; only drop below 2s if you're specifically testing fast-start behavior.
- **Polling external state by hand** — don't write `sh -c 'for i in 1..10; do curl ...; done'` inside a step. Use `ready: { command: ... }` so the runner owns the polling and surfaces it as a daemon failure when it times out.

## Verify

```
go test ./tests/e2e/ -run "TestE2E/<wave>/<name>" -v -timeout 60s
```

Look for these JSONL records in `tests/e2e/results/run-*.jsonl`:

- `"type":"daemon_start"` with a non-zero PID.
- `"type":"daemon_ready"` with `duration_ms` < `start_timeout`.
- `"type":"daemon_stop"` with `clean_exit: true` and (if set) the log_assertions all passing.

If `daemon_died` appears mid-spec, the daemon crashed — `gotit-diagnose-failure` will help.

## References

- Daemon lifecycle: https://github.com/shivamstaq/gotit/blob/main/SPEC.md#65-daemon-lifecycle
- Example specs: `examples/demo-suite/specs/wave2-daemons/` in the gotit repo.
- Schema: `daemon`, `ready_check`, `stop_spec` defs in `schema/spec.schema.json`.

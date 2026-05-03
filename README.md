# gotit

> **Go Test It!** — a portable end-to-end test kit for Go CLIs.

A YAML-driven, isolated, parallel E2E suite for any Go CLI, built in three composable layers:

```
Layer 3 — TUI                 binary `gotit` (interactive, dev-only)
            ▲                 panes for test list, step output, embedded shell, sidecar notes
            │                 wraps `go test` + tails JSONL for live events
Layer 2 — Runner              Go module imported by your runner_test.go
            ▲                 pure CI surface, no UI deps, runs unchanged via `go test`
            │
Layer 1 — Specs               YAML files, fixtures, helpers, feature flags
                              consumer-owned; same format consumed by both runner and TUI
```

CI uses **plain `go test`** (Layer 2). Daily-driver development uses **`gotit`** (Layer 3). Both surfaces consume the same Layer 1 specs.

11 installable skills (Claude Code / OpenCode / Codex / Cursor / Aider) author, run, and diagnose specs.

---

## 60-second start

**1. Install the skills + scaffold the suite** (in your Go CLI's repo root):

```bash
npx skills add shivamstaq/gotit
```

Then ask your AI agent: *"Set up gotit E2E tests for this project."* The `gotit-bootstrap` skill takes it from there.

**No AI agent?** Install the binary and scaffold manually:

```bash
go install github.com/shivamstaq/gotit/cmd/gotit@latest
gotit init                              # scaffold tests/e2e/
gotit doctor                            # confirm environment is ready
gotit run wave1                         # headless run, like `go test`
gotit                                   # interactive TUI
```

**Already have specs and just want the TUI?**

```bash
go install github.com/shivamstaq/gotit/cmd/gotit@latest
cd <your-project>
gotit
```

---

## What you get

- **One YAML file = one E2E scenario.** No Go boilerplate per test.
- **Full isolation per spec.** Temp `HOME`, temp work dir, fresh git repo, scoped `PATH`. Tests can't leak into your dev environment or each other.
- **8 assertion types**: `exit_code`, `contains`, `not_contains`, `stderr_contains`, `regex`, `json_path`, `count`, `golden_file`. Custom types use the `x-` prefix and register with one map entry.
- **Variable capture between steps** via JSONPath or regex; reference with `{{ var }}`.
- **Feature flags built in.** Land specs ahead of the implementation; flip the flag in the same PR that ships the feature.
- **Parallel by default.** Subtests via `t.Parallel()`, isolation invariants enforced by the runner.
- **JSONL run logs** for post-mortem and CI artifacts.
- **First-class agent integration.** 10 skills ship with the kit and install across Claude Code, OpenCode, Codex, Cursor, Aider, and 40+ other agents via `npx skills`.

---

## Three ways to consume

| Mode | When | How |
|---|---|---|
| **Library import** | Default. Most consumers. | `go get github.com/shivamstaq/gotit` and write a 25-line `runner_test.go`. |
| **Vendored** | Air-gapped, or you want zero dependencies. | `gotit-bootstrap --vendor` copies `runner/` into your project; no module dep. |
| **Forked** | You want to own the runner. | `git clone` this repo, customize, maintain your fork. |

---

## The 25-line wiring file

```go
package e2e

import (
    "testing"

    "github.com/shivamstaq/gotit/runner"
    "github.com/shivamstaq/gotit/runner/testdriver"
    "github.com/myorg/myproj/tests/e2e/helpers"
)

func TestE2E(t *testing.T) {
    testdriver.Run(t, runner.Config{
        BinaryName: "mytool",
        BuildPath:  "./cmd/mytool",
        EnvPrefix:  "MYTOOL",
        RepoHelpers: map[string]runner.RepoHelper{
            "two-commit-rename": helpers.TwoCommitRename,
        },
        RequirementCheckers: map[string]runner.RequirementChecker{
            "my-extractor": helpers.CheckMyExtractor,
        },
    })
}
```

That's it. `testdriver.Run` builds the binary, walks `tests/e2e/specs/`, dispatches every spec as a parallel subtest, and reports.

---

## A spec, end to end

```yaml
# yaml-language-server: $schema=https://github.com/shivamstaq/gotit/raw/main/schema/spec.schema.json
spec_version: 1
name: list-shows-versions
description: `mytool list --json` returns at least one version object
tags: [wave1, list]
order: 10
repo:
  fixture: minimal-repo

steps:
  - name: list_json
    command: mytool list --json
    capture:
      first_id: "$[0].id"
    assert:
      - type: exit_code
        expected: 0
      - type: count
        path: "$"
        op: ">="
        expected: 1

  - name: detail_for_first
    command: mytool show {{ first_id }}
    assert:
      - type: contains
        value: "Version:"
```

---

## The 11 skills

| Skill | What it does |
|---|---|
| `gotit-bootstrap` | One-shot setup of the suite in a Go CLI project (agent path; `gotit init` is the human path). |
| `gotit-tui` | Install + use the `gotit` interactive binary; keybindings; environment fallbacks. |
| `gotit-add-spec` | Author one YAML spec for one CLI feature. |
| `gotit-add-fixture` | Add a static repo fixture under `testdata/repos/`. |
| `gotit-add-helper` | Register a programmatic repo builder. |
| `gotit-add-assertion` | Add a project-local assertion type. |
| `gotit-add-feature-flag` | Land a spec for an unshipped feature behind a flag. |
| `gotit-diagnose-failure` | Open the TUI on a failed spec (or parse JSONL when no TUI). |
| `gotit-run-suite` | Pick the right run mode (`gotit` / `gotit run` / `go test`). |
| `gotit-port-from-bats` | Convert bats / expect / pytest suites to gotit YAML. |
| `gotit-ship-wave` | Open or close a wave; flip flags. |

Skills live at `.claude/skills/<name>/SKILL.md` and are mirrored to `.opencode/skills/`. `npx skills add shivamstaq/gotit` installs them into all supported agent locations in one command.

---

## Documentation

- **[SPEC.md](./SPEC.md)** — the cross-tool YAML contract. The agent-readable single source of truth.
- **[AGENTS.md](./AGENTS.md)** — behavioral contract for agents working in this kit repo.
- **[docs/design-rationale.md](./docs/design-rationale.md)** — why YAML, why JSONL, why hybrid distribution.
- **[docs/porting-guide.md](./docs/porting-guide.md)** — migrating from bats / expect / pytest.
- **[docs/extending.md](./docs/extending.md)** — adding assertion types, requirements, helpers.
- **[docs/changelog.md](./docs/changelog.md)** — spec version history.
- **[examples/demo-suite/](./examples/demo-suite/)** — a real consumer of the kit, dogfooded by CI.

---

## License

Apache 2.0. See [LICENSE](./LICENSE).

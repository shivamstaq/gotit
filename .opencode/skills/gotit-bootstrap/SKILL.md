---
name: gotit-bootstrap
description: One-shot setup of the gotit E2E test suite in a Go CLI project — wires runner_test.go, features.yaml, AGENTS.md, .gitignore, and a passing smoke spec. Invoke when the user asks to "add e2e tests", "set up gotit", or "scaffold end-to-end tests" in a Go project.
version: 1
---

# gotit-bootstrap

Set up the gotit E2E test suite in the current Go CLI project. Goal: from a vanilla Go module to one passing `go test ./tests/e2e/` invocation.

> **Human path**: users with the `gotit` binary installed (`go install github.com/shivamstaq/gotit/cmd/gotit@latest`) can run `gotit init` to scaffold the same files non-interactively. Both paths converge on identical outputs. Use the agent path when wiring up an unfamiliar project, when the user wants explanation, or when the binary isn't installed.

## Prerequisites

- Working directory is a Go module (`go.mod` exists at repo root).
- `go`, `git` on PATH.

## Inputs (ask the user only if you can't infer)

- **Binary name**. Detect from `cmd/<name>/` — the most common layout. If multiple binaries, ask which one to test.
- **Build path**. Default `./cmd/<binary>`.
- **Env prefix**. Uppercase the binary name and replace non-alphanumerics with `_`. Confirm only if unusual (e.g. one-letter binary).
- **Optional adapters**. Ask once: "Generate Cursor (.cursor/rules/) and/or Aider (CONVENTIONS.md) adapters?" Default: skip both.

## Steps

1. **Add the module dependency** — `go get github.com/shivamstaq/gotit@latest`.

2. **Render `tests/e2e/runner_test.go`** from the template at `https://github.com/shivamstaq/gotit/blob/main/templates/runner_test.go.tmpl`. Substitute BinaryName, BuildPath, EnvPrefix. Leave the helpers package import commented until the user actually needs helpers.

3. **Write `tests/e2e/gotit.yaml`** so the TUI can find this project's wiring. Template at `templates/gotit.yaml.tmpl`. Required fields: `binary_name`, `build_path`, `env_prefix`. The schema lives at `schema/gotit-config.schema.json`.

4. **Write `tests/e2e/features.yaml`** as an empty allowlist (template at `templates/features.yaml.tmpl`). Substitute the env-prefix into the comment.

4. **Write a smoke spec** at `tests/e2e/specs/wave1/smoke.yaml`:

   ```yaml
   # yaml-language-server: $schema=https://github.com/shivamstaq/gotit/raw/main/schema/spec.schema.json
   spec_version: 1
   name: smoke-version
   description: <binary> --version exits 0
   tags: [wave1, smoke]
   order: 10
   steps:
     - name: version
       command: <binary> --version
       assert:
         - type: exit_code
           expected: 0
   ```

   If the CLI exposes a `version` subcommand instead of `--version`, prefer that. The point is one trivial passing assertion.

5. **Append the AGENTS.md snippet** (from `templates/agents-md.snippet`) to the project's `AGENTS.md` (create if missing). Do not overwrite existing content; add a new `## End-to-end testing (gotit)` section if absent.

6. **Append `tests/e2e/results/`** to `.gitignore` if not already present.

7. **Optional adapters** (only if the user opted in):
   - Cursor: write `.cursor/rules/gotit.mdc` from `adapters/cursor/gotit.mdc.tmpl`.
   - Aider: append `adapters/aider/conventions.snippet` to `CONVENTIONS.md`.

8. **Run the smoke test**: `go test ./tests/e2e/ -run "TestE2E/wave1/smoke-version" -v`. Report the result. If it fails, surface the first assertion error verbatim — do not attempt to "fix" the user's CLI.

## Vendoring mode (optional)

If the user explicitly asks to vendor the runner instead of taking a module dependency, pass `--vendor`:

- Clone `github.com/shivamstaq/gotit` to a temp dir.
- Copy `runner/` into `tests/e2e/internal/runner/`.
- Rewrite imports in the rendered `runner_test.go` from `github.com/shivamstaq/gotit/runner` to `<consumer-module>/tests/e2e/internal/runner`.
- Skip `go get`.

## Outputs

- `tests/e2e/runner_test.go`
- `tests/e2e/gotit.yaml`
- `tests/e2e/features.yaml`
- `tests/e2e/specs/wave1/smoke.yaml`
- `AGENTS.md` (appended)
- `.gitignore` (appended)
- `go.mod` (require line for gotit)
- Optional: `.cursor/rules/gotit.mdc`, `CONVENTIONS.md`

After bootstrapping, suggest the user install the TUI binary for the daily-driver experience:

```
go install github.com/shivamstaq/gotit/cmd/gotit@latest
```

## References

- Spec: https://github.com/shivamstaq/gotit/blob/main/SPEC.md
- Schema: https://github.com/shivamstaq/gotit/blob/main/schema/spec.schema.json
- Templates: https://github.com/shivamstaq/gotit/tree/main/templates

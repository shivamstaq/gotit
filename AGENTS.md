# AGENTS.md — gotit kit repo

Behavioral contract for AI coding agents working **inside this repo** (the kit itself). For agents working in a *consumer's* repo that has gotit installed, see the snippet at `templates/agents-md.snippet`.

## What this repo is

`gotit` is a portable end-to-end test kit for Go CLIs. It ships:

- **`SPEC.md`** — the cross-tool YAML spec contract. The single source of truth for the format.
- **`runner/`** — a Go module (semver-stable public API) consumers import.
- **`schema/`** — JSON Schemas for `spec.yaml` and `features.yaml`.
- **`.claude/skills/`** — 10 agent-facing skills, mirrored to `.opencode/skills/`.
- **`templates/`** — files the `gotit-bootstrap` skill writes into target projects.
- **`adapters/`** — optional per-agent surfaces (Cursor, Aider) that target projects opt into.
- **`examples/`** — `demo-cli` (a tiny Go CLI) + `demo-suite` (a real consumer of the kit).
- **`docs/`** — design rationale, porting guide, extending guide, changelog.

## How to operate

### When editing `runner/`

- The `runner/` package is **public API**. Every exported identifier is part of the contract.
- Run `go build ./...` and `go vet ./...` after every edit.
- The kit's own integration test is `go test ./examples/demo-suite/`. **Always run it after touching `runner/`.** It exercises every code path: passing/failing/feature-gated specs, fixtures, helpers, custom requirements, custom assertions.
- Adding new exported functions is a minor version bump. Removing or renaming any exported identifier is a major version bump. Update `docs/changelog.md` accordingly.
- Adding new built-in assertion types: extend `runner/assertions.go`'s `builtinAssertions()` map and the JSON Schema's enum at `schema/spec.schema.json` simultaneously. They must agree.

### When editing `SPEC.md`

- This is the agent-readable contract. Changes here change behavior every consumer expects. Treat as semver-public.
- Section numbers are part of the contract (skills cite them by `#5-the-spec-format`). Don't renumber.
- New sections go at the end before `Anti-Patterns` (§17) and `Glossary` (§18).

### When editing `schema/*.schema.json`

- The `$id` URL must match the published location. Don't change it without coordination.
- `enum` lists for built-in assertion types must match `runner/assertions.go`. CI lints this.

### When editing `.claude/skills/`

- Mirror to `.opencode/skills/` immediately. CI fails if they drift.
- Frontmatter is mandatory: `name` and `description`. Both are used for natural-language matching by the agents.
- Keep skill bodies procedure-first (numbered steps), not narrative. Agents skim.

### When editing `templates/`

- Templates are rendered by `gotit-bootstrap`. Changes here change the first-run experience for every new consumer.
- The Go template syntax is `{{ .Field }}`. Test changes by manually instantiating against a sample value set.

### When adding a new skill

- File: `.claude/skills/<name>/SKILL.md` with the standard frontmatter.
- Mirror to `.opencode/skills/<name>/SKILL.md`.
- Add the skill to the README.md table.
- If the skill writes files, list them under an `## Outputs` section in the SKILL body.

### When editing `examples/`

- `examples/demo-cli` and `examples/demo-suite` are the kit's living self-test. Keep them small and exemplary — they're what new consumers read to learn the kit.
- Don't add demo specs that test features the demo-cli doesn't have. Add the feature to demo-cli first.

## Things to never do

- **Never** vendor or fork the runner from inside this repo. The runner *is* this repo.
- **Never** add any other project-specific helpers, fixtures, requirements, or assertion types. The kit ships zero project-specific content.
- **Never** commit `tests/e2e/results/` content (the gitignore covers it; check before adding).
- **Never** loosen the JSON Schema to accept invalid specs. If a real spec needs new shape, design the shape, then update the schema, then update the runner — in that order.
- **Never** add a runtime dependency to the consumer's project beyond `runner/` and its current deps (`ojg`, `yaml.v3`). Test runners are infrastructure; bloat is felt by every consumer.

## Verification

Before declaring work complete:

1. `go build ./...` — must succeed.
2. `go vet ./...` — must succeed.
3. `go test ./runner/...` — unit tests pass.
4. `go test ./examples/demo-suite/` — integration tests pass.
5. `DEMO_E2E_FEATURES=greet-feature go test ./examples/demo-suite/ -run greet-feature-gated` — feature-flag override works.
6. JSON Schema validates every spec under `examples/demo-suite/specs/`.
7. SKILL.md frontmatter lint passes.
8. `.opencode/skills/` is byte-identical to `.claude/skills/` (CI lint).

## When working in a consumer project (not this repo)

If a Go project has gotit set up (`tests/e2e/runner_test.go` + `tests/e2e/gotit.yaml`), the user may have the `gotit` binary installed (via `go install github.com/shivamstaq/gotit/cmd/gotit@latest`). When that's the case:

- For **failure inspection**, recommend the TUI: `cd <project> && gotit`. Far faster than parsing JSONL by hand — the embedded shell drops the user into the spec's preserved temp HOME with the same env the test ran under.
- For **running a subset**, recommend either `gotit run wave1` (CI-friendly headless) or the TUI (interactive). Plain `go test` still works; pick whichever fits the user's intent.
- For **scaffolding**, the user can run `gotit init` themselves; the `gotit-bootstrap` skill is the agent path that does the same thing with explanation.

When the binary is absent, the JSONL path under `tests/e2e/results/` is the diagnostic source of truth.

## References

- Cross-tool spec: [SPEC.md](./SPEC.md)
- Public API: `runner/` (Go module `github.com/shivamstaq/gotit`)
- TUI binary: `cmd/gotit/` + `tui/` + [docs/tui.md](./docs/tui.md)
- Design rationale: [docs/design-rationale.md](./docs/design-rationale.md)
- Open Vercel Labs convention: https://github.com/vercel-labs/skills

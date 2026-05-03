# Design Rationale

Why gotit is shaped the way it is. Each section starts with the choice and follows with the alternatives we rejected.

## Why YAML specs (and not Go test files)

**Choice**: scenarios are YAML, not `*_test.go` functions.

- A YAML file is editable by anyone who can read documentation. Non-Go contributors (PMs, support engineers, AI agents) can write or review tests without learning Go.
- Specs are diffable, search-grep-able, and analyzable by tooling (JSON Schema, lint, tag filters) without parsing Go AST.
- Decoupling intent from harness: the runner can grow, change, or be ported to another language without rewriting every spec.

**Rejected alternatives**:

- Pure Go subtests with `testify` or `gomega`. Forces every test author through Go and a specific assertion DSL; couples test intent to implementation language.
- Gherkin / Cucumber. Heavyweight grammar; the value is in the assertion vocabulary, and Cucumber's value is shared phrasebooks across teams — not relevant for a single-project E2E suite.

## Why one big subtest function (`TestE2E`)

**Choice**: a single `TestE2E` function that walks `specs/` and dispatches subtests via `t.Run`.

- One discovery point. Adding a spec is creating a YAML file; no Go-file edit required.
- Filtering via `go test -run "TestE2E/wave3"` is free.
- IDE integrations recognize subtests. Jumping to a failed subtest works.

**Rejected**: per-spec generated `*_test.go` files. Doubles the spec count in editor file lists; makes refactoring spec discovery a code-generation problem.

## Why exec the binary (and not call `main()` in-process)

**Choice**: every step builds and execs the real CLI binary.

- Catches bugs that only surface during binary startup (init order, embedded assets, build tags, version strings).
- Catches flag-parsing edge cases that in-process tests miss.
- Mirrors how end users invoke the CLI.

**Rejected**: in-process testing via `cmd.Execute()`. Faster, but regressions in `main()`, in flag wiring, or in build configuration go undetected. The 100ms-per-spec overhead from exec is dwarfed by the assertion logic.

## Why JSONL logs (not plain text or pretty JSON)

**Choice**: one JSON object per line, one log file per `go test` invocation.

- Streamable: `tail -f` works, partial files are valid, crashes don't corrupt the file.
- Composable with Unix tools (`jq`, `grep`, `awk`).
- Self-contained per line — no whole-file load required to inspect one record.
- A pretty-printed JSON array breaks all of the above.

**Rejected**: human-readable text logs. Lose machine consumption (TUIs, CI artifact analysis). The verbose `t.Logf` output is the human-readable surface; JSONL is for tooling.

## Why hybrid distribution (module + skills + templates)

**Choice**: the runner is a Go module consumed via `go get`; the skills and templates are copied into the target project by the bootstrap skill.

- The runner is fiddly enough that drift via fork-and-adapt would be painful. A module dep gets bug fixes via `go get -u`.
- Skills and templates *should* live in the consumer's repo so they're version-controlled with the project's other config — and so an offline reader sees them.
- SPEC.md is also copied during bootstrap (or referenced via URL); same reasoning.

**Rejected**:
- Pure module (no skills, no templates): forces every consumer to read docs and write the wiring file by hand. Hostile to AI-agent-first workflows.
- Pure fork-and-adapt (Symphony-style): the runner is ~1500 LOC of carefully isolated code; making each consumer maintain a fork guarantees drift on bug fixes.

## Why feature flags (and not skipped tests)

**Choice**: specs declare `requires: [feature:<name>]`; the flag is `false` until the feature ships, then flips to `true`.

- Tests written before the implementation are real, runnable, runnable-with-`<PREFIX>_E2E_FEATURES=name`. They prove what was intended; they fail loudly the moment they're enabled and the feature isn't there.
- One commit flips the flag. No spec edit, no CI tweak, no race between landing the feature and turning on the test.
- Unknown flags default to `false`, so spec authors aren't blocked by features.yaml entries.

**Rejected**:
- `t.Skip` inline in Go: requires Go knowledge to author or remove; couples skip logic to runner code.
- Naming-convention skips (`xtest_*.yaml`): no machine-checkable contract; renames are silent.

## Why the `x-` prefix for custom assertions

**Choice**: project-local assertion types must be named `x-<name>`; the JSON Schema enforces this.

- Future kit-shipped types won't collide with project-local types.
- Reviewers can tell at a glance which assertions are universal and which are project-specific.
- Mirrors the `x-` extension prefix from other specs (HTTP headers, OpenAPI, JSON Schema vocabularies).

**Rejected**: a flat namespace. Inevitable collisions when the kit grows.

## Why no shell invocation

**Choice**: `command:` is split into argv with quote-aware tokenization; no shell is invoked.

- Predictable. No `$VAR` interpolation surprises, no shell-quoting bugs, no per-OS shell differences.
- Specs are portable: the same YAML runs identically on Linux, macOS, and (best-effort) Windows.

**Escape hatch**: `command: sh -c '...'` is allowed. Use it for pipelines, redirection, env interpolation. Be explicit.

**Rejected**: invoking `sh -c` for every command. Hidden shell quoting bugs; OS differences leak in.

## Why `t.Parallel()` and not a worker pool

**Choice**: each spec's subtest calls `t.Parallel()`; Go's testing framework caps concurrency at `GOMAXPROCS`.

- Free integration with `-parallel N`, `-race`, `-count=N`, and IDE test runners.
- Each spec is fully isolated, so true parallelism is safe.
- No custom concurrency primitives to maintain or debug.

**Rejected**: a hand-rolled worker pool. Reinvents the testing framework's primitives; loses the integration.

## Why one repo helper per shape

**Choice**: helpers like `TwoCommitRename` build one specific repo shape; specs reference by name.

- Each helper is a small, named, testable Go function.
- Reusable across specs without sharing mutable state.
- The map registration is explicit — no magic discovery.

**Rejected**:
- One mega-helper with `if args["mode"] == "rename"`. Becomes unmaintainable at 5+ shapes.
- Shell scripts under `testdata/`. Fine for static fixtures, but no commit-history control without re-shelling git.

## Why fixtures *and* helpers (not one or the other)

**Choice**: both are first-class.

- Fixtures cover ~80% of cases (single-snapshot file trees).
- Helpers cover the long tail (commit history, renames, branches, conflicts) without contorting fixtures.

Picking one would force ugly workarounds in the other domain.

## Why no built-in tag filtering

**Choice**: tags are free-form metadata; filtering uses `go test -run`.

- The subtest name (`<wave>/<name>`) gives wave filtering for free.
- Naming conventions (`journey-*`, `error-*`) give pattern filtering for free.
- Tag filtering would require parsing YAML before `go test` runs — separate tool, separate maintenance burden.

If a project genuinely needs tag filtering, a 30-line helper script can read YAML and produce a `-run` regex.

## Why isolation includes `<PREFIX>_HOME` *and* `HOME`

**Choice**: both env vars are set; the runner doesn't trust the CLI to honor either alone.

- Many CLIs read `HOME` directly. Always sandbox it.
- Many CLIs *also* read `<TOOL>_HOME` (XDG-style override). Set it explicitly to belt-and-braces against any path-construction bug.
- The cost of setting both is zero; the cost of leaking is debugging a flake six months later.

**Rejected**: setting only `HOME`. Fragile; depends on the CLI's path-construction logic.

## Why JSONPath via `ojg`

**Choice**: `github.com/ohler55/ojg/jp` is the JSONPath engine.

- Fast (5–10× the alternatives in benchmarks).
- Permissive license.
- Good error messages for malformed paths.

**Rejected**: `PaesslerAG/jsonpath` (slower, more deps), hand-rolled implementations (incomplete spec coverage). The choice is reversible — JSONPath is a leaf dependency.

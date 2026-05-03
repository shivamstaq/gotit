# Extending gotit

The runner ships with a deliberately small surface (8 assertions, 1 built-in requirement, 0 default helpers). Project-local needs are met by registering extensions through `runner.Config`. The kit's surface stays opinionated; your project's surface stays project-shaped.

## Extension surfaces

| Surface | Map | Naming rule | Skill |
|---|---|---|---|
| Repo helpers | `Config.RepoHelpers` | Free-form (e.g. `two-commit-rename`) | [gotit-add-helper](../.claude/skills/gotit-add-helper/SKILL.md) |
| Requirement checkers | `Config.RequirementCheckers` | Free-form (e.g. `python-extractor`) | (declarative — register inline) |
| Custom assertions | `Config.Assertions` | Must use `x-` prefix | [gotit-add-assertion](../.claude/skills/gotit-add-assertion/SKILL.md) |
| Feature flags | `tests/e2e/features.yaml` | kebab-case | [gotit-add-feature-flag](../.claude/skills/gotit-add-feature-flag/SKILL.md) |
| Event consumers (TUI, CI report, …) | (subscribe to channel) | n/a | n/a |

## RepoHelper

Signature:

```go
func(projectRoot, workDir string, args map[string]any) error
```

- `projectRoot` — the consumer's repo root (absolute path). Useful for reading committed templates from `tests/e2e/testdata/`.
- `workDir` — the per-spec isolated work dir. **Everything you write goes here.**
- `args` — `repo.helper_args` from the YAML, decoded as `map[string]any`. Use type assertions (`args["count"].(int)`) with care; YAML may decode integers as `int` or `int64` depending on parser.

Use the runner's git/file helpers rather than re-shelling:

```go
runner.GitInit(workDir)
runner.WriteFile(filepath.Join(workDir, "foo.txt"), "hello\n")
runner.GitCommitAll(workDir, "initial")
runner.RunGit(workDir, "mv", "old.txt", "new.txt")
```

## RequirementChecker

Signature:

```go
func(projectRoot string) error
```

- Return `nil` if the requirement is satisfied.
- Return an error to skip every spec that lists this requirement. The error message becomes the skip reason (visible in `go test -v` and the JSONL log).

Common patterns:

```go
// Binary on PATH
func CheckJq(_ string) error {
    if _, err := exec.LookPath("jq"); err != nil {
        return fmt.Errorf("jq not found on PATH")
    }
    return nil
}

// Env var present
func CheckAPIKey(_ string) error {
    if os.Getenv("MY_API_KEY") == "" {
        return fmt.Errorf("MY_API_KEY not set")
    }
    return nil
}

// File checked into the project
func CheckBinary(projectRoot string) error {
    p := filepath.Join(projectRoot, "build", "my-tool")
    if _, err := os.Stat(p); err != nil {
        return fmt.Errorf("build/my-tool missing — run `make`: %w", err)
    }
    return nil
}
```

Don't make checkers expensive (they run on every test). Cache results in package-level vars if needed.

## AssertionFunc

Signature:

```go
func(a runner.Assertion, r runner.StepResult, vars map[string]string) error
```

- `a` — the assertion entry from YAML, with template variables already resolved in string fields.
- `r` — the step's result (`Stdout`, `Stderr`, `ExitCode`, `Duration`).
- `vars` — the captured variables map (rarely needed; templating already happened).

Custom types **must** start with `x-`. The JSON Schema rejects everything else.

Reuse existing field names:

| YAML field | Use it for |
|---|---|
| `path` | A path-like selector (JSONPath, file path, etc.). |
| `value` | A substring or scalar comparison value. |
| `pattern` | A regex pattern. |
| `expected` | The expected literal (any type). |
| `op` | A comparison operator. |

Example: an assertion that the stdout has at least N non-empty lines.

```go
func AssertLinesGte(a runner.Assertion, r runner.StepResult, _ map[string]string) error {
    expected, ok := a.Expected.(int)
    if !ok {
        switch v := a.Expected.(type) {
        case int64:
            expected = int(v)
        case float64:
            expected = int(v)
        default:
            return fmt.Errorf("x-output-lines-gte: expected must be an integer, got %T", a.Expected)
        }
    }
    count := 0
    for _, line := range strings.Split(r.Stdout, "\n") {
        if strings.TrimSpace(line) != "" {
            count++
        }
    }
    if count < expected {
        return fmt.Errorf("x-output-lines-gte: got %d non-empty lines, want >= %d", count, expected)
    }
    return nil
}
```

Register and use:

```go
// runner_test.go
Assertions: map[string]runner.AssertionFunc{
    "x-output-lines-gte": helpers.AssertLinesGte,
},
```

```yaml
- name: many_results
  command: mytool dump
  assert:
    - type: x-output-lines-gte
      expected: 100
```

## Event consumers

The runner is a pure event source. Anything that wants live updates can drain `chan<- SpecEvent`.

```go
events := make(chan runner.SpecEvent, 100)
go func() {
    for ev := range events {
        // handle spec_start, step_start, step_done, spec_end
    }
}()
r := &runner.Runner{ /* … */ }
r.RunSpec(ctx, spec, events)
```

Examples of useful consumers:

- A TUI that streams test progress in real time.
- A CI plugin that posts step-by-step status to a chat channel.
- A test-impact analyzer that records which CLI commands changed which files (via the work dir snapshot from `spec_start`).

The kit doesn't ship any of these; consumers can build them on top.

## What you can't extend

These are part of the spec, not the runtime, and changing them breaks the cross-tool contract:

- The YAML spec format (versioned via `spec_version`).
- The 8 built-in assertion types (extend with `x-` types instead).
- The event types (`spec_start`, `step_start`, `step_done`, `spec_end`) and their fields.
- The isolation invariants (temp HOME, fresh git, scoped PATH).

Extensions to these would have to ship as a new `spec_version` and be coordinated through the kit. File an issue first.

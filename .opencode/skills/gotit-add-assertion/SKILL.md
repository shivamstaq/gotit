---
name: gotit-add-assertion
description: Register a project-local assertion type (the `x-` prefix family) when the 8 built-ins (exit_code, contains, not_contains, stderr_contains, regex, json_path, count, golden_file) cannot express the check. Invoke only when a built-in genuinely doesn't fit.
version: 1
---

# gotit-add-assertion

Add a custom assertion type to the project. Use sparingly — the built-in 8 cover most needs; every custom type is a maintenance cost.

## Decision check

Before adding a custom type, confirm none of these fit:

- Stdout substring: `contains` / `not_contains`.
- Stderr substring: `stderr_contains`.
- JSON shape: `json_path` with `op: ==`, `!=`, `>=`, `contains`, `contains_any`, `length_gte`.
- Array length: `count` with `op: ==`/`>=` etc.
- Pattern match: `regex`.
- Whole-output canonical compare: `golden_file`.

If you're tempted to write `x-stdout-not-empty`, that's `count` on `$.something` or `regex` on `\S`. Pause and try harder.

## When custom is correct

- Domain-specific structural checks (e.g. validate a Protobuf wire format, parse a custom DSL, walk a graph stored in stdout).
- Cross-step invariants computed from captured variables.

## Steps

1. **Pick a name** with the mandatory `x-` prefix: `x-output-lines-gte`, `x-graph-acyclic`, `x-protobuf-shape`. The schema enforces the prefix.

2. **Define a Go function** in `tests/e2e/helpers/`:

   ```go
   import "github.com/shivamstaq/gotit/runner"

   func AssertLinesGte(a runner.Assertion, r runner.StepResult, _ map[string]string) error {
       expected := toIntFromAny(a.Expected) // YAML may decode as int or float64
       lines := strings.Count(r.Stdout, "\n")
       if lines < expected {
           return fmt.Errorf("x-output-lines-gte: got %d lines, want >= %d", lines, expected)
       }
       return nil
   }
   ```

   The error message must include the type name and enough context to debug (got vs want, the operator, a truncated slice of stdout for substring failures).

3. **Register in `runner_test.go`**:

   ```go
   Assertions: map[string]runner.AssertionFunc{
       "x-output-lines-gte": helpers.AssertLinesGte,
   },
   ```

4. **Use in a spec**:

   ```yaml
   - name: many_lines
     command: <binary> dump
     assert:
       - type: x-output-lines-gte
         expected: 100
   ```

5. **Verify**: run the spec; cause a failure on purpose first to confirm the error message is useful.

## Schema notes

The JSON Schema accepts any `x-`-prefixed type. Editors won't autocomplete custom-type-specific fields — keep your custom type's "shape" close to the built-ins (use `expected`, `value`, `pattern`, `path` rather than inventing new field names) so editors and reviewers understand it without docs.

## References

- Built-in assertions: https://github.com/shivamstaq/gotit/blob/main/runner/assertions.go
- Spec extension points: https://github.com/shivamstaq/gotit/blob/main/SPEC.md#16-extension-points

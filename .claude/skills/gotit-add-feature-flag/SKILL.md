---
name: gotit-add-feature-flag
description: Land an E2E spec for an unshipped feature behind a feature flag — spec is committed but skipped in CI until the flag is flipped. Invoke when writing tests-first for a feature that isn't implemented yet, or when an existing flag needs to flip from false to true on shipping.
version: 1
---

# gotit-add-feature-flag

Use feature flags to land specs ahead of the implementation. The spec is real, runnable, and ships with the PR; CI just skips it until the flag flips.

## When to use a feature flag

- The spec asserts behavior the CLI doesn't implement yet.
- You're shipping a feature in stages and want the test green only after the last stage merges.
- You're documenting expected behavior for a refactor that's still in flight.

Do not gate specs that *should* pass today. The flag system is for unshipped features, not for flaky tests — flaky tests get fixed or deleted.

## Steps

1. **Pick a flag name** in kebab-case, descriptive of the feature: `rename-detection`, `multi-tenant-config`, `progressive-streaming`. Avoid generic names (`new-feature`, `wip`).

2. **Add the flag to `tests/e2e/features.yaml`** with `false`:

   ```yaml
   enabled:
     rename-detection: false   # #123 detect renames across commits
   ```

   Add a comment with a tracking link (issue number, PR, ticket) so future-you knows what the flag is gating.

3. **Gate the spec** with `requires: [feature:<name>]`:

   ```yaml
   spec_version: 1
   name: rename-detection-basic
   requires: [feature:rename-detection]
   repo:
     helper: two-commit-rename
   steps:
     - …
   ```

4. **Verify the spec is skipped by default**:
   ```
   go test ./tests/e2e/ -run "TestE2E/<wave>/<spec>" -v
   ```
   Expect `--- SKIP` with the message about the flag not being shipped.

5. **Verify the spec runs when forced on**:
   ```
   <PREFIX>_E2E_FEATURES=rename-detection go test ./tests/e2e/ -run "TestE2E/<wave>/<spec>" -v
   ```
   Substitute `<PREFIX>` with the project's `EnvPrefix`. The spec should now run (and may fail if the feature isn't implemented — that's expected).

6. **When shipping the feature**, flip the flag in the same PR that lands the implementation:
   ```yaml
   enabled:
     rename-detection: true
   ```
   The spec activates automatically on the next CI run; no spec edits needed.

## Tips

- One flag can gate many specs. Reuse names rather than coining `rename-detection-1`, `rename-detection-2`.
- The `all` sentinel in the env override turns on every flag listed in the file: `<PREFIX>_E2E_FEATURES=all`. Use locally to see what's currently failing; never ship a CI run with `=all`.
- Unknown flag names default to `false` (skip). It's safe to commit a spec referencing a flag that doesn't exist yet — but that obscures intent. Add the `false` line to `features.yaml` in the same commit.

## References

- Feature gating: https://github.com/shivamstaq/gotit/blob/main/SPEC.md#10-requirements--feature-gating
- Schema: https://github.com/shivamstaq/gotit/blob/main/schema/features.schema.json

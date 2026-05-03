---
name: gotit-add-fixture
description: Add a static repository fixture under tests/e2e/testdata/repos/ that specs reference via `repo.fixture:`. Invoke when a spec needs a small, file-tree-based test repo (no commit history beyond an initial commit).
version: 1
---

# gotit-add-fixture

Create a static repository fixture for use as `repo.fixture: <name>` in specs.

## When to use a fixture vs a helper

- **Fixture**: file tree only, single initial commit. Diff-able, version-controlled, fast.
- **Helper** (see `gotit-add-helper`): when commit history matters — renames, multi-file co-changes, branches, conflicts, time-stamped commits.

If the test only cares about file contents at a single point in time, a fixture is the right answer.

## Steps

1. **Pick a name** (kebab-case): describes the *shape* of the repo, not the test (e.g. `multi-package`, `single-binary`, `monorepo-with-vendor`).

2. **Create the directory** `tests/e2e/testdata/repos/<name>/`.

3. **Add the files** that the test repo should contain. The runner copies the tree as-is, then runs `git init -b main`, `git add -A`, `git commit -m "initial fixture commit"`. So:
   - Don't include a `.git/` directory — the runner builds it.
   - Don't include a `.gitignore` *unless the test specifically needs to verify ignore behavior*.

4. **Reference from a spec**:
   ```yaml
   repo:
     fixture: <name>
   ```

5. **Verify**: run any spec that uses the fixture to confirm it provisions correctly.

## Conventions

- Keep fixtures **minimal**. The smallest tree that proves the behavior. Twenty-line CSV is better than the full dataset.
- Name them by **shape**, not by test. One fixture often serves many specs.
- If two specs need slightly different trees, prefer two fixtures over branching logic in one.

## References

- Repo provisioning: https://github.com/shivamstaq/gotit/blob/main/SPEC.md#9-repository-provisioning

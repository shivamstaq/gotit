---
name: gotit-add-helper
description: Register a programmatic Go function under tests/e2e/helpers/ that builds a test repository with custom commit history, then wire it into runner_test.go's RepoHelpers map. Invoke when a spec needs renames, multi-commit history, branches, or other state a static fixture cannot express.
version: 1
---

# gotit-add-helper

Add a `RepoHelper` — a Go function that builds a test repo programmatically.

## When to use a helper vs a fixture

Use a helper when the test needs any of:

- Multiple commits with specific messages, authors, or order.
- File renames detected by `git log --follow`.
- Co-change windows — N commits touching the same files.
- Branches, conflicts, or merges.
- Time-deterministic commits.

Otherwise prefer a fixture (see `gotit-add-fixture`).

## Steps

1. **Pick a name** describing the repo shape (e.g. `two-commit-rename`, `co-change-trio`, `branch-conflict`).

2. **Create the helper package** if it doesn't exist: `tests/e2e/helpers/helpers.go`. The package name is `helpers`.

3. **Write the function** with this exact signature:

   ```go
   import (
       "github.com/shivamstaq/gotit/runner"
   )

   func TwoCommitRename(_, workDir string, _ map[string]any) error {
       if err := runner.GitInit(workDir); err != nil {
           return err
       }
       if err := runner.WriteFile(filepath.Join(workDir, "old.txt"), "hello\n"); err != nil {
           return err
       }
       if err := runner.GitCommitAll(workDir, "initial"); err != nil {
           return err
       }
       if err := runner.RunGit(workDir, "mv", "old.txt", "new.txt"); err != nil {
           return err
       }
       return runner.GitCommitAll(workDir, "rename: old.txt -> new.txt")
   }
   ```

   The runner ships `GitInit`, `GitCommitAll`, `RunGit`, `WriteFile`, `CopyDir` as helpers — use them rather than re-shelling.

4. **Register in `tests/e2e/runner_test.go`**:

   ```go
   RepoHelpers: map[string]runner.RepoHelper{
       "two-commit-rename": helpers.TwoCommitRename,
       // existing entries…
   },
   ```

   The map key is what specs reference; it can differ from the Go function name.

5. **Reference from a spec**:

   ```yaml
   repo:
     helper: two-commit-rename
     # helper_args: { commits: 5 }   # optional, passed as map[string]any
   ```

6. **Verify**: run a spec that uses the helper.

## Anti-patterns

- Helpers that depend on global state (working directory of the host process, env vars beyond what the runner sets). They run inside an isolated temp dir; everything must be relative to `workDir`.
- Helpers that re-implement `git init` / `git commit` by hand. Use the runner's helpers — they set deterministic env (`GIT_AUTHOR_*`, `GIT_CONFIG_NOSYSTEM=1`).
- Catch-all helpers with `if args[...]` branches for ten different shapes. One helper per repo shape; common setup goes in unexported helpers.

## References

- Repo provisioning: https://github.com/shivamstaq/gotit/blob/main/SPEC.md#9-repository-provisioning
- Built-in git helpers: https://github.com/shivamstaq/gotit/blob/main/runner/repohelpers.go

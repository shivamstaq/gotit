---
name: gotit-ship-wave
description: Open a new wave directory or close an existing one — flip its feature flags, rename files for archival, and update the AGENTS/README pointers. Invoke at release boundaries, when the user says "open wave3" or "ship the rename-detection feature".
version: 1
---

# gotit-ship-wave

Run two operations: opening a new wave and closing an existing one (flipping flags + archiving).

## Opening a wave

1. **Pick a wave name**. Conventional: `wave1`, `wave2`, `wave2.5`, `wave3`. The name is a directory; `1`, `2`, `2.5` is the de facto sort key. Decimal waves are fine for in-flight bug fixes that don't merit a full integer bump.

2. **Create the directory**: `tests/e2e/specs/<wave>/`. If the project uses an `errors/` subdir convention, create `tests/e2e/specs/<wave>/errors/` too.

3. **Optional**: drop a `.gitkeep` so the empty dir lands in git.

4. **Update `tests/e2e/features.yaml`** with a new section header comment for the wave's flags:

   ```yaml
   enabled:
     # ==============================================================
     # Wave 3 — <theme>
     # ==============================================================
     rename-detection: false
     co-change-windows: false
   ```

5. **Tell the user**: "Wave 3 opened at `tests/e2e/specs/wave3/`. Add specs with `gotit-add-spec` (default `order` will start at 10)."

## Closing a wave (shipping)

Closing means: every flag for the wave's features is `true`, every spec passes, and the wave is "frozen" in the sense that subsequent feature work goes to the next wave.

1. **List the wave's specs and their `requires:` entries**:
   ```
   grep -h "feature:" tests/e2e/specs/wave3/*.yaml
   ```

2. **For each flag**, confirm with the user that the underlying feature has shipped, then flip in `features.yaml`:
   ```yaml
   rename-detection: true   # was false
   ```

3. **Run the wave** to confirm everything passes now:
   ```
   go test ./tests/e2e/ -run 'TestE2E/wave3' -v
   ```

4. **If a spec was always meant to be skipped post-ship** (e.g. it tested an interim behavior that the final implementation removed), delete the spec. Don't leave it as a permanent skip.

5. **Update any wave-level docs** (e.g. a `docs/changelog.md` line, a `WAVES.md` table) to mark the wave as shipped with a date.

## Anti-patterns

- Flipping a flag to `true` without running the wave to confirm. The flag is a CI gate, not a wish.
- Opening multiple waves at once. The wave is a release-cadence artifact; keep it tied to actual release boundaries.
- Renumbering closed waves. Waves are append-only; if wave2 turned out to need a bug-fix wave between it and wave3, that's wave2.5, not "renumber wave3 to wave4".

## References

- Wave organization: https://github.com/shivamstaq/gotit/blob/main/SPEC.md#16-wave-organization
- Feature gating: https://github.com/shivamstaq/gotit/blob/main/SPEC.md#10-requirements--feature-gating

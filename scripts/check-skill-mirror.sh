#!/usr/bin/env bash
#
# Verify .opencode/skills/<name>/SKILL.md is byte-identical to
# .claude/skills/<name>/SKILL.md for every skill. Drift is a CI failure.

set -euo pipefail

fail=0
for src in .claude/skills/*/SKILL.md; do
  rel="${src#.claude/skills/}"        # gotit-foo/SKILL.md
  dst=".opencode/skills/$rel"
  if [[ ! -f "$dst" ]]; then
    echo "FAIL: missing mirror $dst (source $src exists)" >&2
    fail=1
    continue
  fi
  if ! diff -q "$src" "$dst" >/dev/null; then
    echo "FAIL: mirror drift between $src and $dst" >&2
    diff -u "$src" "$dst" >&2 || true
    fail=1
  fi
done

# Catch reverse drift: an .opencode skill with no .claude source.
for dst in .opencode/skills/*/SKILL.md; do
  rel="${dst#.opencode/skills/}"
  src=".claude/skills/$rel"
  if [[ ! -f "$src" ]]; then
    echo "FAIL: orphan mirror $dst (no .claude/skills/$rel)" >&2
    fail=1
  fi
done

if [[ $fail -ne 0 ]]; then
  exit 1
fi
echo "ok: .opencode/skills/ matches .claude/skills/"

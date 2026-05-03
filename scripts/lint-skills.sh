#!/usr/bin/env bash
#
# Verify every .claude/skills/<name>/SKILL.md has the mandatory Agent Skills
# frontmatter (`name:` and `description:`).
#
# Exits 0 on success, non-zero on any failure with a list of broken files.

set -euo pipefail

fail=0
for f in .claude/skills/*/SKILL.md; do
  if [[ ! -f "$f" ]]; then
    continue
  fi

  # Frontmatter must start with `---` on the first line.
  first_line=$(head -n1 "$f")
  if [[ "$first_line" != "---" ]]; then
    echo "FAIL: $f does not start with ---" >&2
    fail=1
    continue
  fi

  # Extract the frontmatter block (everything between the first two --- lines).
  frontmatter=$(awk '/^---$/{n++; next} n==1{print} n==2{exit}' "$f")

  if ! echo "$frontmatter" | grep -qE '^name:\s+'; then
    echo "FAIL: $f missing required 'name:' field" >&2
    fail=1
  fi
  if ! echo "$frontmatter" | grep -qE '^description:\s+'; then
    echo "FAIL: $f missing required 'description:' field" >&2
    fail=1
  fi
done

if [[ $fail -ne 0 ]]; then
  exit 1
fi
echo "ok: all SKILL.md files have valid frontmatter"

# Changelog

All notable changes to the gotit kit. Two version streams:

- **Spec version** (`spec_version` in YAML) — bumps on incompatible changes to the YAML format. Currently v1.
- **Runtime version** (`runner` Go module) — semver. Bumps on changes to the public Go API, even when the spec format is stable.

## Spec v1

The first published version. All sections of [SPEC.md](../SPEC.md) describe v1 behavior.

Key contracts:

- Top-level fields: `spec_version`, `name`, `description`, `tags`, `requires`, `repo`, `setup`, `steps`, `cleanup`, `order`, `notes_file`.
- Eight built-in assertion types: `exit_code`, `contains`, `not_contains`, `stderr_contains`, `regex`, `json_path`, `count`, `golden_file`.
- Capture syntax: JSONPath (`$.path`) and `regex:(pattern)`.
- Template syntax: `{{ var }}` and `{{var}}`.
- Custom assertion types: `x-` prefix.
- Feature flags: `requires: [feature:<name>]`, allowlist at `tests/e2e/features.yaml`, env override `<PREFIX>_E2E_FEATURES`.
- Subtest naming: `TestE2E/<wave>/<spec-name>`.

## Runtime — unreleased

Initial public release in progress.

- `runner` package: `Config`, `Runner`, `Spec`, `Step`, `Assertion`, `StepResult`, `SpecEvent`, `JSONLLogger`.
- `runner/testdriver` package: `Run(t, cfg)` for the canonical 25-line wiring file.
- Schemas: `schema/spec.schema.json`, `schema/features.schema.json`.
- Skills: 10 SKILL.md files under `.claude/skills/`, mirrored to `.opencode/skills/`.

---

## Migration notes

When the spec format eventually moves to v2 (no plan), this section will document:

- The breaking change.
- The minimum runtime version that accepts v2.
- A diff-level migration recipe.
- Whether v1 specs are still accepted by the v2 runtime (deprecation window).

Until then, this section stays empty.

---

## Versioning policy

- **Patch** (`runner/` v0.1.0 → v0.1.1): bug fixes, doc-only changes, internal refactors with no public-API impact.
- **Minor** (v0.1.x → v0.2.0): new exported functions, new built-in assertion types, new fields in event/log records (additive only).
- **Major** (v0.x.y → v1.0.0): public-API breaks. This becomes increasingly rare after v1.0.0; spec-format breaks bump `spec_version` instead.
- **Spec major bump** (v1 → v2): see "Migration notes" above.

Pre-1.0, minor bumps may include compatible-looking but stricter validation (e.g. previously-tolerated invalid YAML now rejected). Track these here so consumers know to bump intentionally.

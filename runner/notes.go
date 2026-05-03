package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnsureNotesFile returns the sidecar notes path for a spec, creating it from
// a default template when missing. Existing files are left untouched.
func EnsureNotesFile(spec *Spec) (string, error) {
	if spec.NotesPath == "" {
		return "", fmt.Errorf("spec %q has no NotesPath set", spec.Name)
	}
	if _, err := os.Stat(spec.NotesPath); err == nil {
		return spec.NotesPath, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat notes file: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(spec.NotesPath), 0o755); err != nil {
		return "", fmt.Errorf("create notes dir: %w", err)
	}

	content := RenderNotesTemplate(spec)
	if err := os.WriteFile(spec.NotesPath, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write notes file: %w", err)
	}
	return spec.NotesPath, nil
}

// RenderNotesTemplate produces the default sidecar markdown for a spec.
// Sections are intentionally empty; contributors fill them in.
func RenderNotesTemplate(spec *Spec) string {
	title := spec.Name
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(spec.FilePath), filepath.Ext(spec.FilePath))
	}
	tags := strings.Join(spec.Tags, ", ")
	if tags == "" {
		tags = "_(none)_"
	}
	desc := spec.Description
	if desc == "" {
		desc = "_(add a one-line summary here)_"
	}
	return fmt.Sprintf(`# %s

> %s

**Wave:** %s
**Tags:** %s
**Spec:** `+"`%s`"+`

## Reference

_(link to design doc, RFC, ticket, or related code)_

## Test Setup

_(What fixture or helper builds the repo? What CLI commands run? What's asserted?)_

## Flow

`+"```"+`
_(ASCII diagram of actors, commands, state transitions)_
`+"```"+`

## Notes

_(Gotchas, related specs, follow-up work.)_
`, title, desc, spec.Wave, tags, filepath.Base(spec.FilePath))
}

package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/shivamstaq/gotit/runner"
)

// openNotes opens (or re-focuses) the read-only glamour-rendered notes pane
// for the currently selected spec. Rendering is async — the pane shows a
// "Rendering notes…" placeholder until renderNotesCmd delivers a
// notesRenderedMsg back to Update.
func (m *Model) openNotes() (tea.Model, tea.Cmd) {
	entry := m.selectedEntry()
	if entry == nil || entry.Spec == nil {
		return m, nil
	}

	rightW, rightH := m.rightPanelDimensions()
	viewerW := rightW - 4
	viewerH := rightH - 4

	// Cache hit: same entry, same width, already rendered — instant re-focus.
	if m.notes != nil && m.notes.entry == entry && m.notes.renderedFor == cacheKey(entry, viewerW) {
		m.focus = focusNotes
		return m, nil
	}

	// Close any live shell — the right pane is shared.
	if m.shell != nil && !m.shell.exited {
		m.shell.Close()
		m.shell = nil
	}

	path, err := runner.EnsureNotesFile(entry.Spec)
	if err != nil {
		return m, nil
	}

	// Reuse the existing viewer if it's the same entry (preserves previous
	// content as a backdrop while we re-render). Otherwise build fresh with
	// the placeholder.
	if m.notes != nil && m.notes.entry == entry {
		m.notes.width = viewerW
		m.notes.height = viewerH
		m.notes.viewport.Width = viewerW
		m.notes.viewport.Height = viewerH
		m.notes.rendering = true
	} else {
		viewer, err := newNotesViewer(entry, path, viewerW, viewerH)
		if err != nil {
			return m, nil
		}
		m.notes = viewer
	}

	m.focus = focusNotes
	return m, renderNotesCmd(entry, path, viewerW)
}

// selectedEntry returns the entry the user is currently focused on — step-
// reviewed, notes-viewed, or highlighted in the test list.
func (m *Model) selectedEntry() *TestEntry {
	switch m.focus {
	case focusStepReview:
		if m.stepView.entry != nil {
			return m.stepView.entry
		}
	case focusNotes:
		if m.notes != nil {
			return m.notes.entry
		}
	}
	return m.testList.SelectedEntry()
}

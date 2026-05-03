package tui

import (
	"fmt"
	"os"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
)

// notesViewerModel renders a spec's sidecar .md notes through glamour and
// shows them in a scrollable viewport on the right pane. Read-only.
// Rendering is async — openNotes installs a placeholder and dispatches a
// renderNotesCmd; the message comes back to Update with the rendered content.
type notesViewerModel struct {
	viewport viewport.Model
	entry    *TestEntry
	path     string
	width    int
	height   int

	// renderedFor is non-empty when `content` matches the markdown last
	// rendered at (entry, width). rendering is true while an async render
	// is in flight.
	renderedFor string
	content     string
	rendering   bool
}

// notesRenderedMsg is dispatched when renderNotesCmd finishes. Update only
// applies it if the current viewer is still looking at the same (entry, width).
type notesRenderedMsg struct {
	entry   *TestEntry
	width   int
	content string
}

// notesWarmupDoneMsg is a benign signal that the Init-time warmup render
// finished. The Update loop ignores it; its only purpose is to force
// glamour/chroma to pay the lazy-load cost off the UI goroutine.
type notesWarmupDoneMsg struct{}

const notesPlaceholder = "\n  Rendering notes…\n"

// newNotesViewer builds a viewer with a placeholder already installed. The
// caller is responsible for dispatching renderNotesCmd.
func newNotesViewer(entry *TestEntry, path string, width, height int) (*notesViewerModel, error) {
	if entry == nil {
		return nil, fmt.Errorf("entry required")
	}
	if width < 20 {
		width = 20
	}
	if height < 4 {
		height = 4
	}
	vp := viewport.New(width, height)
	vp.SetContent(notesPlaceholder)
	return &notesViewerModel{
		viewport:  vp,
		entry:     entry,
		path:      path,
		width:     width,
		height:    height,
		rendering: true,
	}, nil
}

// cacheKey is the (entry, width) identifier for memoised renders.
func cacheKey(entry *TestEntry, width int) string {
	return fmt.Sprintf("%p:%d", entry, width)
}

// renderNotesCmd reads the notes file and renders it through glamour off the
// UI goroutine. Returns a notesRenderedMsg when done.
func renderNotesCmd(entry *TestEntry, path string, width int) tea.Cmd {
	return func() tea.Msg {
		data, err := os.ReadFile(path)
		if err != nil {
			return notesRenderedMsg{
				entry:   entry,
				width:   width,
				content: "\n  Error reading notes: " + err.Error() + "\n",
			}
		}
		return notesRenderedMsg{
			entry:   entry,
			width:   width,
			content: renderMarkdown(string(data), width),
		}
	}
}

// warmGlamourCmd runs a trivial render at TUI start so chroma/glamour pay
// their lazy-load cost before the first user-visible open. Runs once.
func warmGlamourCmd() tea.Cmd {
	return func() tea.Msg {
		_ = renderMarkdown("```go\nfoo := 1\n```\n", 80)
		return notesWarmupDoneMsg{}
	}
}

// renderMarkdown runs `md` through glamour at the given width.
//
// Env overrides:
//   - GOTIT_TUI_NOTES_PLAIN=1  — skip glamour entirely, return raw markdown.
//   - GOTIT_TUI_NOTES_STYLE=…  — glamour style name or absolute path
//     (default "dark").
//
// IMPORTANT: never use glamour.WithAutoStyle here — it issues an OSC-11
// terminal background query that bubbletea's alt-screen input loop will
// swallow, causing a ~1 s freeze per invocation.
func renderMarkdown(md string, width int) string {
	if os.Getenv("GOTIT_TUI_NOTES_PLAIN") == "1" {
		return md
	}
	style := os.Getenv("GOTIT_TUI_NOTES_STYLE")
	if style == "" {
		style = "dark"
	}
	if width < 20 {
		width = 20
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStylePath(style),
		glamour.WithWordWrap(width-4),
	)
	if err != nil {
		return md
	}
	out, err := r.Render(md)
	if err != nil {
		return md
	}
	return out
}

// resize updates pane dimensions. On a width change it returns a render cmd
// so the new wrap takes effect without blocking the UI; height-only changes
// just adjust the viewport.
func (m *notesViewerModel) resize(width, height int) tea.Cmd {
	if width < 20 || height < 4 {
		return nil
	}
	widthChanged := width != m.width
	m.width = width
	m.height = height
	m.viewport.Width = width
	m.viewport.Height = height
	if !widthChanged {
		return nil
	}
	// Keep showing previously-rendered content (not the placeholder) so
	// resize doesn't flash. Mark cache stale; the dispatched cmd repopulates.
	m.renderedFor = ""
	m.rendering = true
	return renderNotesCmd(m.entry, m.path, width)
}

// View returns the rendered viewport.
func (m *notesViewerModel) View() string {
	return m.viewport.View()
}

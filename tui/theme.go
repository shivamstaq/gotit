package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Test state indicators.
	passedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	failedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	runningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	queuedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	skippedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	pendingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	// Dimmed text for setup/cleanup phases and metadata.
	dimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	// Headers and labels.
	headerStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	waveHeader    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	phaseLabel    = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("245"))
	commandStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	filePathStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	// Panel borders.
	panelBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240"))

	// Selected item in list.
	selectedStyle = lipgloss.NewStyle().Reverse(true)

	// Footer.
	footerStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	footerKeyStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))

	// Assertion results.
	assertPassStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	assertFailStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196"))

	// Skipped badge in the right-panel detail view (bold, dim red).
	skipBadgeStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("124"))

	// Shell pane indicator.
	shellActiveStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("82"))
	shellExitedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	// Search bar.
	searchPromptStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	searchInputStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	filterBadgeStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
)

// spinnerFrames are the braille-dot frames used for the running indicator.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// stateIndicator returns the styled marker for a test state.
func stateIndicator(state TestState, frame int) string {
	switch state {
	case StatePending:
		return pendingStyle.Render("○")
	case StateQueued:
		return queuedStyle.Render("◎")
	case StateRunning:
		f := spinnerFrames[frame%len(spinnerFrames)]
		return runningStyle.Render(f)
	case StatePassed:
		return passedStyle.Render("✓")
	case StateFailed:
		return failedStyle.Render("✗")
	case StateSkipped:
		return skippedStyle.Render("⊘")
	default:
		return " "
	}
}

// Touch unused-style guards (kept exported for theme overrides in future).
var _ = filePathStyle
var _ = searchInputStyle
var _ = panelBorder

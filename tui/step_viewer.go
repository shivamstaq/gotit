package tui

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

const maxOutputDisplay = 100 * 1024 // truncate per-step output beyond ~100KB

// stepViewerModel is the right panel. Its content depends on test state:
//   - pending/skipped: spec metadata + notes preview
//   - queued/running:  live progress
//   - passed/failed:   step-by-step review (all-steps overview or focused step)
type stepViewerModel struct {
	entry    *TestEntry
	stepIdx  int
	viewport viewport.Model
	width    int
	height   int
}

func newStepViewerModel(width, height int) stepViewerModel {
	vp := viewport.New(width-2, height-4)
	return stepViewerModel{
		viewport: vp,
		width:    width,
		height:   height,
	}
}

func (m *stepViewerModel) SetEntry(entry *TestEntry) {
	m.entry = entry
	m.stepIdx = -1 // -1 = "all steps" view (default)
	m.updateContent()
}

func (m *stepViewerModel) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.viewport.Width = width - 4
	m.viewport.Height = height - 4
	m.updateContent()
}

func (m *stepViewerModel) NextStep() {
	if m.entry == nil || len(m.entry.Steps) == 0 {
		return
	}
	if m.stepIdx == -1 {
		m.stepIdx = 0
	} else if m.stepIdx < len(m.entry.Steps)-1 {
		m.stepIdx++
	}
	m.updateContent()
}

func (m *stepViewerModel) PrevStep() {
	if m.stepIdx == 0 {
		m.stepIdx = -1
	} else if m.stepIdx > 0 {
		m.stepIdx--
	}
	m.updateContent()
}

func (m *stepViewerModel) IsAllView() bool { return m.stepIdx == -1 }

func (m *stepViewerModel) Refresh() { m.updateContent() }

func (m *stepViewerModel) updateContent() {
	if m.entry == nil {
		m.viewport.SetContent(dimStyle.Render("Select a test to view"))
		return
	}
	switch m.entry.State {
	case StatePending:
		m.renderPending()
	case StateSkipped:
		m.renderSkipped()
	case StateQueued:
		m.renderQueued()
	case StateRunning:
		m.renderRunning()
	case StatePassed, StateFailed:
		if m.stepIdx == -1 {
			m.renderAllSteps()
		} else {
			m.renderStepReview()
		}
	}
}

func (m *stepViewerModel) renderPending() {
	var b strings.Builder
	spec := m.entry.Spec
	fmt.Fprintf(&b, "%s\n", headerStyle.Render(spec.Wave+"/"+spec.Name))
	fmt.Fprintf(&b, "\n%s %s\n", dimStyle.Render("Description:"), spec.Description)
	if len(spec.Tags) > 0 {
		fmt.Fprintf(&b, "%s %s\n", dimStyle.Render("Tags:"), strings.Join(spec.Tags, ", "))
	}
	if len(spec.Requires) > 0 {
		fmt.Fprintf(&b, "%s %s\n", dimStyle.Render("Requires:"), strings.Join(spec.Requires, ", "))
	}
	fmt.Fprintf(&b, "%s %d (+ %d setup)\n", dimStyle.Render("Steps:"), len(spec.Steps), len(spec.Setup))

	if preview := renderNotesPreview(spec.NotesPath, 12); preview != "" {
		fmt.Fprintf(&b, "\n%s\n%s\n", headerStyle.Render("Notes:"), preview)
	} else if spec.NotesPath != "" {
		fmt.Fprintf(&b, "\n%s\n", dimStyle.Render("Notes: (none — press 'n' to create)"))
	}

	fmt.Fprintf(&b, "\n%s", dimStyle.Render("Press 'r' to run  ·  'n' to edit notes"))
	m.viewport.SetContent(b.String())
	m.viewport.GotoTop()
}

// renderNotesPreview returns the first maxLines lines of the notes file,
// dimmed. Empty if the file does not exist or is empty.
func renderNotesPreview(path string, maxLines int) string {
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() && len(lines) < maxLines {
		lines = append(lines, scanner.Text())
	}
	if len(lines) == 0 {
		return ""
	}
	return dimStyle.Render(strings.Join(lines, "\n"))
}

func (m *stepViewerModel) renderSkipped() {
	var b strings.Builder
	spec := m.entry.Spec
	fmt.Fprintf(&b, "%s\n", headerStyle.Render(spec.Wave+"/"+spec.Name))
	fmt.Fprintf(&b, "\n%s\n", skippedStyle.Render("⊘ SKIPPED"))
	fmt.Fprintf(&b, "%s\n", m.entry.ReqError)
	m.viewport.SetContent(b.String())
	m.viewport.GotoTop()
}

func (m *stepViewerModel) renderQueued() {
	var b strings.Builder
	spec := m.entry.Spec
	fmt.Fprintf(&b, "%s\n", headerStyle.Render(spec.Wave+"/"+spec.Name))
	fmt.Fprintf(&b, "\n%s\n", queuedStyle.Render("◎ Queued — waiting for worker slot"))
	m.viewport.SetContent(b.String())
	m.viewport.GotoTop()
}

func (m *stepViewerModel) renderRunning() {
	var b strings.Builder
	spec := m.entry.Spec
	totalSteps := len(spec.Setup) + len(spec.Steps)
	currentIdx := m.entry.CurrentStep + 1
	if currentIdx < 1 {
		currentIdx = 1
	}
	fmt.Fprintf(&b, "%s > %s step %d/%d\n\n",
		headerStyle.Render(spec.Wave+"/"+spec.Name),
		runningStyle.Render("running"),
		currentIdx, totalSteps)

	for _, step := range m.entry.Steps {
		status := passedStyle.Render("✓")
		if !step.Passed {
			status = failedStyle.Render("✗")
		}
		phase := phaseLabel.Render("[" + step.Phase + "]")
		fmt.Fprintf(&b, "%s %s %s (exit=%d, %dms)\n",
			phase, step.Name, status, step.ExitCode, step.DurationMs)
	}
	if m.entry.CurrentStep >= 0 {
		fmt.Fprintf(&b, "\n%s\n", runningStyle.Render("● running..."))
	}
	m.viewport.SetContent(b.String())
	m.viewport.GotoBottom()
}

func (m *stepViewerModel) renderAllSteps() {
	if len(m.entry.Steps) == 0 {
		m.viewport.SetContent(dimStyle.Render("No steps recorded"))
		return
	}
	divider := dimStyle.Render(strings.Repeat("─", m.width-6))
	var b strings.Builder
	for i, step := range m.entry.Steps {
		if i > 0 {
			fmt.Fprintf(&b, "\n%s\n\n", divider)
		}
		phase := phaseLabel.Render("[" + step.Phase + "]")
		status := passedStyle.Render("✓ PASSED")
		if !step.Passed {
			status = failedStyle.Render("✗ FAILED")
		}
		fmt.Fprintf(&b, "%s %s  %s  %s\n", phase, step.Name, status, dimStyle.Render(fmt.Sprintf("%dms", step.DurationMs)))
		if step.Command != "" {
			fmt.Fprintf(&b, "%s\n", commandStyle.Render("$ "+step.Command))
		}
		if step.Stdout != "" {
			out := step.Stdout
			if len(out) > maxOutputDisplay {
				out = out[:maxOutputDisplay] + "\n" + dimStyle.Render("(truncated)")
			}
			b.WriteString("\n")
			b.WriteString(out)
			if !strings.HasSuffix(out, "\n") {
				b.WriteString("\n")
			}
		}
		if step.Stderr != "" {
			fmt.Fprintf(&b, "\n%s\n", headerStyle.Render("stderr:"))
			err := step.Stderr
			if len(err) > maxOutputDisplay {
				err = err[:maxOutputDisplay] + "\n" + dimStyle.Render("(truncated)")
			}
			b.WriteString(err)
			if !strings.HasSuffix(err, "\n") {
				b.WriteString("\n")
			}
		}
		if len(step.Assertions) > 0 {
			passCount := 0
			var failures []string
			for _, a := range step.Assertions {
				if a.Passed {
					passCount++
				} else {
					msg := a.Type
					if a.Error != "" {
						msg += " — " + a.Error
					}
					failures = append(failures, msg)
				}
			}
			if len(failures) == 0 {
				fmt.Fprintf(&b, "%s\n", dimStyle.Render(fmt.Sprintf("assertions: %d/%d passed", passCount, len(step.Assertions))))
			} else {
				fmt.Fprintf(&b, "assertions: %d/%d passed\n", passCount, len(step.Assertions))
				for _, f := range failures {
					fmt.Fprintf(&b, "%s\n", assertFailStyle.Render("  ✗ "+f))
				}
			}
		}
	}
	m.viewport.SetContent(b.String())
	m.viewport.GotoTop()
}

func (m *stepViewerModel) renderStepReview() {
	if len(m.entry.Steps) == 0 {
		m.viewport.SetContent(dimStyle.Render("No steps recorded"))
		return
	}
	if m.stepIdx < 0 || m.stepIdx >= len(m.entry.Steps) {
		m.stepIdx = 0
	}
	step := m.entry.Steps[m.stepIdx]
	var b strings.Builder

	phase := phaseLabel.Render("[" + step.Phase + "]")
	cmd := ""
	if step.Command != "" {
		cmd = commandStyle.Render("$ " + step.Command)
	}
	fmt.Fprintf(&b, "%s %s\n", phase, step.Name)
	if cmd != "" {
		b.WriteString(cmd + "\n")
	}
	b.WriteString("\n")

	if step.Stdout != "" {
		b.WriteString(headerStyle.Render("stdout:") + "\n")
		out := step.Stdout
		if len(out) > maxOutputDisplay {
			out = out[:maxOutputDisplay] + "\n" + dimStyle.Render("(truncated — use shell for full output)")
		}
		b.WriteString(out)
		if !strings.HasSuffix(out, "\n") {
			b.WriteString("\n")
		}
	} else {
		b.WriteString(dimStyle.Render("stdout: (empty)") + "\n")
	}
	b.WriteString("\n")

	if step.Stderr != "" {
		b.WriteString(headerStyle.Render("stderr:") + "\n")
		err := step.Stderr
		if len(err) > maxOutputDisplay {
			err = err[:maxOutputDisplay] + "\n" + dimStyle.Render("(truncated)")
		}
		b.WriteString(err)
		if !strings.HasSuffix(err, "\n") {
			b.WriteString("\n")
		}
	} else {
		b.WriteString(dimStyle.Render("stderr: (empty)") + "\n")
	}
	b.WriteString("\n")

	statusStyle := passedStyle
	statusLabel := "PASSED"
	if !step.Passed {
		statusStyle = failedStyle
		statusLabel = "FAILED"
	}
	fmt.Fprintf(&b, "exit: %d  duration: %dms  %s\n",
		step.ExitCode, step.DurationMs, statusStyle.Render(statusLabel))

	if len(step.Assertions) > 0 {
		passCount := 0
		for _, a := range step.Assertions {
			if a.Passed {
				passCount++
			}
		}
		fmt.Fprintf(&b, "assertions: %d/%d passed\n", passCount, len(step.Assertions))
		for _, a := range step.Assertions {
			if a.Passed {
				b.WriteString(assertPassStyle.Render("  ✓ "+a.Type) + "\n")
			} else {
				m := a.Type
				if a.Error != "" {
					m += " — " + a.Error
				}
				b.WriteString(assertFailStyle.Render("  ✗ "+m) + "\n")
			}
		}
	}
	m.viewport.SetContent(b.String())
	m.viewport.GotoTop()
}

func (m *stepViewerModel) View(focused bool, shellActive bool) string {
	if m.entry == nil {
		content := lipgloss.NewStyle().
			Width(m.width-2).
			Height(m.height).
			Align(lipgloss.Center, lipgloss.Center).
			Foreground(lipgloss.Color("240")).
			Render("Select a test to view")
		return lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Render(content)
	}

	var headerText string
	if m.entry.State == StatePassed || m.entry.State == StateFailed {
		stepCount := len(m.entry.Steps)
		if m.stepIdx == -1 {
			headerText = fmt.Sprintf("%s/%s > %s  %s",
				m.entry.Spec.Wave, m.entry.Spec.Name,
				headerStyle.Render("all"),
				dimStyle.Render(fmt.Sprintf("(%d steps — → to focus individual)", stepCount)))
		} else {
			headerText = fmt.Sprintf("%s/%s > step %d/%d  %s",
				m.entry.Spec.Wave, m.entry.Spec.Name, m.stepIdx+1, stepCount,
				dimStyle.Render("(← back to all)"))
		}
		if shellActive {
			headerText += "  " + shellActiveStyle.Render("[shell]")
		}
	} else {
		headerText = fmt.Sprintf("%s/%s", m.entry.Spec.Wave, m.entry.Spec.Name)
	}
	header := headerStyle.Render(headerText)

	borderColor := lipgloss.Color("240")
	if focused {
		borderColor = lipgloss.Color("15")
	}
	body := lipgloss.JoinVertical(lipgloss.Left, header, "", m.viewport.View())
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(m.width - 2).
		Height(m.height).
		Render(body)
}

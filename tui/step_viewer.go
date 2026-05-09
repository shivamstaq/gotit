package tui

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"

	"github.com/shivamstaq/gotit/runner"
)

const (
	maxOutputDisplay = 100 * 1024 // truncate per-step output beyond ~100KB
	assertGotInline  = 80         // single-line Got snippet width on failure
	assertGotPass    = 40         // single-line Got snippet width on pass
)

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
	fmt.Fprintf(&b, "%s %s\n", dimStyle.Render("Description:"), spec.Description)
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
	fmt.Fprintf(&b, "%s %s\n", dimStyle.Render("Description:"), spec.Description)
	if len(spec.Tags) > 0 {
		fmt.Fprintf(&b, "%s %s\n", dimStyle.Render("Tags:"), strings.Join(spec.Tags, ", "))
	}
	if len(spec.Requires) > 0 {
		fmt.Fprintf(&b, "%s %s\n", dimStyle.Render("Requires:"), strings.Join(spec.Requires, ", "))
	}
	fmt.Fprintf(&b, "%s %d (+ %d setup)\n", dimStyle.Render("Steps:"), len(spec.Steps), len(spec.Setup))

	b.WriteString("\n" + renderSkipBox(m.entry.ReqError, m.width-4) + "\n")

	if preview := renderNotesPreview(spec.NotesPath, 12); preview != "" {
		fmt.Fprintf(&b, "\n%s\n%s\n", headerStyle.Render("Notes:"), preview)
	}

	m.viewport.SetContent(b.String())
	m.viewport.GotoTop()
}

// renderSkipBox builds the bordered SKIPPED block: a bold-red ⊘ SKIPPED badge,
// then the main reason, then a dimmed hint. Lines are word-wrapped to the box's
// inner width.
func renderSkipBox(reason string, panelWidth int) string {
	inner := maxWrapWidth(panelWidth - 4) // subtract border + padding
	wrap := lipgloss.NewStyle().Width(inner)

	var inside strings.Builder
	inside.WriteString(skipBadgeStyle.Render("⊘ SKIPPED"))
	if reason != "" {
		main, hint := splitSkipReason(reason)
		inside.WriteString("\n" + wrap.Render(main))
		if hint != "" {
			inside.WriteString("\n" + wrap.Foreground(lipgloss.Color("240")).Render(hint))
		}
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1).
		Render(inside.String())
}

// splitSkipReason separates the headline from any trailing parenthesized hint
// so the TUI can render the main reason on one line and the (typically longer)
// "how to fix" hint dimmed below it. Absolute paths in the hint are reduced
// to their basenames to keep the line short. Falls back to (msg, "") when
// there's no parenthesized tail.
func splitSkipReason(msg string) (main, hint string) {
	msg = strings.TrimSpace(msg)
	open := strings.LastIndex(msg, " (")
	if open < 0 || !strings.HasSuffix(msg, ")") {
		return msg, ""
	}
	hint = strings.TrimSuffix(msg[open+2:], ")")
	hint = shortenPaths(hint)
	return msg[:open], hint
}

// shortenPaths replaces absolute paths in s with their basenames so a verbose
// "add to /long/abs/path/features.yaml" becomes "add to features.yaml".
func shortenPaths(s string) string {
	fields := strings.Fields(s)
	for i, f := range fields {
		if strings.HasPrefix(f, "/") && strings.Contains(f, "/") {
			fields[i] = filepath.Base(f)
		}
	}
	return strings.Join(fields, " ")
}

// maxWrapWidth clamps the wrap width so very wide terminals don't yield
// awkwardly long single lines, while still respecting smaller widths.
func maxWrapWidth(w int) int {
	return min(80, max(20, w))
}

func (m *stepViewerModel) renderQueued() {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", queuedStyle.Render("◎ Queued — waiting for worker slot"))
	m.viewport.SetContent(b.String())
	m.viewport.GotoTop()
}

func (m *stepViewerModel) renderRunning() {
	var b strings.Builder
	spec := m.entry.Spec
	totalSteps := len(spec.Setup) + len(spec.Steps)
	currentIdx := max(m.entry.CurrentStep+1, 1)
	fmt.Fprintf(&b, "%s step %d/%d\n\n",
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
			renderAssertions(&b, step.Assertions, false)
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
		renderAssertions(&b, step.Assertions, true)
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

// renderAssertions writes a compact, structured block summarizing the step's
// assertions: a header line with pass/total, then one line per pass and up to
// three lines per failure (✗ summary + indented want/got). When detail is
// false, only failures are listed individually (used in the all-steps overview
// to keep noise down); when true, every assertion is listed (focused step view).
func renderAssertions(b *strings.Builder, asserts []runner.AssertionResult, detail bool) {
	passCount := 0
	for _, a := range asserts {
		if a.Passed {
			passCount++
		}
	}
	header := fmt.Sprintf("assertions: %d/%d passed", passCount, len(asserts))
	if passCount == len(asserts) {
		fmt.Fprintf(b, "%s\n", dimStyle.Render(header))
	} else {
		fmt.Fprintf(b, "%s\n", header)
	}

	for _, a := range asserts {
		if a.Passed {
			if !detail {
				continue
			}
			line := assertPassStyle.Render("  ✓ " + assertLabel(a))
			if a.Got != "" {
				line += dimStyle.Render("  → " + truncRunes(a.Got, assertGotPass))
			}
			b.WriteString(line + "\n")
		} else {
			b.WriteString(assertFailStyle.Render("  ✗ "+assertLabel(a)) + "\n")
			// Show want/got when the runner provided them; otherwise fall back
			// to the legacy single-line "type — error" format inline (older logs).
			if a.Want != "" || a.Got != "" {
				if a.Want != "" {
					b.WriteString(dimStyle.Render("       want: ") + truncRunes(a.Want, assertGotInline) + "\n")
				}
				if a.Got != "" {
					b.WriteString(dimStyle.Render("       got:  ") + truncRunes(a.Got, assertGotInline) + "\n")
				}
			} else if a.Error != "" {
				// Legacy fallback: surface the raw error on a dimmed indented line.
				first := strings.SplitN(a.Error, "\n", 2)[0]
				b.WriteString(dimStyle.Render("       "+truncRunes(first, assertGotInline)) + "\n")
			}
		}
	}
}

// assertLabel returns the assertion's display label: Summary if present,
// otherwise the bare type (legacy logs).
func assertLabel(a runner.AssertionResult) string {
	if a.Summary != "" {
		return a.Summary
	}
	return a.Type
}

// truncRunes truncates s to max runes, appending "..." when shortened. It
// operates on runes (not bytes) to keep multibyte characters intact.
func truncRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "..."
}

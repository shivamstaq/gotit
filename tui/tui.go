package tui

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	gtrunner "github.com/shivamstaq/gotit/runner"
)

type focusState int

const (
	focusTestList focusState = iota
	focusStepReview
	focusShell
	focusSearch
	focusNotes
)

// specEventMsg wraps a runner.SpecEvent for delivery to the bubbletea Update loop.
type specEventMsg struct {
	idx   int
	event gtrunner.SpecEvent
}

// spinnerTickMsg triggers spinner animation.
type spinnerTickMsg time.Time

// Model is the root bubbletea model for the gotit TUI.
type Model struct {
	focus       focusState
	testList    testListModel
	stepView    stepViewerModel
	shell       *shellModel
	notes       *notesViewerModel
	entries     []*TestEntry
	cfg         GotitConfig
	projectRoot string
	program     *tea.Program // set after program starts
	width       int
	height      int
	ready       bool

	// Worker pool — caps the number of in-flight `go test` invocations so we
	// don't overwhelm the user's machine.
	sem chan struct{}
}

// NewModel constructs the root TUI model. The caller (cmd/gotit/tui_cmd.go)
// is responsible for loading specs into entries.
func NewModel(entries []*TestEntry, cfg GotitConfig, projectRoot string) Model {
	concurrency := runtime.NumCPU()
	if concurrency < 1 {
		concurrency = 4
	}
	tl := newTestListModel(entries, 30, 24)
	sv := newStepViewerModel(80, 24)
	if first := tl.SelectedEntry(); first != nil {
		sv.SetEntry(first)
	}
	return Model{
		focus:       focusTestList,
		entries:     entries,
		cfg:         cfg,
		projectRoot: projectRoot,
		sem:         make(chan struct{}, concurrency),
		testList:    tl,
		stepView:    sv,
	}
}

// SetProgram stores the tea.Program reference so goroutines can deliver
// events back into the Update loop.
func (m *Model) SetProgram(p *tea.Program) { m.program = p }

func (m *Model) Init() tea.Cmd {
	return tea.Batch(tickSpinner(), warmGlamourCmd())
}

func tickSpinner() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return spinnerTickMsg(t)
	})
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		cmd := m.layout()
		m.ready = true
		return m, cmd

	case spinnerTickMsg:
		m.testList.TickSpinner()
		return m, tickSpinner()

	case specEventMsg:
		return m.handleSpecEvent(msg)

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case shellOutputMsg:
		if m.shell != nil {
			m.shell.handleOutput(msg)
		}
		if m.shell != nil && !m.shell.exited {
			return m, m.shell.waitForOutput()
		}
		return m, nil

	case shellExitMsg:
		if m.focus == focusShell {
			m.focus = focusStepReview
		}
		if m.shell != nil {
			m.shell.exited = true
		}
		return m, nil

	case notesRenderedMsg:
		if m.notes != nil && m.notes.entry == msg.entry && m.notes.width == msg.width {
			m.notes.content = msg.content
			m.notes.renderedFor = cacheKey(msg.entry, msg.width)
			m.notes.viewport.SetContent(msg.content)
			m.notes.rendering = false
		}
		return m, nil

	case notesWarmupDoneMsg:
		return m, nil
	}

	if m.focus == focusSearch {
		var cmd tea.Cmd
		m.testList.searchInput, cmd = m.testList.searchInput.Update(msg)
		m.testList.UpdateSearch(m.testList.searchInput.Value())
		return m, cmd
	}
	if m.focus == focusStepReview {
		var cmd tea.Cmd
		m.stepView.viewport, cmd = m.stepView.viewport.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) handleSpecEvent(msg specEventMsg) (tea.Model, tea.Cmd) {
	if msg.idx < 0 || msg.idx >= len(m.entries) {
		return m, nil
	}
	entry := m.entries[msg.idx]
	ev := msg.event

	switch ev.Type {
	case "spec_start":
		entry.WorkDir = ev.WorkDir
		entry.HomeDir = ev.HomeDir
		entry.BinDir = ev.BinDir
		entry.Env = ev.Env
		entry.CurrentStep = -1

	case "step_start":
		entry.CurrentStep++

	case "step_done":
		entry.Steps = append(entry.Steps, &StepResult{
			Phase:      ev.Phase,
			Name:       ev.Step,
			Command:    ev.Command,
			ExitCode:   ev.ExitCode,
			DurationMs: ev.DurationMs,
			Stdout:     ev.Stdout,
			Stderr:     ev.Stderr,
			Assertions: ev.Assertions,
			Passed:     ev.Passed,
		})

	case "spec_end":
		entry.DurationMs = ev.DurationMs
		if ev.Passed {
			entry.State = StatePassed
		} else {
			entry.State = StateFailed
		}
		if ev.Error != "" {
			entry.Error = ev.Error
		}
	}

	if m.stepView.entry == entry {
		m.stepView.Refresh()
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.focus == focusNotes {
		if msg.String() == "n" {
			m.focus = focusStepReview
			return m, nil
		}
		if m.notes != nil {
			var cmd tea.Cmd
			m.notes.viewport, cmd = m.notes.viewport.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	if m.focus == focusShell {
		if msg.String() == "ctrl+\\" {
			m.focus = focusStepReview
			return m, nil
		}
		if m.shell != nil && !m.shell.exited {
			m.shell.sendKey(msg)
		}
		return m, nil
	}

	if m.focus == focusSearch {
		switch msg.Type {
		case tea.KeyEscape:
			m.testList.CloseSearch(false)
			m.focus = focusTestList
			return m, nil
		case tea.KeyEnter:
			m.testList.CloseSearch(true)
			m.focus = focusTestList
			return m, nil
		default:
			var cmd tea.Cmd
			m.testList.searchInput, cmd = m.testList.searchInput.Update(msg)
			m.testList.UpdateSearch(m.testList.searchInput.Value())
			return m, cmd
		}
	}

	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "q":
		if m.focus == focusTestList || m.focus == focusStepReview {
			return m, tea.Quit
		}
	case "tab":
		switch m.focus {
		case focusTestList:
			if m.stepView.entry != nil {
				m.focus = focusStepReview
			}
		case focusStepReview:
			m.focus = focusTestList
		}
		return m, nil
	}

	switch m.focus {
	case focusTestList:
		return m.handleTestListKey(msg)
	case focusStepReview:
		return m.handleStepReviewKey(msg)
	}
	return m, nil
}

func (m *Model) syncSelection() {
	entry := m.testList.SelectedEntry()
	if entry != nil {
		m.stepView.SetEntry(entry)
	}
}

func (m *Model) handleTestListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.testList.MoveUp()
		m.syncSelection()
	case "down", "j":
		m.testList.MoveDown()
		m.syncSelection()
	case "enter":
		if m.stepView.entry != nil {
			m.focus = focusStepReview
		}
	case "r":
		return m.runSelected()
	case "a":
		return m.runAll()
	case "s":
		return m.stopSelected()
	case "R":
		return m.resetAll()
	case "/":
		m.testList.StartSearch()
		m.focus = focusSearch
		return m, textinput.Blink
	case "n":
		return m.openNotes()
	}
	return m, nil
}

func (m *Model) handleStepReviewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.focus = focusTestList
	case "left", "h":
		m.stepView.PrevStep()
	case "right", "l":
		m.stepView.NextStep()
	case "up", "k":
		var cmd tea.Cmd
		m.stepView.viewport, cmd = m.stepView.viewport.Update(msg)
		return m, cmd
	case "down", "j":
		var cmd tea.Cmd
		m.stepView.viewport, cmd = m.stepView.viewport.Update(msg)
		return m, cmd
	case "enter":
		return m.enterShell()
	case "r":
		return m.runViewed()
	case "s":
		return m.stopViewed()
	case "a":
		return m.runAll()
	case "R":
		return m.resetAll()
	case "n":
		return m.openNotes()
	}
	return m, nil
}

func (m *Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	leftWidth := m.leftPanelWidth()
	if msg.X < leftWidth {
		if m.focus == focusShell || m.focus == focusStepReview {
			m.focus = focusTestList
		}
	} else {
		if m.focus == focusTestList && m.stepView.entry != nil {
			m.focus = focusStepReview
		}
	}
	return m, nil
}

// --- test execution ---

func (m *Model) runViewed() (tea.Model, tea.Cmd) {
	entry := m.stepView.entry
	if entry == nil || entry.State == StateSkipped || entry.State == StateRunning || entry.State == StateQueued {
		return m, nil
	}
	for i, e := range m.entries {
		if e == entry {
			return m.startSpec(i)
		}
	}
	return m, nil
}

func (m *Model) stopViewed() (tea.Model, tea.Cmd) {
	entry := m.stepView.entry
	if entry == nil || entry.State != StateRunning {
		return m, nil
	}
	entry.Cancel()
	return m, nil
}

func (m *Model) runSelected() (tea.Model, tea.Cmd) {
	idx := m.testList.SelectedIndex()
	if idx < 0 {
		return m, nil
	}
	entry := m.entries[idx]
	if entry.State == StateSkipped || entry.State == StateRunning || entry.State == StateQueued {
		return m, nil
	}
	return m.startSpec(idx)
}

func (m *Model) runAll() (tea.Model, tea.Cmd) {
	indices := m.testList.MatchingIndices()
	var cmds []tea.Cmd
	for _, idx := range indices {
		entry := m.entries[idx]
		if entry.State == StateSkipped || entry.State == StateRunning || entry.State == StateQueued {
			continue
		}
		newM, cmd := m.startSpec(idx)
		m = newM
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return m, tea.Batch(cmds...)
}

// startSpec spawns a `go test -run TestE2E/<wave>/<name>` invocation, tails
// the JSONL log, and feeds runner.SpecEvents back into the Update loop.
//
// Architectural note (Phase 2): we no longer call runner.RunSpec directly —
// that would require importing the consumer's helpers/checkers/custom
// assertions, which the global gotit binary cannot do. The JSONL stream is
// the cross-process bridge.
func (m *Model) startSpec(idx int) (*Model, tea.Cmd) {
	entry := m.entries[idx]

	if m.shell != nil && m.shell.specName == entry.Spec.Wave+"/"+entry.Spec.Name {
		m.shell.Close()
		m.shell = nil
	}
	if entry.HomeDir != "" {
		os.RemoveAll(entry.HomeDir)
		entry.HomeDir = ""
		entry.WorkDir = ""
	}
	entry.Steps = nil
	entry.CurrentStep = -1
	entry.Error = ""
	entry.DurationMs = 0
	entry.State = StateQueued

	p := m.program
	cfg := m.cfg
	projectRoot := m.projectRoot
	wave := entry.Spec.Wave
	name := entry.Spec.Name

	cmd := func() tea.Msg {
		m.sem <- struct{}{}
		defer func() { <-m.sem }()

		entry.mu.Lock()
		entry.State = StateRunning
		ctx, cancel := context.WithCancel(context.Background())
		entry.cancel = cancel
		entry.mu.Unlock()

		events := make(chan gtrunner.SpecEvent, 100)
		go func() {
			for ev := range events {
				if p != nil {
					p.Send(specEventMsg{idx: idx, event: ev})
				}
			}
		}()

		_, err := SpawnGoTest(ctx, projectRoot, cfg.SpecsDir, wave, name, events)
		if err != nil && p != nil {
			// `go test` non-zero is normal when a spec fails; the JSONL
			// stream already conveyed the failure. Only record genuine
			// invocation errors (e.g. couldn't spawn).
			if !strings.Contains(err.Error(), "exit status") {
				p.Send(specEventMsg{idx: idx, event: gtrunner.SpecEvent{
					Type: "spec_end", Wave: wave, Spec: name,
					Passed: false, Error: err.Error(),
				}})
			}
		}
		return nil
	}
	return m, cmd
}

func (m *Model) stopSelected() (tea.Model, tea.Cmd) {
	entry := m.testList.SelectedEntry()
	if entry == nil || entry.State != StateRunning {
		return m, nil
	}
	entry.Cancel()
	return m, nil
}

// resetAll clears state for every non-skipped entry. The CLI binary is
// rebuilt automatically on the next `go test` invocation, so there's no
// separate "rebuild" step for the TUI.
func (m *Model) resetAll() (tea.Model, tea.Cmd) {
	for _, entry := range m.entries {
		if entry.State == StateRunning {
			entry.Cancel()
		}
		if entry.State != StateSkipped {
			if entry.HomeDir != "" {
				os.RemoveAll(entry.HomeDir)
				entry.HomeDir = ""
				entry.WorkDir = ""
			}
			entry.State = StatePending
			entry.Steps = nil
			entry.CurrentStep = -1
			entry.Error = ""
			entry.DurationMs = 0
		}
	}
	if m.shell != nil {
		m.shell.Close()
		m.shell = nil
	}
	return m, nil
}

// --- shell ---

func (m *Model) enterShell() (tea.Model, tea.Cmd) {
	entry := m.stepView.entry
	if entry == nil || entry.WorkDir == "" {
		return m, nil
	}
	if entry.State != StatePassed && entry.State != StateFailed {
		return m, nil
	}

	specName := entry.Spec.Wave + "/" + entry.Spec.Name
	if m.shell != nil && !m.shell.exited && m.shell.specName == specName {
		m.focus = focusShell
		return m, m.shell.waitForOutput()
	}
	if m.shell != nil && !m.shell.exited {
		m.shell.Close()
		m.shell = nil
	}

	info := &shellSpecInfo{
		Name:    entry.Spec.Name,
		Wave:    entry.Spec.Wave,
		WorkDir: entry.WorkDir,
		HomeDir: entry.HomeDir,
		BinDir:  entry.BinDir,
		Env:     entry.Env,
	}
	_, rightH := m.rightPanelDimensions()
	shellW := m.width - m.leftPanelWidth() - 4
	shellH := rightH - 4

	shell, cmd, err := newShellFromEntry(info, shellW, shellH)
	if err != nil {
		return m, nil
	}
	m.shell = shell
	m.focus = focusShell
	return m, cmd
}

// --- layout ---

func (m *Model) layout() tea.Cmd {
	leftW := m.leftPanelWidth()
	rightW, _ := m.rightPanelDimensions()
	bodyH := m.height - 2

	m.testList.SetSize(leftW, bodyH-2)
	m.stepView.SetSize(rightW, bodyH-2)

	if m.shell != nil && !m.shell.exited {
		m.shell.resize(rightW-4, bodyH-6)
	}
	var notesCmd tea.Cmd
	if m.notes != nil {
		notesCmd = m.notes.resize(rightW-4, bodyH-6)
	}
	return notesCmd
}

func (m *Model) leftPanelWidth() int {
	w := m.width / 3
	if w > 30 {
		w = 30
	}
	if w < 15 {
		w = 15
	}
	return w
}

func (m *Model) rightPanelDimensions() (int, int) {
	return m.width - m.leftPanelWidth(), m.height - 2
}

// --- view ---

func (m *Model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	leftView := m.testList.View(m.focus == focusTestList || m.focus == focusSearch)

	var rightView string
	switch {
	case m.focus == focusNotes && m.notes != nil:
		rightView = m.renderNotesPanel()
	case m.focus == focusShell && m.shell != nil && !m.shell.exited:
		rightView = m.renderShellPanel()
	default:
		shellActive := m.shell != nil && !m.shell.exited && m.stepView.entry != nil &&
			m.shell.specName == m.stepView.entry.Spec.Wave+"/"+m.stepView.entry.Spec.Name
		rightView = m.stepView.View(m.focus == focusStepReview, shellActive)
	}

	body := lipgloss.JoinHorizontal(lipgloss.Top, leftView, rightView)
	footer := m.renderFooter()
	return lipgloss.JoinVertical(lipgloss.Left, body, footer)
}

func (m *Model) renderShellPanel() string {
	if m.shell == nil {
		return ""
	}
	headerText := fmt.Sprintf("%s  %s",
		m.shell.specName,
		shellActiveStyle.Render("[shell active — ctrl+\\ to exit]"))
	header := headerStyle.Render(headerText)
	content := m.shell.View()

	rightW, _ := m.rightPanelDimensions()
	body := lipgloss.JoinVertical(lipgloss.Left, header, "", content)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("82")).
		Width(rightW - 2).
		Height(m.height - 4).
		Render(body)
}

func (m *Model) renderNotesPanel() string {
	if m.notes == nil {
		return ""
	}
	specName := "notes"
	if m.notes.entry != nil && m.notes.entry.Spec != nil {
		specName = m.notes.entry.Spec.Wave + "/" + m.notes.entry.Spec.Name
	}
	scrollPct := int(m.notes.viewport.ScrollPercent() * 100)
	headerText := fmt.Sprintf("%s  %s  %s",
		specName,
		shellActiveStyle.Render("[notes — n to close]"),
		dimStyle.Render(fmt.Sprintf("%d%%", scrollPct)))
	header := headerStyle.Render(headerText)
	content := m.notes.View()

	rightW, _ := m.rightPanelDimensions()
	body := lipgloss.JoinVertical(lipgloss.Left, header, "", content)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("82")).
		Width(rightW - 2).
		Height(m.height - 4).
		Render(body)
}

func (m *Model) renderFooter() string {
	var pass, fail, running, queued int
	for _, e := range m.entries {
		switch e.State {
		case StatePassed:
			pass++
		case StateFailed:
			fail++
		case StateRunning:
			running++
		case StateQueued:
			queued++
		}
	}
	var statParts []string
	if pass > 0 {
		statParts = append(statParts, passedStyle.Render(fmt.Sprintf("✓ %d", pass)))
	}
	if fail > 0 {
		statParts = append(statParts, failedStyle.Render(fmt.Sprintf("✗ %d", fail)))
	}
	if running > 0 {
		statParts = append(statParts, runningStyle.Render(fmt.Sprintf("● %d", running)))
	}
	if queued > 0 {
		statParts = append(statParts, queuedStyle.Render(fmt.Sprintf("◎ %d", queued)))
	}
	stats := " " + strings.Join(statParts, "  ")

	if m.testList.filter != "" {
		matching := len(m.testList.MatchingIndices())
		total := len(m.entries)
		stats += "  " + filterBadgeStyle.Render(fmt.Sprintf("(%d/%d matching)", matching, total))
	}

	var keys string
	switch m.focus {
	case focusTestList:
		keys = fmt.Sprintf("%s nav  %s view  %s run  %s all  %s stop  %s search  %s notes  %s reset  %s quit",
			footerKeyStyle.Render("↑↓"),
			footerKeyStyle.Render("⏎"),
			footerKeyStyle.Render("r"),
			footerKeyStyle.Render("a"),
			footerKeyStyle.Render("s"),
			footerKeyStyle.Render("/"),
			footerKeyStyle.Render("n"),
			footerKeyStyle.Render("R"),
			footerKeyStyle.Render("q"))
	case focusStepReview:
		keys = fmt.Sprintf("%s steps  %s scroll  %s shell  %s notes  %s back  %s quit",
			footerKeyStyle.Render("←→"),
			footerKeyStyle.Render("↑↓"),
			footerKeyStyle.Render("⏎"),
			footerKeyStyle.Render("n"),
			footerKeyStyle.Render("esc"),
			footerKeyStyle.Render("q"))
	case focusShell:
		keys = fmt.Sprintf("%s exit shell", footerKeyStyle.Render("ctrl+\\"))
	case focusNotes:
		keys = fmt.Sprintf("%s scroll  %s toggle notes",
			footerKeyStyle.Render("↑↓"),
			footerKeyStyle.Render("n"))
	case focusSearch:
		keys = fmt.Sprintf("%s apply  %s clear",
			footerKeyStyle.Render("⏎"),
			footerKeyStyle.Render("esc"))
	}

	statsWidth := lipgloss.Width(stats)
	keysWidth := lipgloss.Width(keys)
	padding := m.width - statsWidth - keysWidth - 2
	if padding < 1 {
		padding = 1
	}
	footer := stats + strings.Repeat(" ", padding) + keys
	return footerStyle.Render(footer)
}

// Cleanup kills shell processes and removes preserved temp HOMEs. The TUI
// is responsible for reaping HOMEs because GOTIT_KEEP_HOMEDIR=1 told the
// runner not to.
func (m *Model) Cleanup() {
	if m.shell != nil {
		m.shell.Close()
	}
	for _, entry := range m.entries {
		if entry.State == StateRunning {
			entry.Cancel()
		}
		if entry.HomeDir != "" {
			os.RemoveAll(entry.HomeDir)
		}
	}
}

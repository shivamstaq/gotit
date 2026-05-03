package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
)

// testListModel is the left panel: specs grouped by wave, status markers,
// search/filter input, cursor navigation.
type testListModel struct {
	entries []*TestEntry
	cursor  int
	offset  int // scroll offset
	width   int
	height  int

	searchActive bool
	searchInput  textinput.Model
	filter       string
	filteredIdx  []int

	spinnerFrame int
}

func newTestListModel(entries []*TestEntry, width, height int) testListModel {
	ti := textinput.New()
	ti.Placeholder = "filter by name, tag..."
	ti.CharLimit = 64

	m := testListModel{
		entries:     entries,
		width:       width,
		height:      height,
		searchInput: ti,
	}
	m.rebuildFilter()
	return m
}

func (m *testListModel) rebuildFilter() {
	if m.filter == "" {
		m.filteredIdx = nil
		return
	}
	m.filteredIdx = nil
	lower := strings.ToLower(m.filter)
	for i, e := range m.entries {
		if strings.Contains(strings.ToLower(e.Spec.Name), lower) ||
			strings.Contains(strings.ToLower(e.Spec.Description), lower) ||
			strings.Contains(strings.ToLower(e.Spec.Wave), lower) {
			m.filteredIdx = append(m.filteredIdx, i)
			continue
		}
		for _, tag := range e.Spec.Tags {
			if strings.Contains(strings.ToLower(tag), lower) {
				m.filteredIdx = append(m.filteredIdx, i)
				break
			}
		}
	}
}

func (m *testListModel) visibleEntries() []*TestEntry {
	if m.filteredIdx == nil {
		return m.entries
	}
	out := make([]*TestEntry, 0, len(m.filteredIdx))
	for _, i := range m.filteredIdx {
		out = append(out, m.entries[i])
	}
	return out
}

// listItem is one row in the rendered list — either a wave header or an entry.
type listItem struct {
	entry   *TestEntry
	isWave  bool
	wave    string
	realIdx int // index in m.entries, valid only when !isWave
}

func (m *testListModel) buildItems() []listItem {
	visible := m.visibleEntries()
	var items []listItem
	currentWave := ""
	for _, e := range visible {
		if e.Spec.Wave != currentWave {
			currentWave = e.Spec.Wave
			items = append(items, listItem{isWave: true, wave: currentWave})
		}
		idx := -1
		for i, orig := range m.entries {
			if orig == e {
				idx = i
				break
			}
		}
		items = append(items, listItem{entry: e, realIdx: idx})
	}
	return items
}

func (m *testListModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m *testListModel) MoveUp() {
	items := m.buildItems()
	for i := m.cursor - 1; i >= 0; i-- {
		if !items[i].isWave {
			m.cursor = i
			m.ensureVisible()
			return
		}
	}
}

func (m *testListModel) MoveDown() {
	items := m.buildItems()
	for i := m.cursor + 1; i < len(items); i++ {
		if !items[i].isWave {
			m.cursor = i
			m.ensureVisible()
			return
		}
	}
}

func (m *testListModel) SelectedEntry() *TestEntry {
	items := m.buildItems()
	if m.cursor < 0 || m.cursor >= len(items) {
		return nil
	}
	item := items[m.cursor]
	if item.isWave {
		return nil
	}
	return item.entry
}

func (m *testListModel) SelectedIndex() int {
	items := m.buildItems()
	if m.cursor < 0 || m.cursor >= len(items) {
		return -1
	}
	return items[m.cursor].realIdx
}

func (m *testListModel) ensureVisible() {
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+m.height {
		m.offset = m.cursor - m.height + 1
	}
}

// MatchingIndices returns all entry indices that match the active filter
// (or all indices if no filter is set). Used by "run all".
func (m *testListModel) MatchingIndices() []int {
	if m.filteredIdx == nil {
		indices := make([]int, len(m.entries))
		for i := range indices {
			indices[i] = i
		}
		return indices
	}
	return m.filteredIdx
}

func (m *testListModel) StartSearch() {
	m.searchActive = true
	m.searchInput.Focus()
	m.searchInput.SetValue(m.filter)
}

func (m *testListModel) CloseSearch(keepFilter bool) {
	m.searchActive = false
	m.searchInput.Blur()
	if !keepFilter {
		m.filter = ""
		m.rebuildFilter()
	}
	items := m.buildItems()
	for i, item := range items {
		if !item.isWave {
			m.cursor = i
			break
		}
	}
	m.offset = 0
}

func (m *testListModel) UpdateSearch(val string) {
	m.filter = val
	m.rebuildFilter()
	items := m.buildItems()
	for i, item := range items {
		if !item.isWave {
			m.cursor = i
			break
		}
	}
	m.offset = 0
}

func (m *testListModel) TickSpinner() {
	m.spinnerFrame++
}

func (m *testListModel) View(focused bool) string {
	items := m.buildItems()
	innerWidth := m.width - 4
	if innerWidth < 5 {
		innerWidth = 5
	}

	var lines []string
	if m.searchActive {
		lines = append(lines, searchPromptStyle.Render("/")+m.searchInput.View())
	} else if m.filter != "" {
		lines = append(lines, filterBadgeStyle.Render(fmt.Sprintf("filter: %s", m.filter)))
	}

	availH := m.height
	if len(lines) > 0 {
		availH -= len(lines)
	}

	end := m.offset + availH
	if end > len(items) {
		end = len(items)
	}

	for i := m.offset; i < end; i++ {
		item := items[i]
		if item.isWave {
			lines = append(lines, waveHeader.Render(item.wave))
			continue
		}
		ind := stateIndicator(item.entry.State, m.spinnerFrame)
		name := item.entry.Spec.Name
		maxName := innerWidth - 4
		if maxName > 0 && len(name) > maxName {
			name = name[:maxName-1] + "…"
		}
		line := fmt.Sprintf("  %s %s", ind, name)
		if i == m.cursor && focused && !m.searchActive {
			line = selectedStyle.Width(innerWidth).Render(stripAnsi(line))
		}
		lines = append(lines, line)
	}
	for len(lines) < m.height {
		lines = append(lines, "")
	}

	content := strings.Join(lines, "\n")

	borderColor := lipgloss.Color("240")
	if focused {
		borderColor = lipgloss.Color("15")
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(m.width - 2).
		Height(m.height).
		Render(content)
}

// stripAnsi removes ANSI escape sequences for re-rendering with a different style.
func stripAnsi(s string) string {
	var out strings.Builder
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

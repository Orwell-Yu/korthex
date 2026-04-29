package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/Orwell-Yu/korthex/internal/config"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
)

// kubeSwitchStep tracks the current step in the kubeconfig switch flow.
type kubeSwitchStep int

const (
	stepSelectKubeconfig kubeSwitchStep = iota
	stepSelectContext
	stepConnecting
	stepError
)

// KubeSwitchModel is an overlay for selecting a kubeconfig file and context.
// Two-step flow: select kubeconfig file -> select context within that file.
type KubeSwitchModel struct {
	visible bool
	step    kubeSwitchStep

	// Kubeconfig list (step 1)
	kubeconfigs  []config.KubeconfigEntry
	kubeCursor   int
	kubeFiltered []int // indices into kubeconfigs after fuzzy filter

	// Context list (step 2)
	contexts    []config.ContextEntry
	ctxCursor   int
	ctxFiltered []int // indices into contexts after fuzzy filter

	// Search state
	searchMode  bool
	searchQuery string

	// Current active kubeconfig/context (for "(current)" marker)
	currentKubeconfig string
	currentContext    string

	// Selected kubeconfig path (chosen in step 1, used to load contexts in step 2)
	selectedKubeconfig string

	// Error message (step error)
	errMsg string

	// Dimensions
	width  int
	height int
	theme  Theme
}

// NewKubeSwitchModel creates a KubeSwitchModel with the given theme.
func NewKubeSwitchModel(theme Theme) KubeSwitchModel {
	return KubeSwitchModel{theme: theme}
}

// Show opens the overlay, discovers kubeconfig files, and auto-skips step 1
// if only one kubeconfig exists.
func (m *KubeSwitchModel) Show(currentKubeconfig, currentContext, kubeDir, envKubeconfig string) {
	m.visible = true
	m.currentKubeconfig = currentKubeconfig
	m.currentContext = currentContext
	m.searchMode = false
	m.searchQuery = ""
	m.errMsg = ""

	m.kubeconfigs = config.DiscoverKubeconfigs(kubeDir, envKubeconfig)
	m.kubeCursor = 0
	m.kubeFiltered = allIndices(len(m.kubeconfigs))

	// Auto-skip step 1 if only one kubeconfig file
	if len(m.kubeconfigs) == 1 {
		m.selectedKubeconfig = m.kubeconfigs[0].Path
		m.loadContexts()
		m.step = stepSelectContext
	} else {
		m.step = stepSelectKubeconfig
	}
}

// Visible returns whether the overlay is currently shown.
func (m KubeSwitchModel) Visible() bool {
	return m.visible
}

// Close hides the overlay.
func (m *KubeSwitchModel) Close() {
	m.visible = false
}

// ShowError switches the overlay to the error state.
func (m *KubeSwitchModel) ShowError(err error) {
	m.errMsg = err.Error()
	m.step = stepError
}

// SetConnecting switches the overlay to the connecting state.
func (m *KubeSwitchModel) SetConnecting() {
	m.step = stepConnecting
}

// SetDimensions updates the overlay dimensions.
func (m *KubeSwitchModel) SetDimensions(w, h int) {
	m.width = w
	m.height = h
}

// Update handles messages when the overlay is visible.
func (m KubeSwitchModel) Update(msg tea.Msg) (KubeSwitchModel, tea.Cmd) {
	if !m.visible {
		return m, nil
	}

	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch m.step {
	case stepSelectKubeconfig:
		return m.updateKubeconfigSelect(keyMsg)
	case stepSelectContext:
		return m.updateContextSelect(keyMsg)
	case stepError:
		return m.updateError(keyMsg)
	case stepConnecting:
		// Non-interactive; ignore all keys
		return m, nil
	}

	return m, nil
}

// --- Step handlers ---

func (m KubeSwitchModel) updateKubeconfigSelect(msg tea.KeyMsg) (KubeSwitchModel, tea.Cmd) {
	if m.searchMode {
		return m.updateSearch(msg, true)
	}

	switch msg.String() {
	case "j", "down":
		if m.kubeCursor < len(m.kubeFiltered)-1 {
			m.kubeCursor++
		}
	case "k", "up":
		if m.kubeCursor > 0 {
			m.kubeCursor--
		}
	case "enter":
		if len(m.kubeFiltered) > 0 {
			idx := m.kubeFiltered[m.kubeCursor]
			m.selectedKubeconfig = m.kubeconfigs[idx].Path
			m.loadContexts()
			m.step = stepSelectContext
			m.searchMode = false
			m.searchQuery = ""
		}
	case "esc":
		m.visible = false
	case "/":
		m.searchMode = true
		m.searchQuery = ""
	}
	return m, nil
}

func (m KubeSwitchModel) updateContextSelect(msg tea.KeyMsg) (KubeSwitchModel, tea.Cmd) {
	if m.searchMode {
		return m.updateSearch(msg, false)
	}

	switch msg.String() {
	case "j", "down":
		if m.ctxCursor < len(m.ctxFiltered)-1 {
			m.ctxCursor++
		}
	case "k", "up":
		if m.ctxCursor > 0 {
			m.ctxCursor--
		}
	case "enter":
		if len(m.ctxFiltered) > 0 {
			idx := m.ctxFiltered[m.ctxCursor]
			ctxName := m.contexts[idx].Name
			kubeconfig := m.selectedKubeconfig
			m.step = stepConnecting
			return m, func() tea.Msg {
				return kubeSwitchExecuteMsg{
					Kubeconfig: kubeconfig,
					Context:    ctxName,
				}
			}
		}
	case "esc", "backspace":
		// Go back to step 1, or close if only 1 kubeconfig
		if len(m.kubeconfigs) <= 1 {
			m.visible = false
		} else {
			m.step = stepSelectKubeconfig
			m.searchMode = false
			m.searchQuery = ""
			m.kubeFiltered = allIndices(len(m.kubeconfigs))
		}
	case "/":
		m.searchMode = true
		m.searchQuery = ""
	}
	return m, nil
}

func (m KubeSwitchModel) updateError(msg tea.KeyMsg) (KubeSwitchModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		// Retry: go back to step 1
		m.step = stepSelectKubeconfig
		m.errMsg = ""
		m.searchMode = false
		m.searchQuery = ""
		m.kubeFiltered = allIndices(len(m.kubeconfigs))
		m.kubeCursor = 0
	case "esc":
		m.visible = false
	}
	return m, nil
}

func (m KubeSwitchModel) updateSearch(msg tea.KeyMsg, isKube bool) (KubeSwitchModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.searchMode = false
		m.searchQuery = ""
		// Reset filter to show all items
		if isKube {
			m.kubeFiltered = allIndices(len(m.kubeconfigs))
			m.kubeCursor = 0
		} else {
			m.ctxFiltered = allIndices(len(m.contexts))
			m.ctxCursor = 0
		}
	case "enter":
		// Accept the current filter and exit search mode
		m.searchMode = false
	case "backspace":
		if len(m.searchQuery) > 0 {
			// CJK-safe backspace: use utf8.DecodeLastRuneInString (CLAUDE.md Rule #9)
			_, size := utf8.DecodeLastRuneInString(m.searchQuery)
			m.searchQuery = m.searchQuery[:len(m.searchQuery)-size]
			m.applyFilter(isKube)
		}
	default:
		// Character input via msg.Runes (CJK-safe per CLAUDE.md Rule #9)
		if len(msg.Runes) > 0 {
			m.searchQuery += string(msg.Runes)
			m.applyFilter(isKube)
		}
	}
	return m, nil
}

// --- Filter ---

func (m *KubeSwitchModel) applyFilter(isKube bool) {
	if isKube {
		if m.searchQuery == "" {
			m.kubeFiltered = allIndices(len(m.kubeconfigs))
			m.kubeCursor = 0
			return
		}
		names := make([]string, len(m.kubeconfigs))
		for i, kc := range m.kubeconfigs {
			names[i] = kc.Path
		}
		matches := fuzzy.Find(m.searchQuery, names)
		m.kubeFiltered = make([]int, len(matches))
		for i, match := range matches {
			m.kubeFiltered[i] = match.Index
		}
		m.kubeCursor = 0
	} else {
		if m.searchQuery == "" {
			m.ctxFiltered = allIndices(len(m.contexts))
			m.ctxCursor = 0
			return
		}
		names := make([]string, len(m.contexts))
		for i, ctx := range m.contexts {
			names[i] = ctx.Name
		}
		matches := fuzzy.Find(m.searchQuery, names)
		m.ctxFiltered = make([]int, len(matches))
		for i, match := range matches {
			m.ctxFiltered[i] = match.Index
		}
		m.ctxCursor = 0
	}
}

// --- Private helpers ---

func (m *KubeSwitchModel) loadContexts() {
	ctxs, err := config.ParseContexts(m.selectedKubeconfig)
	if err != nil {
		m.contexts = nil
		m.ctxFiltered = nil
		m.ctxCursor = 0
		m.errMsg = fmt.Sprintf("Failed to parse contexts: %v", err)
		m.step = stepError
		return
	}
	m.contexts = ctxs
	m.ctxFiltered = allIndices(len(ctxs))
	m.ctxCursor = 0
	m.searchMode = false
	m.searchQuery = ""
}

func allIndices(n int) []int {
	indices := make([]int, n)
	for i := range indices {
		indices[i] = i
	}
	return indices
}

// --- View ---

// View renders the overlay centered within the given width and height.
func (m KubeSwitchModel) View(width, height int) string {
	if !m.visible {
		return ""
	}

	// Calculate overlay dimensions: 60% of terminal, clamped to min/max
	overlayW := width * 60 / 100
	if overlayW > 80 {
		overlayW = 80
	}
	if overlayW < 30 {
		overlayW = 30
	}

	overlayH := height * 60 / 100
	if overlayH > 20 {
		overlayH = 20
	}
	if overlayH < 8 {
		overlayH = 8
	}

	// Inner dimensions (subtract border)
	innerW := max(overlayW-4, 1) // rounded border = 1 char each side + 1 padding each side
	innerH := max(overlayH-2, 1) // rounded border = 1 char top/bottom

	var content string
	switch m.step {
	case stepSelectKubeconfig:
		content = m.renderKubeconfigList(innerW, innerH)
	case stepSelectContext:
		content = m.renderContextList(innerW, innerH)
	case stepConnecting:
		content = m.renderConnecting(innerW)
	case stepError:
		content = m.renderError(innerW, innerH)
	}

	// Build the overlay box with rounded border
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.ActiveBorder.GetBorderTopForeground()).
		Width(innerW).
		Height(innerH).
		Padding(0, 1)

	box := boxStyle.Render(content)

	// Center the box in the terminal (lipgloss.Place fills the full canvas,
	// unlike manual padding which only shifts the first line of multi-line content)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

// --- Render methods ---

func (m KubeSwitchModel) renderKubeconfigList(w, h int) string {
	var b strings.Builder

	title := m.theme.Title.Render("Switch Kubeconfig [1/2]")
	b.WriteString(title)
	b.WriteString("\n\n")

	// Calculate available lines for the list
	usedLines := 3 // title + blank + footer
	if m.searchMode {
		usedLines++ // search bar
	}
	listH := max(h-usedLines, 1)

	// Scroll offset for long lists
	scrollOff := 0
	if m.kubeCursor >= listH {
		scrollOff = m.kubeCursor - listH + 1
	}

	rendered := 0
	for i, filtIdx := range m.kubeFiltered {
		if i < scrollOff {
			continue
		}
		if rendered >= listH {
			break
		}
		kc := m.kubeconfigs[filtIdx]

		// Display shortened path (use base dir + filename)
		display := shortenPath(kc.Path)
		suffix := fmt.Sprintf(" (%d contexts)", kc.ContextCount)

		// Mark current
		if kc.Path == m.currentKubeconfig {
			suffix += " (current)"
		}

		line := display + suffix
		// Truncate to fit width
		if len(line) > w {
			line = line[:max(w-3, 0)] + "..."
		}

		if i == m.kubeCursor {
			b.WriteString(m.theme.Selected.Render("> " + line))
		} else {
			b.WriteString("  " + line)
		}
		b.WriteString("\n")
		rendered++
	}

	if len(m.kubeFiltered) == 0 {
		b.WriteString(m.theme.Subtitle.Render("  No matching kubeconfig files."))
		b.WriteString("\n")
	}

	// Search bar
	if m.searchMode {
		b.WriteString(m.theme.Subtitle.Render("/ " + m.searchQuery + "_"))
		b.WriteString("\n")
	}

	// Footer
	footer := "j/k:navigate  Enter:select  /:search  Esc:close"
	b.WriteString(m.theme.Subtitle.Render(footer))

	return b.String()
}

func (m KubeSwitchModel) renderContextList(w, h int) string {
	var b strings.Builder

	title := m.theme.Title.Render("Select Context [2/2]")
	b.WriteString(title)
	b.WriteString("\n")

	// Show which kubeconfig is selected
	kubeLabel := m.theme.Subtitle.Render(shortenPath(m.selectedKubeconfig))
	b.WriteString(kubeLabel)
	b.WriteString("\n\n")

	// Calculate available lines for the list
	usedLines := 4 // title + kubeconfig label + blank + footer
	if m.searchMode {
		usedLines++
	}
	listH := max(h-usedLines, 1)

	// Scroll offset
	scrollOff := 0
	if m.ctxCursor >= listH {
		scrollOff = m.ctxCursor - listH + 1
	}

	rendered := 0
	for i, filtIdx := range m.ctxFiltered {
		if i < scrollOff {
			continue
		}
		if rendered >= listH {
			break
		}
		ctx := m.contexts[filtIdx]

		display := ctx.Name
		if ctx.Cluster != "" {
			display += " [" + ctx.Cluster + "]"
		}

		// Mark current
		if ctx.Current && m.selectedKubeconfig == m.currentKubeconfig {
			display += " (current)"
		}

		// Truncate
		if len(display) > w {
			display = display[:max(w-3, 0)] + "..."
		}

		if i == m.ctxCursor {
			b.WriteString(m.theme.Selected.Render("> " + display))
		} else {
			b.WriteString("  " + display)
		}
		b.WriteString("\n")
		rendered++
	}

	if len(m.ctxFiltered) == 0 {
		b.WriteString(m.theme.Subtitle.Render("  No matching contexts."))
		b.WriteString("\n")
	}

	// Search bar
	if m.searchMode {
		b.WriteString(m.theme.Subtitle.Render("/ " + m.searchQuery + "_"))
		b.WriteString("\n")
	}

	// Footer
	footer := "j/k:navigate  Enter:select  /:search  Esc/BS:back"
	b.WriteString(m.theme.Subtitle.Render(footer))

	return b.String()
}

func (m KubeSwitchModel) renderConnecting(w int) string {
	msg := "Connecting..."
	padTop := 2
	return strings.Repeat("\n", padTop) +
		strings.Repeat(" ", max((w-len(msg))/2, 0)) +
		m.theme.Status.Render(msg)
}

func (m KubeSwitchModel) renderError(w, _ int) string {
	var b strings.Builder

	b.WriteString(m.theme.Error.Render("Error"))
	b.WriteString("\n\n")

	// Wrap error message to fit width
	errText := m.errMsg
	if len(errText) > w*3 {
		errText = errText[:max(w*3-3, 0)] + "..."
	}
	b.WriteString(m.theme.Status.Render(errText))
	b.WriteString("\n\n")
	b.WriteString(m.theme.Subtitle.Render("Enter:retry  Esc:close"))

	return b.String()
}

// shortenPath returns a display-friendly version of a kubeconfig path.
// For paths under ~/.kube, shows "~/.kube/filename"; otherwise shows the last 2 components.
func shortenPath(p string) string {
	dir := filepath.Dir(p)
	base := filepath.Base(p)
	dirBase := filepath.Base(dir)

	return filepath.Join("~", dirBase, base)
}

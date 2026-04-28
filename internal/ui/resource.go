package ui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Orwell-Yu/korthex/internal/k8s"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
)

// Navigation levels in the resource browser.
const (
	levelNamespace  = 0
	levelDeployment = 1
	levelPod        = 2
	levelContainer  = 3
)

// ResourceModel implements a hierarchical resource browser.
type ResourceModel struct {
	level  int
	cursor int

	// Phase 2: Current resource kind (active within a namespace)
	resourceKind k8s.ResourceKind

	// Data at each level
	namespaces  []k8s.Namespace
	deployments []k8s.Deployment
	pods        []k8s.Pod
	containers  []k8s.Container

	// Phase 2: Registry-based resource items (for non-Deployment kinds)
	resourceItems []k8s.ResourceItem

	// Selected context for navigation
	selectedNS  string
	selectedDep string
	selectedPod string

	// Search/filter
	searching   bool
	searchQuery string
	filtered    []int // indices into current list

	// Describe overlay
	describeResult string // non-empty means show describe output

	// Pending highlight from NavigateToResourceMsg
	pendingHighlight string // resource name to cursor-to when data loads

	// Dependencies
	k8sClient k8s.Client

	// Dimensions
	width, height int
	theme         Theme
}

// NewResourceModel creates a ResourceModel.
func NewResourceModel(k8sClient k8s.Client, theme Theme) ResourceModel {
	return ResourceModel{
		k8sClient:    k8sClient,
		theme:        theme,
		resourceKind: k8s.KindDeployment, // default to Deployments
	}
}

// Init implements tea.Model.
func (m ResourceModel) Init() tea.Cmd {
	return nil
}

// --- tea.Cmd functions that load data asynchronously ---

func loadNamespaces(client k8s.Client) tea.Cmd {
	return func() tea.Msg {
		if client == nil {
			return ErrorMsg{Err: fmt.Errorf("k8s client not initialized")}
		}
		ns, err := client.Resources().ListNamespaces()
		if err != nil {
			return ErrorMsg{Err: fmt.Errorf("list namespaces: %w", err)}
		}
		names := make([]string, len(ns))
		for i, n := range ns {
			names[i] = n.Name
		}
		return ClusterConnectedMsg{Namespaces: names}
	}
}

type deploymentsLoadedMsg struct {
	Deployments []k8s.Deployment
}

func loadDeployments(client k8s.Client, ns string) tea.Cmd {
	return func() tea.Msg {
		deps, err := client.Resources().ListDeployments(ns)
		if err != nil {
			return ErrorMsg{Err: fmt.Errorf("list deployments: %w", err)}
		}
		return deploymentsLoadedMsg{Deployments: deps}
	}
}

type podsLoadedMsg struct {
	Pods []k8s.Pod
}

func loadPods(client k8s.Client, ns string) tea.Cmd {
	return func() tea.Msg {
		pods, err := client.Resources().ListPods(ns)
		if err != nil {
			return ErrorMsg{Err: fmt.Errorf("list pods: %w", err)}
		}
		return podsLoadedMsg{Pods: pods}
	}
}

type describeResultMsg struct {
	Result string
}

func loadDescribe(client k8s.Client, ns, kind, name string) tea.Cmd {
	return func() tea.Msg {
		result, err := client.Describer().Describe(ns, kind, name)
		if err != nil {
			return ErrorMsg{Err: fmt.Errorf("describe %s/%s: %w", kind, name, err)}
		}
		return describeResultMsg{Result: result}
	}
}

// loadResourceItems uses the registry pattern to load any resource kind.
func loadResourceItems(client k8s.Client, ns string, kind k8s.ResourceKind) tea.Cmd {
	return func() tea.Msg {
		accessor, ok := k8s.AccessorFor(kind)
		if !ok {
			return ErrorMsg{Err: fmt.Errorf("unknown resource kind: %s", kind)}
		}
		items, err := accessor.List(client.Resources(), ns)
		if err != nil {
			return ErrorMsg{Err: fmt.Errorf("list %s: %w", kind, err)}
		}
		return resourceItemsLoadedMsg{Kind: kind, Items: items}
	}
}

// Update handles keyboard input for the resource browser.
func (m ResourceModel) Update(msg tea.Msg) (ResourceModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case ClusterConnectedMsg:
		m.namespaces = make([]k8s.Namespace, len(msg.Namespaces))
		for i, name := range msg.Namespaces {
			m.namespaces[i] = k8s.Namespace{Name: name}
		}
		m.level = levelNamespace
		m.cursor = 0
		m.clearSearch()
		if m.pendingHighlight != "" {
			for i, ns := range m.namespaces {
				if strings.EqualFold(ns.Name, m.pendingHighlight) {
					m.cursor = i
					break
				}
			}
			m.pendingHighlight = ""
		}

	case InformerUpdateMsg:
		return m, m.reloadCurrentLevel()

	case deploymentsLoadedMsg:
		m.deployments = msg.Deployments
		m.cursor = 0
		m.clearSearch()
		if m.pendingHighlight != "" {
			for i, d := range m.deployments {
				if strings.EqualFold(d.Name, m.pendingHighlight) {
					m.cursor = i
					break
				}
			}
			m.pendingHighlight = ""
		}

	case podsLoadedMsg:
		m.pods = msg.Pods
		m.cursor = 0
		m.clearSearch()
		if m.pendingHighlight != "" {
			for i, p := range m.pods {
				if strings.EqualFold(p.Name, m.pendingHighlight) {
					m.cursor = i
					break
				}
			}
			m.pendingHighlight = ""
		}

	case NavigateToResourceMsg:
		m.describeResult = ""
		m.cursor = 0
		m.clearSearch()
		m.pendingHighlight = msg.Name
		switch msg.Level {
		case levelNamespace:
			m.level = levelNamespace
			return m, loadNamespaces(m.k8sClient)
		case levelDeployment:
			m.selectedNS = msg.Namespace
			m.level = levelDeployment
			return m, loadDeployments(m.k8sClient, msg.Namespace)
		case levelPod:
			m.selectedNS = msg.Namespace
			m.level = levelPod
			return m, loadPods(m.k8sClient, msg.Namespace)
		}

	case resourceItemsLoadedMsg:
		if msg.Kind == m.resourceKind {
			m.resourceItems = msg.Items
			m.cursor = 0
			m.clearSearch()
			if m.pendingHighlight != "" {
				for i, item := range m.resourceItems {
					if strings.EqualFold(item.Name, m.pendingHighlight) {
						m.cursor = i
						break
					}
				}
				m.pendingHighlight = ""
			}
		}

	case describeResultMsg:
		m.describeResult = msg.Result

	case tea.MouseMsg:
		if m.describeResult != "" {
			return m, nil
		}
		listLen := m.currentListLen()
		switch msg.Button { //nolint:exhaustive // only wheel events are relevant
		case tea.MouseButtonWheelUp:
			if m.cursor > 0 {
				m.cursor--
			}
		case tea.MouseButtonWheelDown:
			if m.cursor < listLen-1 {
				m.cursor++
			}
		}

	case tea.KeyMsg:
		// Dismiss describe overlay on any key
		if m.describeResult != "" {
			m.describeResult = ""
			return m, nil
		}
		if m.searching {
			return m.updateSearch(msg)
		}
		return m.updateNavigation(msg)
	}
	return m, nil
}

func (m ResourceModel) updateNavigation(msg tea.KeyMsg) (ResourceModel, tea.Cmd) {
	listLen := m.currentListLen()

	switch msg.String() {
	case "j", "down":
		if m.cursor < listLen-1 {
			m.cursor++
		}

	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}

	case "enter":
		return m.drillIn()

	case "esc", "backspace":
		return m.drillOut()

	case "/":
		m.searching = true
		m.searchQuery = ""
		m.filtered = nil

	case "l":
		// Emit NavigateToLogsMsg for selected pod
		if m.level == levelPod && len(m.pods) > 0 {
			pod := m.pods[m.clampedCursor()]
			return m, func() tea.Msg {
				return NavigateToLogsMsg{
					Namespace: m.selectedNS,
					PodName:   pod.Name,
				}
			}
		}
		if m.level == levelContainer && len(m.containers) > 0 {
			container := m.containers[m.clampedCursor()]
			return m, func() tea.Msg {
				return NavigateToLogsMsg{
					Namespace: m.selectedNS,
					PodName:   m.selectedPod,
					Container: container.Name,
				}
			}
		}

	case "d":
		return m.describeSelected()

	case "y":
		// Copy resource name — noop in TUI context (clipboard requires OS integration)

	case "1", "2", "3", "4", "5":
		// Phase 2: Switch resource kind (only at deployment level, i.e. inside a namespace)
		if m.level == levelDeployment {
			return m.switchResourceKind(msg.String())
		}
	}

	return m, nil
}

func (m ResourceModel) updateSearch(msg tea.KeyMsg) (ResourceModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.clearSearch()
	case "enter":
		m.searching = false
		// Keep filter applied
	case "backspace":
		if len(m.searchQuery) > 0 {
			_, size := utf8.DecodeLastRuneInString(m.searchQuery)
			m.searchQuery = m.searchQuery[:len(m.searchQuery)-size]
			m.applyFilter()
		}
	default:
		if len(msg.Runes) > 0 {
			m.searchQuery += string(msg.Runes)
			m.applyFilter()
		}
	}
	return m, nil
}

func (m *ResourceModel) applyFilter() {
	names := m.currentListNames()
	if m.searchQuery == "" {
		m.filtered = nil
		return
	}
	matches := fuzzy.Find(m.searchQuery, names)
	m.filtered = make([]int, len(matches))
	for i, match := range matches {
		m.filtered[i] = match.Index
	}
	m.cursor = 0
}

func (m *ResourceModel) clearSearch() {
	m.searching = false
	m.searchQuery = ""
	m.filtered = nil
}

func (m ResourceModel) drillIn() (ResourceModel, tea.Cmd) {
	idx := m.clampedCursor()
	switch m.level {
	case levelNamespace:
		if len(m.namespaces) == 0 {
			return m, nil
		}
		m.selectedNS = m.namespaces[idx].Name
		m.level = levelDeployment
		m.cursor = 0
		m.resourceKind = k8s.KindDeployment // reset to default when entering namespace
		m.clearSearch()
		return m, loadDeployments(m.k8sClient, m.selectedNS)

	case levelDeployment:
		// Handle both native Deployment list and registry-based resource items
		if m.resourceKind == k8s.KindDeployment {
			if len(m.deployments) == 0 {
				return m, nil
			}
			dep := m.deployments[idx]
			m.selectedDep = dep.Name
			m.level = levelPod
			m.cursor = 0
			m.clearSearch()
			if len(dep.Selector) > 0 {
				return m, func() tea.Msg {
					pods, err := m.k8sClient.Resources().ListPodsBySelector(m.selectedNS, dep.Selector)
					if err != nil {
						return ErrorMsg{Err: fmt.Errorf("list pods by selector: %w", err)}
					}
					return podsLoadedMsg{Pods: pods}
				}
			}
			return m, loadPods(m.k8sClient, m.selectedNS)
		}
		// Registry-based resource items: drill into pods via selector
		if len(m.resourceItems) == 0 {
			return m, nil
		}
		item := m.resourceItems[idx]
		m.selectedDep = item.Name
		m.level = levelPod
		m.cursor = 0
		m.clearSearch()
		if len(item.Selector) > 0 {
			selector := item.Selector
			return m, func() tea.Msg {
				pods, err := m.k8sClient.Resources().ListPodsBySelector(m.selectedNS, selector)
				if err != nil {
					return ErrorMsg{Err: fmt.Errorf("list pods by selector: %w", err)}
				}
				return podsLoadedMsg{Pods: pods}
			}
		}
		// For CronJobs without direct selectors, load all pods in namespace
		return m, loadPods(m.k8sClient, m.selectedNS)

	case levelPod:
		if len(m.pods) == 0 {
			return m, nil
		}
		pod := m.pods[idx]
		m.selectedPod = pod.Name
		m.containers = pod.Containers
		m.level = levelContainer
		m.cursor = 0
		m.clearSearch()
	}
	return m, nil
}

func (m ResourceModel) drillOut() (ResourceModel, tea.Cmd) {
	switch m.level {
	case levelDeployment:
		m.level = levelNamespace
		m.cursor = 0
		m.resourceKind = k8s.KindDeployment // reset to default on back to namespace
		m.clearSearch()
	case levelPod:
		m.level = levelDeployment
		m.cursor = 0
		m.clearSearch()
	case levelContainer:
		m.level = levelPod
		m.cursor = 0
		m.clearSearch()
	}
	return m, nil
}

// switchResourceKind handles 1-5 key presses to change the displayed resource type.
func (m ResourceModel) switchResourceKind(key string) (ResourceModel, tea.Cmd) {
	kinds := k8s.AllKinds() // [Deployment, StatefulSet, DaemonSet, Job, CronJob]
	idx := int(key[0]-'0') - 1
	if idx < 0 || idx >= len(kinds) {
		return m, nil
	}
	kind := kinds[idx]
	if kind == m.resourceKind {
		return m, nil // already showing this kind
	}
	m.resourceKind = kind
	m.cursor = 0
	m.clearSearch()
	if kind == k8s.KindDeployment {
		return m, loadDeployments(m.k8sClient, m.selectedNS)
	}
	return m, loadResourceItems(m.k8sClient, m.selectedNS, kind)
}

func (m ResourceModel) describeSelected() (ResourceModel, tea.Cmd) {
	if m.k8sClient == nil {
		return m, nil
	}
	idx := m.clampedCursor()
	switch m.level {
	case levelNamespace:
		if len(m.namespaces) > 0 {
			return m, loadDescribe(m.k8sClient, "", "Namespace", m.namespaces[idx].Name)
		}
	case levelDeployment:
		if m.resourceKind == k8s.KindDeployment {
			if len(m.deployments) > 0 {
				return m, loadDescribe(m.k8sClient, m.selectedNS, "Deployment", m.deployments[idx].Name)
			}
		} else if len(m.resourceItems) > 0 {
			item := m.resourceItems[idx]
			return m, loadDescribe(m.k8sClient, m.selectedNS, string(item.Kind), item.Name)
		}
	case levelPod:
		if len(m.pods) > 0 {
			pod := m.pods[idx]
			ns := m.selectedNS
			return m, func() tea.Msg {
				return showPodDetailMsg{Namespace: ns, PodName: pod.Name}
			}
		}
	}
	return m, nil
}

func (m ResourceModel) reloadCurrentLevel() tea.Cmd {
	if m.k8sClient == nil {
		return nil
	}
	switch m.level {
	case levelNamespace:
		return loadNamespaces(m.k8sClient)
	case levelDeployment:
		if m.resourceKind == k8s.KindDeployment {
			return loadDeployments(m.k8sClient, m.selectedNS)
		}
		return loadResourceItems(m.k8sClient, m.selectedNS, m.resourceKind)
	case levelPod:
		return loadPods(m.k8sClient, m.selectedNS)
	}
	return nil
}

// --- View ---

// View renders the resource browser panel.
func (m ResourceModel) View() string {
	// Describe overlay takes precedence
	if m.describeResult != "" {
		var b strings.Builder
		b.WriteString(m.theme.Title.Render("Describe"))
		b.WriteString("\n\n")
		// Show as much as fits in the viewport
		lines := strings.Split(m.describeResult, "\n")
		viewportH := max(m.height-3, 1)
		for i, line := range lines {
			if i >= viewportH {
				b.WriteString(m.theme.Subtitle.Render("... (press any key to close)"))
				break
			}
			b.WriteString(line)
			b.WriteString("\n")
		}
		if len(lines) <= viewportH {
			b.WriteString("\n")
			b.WriteString(m.theme.Subtitle.Render("(press any key to close)"))
		}
		return b.String()
	}

	var b strings.Builder

	// Breadcrumb
	b.WriteString(m.theme.Title.Render(m.breadcrumb()))
	b.WriteString("\n")

	if m.searching {
		b.WriteString(m.theme.Subtitle.Render("/ " + m.searchQuery + "_"))
		b.WriteString("\n")
	}

	// List items
	names := m.currentListNames()
	indices := m.visibleIndices()

	// Compute visible area (account for header lines)
	headerLines := 2
	if m.searching {
		headerLines = 3
	}
	viewportH := max(m.height-headerLines, 1)

	// Scroll offset: keep cursor visible
	scrollOff := 0
	if m.cursor >= viewportH {
		scrollOff = m.cursor - viewportH + 1
	}

	rendered := 0
	for i, idx := range indices {
		if i < scrollOff {
			continue
		}
		if rendered >= viewportH {
			break
		}

		line := m.formatItem(idx, names[idx])
		if i == m.cursor {
			b.WriteString(m.theme.Selected.Render("> " + line))
		} else {
			b.WriteString("  " + line)
		}
		b.WriteString("\n")
		rendered++
	}

	if len(indices) == 0 {
		b.WriteString(m.theme.Subtitle.Render("  (empty)"))
		b.WriteString("\n")
	}

	return b.String()
}

func (m ResourceModel) breadcrumb() string {
	parts := []string{"Cluster"}
	if m.level >= levelDeployment {
		parts = append(parts, m.selectedNS)
	}
	if m.level >= levelPod {
		parts = append(parts, m.selectedDep)
	}
	if m.level >= levelContainer {
		parts = append(parts, m.selectedPod)
	}

	label := ""
	switch m.level {
	case levelNamespace:
		label = "Namespaces"
	case levelDeployment:
		label = string(m.resourceKind) + "s"
	case levelPod:
		label = "Pods"
	case levelContainer:
		label = "Containers"
	}

	return strings.Join(parts, " > ") + " > " + label
}

func (m ResourceModel) formatItem(idx int, name string) string {
	switch m.level {
	case levelDeployment:
		if m.resourceKind == k8s.KindDeployment {
			if idx < len(m.deployments) {
				dep := m.deployments[idx]
				return fmt.Sprintf("%-30s %d/%d", dep.Name, dep.Ready, dep.Replicas)
			}
		} else if idx < len(m.resourceItems) {
			item := m.resourceItems[idx]
			if item.Status != "" {
				return fmt.Sprintf("%-30s %-12s %s", item.Name, item.Ready, item.Status)
			}
			return fmt.Sprintf("%-30s %s", item.Name, item.Ready)
		}
	case levelPod:
		if idx < len(m.pods) {
			pod := m.pods[idx]
			return fmt.Sprintf("%-30s %-18s R:%d  %s",
				pod.Name, pod.Status, pod.Restarts, formatAge(pod.Age))
		}
	case levelContainer:
		if idx < len(m.containers) {
			c := m.containers[idx]
			readyStr := "not-ready"
			if c.Ready {
				readyStr = "ready"
			}
			return fmt.Sprintf("%-30s %-10s %s", c.Name, c.State, readyStr)
		}
	}
	return name
}

func formatAge(d interface{ String() string }) string {
	return d.String()
}

// --- Helpers ---

func (m ResourceModel) currentListLen() int {
	if m.filtered != nil {
		return len(m.filtered)
	}
	switch m.level {
	case levelNamespace:
		return len(m.namespaces)
	case levelDeployment:
		if m.resourceKind == k8s.KindDeployment {
			return len(m.deployments)
		}
		return len(m.resourceItems)
	case levelPod:
		return len(m.pods)
	case levelContainer:
		return len(m.containers)
	}
	return 0
}

func (m ResourceModel) currentListNames() []string {
	switch m.level {
	case levelNamespace:
		names := make([]string, len(m.namespaces))
		for i, ns := range m.namespaces {
			names[i] = ns.Name
		}
		return names
	case levelDeployment:
		if m.resourceKind == k8s.KindDeployment {
			names := make([]string, len(m.deployments))
			for i, d := range m.deployments {
				names[i] = d.Name
			}
			return names
		}
		names := make([]string, len(m.resourceItems))
		for i, item := range m.resourceItems {
			names[i] = item.Name
		}
		return names
	case levelPod:
		names := make([]string, len(m.pods))
		for i, p := range m.pods {
			names[i] = p.Name
		}
		return names
	case levelContainer:
		names := make([]string, len(m.containers))
		for i, c := range m.containers {
			names[i] = c.Name
		}
		return names
	}
	return nil
}

func (m ResourceModel) visibleIndices() []int {
	if m.filtered != nil {
		return m.filtered
	}
	n := m.currentListLen()
	indices := make([]int, n)
	for i := range n {
		indices[i] = i
	}
	return indices
}

func (m ResourceModel) clampedCursor() int {
	indices := m.visibleIndices()
	if len(indices) == 0 {
		return 0
	}
	c := m.cursor
	if c >= len(indices) {
		c = len(indices) - 1
	}
	return indices[c]
}

// Reset clears resource state for cluster switch. Returns a tea.Cmd to reload namespaces.
func (m *ResourceModel) Reset() tea.Cmd {
	m.level = levelNamespace
	m.cursor = 0
	m.namespaces = nil
	m.deployments = nil
	m.pods = nil
	m.containers = nil
	m.resourceItems = nil
	m.selectedNS = ""
	m.selectedDep = ""
	m.selectedPod = ""
	m.resourceKind = k8s.KindDeployment
	m.describeResult = ""
	m.pendingHighlight = ""
	m.clearSearch()
	return loadNamespaces(m.k8sClient)
}

// SetDimensions updates the panel dimensions.
func (m *ResourceModel) SetDimensions(w, h int) {
	m.width = w
	m.height = h
}

// BorderedView renders the resource panel with a border.
func (m ResourceModel) BorderedView(active bool) string {
	var border lipgloss.Style
	if active {
		border = m.theme.ActiveBorder
	} else {
		border = m.theme.InactiveBorder
	}
	// Account for border width (2 for left/right)
	innerW := max(m.width-2, 1)
	innerH := max(m.height-2, 1)
	return border.Width(innerW).Height(innerH).Render(m.View())
}

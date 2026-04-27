package ui

import (
	"fmt"
	"strings"

	"github.com/Orwell-Yu/korthex/internal/k8s"

	tea "github.com/charmbracelet/bubbletea"
)

// PodDetailModel is an overlay that shows detailed pod information.
// It follows the same overlay pattern as the describe overlay in resource.go.
type PodDetailModel struct {
	visible   bool
	scrollOff int

	pod    k8s.Pod
	events []k8s.Event

	// Dependencies
	k8sClient k8s.Client

	// Dimensions
	width, height int
	theme         Theme
}

// NewPodDetailModel creates a PodDetailModel.
func NewPodDetailModel(k8sClient k8s.Client, theme Theme) PodDetailModel {
	return PodDetailModel{
		k8sClient: k8sClient,
		theme:     theme,
	}
}

// Visible returns whether the overlay is shown.
func (m PodDetailModel) Visible() bool {
	return m.visible
}

// podDetailReadyMsg carries the pod detail data once loaded.
type podDetailReadyMsg struct {
	Pod    k8s.Pod
	Events []k8s.Event
}

// ShowPodDetail triggers an async load of pod details + events.
func (m PodDetailModel) ShowPodDetail(ns, podName string) (PodDetailModel, tea.Cmd) {
	m.visible = true
	m.scrollOff = 0
	return m, func() tea.Msg {
		pods, err := m.k8sClient.Resources().ListPods(ns)
		if err != nil {
			return ErrorMsg{Err: fmt.Errorf("pod detail: %w", err)}
		}

		var pod k8s.Pod
		for _, p := range pods {
			if p.Name == podName {
				pod = p
				break
			}
		}
		if pod.Name == "" {
			return ErrorMsg{Err: fmt.Errorf("pod %s not found in %s", podName, ns)}
		}

		events, err := m.k8sClient.Events().ListEvents(ns)
		if err != nil {
			// Non-fatal: show pod without events
			events = nil
		}

		// Filter events for this pod
		var podEvents []k8s.Event
		for _, e := range events {
			if strings.Contains(e.Object, pod.Name) {
				podEvents = append(podEvents, e)
			}
		}

		return podDetailReadyMsg{Pod: pod, Events: podEvents}
	}
}

// Update handles keys and data messages for the pod detail overlay.
func (m PodDetailModel) Update(msg tea.Msg) (PodDetailModel, tea.Cmd) {
	switch msg := msg.(type) {
	case podDetailReadyMsg:
		m.pod = msg.Pod
		m.events = msg.Events
		m.scrollOff = 0

	case tea.KeyMsg:
		if !m.visible {
			return m, nil
		}
		switch msg.String() {
		case "esc", "q":
			m.visible = false
			return m, nil
		case "j", "down":
			m.scrollOff++
		case "k", "up":
			if m.scrollOff > 0 {
				m.scrollOff--
			}
		}
	}
	return m, nil
}

// View renders the pod detail overlay content.
func (m PodDetailModel) View() string {
	if !m.visible || m.pod.Name == "" {
		return ""
	}

	var b strings.Builder
	b.WriteString(m.theme.Title.Render("Pod Detail: " + m.pod.Name))
	b.WriteString("\n\n")

	// Status / Node
	fmt.Fprintf(&b, "  Status:    %s\n", m.pod.Status)
	fmt.Fprintf(&b, "  Node:      %s\n", m.pod.NodeName)
	fmt.Fprintf(&b, "  Namespace: %s\n", m.pod.Namespace)
	fmt.Fprintf(&b, "  Restarts:  %d\n", m.pod.Restarts)
	fmt.Fprintf(&b, "  Age:       %s\n", formatAge(m.pod.Age))
	b.WriteString("\n")

	// Containers table
	b.WriteString(m.theme.Subtitle.Render("Containers:"))
	b.WriteString("\n")
	fmt.Fprintf(&b, "  %-25s %-10s %-10s\n", "NAME", "STATE", "READY")
	for _, c := range m.pod.Containers {
		readyStr := "false"
		if c.Ready {
			readyStr = "true"
		}
		fmt.Fprintf(&b, "  %-25s %-10s %-10s\n", c.Name, c.State, readyStr)
	}
	b.WriteString("\n")

	// Labels
	if len(m.pod.Labels) > 0 {
		b.WriteString(m.theme.Subtitle.Render("Labels:"))
		b.WriteString("\n")
		for k, v := range m.pod.Labels {
			fmt.Fprintf(&b, "  %s=%s\n", k, v)
		}
		b.WriteString("\n")
	}

	// Events
	if len(m.events) > 0 {
		b.WriteString(m.theme.Subtitle.Render("Events:"))
		b.WriteString("\n")
		fmt.Fprintf(&b, "  %-8s %-20s %s\n", "TYPE", "REASON", "MESSAGE")
		for _, e := range m.events {
			fmt.Fprintf(&b, "  %-8s %-20s %s\n", e.Type, e.Reason, e.Message)
		}
		b.WriteString("\n")
	}

	b.WriteString(m.theme.Subtitle.Render("(j/k scroll, Esc close)"))

	// Apply scroll offset
	lines := strings.Split(b.String(), "\n")
	viewportH := max(m.height-1, 1)
	if m.scrollOff > len(lines)-viewportH {
		m.scrollOff = max(len(lines)-viewportH, 0)
	}

	end := min(m.scrollOff+viewportH, len(lines))
	return strings.Join(lines[m.scrollOff:end], "\n")
}

// SetDimensions updates the overlay dimensions.
func (m *PodDetailModel) SetDimensions(w, h int) {
	m.width = w
	m.height = h
}

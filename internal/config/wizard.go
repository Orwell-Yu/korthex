package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// wizardStep represents the current step in the setup wizard.
type wizardStep int

const (
	stepSelectContext wizardStep = iota
	stepSelectProvider
	stepEnterAPIKey
	stepEnterBaseURL
	stepSelectModel
	stepPrivacyDisclosure
	stepComplete
)

// provider options
var providerOptions = []struct {
	name  string
	value string
	model string
}{
	{"OpenAI (API Key)", "openai", "gpt-4o"},
	{"Anthropic Claude (API Key)", "anthropic", "claude-sonnet-4-20250514"},
	{"Google Gemini (API Key)", "gemini", "gemini-2.5-flash"},
	{"Custom endpoint (OpenAI-compatible)", "openai", "gpt-4o"},
	{"Custom endpoint (Anthropic-compatible)", "anthropic", "claude-sonnet-4-20250514"},
}

// wizard implements the Wizard interface.
type wizard struct {
	configPath string
}

// NewWizard creates a new setup wizard.
func NewWizard(configPath ...string) Wizard {
	p := defaultConfigPath()
	if len(configPath) > 0 && configPath[0] != "" {
		p = configPath[0]
	}
	return &wizard{configPath: p}
}

// NeedsSetup returns true if the config file does not exist.
func (w *wizard) NeedsSetup() bool {
	_, err := os.Stat(w.configPath)
	return os.IsNotExist(err)
}

// Run executes the setup wizard as an independent Bubble Tea program.
func (w *wizard) Run(detected KubeDetection) (*Config, error) {
	m := newWizardModel(detected)
	p := tea.NewProgram(m)
	finalModel, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("wizard failed: %w", err)
	}

	wm, ok := finalModel.(wizardModel)
	if !ok {
		return nil, fmt.Errorf("unexpected model type from wizard")
	}

	if wm.cancelled {
		return nil, fmt.Errorf("setup wizard cancelled by user")
	}

	return wm.buildConfig(), nil
}

// styles
var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99"))
	stepStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	successStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	warningStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
)

// wizardModel is the Bubble Tea model for the setup wizard.
type wizardModel struct {
	step      wizardStep
	detected  KubeDetection
	cancelled bool

	// SelectContext
	contexts      []string
	contextCursor int

	// SelectProvider
	providerCursor int

	// EnterAPIKey
	apiKeyInput textinput.Model

	// EnterBaseURL (custom provider)
	baseURLInput textinput.Model

	// SelectModel
	modelInput textinput.Model

	// PrivacyDisclosure
	privacyConfirmed bool
}

func newWizardModel(detected KubeDetection) wizardModel {
	apiKey := textinput.New()
	apiKey.Placeholder = "sk-..."
	apiKey.EchoMode = textinput.EchoPassword
	apiKey.CharLimit = 256
	apiKey.Width = 60

	baseURL := textinput.New()
	baseURL.Placeholder = "https://api.example.com/v1"
	baseURL.CharLimit = 256
	baseURL.Width = 60

	modelInput := textinput.New()
	modelInput.CharLimit = 128
	modelInput.Width = 60

	// Default context cursor to current context
	contextCursor := 0
	for i, ctx := range detected.Contexts {
		if ctx == detected.CurrentContext {
			contextCursor = i
			break
		}
	}

	return wizardModel{
		step:          stepSelectContext,
		detected:      detected,
		contexts:      detected.Contexts,
		contextCursor: contextCursor,
		apiKeyInput:   apiKey,
		baseURLInput:  baseURL,
		modelInput:    modelInput,
	}
}

func (m wizardModel) Init() tea.Cmd {
	return nil
}

func (m wizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.cancelled = true
			return m, tea.Quit
		}
	}

	switch m.step {
	case stepSelectContext:
		return m.updateSelectContext(msg)
	case stepSelectProvider:
		return m.updateSelectProvider(msg)
	case stepEnterAPIKey:
		return m.updateEnterAPIKey(msg)
	case stepEnterBaseURL:
		return m.updateEnterBaseURL(msg)
	case stepSelectModel:
		return m.updateSelectModel(msg)
	case stepPrivacyDisclosure:
		return m.updatePrivacyDisclosure(msg)
	case stepComplete:
		return m.updateComplete(msg)
	}

	return m, nil
}

func (m wizardModel) View() string {
	var b strings.Builder

	b.WriteString("\n")
	b.WriteString(titleStyle.Render("  Welcome to Korthex! Let's get you set up."))
	b.WriteString("\n\n")

	switch m.step {
	case stepSelectContext:
		m.viewSelectContext(&b)
	case stepSelectProvider:
		m.viewSelectProvider(&b)
	case stepEnterAPIKey:
		m.viewEnterAPIKey(&b)
	case stepEnterBaseURL:
		m.viewEnterBaseURL(&b)
	case stepSelectModel:
		m.viewSelectModel(&b)
	case stepPrivacyDisclosure:
		m.viewPrivacyDisclosure(&b)
	case stepComplete:
		m.viewComplete(&b)
	}

	b.WriteString("\n")
	b.WriteString(dimStyle.Render("  Press Ctrl+C to cancel"))
	b.WriteString("\n")

	return b.String()
}

// --- SelectContext ---

func (m wizardModel) updateSelectContext(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "up", "k":
			if m.contextCursor > 0 {
				m.contextCursor--
			}
		case "down", "j":
			if m.contextCursor < len(m.contexts)-1 {
				m.contextCursor++
			}
		case "enter":
			m.step = stepSelectProvider
		}
	}
	return m, nil
}

func (m wizardModel) viewSelectContext(b *strings.Builder) {
	b.WriteString(stepStyle.Render("  Step 1/5: Kubernetes Configuration"))
	b.WriteString("\n")
	b.WriteString("  " + strings.Repeat("\u2500", 35))
	b.WriteString("\n")

	if m.detected.KubeconfigPath != "" {
		fmt.Fprintf(b, "  Detected kubeconfig at: %s\n", m.detected.KubeconfigPath)
	}
	b.WriteString("  Available contexts:\n")

	for i, ctx := range m.contexts {
		cursor := "  "
		style := dimStyle
		if i == m.contextCursor {
			cursor = "> "
			style = selectedStyle
		}
		fmt.Fprintf(b, "  %s%s\n", cursor, style.Render(ctx))
	}

	b.WriteString("\n")
	b.WriteString(dimStyle.Render("  Use arrows to select, Enter to confirm"))
	b.WriteString("\n")
}

// --- SelectProvider ---

func (m wizardModel) updateSelectProvider(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "up", "k":
			if m.providerCursor > 0 {
				m.providerCursor--
			}
		case "down", "j":
			if m.providerCursor < len(providerOptions)-1 {
				m.providerCursor++
			}
		case "enter":
			m.apiKeyInput.Focus()
			m.step = stepEnterAPIKey
			return m, textinput.Blink
		}
	}
	return m, nil
}

func (m wizardModel) viewSelectProvider(b *strings.Builder) {
	b.WriteString(stepStyle.Render("  Step 2/5: LLM Provider"))
	b.WriteString("\n")
	b.WriteString("  " + strings.Repeat("\u2500", 24))
	b.WriteString("\n")
	b.WriteString("  Select LLM provider:\n")

	for i, opt := range providerOptions {
		cursor := "  "
		style := dimStyle
		if i == m.providerCursor {
			cursor = "> "
			style = selectedStyle
		}
		fmt.Fprintf(b, "  %s%s\n", cursor, style.Render(opt.name))
	}

	b.WriteString("\n")
	b.WriteString(dimStyle.Render("  Use arrows to select, Enter to confirm"))
	b.WriteString("\n")
}

// --- EnterAPIKey ---

func (m wizardModel) updateEnterAPIKey(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.String() == "enter" {
			val := strings.TrimSpace(m.apiKeyInput.Value())
			if val != "" {
				// All providers support optional BaseURL (for proxies/gateways)
				m.baseURLInput.Focus()
				m.step = stepEnterBaseURL
				return m, textinput.Blink
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.apiKeyInput, cmd = m.apiKeyInput.Update(msg)
	return m, cmd
}

func (m wizardModel) viewEnterAPIKey(b *strings.Builder) {
	b.WriteString(stepStyle.Render("  Step 3/5: API Key"))
	b.WriteString("\n")
	b.WriteString("  " + strings.Repeat("\u2500", 19))
	b.WriteString("\n")
	fmt.Fprintf(b, "  Provider: %s\n", providerOptions[m.providerCursor].name)
	b.WriteString("  API Key: ")
	b.WriteString(m.apiKeyInput.View())
	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("  Enter your API key, then press Enter"))
	b.WriteString("\n")
}

// --- EnterBaseURL (custom provider only) ---

func (m wizardModel) updateEnterBaseURL(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.String() == "enter" {
			// BaseURL is optional — empty means use the provider's official API
			defaultModel := providerOptions[m.providerCursor].model
			if defaultModel == "" {
				m.modelInput.Placeholder = "model-name"
			} else {
				m.modelInput.Placeholder = defaultModel
			}
			m.modelInput.Focus()
			m.step = stepSelectModel
			return m, textinput.Blink
		}
	}
	var cmd tea.Cmd
	m.baseURLInput, cmd = m.baseURLInput.Update(msg)
	return m, cmd
}

func (m wizardModel) viewEnterBaseURL(b *strings.Builder) {
	provider := providerOptions[m.providerCursor]
	b.WriteString(stepStyle.Render("  Step 3.5/5: Base URL (Optional)"))
	b.WriteString("\n")
	b.WriteString("  " + strings.Repeat("\u2500", 32))
	b.WriteString("\n\n")
	b.WriteString("  If you use a proxy/gateway, enter its URL.\n")
	fmt.Fprintf(b, "  Leave empty to use the official %s API.\n\n", provider.name)
	b.WriteString("  Base URL: ")
	b.WriteString(m.baseURLInput.View())
	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("  Press Enter to continue (empty = official API)"))
	b.WriteString("\n")
}

// --- SelectModel ---

func (m wizardModel) updateSelectModel(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.String() == "enter" {
			m.step = stepPrivacyDisclosure
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.modelInput, cmd = m.modelInput.Update(msg)
	return m, cmd
}

func (m wizardModel) viewSelectModel(b *strings.Builder) {
	b.WriteString(stepStyle.Render("  Step 4/5: Model Selection"))
	b.WriteString("\n")
	b.WriteString("  " + strings.Repeat("\u2500", 26))
	b.WriteString("\n")

	defaultModel := providerOptions[m.providerCursor].model
	if defaultModel != "" {
		fmt.Fprintf(b, "  Default model: %s\n", defaultModel)
		b.WriteString("  Override (leave empty for default): ")
	} else {
		b.WriteString("  Model: ")
	}
	b.WriteString(m.modelInput.View())
	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("  Press Enter to continue"))
	b.WriteString("\n")
}

// --- PrivacyDisclosure ---

func (m wizardModel) updatePrivacyDisclosure(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "y", "Y":
			m.privacyConfirmed = true
			m.step = stepComplete
		case "n", "N":
			m.cancelled = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m wizardModel) viewPrivacyDisclosure(b *strings.Builder) {
	b.WriteString(stepStyle.Render("  Step 5/5: Privacy Disclosure"))
	b.WriteString("\n")
	b.WriteString("  " + strings.Repeat("\u2500", 29))
	b.WriteString("\n\n")

	b.WriteString(warningStyle.Render("  IMPORTANT: Privacy Notice"))
	b.WriteString("\n\n")
	b.WriteString("  When send_logs is enabled, Korthex will send Kubernetes log data\n")
	b.WriteString("  to your selected LLM provider for analysis. This data may include:\n\n")
	b.WriteString("    - Pod/container log output\n")
	b.WriteString("    - Error messages and stack traces\n")
	b.WriteString("    - Environment-specific information\n\n")
	b.WriteString("  Your LLM provider's data handling policies will apply to this data.\n\n")
	b.WriteString(warningStyle.Render("  Do you accept and wish to continue? (y/n)"))
	b.WriteString("\n")
}

// --- Complete ---

func (m wizardModel) updateComplete(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.String() == "enter" {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m wizardModel) viewComplete(b *strings.Builder) {
	provider := providerOptions[m.providerCursor]
	model := m.selectedModel()
	context := m.selectedContext()

	b.WriteString(successStyle.Render("  Setup Complete!"))
	b.WriteString("\n\n")
	fmt.Fprintf(b, "  Context:  %s\n", context)
	fmt.Fprintf(b, "  Provider: %s\n", provider.value)
	fmt.Fprintf(b, "  Model:    %s\n", model)
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("  Press Enter to start Korthex..."))
	b.WriteString("\n")
}

// --- Helpers ---

func (m wizardModel) selectedContext() string {
	if len(m.contexts) == 0 {
		return ""
	}
	return m.contexts[m.contextCursor]
}

func (m wizardModel) selectedModel() string {
	val := strings.TrimSpace(m.modelInput.Value())
	if val != "" {
		return val
	}
	return providerOptions[m.providerCursor].model
}

func (m wizardModel) buildConfig() *Config {
	defaults := applyDefaults()

	cfg := &Config{
		Kubernetes: KubernetesConfig{
			Kubeconfig:     m.detected.KubeconfigPath,
			DefaultContext: m.selectedContext(),
		},
		LLM: LLMConfig{
			Provider:    providerOptions[m.providerCursor].value,
			APIKey:      strings.TrimSpace(m.apiKeyInput.Value()),
			Model:       m.selectedModel(),
			BaseURL:     strings.TrimSpace(m.baseURLInput.Value()),
			Temperature: defaults.LLM.Temperature,
			MaxTokens:   defaults.LLM.MaxTokens,
			SendLogs:    true,
		},
		Agent: defaults.Agent,
		UI:    defaults.UI,
	}

	return cfg
}

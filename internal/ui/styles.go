package ui

import (
	"github.com/Orwell-Yu/korthex/pkg/logparse"

	"github.com/charmbracelet/lipgloss"
)

// Theme holds all Lipgloss styles for the TUI.
type Theme struct {
	Name string

	// Panel borders
	ActiveBorder   lipgloss.Style
	InactiveBorder lipgloss.Style

	// Text styles
	Title    lipgloss.Style
	Subtitle lipgloss.Style
	Status   lipgloss.Style
	Error    lipgloss.Style
	Selected lipgloss.Style // selected row highlight

	// Severity colors (for log lines)
	SeverityStyles map[logparse.Severity]lipgloss.Style

	// Status bar
	StatusBar      lipgloss.Style
	StatusBarKey   lipgloss.Style
	StatusBarValue lipgloss.Style

	// Help overlay
	HelpOverlay lipgloss.Style
	HelpKey     lipgloss.Style
	HelpDesc    lipgloss.Style

	// Base colors
	FG       lipgloss.Color
	BG       lipgloss.Color
	DimFG    lipgloss.Color
	AccentFG lipgloss.Color
}

// Predefined theme names.
const (
	ThemeDark    = "dark"
	ThemeLight   = "light"
	ThemeDracula = "dracula"
	ThemeNord    = "nord"
)

// severityColors are fixed across all themes per CLAUDE.md Rule #7:
// ERROR=red, WARN=yellow, INFO=blue, DEBUG=gray, FATAL=bold red
var severityColors = map[logparse.Severity]struct {
	fg   lipgloss.Color
	bold bool
}{
	logparse.SeverityFatal:   {fg: lipgloss.Color("#FF0000"), bold: true},
	logparse.SeverityError:   {fg: lipgloss.Color("#FF5555"), bold: false},
	logparse.SeverityWarn:    {fg: lipgloss.Color("#FFFF55"), bold: false},
	logparse.SeverityInfo:    {fg: lipgloss.Color("#5555FF"), bold: false},
	logparse.SeverityDebug:   {fg: lipgloss.Color("#888888"), bold: false},
	logparse.SeverityUnknown: {fg: lipgloss.Color("#AAAAAA"), bold: false},
}

func buildSeverityStyles() map[logparse.Severity]lipgloss.Style {
	m := make(map[logparse.Severity]lipgloss.Style, len(severityColors))
	for sev, sc := range severityColors {
		s := lipgloss.NewStyle().Foreground(sc.fg)
		if sc.bold {
			s = s.Bold(true)
		}
		m[sev] = s
	}
	return m
}

// GetTheme returns the Theme for the given name. Falls back to dark.
func GetTheme(name string) Theme {
	switch name {
	case ThemeLight:
		return lightTheme()
	case ThemeDracula:
		return draculaTheme()
	case ThemeNord:
		return nordTheme()
	default:
		return darkTheme()
	}
}

// --- Dark theme (default) ---

func darkTheme() Theme {
	accent := lipgloss.Color("#7C3AED")
	dim := lipgloss.Color("#555555")
	fg := lipgloss.Color("#EEEEEE")
	bg := lipgloss.Color("#1A1A2E")
	dimFG := lipgloss.Color("#777777")

	return Theme{
		Name:           ThemeDark,
		ActiveBorder:   lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).BorderForeground(accent),
		InactiveBorder: lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).BorderForeground(dim),
		Title:          lipgloss.NewStyle().Bold(true).Foreground(accent),
		Subtitle:       lipgloss.NewStyle().Foreground(dimFG),
		Status:         lipgloss.NewStyle().Foreground(fg),
		Error:          lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5555")).Bold(true),
		Selected:       lipgloss.NewStyle().Background(lipgloss.Color("#3D3D5C")).Foreground(fg).Bold(true),
		SeverityStyles: buildSeverityStyles(),
		StatusBar:      lipgloss.NewStyle().Background(lipgloss.Color("#2D2D4A")).Foreground(fg),
		StatusBarKey:   lipgloss.NewStyle().Background(lipgloss.Color("#7C3AED")).Foreground(lipgloss.Color("#FFFFFF")).Bold(true).Padding(0, 1),
		StatusBarValue: lipgloss.NewStyle().Background(lipgloss.Color("#2D2D4A")).Foreground(fg).Padding(0, 1),
		HelpOverlay:    lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(1, 2),
		HelpKey:        lipgloss.NewStyle().Foreground(accent).Bold(true).Width(16),
		HelpDesc:       lipgloss.NewStyle().Foreground(fg),
		FG:             fg,
		BG:             bg,
		DimFG:          dimFG,
		AccentFG:       accent,
	}
}

// --- Light theme ---

func lightTheme() Theme {
	accent := lipgloss.Color("#6D28D9")
	dim := lipgloss.Color("#CCCCCC")
	fg := lipgloss.Color("#1A1A1A")
	bg := lipgloss.Color("#FAFAFA")
	dimFG := lipgloss.Color("#999999")

	return Theme{
		Name:           ThemeLight,
		ActiveBorder:   lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).BorderForeground(accent),
		InactiveBorder: lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).BorderForeground(dim),
		Title:          lipgloss.NewStyle().Bold(true).Foreground(accent),
		Subtitle:       lipgloss.NewStyle().Foreground(dimFG),
		Status:         lipgloss.NewStyle().Foreground(fg),
		Error:          lipgloss.NewStyle().Foreground(lipgloss.Color("#DC2626")).Bold(true),
		Selected:       lipgloss.NewStyle().Background(lipgloss.Color("#E8E0F0")).Foreground(fg).Bold(true),
		SeverityStyles: buildSeverityStyles(),
		StatusBar:      lipgloss.NewStyle().Background(lipgloss.Color("#E8E0F0")).Foreground(fg),
		StatusBarKey:   lipgloss.NewStyle().Background(lipgloss.Color("#6D28D9")).Foreground(lipgloss.Color("#FFFFFF")).Bold(true).Padding(0, 1),
		StatusBarValue: lipgloss.NewStyle().Background(lipgloss.Color("#E8E0F0")).Foreground(fg).Padding(0, 1),
		HelpOverlay:    lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(1, 2),
		HelpKey:        lipgloss.NewStyle().Foreground(accent).Bold(true).Width(16),
		HelpDesc:       lipgloss.NewStyle().Foreground(fg),
		FG:             fg,
		BG:             bg,
		DimFG:          dimFG,
		AccentFG:       accent,
	}
}

// --- Dracula theme ---

func draculaTheme() Theme {
	accent := lipgloss.Color("#BD93F9") // purple
	dim := lipgloss.Color("#6272A4")    // comment
	fg := lipgloss.Color("#F8F8F2")     // foreground
	bg := lipgloss.Color("#282A36")     // background
	dimFG := lipgloss.Color("#6272A4")

	return Theme{
		Name:           ThemeDracula,
		ActiveBorder:   lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).BorderForeground(accent),
		InactiveBorder: lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).BorderForeground(dim),
		Title:          lipgloss.NewStyle().Bold(true).Foreground(accent),
		Subtitle:       lipgloss.NewStyle().Foreground(dimFG),
		Status:         lipgloss.NewStyle().Foreground(fg),
		Error:          lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5555")).Bold(true),
		Selected:       lipgloss.NewStyle().Background(lipgloss.Color("#44475A")).Foreground(fg).Bold(true),
		SeverityStyles: buildSeverityStyles(),
		StatusBar:      lipgloss.NewStyle().Background(lipgloss.Color("#44475A")).Foreground(fg),
		StatusBarKey:   lipgloss.NewStyle().Background(lipgloss.Color("#BD93F9")).Foreground(lipgloss.Color("#282A36")).Bold(true).Padding(0, 1),
		StatusBarValue: lipgloss.NewStyle().Background(lipgloss.Color("#44475A")).Foreground(fg).Padding(0, 1),
		HelpOverlay:    lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(1, 2),
		HelpKey:        lipgloss.NewStyle().Foreground(accent).Bold(true).Width(16),
		HelpDesc:       lipgloss.NewStyle().Foreground(fg),
		FG:             fg,
		BG:             bg,
		DimFG:          dimFG,
		AccentFG:       accent,
	}
}

// --- Nord theme ---

func nordTheme() Theme {
	accent := lipgloss.Color("#88C0D0") // frost
	dim := lipgloss.Color("#4C566A")    // polar night
	fg := lipgloss.Color("#ECEFF4")     // snow storm
	bg := lipgloss.Color("#2E3440")     // polar night
	dimFG := lipgloss.Color("#616E88")

	return Theme{
		Name:           ThemeNord,
		ActiveBorder:   lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).BorderForeground(accent),
		InactiveBorder: lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).BorderForeground(dim),
		Title:          lipgloss.NewStyle().Bold(true).Foreground(accent),
		Subtitle:       lipgloss.NewStyle().Foreground(dimFG),
		Status:         lipgloss.NewStyle().Foreground(fg),
		Error:          lipgloss.NewStyle().Foreground(lipgloss.Color("#BF616A")).Bold(true),
		Selected:       lipgloss.NewStyle().Background(lipgloss.Color("#3B4252")).Foreground(fg).Bold(true),
		SeverityStyles: buildSeverityStyles(),
		StatusBar:      lipgloss.NewStyle().Background(lipgloss.Color("#3B4252")).Foreground(fg),
		StatusBarKey:   lipgloss.NewStyle().Background(lipgloss.Color("#88C0D0")).Foreground(lipgloss.Color("#2E3440")).Bold(true).Padding(0, 1),
		StatusBarValue: lipgloss.NewStyle().Background(lipgloss.Color("#3B4252")).Foreground(fg).Padding(0, 1),
		HelpOverlay:    lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(1, 2),
		HelpKey:        lipgloss.NewStyle().Foreground(accent).Bold(true).Width(16),
		HelpDesc:       lipgloss.NewStyle().Foreground(fg),
		FG:             fg,
		BG:             bg,
		DimFG:          dimFG,
		AccentFG:       accent,
	}
}

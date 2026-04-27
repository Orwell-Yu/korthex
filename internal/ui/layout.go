package ui

// PanelDimensions holds the calculated dimensions for all panels.
type PanelDimensions struct {
	ResourceW, ResourceH   int
	LogViewerW, LogViewerH int
	ChatW, ChatH           int
	StatusBarH             int // always 1
}

const (
	minTermWidth  = 60
	minTermHeight = 16
	statusBarH    = 1
)

// CalculateLayout computes panel dimensions for the given terminal size and layout mode.
func CalculateLayout(width, height int, mode LayoutMode) PanelDimensions {
	dim := PanelDimensions{StatusBarH: statusBarH}

	// Usable height excludes the status bar.
	usable := max(height-statusBarH, 1)

	switch mode {
	case LayoutFull:
		// Left 25% = resource browser, right 75% = log viewer (top 60%) + chat (bottom 40%)
		dim.ResourceW = width / 4
		rightW := width - dim.ResourceW
		dim.ResourceH = usable

		dim.LogViewerW = rightW
		dim.LogViewerH = max(usable*60/100, 1)

		dim.ChatW = rightW
		dim.ChatH = max(usable-dim.LogViewerH, 1)

	case LayoutChatFocus:
		// Chat full width top 70%, log viewer bottom 30%, no resource panel.
		dim.ResourceW = 0
		dim.ResourceH = 0

		dim.ChatW = width
		dim.ChatH = max(usable*70/100, 1)

		dim.LogViewerW = width
		dim.LogViewerH = max(usable-dim.ChatH, 1)

	case LayoutLogFocus:
		// Full screen log viewer, no resource panel, no chat.
		dim.ResourceW = 0
		dim.ResourceH = 0
		dim.ChatW = 0
		dim.ChatH = 0

		dim.LogViewerW = width
		dim.LogViewerH = usable
	}

	return dim
}

// IsTerminalTooSmall returns true if the terminal is below the minimum usable size.
func IsTerminalTooSmall(width, height int) bool {
	return width < minTermWidth || height < minTermHeight
}

// TerminalTooSmallMsg returns a warning string for undersized terminals.
func TerminalTooSmallMsg(width, height int) string {
	return "Terminal too small. Minimum: 80x24, current: " +
		itoa(width) + "x" + itoa(height)
}

// itoa converts a small int to string without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

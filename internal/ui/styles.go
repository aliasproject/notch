package ui

import "github.com/charmbracelet/lipgloss"

// ── Palette ───────────────────────────────────────────────────────────────────

var (
	colorPrimary  = lipgloss.Color("#7C3AED")
	colorAccent   = lipgloss.Color("#06B6D4")
	colorSuccess  = lipgloss.Color("#10B981")
	colorWarning  = lipgloss.Color("#F59E0B")
	colorDanger   = lipgloss.Color("#EF4444")
	colorMuted    = lipgloss.Color("#6B7280")
	colorSubtle   = lipgloss.Color("#374151")
	colorText     = lipgloss.Color("#F9FAFB")
	colorTextDim  = lipgloss.Color("#9CA3AF")
	colorBg       = lipgloss.Color("#0F172A")
	colorBgAlt    = lipgloss.Color("#1E293B")
	colorBgAlt2   = lipgloss.Color("#1a2234")
	colorBorder   = lipgloss.Color("#2D3748")
	colorRunning  = lipgloss.Color("#10B981")
)

// ── Layout constants ──────────────────────────────────────────────────────────

// MaxContentWidth is the maximum width of the centered content column.
const MaxContentWidth = 120

// ChromeRows is the number of rows consumed by the header and footer.
// The top bar uses Padding(1, 0) = 1 blank top + 1 content + 1 blank bottom = 3 rows.
// The footer is 1 row with Padding(0, 2) = 1 row.
// Total chrome = 3 + 1 = 4.
const ChromeRows = 4

// ── Base ──────────────────────────────────────────────────────────────────────

var (
	StyleBase = lipgloss.NewStyle().
			Foreground(colorText).
			Background(colorBg)

	StyleMuted = lipgloss.NewStyle().
			Foreground(colorMuted)

	StyleDim = lipgloss.NewStyle().
			Foreground(colorTextDim)

	StylePrimary = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	StyleAccent = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	StyleSuccess = lipgloss.NewStyle().
			Foreground(colorSuccess)

	StyleWarning = lipgloss.NewStyle().
			Foreground(colorWarning)

	StyleDanger = lipgloss.NewStyle().
			Foreground(colorDanger)

	StyleRunning = lipgloss.NewStyle().
			Foreground(colorRunning).
			Bold(true)
)

// ── Chrome ────────────────────────────────────────────────────────────────────

var (
	// StyleHeaderBar — full-width top bar background
	StyleHeaderBar = lipgloss.NewStyle().
			Background(colorBgAlt).
			Foreground(colorText)

	// StyleAppName — the ⏱ TimeTUI logo text
	StyleAppName = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true).
			Padding(0, 2)

	// StyleFooterBar — full-width bottom bar
	StyleFooterBar = lipgloss.NewStyle().
			Background(colorBgAlt).
			Foreground(colorMuted)

	// StyleStatusOK — success message in footer
	StyleStatusOK = lipgloss.NewStyle().
			Foreground(colorSuccess).
			Bold(true)

	// StyleStatusErr — error message in footer
	StyleStatusErr = lipgloss.NewStyle().
			Foreground(colorDanger).
			Bold(true)
)

// ── Tabs ──────────────────────────────────────────────────────────────────────

var (
	StyleTabActive = lipgloss.NewStyle().
			Foreground(colorText).
			Background(colorPrimary).
			Bold(true).
			Padding(0, 3)

	StyleTabInactive = lipgloss.NewStyle().
			Foreground(colorMuted).
			Background(colorBgAlt).
			Padding(0, 3)
)

// ── Content area ──────────────────────────────────────────────────────────────

var (
	StyleContent = lipgloss.NewStyle().
			Background(colorBg).
			Foreground(colorText).
			Padding(1, 3)
)

// ── Typography ────────────────────────────────────────────────────────────────

var (
	StyleTitle = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	StyleSubtitle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	StyleLabel = lipgloss.NewStyle().
			Foreground(colorTextDim)

	StyleHelp = lipgloss.NewStyle().
			Foreground(colorSubtle)
)

// ── Table ─────────────────────────────────────────────────────────────────────

var (
	StyleTableHeader = lipgloss.NewStyle().
				Foreground(colorTextDim).
				Bold(true)

	StyleTableRow = lipgloss.NewStyle().
			Foreground(colorText)

	StyleTableRowAlt = lipgloss.NewStyle().
				Foreground(colorText).
				Background(colorBgAlt2)

	StyleTableRowSelected = lipgloss.NewStyle().
				Foreground(colorText).
				Background(colorPrimary).
				Bold(true)

	StyleTableRowRunning = lipgloss.NewStyle().
				Foreground(colorRunning).
				Bold(true)

	StyleTableDivider = lipgloss.NewStyle().
				Foreground(colorBorder)
)

// ── Forms ─────────────────────────────────────────────────────────────────────

var (
	StyleFormLabel = lipgloss.NewStyle().
			Foreground(colorTextDim).
			Width(16)

	StyleFormLabelFocused = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true).
				Width(16)

	StyleFormInput = lipgloss.NewStyle().
			Foreground(colorText).
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(colorBorder).
			Width(40)

	StyleFormInputFocused = lipgloss.NewStyle().
				Foreground(colorText).
				Border(lipgloss.NormalBorder(), false, false, true, false).
				BorderForeground(colorPrimary).
				Width(40)

	StyleFormHint = lipgloss.NewStyle().
			Foreground(colorSubtle)
)

// ── Buttons ───────────────────────────────────────────────────────────────────

var (
	StyleButton = lipgloss.NewStyle().
			Foreground(colorTextDim).
			Background(colorBgAlt).
			Padding(0, 3).
			Bold(true)

	StyleButtonPrimary = lipgloss.NewStyle().
				Foreground(colorText).
				Background(colorPrimary).
				Padding(0, 3).
				Bold(true)

	StyleButtonActive = lipgloss.NewStyle().
				Foreground(colorBg).
				Background(colorSuccess).
				Padding(0, 3).
				Bold(true)

	StyleButtonDanger = lipgloss.NewStyle().
				Foreground(colorText).
				Background(colorDanger).
				Padding(0, 3).
				Bold(true)
)

// ── Badges ────────────────────────────────────────────────────────────────────

var (
	StyleBadgeRunning = lipgloss.NewStyle().
				Foreground(colorBg).
				Background(colorRunning).
				Padding(0, 1).
				Bold(true)

	StyleBadgeInvoiced = lipgloss.NewStyle().
				Foreground(colorBg).
				Background(colorAccent).
				Padding(0, 1)

	StyleBadgePaid = lipgloss.NewStyle().
			Foreground(colorBg).
			Background(colorSuccess).
			Padding(0, 1)

	StyleBadgePending = lipgloss.NewStyle().
				Foreground(colorBg).
				Background(colorWarning).
				Padding(0, 1)
)

// ── Panels ────────────────────────────────────────────────────────────────────

var (
	StylePanel = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(1, 2)

	StylePanelFocused = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorPrimary).
				Padding(1, 2)

	StyleCard = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 2)
)

// ── Dropdown ──────────────────────────────────────────────────────────────────

var (
	StyleDropdown = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorPrimary).
			Background(colorBgAlt)

	StyleDropdownItem = lipgloss.NewStyle().
				Foreground(colorTextDim)

	StyleDropdownItemSelected = lipgloss.NewStyle().
					Foreground(colorText).
					Background(colorPrimary).
					Bold(true)

	StyleDropdownCreate = lipgloss.NewStyle().
				Foreground(colorPrimary)
)

// ── Layout helpers ────────────────────────────────────────────────────────────

// Center horizontally pads s so it appears centered within the given total width.
func Center(width int, s string) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	pad := (width - w) / 2
	return lipgloss.NewStyle().PaddingLeft(pad).Render(s)
}

// ContentWidth returns the actual content width clamped to MaxContentWidth.
func ContentWidth(termWidth int) int {
	if termWidth < MaxContentWidth {
		return termWidth
	}
	return MaxContentWidth
}

// ContentPad returns the left-padding needed to center MaxContentWidth within termWidth.
func ContentPad(termWidth int) int {
	if termWidth <= MaxContentWidth {
		return 0
	}
	return (termWidth - MaxContentWidth) / 2
}

// HRule renders a horizontal divider line at the given width.
func HRule(width int, color lipgloss.Color) string {
	return lipgloss.NewStyle().Foreground(color).Render(lipgloss.NewStyle().Width(width).Render(""))
}

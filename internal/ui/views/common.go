package views

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/maguiard/timetui/internal/db"
	"github.com/maguiard/timetui/internal/model"
)

// ── Palette ───────────────────────────────────────────────────────────────────

var (
	cPrimary  = lipgloss.Color("#7C3AED")
	cAccent   = lipgloss.Color("#06B6D4")
	cGreen    = lipgloss.Color("#10B981")
	cAmber    = lipgloss.Color("#F59E0B")
	cRed      = lipgloss.Color("#EF4444")
	cText     = lipgloss.Color("#E2E8F0")
	cDim      = lipgloss.Color("#64748B")
	cSubtle   = lipgloss.Color("#334155")
	cBg       = lipgloss.Color("#0F172A")
	cBorder   = lipgloss.Color("#1E293B")
)

// ── Shared message types ──────────────────────────────────────────────────────

type StatusMsg string
type ErrMsg string
type RunningChangedMsg struct{}
type ReloadMsg struct{}

// ── Shared load message types ─────────────────────────────────────────────────

type clientsLoadedMsg struct{ clients []*model.Client }
type projectsLoadedMsg struct{ projects []*model.Project }
type timerEntriesMsg []*model.Entry

// ── Shared commands ───────────────────────────────────────────────────────────

func StatusCmd(s string) tea.Cmd {
	return func() tea.Msg { return StatusMsg(s) }
}

func ErrCmd(s string) tea.Cmd {
	return func() tea.Msg { return ErrMsg(s) }
}

func RunningChangedCmd() tea.Cmd {
	return func() tea.Msg { return RunningChangedMsg{} }
}

func loadClientsCmd(database *db.DB) tea.Cmd {
	return func() tea.Msg {
		clients, err := database.ListClients()
		if err != nil {
			return ErrMsg(fmt.Sprintf("load clients: %v", err))
		}
		return clientsLoadedMsg{clients: clients}
	}
}

func loadProjectsCmd(database *db.DB, clientID int64) tea.Cmd {
	return func() tea.Msg {
		projects, err := database.ListProjects(clientID)
		if err != nil {
			return ErrMsg(fmt.Sprintf("load projects: %v", err))
		}
		return projectsLoadedMsg{projects: projects}
	}
}

func loadTimerEntriesCmd(database *db.DB) tea.Cmd {
	return func() tea.Msg {
		entries, err := database.ListEntries(0, "", "", true)
		if err != nil {
			return ErrMsg(fmt.Sprintf("load entries: %v", err))
		}
		return timerEntriesMsg(entries)
	}
}

// ── Shared key maps ───────────────────────────────────────────────────────────

type listKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	New    key.Binding
	Edit   key.Binding
	Delete key.Binding
}

var listKeys = listKeyMap{
	Up:     key.NewBinding(key.WithKeys("up", "k")),
	Down:   key.NewBinding(key.WithKeys("down", "j")),
	New:    key.NewBinding(key.WithKeys("n")),
	Edit:   key.NewBinding(key.WithKeys("e")),
	Delete: key.NewBinding(key.WithKeys("d")),
}

type formKeyMap struct {
	Submit    key.Binding
	Cancel    key.Binding
	NextField key.Binding
	PrevField key.Binding
}

var formKeys = formKeyMap{
	Submit:    key.NewBinding(key.WithKeys("enter")),
	Cancel:    key.NewBinding(key.WithKeys("esc")),
	NextField: key.NewBinding(key.WithKeys("tab")),
	PrevField: key.NewBinding(key.WithKeys("shift+tab")),
}

// ── Styles ────────────────────────────────────────────────────────────────────
//
// Rule: NO background colors on table rows or list items.
// Selection is shown with a left gutter cursor character only.
// This prevents the "dark block" artifact from width-padded cells.

var (
	// Typography
	StyleTitle    = lipgloss.NewStyle().Foreground(cText).Bold(true)
	StyleSubtitle = lipgloss.NewStyle().Foreground(cDim)
	StyleMuted    = lipgloss.NewStyle().Foreground(cDim)
	StyleDim      = lipgloss.NewStyle().Foreground(cSubtle)
	StyleDanger   = lipgloss.NewStyle().Foreground(cRed)
	StyleWarning  = lipgloss.NewStyle().Foreground(cAmber)
	StyleSuccess  = lipgloss.NewStyle().Foreground(cGreen)
	StyleRunning  = lipgloss.NewStyle().Foreground(cGreen).Bold(true)
	StylePrimary  = lipgloss.NewStyle().Foreground(cPrimary).Bold(true)
	StyleAccent   = lipgloss.NewStyle().Foreground(cAccent)
	StyleHelp     = lipgloss.NewStyle().Foreground(cSubtle)

	// Table — NO backgrounds, selection via gutter only.
	// Padding(0, 1) gives one space of breathing room on each side of every cell.
	StyleTableHeader = lipgloss.NewStyle().Foreground(cDim).Bold(true).Padding(0, 1)
	StyleTableRow    = lipgloss.NewStyle().Foreground(cText).Padding(0, 1)
	StyleTableRowSel = lipgloss.NewStyle().Foreground(cPrimary).Bold(true).Padding(0, 1)
	StyleTableRowRun = lipgloss.NewStyle().Foreground(cGreen).Bold(true).Padding(0, 1)
	StyleTableDiv    = lipgloss.NewStyle().Foreground(cBorder)

	// Forms
	StyleFormLabel        = lipgloss.NewStyle().Foreground(cDim).Width(16).PaddingRight(1)
	StyleFormLabelFocused = lipgloss.NewStyle().Foreground(cAccent).Bold(true).Width(16).PaddingRight(1)
	StyleFormInput        = lipgloss.NewStyle().
				Foreground(cText).
				Border(lipgloss.NormalBorder(), false, false, true, false).
				BorderForeground(cBorder).
				Width(40)
	StyleFormInputFocused = lipgloss.NewStyle().
				Foreground(cText).
				Border(lipgloss.NormalBorder(), false, false, true, false).
				BorderForeground(cPrimary).
				Width(40)

	// Buttons
	StyleButton = lipgloss.NewStyle().
			Foreground(cDim).
			Border(lipgloss.NormalBorder()).
			BorderForeground(cBorder).
			Padding(0, 2)
	StyleButtonPrimary = lipgloss.NewStyle().
				Foreground(cText).
				Background(cPrimary).
				Padding(0, 3).
				Bold(true)
	StyleButtonActive = lipgloss.NewStyle().
				Foreground(cBg).
				Background(cGreen).
				Padding(0, 3).
				Bold(true)
	StyleButtonDanger = lipgloss.NewStyle().
				Foreground(cText).
				Background(cRed).
				Padding(0, 3).
				Bold(true)

	// Badges
	StyleBadgeRunning = lipgloss.NewStyle().
				Foreground(cBg).
				Background(cGreen).
				Padding(0, 1).
				Bold(true)
	StyleBadgeInvoiced = lipgloss.NewStyle().
				Foreground(cBg).
				Background(cAccent).
				Padding(0, 1)
	StyleBadgePaid = lipgloss.NewStyle().
			Foreground(cBg).
			Background(cGreen).
			Padding(0, 1)
	StyleBadgePending = lipgloss.NewStyle().
				Foreground(cBg).
				Background(cAmber).
				Padding(0, 1)

	// Panels — subtle border, no filled background
	StylePanel = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cBorder).
			Padding(1, 2)
	StylePanelFocused = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(cPrimary).
				Padding(1, 2)
)

// ── Gutter cursor ─────────────────────────────────────────────────────────────

const cursorOn  = "▌ "
const cursorOff = "  "

// RowPrefix returns the gutter cursor for a list row.
func RowPrefix(selected bool) string {
	if selected {
		return lipgloss.NewStyle().Foreground(cPrimary).Render(cursorOn)
	}
	return cursorOff
}

// ── Shared utilities ──────────────────────────────────────────────────────────

func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 1 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + "…"
}

func renderConfirmDelete(target string, _ int) string {
	return lipgloss.JoinVertical(lipgloss.Left,
		StyleDanger.Bold(true).Render("Delete "+target+"?"),
		"",
		StyleMuted.Render("This cannot be undone."),
		"",
		StyleHelp.Render("y  confirm    esc  cancel"),
	)
}

func makeTextInput(placeholder string, charLimit, width int) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.CharLimit = charLimit
	ti.Width = width
	return ti
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// hRule draws a horizontal line using the border color.
func hRule(width int) string {
	return StyleTableDiv.Render(lipgloss.NewStyle().Width(width).Render(""))
}

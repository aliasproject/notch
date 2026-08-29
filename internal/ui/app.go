package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/aliasproject/notch/internal/db"
	"github.com/aliasproject/notch/internal/model"
	"github.com/aliasproject/notch/internal/theme"
	"github.com/aliasproject/notch/internal/ui/views"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Tab indices
const (
	TabTimers   = 0
	TabProjects = 1
	TabClients  = 2
	TabReports  = 3
)

// hotkeySep separates hotkey chips in the hotkey bar.
const hotkeySep = "   ·   "

// hotkeyGlobal lists the hotkeys shown on every tab, appended after the
// active view's contextual keys in the hotkey bar.
var hotkeyGlobal = []views.Hotkey{{Key: "1-4", Label: "tabs"}, {Key: "q", Label: "quit"}}

var tabNames = []string{"Timers", "Projects", "Clients", "Reports"}

// palette mirrors views/common.go's theme.Colors — kept as its own copy since
// app.go's bars are self-contained (see renderTopBar/renderFooter/
// renderHotkeyBar, which rebuild their lipgloss.Styles from these vars on
// every render call, unlike views' cached Style* vars). refreshAppTheme keeps
// this copy in sync with theme.Colors — called at init and again whenever
// theme.CheckReload reports a change (see the TickMsg handler below).
var (
	appColorPrimary lipgloss.Color
	appColorGreen   lipgloss.Color
	appColorDanger  lipgloss.Color
	appColorDim     lipgloss.Color
	appColorSubtle  lipgloss.Color
	appColorText    lipgloss.Color
	appColorBg      lipgloss.Color
	appColorBgAlt   lipgloss.Color
	appColorBorder  lipgloss.Color
)

func init() { refreshAppTheme() }

func refreshAppTheme() {
	appColorPrimary = theme.Colors.Primary
	appColorGreen = theme.Colors.Success
	appColorDanger = theme.Colors.Danger
	appColorDim = theme.Colors.Dim
	appColorSubtle = theme.Colors.Subtle
	appColorText = theme.Colors.Text
	appColorBg = theme.Colors.Bg
	appColorBgAlt = theme.Colors.BgAlt
	appColorBorder = theme.Colors.Border
}

// TickMsg is sent every second to update the running timer display.
type TickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

// themeWatch fires whenever the OS theme or notch's theme.conf changes on
// disk, so a theme switch is reflected within milliseconds rather than
// waiting for TickMsg's once-a-second poll. Started once at package init;
// nil if the filesystem watcher couldn't be set up, in which case TickMsg's
// poll (still run every tick regardless) remains the only path.
var themeWatch = theme.Watch()

// themeWatchMsg is sent when themeWatch fires.
type themeWatchMsg struct{}

// watchThemeCmd waits on themeWatch and reports it as a themeWatchMsg.
// Returns nil if there's no watcher to wait on.
func watchThemeCmd() tea.Cmd {
	if themeWatch == nil {
		return nil
	}
	return func() tea.Msg {
		if _, ok := <-themeWatch; !ok {
			return nil
		}
		return themeWatchMsg{}
	}
}

// AppModel is the root Bubble Tea model.
type AppModel struct {
	db        *db.DB
	width     int
	height    int
	activeTab int
	running   *model.Entry
	err       string
	statusMsg string

	timers   views.TimersModel
	projects views.ProjectsModel
	clients  views.ClientsModel
	reports  views.ReportsModel
}

type appKeyMap struct {
	Tab1 key.Binding
	Tab2 key.Binding
	Tab3 key.Binding
	Tab4 key.Binding
	Quit key.Binding
}

var appKeys = appKeyMap{
	Tab1: key.NewBinding(key.WithKeys("1")),
	Tab2: key.NewBinding(key.WithKeys("2")),
	Tab3: key.NewBinding(key.WithKeys("3")),
	Tab4: key.NewBinding(key.WithKeys("4")),
	Quit: key.NewBinding(key.WithKeys("q", "ctrl+c")),
}

func New(database *db.DB) (AppModel, error) {
	running, err := database.GetRunningEntry()
	if err != nil {
		return AppModel{}, fmt.Errorf("get running entry: %w", err)
	}

	return AppModel{
		db:        database,
		activeTab: TabTimers,
		running:   running,
		timers:    views.NewTimers(database),
		projects:  views.NewProjects(database),
		clients:   views.NewClients(database),
		reports:   views.NewReports(database),
	}, nil
}

func (m AppModel) Init() tea.Cmd {
	return tea.Batch(
		tickCmd(),
		watchThemeCmd(),
		m.timers.Init(),
		m.projects.Init(),
		m.clients.Init(),
		m.reports.Init(),
	)
}

func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		cw, ch := m.contentDims()
		m.timers.SetSize(cw, ch)
		m.projects.SetSize(cw, ch)
		m.clients.SetSize(cw, ch)
		m.reports.SetSize(cw, ch)
		return m, nil

	case TickMsg:
		cmds = append(cmds, tickCmd())
		m.running, _ = m.db.GetRunningEntry()
		if theme.CheckReload() {
			refreshAppTheme()
			views.RefreshTheme()
		}

	case themeWatchMsg:
		cmds = append(cmds, watchThemeCmd())
		if theme.CheckReload() {
			refreshAppTheme()
			views.RefreshTheme()
		}

	case tea.KeyMsg:
		if !m.activeViewBusy() {
			switch {
			case key.Matches(msg, appKeys.Quit):
				return m, tea.Quit
			case key.Matches(msg, appKeys.Tab1):
				m.activeTab = TabTimers
				return m, m.timers.Init()
			case key.Matches(msg, appKeys.Tab2):
				m.activeTab = TabProjects
				return m, m.projects.Init()
			case key.Matches(msg, appKeys.Tab3):
				m.activeTab = TabClients
				return m, m.clients.Init()
			case key.Matches(msg, appKeys.Tab4):
				m.activeTab = TabReports
				return m, m.reports.Init()
			}
		}
		if key.Matches(msg, appKeys.Quit) {
			return m, tea.Quit
		}

	case views.StatusMsg:
		m.statusMsg = string(msg)
		m.err = ""
		return m, nil

	case views.ErrMsg:
		m.err = string(msg)
		m.statusMsg = ""
		return m, nil

	case views.RunningChangedMsg:
		m.running, _ = m.db.GetRunningEntry()
	}

	// Key events → active tab only. Everything else → all tabs.
	if _, isKey := msg.(tea.KeyMsg); isKey {
		switch m.activeTab {
		case TabTimers:
			u, c := m.timers.Update(msg)
			m.timers = u
			cmds = append(cmds, c)
		case TabProjects:
			u, c := m.projects.Update(msg)
			m.projects = u
			cmds = append(cmds, c)
		case TabClients:
			u, c := m.clients.Update(msg)
			m.clients = u
			cmds = append(cmds, c)
		case TabReports:
			u, c := m.reports.Update(msg)
			m.reports = u
			cmds = append(cmds, c)
		}
	} else {
		u1, c1 := m.timers.Update(msg)
		m.timers = u1
		cmds = append(cmds, c1)

		u2, c2 := m.projects.Update(msg)
		m.projects = u2
		cmds = append(cmds, c2)

		u3, c3 := m.clients.Update(msg)
		m.clients = u3
		cmds = append(cmds, c3)

		u4, c4 := m.reports.Update(msg)
		m.reports = u4
		cmds = append(cmds, c4)
	}

	return m, tea.Batch(cmds...)
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m AppModel) View() string {
	if m.width == 0 {
		return ""
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		m.renderTopBar(),
		m.renderBody(),
		m.renderHotkeyBar(),
		m.renderFooter(),
	)
}

// renderTopBar renders a full-width bar with: app name | tabs centered.
// Keep it simple: build a plain string, then render it once in a styled container.
func (m AppModel) renderTopBar() string {
	// Every fragment below carries an explicit Background(appColorBgAlt): each
	// .Render() call emits its own trailing SGR reset, so once fragments are
	// concatenated into `line` and handed to the outer Background()-wrapped
	// style, any fragment (or raw gap) without its own background would show
	// the terminal's default background instead of the bar color.
	barBg := lipgloss.NewStyle().Background(appColorBgAlt)

	// ── app name (left) ───────────────────────────────────────────────────────
	// Muted wordmark, no icon: accent color is reserved for things that are
	// actually interactive (the active tab, buttons), and a clock glyph next
	// to the name was more clutter than identity. appColorDim (the inactive
	// tab color) reads as quieter/more minimal here than full-brightness
	// appColorText, which in some themes is close enough to the active-tab
	// color to compete with it for attention.
	appName := barBg.
		Foreground(appColorDim).
		Bold(true).
		Padding(0, 1).
		Render("Notch")

	// ── tabs (center) ─────────────────────────────────────────────────────────
	activeSty := barBg.
		Foreground(appColorText).
		Bold(true).
		Padding(0, 2)
	inactiveSty := barBg.
		Foreground(appColorDim).
		Padding(0, 2)
	sepSty := barBg.Foreground(appColorSubtle)

	var tabParts []string
	for i, name := range tabNames {
		if i == m.activeTab {
			tabParts = append(tabParts, activeSty.Render(name))
		} else {
			tabParts = append(tabParts, inactiveSty.Render(name))
		}
	}
	tabRow := strings.Join(tabParts, sepSty.Render(" │ "))

	// ── layout: logo flush left, tabs centred on the full window width ───────
	appW := lipgloss.Width(appName)
	tabW := lipgloss.Width(tabRow)

	// left of tabs = (m.width - tabW)/2; the logo occupies appW of that, so the
	// gap between logo and tabs is that offset minus the logo width.
	leftGap := (m.width-tabW)/2 - appW
	if leftGap < 0 {
		leftGap = 0
	}

	line := appName + barBg.Render(strings.Repeat(" ", leftGap)) + tabRow

	// Render the whole line once inside the bar container (padding adds top/bottom rows)
	return lipgloss.NewStyle().
		Background(appColorBgAlt).
		Foreground(appColorText).
		Width(m.width).
		Padding(1, 0).
		Render(line)
}

// renderHotkeyBar renders a full-width, fixed-position row of hotkeys: the
// active view's contextual keys and the global app keys as a single centered
// line. Unlike renderFooter, its content depends only on m.activeTab + that
// view's current mode — never on transient state (errors/status/busy) — so
// it never moves with content length and is never overwritten by a status
// message.
// hotkeyKeyCapStyle renders a hotkey's Key in a small boxed "cap" — a
// background-filled rectangle — so it reads as a distinct button separate
// from its plain-text label, the common hotkey-bar convention.
func hotkeyKeyCapStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(appColorText).
		Background(appColorBgAlt).
		Padding(0, 1)
}

// renderHotkeyChip renders one "[key] label" pair using capStyle for the
// boxed key and appColorSubtle for the label.
func renderHotkeyChip(h views.Hotkey, capStyle lipgloss.Style) string {
	return capStyle.Render(h.Key) +
		lipgloss.NewStyle().Background(appColorBg).Foreground(appColorSubtle).Render(" "+h.Label)
}

// hotkeyChipWidth returns the rendered width of h as drawn by renderHotkeyChip.
func hotkeyChipWidth(h views.Hotkey) int {
	return lipgloss.Width(h.Key) + 2 /* cap padding */ + 1 /* space */ + lipgloss.Width(h.Label)
}

func (m AppModel) renderHotkeyBar() string {
	pad := 2
	inner := max(10, m.width-pad*2)
	capStyle := hotkeyKeyCapStyle()
	sepStyle := lipgloss.NewStyle().Background(appColorBg).Foreground(appColorSubtle)

	renderRow := func(hs []views.Hotkey) string {
		parts := make([]string, len(hs))
		for i, h := range hs {
			parts[i] = renderHotkeyChip(h, capStyle)
		}
		return strings.Join(parts, sepStyle.Render(hotkeySep))
	}

	globalWidth := 0
	for i, h := range hotkeyGlobal {
		globalWidth += hotkeyChipWidth(h)
		if i > 0 {
			globalWidth += lipgloss.Width(hotkeySep)
		}
	}

	// Context hotkeys are dropped wholesale (never mid-chip truncated) once
	// they no longer fit, so a narrow terminal never cuts a key cap in half.
	maxContext := max(0, inner-globalWidth-lipgloss.Width(hotkeySep))
	var context []views.Hotkey
	w := 0
	for i, h := range m.activeViewHelp() {
		cw := hotkeyChipWidth(h)
		if i > 0 {
			cw += lipgloss.Width(hotkeySep)
		}
		if w+cw > maxContext {
			break
		}
		w += cw
		context = append(context, h)
	}

	combined := renderRow(hotkeyGlobal)
	if len(context) > 0 {
		combined = renderRow(context) + sepStyle.Render(hotkeySep) + combined
	}

	return lipgloss.NewStyle().
		Width(m.width).
		Padding(1, pad).
		// PaddingTop(1).
		Align(lipgloss.Center).
		Background(appColorBg).
		Foreground(appColorSubtle).
		Render(combined)
}

// renderFooter renders a full-width status bar: the running timer on the left,
// errors/status/help on the right.
func (m AppModel) renderFooter() string {
	pad := 2
	inner := m.width - pad*2
	if inner < 10 {
		inner = 10
	}

	rightBudget := 62
	if inner-rightBudget < 20 {
		rightBudget = inner / 3
	}
	if rightBudget < 10 {
		rightBudget = 10
	}

	// Every fragment below carries an explicit Background(appColorBgAlt) for the
	// same reason as renderTopBar: each .Render() ends with its own SGR reset,
	// so an un-backgrounded fragment (or raw gap) would show the terminal's
	// default background instead of the bar color once concatenated together.
	barBg := lipgloss.NewStyle().Background(appColorBgAlt)

	// ── left: running timer (or idle hint) ────────────────────────────────────
	// The running box below has Padding(0, 2), so its "●" sits two columns in
	// from its own left edge — the idle "●" gets the same two-column inset so
	// the dot lands in the exact same spot either way.
	var left string
	if m.running == nil {
		left = barBg.Render("  ") + barBg.Foreground(appColorSubtle).Render("●") +
			barBg.Render("  ") + barBg.Foreground(appColorDim).Render("idle")
	} else {
		r := m.running
		pill := lipgloss.NewStyle().
			Background(appColorGreen).
			Foreground(appColorBg).
			Bold(true).
			Padding(0, 2).
			Render("●  " + model.FormatDuration(r.Duration()))
		pillW := lipgloss.Width(pill)

		leftBudget := inner - rightBudget - 2
		if leftBudget < pillW {
			leftBudget = pillW
		}

		taskMax := leftBudget - pillW - 4
		if taskMax < 4 {
			taskMax = 4
		}
		task := barBg.
			Foreground(appColorText).
			Bold(true).
			Render(truncateStr(r.Task, taskMax))
		left = pill + barBg.Render("  ") + task

		if r.Project != nil {
			rest := leftBudget - lipgloss.Width(left) - 2
			if rest >= 6 {
				left += barBg.Render("  ") + barBg.
					Foreground(appColorDim).
					Render(truncateStr(r.Project.Client.Name+" › "+r.Project.Name, rest))
			}
		}
	}

	// ── right: error, status, or busy-mode hint ───────────────────────────────
	// Hotkeys live in the dedicated bar rendered by renderHotkeyBar(), one row
	// up — so when idle (no error/status, view not busy) this is intentionally
	// blank rather than falling back to a generic hotkey list.
	var rightText string
	var rightStyle lipgloss.Style
	switch {
	case m.err != "":
		rightText = "✖  " + m.err
		rightStyle = barBg.Foreground(appColorDanger).Bold(true)
	case m.statusMsg != "":
		rightText = "✔  " + m.statusMsg
		rightStyle = barBg.Foreground(appColorGreen).Bold(true)
	case m.activeViewBusy():
		rightText = "enter confirm  ·  esc cancel  ·  tab next field"
		rightStyle = barBg.Foreground(appColorSubtle)
	}
	right := rightStyle.Render(truncateStr(rightText, rightBudget))

	lw := lipgloss.Width(left)
	rw := lipgloss.Width(right)
	gap := inner - lw - rw
	if gap < 2 {
		gap = 2
	}

	line := left + barBg.Render(strings.Repeat(" ", gap)) + right

	// A top border visually separates this bar from the hotkey bar directly
	// above it; BorderBackground must be set explicitly (it's a separate
	// property from Background, see renderSummaryCards in reports.go for the
	// same rule) or the border row shows the terminal's default background.
	// Padding is asymmetric (top=0, bottom=1): the border row itself already
	// reads as visual space above the content, so a symmetric Padding(1, pad)
	// stacked on top of it made the gap above content look taller than the
	// gap below. Dropping the top padding balances the two, and slims the bar
	// by one row.
	return lipgloss.NewStyle().
		Background(appColorBgAlt).
		Foreground(appColorDim).
		Width(m.width).
		// Border(lipgloss.NormalBorder(), true, false, false, false).
		// BorderForeground(appColorSubtle).
		// BorderBackground(appColorBgAlt).
		Padding(1, pad).
		Render(line)
}

func truncateStr(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}

// renderBody renders the centered content column.
func (m AppModel) renderBody() string {
	_, ch := m.contentDims()
	cw := ContentWidth(m.width)
	pad := ContentPad(m.width)

	var view string
	switch m.activeTab {
	case TabTimers:
		view = m.timers.View()
	case TabProjects:
		view = m.projects.View()
	case TabClients:
		view = m.clients.View()
	case TabReports:
		view = m.reports.View()
	}

	// Content column — no background set so terminal default shows through cleanly
	column := lipgloss.NewStyle().
		Width(cw).
		Height(ch).
		Background(appColorBg).
		Padding(1, 3).
		Render(view)

	// Skip gutters only when the column truly fills the whole row (m.width
	// <= MaxContentWidth, so cw == m.width) — not just when pad == 0.
	// ContentPad truncates (m.width-cw)/2, so it can floor to 0 even past
	// MaxContentWidth (e.g. m.width=151, cw=150: a real 1-column leftover
	// that still needs a gutter). Returning `column` alone on pad==0 skipped
	// that leftover column entirely, same underlying mismatch as below but
	// via this early return instead of the split.
	if m.width == cw {
		return column
	}

	// Gutters match the body background — no color blocks. The right gutter
	// gets the true remainder (m.width - cw - pad), not `pad` again: reusing
	// the same truncated pad for both sides left the row 1 column short of
	// m.width whenever (m.width - cw) is odd — a mismatch against the top
	// bar/hotkey bar/footer, which all render at the full Width(m.width).
	leftGutter := lipgloss.NewStyle().
		Background(appColorBg).
		Width(pad).
		Height(ch).
		Render("")
	rightPad := m.width - cw - pad
	rightGutter := lipgloss.NewStyle().
		Background(appColorBg).
		Width(rightPad).
		Height(ch).
		Render("")

	return lipgloss.JoinHorizontal(lipgloss.Top, leftGutter, column, rightGutter)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func (m AppModel) activeViewBusy() bool {
	switch m.activeTab {
	case TabTimers:
		return m.timers.IsBusy()
	case TabProjects:
		return m.projects.IsBusy()
	case TabClients:
		return m.clients.IsBusy()
	case TabReports:
		return m.reports.IsBusy()
	}
	return false
}

// activeViewHelp returns the active view's contextual hotkeys for its
// current mode, mirroring the activeViewBusy() dispatch pattern.
func (m AppModel) activeViewHelp() []views.Hotkey {
	switch m.activeTab {
	case TabTimers:
		return m.timers.Help()
	case TabProjects:
		return m.projects.Help()
	case TabClients:
		return m.clients.Help()
	case TabReports:
		return m.reports.Help()
	}
	return nil
}

// contentDims returns the width and height of the centered content column.
// The chrome (top bar + banner + footer) height is set in styles.go.
func (m AppModel) contentDims() (width, height int) {
	return ContentWidth(m.width), m.height - ChromeRows
}
